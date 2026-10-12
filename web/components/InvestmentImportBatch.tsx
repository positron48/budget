"use client";
import InvestmentBatchCancellation from "./InvestmentBatchCancellation";
import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Code, ConnectError } from "@connectrpc/connect";
import { useLocale, useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { reviewedBatch, storedBatchRows, type ReviewedBatchRow } from "@/lib/investment-batch";
import type { InvestmentImportBatch as Batch, InvestmentSnapshot, InvestmentImportDraft as Draft, InvestmentImportDraftSummary as Summary } from "@/proto/budget/v1/investment_pb";
import type { InvestmentImport } from "@/proto/budget/v1/investment_import_pb";

const button = "rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50";
export default function InvestmentImportBatch({tenantId,rows,onChange,onLockedChange,onCommitted}:{
  tenantId:string;rows:ReviewedBatchRow[];onChange:(rows:ReviewedBatchRow[])=>void;
  onLockedChange:(locked:boolean)=>void;onCommitted:(cancelled?:boolean)=>void;
}) {
  const t=useTranslations("investmentJournal");
  const locale=useLocale();
  const operationDetails=(row:ReviewedBatchRow)=>{
    const op=row.operation;
    const fields:[string,string][]=[];
    const date=(value:typeof op.occurredAt)=>value?new Date(Number(value.seconds)*1000+value.nanos/1000000).toLocaleString(locale,{timeZoneName:"short"}):"—";
    fields.push(["occurredAt",date(op.occurredAt)]);
    if(op.settlementAt)fields.push(["settlementAt",date(op.settlementAt)]);
    if(op.instrumentKey)fields.push(["instrumentKey",op.instrumentKey]);
    if(op.quantity&&op.quantity!=="0")fields.push(["quantity",op.quantity]);
    if(op.taxReviewRequired)fields.push(["taxReviewRequired",t("taxReviewHelp")]);
    if(op.kind!=="split" && op.kind!=="distribution")fields.push(["amount",`${op.kind==="opening_position"&&!op.openingCostKnown?"—":op.amount} ${op.currency}`]);
    if(["buy","sell","opening_position"].includes(op.kind))fields.push(["accrued",op.kind==="opening_position"&&!op.openingAccruedKnown?"—":op.accrued||"0"]);
    if(["buy","sell","fx"].includes(op.kind))fields.push(["fee",op.fee||"0"]);
    if(["dividend","coupon"].includes(op.kind))fields.push(["tax",op.tax||"0"]);
    if(op.kind==="fx")fields.push(["toAmount",`${op.toAmount} ${op.toCurrency}`]);
    if(op.distribution){
      const terms=op.distribution;
      fields.push(["currency",op.currency]);
      for(const key of ["eventKey","targetInstrumentKey","newUnits","oldUnits","costNumerator","costDenominator"] as const)fields.push([`distributionFields.${key}`,terms[key]]);
      fields.push(["distributionFields.consumeSource",t(terms.consumeSource?"batchYes":"batchNo")]);
    }
    if(op.kind==="split")fields.push(["splitNew",op.splitNew],["splitOld",op.splitOld]);
    if(["amortization","redemption"].includes(op.kind))fields.push(["nominalBefore",op.nominalBefore],["nominalAfter",op.nominalAfter]);
    if(op.acquiredAt)fields.push(["acquiredAt",date(op.acquiredAt)]);
    if(op.kind==="coupon")fields.push(["couponConfirmed",t(op.couponAllocationConfirmed?"batchYes":"batchNo")]);
    if(op.note)fields.push(["note",op.note]);
    return <details className="mt-2"><summary className="text-blue-600 dark:text-blue-400">{t("batchDetails")}</summary><dl className="mt-2 space-y-2">{fields.map(([key,value])=><div key={key}><dt className="text-xs text-gray-500">{t(key)}</dt><dd className="break-words tabular-nums">{value}</dd></div>)}</dl>{op.accruedRecoveries.map((recovery,index)=><p className="mt-2 break-all" key={index}>{t("lotOrigin")}: {recovery.originId} · {recovery.allRemaining?t("recoverAll"):recovery.amount}</p>)}</details>;
  };
  const {investment:api,investmentImport:imports}=useClients();
  const [current,setCurrent]=useState<Draft|null>(null);
  const [cancellationBlocked,setCancellationBlocked]=useState(false);
  const savedRows=useRef<ReviewedBatchRow[]|null>(null);
  const identity=useRef<{id:string;requestKey:string}|null>(null);
  const pendingSave=useRef<{id:string;expectedVersion:bigint;batch:Batch;rows:ReviewedBatchRow[]}|null>(null);
  const [saveUncertain,setSaveUncertain]=useState(false);
  const [uncertain,setUncertain]=useState(false);
  const [page,setPage]=useState(0);
  const [confirmed,setConfirmed]=useState(false);
  const [session]=useState(()=>crypto.randomUUID());
  const [cursors,setCursors]=useState<string[]>([""]);
  const drafts=useQuery<{drafts:Summary[]}>({
    queryKey:["investment-drafts",tenantId,session,cursors[cursors.length-1]],gcTime:0,
    queryFn:()=>api.listInvestmentImportDrafts({afterId:cursors[cursors.length-1]}),
  });
  const dirty=!!rows.length&&savedRows.current!==rows;
  const prepared=!dirty&&current?.preview&&current.batch?{batch:current.batch,result:current.preview}:null;
  const committed=!!prepared?.result.committed;
  useEffect(()=>{setConfirmed(false);setPage(0);},[rows]);
  useEffect(()=>{
    if(!dirty)return;
    const warn=(event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue="";};
    window.addEventListener("beforeunload",warn);
    return()=>window.removeEventListener("beforeunload",warn);
  },[dirty]);
  const knownFailure=(error:unknown)=>[Code.Aborted,Code.InvalidArgument,Code.FailedPrecondition,Code.PermissionDenied,Code.Unauthenticated,Code.NotFound].includes(ConnectError.from(error).code);
  const persist=async()=>{
    if(!pendingSave.current){
      const source=await imports.getInvestmentImport({id:rows[0].importId}) as {import?:InvestmentImport};
      if(!source.import)throw new Error(t("batchMixedImport"));
      const versions:Record<string,bigint>={};
      for(const ledgerId of new Set(rows.map(row=>row.operation.ledgerId))){
        const response=await api.getInvestmentSnapshot({ledgerId}) as {snapshot?:InvestmentSnapshot};
        if(!response.snapshot?.ledger)throw new Error(t("batchLedgerMissing"));
        versions[ledgerId]=response.snapshot.ledger.version;
      }
      if(!identity.current)identity.current={id:crypto.randomUUID(),requestKey:crypto.randomUUID()};
      const batch=reviewedBatch(rows,source.import,versions,identity.current.requestKey);
      pendingSave.current={id:identity.current.id,expectedVersion:current?.version||0n,batch,rows};
    }
    const attempt=pendingSave.current;
    try{
      const response=await api.saveInvestmentImportDraft({id:attempt.id,expectedVersion:attempt.expectedVersion,batch:attempt.batch}) as {draft?:Draft};
      if(!response.draft)throw new Error(t("batchSaveUncertain"));
      savedRows.current=attempt.rows;setCurrent(response.draft);pendingSave.current=null;setSaveUncertain(false);
      await drafts.refetch();
      return response.draft;
    }catch(error){
      if(knownFailure(error)){pendingSave.current=null;setSaveUncertain(false);}
      else setSaveUncertain(true);
      throw error;
    }
  };
  const save=useMutation({mutationFn:persist});
  const preview=useMutation({
    mutationFn:async()=>{
      const stored=await persist();
      const response=await api.previewInvestmentImportDraft({id:stored.id,expectedVersion:stored.version}) as {draft?:Draft};
      if(!response.draft?.preview?.hash)throw new Error(t("batchNoPreview"));
      return response.draft;
    },
    onSuccess:value=>{setCurrent(value);setConfirmed(false);},
  });
  const commit=useMutation({
    mutationFn:async()=>{
      if(!prepared||!current)throw new Error(t("batchNoPreview"));
      setUncertain(true);
      const response=await api.commitInvestmentImportDraft({id:current.id,expectedVersion:current.version,expectedHash:prepared.result.hash}) as {draft?:Draft};
      if(!response.draft?.preview?.committed)throw new Error(t("batchUncertain"));
      return response.draft;
    },
    onSuccess:value=>{setCurrent(value);setUncertain(false);drafts.refetch();onCommitted();},
    onError:error=>{
      if(knownFailure(error)){setUncertain(false);setCurrent(value=>value?{...value,preview:undefined}:null);setConfirmed(false);}
    },
  });
  const load=useMutation({
    mutationFn:async(id:string)=>{
      const response=await api.getInvestmentImportDraft({id}) as {draft?:Draft};
      const stored=response.draft;
      if(!stored?.batch)throw new Error(t("batchNoPreview"));
      const source=await imports.getInvestmentImport({id:stored.batch.importId}) as {import?:InvestmentImport};
      const names:Record<string,string>={};
      for(const ledgerId of new Set(stored.batch.entries.map(entry=>entry.operation?.ledgerId||""))){
        const snapshot=await api.getInvestmentSnapshot({ledgerId}) as {snapshot?:InvestmentSnapshot};
        names[ledgerId]=snapshot.snapshot?.ledger?.name||ledgerId;
      }
      const next=storedBatchRows(stored.batch,source.import?.filename||stored.batch.importId,names);
      return {stored,next};
    },
    onSuccess:({stored,next})=>{
      identity.current={id:stored.id,requestKey:stored.batch!.requestKey};
      savedRows.current=next;pendingSave.current=null;
      setSaveUncertain(false);setUncertain(false);setCurrent(stored);onChange(next);setConfirmed(false);
      save.reset();preview.reset();commit.reset();
      if(stored.preview?.committed)onCommitted(!!stored.cancellation?.cancelled);
    },
  });
  const remove=useMutation({
    mutationFn:(draft:Summary)=>api.deleteInvestmentImportDraft({id:draft.id,expectedVersion:draft.version}),
    onSuccess:()=>drafts.refetch(),
  });
  const checkStatus=useMutation({
    mutationFn:()=>api.getInvestmentImportDraft({id:current!.id}) as Promise<{draft?:Draft}>,
    onSuccess:response=>{
      // An uncommitted read can race an in-flight commit; only a durable
      // committed receipt resolves an uncertain financial outcome.
      const stored=response.draft;
      if(stored?.preview?.committed&&stored.batch){
        // Another tab may have committed a newer draft version. Replace both
        // the financial rows and the result; never pair old rows with new totals.
        const names=Object.fromEntries(stored.preview.snapshots.filter(snapshot=>snapshot.ledger).map(snapshot=>[snapshot.ledger!.id,snapshot.ledger!.name]));
        const next=storedBatchRows(stored.batch,rows[0]?.importId===stored.batch.importId?rows[0].filename:stored.batch.importId,names);
        savedRows.current=next;setCurrent(stored);onChange(next);setUncertain(false);drafts.refetch();onCommitted(!!stored.cancellation?.cancelled);
      }
    },
  });
  const busy=save.isPending||preview.isPending||commit.isPending||load.isPending||remove.isPending||checkStatus.isPending;
  const blocked=busy||uncertain||saveUncertain||cancellationBlocked;
  const locked=blocked||committed;
  useEffect(()=>{onLockedChange(locked);},[locked,onLockedChange]);
  const close=()=>{
    setCurrent(null);identity.current=null;pendingSave.current=null;savedRows.current=null;
    setUncertain(false);setSaveUncertain(false);setConfirmed(false);onChange([]);
    save.reset();preview.reset();commit.reset();load.reset();checkStatus.reset();
  };
  const error=save.error||preview.error||commit.error||load.error||remove.error||checkStatus.error||drafts.error;
  const errorMessage=error instanceof Error?(t.has(error.message)?t(error.message):error.message):String(error);
  return <div className="space-y-4">
    <section data-testid="investment-saved-batches" className="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-gray-800">
      <div className="flex flex-wrap justify-between gap-3"><h2 className="text-xl font-semibold">{t("batchSavedTitle")}</h2><button className="text-blue-600 dark:text-blue-400 disabled:opacity-50" disabled={blocked} onClick={()=>drafts.refetch()}>{t("batchReload")}</button></div>
      {drafts.isPending?<p>{t("loading")}</p>:!drafts.data?.drafts.length?<p className="text-sm text-gray-500">{t("batchSavedEmpty")}</p>:<ul className="space-y-2">{drafts.data.drafts.map(item=><li key={item.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-gray-800">
        <div className="min-w-0 text-sm"><p>{t("batchTitle",{count:item.rowCount})} · {t(item.cancelled?"batchCancelled":item.committed?"batchPosted":"batchSaved")}</p><p className="break-all text-xs text-gray-500">{item.updatedAt?new Date(Number(item.updatedAt.seconds)*1000).toLocaleString(locale):""} · {item.id.slice(0,8)}</p></div>
        <div className="flex gap-3"><button className="text-blue-600 dark:text-blue-400 disabled:opacity-50" disabled={blocked||!!rows.length} onClick={()=>load.mutate(item.id)}>{t(item.committed?"batchViewResult":"batchOpen")}</button>{!item.committed&&<button className="text-red-600 dark:text-red-400 disabled:opacity-50" disabled={blocked||!!rows.length} onClick={()=>remove.mutate(item)}>{t("batchDeleteSaved")}</button>}</div>
      </li>)}</ul>}
      {(cursors.length>1||drafts.data?.drafts.length===50)&&<div className="flex gap-3"><button disabled={blocked||cursors.length===1} onClick={()=>setCursors(xs=>xs.slice(0,-1))}>{t("previous")}</button><button disabled={blocked||drafts.data?.drafts.length!==50} onClick={()=>setCursors(xs=>[...xs,drafts.data!.drafts[49].id])}>{t("next")}</button></div>}
      {!!rows.length&&<p className="text-sm text-gray-500">{t("batchCloseToOpen")}</p>}
      {error&&!rows.length&&<p role="alert" className="break-words text-red-600">{errorMessage}</p>}
    </section>
    {!!rows.length&&<section data-testid="investment-import-batch" className="space-y-4 rounded-xl border border-blue-200 bg-blue-50/30 p-4 dark:border-blue-900 dark:bg-blue-950/20">
    <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-xl font-semibold">{t("batchTitle",{count:rows.length})}</h2><span className="break-all text-sm text-gray-500">{rows[0].filename}</span></div>
    <p className="text-sm text-gray-600 dark:text-gray-400">{t(committed?"batchResultHelp":"batchHelp")}</p>
    <ol className="space-y-2">{rows.slice(page*20,page*20+20).map(row=><li key={row.record} className="flex flex-wrap items-start justify-between gap-3 rounded-lg border border-gray-200 bg-white p-3 text-sm dark:border-gray-800 dark:bg-gray-950">
      <div className="min-w-0 flex-1 break-words"><p className="font-medium">{t("sourceRow")} {row.record} · {row.ledgerName} · {t(`kinds.${row.operation.kind}`)}</p><p className="tabular-nums">{row.operation.instrumentKey&&<>{row.operation.instrumentKey} · </>}{row.operation.distribution?`${row.operation.instrumentKey} → ${row.operation.distribution.targetInstrumentKey} · ${row.operation.distribution.newUnits} : ${row.operation.distribution.oldUnits}`:row.operation.kind==="split"?`${row.operation.splitNew} : ${row.operation.splitOld}`:row.operation.kind==="opening_position"&&!row.operation.openingCostKnown?"—":row.operation.amount} {row.operation.currency}</p><p className="text-gray-500">{row.reviewReason}</p>{operationDetails(row)}</div>
      {!committed&&<button className="text-red-600 dark:text-red-400 disabled:opacity-50" disabled={locked} onClick={()=>{preview.reset();commit.reset();if(rows.length===1)close();else onChange(rows.filter(r=>r.record!==row.record));}}>{t("batchRemove")}</button>}
    </li>)}</ol>
    {rows.length>20&&<div className="flex gap-3"><button disabled={!page} onClick={()=>setPage(p=>p-1)}>{t("previous")}</button><button disabled={(page+1)*20>=rows.length} onClick={()=>setPage(p=>p+1)}>{t("next")}</button></div>}
    {error&&<p role="alert" className="break-words text-sm text-red-600">{errorMessage}</p>}
    {uncertain&&!commit.isPending&&<p role="alert" className="text-sm text-amber-700 dark:text-amber-400">{t("batchUncertain")}</p>}
    <p role="status" className="text-sm text-gray-500">{t(current?.cancellation?.cancelled?"batchCancelled":committed?"batchSuccess":dirty?"batchUnsaved":"batchSaved")}</p>
    {saveUncertain&&<p role="alert" className="text-sm text-amber-700 dark:text-amber-400">{t("batchSaveUncertain")}</p>}
    <div className="flex flex-wrap gap-3">
      {!committed&&<button className={button} disabled={busy||uncertain||(!dirty&&!saveUncertain)} onClick={()=>{save.reset();save.mutate();}}>{saveUncertain?t("batchRetrySave"):t("batchSave")}</button>}
      {!committed&&<button className={button} disabled={blocked} onClick={()=>{setConfirmed(false);commit.reset();save.reset();preview.mutate();}}>{preview.isPending?t("loading"):t("batchPreview")}</button>}
      {uncertain&&<button className={button} disabled={busy} onClick={()=>checkStatus.mutate()}>{t("batchCheckStatus")}</button>}
      <button disabled={blocked} onClick={close}>{t(dirty?"batchClear":"batchClose")}</button>
    </div>
    {prepared&&<div className="space-y-4 border-t border-gray-200 pt-4 dark:border-gray-800">
      <h3 className="font-semibold">{t("batchAfter")}</h3>
      <div className="grid gap-3 md:grid-cols-2">{prepared.result.snapshots.map(snapshot=><article key={snapshot.ledger?.id} className="min-w-0 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-800">
        <h4 className="font-medium">{snapshot.ledger?.name}</h4>
        {snapshot.cash.map(cash=><p key={cash.currency} className="break-all text-sm tabular-nums">{cash.economic} {cash.currency}<span className="block text-xs text-gray-500">{t("settled")}: {cash.settled} · {t("receivable")}: {cash.receivable} · {t("payable")}: {cash.payable}</span></p>)}
        <details><summary className="text-sm">{t("positions")} ({snapshot.positions.length})</summary><ul className="space-y-1 text-sm">{snapshot.positions.map(position=><li className="break-all" key={position.instrumentKey}>{position.instrumentKey} · {position.quantity} · {t("cleanCost")}: {position.cleanCost||"—"} {position.currency}</li>)}</ul></details>
        {snapshot.issues.length>0&&<p className="text-sm text-amber-700 dark:text-amber-400">{t("batchQuality")}</p>}
      </article>)}</div>
      {!committed&&<label className="flex items-start gap-2 text-sm"><input type="checkbox" checked={confirmed} disabled={locked} onChange={e=>setConfirmed(e.target.checked)} />{t("batchConfirm",{count:rows.length})}</label>}
      {!committed&&<button className={button} disabled={busy||(!uncertain&&!confirmed)} onClick={()=>commit.mutate()}>{commit.isPending?t("loading"):uncertain?t("batchRetry"):t("batchCommit")}</button>}
    </div>}
    {committed&&current&&<InvestmentBatchCancellation key={current.id} draft={current} renderOperation={operation=>operationDetails({...rows[0],operation})} onBlocked={setCancellationBlocked} onChange={value=>{setCurrent(value);drafts.refetch();onCommitted(true);}}/>}
  </section>}
  </div>;
}
