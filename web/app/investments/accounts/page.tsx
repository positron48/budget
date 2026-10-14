"use client";
import {useState} from "react";
import Link from "next/link";
import {useLocale} from "next-intl";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import Modal from "@/components/Modal";
import PortfolioExport from "@/components/PortfolioExport";
import PortfolioAssetLink from "@/components/PortfolioAssetLink";
import {trackerError} from "@/lib/portfolio-tracker";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";
import type {InvestmentLedger} from "@/proto/budget/v1/investment_pb";
const input="mt-1 w-full rounded-lg border border-slate-300 bg-transparent px-3 py-2 dark:border-slate-700";
const button="rounded-lg border border-slate-300 px-4 py-2 text-sm dark:border-slate-700 disabled:opacity-40";
function Accounts({tenantId}:{tenantId:string}){
 const ru=useLocale()==="ru",tr=(r:string,e:string)=>ru?r:e,{investment:api,tenant}=useClients(),qc=useQueryClient(),readOnly=useInvestmentReadOnly();
 const [editing,setEditing]=useState<InvestmentLedger|null>(null),[draft,setDraft]=useState({name:"",broker:"",type:"regular"}),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const roles=useQuery<{memberships:TenantMembership[]}>({queryKey:["investment",tenantId,"tracker","roles"],queryFn:()=>tenant.listMyTenants({})});
 const canExport=roles.data?.memberships.some(m=>m.tenant?.id===tenantId&&[1,2].includes(m.role));
 const canWrite=!readOnly&&canExport;
 const list=useQuery({queryKey:["investment",tenantId,"tracker","settings"],queryFn:async()=>{
  const ledgers:InvestmentLedger[]=[];
  for(let offset=0;;offset+=200){const page=await api.listInvestmentLedgers({limit:200,offset});ledgers.push(...page.ledgers);if(page.ledgers.length<200)return ledgers;}
 }});
 const save=async()=>{if(!editing)return;setBusy(true);setError("");try{
  await api.updateInvestmentLedger({id:editing.id,action:"edit",name:draft.name,broker:draft.broker,accountType:draft.type,defaultCurrency:editing.defaultCurrency,comment:editing.comment,expectedMetadataVersion:editing.metadataVersion,expectedLedgerVersion:editing.version,requestKey:crypto.randomUUID(),reason:"User edited portfolio account"});
  setEditing(null);await qc.invalidateQueries({queryKey:["investment",tenantId]});
 }catch(e){setError(trackerError(e,ru));}finally{setBusy(false);}};
 return <div className="mx-auto max-w-3xl space-y-6 px-4 py-8"><header className="flex items-center justify-between gap-4"><h1 className="text-2xl font-semibold">{tr("Мои счета","My accounts")}</h1><Link className={button} href="/investments">{tr("К портфелю","Back to portfolio")}</Link></header>
 {canExport&&<PortfolioExport tenantId={tenantId}/>}
 {(list.error||roles.error)&&<p role="alert">{trackerError(list.error||roles.error,ru)}</p>}
 {list.data?.map(l=><article key={l.id} className="flex flex-wrap items-center justify-between gap-4 rounded-2xl border border-slate-200 p-5 dark:border-slate-800"><div><h2 className="font-semibold">{l.name}</h2><p className="mt-1 text-sm text-slate-500">{l.broker}{l.broker?" · ":""}{l.accountType==="iis"?tr("ИИС","IIS"):tr("Брокерский счёт","Brokerage account")}</p></div>{canWrite&&<div className="flex flex-wrap gap-2">{l.portfolioTracking&&<PortfolioAssetLink ledger={l} tenantId={tenantId}/>}<button className={button} onClick={()=>{setEditing(l);setDraft({name:l.name,broker:l.broker,type:l.accountType||"regular"});setError("");}}>{tr("Изменить","Edit")}</button></div>}</article>)}
 {editing&&<Modal open title={tr("Изменить счёт","Edit account")} onClose={()=>{if(!busy)setEditing(null);}}><form className="space-y-4" onSubmit={e=>{e.preventDefault();void save();}}><fieldset className="space-y-4" disabled={busy}>
 <label className="block text-sm">{tr("Название","Name")}<input required className={input} value={draft.name} onChange={e=>setDraft({...draft,name:e.target.value})}/></label>
 <label className="block text-sm">{tr("Брокер","Broker")}<input className={input} value={draft.broker} onChange={e=>setDraft({...draft,broker:e.target.value})}/></label>
 <label className="block text-sm">{tr("Тип счёта","Account type")}<select className={input} value={draft.type} onChange={e=>setDraft({...draft,type:e.target.value})}><option value="regular">{tr("Брокерский","Brokerage")}</option><option value="iis">{tr("ИИС","IIS")}</option><option value="other">{tr("Другой","Other")}</option></select></label>
 {error&&<p role="alert">{error}</p>}<button className={button} type="submit">{tr("Сохранить","Save")}</button></fieldset></form></Modal>}
 </div>;
}
export default function Page(){const tenantId=useAssetTenant();return tenantId?<Accounts key={tenantId} tenantId={tenantId}/>:null;}
