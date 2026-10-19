"use client";
import {Code,ConnectError} from "@connectrpc/connect";
import {useEffect,useRef,useState} from "react";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {authStore,AUTH_CHANGED_EVENT} from "@/lib/auth/store";
import {usePortfolioFilters} from "@/lib/portfolio-filters";
import {accountSelection,togglePortfolioAccount,visiblePortfolioAccounts} from "@/lib/portfolio-accounts";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import {trackerError} from "@/lib/portfolio-tracker";
import type {InvestmentLedger} from "@/proto/budget/v1/investment_pb";
import type {InvestmentPortfolio,ListInvestmentPortfoliosResponse} from "@/proto/budget/v1/investment_nav_pb";
const button="rounded-lg border border-slate-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-slate-700";
export default function PortfolioAccountSelection({tenantId,accounts,ru,canSave}:{tenantId:string;accounts:InvestmentLedger[];ru:boolean;canSave:boolean}){
 const tr=(r:string,e:string)=>ru?r:e,{investmentNAV:api}=useClients(),qc=useQueryClient(),{params,update}=usePortfolioFilters(),scope=accountSelection(params);
 const [name,setName]=useState(""),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const pending=useRef<{signature:string;request:Record<string,unknown>}|undefined>(undefined);
 const [actor,setActor]=useState<string>();
 useEffect(()=>{const update=()=>{const next=authStore.getActor();setActor(previous=>{if(previous!==next)pending.current=undefined;return next;});};update();window.addEventListener(AUTH_CHANGED_EVENT,update);return()=>window.removeEventListener(AUTH_CHANGED_EVENT,update);},[]);
 const key=["investment",tenantId,"tracker-selections",actor];
 const list=useQuery<InvestmentPortfolio[]>({queryKey:key,enabled:!!actor,queryFn:async({signal})=>{const task=tenantRequestScope(tenantId,signal,actor);try{const all:InvestmentPortfolio[]=[];let afterId="";do{const p=await task.run<ListInvestmentPortfoliosResponse>(options=>api.listInvestmentPortfolios({afterId},options));all.push(...p.portfolios.filter(p=>p.trackerSelection));afterId=p.nextAfterId;}while(afterId);return all;}finally{task.dispose();}}});
 const selected=scope.explicit?scope.ids:accounts.filter(l=>!l.archived).map(l=>l.id).sort();
 const save=async(existing?:InvestmentPortfolio,archived=false)=>{if(!actor)return;setBusy(true);setError("");const task=tenantRequestScope(tenantId,undefined,actor);try{
  const signature=JSON.stringify([existing?.id,existing?.version.toString(),existing?.name||name,existing?.ledgerIds||selected,archived]);
  if(pending.current&&pending.current.signature!==signature)throw new Error("pending_write_changed");
  if(!pending.current)pending.current={signature,request:{id:existing?.id||"",name:existing?.name||name,ledgerIds:existing?.ledgerIds||selected,trackerSelection:true,archived,expectedVersion:existing?.version||0n,requestKey:crypto.randomUUID()}};
  await task.run(options=>api.saveInvestmentPortfolio(pending.current!.request,options));pending.current=undefined;setName("");await qc.invalidateQueries({queryKey:key});
 }catch(e){if([Code.InvalidArgument,Code.FailedPrecondition,Code.PermissionDenied,Code.Aborted,Code.AlreadyExists].includes(ConnectError.from(e).code))pending.current=undefined;setError(trackerError(e,ru));}finally{task.dispose();setBusy(false);}};
 return <details className="space-y-3 text-sm"><summary className="cursor-pointer">{tr("Выбрать несколько счетов","Select multiple accounts")}</summary><fieldset disabled={busy} className="mt-3 space-y-2" aria-label={tr("Счета в портфеле","Portfolio accounts")}>{visiblePortfolioAccounts(params,accounts).map(l=><label key={l.id} className="flex items-center gap-2"><input type="checkbox" checked={!scope.explicit||scope.ids.includes(l.id)} onChange={e=>update(togglePortfolioAccount(params,accounts,l.id,e.target.checked))}/>{l.name}{l.archived?tr(" · архив"," · archived"):""}</label>)}</fieldset>
 {canSave&&actor&&<form className="mt-3 space-y-2" onSubmit={e=>{e.preventDefault();void save();}}><label className="block text-xs">{tr("Название набора счетов","Account set name")}<input className="mt-1 w-full rounded border border-slate-300 bg-transparent px-2 py-1 dark:border-slate-700" required maxLength={200} disabled={busy} value={name} onChange={e=>setName(e.target.value)}/></label><button className={button} disabled={busy||!selected.length||!name.trim()}>{tr("Сохранить набор","Save account set")}</button></form>}
 <div className="mt-3 space-y-2">{actor&&list.data?.map(p=><div className="flex items-center justify-between gap-2" key={p.id}><button className={button} disabled={busy} onClick={()=>update({account:p.ledgerIds,scope:"selected",selection:p.id})}>{p.name}{p.archived?tr(" · архив"," · archived"):""}</button>{canSave&&<button className="text-xs text-slate-500" disabled={busy} aria-label={(p.archived?tr("Восстановить набор ","Restore account set "):tr("Архивировать набор ","Archive account set "))+p.name} onClick={()=>void save(p,!p.archived)}>{p.archived?tr("Вернуть","Restore"):tr("В архив","Archive")}</button>}</div>)}</div>
 {(error||list.error)&&<p role="alert" className="text-xs text-rose-700">{error||tr("Не удалось загрузить наборы счетов","Could not load account sets")}</p>}
 <p className="mt-3 text-xs text-slate-500">{tr("Набор содержит ссылки на счета. Сделки и выплаты остаются на своих счетах.","Sets reference accounts. Trades and payouts remain on their original accounts.")}</p>
 </details>;
}
