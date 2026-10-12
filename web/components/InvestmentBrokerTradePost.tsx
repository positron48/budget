"use client";
import {useRef,useState} from "react";
import {useMutation,useQueryClient} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";

export default function InvestmentBrokerTradePost({tenantId,snapshotId,brokerOperationId,ledgerId,version}:{tenantId:string;snapshotId:string;brokerOperationId:string;ledgerId:string;version:bigint}){
 const t=useTranslations("brokerSource");const {investmentImport}=useClients();const qc=useQueryClient();
 const [reason,setReason]=useState("");const [confirmed,setConfirmed]=useState(false);
 const request=useRef<{snapshotId:string;brokerOperationId:string;ledgerId:string;expectedLedgerVersion:bigint;requestKey:string;reason:string;reconciliationConfirmed:boolean}|null>(null);
 const post=useMutation({mutationFn:async()=>{
  if(!request.current){if(!confirmed||!reason.trim())throw new Error(t("postReviewRequired"));request.current={snapshotId,brokerOperationId,ledgerId,expectedLedgerVersion:version,requestKey:crypto.randomUUID(),reason,reconciliationConfirmed:true};}
  return await investmentImport.postBrokerTrade(request.current) as {operationId:string;linkId:string};
 },onSuccess:()=>{void qc.invalidateQueries({queryKey:["broker-source-rows",tenantId]});void qc.invalidateQueries({queryKey:["investment",tenantId]});}});
 const locked=!!request.current||post.isPending||post.isSuccess;
 return <form className="space-y-3 rounded border p-3" onSubmit={e=>{e.preventDefault();post.mutate();}}>
  <h4 className="font-medium">{t("postTitle")}</h4>
  <label className="block text-sm">{t("postReason")}<textarea aria-label={t("postReason")} className="block w-full rounded border bg-white p-2 dark:bg-gray-950" maxLength={2000} disabled={locked} value={reason} onChange={e=>setReason(e.target.value)}/></label>
  <label className="flex items-start gap-2 text-sm"><input type="checkbox" checked={confirmed} disabled={locked} onChange={e=>setConfirmed(e.target.checked)}/>{t("postConfirm")}</label>
  <button className="rounded bg-blue-600 px-4 py-2 text-white disabled:opacity-50" disabled={!confirmed||!reason.trim()||post.isPending||post.isSuccess}>{t(post.isError?"postRetry":"postTrade")}</button>
  {post.isPending&&<p role="status">{t("busy")}</p>}
  {post.error&&<p role="alert" className="break-words text-red-600">{String(post.error)}</p>}
  {post.isError&&<p className="text-sm">{t("postRetryHelp")}</p>}
  {post.isSuccess&&<p role="status" className="break-all text-green-700 dark:text-green-400">{t("postSuccess")} {post.data.operationId} · {post.data.linkId}</p>}
 </form>;
}
