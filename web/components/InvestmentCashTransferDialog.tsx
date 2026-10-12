"use client";
import {Code} from "@connectrpc/connect";
import {useEffect,useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import type {AssetAccount} from "@/proto/budget/v1/asset_pb";
import type {InvestmentCashTransfer,InvestmentSnapshot,PreviewInvestmentCashTransferResponse} from "@/proto/budget/v1/investment_pb";
import InvestmentMutationPreview from "@/components/InvestmentMutationPreview";
import InvestmentTransferTimingFields from "@/components/InvestmentTransferTimingFields";
import {transferTimePayload} from "@/lib/investment-transfer-time";
import Modal from "@/components/Modal";
import {accountCurrencies,assetAmountInput,assetTimestamp,localAssetTime,parseAssetAmount} from "@/lib/assets";
const field="w-full rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-900";
const button="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50";
export default function InvestmentCashTransferDialog({tenantId,ledgerId,transferId,onClose,onSaved}:{tenantId:string;ledgerId:string;transferId?:string;onClose:()=>void;onSaved:()=>void}){
 const t=useTranslations("investmentCashTransfer"),pt=useTranslations("investmentPreview"),{investment,asset}=useClients(),qc=useQueryClient();
 const [inbound,setInbound]=useState(true),[ordinary,setOrdinary]=useState(""),[currency,setCurrency]=useState("RUB"),[bookCurrency,setBookCurrency]=useState("RUB"),[bankAmount,setBankAmount]=useState(""),[bookAmount,setBookAmount]=useState(""),[at,setAt]=useState(localAssetTime),[note,setNote]=useState(""),[reason,setReason]=useState(""),[initialized,setInitialized]=useState(!transferId),[linkedId,setLinkedId]=useState("");
 const [key,setKey]=useState(()=>crypto.randomUUID()),[dateOnly,setDateOnly]=useState(false),[timezone,setTimezone]=useState("Europe/Moscow");
 const changed=()=>{setKey(crypto.randomUUID());mutation.reset();};
 const accounts=useQuery<{accounts:AssetAccount[]}>({queryKey:["investment-cash-accounts",tenantId],queryFn:()=>asset.listAccounts({includeArchived:false})});
 const existing=useQuery<{transfer?:InvestmentCashTransfer}>({queryKey:["investment-cash-transfer",tenantId,transferId],enabled:!!transferId,queryFn:()=>investment.getInvestmentCashTransfer({id:transferId!})});
 const selectedLedger=existing.data?.transfer?.ledgerId||ledgerId;
 const snapshot=useQuery<{snapshot?:InvestmentSnapshot}>({queryKey:["investment-cash-snapshot",tenantId,selectedLedger],enabled:!!selectedLedger,queryFn:()=>investment.getInvestmentSnapshot({ledgerId:selectedLedger,asOf:assetTimestamp(localAssetTime())})});
 const linked=accounts.data?.accounts.find(a=>a.investmentMode==="ledger"&&(linkedId?a.id===linkedId:a.investmentLedgerId===selectedLedger));
 const bank=accounts.data?.accounts.find(a=>a.id===ordinary);
 useEffect(()=>{const v=existing.data?.transfer,doc=v?.transfer;if(initialized||!doc||!accounts.data)return;const source=accounts.data.accounts.find(a=>a.id===doc.fromAccountId),incoming=source?.investmentMode!=="ledger";setInbound(incoming);setOrdinary(incoming?doc.fromAccountId:doc.toAccountId);setLinkedId(incoming?doc.toAccountId:doc.fromAccountId);const bm=incoming?doc.fromAmount:doc.toAmount,im=incoming?doc.toAmount:doc.fromAmount;setCurrency(bm?.currencyCode||"RUB");setBookCurrency(im?.currencyCode||"RUB");setBankAmount(assetAmountInput(bm?.minorUnits));setBookAmount(assetAmountInput(im?.minorUnits));setDateOnly(!!v?.timing?.occurredDate);setTimezone(v?.timing?.timezone||"Europe/Moscow");setAt(v?.timing?.occurredDate||localAssetTime(new Date(Number(doc.occurredAt?.seconds||0n)*1000+(doc.occurredAt?.nanos||0)/1e6)));setNote(doc.note);setInitialized(true);},[existing.data,accounts.data,initialized]);
 const same=currency===bookCurrency;
 const signature=JSON.stringify({inbound,ordinary,currency,bookCurrency,bankAmount,bookAmount,at,dateOnly,timezone,note,reason,key,ledgerVersion:snapshot.data?.snapshot?.ledger?.version,bankVersion:bank?.version,linkedVersion:linked?.version,version:existing.data?.transfer?.transfer?.version},(_k,v)=>typeof v==="bigint"?v.toString():v);
 const payload=(action:"create"|"amend"|"void")=>{
 if(!linked||!bank||!snapshot.data?.snapshot?.ledger)throw new Error(t("unavailable"));
  const actualBank={currencyCode:currency,minorUnits:parseAssetAmount(bankAmount,true)},actualBook={currencyCode:bookCurrency,minorUnits:parseAssetAmount(same?bankAmount:bookAmount,true)};
 const old=existing.data?.transfer?.transfer;
 const time=action==="void"?{occurredAt:undefined,timing:undefined}:transferTimePayload(at,dateOnly,timezone,old?.occurredAt,existing.data?.transfer?.timing);
  return ({action,transfer:{id:transferId||"",fromAccountId:inbound?bank.id:linked.id,toAccountId:inbound?linked.id:bank.id,fromAmount:inbound?actualBank:actualBook,toAmount:inbound?actualBook:actualBank,occurredAt:time.occurredAt,note},timing:time.timing,expectedVersion:old?.version||0n,expectedLedgerVersion:snapshot.data.snapshot.ledger.version,expectedFromAccountVersion:inbound?bank.version:linked.version,expectedToAccountVersion:inbound?linked.version:bank.version,reason,requestKey:key});
 };
 const preview=useMutation<PreviewInvestmentCashTransferResponse,Error,{action:"create"|"amend"|"void";signature:string}>({mutationFn:({action}:{action:"create"|"amend"|"void";signature:string})=>investment.previewInvestmentCashTransfer(payload(action))});
 const reviewed=(action:string)=>!preview.isPending&&!!preview.data?.preview?.valid&&!!preview.data.preview.previewHash&&preview.variables?.signature===signature&&preview.variables?.action===action;
 const mutation=useMutation({mutationFn:async(action:"create"|"amend"|"void")=>{if(!reviewed(action))throw new Error(pt("required"));return investment.mutateInvestmentCashTransfer({...payload(action),previewHash:preview.data!.preview!.previewHash})},onError:(e)=>{if([9,10].includes((e as {code?:number}).code||0))preview.reset()},onSuccess:async()=>{await qc.invalidateQueries({queryKey:["assets"]});onSaved();onClose();}});
 const busy=mutation.isPending,voided=existing.data?.transfer?.status==="voided",error=accounts.error||existing.error||snapshot.error||mutation.error||preview.error;
 const errorKey=(error as {code?:Code})?.code===Code.Aborted?"changed":(error as {code?:Code})?.code===Code.FailedPrecondition?"inconsistent":(error as {code?:Code})?.code===Code.InvalidArgument||(error as Error)?.message==="amount"?"invalid":"failed";
 const ready=!!linked&&!!bank&&!!snapshot.data?.snapshot&&initialized&&!accounts.isFetching&&!snapshot.isFetching&&!existing.isFetching&&!accounts.error&&!snapshot.error&&!existing.error;
 const reload=async()=>{changed();await Promise.all([accounts.refetch(),snapshot.refetch(),...(transferId?[existing.refetch()]:[])]);if(transferId)setInitialized(false);setReason("");};
 return <Modal open title={t("title")} onClose={()=>{if(!busy)onClose();}} maxWidthClass="max-w-2xl"><form className="space-y-4" onSubmit={e=>{e.preventDefault();mutation.mutate(transferId?"amend":"create");}}>
 <p className="text-sm text-gray-500">{t("help")}</p>{error&&<p role="alert" className="text-red-600">{t(errorKey)}</p>}{!accounts.isFetching&&!linked&&<p role="status">{t("unavailable")}</p>}
 {voided?<p>{t("voided")}</p>:<fieldset disabled={busy||(!initialized&&!!transferId)} className="space-y-3">
 <label className="block text-sm">{t("direction")}<select className={field} disabled={!!transferId} value={inbound?"in":"out"} onChange={e=>{setInbound(e.target.value==="in");changed();}}><option value="in">{t("deposit")}</option><option value="out">{t("withdrawal")}</option></select></label>
 <p className="text-sm">{t("investment")}: {linked?.name||"—"}</p>
 <label className="block text-sm">{t("ordinary")}<select className={field} required disabled={!!transferId} value={ordinary} onChange={e=>{setOrdinary(e.target.value);const c=accountCurrencies(accounts.data?.accounts.find(a=>a.id===e.target.value))[0]||"RUB";setCurrency(c);setBookCurrency(c);changed();}}><option value="">—</option>{accounts.data?.accounts.filter(a=>!a.archived&&["cash","bank","deposit"].includes(a.kind)).map(a=><option key={a.id} value={a.id}>{a.name}</option>)}</select></label>
 <label className="block text-sm">{t("currency")}<select className={field} value={currency} onChange={e=>{setCurrency(e.target.value);changed();}}>{(bank?accountCurrencies(bank):["RUB","USD","EUR"]).map(c=><option key={c}>{c}</option>)}</select></label>
 <label className="block text-sm">{t("bookCurrency")}<select className={field} value={bookCurrency} onChange={e=>{setBookCurrency(e.target.value);changed();}}>{["RUB","USD","EUR"].map(c=><option key={c}>{c}</option>)}</select></label>
 <label className="block text-sm">{t("bankAmount")} ({currency})<input className={field} required inputMode="decimal" value={bankAmount} onChange={e=>{setBankAmount(e.target.value);changed();}} /></label>
 {!same&&<label className="block text-sm">{t("bookAmount")} ({bookCurrency})<input className={field} required inputMode="decimal" value={bookAmount} onChange={e=>{setBookAmount(e.target.value);changed();}} /></label>}
 <InvestmentTransferTimingFields at={at} dateOnly={dateOnly} timezone={timezone} onChange={v=>{setAt(v.at);setDateOnly(v.dateOnly);setTimezone(v.timezone);changed()}}/>
 <label className="block text-sm">{t("note")}<textarea className={field} maxLength={5000} value={note} onChange={e=>{setNote(e.target.value);changed();}} /></label>
 {transferId&&<label className="block text-sm">{t("reason")}<textarea className={field} required maxLength={2000} value={reason} onChange={e=>{setReason(e.target.value);changed();}} /></label>}
 <div className="space-y-3"><button type="button" className={button} disabled={!ready||preview.isPending||(!!transferId&&!reason.trim())} onClick={()=>preview.mutate({action:transferId?"amend":"create",signature})}>{pt("check")}</button>{transferId&&<button type="button" className="ml-3 text-red-600" disabled={!ready||preview.isPending||!reason.trim()} onClick={()=>preview.mutate({action:"void",signature})}>{pt("checkVoid")}</button>}{preview.data?.preview&&preview.variables?.signature===signature&&<><InvestmentMutationPreview value={preview.data.preview}/>{preview.data.accounts.map((a,i)=><p key={i} className="break-all text-xs font-mono">{pt(`stages.${a.stage}`)} · {bank?.name} · {a.currency}: {a.beforeKnown?assetAmountInput(a.beforeMinor):"—"} → {a.afterKnown?assetAmountInput(a.afterMinor):"—"}</p>)}</>}</div>
 <div className="flex flex-wrap gap-3"><button className={button} disabled={!ready||!reviewed(transferId?"amend":"create")||(!!transferId&&!reason.trim())}>{t("save")}</button>{transferId&&<button type="button" className="text-red-600 disabled:opacity-50" disabled={!ready||!reason.trim()||!reviewed("void")} onClick={()=>mutation.mutate("void")}>{t("void")}</button>}<button type="button" onClick={reload}>{t("reload")}</button></div>
 </fieldset>}
 </form></Modal>;
}
