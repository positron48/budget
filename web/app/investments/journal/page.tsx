"use client";
import InvestmentTradeFinancialsDetails from "@/components/InvestmentTradeFinancialsDetails";
import InvestmentTradeFinancialsForm,{tradeFinancialDraft,tradeFinancialPayload} from "@/components/InvestmentTradeFinancialsForm";
import {operationTime} from "@/lib/investment-operation-time";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import { useState } from "react";
import {useSearchParams} from "next/navigation";
import InvestmentMutationPreview from "@/components/InvestmentMutationPreview";
import type {InvestmentLotConversion,PreviewInvestmentLotQuantityResponse,PreviewInvestmentOperationResponse} from "@/proto/budget/v1/investment_pb";
import InvestmentCashTransferDialog from "@/components/InvestmentCashTransferDialog";
import { create as createMessage } from "@bufbuild/protobuf";
import { InvestmentOperationSchema } from "@/proto/budget/v1/investment_pb";
import InvestmentImportBatch from "@/components/InvestmentImportBatch";
import { queueReviewedRow, type ReviewedBatchRow } from "@/lib/investment-batch";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocale, useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { useAssetTenant } from "@/lib/assets";
import InvestmentAssetActivation from "@/components/InvestmentAssetActivation";
import InvestmentTransferDialog from "@/components/InvestmentTransferDialog";
import Modal from "@/components/Modal";
import type { InvestmentLedger, InvestmentOperation, InvestmentSnapshot, GetInvestmentOperationHistoryResponse } from "@/proto/budget/v1/investment_pb";
import type { InvestmentImport, InvestmentImportRow } from "@/proto/budget/v1/investment_import_pb";
import type { TenantMembership } from "@/proto/budget/v1/tenant_pb";

const field = "w-full rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-900";
const button = "rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50";
const kinds = ["cash_reconciliation", "distribution", "repo_income", "cash_interest", "deposit", "withdrawal", "buy", "sell", "dividend", "coupon", "fee", "tax", "tax_refund", "split", "amortization", "redemption", "fx", "opening_cash", "opening_position"] as const;
const localDate = (date: Date) => new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 19);
const stamp = (value: string) => {
  const ms = new Date(value).getTime();
  if (!Number.isFinite(ms)) throw new Error("Invalid date");
  return { seconds: BigInt(Math.floor(ms / 1000)), nanos: (ms % 1000) * 1000000 };
};
const dateOf = (timestamp?: { seconds: bigint; nanos: number }) => timestamp ? new Date(Number(timestamp.seconds) * 1000 + timestamp.nanos / 1000000) : new Date();
const blank = () => ({ quantityMode:"units",lots:"", occurredPrecision:"timestamp",settlementPrecision:"timestamp",timingTimezone:"Europe/Moscow", expectedSettled:"", observedSettled:"", kind: "deposit", instrumentKey: "", currency: "RUB", toCurrency: "USD", toAmount: "", quantity: "", amount: "", accrued: "", fee: "", tax: "", splitNew: "", splitOld: "", nominalBefore: "", nominalAfter: "", acquiredAt: "", occurredAt: localDate(new Date()), settlementAt: "", note: "" });
type Draft = ReturnType<typeof blank>;
const blankDistribution = () => ({eventKey:"",targetInstrumentKey:"",newUnits:"",oldUnits:"",costNumerator:"",costDenominator:"",consumeSource:false,marketValue:"",marketValueEvidence:""});

function Journal({ tenantId }: { tenantId: string }) {
  const pt=useTranslations("investmentPreview"),tt=useTranslations("investmentTiming"),lt=useTranslations("investmentLotInput");
 const t = useTranslations("investmentJournal"),opsT=useTranslations("investmentOperations");
  const locale = useLocale();
  const { investment: api, investmentImport: importsAPI, tenant } = useClients();
  const qc = useQueryClient();
  const search=useSearchParams();
  const [id, setID] = useState(search.get("ledger")||"");
  const [cashDialog,setCashDialog]=useState<{id?:string}|null>(search.get("cashTransfer")?(search.get("cashTransfer")==="new"?{}:{id:search.get("cashTransfer")!}):null);
  const [batchRows,setBatchRows]=useState<ReviewedBatchRow[]>([]);
  const [batchLocked,setBatchLocked]=useState(false);
  const [batchSuccess,setBatchSuccess]=useState(false);
  const [queueMode,setQueueMode]=useState(false);
  const [offset, setOffset] = useState(0);
  const [ledgerOffset, setLedgerOffset] = useState(0);
  const [asOf, setAsOf] = useState("");
  const [name, setName] = useState("");
  const [started, setStarted] = useState(localDate(new Date()));
  const [createKey, setCreateKey] = useState("");
  const [transferDialog,setTransferDialog]=useState<{id?:string}|null>(null);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<InvestmentOperation | null>(null);
  const [voiding, setVoiding] = useState<InvestmentOperation | null>(null);
  const [draft, setDraft] = useState<Draft>(blank);
 const [cashDeficitReason,setCashDeficitReason]=useState("");
 const [financialDraft,setFinancialDraft]=useState(tradeFinancialDraft);
 const [storedLotTerms,setStoredLotTerms]=useState<InvestmentLotConversion>();
  const [openingCostKnown,setOpeningCostKnown]=useState(false);
  const [openingAccruedKnown,setOpeningAccruedKnown]=useState(false);
  const [distributionTerms,setDistributionTerms]=useState(blankDistribution);
  const [taxReviewRequired,setTaxReviewRequired]=useState(false);
  const [couponConfirmed, setCouponConfirmed] = useState(false);
  const [recoveries, setRecoveries] = useState<{originId:string;amount:string;allRemaining:boolean}[]>([]);
  const [reason, setReason] = useState("");
  const [requestKey, setRequestKey] = useState("");
  const [sourceID, setSourceID] = useState("");
  const [sourceRecord, setSourceRecord] = useState("1");
  const [reviewReason, setReviewReason] = useState("");
  const [importsOffset, setImportsOffset] = useState(0);
  const [expectedVersion, setExpectedVersion] = useState(0n);
  const roles = useQuery<{ memberships: TenantMembership[] }>({ queryKey: ["investment", tenantId, "roles"], queryFn: () => tenant.listMyTenants({}) });
  const role = roles.data?.memberships.find(m => m.tenant?.id === tenantId)?.role;
  const readOnly=useInvestmentReadOnly();
  const canWriteRole = !readOnly && (role === 1 || role === 2 || role === 3);
  const admin = !readOnly && (role === 1 || role === 2);
  const imports = useQuery<{ imports: InvestmentImport[] }>({ queryKey: ["investment", tenantId, "sources", importsOffset], enabled: open && admin && !editing, queryFn: () => importsAPI.listInvestmentImports({ limit: 200, offset: importsOffset }) });
  const record = Number(sourceRecord);
  const validRecord = Number.isInteger(record) && record > 0 && record <= 100000;
  const source = useQuery<{ rows: InvestmentImportRow[] }>({ queryKey: ["investment", tenantId, "source", sourceID, sourceRecord], enabled: open && !!sourceID && validRecord && !editing, queryFn: () => importsAPI.listInvestmentImportRows({ importId: sourceID, limit: 1, offset: record - 1 }) });
  const reviewedRow = validRecord && source.data?.rows[0]?.record === record ? source.data.rows[0] : undefined;
  const ledgers = useQuery<{ ledgers: InvestmentLedger[] }>({ queryKey: ["investment", tenantId, "ledgers", ledgerOffset], queryFn: () => api.listInvestmentLedgers({ limit: 50, offset: ledgerOffset }) });
  const snapshot = useQuery<{ snapshot?: InvestmentSnapshot }>({ queryKey: ["investment", tenantId, id, "snapshot", asOf], enabled: !!id, queryFn: () => api.getInvestmentSnapshot({ ledgerId: id, asOf: asOf ? stamp(asOf) : undefined }) });
  const history = useQuery<{ operations: InvestmentOperation[] }>({ queryKey: ["investment", tenantId, id, "operations", offset], enabled: !!id, queryFn: () => api.listInvestmentOperations({ ledgerId: id, limit: 50, offset }) });
  const selectedOperation=useQuery<GetInvestmentOperationHistoryResponse>({queryKey:["investment",tenantId,id,"selected-operation",search.get("operation")],enabled:!!id&&!!search.get("operation"),queryFn:()=>api.getInvestmentOperationHistory({ledgerId:id,operationId:search.get("operation"),limit:1})});
  const refresh = () => qc.invalidateQueries({ queryKey: ["investment", tenantId] });
  const create = useMutation({ mutationFn: () => api.createInvestmentLedger({ name, startedAt: stamp(started), requestKey: createKey }), onSuccess: (r: { ledger: InvestmentLedger }) => { setID(r.ledger.id); setOffset(0); setName(""); setCreateKey(""); refresh(); } });
  const openingPosition = draft.kind === "opening_position";
  const opening = openingPosition || draft.kind === "opening_cash";
  const reconciliation=draft.kind === "cash_reconciliation";
  const fx = draft.kind === "fx";
  const trade = draft.kind === "buy" || draft.kind === "sell";
  const principal = draft.kind === "amortization" || draft.kind === "redemption";
  const settlement = trade || principal || fx;
  const income = draft.kind === "repo_income" || draft.kind === "dividend" || draft.kind === "coupon";
  const distribution = draft.kind === "distribution";
  const split = draft.kind === "split";
 const lotsMode=trade&&draft.quantityMode==="lots";
 const lotSignature=JSON.stringify([draft.instrumentKey,draft.occurredAt,draft.occurredPrecision,draft.timingTimezone,draft.lots]);
 const lotConverter=useMutation<PreviewInvestmentLotQuantityResponse,Error,{signature:string}>({mutationFn:()=>api.previewInvestmentLotQuantity({instrumentKey:draft.instrumentKey,lots:draft.lots,occurredAt:draft.occurredPrecision==="date_only"?undefined:editing?.occurredAt&&draft.occurredAt===localDate(dateOf(editing.occurredAt))?editing.occurredAt:stamp(draft.occurredAt),timing:draft.occurredPrecision==="date_only"?{timezone:draft.timingTimezone,occurredDate:draft.occurredAt}:undefined})});
 const convertedLot=lotConverter.variables?.signature===lotSignature?lotConverter.data:undefined;
 const lotTerms=convertedLot?.conversion||storedLotTerms;
  const buildPayload=()=>{
 if(lotsMode&&!lotTerms)throw new Error(lt("required"));
 if (sourceID && (!reviewedRow || !!reviewedRow.operationId || source.isFetching || !reviewReason.trim())) throw new Error(t("sourceRequired"));
      return {
      source: sourceID && reviewedRow ? { importId: sourceID, record: reviewedRow.record, expectedVersion: reviewedRow.version, reviewReason } : undefined,
      operation: { importedCashDeficitReason: (sourceID||editing?.source)?cashDeficitReason:"", tradeFinancials:trade&&financialDraft.enabled?tradeFinancialPayload(financialDraft,draft.currency,editing?.tradeFinancials):undefined,lotConversion:lotsMode&&lotTerms?{...lotTerms,lots:draft.lots}:undefined, timing: draft.occurredPrecision==="date_only" || (settlement && draft.settlementPrecision==="date_only" && draft.settlementAt) ? {timezone:draft.timingTimezone,occurredDate:draft.occurredPrecision==="date_only"?draft.occurredAt:"",settlementDate:settlement && draft.settlementPrecision==="date_only"?draft.settlementAt:"",sourceDate:editing?.timing?.sourceDate&&draft.occurredAt===(editing.timing.occurredDate||localDate(dateOf(editing.occurredAt)))?editing.timing.sourceDate:""} : editing?.timing && draft.occurredAt===localDate(dateOf(editing.occurredAt)) ? {...editing.timing,occurredDate:"",settlementDate:""}:undefined, cashReconciliation:reconciliation?{expectedSettled:draft.expectedSettled,observedSettled:draft.observedSettled}:undefined, distribution: distribution ? distributionTerms : undefined, taxReviewRequired: (income || draft.kind === "cash_interest") && taxReviewRequired, openingCostKnown: openingPosition && openingCostKnown, openingAccruedKnown: openingPosition && openingAccruedKnown, acquiredAt:openingPosition && draft.acquiredAt ? (editing?.acquiredAt && draft.acquiredAt === localDate(dateOf(editing.acquiredAt)) ? editing.acquiredAt : stamp(draft.acquiredAt)) : undefined, couponAllocationConfirmed: draft.kind === "coupon" && couponConfirmed, accruedRecoveries: draft.kind === "coupon" && couponConfirmed ? recoveries.map(a => ({...a, amount:a.allRemaining ? "0" : a.amount})) : [], id: editing?.id || "", ledgerId: id, kind: draft.kind, instrumentKey: trade || income || split || principal || openingPosition || distribution ? draft.instrumentKey : "", currency: split ? "" : draft.currency, toCurrency: fx ? draft.toCurrency : "", toAmount: fx ? draft.toAmount : "0",
        quantity: lotsMode?"0":trade || draft.kind === "redemption" || openingPosition ? draft.quantity : "0", amount: trade&&financialDraft.enabled?"0":reconciliation || split || distribution || (openingPosition && !openingCostKnown) ? "0" : draft.amount, accrued: trade&&financialDraft.enabled?"0":trade || (openingPosition && openingAccruedKnown) ? draft.accrued : "0", fee: trade&&financialDraft.enabled?"0":trade || fx ? draft.fee : "0", tax: income ? draft.tax : "0", splitNew: split ? draft.splitNew : "0", splitOld: split ? draft.splitOld : "0",
        occurredAt: draft.occurredPrecision==="date_only"?undefined: opening && state?.ledger?.startedAt && draft.occurredAt === localDate(dateOf(state.ledger.startedAt)) ? state.ledger.startedAt : editing?.occurredAt && draft.occurredAt === localDate(dateOf(editing.occurredAt)) ? editing.occurredAt : stamp(draft.occurredAt), settlementAt: settlement && draft.settlementAt && draft.settlementPrecision!=="date_only" ? (editing?.settlementAt && draft.settlementAt === localDate(dateOf(editing.settlementAt)) ? editing.settlementAt : stamp(draft.settlementAt)) : undefined, note: draft.note, nominalBefore: principal ? draft.nominalBefore : "0", nominalAfter: draft.kind === "amortization" ? draft.nominalAfter : "0" },
      expectedLedgerVersion: expectedVersion, expectedOperationVersion: editing?.version || 0n, reason, requestKey,
    };
  };
 const formSignature=JSON.stringify({id,draft,cashDeficitReason,financialDraft,lotTerms,editing:editing?.id,expectedVersion,sourceID,sourceRecord,sourceVersion:reviewedRow?.version,reviewReason,openingCostKnown,openingAccruedKnown,taxReviewRequired,couponConfirmed,recoveries,distributionTerms,reason,requestKey},(_key,value)=>typeof value==="bigint"?value.toString():value);
 const preview=useMutation<PreviewInvestmentOperationResponse,Error,{signature:string}>({mutationFn:()=>api.previewInvestmentOperation({...buildPayload(),action:editing?"amend":"create"})});
 const reviewed=!!preview.data?.valid&&!!preview.data.previewHash&&preview.variables?.signature===formSignature;
 const voidSignature=JSON.stringify({id,operation:voiding?.id,operationVersion:voiding?.version,reason,requestKey,expectedVersion},(_key,value)=>typeof value==="bigint"?value.toString():value);
 const voidPayload=()=>({action:"void",operation:{ledgerId:id,id:voiding!.id},expectedLedgerVersion:expectedVersion,expectedOperationVersion:voiding!.version,reason,requestKey});
 const voidPreview=useMutation<PreviewInvestmentOperationResponse,Error,{signature:string}>({mutationFn:()=>api.previewInvestmentOperation(voidPayload())});
 const voidReviewed=!!voidPreview.data?.valid&&!!voidPreview.data.previewHash&&voidPreview.variables?.signature===voidSignature;
 const save = useMutation({
 mutationFn:async()=>{
 const payload=buildPayload();
      if(queueMode && sourceID && !editing){
        if(batchLocked || !reviewedRow)throw new Error(t("batchBusy"));
        const response=await importsAPI.getInvestmentImport({id:sourceID}) as {import?:InvestmentImport};
        if(!response.import?.previewHash)throw new Error(t("batchMixedImport"));
        try {
          const next=queueReviewedRow(batchRows,{importId:sourceID,previewHash:response.import.previewHash,filename:response.import.filename,record:reviewedRow.record,rowVersion:reviewedRow.version,reviewReason,ledgerName:state?.ledger?.name||id,operation:createMessage(InvestmentOperationSchema,payload.operation)});
          setBatchRows(next);setBatchSuccess(false);
        } catch(error) {throw new Error(t(error instanceof Error&&["batchMixedImport","batchLimit"].includes(error.message)?error.message:"batchMixedImport"));}
        return {queued:true};
      }
      if(!reviewed)throw new Error(pt("required"));
      return api.saveInvestmentOperation({...payload,previewHash:preview.data!.previewHash});
    },
    onError:(error)=>{if([9,10].includes((error as {code?:number}).code||0))preview.reset()},
    onSuccess: () => { setOpen(false); setOffset(0); refresh(); },
  });
  const cancel = useMutation({ mutationFn: () => {if(!voidReviewed)throw new Error(pt("required"));return api.voidInvestmentOperation({ previewHash:voidPreview.data!.previewHash,ledgerId: id, operationId: voiding!.id, expectedLedgerVersion: expectedVersion, expectedOperationVersion: voiding!.version, reason, requestKey })}, onError:(error)=>{if([9,10].includes((error as {code?:number}).code||0))voidPreview.reset()}, onSuccess: () => { setVoiding(null); refresh(); } });
  const state = snapshot.data?.snapshot;
 const canWrite=canWriteRole&&!state?.ledger?.archived;
  const error = ledgers.error || snapshot.error || history.error || roles.error || create.error;
  const begin = (operation?: InvestmentOperation) => {
    setCashDeficitReason(operation?.importedCashDeficitReason||"");
    setFinancialDraft(tradeFinancialDraft(operation?.tradeFinancials,operation?.currency||state?.ledger?.defaultCurrency||"RUB"));setQueueMode(false);setStoredLotTerms(operation?.lotConversion);lotConverter.reset();
    setDistributionTerms(operation?.distribution ? {...operation.distribution} : blankDistribution());
    setOpeningCostKnown(operation?.openingCostKnown||false);setOpeningAccruedKnown(operation?.openingAccruedKnown||false);
    setTaxReviewRequired(operation?.taxReviewRequired || false);
    setCouponConfirmed(operation?.couponAllocationConfirmed || false); setRecoveries(operation?.accruedRecoveries.map(a => ({originId:a.originId,amount:a.amount,allRemaining:a.allRemaining})) || []);
    setSourceID(""); setSourceRecord("1"); setReviewReason("");
    setExpectedVersion(state?.ledger?.version || 0n); setRequestKey(crypto.randomUUID()); setReason(""); setEditing(operation || null); save.reset();
    setDraft(operation ? { quantityMode:operation.lotConversion?"lots":"units",lots:operation.lotConversion?.lots||"",occurredPrecision:operation.timing?.occurredDate?"date_only":"timestamp",settlementPrecision:operation.timing?.settlementDate?"date_only":"timestamp",timingTimezone:operation.timing?.timezone||"Europe/Moscow",expectedSettled:operation.cashReconciliation?.expectedSettled||"",observedSettled:operation.cashReconciliation?.observedSettled||"", kind: operation.kind, instrumentKey: operation.instrumentKey, currency: operation.currency || "RUB", toCurrency: operation.toCurrency || "USD", toAmount: operation.toAmount, quantity: operation.quantity, amount: operation.amount, accrued: operation.accrued, fee: operation.fee, tax: operation.tax, splitNew: operation.splitNew, splitOld: operation.splitOld, nominalBefore: operation.nominalBefore, nominalAfter: operation.nominalAfter, acquiredAt:operation.acquiredAt ? localDate(dateOf(operation.acquiredAt)) : "", occurredAt:operation.timing?.occurredDate||localDate(dateOf(operation.occurredAt)), settlementAt: operation.settlementAssumed ? "" : operation.timing?.settlementDate||localDate(dateOf(operation.settlementAt)), note: operation.note } : {...blank(),currency:state?.ledger?.defaultCurrency||"RUB"});
    setOpen(true);
  };
  const change = (key: keyof Draft, value: string) => { if(["quantityMode","lots","occurredAt","occurredPrecision","timingTimezone","instrumentKey"].includes(key)){setStoredLotTerms(undefined);lotConverter.reset();} setDraft(d => ({ ...d, [key]: value })); setRequestKey(crypto.randomUUID()); save.reset(); };
  const textField = (key: keyof Draft, required = false, type = "text") => <label className="block text-sm" key={key}>{t(key)}<input className={field} type={type} step={type === "datetime-local" ? "1" : undefined} required={required} value={draft[key]} disabled={save.isPending} maxLength={key === "instrumentKey" ? 200 : 128} onChange={e => change(key, e.target.value)} /></label>;
  return <main className="mx-auto max-w-7xl space-y-6 px-4 py-8">
    <div className="flex flex-wrap items-center justify-between gap-3"><h1 className="text-3xl font-semibold">{t("title")}</h1><div className="flex min-w-0 flex-wrap gap-4"><Link className="text-blue-600" href="/investments">{t("import")}</Link><Link className="text-blue-600" href="/investments/accounts">{locale === "ru" ? "Счета" : "Accounts"}</Link><Link className="text-blue-600" href="/investments/operations">{opsT("title")}</Link><Link className="text-blue-600" href="/investments/market">{t("market")}</Link><Link className="text-blue-600" href="/investments/value">{t("value")}</Link></div></div>
    <p className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100">{t("notice")}</p>
    {error && <p role="alert" className="text-red-600">{String(error)}</p>}
    {batchSuccess&&<p role="status" className="rounded-lg bg-green-50 p-3 text-green-800 dark:bg-green-950 dark:text-green-200">{t("batchSuccess")}</p>}
    {admin&&<InvestmentImportBatch tenantId={tenantId} rows={batchRows} onChange={rows=>{setBatchRows(rows);setBatchSuccess(false);}} onLockedChange={setBatchLocked} onCommitted={(cancelled)=>{setBatchSuccess(!cancelled);refresh();qc.invalidateQueries({queryKey:["investment-import",tenantId]});}} />}

    {admin && <form className="grid items-end gap-3 rounded-xl border border-gray-200 p-4 dark:border-gray-800 md:grid-cols-3" onSubmit={e => { e.preventDefault(); create.mutate(); }}>
      <label className="text-sm">{t("name")}<input className={field} value={name} maxLength={200} required disabled={create.isPending} onChange={e => { setName(e.target.value); setCreateKey(crypto.randomUUID()); create.reset(); }} /></label>
      <label className="text-sm">{t("started")}<input className={field} type="datetime-local" step="1" value={started} required disabled={create.isPending} onChange={e => { setStarted(e.target.value); setCreateKey(crypto.randomUUID()); create.reset(); }} /></label>
      <button className={button} disabled={create.isPending || !createKey || !name.trim()}>{t("create")}</button><p className="text-sm text-gray-500 md:col-span-3">{t("openingHelp")}</p>
    </form>}
    <div className="flex flex-wrap gap-2">{ledgers.data?.ledgers.map(l => <button key={l.id} className={`rounded-lg border px-4 py-2 ${id === l.id ? "border-blue-500 bg-blue-50 text-blue-950 dark:bg-blue-950 dark:text-blue-100" : "border-gray-300 dark:border-gray-700"}`} onClick={() => { setID(l.id); setOffset(0); setAsOf(""); }}>{l.name}</button>)}</div>
    {(ledgerOffset > 0 || ledgers.data?.ledgers.length === 50) && <div className="flex gap-3"><button disabled={!ledgerOffset} onClick={() => setLedgerOffset(x => x - 50)}>{t("previous")}</button><button disabled={ledgers.data?.ledgers.length !== 50} onClick={() => setLedgerOffset(x => x + 50)}>{t("next")}</button></div>}
    {ledgers.isPending && <p>{t("loading")}</p>}{ledgers.data?.ledgers.length === 0 && <p>{t("empty")}</p>}
    {state?.ledger?.archived&&<p role="status">{locale==="ru"?"Счёт архивирован. История доступна; для исправлений восстановите счёт в разделе «Счета».":"This account is archived. History remains available; restore the account in Accounts before making corrections."}</p>}
    {id && <div className="flex flex-wrap items-end gap-3"><label className="text-sm">{t("asOf")}<input className={field} type="datetime-local" step="1" value={asOf} onChange={e => setAsOf(e.target.value)} /></label><button className={button} onClick={() => { setAsOf(""); refresh(); }}>{t("current")}</button>{canWrite && <button className={button} disabled={!state || snapshot.isFetching} onClick={() => begin()}>{t("add")}</button>}{canWrite && <button className={button} disabled={!state || snapshot.isFetching} onClick={() => setTransferDialog({})}>{t("transfer")}</button>}</div>}
    {canWrite&&state&&<button className={button} onClick={()=>setCashDialog({})}>{t("cashTransfer")}</button>}
    {admin&&state&&<InvestmentAssetActivation key={id} tenantId={tenantId} ledgerId={id} />}
    {(snapshot.isFetching || history.isFetching) && id && <p role="status">{t("loading")}</p>}
    {state && <>
      {state.issues.includes("tax_review_required") && <p className="text-sm text-amber-700 dark:text-amber-400">{t("taxReviewHelp")}</p>}
      {state.issues.filter(v=>["negative_imported_cash","imported_cash_history_incomplete"].includes(v)).map(v=><p key={v} className="text-sm text-amber-700">{pt(`warnings.${v}`)}</p>)}
      {state.issues.includes("trade_settlement_difference_reviewed")&&<p className="text-sm text-amber-700">{locale==="ru"?"Расхождение расчёта подтверждено и отнесено на чистую сумму сделки.":"A settlement difference was reviewed and allocated to clean principal."}</p>}
      {state.issues.includes("settlement_assumed_trade_time") && <p className="text-sm text-amber-700 dark:text-amber-400">{t("assumedSettlement")}</p>}
      {state.issues.includes("coupon_allocation_required") && <p className="text-sm text-amber-700 dark:text-amber-400">{t("couponUnknown")}</p>}
      {state.issues.some(i=>["history_before_opening_unknown","unknown_acquisition_cost","unknown_acquired_interest","historical_acquisition_date_unknown"].includes(i)) && <p className="text-sm text-amber-700 dark:text-amber-400">{t("openingQuality")}</p>}
      <section className="space-y-3"><h2 className="text-xl font-semibold">{t("cash")}</h2><div className="grid gap-3 md:grid-cols-3">{state.cash.map(c => <div key={c.currency} className="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-gray-800"><p className="break-all text-xl font-semibold tabular-nums">{c.economic} {c.currency}</p><dl className="mt-3 grid grid-cols-2 gap-2 text-sm"><dt>{t("settled")}</dt><dd className="min-w-0 break-all text-right tabular-nums">{c.settled}</dd><dt>{t("receivable")}</dt><dd className="min-w-0 break-all text-right tabular-nums">{c.receivable}</dd><dt>{t("payable")}</dt><dd className="min-w-0 break-all text-right tabular-nums">{c.payable}</dd></dl></div>)}</div>{!state.cash.length && <p className="text-gray-500">{t("noCash")}</p>}</section>
      <section className="space-y-3"><h2 className="text-xl font-semibold">{t("positions")}</h2><div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr>{["instrumentKey", "quantity", "outstandingNominal", "cleanCost", "acquiredAccrued", "realizedClean", "realizedAccrued"].map(k => <th key={k} className="p-3">{t(k)}</th>)}</tr></thead><tbody>{state.positions.map(p => <tr key={p.instrumentKey} className="border-t border-gray-200 dark:border-gray-800"><td className="p-3">{p.instrumentKey}</td><td className="p-3 tabular-nums">{p.quantity}</td><td className="p-3 tabular-nums">{p.outstandingNominal || "—"}</td>{[p.cleanCost, p.acquiredAccrued, p.realizedClean, p.realizedAccrued].map((v, i) => <td className="whitespace-nowrap p-3 tabular-nums" key={i}>{v || "—"} {p.currency}</td>)}</tr>)}</tbody></table></div></section>
      <section className="space-y-3"><h2 className="text-xl font-semibold">{t("couponSummary")}</h2>{state.positions.map(p => <details key={p.instrumentKey} className="rounded-lg border border-gray-200 p-3 dark:border-gray-800"><summary>{p.instrumentKey} · {t("couponEarned")}: {p.couponEarned || "—"} {p.currency}</summary><p className="my-2 text-sm">{t("couponGross")}: {p.couponGross} · {t("couponRecovered")}: {p.couponRecovered} · {t("tax")}: {p.couponTax}</p><div className="overflow-x-auto"><table className="w-full text-sm"><thead><tr>{["lotOrigin","quantity","cleanCost","acquiredAccrued"].map(k => <th className="p-2 text-left" key={k}>{t(k)}</th>)}</tr></thead><tbody>{p.lots.map(l => <tr key={l.originId}><td className="select-all p-2 font-mono text-xs">{l.originId}</td><td className="p-2">{l.quantity}</td><td className="p-2">{l.cleanCost || "—"}</td><td className="p-2">{l.acquiredAccrued || "—"}</td></tr>)}</tbody></table></div></details>)}</section>
    </>}
    {selectedOperation.error&&<p role="alert">{opsT("error")}</p>}
    {selectedOperation.data?.current&&<section className="space-y-2 rounded-xl border border-blue-300 p-4 dark:border-blue-800"><h2 className="font-semibold">{opsT("selected")}</h2><p className="break-all text-sm">{selectedOperation.data.current.id} · {t(`kinds.${selectedOperation.data.current.kind}`)} · {selectedOperation.data.current.amount} {selectedOperation.data.current.currency}</p>{selectedOperation.data.current.tradeFinancials&&<InvestmentTradeFinancialsDetails operation={selectedOperation.data.current}/>}<p>{selectedOperation.data.current.status==="voided"?t("voided"):t("posted")}</p>{canWrite&&selectedOperation.data.current.status==="posted"&&<button className={button} disabled={!state||snapshot.isFetching} onClick={()=>{const o=selectedOperation.data!.current!;if(o.budgetTransferId)setCashDialog({id:o.budgetTransferId});else if(o.transferId)setTransferDialog({id:o.transferId});else begin(o)}}>{opsT("editSelected")}</button>}</section>}
    {id && <section className="space-y-3"><h2 className="text-xl font-semibold">{t("operations")}</h2><div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr>{["occurredAt", "kind", "instrumentKey", "quantity", "amount", "status", "actions"].map(k => <th key={k} className="p-3">{t(k)}</th>)}</tr></thead><tbody>{history.data?.operations.map(o => <tr key={o.id} className={`border-t border-gray-200 dark:border-gray-800 ${o.status === "voided" ? "opacity-50" : ""}`}><td className="whitespace-nowrap p-3">{operationTime(o,false,locale,tt("dateOnly"))}</td><td className="p-3">{t(`kinds.${o.kind}`)}{o.importedCashDeficitReason&&<p className="mt-1 max-w-xs whitespace-normal text-xs text-amber-700">{pt("cashDeficitReason")}: {o.importedCashDeficitReason}</p>}{o.source && <p className="mt-1 text-xs text-gray-500" title={o.source.reviewReason}>{t("sourceRow")} {o.source.record}</p>}</td><td className="p-3">{o.instrumentKey || "—"}</td><td className="p-3 tabular-nums">{o.quantity}</td><td className="whitespace-nowrap p-3 tabular-nums">{o.kind === "opening_position" && !o.openingCostKnown ? "—" : o.amount} {o.currency}{o.tradeFinancials&&<span className="block">{locale==="ru"?"Фактический расчёт":"Actual settlement"}: {o.tradeFinancials.actualSettlement} {o.tradeFinancials.principal?.currency}</span>}{o.kind === "fx" && <span> → {o.toAmount} {o.toCurrency}</span>}</td><td className="p-3">{o.status === "voided" ? t("voided") : t("posted")}</td><td className="space-x-3 whitespace-nowrap p-3">{canWrite && o.transferId && <button className="text-blue-600" onClick={() => setTransferDialog({id:o.transferId})}>{t("transfer")}</button>}{canWrite&&o.budgetTransferId&&<button className="text-blue-600" onClick={()=>setCashDialog({id:o.budgetTransferId})}>{t("cashTransfer")}</button>}{canWrite && !o.transferId && !o.budgetTransferId && o.status === "posted" && <><button className="text-blue-600" disabled={!state || snapshot.isFetching} onClick={() => begin(o)}>{t("edit")}</button><button className="text-red-600" disabled={!state || snapshot.isFetching} onClick={() => { setVoiding(o); setReason(""); setRequestKey(crypto.randomUUID()); setExpectedVersion(state?.ledger?.version || 0n); cancel.reset(); }}>{t("void")}</button></>}</td></tr>)}</tbody></table></div><div className="flex justify-between"><button className={button} disabled={!offset || history.isFetching} onClick={() => setOffset(x => Math.max(0, x - 50))}>{t("previous")}</button><button className={button} disabled={history.data?.operations.length !== 50 || history.isFetching} onClick={() => setOffset(x => x + 50)}>{t("next")}</button></div></section>}
    {canWrite&&cashDialog&&<InvestmentCashTransferDialog tenantId={tenantId} ledgerId={id} transferId={cashDialog.id} onClose={()=>setCashDialog(null)} onSaved={refresh} />}
    {transferDialog && <InvestmentTransferDialog tenantId={tenantId} fromLedgerId={id} transferId={transferDialog.id} onClose={() => setTransferDialog(null)} onSaved={refresh} />}
    <Modal open={open} title={editing ? t("edit") : t("add")} onClose={() => { if (!save.isPending) setOpen(false); }} maxWidthClass="max-w-2xl">
      <form className="space-y-4" onSubmit={e => { e.preventDefault(); save.mutate(); }}>
        {admin && !editing && <fieldset className="space-y-3 rounded-lg border border-gray-300 p-3 dark:border-gray-700" disabled={save.isPending}>
          <legend className="px-1 text-sm font-medium">{t("sourceTitle")}</legend>
          <label className="block text-sm">{t("sourceFile")}<select className={field} value={sourceID} onChange={e => { setSourceID(e.target.value); setSourceRecord("1"); setReviewReason(""); setRequestKey(crypto.randomUUID()); save.reset(); }}><option value="">{t("manual")}</option>{imports.data?.imports.map(i => <option key={i.id} value={i.id}>{i.filename} · {i.id.slice(0,8)}</option>)}</select></label>
          {(importsOffset > 0 || imports.data?.imports.length === 200) && <div className="flex gap-3"><button type="button" disabled={!importsOffset} onClick={() => { setImportsOffset(x => x - 200); setSourceID(""); }}>{t("previous")}</button><button type="button" disabled={imports.data?.imports.length !== 200} onClick={() => { setImportsOffset(x => x + 200); setSourceID(""); }}>{t("next")}</button></div>}
          {imports.error && <p role="alert">{String(imports.error)}</p>}
          {sourceID && <>
            <label className="block text-sm">{t("sourceRow")}<input className={field} type="number" min="1" max="100000" step="1" value={sourceRecord} onChange={e => { setSourceRecord(e.target.value); setReviewReason(""); setRequestKey(crypto.randomUUID()); save.reset(); }} /></label>
            {source.isFetching && <p role="status">{t("loading")}</p>}
            {source.error && <p role="alert">{String(source.error)}</p>}
            {!source.isFetching && !reviewedRow && <p role="alert">{t("sourceRequired")}</p>}
            {reviewedRow && <dl className="grid grid-cols-2 gap-1 break-words text-xs">{Object.entries(reviewedRow.sourceFields).map(([key,value]) => <div className="contents" key={key}><dt>{key}</dt><dd>{value || "—"}</dd></div>)}</dl>}
            {reviewedRow?.operationId && <p role="alert">{t("sourcePosted")} · {reviewedRow.operationStatus}</p>}
            <p className="text-sm text-amber-700 dark:text-amber-400">{t("sourceHelp")}</p>
            {reviewedRow && reviewedRow.identicalRecords.length > 1 && <p role="alert" className="text-sm text-amber-700 dark:text-amber-400">{t("sourceDuplicates")} {reviewedRow.identicalRecords.join(", ")}</p>}
            <label className="flex items-start gap-2 text-sm"><input type="checkbox" checked={queueMode} disabled={batchLocked} onChange={e=>{setQueueMode(e.target.checked);save.reset();}} />{t("batchQueueMode")}</label>
            <label className="block text-sm">{t("sourceReview")}<textarea className={field} required maxLength={2000} value={reviewReason} onChange={e => { setReviewReason(e.target.value); setRequestKey(crypto.randomUUID()); save.reset(); }} /></label>
          </>}
        </fieldset>}
        {(sourceID||editing?.source)&&<fieldset className="space-y-2 rounded-lg border border-amber-300 p-3"><legend>{pt("cashDeficitTitle")}</legend><p className="text-sm text-amber-700 dark:text-amber-400">{pt("cashDeficitPolicy")}</p><label className="block text-sm">{pt("cashDeficitReason")}<textarea className={field} maxLength={2000} value={cashDeficitReason} disabled={save.isPending} onChange={e=>{setCashDeficitReason(e.target.value);setRequestKey(crypto.randomUUID());save.reset()}}/></label></fieldset>}
        {editing?.source && <p className="text-sm">{t("sourceRow")} {editing.source.record}: {editing.source.reviewReason}</p>}
        {opening && <p className="text-sm text-amber-700 dark:text-amber-400">{t("openingNotice")}</p>}
        {openingPosition && <div className="space-y-2"><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={openingCostKnown} disabled={save.isPending} onChange={e=>{setOpeningCostKnown(e.target.checked);setRequestKey(crypto.randomUUID());}} />{t("openingCostKnown")}</label><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={openingAccruedKnown} disabled={save.isPending} onChange={e=>{setOpeningAccruedKnown(e.target.checked);setRequestKey(crypto.randomUUID());}} />{t("openingAccruedKnown")}</label></div>}
        <div className="grid gap-3 sm:grid-cols-2"><label className="text-sm">{t("kind")}<select className={field} value={draft.kind} disabled={save.isPending} onChange={e => {change("kind",e.target.value);if((e.target.value === "opening_cash" || e.target.value === "opening_position") && state?.ledger?.startedAt) {change("occurredAt",localDate(dateOf(state.ledger.startedAt)));change("occurredPrecision","timestamp");}}}>{kinds.map(k => <option key={k} value={k}>{t(`kinds.${k}`)}</option>)}</select></label>{textField("occurredAt", true, draft.occurredPrecision==="date_only"?"date":"datetime-local")}
 {!opening&&<label className="text-sm"><input type="checkbox" checked={draft.occurredPrecision==="date_only"} disabled={save.isPending} onChange={e=>{change("occurredPrecision",e.target.checked?"date_only":"timestamp");change("occurredAt",e.target.checked?draft.occurredAt.slice(0,10):draft.occurredAt+"T00:00:00")}}/> {tt("occurredDateOnly")}</label>}
          {(trade || income || split || principal || openingPosition || distribution) && textField("instrumentKey", true)}{!split && <label className="text-sm">{t("currency")}<select className={field} value={draft.currency} disabled={save.isPending} onChange={e => change("currency", e.target.value)}>{["RUB", "USD", "EUR"].map(c => <option key={c}>{c}</option>)}</select></label>}
          {trade&&<label className="text-sm">{lt("mode")}<select className={field} value={draft.quantityMode} disabled={save.isPending} onChange={e=>change("quantityMode",e.target.value)}><option value="units">{lt("units")}</option><option value="lots">{lt("lots")}</option></select></label>}{(trade || draft.kind === "redemption" || openingPosition) && !lotsMode && textField("quantity", true)}{!(trade&&financialDraft.enabled) && !reconciliation && !split && !distribution && (!openingPosition || openingCostKnown) && textField("amount", true)}{(trade&&!financialDraft.enabled || fx) && textField("fee")}{(trade&&!financialDraft.enabled || (openingPosition && openingAccruedKnown)) && textField("accrued",openingPosition)}{openingPosition && textField("acquiredAt",false,"datetime-local")}{fx && <><label className="text-sm">{t("toCurrency")}<select className={field} value={draft.toCurrency} disabled={save.isPending} onChange={e => change("toCurrency",e.target.value)}>{["RUB","USD","EUR"].map(c => <option key={c}>{c}</option>)}</select></label>{textField("toAmount",true)}</>}{settlement && <>{textField("settlementAt",draft.settlementPrecision==="date_only",draft.settlementPrecision==="date_only"?"date":"datetime-local")}<label className="text-sm"><input type="checkbox" checked={draft.settlementPrecision==="date_only"} disabled={save.isPending} onChange={e=>{change("settlementPrecision",e.target.checked?"date_only":"timestamp");if(draft.settlementAt)change("settlementAt",e.target.checked?draft.settlementAt.slice(0,10):draft.settlementAt+"T00:00:00")}}/> {tt("settlementDateOnly")}</label></>}{principal && textField("nominalBefore",true)}{draft.kind === "amortization" && textField("nominalAfter",true)}{income && textField("tax")}{split && <>{textField("splitNew", true)}{textField("splitOld", true)}</>}
        </div>{trade&&<InvestmentTradeFinancialsForm value={financialDraft} base={draft.currency} disabled={save.isPending} onChange={v=>{setFinancialDraft(v);setRequestKey(crypto.randomUUID());save.reset()}}/>}{lotsMode&&<fieldset className="space-y-3 rounded-lg border p-3" disabled={save.isPending}><legend>{lt("title")}</legend><label className="block text-sm">{lt("count")}<input className={field} required maxLength={30} value={draft.lots} onChange={e=>change("lots",e.target.value)}/></label><p className="text-sm">{lt("policy")}</p><button type="button" className={button} disabled={lotConverter.isPending||!draft.instrumentKey||!draft.lots||!draft.occurredAt} onClick={()=>{setStoredLotTerms(undefined);lotConverter.mutate({signature:lotSignature})}}>{lt("resolve")}</button>{lotConverter.error&&<p role="alert">{lt("error")} {String(lotConverter.error)}</p>}{lotTerms&&<div className="space-y-2 break-all text-sm"><p>{lt("formula",{lots:draft.lots,size:lotTerms.lotSize})}</p><p>{lt("mappingVersion")}: {lotTerms.mappingVersion.toString()} · {lotTerms.provider}/{lotTerms.accessScope}</p><p>{lotTerms.evidence}</p>{convertedLot&&<p>{lt("converted")}: <span className="font-mono">{convertedLot.quantity}</span></p>}</div>}{!lotTerms&&<p>{lt("required")}</p>}</fieldset>}{(draft.occurredPrecision==="date_only" || settlement&&draft.settlementPrecision==="date_only")&&<fieldset className="space-y-2 rounded-lg border p-3"><legend>{tt("precision")}</legend><label className="block text-sm">{tt("timezone")}<input className={field} required maxLength={100} value={draft.timingTimezone} disabled={save.isPending} onChange={e=>change("timingTimezone",e.target.value)}/></label><p className="text-sm">{tt("policy")}</p></fieldset>}{reconciliation&&<fieldset className="space-y-3 rounded-lg border p-3" disabled={save.isPending}><legend>{t("kinds.cash_reconciliation")}</legend><p className="text-sm">{t("reconciliationHelp")}</p>{textField("expectedSettled",true)}{textField("observedSettled",true)}</fieldset>}<p className="text-sm text-gray-500">{t("amountHelp")}{settlement && ` ${t("settlementHelp")}`}{principal && ` ${t("principalHelp")}`}{fx && ` ${t("fxHelp")}`}</p>
        {distribution && <fieldset className="space-y-3 rounded-lg border p-3" disabled={save.isPending}><legend>{t("kinds.distribution")}</legend><p className="text-sm text-gray-500">{t("distributionHelp")}</p><div className="grid gap-3 sm:grid-cols-2">{(["eventKey","targetInstrumentKey","newUnits","oldUnits","costNumerator","costDenominator"] as const).map(key=><label key={key} className="text-sm">{t(`distributionFields.${key}`)}<input className={field} required maxLength={key === "eventKey" || key === "targetInstrumentKey" ? 200 : 30} value={distributionTerms[key]} onChange={e=>{setDistributionTerms(x=>({...x,[key]:e.target.value}));setRequestKey(crypto.randomUUID());save.reset();}}/></label>)}</div><label className="block text-sm"><input type="checkbox" checked={distributionTerms.consumeSource} onChange={e=>{setDistributionTerms(x=>({...x,consumeSource:e.target.checked}));setRequestKey(crypto.randomUUID());save.reset();}}/> {t("distributionFields.consumeSource")}</label><p className="text-sm text-gray-500">{t("distributionMarketHelp",{currency:draft.currency})}</p><div className="grid gap-3 sm:grid-cols-2">{(["marketValue","marketValueEvidence"] as const).map(key=><label key={key} className="text-sm">{t(`distributionFields.${key}`)}<input className={field} maxLength={key==="marketValue"?42:2000} value={distributionTerms[key]} onChange={e=>{setDistributionTerms(x=>({...x,[key]:e.target.value}));setRequestKey(crypto.randomUUID());save.reset();}}/></label>)}</div></fieldset>}
        {(income || draft.kind === "cash_interest") && <label className="block text-sm"><input type="checkbox" checked={taxReviewRequired} disabled={save.isPending} onChange={e=>{setTaxReviewRequired(e.target.checked);setRequestKey(crypto.randomUUID());save.reset();}}/> {t("taxReviewRequired")}<span className="block text-xs text-gray-500">{t("taxReviewHelp")}</span></label>}
        {draft.kind === "coupon" && <fieldset disabled={save.isPending} className="space-y-3 rounded-lg border border-gray-300 p-3 dark:border-gray-700"><legend className="text-sm">{t("couponAllocation")}</legend><p className="text-sm text-gray-500">{t("couponHelp")}</p><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={couponConfirmed} onChange={e => { setCouponConfirmed(e.target.checked); setRequestKey(crypto.randomUUID()); save.reset(); }} />{t("couponConfirmed")}</label>{couponConfirmed && <>
          <datalist id="investment-lot-origins">{state?.positions.find(p => p.instrumentKey === draft.instrumentKey)?.lots.map(l => <option key={l.originId} value={l.originId}>{l.quantity} · {l.acquiredAccrued}</option>)}</datalist>
          {recoveries.map((a,i) => <div key={i} className="grid gap-2 border-t pt-3 sm:grid-cols-2"><label className="text-sm">{t("lotOrigin")}<input className={field} required value={a.originId} list="investment-lot-origins" onChange={e => { setRecoveries(xs => xs.map((v,j) => i === j ? {...v,originId:e.target.value} : v)); setRequestKey(crypto.randomUUID()); }} /></label><label className="text-sm">{t("recoveryAmount")}<input className={field} required={!a.allRemaining} disabled={a.allRemaining} value={a.amount} onChange={e => { setRecoveries(xs => xs.map((v,j) => i === j ? {...v,amount:e.target.value} : v)); setRequestKey(crypto.randomUUID()); }} /></label><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={a.allRemaining} onChange={e => { setRecoveries(xs => xs.map((v,j) => i === j ? {...v,allRemaining:e.target.checked} : v)); setRequestKey(crypto.randomUUID()); }} />{t("recoverAll")}</label><button type="button" onClick={() => { setRecoveries(xs => xs.filter((_,j) => i !== j)); setRequestKey(crypto.randomUUID()); }}>{t("removeRecovery")}</button></div>)}
          <button type="button" className="text-blue-600" onClick={() => { setRecoveries(xs => [...xs,{originId:"",amount:"",allRemaining:false}]); setRequestKey(crypto.randomUUID()); }}>{t("addRecovery")}</button>
        </>}</fieldset>}
        <label className="block text-sm">{t("note")}<textarea className={field} required={opening||reconciliation} maxLength={5000} value={draft.note} disabled={save.isPending} onChange={e => change("note", e.target.value)} /></label>
        {editing && <label className="block text-sm">{t("reason")}<textarea className={field} maxLength={2000} required value={reason} disabled={save.isPending} onChange={e => { setReason(e.target.value); setRequestKey(crypto.randomUUID()); }} /></label>}
        {!(queueMode&&sourceID&&!editing)&&<div className="space-y-3"><button type="button" className={button} disabled={save.isPending||preview.isPending||(lotsMode&&!lotTerms)} onClick={()=>preview.mutate({signature:formSignature})}>{pt("check")}</button>{preview.error&&<p role="alert">{pt("error")} {String(preview.error)}</p>}{preview.data&&preview.variables?.signature===formSignature&&<InvestmentMutationPreview value={preview.data}/>} {!reviewed&&<p className="text-sm">{pt("required")}</p>}</div>}
        {save.error && <p role="alert" className="text-red-600">{String(save.error)}</p>}<div className="flex justify-end gap-3"><button type="button" disabled={save.isPending} onClick={() => setOpen(false)}>{t("cancel")}</button><button className={button} disabled={save.isPending || (!(queueMode&&sourceID&&!editing)&&!reviewed) || (queueMode && batchLocked) || !expectedVersion || (!!sourceID && (!reviewedRow || !!reviewedRow.operationId || source.isFetching || !reviewReason.trim()))}>{queueMode&&sourceID&&!editing?t("batchAdd"):t("save")}</button></div>
      </form>
    </Modal>
    <Modal open={!!voiding} title={t("void")} onClose={() => { if (!cancel.isPending) setVoiding(null); }}><form className="space-y-4 p-5" onSubmit={e => { e.preventDefault(); cancel.mutate(); }}><p>{t("voidHelp")}</p><label className="block text-sm">{t("reason")}<textarea className={field} required maxLength={2000} value={reason} disabled={cancel.isPending} onChange={e => setReason(e.target.value)} /></label><button type="button" className={button} disabled={cancel.isPending||voidPreview.isPending||!reason.trim()} onClick={()=>voidPreview.mutate({signature:voidSignature})}>{pt("check")}</button>{voidPreview.error&&<p role="alert">{pt("error")} {String(voidPreview.error)}</p>}{voidPreview.data&&voidPreview.variables?.signature===voidSignature&&<InvestmentMutationPreview value={voidPreview.data}/>}
{cancel.error && <p role="alert" className="text-red-600">{String(cancel.error)}</p>}<button className={button} disabled={cancel.isPending || !reason.trim() || !voidReviewed}>{t("void")}</button></form></Modal>
  </main>;
}
export default function InvestmentJournalPage() { const tenantId = useAssetTenant(); return tenantId ? <Journal key={tenantId} tenantId={tenantId} /> : null; }
