"use client";
import {useState} from "react";
import Link from "next/link";
import {useTranslations,useLocale} from "next-intl";
import {useQuery,useMutation,useQueryClient} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import Modal from "@/components/Modal";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";
import type {InvestmentLedger,GetInvestmentLedgerHistoryResponse} from "@/proto/budget/v1/investment_pb";
const field="w-full rounded border border-gray-300 bg-white p-2 dark:border-gray-700 dark:bg-gray-900";
const button="rounded border border-blue-400 px-3 py-2 text-blue-600 disabled:opacity-50";
const blank=()=>({name:"",broker:"",accountType:"other",comment:"",defaultCurrency:"RUB",startedAt:"2024-01-01",reason:""});
function Accounts({tenantId}:{tenantId:string}){
 const t=useTranslations("investmentAccounts"),locale=useLocale(),{investment:api,tenant}=useClients(),qc=useQueryClient(),readOnly=useInvestmentReadOnly();
 const [include,setInclude]=useState(false),[offset,setOffset]=useState(0),[dialog,setDialog]=useState<{action:string;ledger?:InvestmentLedger}|null>(null),[draft,setDraft]=useState(blank),[key,setKey]=useState("");
 const [audit,setAudit]=useState<InvestmentLedger|null>(null),[after,setAfter]=useState<bigint>(0n),[pages,setPages]=useState<bigint[]>([]);
 const roles=useQuery<{memberships:TenantMembership[]}>({queryKey:["investment",tenantId,"account-roles"],queryFn:()=>tenant.listMyTenants({})});
 const role=roles.data?.memberships.find(m=>m.tenant?.id===tenantId)?.role;
 const canAdmin=!readOnly&&(role===1||role===2);
 const list=useQuery<{ledgers:InvestmentLedger[]}>({queryKey:["investment",tenantId,"account-list",include,offset],queryFn:()=>api.listInvestmentLedgers({includeArchived:include,limit:50,offset}),retry:false});
 const history=useQuery<GetInvestmentLedgerHistoryResponse>({queryKey:["investment",tenantId,"ledger-history",audit?.id,audit?.metadataVersion.toString(),after.toString()],enabled:!!audit,queryFn:()=>api.getInvestmentLedgerHistory({id:audit!.id,expectedMetadataVersion:audit!.metadataVersion,afterVersion:after,limit:25}),retry:false});
 const refresh=()=>qc.invalidateQueries({queryKey:["investment",tenantId]});
 const save=useMutation({mutationFn:()=>dialog?.action==="create"?api.createInvestmentLedger({name:draft.name,broker:draft.broker,accountType:draft.accountType,comment:draft.comment,defaultCurrency:draft.defaultCurrency,startedAt:{seconds:BigInt(Math.floor(Date.parse(draft.startedAt+"T00:00:00+03:00")/1000))},requestKey:key}):api.updateInvestmentLedger({id:dialog!.ledger!.id,action:dialog!.action,name:draft.name,broker:draft.broker,accountType:draft.accountType,comment:draft.comment,defaultCurrency:draft.defaultCurrency,reason:draft.reason,expectedMetadataVersion:dialog!.ledger!.metadataVersion,expectedLedgerVersion:dialog!.ledger!.version,requestKey:key}),onSuccess:()=>{setDialog(null);void refresh()}});
 const begin=(action:string,l?:InvestmentLedger)=>{setDialog({action,ledger:l});setDraft(l?{...blank(),name:l.name,broker:l.broker,accountType:l.accountType||"other",comment:l.comment,defaultCurrency:l.defaultCurrency||"RUB"}:blank());setKey(crypto.randomUUID());save.reset()};
 const input=(k:"name"|"broker"|"comment"|"reason")=><label className="block text-sm" key={k}>{t(k)}{k==="comment"?<textarea className={field} value={draft[k]} maxLength={2000} onChange={e=>change(k,e.target.value)}/>:<input className={field} value={draft[k]} required={k==="name"||k==="reason"} maxLength={k==="reason"?2000:200} onChange={e=>change(k,e.target.value)}/>}</label>;
 const change=(k:keyof ReturnType<typeof blank>,v:string)=>{setDraft(d=>({...d,[k]:v}));setKey(crypto.randomUUID());save.reset()};
 const details=(l?:InvestmentLedger)=><dl className="space-y-1 break-words text-sm">{l&&<><dt>{t("name")}</dt><dd>{l.name}</dd><dt>{t("broker")}</dt><dd>{l.broker||"—"}</dd><dt>{t("accountType")}</dt><dd>{t(`types.${l.accountType||"other"}`)}</dd><dt>{t("defaultCurrency")}</dt><dd>{l.defaultCurrency||"RUB"}</dd><dt>{t("comment")}</dt><dd className="whitespace-pre-wrap">{l.comment||"—"}</dd><dt>{t("status")}</dt><dd>{t(l.archived?"archived":"active")}</dd></>}</dl>;
 return <main className="mx-auto max-w-5xl space-y-5 px-4 py-8"><div className="flex flex-wrap items-center gap-3"><h1 className="text-3xl font-semibold">{t("title")}</h1><Link className={button} href="/investments">{t("overview")}</Link>{canAdmin&&<button className={button} onClick={()=>begin("create")}>{t("create")}</button>}</div>
 <p className="rounded bg-blue-50 p-4 text-sm text-blue-950 dark:bg-blue-950 dark:text-blue-100">{t("notice")}</p>
 <label className="flex items-center gap-2"><input type="checkbox" checked={include} onChange={e=>{setInclude(e.target.checked);setOffset(0)}}/>{t("include")}</label><button className={button} onClick={()=>{setOffset(0);void refresh()}}>{t("refresh")}</button>
 {(list.error||roles.error)&&<p role="alert">{t("error")} {String(list.error||roles.error)}</p>}
 {!list.error&&list.data?.ledgers.map(l=><article className="space-y-3 rounded-xl border border-gray-300 p-4 dark:border-gray-700" key={l.id}><h2 className="text-xl font-semibold break-words">{l.name} · {t(l.archived?"archived":"active")}</h2>{details(l)}<div className="flex flex-wrap gap-2"><Link className={button} href={`/investments/journal?ledger=${encodeURIComponent(l.id)}`}>{t("journal")}</Link><Link className={button} href={`/investments/value?newAccount=${encodeURIComponent(l.id)}&currency=${l.defaultCurrency||"RUB"}`}>{t("valuation")}</Link><button className={button} onClick={()=>{setAudit(l);setAfter(0n);setPages([])}}>{t("history")}</button>{canAdmin&&<><button className={button} onClick={()=>begin("edit",l)}>{t("edit")}</button><button className={button} onClick={()=>begin(l.archived?"restore":"archive",l)}>{t(l.archived?"restore":"archive")}</button></>}</div></article>)}
 {!list.error&&list.data?.ledgers.length===0&&<p>{t("empty")}</p>}
 <div className="flex justify-between"><button className={button} disabled={!offset||list.isFetching} onClick={()=>setOffset(x=>Math.max(0,x-50))}>{t("previous")}</button><button className={button} disabled={list.data?.ledgers.length!==50||list.isFetching} onClick={()=>setOffset(x=>x+50)}>{t("next")}</button></div>
 {dialog&&<Modal open title={t(dialog.action)} onClose={()=>{if(!save.isPending)setDialog(null)}}><form className="space-y-4" onSubmit={e=>{e.preventDefault();save.mutate()}}><fieldset className="space-y-3" disabled={save.isPending}>
 {(dialog.action==="create"||dialog.action==="edit")&&<>{input("name")}{input("broker")}<label className="block text-sm">{t("accountType")}<select className={field} value={draft.accountType} onChange={e=>change("accountType",e.target.value)}>{["regular","iis","other"].map(k=><option key={k} value={k}>{t(`types.${k}`)}</option>)}</select></label><label className="block text-sm">{t("defaultCurrency")}<select className={field} value={draft.defaultCurrency} onChange={e=>change("defaultCurrency",e.target.value)}>{["RUB","USD","EUR"].map(k=><option key={k}>{k}</option>)}</select></label>{input("comment")}</>}
 {dialog.action==="create"&&<label className="block text-sm">{t("started")}<input required className={field} type="date" min="1900-01-01" value={draft.startedAt} onChange={e=>change("startedAt",e.target.value)}/></label>}
 {dialog.action==="archive"&&<p>{t("closure")}</p>}{dialog.action!=="create"&&input("reason")}
 {save.error&&<p role="alert">{t("writeError")} {String(save.error)}</p>}<button className={button} type="submit">{t("save")}</button></fieldset></form></Modal>}
 {audit&&<Modal open title={t("history")} onClose={()=>setAudit(null)}><div className="space-y-4">{history.error?<p role="alert">{t("historyError")}</p>:history.data?.revisions.map(v=><section className="space-y-2 rounded border p-3" key={v.version.toString()}><h3>{v.version.toString()} · {t(`actions.${v.action}`)}</h3><p className="break-words text-sm">{v.recordedAt&&new Date(Number(v.recordedAt.seconds)*1000).toLocaleString(locale)} · {v.actorId}</p><p className="whitespace-pre-wrap break-words">{v.reason}</p>{v.before&&<><h4>{t("before")}</h4>{details(v.before)}</>}<h4>{t("after")}</h4>{details(v.after)}</section>)}<div className="flex justify-between"><button className={button} disabled={!pages.length||history.isFetching} onClick={()=>{setAfter(pages.at(-1)!);setPages(p=>p.slice(0,-1))}}>{t("previous")}</button><button className={button} disabled={!!history.error||!history.data?.nextAfterVersion||history.isFetching} onClick={()=>{setPages(p=>[...p,after]);setAfter(history.data!.nextAfterVersion)}}>{t("next")}</button></div></div></Modal>}
 </main>
}
export default function Page(){const tenantId=useAssetTenant();return tenantId?<Accounts key={tenantId} tenantId={tenantId}/>:null}
