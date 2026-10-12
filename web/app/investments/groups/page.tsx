"use client";
import {useRef,useState} from "react";
import Link from "next/link";
import {useSearchParams} from "next/navigation";
import {useQuery,useMutation} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {Protected} from "@/components";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import type {InvestmentGroups,InvestmentGroupPosition} from "@/proto/budget/v1/investment_nav_pb";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";
const field="rounded-lg border border-gray-300 bg-white p-2 dark:border-gray-700 dark:bg-gray-900";
const button=field+" text-sm disabled:opacity-50";
type Draft={version:bigint;groups:{id:string;name:string}[];memberships:{ledgerId:string;instrumentKey:string;groupId:string}[]};
function Editor({tenantId,classification,positions,reload}:{tenantId:string;classification:InvestmentGroups;positions:InvestmentGroupPosition[];reload:()=>void}){
 const t=useTranslations("investmentGroups"),{investmentNAV:api,tenant}=useClients(),readOnly=useInvestmentReadOnly();
 const [draft,setDraft]=useState<Draft>({version:classification.version,groups:classification.groups.map(({id,name})=>({id,name})),memberships:classification.memberships.map(({ledgerId,instrumentKey,groupId})=>({ledgerId,instrumentKey,groupId}))});
 const [name,setName]=useState(""),[reason,setReason]=useState(""),[filter,setFilter]=useState<string[]>([]),[search,setSearch]=useState(""),[page,setPage]=useState(0);
 const retry=useRef<{signature:string;key:string}|undefined>(undefined);
 const roles=useQuery<{memberships:TenantMembership[]}>({queryKey:["investment",tenantId,"roles"],queryFn:()=>tenant.listMyTenants({})});
 const role=roles.data?.memberships.find(m=>m.tenant?.id===tenantId)?.role;
 const canWrite=!readOnly&&(role===1||role===2);
 const save=useMutation({mutationFn:()=>{const payload={classification:draft,reason:reason.trim()};const signature=JSON.stringify(payload,(_k,v)=>typeof v==="bigint"?v.toString():v);if(retry.current?.signature!==signature)retry.current={signature,key:crypto.randomUUID()};return api.saveInvestmentGroups({...payload,requestKey:retry.current.key});},onSuccess:()=>reload()});
 const locked=!canWrite||save.isPending;
 const member=(p:InvestmentGroupPosition)=>draft.memberships.find(m=>m.ledgerId===p.ledgerId&&m.instrumentKey===p.instrumentKey)?.groupId||"";
 const shown=positions.filter(p=>(filter.length===0||filter.includes(member(p)))&&`${p.ledgerName} ${p.instrumentKey}`.toLowerCase().includes(search.toLowerCase()));
 const change=(p:InvestmentGroupPosition,groupId:string)=>setDraft(d=>({...d,memberships:[...d.memberships.filter(m=>m.ledgerId!==p.ledgerId||m.instrumentKey!==p.instrumentKey),...(groupId?[{ledgerId:p.ledgerId,instrumentKey:p.instrumentKey,groupId}]:[])]}));
 return <div className="space-y-5">
  <p className="rounded-xl bg-amber-50 p-4 text-amber-900 dark:bg-amber-950 dark:text-amber-100">{t("policy")}</p>
  <p className="text-sm text-gray-500">{t("limits")}</p>
  {roles.error&&<p role="alert">{t("error")}</p>}
  <fieldset disabled={locked} className="space-y-3"><legend className="text-xl font-semibold">{t("groups")}</legend>
   {draft.groups.map(g=><div key={g.id} className="flex gap-2"><input aria-label={t("name")} className={field+" min-w-0 flex-1"} maxLength={100} value={g.name} onChange={e=>setDraft(d=>({...d,groups:d.groups.map(v=>v.id===g.id?{...v,name:e.target.value}:v)}))}/><button className={button} onClick={()=>{setDraft(d=>({...d,groups:d.groups.filter(v=>v.id!==g.id),memberships:d.memberships.filter(m=>m.groupId!==g.id)}));setFilter(f=>f.filter(id=>id!==g.id));}}>{t("remove")}</button></div>)}
   <form className="flex gap-2" onSubmit={e=>{e.preventDefault();if(name.trim()){setDraft(d=>({...d,groups:[...d.groups,{id:crypto.randomUUID(),name:name.trim()}]}));setName("");}}}><input aria-label={t("newName")} className={field+" min-w-0 flex-1"} maxLength={100} value={name} onChange={e=>setName(e.target.value)}/><button className={button} disabled={!name.trim()||draft.groups.length>=100}>{t("add")}</button></form>
  </fieldset>
  <section className="space-y-3"><h2 className="text-xl font-semibold">{t("positions")}</h2>
   <input aria-label={t("search")} placeholder={t("search")} className={field+" w-full"} value={search} onChange={e=>{setSearch(e.target.value);setPage(0);}}/>
   <fieldset className="flex flex-wrap gap-3"><legend>{t("filter")}</legend>{[{id:"",name:t("ungrouped")},...draft.groups].map(g=><label key={g.id} className="flex gap-2"><input type="checkbox" checked={filter.includes(g.id)} onChange={e=>{setFilter(f=>e.target.checked?[...f,g.id]:f.filter(v=>v!==g.id));setPage(0);}}/>{g.name}</label>)}</fieldset>
   <p>{t("count",{count:shown.length})}</p><div className="overflow-auto"><table className="w-full text-left"><thead><tr><th>{t("account")}</th><th>{t("instrument")}</th><th>{t("group")}</th></tr></thead><tbody>{shown.slice(page*50,page*50+50).map(p=><tr key={`${p.ledgerId}:${p.instrumentKey}`}><td className="p-2">{p.ledgerName}</td><td className="p-2 break-all">{p.instrumentKey}</td><td className="p-2"><select aria-label={`${p.ledgerName}: ${p.instrumentKey}`} disabled={locked} className={field} value={member(p)} onChange={e=>change(p,e.target.value)}><option value="">{t("ungrouped")}</option>{draft.groups.map(g=><option key={g.id} value={g.id}>{g.name}</option>)}</select></td></tr>)}</tbody></table></div>
   <div className="flex gap-2"><button className={button} disabled={page===0} onClick={()=>setPage(p=>p-1)}>{t("previous")}</button><button className={button} disabled={(page+1)*50>=shown.length} onClick={()=>setPage(p=>p+1)}>{t("next")}</button></div>
  </section>
  {canWrite&&<div className="space-y-2"><label className="block">{t("reason")}<textarea className={field+" mt-1 w-full"} maxLength={2000} disabled={save.isPending} value={reason} onChange={e=>setReason(e.target.value)}/></label><button className={button} disabled={save.isPending||!reason.trim()||draft.groups.some(g=>!g.name.trim())} onClick={()=>save.mutate()}>{t("save")}</button></div>}
  {save.error&&<p role="alert" className="text-red-600">{(save.error as {code?:number}).code===10?t("conflict"):t("error")}</p>}
 </div>;
}
function Groups({tenantId}:{tenantId:string}){
 const params=useSearchParams();
 const t=useTranslations("investmentGroups"),{investmentNAV:api}=useClients();
 const query=useQuery<{classification?:InvestmentGroups;positions:InvestmentGroupPosition[]}>({queryKey:["investmentGroups",tenantId],queryFn:()=>api.getInvestmentGroups({}),refetchOnWindowFocus:false,refetchOnReconnect:false});
 return <main className="mx-auto max-w-5xl space-y-5 px-4 py-8"><div className="flex flex-wrap justify-between gap-3"><h1 className="text-2xl font-bold">{t("title")}</h1><Link className={button} href={`/investments/value?${params}`}>{t("value")}</Link></div>{query.isPending&&<p role="status">{t("loading")}</p>}{query.error&&<p role="alert">{t("error")}</p>}{query.data?.classification&&<Editor key={`${tenantId}:${query.dataUpdatedAt}`} tenantId={tenantId} classification={query.data.classification} positions={query.data.positions} reload={()=>void query.refetch()}/>}</main>;
}
export default function GroupsPage(){const tenantId=useAssetTenant();return <Protected>{tenantId&&<Groups key={tenantId} tenantId={tenantId}/>}</Protected>;}
