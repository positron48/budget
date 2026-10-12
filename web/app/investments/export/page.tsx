"use client";
import {useEffect,useRef,useState} from "react";
import Link from "next/link";
import {useSearchParams,useRouter} from "next/navigation";
import {useQuery,useMutation} from "@tanstack/react-query";
import {useTranslations,useLocale} from "next-intl";
import {Protected} from "@/components";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {AUTH_CHANGED_EVENT} from "@/lib/auth/store";
import {readInvestmentArchive} from "@/lib/investment-archive-download";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";
import type {GetInvestmentExportResponse} from "@/proto/budget/v1/investment_pb";
const button="rounded border border-gray-300 px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";
function Export({tenantId}:{tenantId:string}){
 const t=useTranslations("investmentExport"),locale=useLocale(),{investment:api,tenant}=useClients(),params=useSearchParams(),router=useRouter();
 const [requestKey,setRequestKey]=useState("");const [busy,setBusy]=useState(false),[done,setDone]=useState(0n),[failed,setFailed]=useState(false);
 const download=useRef<AbortController|null>(null),live=useRef(true);
 useEffect(()=>{live.current=true;const abort=()=>download.current?.abort();window.addEventListener(AUTH_CHANGED_EVENT,abort);return()=>{live.current=false;abort();window.removeEventListener(AUTH_CHANGED_EVENT,abort);};},[]);
 const roles=useQuery<{memberships:TenantMembership[]}>({queryKey:["investment",tenantId,"export-roles"],queryFn:()=>tenant.listMyTenants({})});
 const role=roles.data?.memberships.find(m=>m.tenant?.id===tenantId)?.role,canExport=role===1||role===2,jobId=params.get("job")||"";
 const status=useQuery<GetInvestmentExportResponse>({queryKey:["investment",tenantId,"export",jobId],enabled:canExport&&!!jobId,queryFn:({signal})=>api.getInvestmentExport({jobId},{signal}),retry:false,refetchOnWindowFocus:false,refetchInterval:query=>["pending","running"].includes(query.state.data?.job?.state||"")?2000:false});
 const start=useMutation({mutationFn:()=>{const key=requestKey||crypto.randomUUID();setRequestKey(key);return api.startInvestmentExport({requestKey:key});},onSuccess:(data:GetInvestmentExportResponse)=>{if(live.current&&data.job){router.replace(`/investments/export?job=${encodeURIComponent(data.job.id)}`);setRequestKey("");setFailed(false);}}});
 const job=status.data?.job;
 const save=async()=>{
  if(!job||busy)return;
  download.current?.abort();const controller=new AbortController();download.current=controller;setBusy(true);setFailed(false);setDone(0n);
  try{
   const bytes=await readInvestmentArchive(job,offset=>api.downloadInvestmentExport({jobId:job.id,offset},{signal:controller.signal}),controller.signal,setDone);
   controller.signal.throwIfAborted();
   const url=URL.createObjectURL(new Blob([bytes],{type:"application/json"}));const a=document.createElement("a");a.href=url;a.download=`budget-investments-${job.id}.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
  }catch{if(!controller.signal.aborted)setFailed(true);}finally{if(download.current===controller)setBusy(false);}
 };
 const state=job?.state;
 return <main className="mx-auto max-w-3xl space-y-5 px-4 py-8"><h1 className="text-2xl font-bold">{t("title")}</h1><Link className="text-blue-600" href="/investments">{t("back")}</Link><p>{t("scope")}</p><p className="rounded border border-amber-400 p-3 text-sm">{t("privacy")}</p>
 {roles.isPending?<p role="status">{t("loading")}</p>:roles.error?<p role="alert">{t("error")}</p>:!canExport?<p>{t("ownerOnly")}</p>:<>
 <button className={button} disabled={start.isPending||busy||state==="pending"||state==="running"} onClick={()=>start.mutate()}>{t("create")}</button>
 {(start.error||status.error||failed)&&<p role="alert">{t("error")}</p>}
 {job&&<section className="space-y-3 rounded border border-gray-300 p-4 dark:border-gray-700" aria-label={t("job")}><p role="status">{t.has(`states.${job.state}`)?t(`states.${job.state}`):t("error")}</p>
 {job.createdAt&&<p>{t("created")}: {new Date(Number(job.createdAt.seconds)*1000).toLocaleString(locale)}</p>}
 {job.expiresAt&&<p>{t("expires")}: {new Date(Number(job.expiresAt.seconds)*1000).toLocaleString(locale)}</p>}
 {state==="ready"&&<><p>{t("size")}: {job.byteSize.toString()}</p><p className="break-all font-mono text-xs">SHA-256: {job.sha256}</p><button className={button} disabled={busy} onClick={()=>void save()}>{t("download")}</button>{busy&&<p role="status">{t("progress",{done:done.toString(),total:job.byteSize.toString()})}</p>}</>}
 {state==="error"&&<p role="alert">{t.has(`errors.${job.errorCode}`)?t(`errors.${job.errorCode}`):t("error")}</p>}
 </section>}
 {jobId&&<button className={button} disabled={status.isFetching||busy} onClick={()=>void status.refetch()}>{t("refresh")}</button>}
 </>}
 </main>;
}
export default function ExportPage(){const tenant=useAssetTenant();return <Protected>{tenant&&<Export key={tenant} tenantId={tenant}/>}</Protected>;}
