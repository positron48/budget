"use client";
import {useEffect,useRef,useState,type ReactNode} from "react";
import {Code,ConnectError} from "@connectrpc/connect";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import type {InvestmentImportDraft as Draft,InvestmentOperation,InvestmentBatchCancellationResult as Result} from "@/proto/budget/v1/investment_pb";

export default function InvestmentBatchCancellation({draft,onChange,onBlocked,renderOperation}:{draft:Draft;renderOperation:(operation:InvestmentOperation)=>ReactNode;onChange:(draft:Draft)=>void;onBlocked:(blocked:boolean)=>void}){
 const t=useTranslations("investmentJournal");
 const {investment:api}=useClients();
 const [reason,setReason]=useState("");
 const [page,setPage]=useState(0);
 const [result,setResult]=useState<Result|undefined>(draft.cancellation);
 const [busy,setBusy]=useState(false);
 const [uncertain,setUncertain]=useState(false);
 const [confirmed,setConfirmed]=useState(false);
 const [error,setError]=useState("");
 const key=useRef(crypto.randomUUID());
 const cancelled=!!result?.cancelled;
 useEffect(()=>{onBlocked(busy||uncertain);return()=>onBlocked(false);},[busy,uncertain,onBlocked]);
 useEffect(()=>{if(draft.cancellation?.cancelled){setResult(draft.cancellation);setUncertain(false);}},[draft.cancellation]);
 const input=()=>({draftId:draft.id,expectedDraftVersion:draft.version,requestKey:key.current,reason});
 const preview=async()=>{
  setBusy(true);setError("");setResult(undefined);setConfirmed(false);setPage(0);
  try{const response=await api.previewInvestmentBatchCancellation({cancellation:input()});if(!response.result?.hash)throw new Error(t("batchNoPreview"));setResult(response.result);}
  catch(e){setError(ConnectError.from(e).message);}finally{setBusy(false);}
 };
 const commit=async()=>{
  if(!result)return;
  setBusy(true);setUncertain(true);setError("");
  try{
   const response=await api.commitInvestmentBatchCancellation({cancellation:input(),expectedHash:result.hash});
   if(!response.result?.cancelled)throw new Error(t("batchCancelUncertain"));
   setResult(response.result);setUncertain(false);onChange({...draft,cancellation:response.result});
  }catch(e){
   const failure=ConnectError.from(e);setError(failure.message);
   if([Code.Aborted,Code.InvalidArgument,Code.FailedPrecondition,Code.PermissionDenied,Code.Unauthenticated,Code.NotFound].includes(failure.code)){setUncertain(false);setResult(undefined);setConfirmed(false);}
  }finally{setBusy(false);}
 };
 const check=async()=>{
  setBusy(true);setError("");
  try{const response=await api.getInvestmentImportDraft({id:draft.id});if(response.draft?.cancellation?.cancelled){setResult(response.draft.cancellation);setUncertain(false);onChange(response.draft);}}
  catch(e){setError(ConnectError.from(e).message);}finally{setBusy(false);}
 };
 return <section data-testid="investment-batch-cancellation" className="space-y-3 border-t border-gray-200 pt-4 dark:border-gray-800">
  <h3 className="font-semibold">{t(cancelled?"batchCancelled":"batchCancelTitle")}</h3>
  <p className="text-sm text-gray-600 dark:text-gray-400">{t(cancelled?"batchCancelledHelp":"batchCancelHelp")}</p>
  {cancelled?<p className="break-words text-sm">{result?.reason}</p>:<>
   <label className="block text-sm">{t("batchCancelReason")}<textarea className="mt-1 block w-full rounded-lg border border-gray-300 bg-white p-2 dark:border-gray-700 dark:bg-gray-950" value={reason} maxLength={2000} disabled={busy||uncertain} onChange={e=>{setReason(e.target.value);setResult(undefined);setConfirmed(false);}} /></label>
   <button className="rounded-lg border border-red-600 px-4 py-2 text-sm text-red-700 disabled:opacity-50 dark:text-red-400" disabled={busy||uncertain||!reason.trim()} onClick={preview}>{t("batchCancelPreview")}</button>
  </>}
  {error&&<p role="alert" className="break-words text-sm text-red-600 dark:text-red-400">{error}</p>}
  {uncertain&&<p role="alert" className="text-sm text-amber-700 dark:text-amber-400">{t("batchCancelUncertain")}</p>}
  {result&&<div className="space-y-3">
   <p className="text-sm">{t("batchCancelCount",{count:result.operations.length,skipped:result.alreadyVoided.length})}</p>
   <ol className="space-y-2">{result.operations.slice(page*20,page*20+20).map((operation,index)=><li key={operation.id||index} className="rounded-lg border border-gray-200 p-3 text-sm dark:border-gray-800"><p className="break-words font-medium">{t("sourceRow")} {operation.source?.record} · {t(`kinds.${operation.kind}`)}</p>{renderOperation(operation)}</li>)}</ol>
   {result.operations.length>20&&<div className="flex gap-3"><button disabled={!page} onClick={()=>setPage(p=>p-1)}>{t("previous")}</button><button disabled={(page+1)*20>=result.operations.length} onClick={()=>setPage(p=>p+1)}>{t("next")}</button></div>}
   <div className="grid gap-3 md:grid-cols-2">{result.snapshots.map(snapshot=><article key={snapshot.ledger?.id} className="min-w-0 rounded-lg border border-gray-200 p-3 dark:border-gray-800">
    <h4 className="font-medium">{snapshot.ledger?.name}</h4>
    {snapshot.cash.map(cash=><p key={cash.currency} className="break-all text-sm tabular-nums">{cash.economic} {cash.currency}<span className="block text-xs text-gray-500">{t("settled")}: {cash.settled} · {t("receivable")}: {cash.receivable} · {t("payable")}: {cash.payable}</span></p>)}
    <details><summary className="text-sm">{t("positions")} ({snapshot.positions.length})</summary>{snapshot.positions.map(position=><p className="break-all text-sm" key={position.instrumentKey}>{position.instrumentKey} · {position.quantity} · {t("cleanCost")}: {position.cleanCost||"—"} {position.currency}</p>)}</details>
   </article>)}</div>
   {!cancelled&&<>
    <label className="flex items-start gap-2 text-sm"><input type="checkbox" checked={confirmed} disabled={busy||uncertain} onChange={e=>setConfirmed(e.target.checked)}/>{t("batchCancelConfirm")}</label>
    <div className="flex flex-wrap gap-3"><button className="rounded-lg bg-red-700 px-4 py-2 text-sm text-white disabled:opacity-50" disabled={busy||(!uncertain&&!confirmed)} onClick={commit}>{t(uncertain?"batchCancelRetry":"batchCancelCommit")}</button>{uncertain&&<button disabled={busy} onClick={check}>{t("batchCheckStatus")}</button>}</div>
   </>}
  </div>}
 </section>;
}
