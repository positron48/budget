"use client";
import InvestmentBrokerTradePost from "./InvestmentBrokerTradePost";
import InvestmentBrokerTradeLink from "./InvestmentBrokerTradeLink";
import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import type {InvestmentLedger} from "@/proto/budget/v1/investment_pb";
import type {PreviewBrokerTradeResponse} from "@/proto/budget/v1/investment_import_pb";

export default function InvestmentBrokerTradePreview({tenantId,snapshotId,operationId,canWrite}:{tenantId:string;snapshotId:string;operationId:string;canWrite:boolean}) {
 const t=useTranslations("brokerSource");
 const {investment,investmentImport}=useClients();
 const [offset,setOffset]=useState(0);
 const [ledger,setLedger]=useState("");
 const [requested,setRequested]=useState("");
 const ledgers=useQuery<{ledgers:InvestmentLedger[]}>({queryKey:["investment",tenantId,"ledgers",offset],queryFn:()=>investment.listInvestmentLedgers({limit:50,offset})});
 const preview=useQuery<PreviewBrokerTradeResponse>({queryKey:["broker-trade-preview",tenantId,snapshotId,operationId,requested],enabled:!!requested,retry:false,refetchOnWindowFocus:false,queryFn:()=>investmentImport.previewBrokerTrade({snapshotId,operationId,ledgerId:requested})});
 const operation=preview.data?.operation;
 return <div className="space-y-3 rounded border p-3" aria-label={t("tradePreview")}>
  <h3 className="font-medium">{t("tradePreview")} · {operationId}</h3>
  <label className="block text-sm">{t("targetLedger")}<select aria-label={t("targetLedger")} className="mt-1 block w-full rounded border bg-white p-2 dark:bg-gray-950" value={ledger} onChange={e=>{setLedger(e.target.value);setRequested("");}}><option value="">{t("chooseLedger")}</option>{ledgers.data?.ledgers.map(item=><option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
  <div className="flex gap-3"><button type="button" disabled={offset===0||ledgers.isFetching} onClick={()=>{setOffset(x=>Math.max(0,x-50));setLedger("");setRequested("");}}>{t("previous")}</button><button type="button" disabled={ledgers.data?.ledgers.length!==50||ledgers.isFetching} onClick={()=>{setOffset(x=>x+50);setLedger("");setRequested("");}}>{t("next")}</button></div>
  <button type="button" className="rounded bg-blue-600 px-4 py-2 text-white disabled:opacity-50" disabled={!ledger||preview.isFetching} onClick={()=>{if(requested===ledger)void preview.refetch();else setRequested(ledger);}}>{t("prepareTrade")}</button>
  {requested&&preview.isFetching&&<p role="status">{t("busy")}</p>}
  {requested&&preview.error&&<p role="alert" className="break-words text-red-600">{String(preview.error)}</p>}
  {requested&&!preview.isFetching&&!preview.error&&operation&&<div className="space-y-2">
   <p>{operation.kind==="buy"?t("buy"):t("sell")} · {operation.instrumentKey}</p>
   <dl className="grid grid-cols-2 gap-2 text-sm"><dt>{t("quantity")}</dt><dd>{operation.quantity}</dd><dt>{t("principal")}</dt><dd>{operation.amount} {operation.currency}</dd><dt>{t("fee")}</dt><dd>{operation.fee} {operation.currency}</dd><dt>{t("accrued")}</dt><dd>{operation.accrued} {operation.currency}</dd></dl>
   <p className="text-sm">{t("previewOnly")}</p>
   <p className="text-sm">{t(preview.data?.replayValid?"replayValid":"replayBlocked")}{preview.data?.replayIssue&&<span className="block break-words text-red-600">{preview.data.replayIssue}</span>}</p>
   <p className="text-sm text-amber-700 dark:text-amber-400">{t(preview.data?.journalMatches.length?"possibleDuplicates":"noExactMatches")}</p>
   <ul className="break-all text-sm">{preview.data?.journalMatches.map(match=><li key={`${match.ledgerId}:${match.operationId}`}>{match.operationId} · {match.ledgerId} · {match.instrumentKey}</li>)}</ul>
   {canWrite&&preview.data&&<InvestmentBrokerTradeLink key={`${requested}:${preview.dataUpdatedAt}`} tenantId={tenantId} snapshotId={snapshotId} brokerOperationId={operationId} matches={preview.data.journalMatches}/>}
   {canWrite&&preview.data?.postingAllowed&&preview.data.replayLedgerVersion>0n&&<InvestmentBrokerTradePost key={`post:${requested}:${preview.dataUpdatedAt}`} tenantId={tenantId} snapshotId={snapshotId} brokerOperationId={operationId} ledgerId={requested} version={preview.data.replayLedgerVersion}/>}
   <ul className="break-all text-xs">{preview.data?.sourceIdentities.map(identity=><li key={identity}>{identity}</li>)}</ul>
  </div>}
  {ledgers.error&&<p role="alert">{String(ledgers.error)}</p>}
 </div>;
}
