"use client";
import {useState} from "react";
import {useMutation,useQueryClient} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import type {BrokerJournalMatch} from "@/proto/budget/v1/investment_import_pb";

export default function InvestmentBrokerTradeLink({tenantId,snapshotId,brokerOperationId,matches}:{tenantId:string;snapshotId:string;brokerOperationId:string;matches:BrokerJournalMatch[]}) {
 const t=useTranslations("brokerSource");
 const {investmentImport}=useClients();
 const qc=useQueryClient();
 const [selected,setSelected]=useState("");
 const [reason,setReason]=useState("");
 const eligible=matches.filter(m=>m.sameInstrument&&m.sameLedger&&m.exactTime&&m.operationVersion>0n);
 const match=eligible.find(m=>m.operationId===selected);
 const save=useMutation({mutationFn:async()=>{
  if(!match||!reason.trim())throw new Error(t("linkRequired"));
  return await investmentImport.linkBrokerTrade({snapshotId,brokerOperationId,ledgerId:match.ledgerId,operationId:match.operationId,expectedOperationVersion:match.operationVersion,reason}) as {linkId:string};
 },onSuccess:()=>{void qc.invalidateQueries({queryKey:["broker-source-rows",tenantId]});}});
 if(!eligible.length)return null;
 return <form className="space-y-2 rounded border p-3" onSubmit={e=>{e.preventDefault();save.mutate();}}>
  <h4 className="font-medium">{t("linkTitle")}</h4><p className="text-sm">{t("linkHelp")}</p>
  <label className="block text-sm">{t("linkOperation")}<select aria-label={t("linkOperation")} className="block w-full rounded border bg-white p-2 dark:bg-gray-950" value={selected} disabled={save.isPending||save.isSuccess} onChange={e=>{setSelected(e.target.value);save.reset();}}><option value="">{t("chooseOperation")}</option>{eligible.map(m=><option key={m.operationId} value={m.operationId}>{m.operationId} · {m.instrumentKey}</option>)}</select></label>
  <label className="block text-sm">{t("linkReason")}<textarea aria-label={t("linkReason")} className="block w-full rounded border bg-white p-2 dark:bg-gray-950" value={reason} maxLength={2000} disabled={save.isPending||save.isSuccess} onChange={e=>{setReason(e.target.value);save.reset();}}/></label>
  <button className="rounded bg-blue-600 px-4 py-2 text-white disabled:opacity-50" disabled={!match||!reason.trim()||save.isPending||save.isSuccess}>{t("confirmLink")}</button>
  {save.isPending&&<p role="status">{t("busy")}</p>}
  {save.error&&<p role="alert" className="break-words text-red-600">{String(save.error)}</p>}
  {save.isSuccess&&<p role="status" className="break-all text-green-700 dark:text-green-400">{t("linkSaved")} {save.data.linkId}</p>}
 </form>;
}
