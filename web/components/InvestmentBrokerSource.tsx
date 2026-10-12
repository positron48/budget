"use client";

import type {BrokerSnapshotInfo,BrokerSnapshotRow} from "@/proto/budget/v1/investment_import_pb";
import InvestmentBrokerTradePreview from "./InvestmentBrokerTradePreview";
import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";

const field="mt-1 block w-full rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-950";
const button="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50";
export default function InvestmentBrokerSource({canWrite,tenantId}:{canWrite:boolean;tenantId:string}) {
 const t=useTranslations("brokerSource");
 const {investmentImport:api}=useClients();
 const qc=useQueryClient();
 const [offset,setOffset]=useState(0);
 const snapshots=useQuery<{snapshots:BrokerSnapshotInfo[]}>({queryKey:["broker-snapshots",tenantId,offset],queryFn:()=>api.listBrokerSnapshots({limit:20,offset})});
 const [file,setFile]=useState<File|null>(null);
 const [id,setID]=useState("");
 const [rowOffset,setRowOffset]=useState(0);
 const [previewOperation,setPreviewOperation]=useState("");
 const validID=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(id);
 const rows=useQuery<{rows:BrokerSnapshotRow[]}>({queryKey:["broker-source-rows",tenantId,id,rowOffset],enabled:validID,queryFn:()=>api.listBrokerSnapshotRows({id,limit:20,offset:rowOffset})});
 const upload=useMutation({mutationFn:async()=>{
  if(!file || file.size===0 || file.size>20*1024*1024)throw new Error(t("limit"));
  return await api.uploadBrokerSnapshot({originalJson:new Uint8Array(await file.arrayBuffer())}) as {id:string};
 },onSuccess:result=>{setPreviewOperation("");setID(result.id);setRowOffset(0);setOffset(0);qc.invalidateQueries({queryKey:["broker-snapshots",tenantId]});}});
 const download=useMutation({mutationFn:async()=>{
  const response=await api.getBrokerSnapshot({id}) as {originalJson:Uint8Array};
  const url=URL.createObjectURL(new Blob([new Uint8Array(response.originalJson)],{type:"application/json"}));
  const a=document.createElement("a");a.href=url;a.download=`broker-${id}.json`;document.body.appendChild(a);a.click();a.remove();
  setTimeout(()=>URL.revokeObjectURL(url),1000);
 }});
 const busy=upload.isPending||download.isPending;
 return <section className="space-y-3 rounded-xl border border-gray-200 p-5 dark:border-gray-800">
  <h2 className="text-xl font-semibold">{t("title")}</h2><p className="text-sm text-gray-500">{t("help")}</p>
  {canWrite&&<form className="flex flex-wrap items-end gap-3" onSubmit={e=>{e.preventDefault();upload.mutate();}}>
   <label className="min-w-64 flex-1 text-sm">{t("file")}<input className={field} type="file" accept=".json,application/json" disabled={busy} onChange={e=>{setFile(e.target.files?.[0]||null);upload.reset();}}/></label>
   <button className={button} disabled={busy||!file}>{t("upload")}</button>
  </form>}
  {upload.isSuccess&&<p role="status" className="text-sm text-green-700 dark:text-green-400">{t("saved")} <span className="break-all">{upload.data.id}</span></p>}
  <div className="flex flex-wrap items-end gap-3"><label className="min-w-64 flex-1 text-sm">{t("id")}<input className={field} value={id} maxLength={36} disabled={busy} onChange={e=>{setPreviewOperation("");setID(e.target.value);setRowOffset(0);download.reset();}}/></label><button type="button" className={button} disabled={busy||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(id)} onClick={()=>download.mutate()}>{t("download")}</button></div>
  {snapshots.data&&<ul className="space-y-2">{snapshots.data.snapshots.map(item=><li key={item.id}><button type="button" className="w-full rounded border p-3 text-left text-sm disabled:opacity-50" disabled={busy} onClick={()=>{setPreviewOperation("");setID(item.id);setRowOffset(0);download.reset();}}><span className="block break-all">{item.accountId} · {item.rowCount} {t("rows")}</span><span className="block break-all text-xs text-gray-500">{item.id}</span></button></li>)}</ul>}
  <div className="flex gap-3"><button type="button" className={button} disabled={busy||snapshots.isFetching||offset===0} onClick={()=>setOffset(x=>Math.max(0,x-20))}>{t("previous")}</button><button type="button" className={button} disabled={busy||snapshots.isFetching||snapshots.data?.snapshots.length!==20} onClick={()=>setOffset(x=>x+20)}>{t("next")}</button></div>
  {validID&&<div className="space-y-3"><h3 className="font-medium">{t("sourceRows")}</h3>{rows.data?.rows.map(row=><details className="rounded border p-3 text-sm" key={row.observationId}><summary className="break-all">#{row.ordinal} · {row.operationId}{row.hasOtherVersions&&<span className="ml-2 text-amber-700 dark:text-amber-400">{t("changed")}</span>}</summary><pre className="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">{new TextDecoder().decode(row.originalJson)}</pre>{row.linkId&&<div className="my-2 space-y-1 break-all"><p>{t("linkedTo")} {row.linkedOperationId} · {row.linkedLedgerId}</p><p className="text-xs">{row.linkId}</p>{(row.linkedVersion!==row.currentVersion||row.linkedStatus!=="posted"||row.claimedDifferentContent)&&<p className="text-amber-700 dark:text-amber-400">{t("linkedChanged")}</p>}</div>}<button type="button" className={button} onClick={()=>setPreviewOperation(row.operationId)}>{t("tradePreview")}</button></details>)}{rows.error&&<p role="alert">{String(rows.error)}</p>}<div className="flex gap-3"><button type="button" className={button} disabled={rows.isFetching||rowOffset===0} onClick={()=>setRowOffset(x=>Math.max(0,x-20))}>{t("previousRows")}</button><button type="button" className={button} disabled={rows.isFetching||rows.data?.rows.length!==20} onClick={()=>setRowOffset(x=>x+20)}>{t("nextRows")}</button></div></div>}
  {validID&&previewOperation&&<InvestmentBrokerTradePreview key={`${id}:${previewOperation}`} tenantId={tenantId} snapshotId={id} operationId={previewOperation} canWrite={canWrite}/>}
  {snapshots.error&&<p role="alert">{String(snapshots.error)}</p>}
  {busy&&<p role="status">{t("busy")}</p>}
  {(upload.error||download.error)&&<p role="alert" className="break-words text-sm text-red-600">{String(upload.error||download.error)}</p>}
 </section>;
}
