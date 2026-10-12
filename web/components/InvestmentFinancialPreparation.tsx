"use client";
import {useEffect,useRef,useState} from "react";
import Link from "next/link";
import {Code,ConnectError} from "@connectrpc/connect";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import {financialGroups,preparationMapping,selectPreparedBatch,parseTradeEvidenceFile,parseIncomeEvidenceFile,parseCashEvidenceFile,type RuleGroup,type FinancialRule} from "@/lib/investment-preparation";
import type {InvestmentImport,InvestmentImportRow,PreviewInvestmentFinancialMappingResponse as Review} from "@/proto/budget/v1/investment_import_pb";
import type {InvestmentLedger,InvestmentImportBatch} from "@/proto/budget/v1/investment_pb";
const field="mt-1 block w-full rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-950";
const button="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50";
export default function InvestmentFinancialPreparation({source}:{source:InvestmentImport}){
 const t=useTranslations("investmentPreparation");const journal=useTranslations("investmentJournal");const issues=useTranslations("investments");
 const {investment:api,investmentImport:imports}=useClients();
 const [rows,setRows]=useState<InvestmentImportRow[]|null>(null);const [ledgers,setLedgers]=useState<InvestmentLedger[]>([]);
 const [groups,setGroups]=useState<RuleGroup[]>([]);const [groupIndex,setGroupIndex]=useState(0);
 const [accounts,setAccounts]=useState<Record<string,string>>({});const [timezone,setTimezone]=useState("");const [policy,setPolicy]=useState("");
 const [settlements,setSettlements]=useState<Record<number,string>>({});const [duplicates,setDuplicates]=useState<Record<number,string>>({});
 const [incomeEvidence,setIncomeEvidence]=useState<Record<string,unknown>>({});const [useIncomeEvidence,setUseIncomeEvidence]=useState(false);
 const [cashEvidence,setCashEvidence]=useState<Record<string,unknown>>({});const [useCashEvidence,setUseCashEvidence]=useState(false);
 const [tradeEvidence,setTradeEvidence]=useState<Record<string,unknown>>({});const [useEvidence,setUseEvidence]=useState(false);
 const [busy,setBusy]=useState(false);const [uncertain,setUncertain]=useState(false);const [error,setError]=useState("");
 const [review,setReview]=useState<Review|null>(null);const [selected,setSelected]=useState<Set<number>>(new Set());
 const [sourcePage,setSourcePage]=useState(0);const [resultPage,setResultPage]=useState(0);const [serverOffset,setServerOffset]=useState(0);
 const [saved,setSaved]=useState("");const key=useRef(crypto.randomUUID());const pending=useRef<{id:string;batch:InvestmentImportBatch}|null>(null);
 const locked=busy||uncertain||!!saved;
 const active=useRef(true);
 useEffect(()=>{active.current=true;return()=>{active.current=false;};},[]);
 const timestamp=(value?:{seconds:bigint;nanos:number})=>value?new Date(Number(value.seconds)*1000+value.nanos/1000000).toISOString():"—";
 useEffect(()=>{if(!rows||saved)return;const warn=(e:BeforeUnloadEvent)=>{e.preventDefault();e.returnValue="";};window.addEventListener("beforeunload",warn);return()=>window.removeEventListener("beforeunload",warn);},[rows,saved]);
 const failure=(e:unknown)=>{const message=e instanceof Error?e.message:String(e);setError(t.has(message)?t(message):message);};
 const invalidate=()=>{setReview(null);setSelected(new Set());setServerOffset(0);setResultPage(0);setError("");key.current=crypto.randomUUID();};
 const load=async()=>{
  setBusy(true);setError("");
  try{
   const collected:InvestmentImportRow[]=[];
   while(collected.length<source.rowCount){if(!active.current)return;const response=await imports.listInvestmentImportRows({importId:source.id,offset:collected.length,limit:200});if(!response.rows.length)throw new Error("incompleteSource");for(const row of response.rows){if(row.record!==collected.length+1)throw new Error("incompleteSource");collected.push(row);}}
   const books:InvestmentLedger[]=[];
   for(let offset=0;offset<10000;offset+=200){if(!active.current)return;const response=await api.listInvestmentLedgers({limit:200,offset});books.push(...response.ledgers);if(response.ledgers.length<200)break;}
   const current=await imports.getInvestmentImport({id:source.id});if(current.import?.version!==source.version || current.import?.rowCount!==source.rowCount)throw new Error("sourceChanged");
   setRows(collected);setGroups(financialGroups(collected));setLedgers(books);
  }catch(e){failure(e);}finally{setBusy(false);}
 };
 const prepare=async(offset=0)=>{
  if(!rows)return;setBusy(true);setError("");
  try{
   const mapping=preparationMapping(source,rows,accounts,groups,timezone,policy,settlements,duplicates,useEvidence?tradeEvidence:{},useIncomeEvidence?incomeEvidence:{},useCashEvidence?cashEvidence:{});
   const response=await imports.previewInvestmentFinancialMapping({importId:source.id,expectedImportVersion:source.version,mappingJson:new TextEncoder().encode(JSON.stringify(mapping)),requestKey:key.current,offset,limit:5000});
   if(offset!==0&&review&&response.hash!==review.hash){invalidate();throw new Error("sourceChanged");}
   setReview(response);setServerOffset(offset);setResultPage(0);setSelected(new Set());
  }catch(e){setReview(null);failure(e);}finally{setBusy(false);}
 };
 const save=async()=>{
  setBusy(true);setError("");
  try{
   if(!pending.current){if(!review?.batch)throw new Error("selectRows");pending.current={id:crypto.randomUUID(),batch:selectPreparedBatch(review.batch,selected)};}
   const request=pending.current;
   const response=await api.saveInvestmentImportDraft({id:request.id,expectedVersion:0n,batch:request.batch});
   if(!response.draft)throw new Error(t("saveUncertain"));
   setSaved(response.draft.id);setUncertain(false);pending.current=null;
  }catch(e){const code=ConnectError.from(e).code;if([Code.InvalidArgument,Code.Aborted,Code.FailedPrecondition,Code.NotFound,Code.PermissionDenied,Code.Unauthenticated].includes(code)){pending.current=null;setUncertain(false);}else if(pending.current)setUncertain(true);failure(e);}finally{setBusy(false);}
 };
 const updateRule=(name:keyof FinancialRule,value:string)=>{invalidate();setGroups(items=>items.map((g,i)=>{
  if(i!==groupIndex)return g;const rule={...g.rule,[name]:value};
  if(name==="Kind"){
   if(!["buy","sell","dividend","coupon"].includes(value))rule.InstrumentKey="";
   if(!["buy","sell"].includes(value)){rule.QuantityFactor="";rule.AccruedBasis="";}
   if(!["dividend","coupon"].includes(value))rule.IncomeBasis="";
  }
  return {...g,rule};
 }));};
 const group=groups[groupIndex];
 const choice=(name:keyof FinancialRule,options:string[])=>group&&<label className="text-sm">{t(name)}<select aria-label={t(name)} className={field} disabled={locked} value={group.rule[name]} onChange={e=>updateRule(name,e.target.value)}><option value="">{t("choose")}</option>{options.map(value=><option key={value} value={value}>{t(`options.${value}`)}</option>)}</select>{group.rule[name]&&<span className="mt-1 block text-xs text-gray-500">{t(`options.${group.rule[name]}`)}</span>}</label>;
 const text=(name:keyof FinancialRule)=>group&&<label className="text-sm">{t(name)}<input className={field} disabled={locked} value={group.rule[name]} onChange={e=>updateRule(name,e.target.value)}/></label>;
 const issueLabel=(code:string)=>t.has(`issues.${code}`)?t(`issues.${code}`):issues.has(`issues.${code}`)?issues(`issues.${code}`):code;
 return <section data-testid="investment-financial-preparation" className="space-y-4 rounded-xl border border-blue-200 p-4 dark:border-blue-900">
  <h2 className="text-xl font-semibold">{t("title")}</h2><p className="text-sm text-gray-600 dark:text-gray-400">{t("help")}</p>
  {!rows&&<button className={button} disabled={busy} onClick={load}>{t(busy?"loading":"start")}</button>}
  {error&&<p role="alert" className="break-words text-sm text-red-700 dark:text-red-400">{error}</p>}
  {rows&&<>
   <fieldset disabled={locked} className="grid gap-4 md:grid-cols-2"><legend className="mb-3 font-semibold">{t("accounts")}</legend>{source.accounts.map(account=><label className="text-sm" key={account.id}>{account.name}<select className={field} value={accounts[account.id]||""} onChange={e=>{invalidate();setAccounts({...accounts,[account.id]:e.target.value});}}><option value="">{t("unassigned")}</option>{ledgers.map(ledger=><option value={ledger.id} key={ledger.id}>{ledger.name}</option>)}</select></label>)}</fieldset>
   <div className="grid gap-4 md:grid-cols-2"><label className="text-sm">{t("timezone")}<input className={field} placeholder="Europe/Moscow" value={timezone} disabled={locked} onChange={e=>{invalidate();setTimezone(e.target.value);}}/></label><label className="text-sm">{t("settlementPolicy")}<select aria-label={t("settlementPolicy")} className={field} value={policy} disabled={locked} onChange={e=>{invalidate();setPolicy(e.target.value);}}><option value="">{t("choose")}</option><option value="assume_trade_time">{t("assumed")}</option><option value="explicit">{t("explicit")}</option></select>{policy&&<span className="mt-1 block text-xs text-gray-500">{t(policy==="explicit"?"explicit":"assumed")}</span>}</label></div>
   <div className="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-800"><label className="block text-sm">{t("evidenceFile")}<input className={field} type="file" accept=".json,application/json" disabled={locked} onChange={async e=>{const file=e.target.files?.[0];invalidate();setUseEvidence(false);setTradeEvidence({});if(!file)return;setBusy(true);try{if(file.size>2*1024*1024)throw new Error("invalidEvidence");setTradeEvidence(parseTradeEvidenceFile(await file.text(),source));}catch(error){failure(error);}finally{setBusy(false);}}}/></label><p className="text-sm text-gray-500">{t("evidenceHelp")}</p>{Object.keys(tradeEvidence).length>0&&<label className="flex items-start gap-2 text-sm"><input type="checkbox" disabled={locked} checked={useEvidence} onChange={e=>{invalidate();setUseEvidence(e.target.checked);}}/>{t("useEvidence",{count:Object.keys(tradeEvidence).length})}</label>}</div>
   <div className="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-800"><label className="block text-sm">{t("incomeEvidenceFile")}<input className={field} type="file" accept=".json,application/json" disabled={locked} onChange={async e=>{const file=e.target.files?.[0];invalidate();setUseIncomeEvidence(false);setIncomeEvidence({});if(!file)return;setBusy(true);try{if(file.size>2*1024*1024)throw new Error("invalidEvidence");setIncomeEvidence(parseIncomeEvidenceFile(await file.text(),source));}catch(error){failure(error);}finally{setBusy(false);}}}/></label><p className="text-sm text-gray-500">{t("incomeEvidenceHelp")}</p>{Object.keys(incomeEvidence).length>0&&<label className="flex items-start gap-2 text-sm"><input type="checkbox" disabled={locked} checked={useIncomeEvidence} onChange={e=>{invalidate();setUseIncomeEvidence(e.target.checked);}}/>{t("useIncomeEvidence",{count:Object.keys(incomeEvidence).length})}</label>}</div>
   <div className="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-800"><label className="block text-sm">{t("cashEvidenceFile")}<input className={field} type="file" accept=".json,application/json" disabled={locked} onChange={async e=>{const file=e.target.files?.[0];invalidate();setUseCashEvidence(false);setCashEvidence({});if(!file)return;setBusy(true);try{if(file.size>2*1024*1024)throw new Error("invalidEvidence");setCashEvidence(parseCashEvidenceFile(await file.text(),source));}catch(error){failure(error);}finally{setBusy(false);}}}/></label><p className="text-sm text-gray-500">{t("cashEvidenceHelp")}</p>{Object.keys(cashEvidence).length>0&&<label className="flex items-start gap-2 text-sm"><input type="checkbox" disabled={locked} checked={useCashEvidence} onChange={e=>{invalidate();setUseCashEvidence(e.target.checked);}}/>{t("useCashEvidence",{count:Object.keys(cashEvidence).length})}</label>}</div>
   <label className="block text-sm">{t("group")}<select className={field} value={groupIndex} disabled={locked} onChange={e=>setGroupIndex(Number(e.target.value))}>{groups.map((g,i)=><option key={g.key} value={i}>{g.rule.Event} · {g.rule.Symbol||"—"} · {g.rule.Exchange||"—"} · {g.rule.Currency} ({g.count})</option>)}</select></label>
   {group&&<div className="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-gray-800"><label className="flex items-center gap-2 text-sm"><input type="checkbox" disabled={locked} checked={group.enabled} onChange={e=>{invalidate();setGroups(items=>items.map((g,i)=>i===groupIndex?{...g,enabled:e.target.checked}:g));}}/>{t("enableRule")}</label>
    {group.enabled&&<><div className="grid gap-3 md:grid-cols-2">{choice("Kind",["buy","sell","dividend","coupon","deposit","fee","tax","cash_interest"])}{["buy","sell","dividend","coupon"].includes(group.rule.Kind)&&text("InstrumentKey")}{choice("AmountBasis",["price","quantity","quantity_price","fee_tax"])}{text("AmountFactor")}{["buy","sell"].includes(group.rule.Kind)&&<>{text("QuantityFactor")}{choice("AccruedBasis",["zero_only","total","per_source_quantity","per_security_unit"])}</>}{choice("FeePolicy",["zero_only","same_currency"])}{["dividend","coupon"].includes(group.rule.Kind)&&choice("IncomeBasis",["gross","net"])}</div><p className="text-sm text-gray-500">{t("multiplierHelp")}</p>{group.rule.Kind==="coupon"&&<p className="text-sm text-amber-700 dark:text-amber-400">{t("couponHelp")}</p>}<label className="block text-sm">{t("Reason")}<textarea className={field} disabled={locked} maxLength={2000} value={group.rule.Reason} onChange={e=>updateRule("Reason",e.target.value)}/></label></>}
   </div>}
   <details><summary className="cursor-pointer text-sm text-blue-600 dark:text-blue-400">{t("sourceReview")}</summary><div className="mt-3 space-y-3">{rows.slice(sourcePage*25,sourcePage*25+25).map(row=><article key={row.record} className="space-y-2 rounded-lg border border-gray-200 p-3 text-sm dark:border-gray-800"><p className="break-words">#{row.record} · {row.sourceFields.Date} · {row.sourceFields.Event} · {row.sourceFields.Symbol} · {row.decimals.Price} {row.sourceFields.Currency} × {row.decimals.Quantity}</p><p>{t("sourceFees")}: {row.decimals.FeeTax||"0"} {row.sourceFields.FeeCurrency||"—"} · {t("sourceAccrued")}: {row.decimals.NKD||"0"}</p>{row.sourceFields.Note&&<p className="whitespace-pre-wrap break-words">{row.sourceFields.Note}</p>}{row.identicalRecords.length>0&&<label className="block">{t("duplicateReason")}<input className={field} disabled={locked} maxLength={2000} value={duplicates[row.record]||""} onChange={e=>{invalidate();setDuplicates({...duplicates,[row.record]:e.target.value});}}/></label>}{policy==="explicit"&&<label className="block">{t("settlementTime")}<input className={field} disabled={locked} placeholder="2024-01-03T12:00:00+03:00" value={settlements[row.record]||""} onChange={e=>{invalidate();const next={...settlements};if(e.target.value)next[row.record]=e.target.value;else delete next[row.record];setSettlements(next);}}/></label>}</article>)}<div className="flex gap-4"><button disabled={!sourcePage} onClick={()=>setSourcePage(p=>p-1)}>{t("previous")}</button><button disabled={(sourcePage+1)*25>=rows.length} onClick={()=>setSourcePage(p=>p+1)}>{t("next")}</button></div></div></details>
   <button className={button} disabled={locked} onClick={()=>prepare()}>{t("preview")}</button>
   {review&&<div className="space-y-3 border-t border-gray-200 pt-4 dark:border-gray-800"><p role="status">{t("counts",{ready:review.ready,unresolved:review.unresolved,total:review.total})}</p><p className="text-sm text-gray-500">{t("selectionHelp")}</p><label className="flex items-center gap-2 text-sm"><input type="checkbox" disabled={locked||!review.batch?.entries.length} checked={!!review.batch?.entries.length&&selected.size===review.batch.entries.length} onChange={e=>setSelected(new Set(e.target.checked?review.batch!.entries.map(entry=>entry.record):[]))}/>{t("selectReady",{count:review.batch?.entries.length||0})}</label>
    <ol className="space-y-2">{review.rows.slice(resultPage*25,resultPage*25+25).map(row=><li className="rounded-lg border border-gray-200 p-3 text-sm dark:border-gray-800" key={row.record}>{row.operation?<><label className="flex items-start gap-2"><input type="checkbox" disabled={locked} checked={selected.has(row.record)} onChange={e=>setSelected(previous=>{const next=new Set(previous);if(e.target.checked)next.add(row.record);else next.delete(row.record);return next;})}/><span className="min-w-0 break-words">#{row.record} · {journal(`kinds.${row.operation.kind}`)} · {row.operation.instrumentKey} · {row.operation.amount} {row.operation.currency}</span></label><p className="mt-1 break-words">{t("candidateDetail",{quantity:row.operation.quantity,fee:row.operation.fee,tax:row.operation.tax,accrued:row.operation.accrued})}</p><p className="mt-1 break-words">{ledgers.find(ledger=>ledger.id===row.operation!.ledgerId)?.name||row.operation.ledgerId}</p><p className="break-words">{journal("occurredAt")}: {timestamp(row.operation.occurredAt)}</p><p className="break-words">{journal("settlementAt")}: {timestamp(row.operation.settlementAt)} {row.operation.settlementAssumed&&t("assumedShort")}</p>{row.operation.note&&<p className="break-words text-amber-700 dark:text-amber-400">{row.operation.note}</p>}<p className="text-gray-500">{row.reviewReason}</p></>:<><p>#{row.record}</p><ul className="list-inside list-disc">{row.issues.map(code=><li className="break-words" key={code}>{issueLabel(code)}</li>)}</ul></>}</li>)}</ol>
    <div className="flex flex-wrap gap-4"><button disabled={!resultPage} onClick={()=>setResultPage(p=>p-1)}>{t("previous")}</button><button disabled={(resultPage+1)*25>=review.rows.length} onClick={()=>setResultPage(p=>p+1)}>{t("next")}</button>{source.rowCount>5000&&<><button disabled={locked||serverOffset===0} onClick={()=>prepare(Math.max(0,serverOffset-5000))}>{t("previousBlock")}</button><button disabled={locked||serverOffset+5000>=source.rowCount} onClick={()=>prepare(serverOffset+5000)}>{t("nextBlock")}</button></>}</div>
    {uncertain&&<p role="alert" className="text-sm text-amber-700 dark:text-amber-400">{t("saveUncertain")}</p>}
    {!saved&&<button className={button} disabled={busy||!selected.size} onClick={save}>{t(uncertain?"retrySave":"save",{count:selected.size})}</button>}
   </div>}
   {saved&&<div role="status" className="space-y-2 rounded-lg bg-green-50 p-3 text-green-900 dark:bg-green-950 dark:text-green-200"><p>{t("saved",{id:saved.slice(0,8)})}</p><Link className="underline" href="/investments/journal">{t("openJournal")}</Link><button className="ml-4 underline" onClick={()=>{setSaved("");invalidate();}}>{t("newReview")}</button></div>}
  </>}
 </section>;
}
