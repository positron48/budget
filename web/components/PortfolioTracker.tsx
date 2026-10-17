"use client";
import {useDisplayCurrency} from "@/lib/display-currency";

import {Code, ConnectError} from "@connectrpc/connect";
import {useCallback, useMemo, useRef, useState} from "react";
import Link from "next/link";
import {useRouter} from "next/navigation";
import {useLocale} from "next-intl";
import {useQuery, useQueryClient} from "@tanstack/react-query";
import {Plus, Upload, Wallet, ArrowUpRight, RefreshCw, PieChart} from "lucide-react";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import {instrumentHref} from "@/lib/portfolio-instrument";
import {usePortfolioFilters} from "@/lib/portfolio-filters";
import {groupPredicate,portfolioGroupsKey} from "@/lib/portfolio-groups";
import PortfolioCategories from "@/components/PortfolioCategories";

import PortfolioHistory from "@/components/PortfolioHistory";
import PortfolioAccountSelection from "@/components/PortfolioAccountSelection";
import {accountSelection,selectedPortfolioAccounts} from "@/lib/portfolio-accounts";
import PortfolioDayResult from "@/components/PortfolioDayResult";
import PortfolioFX from "@/components/PortfolioFX";
import Modal from "@/components/Modal";
import {allOperations, loadBrokerCatalog, portfolioInstrumentKey} from "@/lib/portfolio-import";
import {add, decimal, divide, payoutKinds, trackerError, trackerKinds, tradeAmount, zero} from "@/lib/portfolio-tracker";
import {portfolioQuotes} from "@/lib/portfolio-quotes";
import {portfolioReport, reportingDate} from "@/lib/portfolio-report";
import type {GetInvestmentGroupsResponse, GetInvestmentExchangeRatesResponse, GetInvestmentQuotesResponse, InvestmentValuationProfileOption} from "@/proto/budget/v1/investment_nav_pb";
import {positionDecimal, type Fraction} from "@/lib/investment-positions";
import type {InvestmentLedger, InvestmentOperation, InvestmentSnapshot} from "@/proto/budget/v1/investment_pb";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";

const panel = "rounded-2xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-[#30313d]";
const input = "mt-1 w-full rounded-lg border border-slate-300 bg-transparent px-3 py-2 dark:border-slate-700";
const secondary = "inline-flex items-center justify-center gap-2 rounded-lg border border-slate-200 px-3 py-2 text-sm hover:bg-slate-50 disabled:opacity-40 dark:border-slate-700 dark:hover:bg-slate-800";
const primary = "inline-flex items-center justify-center gap-2 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-40";

function Tracker({tenantId,analyticsOnly=false}: {tenantId: string;analyticsOnly?:boolean}) {
  const locale = useLocale(), ru = locale === "ru", {investment: api, investmentImport: imports, investmentNAV: nav, tenant} = useClients(), qc = useQueryClient();
  const readOnly = useInvestmentReadOnly(), router = useRouter();
  const writeKey = useRef(crypto.randomUUID()), pendingWrite = useRef<{signature: string; request: Record<string, unknown>} | null>(null);
  const tr = useCallback((r: string, e: string) => ru ? r : e,[ru]);
  const {params,update}=usePortfolioFilters();
  const accountScope=accountSelection(params),account=accountScope.ids.length===1?accountScope.ids[0]:"",currency=useDisplayCurrency(tenantId).currency;
  const fxMode="market",view=!analyticsOnly&&["trades","payments"].includes(params.get("view")||"")?params.get("view")!:"overview",closed=params.get("closed")==="1";
  const setAccount=(value:string)=>update({account:value,scope:"",selection:""});
  const groupKey=JSON.stringify([...new Set(params.getAll("group"))].sort()),instrumentScopeKey=JSON.stringify([...new Set(params.getAll("instrument"))].sort());
  const groupIDs=useMemo<string[]>(()=>JSON.parse(groupKey),[groupKey]);
  const instrumentKeys=useMemo<string[]>(()=>JSON.parse(instrumentScopeKey),[instrumentScopeKey]);
  const groups=useQuery<GetInvestmentGroupsResponse>({queryKey:portfolioGroupsKey(tenantId),queryFn:()=>nav.getInvestmentGroups({})});
  const include=useMemo(()=>{const group=groupPredicate(groups.data?.classification,groupIDs);return(id:string,key:string)=>group(id,key)&&(!instrumentKeys.length||instrumentKeys.includes(key));},[groups.data,groupIDs,instrumentKeys]);
  const invalidGroup=!!groups.data&&groupIDs.some(id=>id!==""&&!groups.data?.classification?.groups.some(g=>g.id===id));
  const groupReady=!groupIDs.length||!!groups.data&&!invalidGroup;
  const [dialog, setDialog] = useState<"operation" | "account" | null>(null), [editing, setEditing] = useState<InvestmentOperation | null>(null);
  const [draft, setDraft] = useState({ledger: "", kind: "buy", instrument: "", exchange: "MCX", quantity: "1", price: "", amount: "", fee: "0", feeCurrency: "", accrued: "0", currency: "RUB", date: new Date().toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"}), note: ""});
  const [newAccount, setNewAccount] = useState({name: "", broker: "", type: "regular"}), [busy, setBusy] = useState(false), [writeError, setWriteError] = useState("");
  const ledgers = useQuery<InvestmentLedger[]>({queryKey: ["investment", tenantId, "tracker", "accounts",accountScope.explicit], queryFn: async () => {
    const all: InvestmentLedger[] = [];
    for (let offset = 0; ; offset += 200) { const page = await api.listInvestmentLedgers({limit: 200, offset,includeArchived:accountScope.explicit}); all.push(...page.ledgers); if (page.ledgers.length < 200) return all; }
  }});
  const roles = useQuery<{memberships: TenantMembership[]}>({queryKey: ["investment", tenantId, "tracker", "roles"], queryFn: () => tenant.listMyTenants({})});
  const canWrite = !readOnly && roles.data?.memberships.some(m => m.tenant?.id === tenantId && [1, 2].includes(m.role));
  const selection=selectedPortfolioAccounts(params,ledgers.data||[]);
  const invalidAccount=!!ledgers.data&&selection.invalid,emptySelection=selection.empty;
  const selected = selection.ledgers;
  const data = useQuery<{snapshots: InvestmentSnapshot[]; operations: InvestmentOperation[]}>({queryKey: ["investment", tenantId, "tracker", "data", selected.map(l => l.id)], enabled: !!ledgers.data&&!invalidAccount&&!emptySelection, queryFn: async () => {
    const snapshots: InvestmentSnapshot[] = [], operations: InvestmentOperation[] = [];
    for (const ledger of selected) {
      const state = await api.getInvestmentSnapshot({ledgerId: ledger.id});
      if (!state.snapshot) throw new Error("snapshot_missing");
      snapshots.push(state.snapshot); operations.push(...await allOperations(api, ledger.id));
    }
    return {snapshots, operations};
  }});
  const catalog = useQuery({queryKey: ["investment", tenantId, "tracker", "broker-quotes"], queryFn: () => loadBrokerCatalog(imports), staleTime: 60000});
  const operations = (data.data?.operations || []).filter(o=>include(o.ledgerId,o.instrumentKey));
  const profiles = useQuery<InvestmentValuationProfileOption[]>({queryKey: ["investment", tenantId, "tracker", "fx-profiles"], queryFn: async () => {
    const all: InvestmentValuationProfileOption[] = []; let afterFxKey = "";
    do { const page = await nav.listInvestmentValuationProfiles({afterFxKey}); all.push(...page.fxProfiles); afterFxKey = page.nextFxKey; } while (afterFxKey);
    return all;
  }});
  const policy = profiles.data?.find(p => p.mode === fxMode);
  const dates = useMemo(() => {
    const days = new Set<string>();
    const now = BigInt(Math.floor(Date.now() / 1000));
    const today = reportingDate(now);
    for (const op of data.data?.operations || []) {
      if (op.status !== "posted" || !op.occurredAt || op.occurredAt.seconds > now) continue;
      days.add(reportingDate(op.occurredAt.seconds));
      if (op.acquiredAt) days.add(reportingDate(op.acquiredAt.seconds));
      for (const money of [op.tradeFinancials?.principal, op.tradeFinancials?.accrued, op.tradeFinancials?.fee]) if (money?.at) days.add(reportingDate(money.at.seconds));
    }
    for (const s of data.data?.snapshots || []) for (const p of s.positions) for (const lot of p.lots) if (lot.acquiredAt) days.add(reportingDate(lot.acquiredAt.seconds));
    return [...days].filter(date => date <= today).sort();
  }, [data.data]);
  const rates = useQuery<GetInvestmentExchangeRatesResponse>({queryKey: ["investment", tenantId, "tracker", "fx", policy?.key, policy?.version, currency, dates], enabled: !!policy && !!data.data, staleTime: 60000, queryFn: async () => {
    const response: GetInvestmentExchangeRatesResponse = await nav.getInvestmentExchangeRates({fxPolicyKey: policy!.key, currency, fromCurrencies: ["RUB", "USD", "EUR"], dates});
    if (response.fxPolicyKey !== policy!.key || response.policyVersion !== policy!.version || response.mode !== fxMode) throw new Error("fx_policy_changed");
    return response;
  }});
  const quoteKeys = useMemo(() => [...new Set([...(data.data?.snapshots || []).flatMap(s => s.positions.filter(p => {const q = decimal(p.quantity); return !!q && BigInt(q.numerator) > 0n;}).map(p => p.instrumentKey)),...(closed?(data.data?.operations||[]).map(o=>o.instrumentKey).filter(Boolean):[])])].sort(), [data.data,closed]);
  const marketQuotes = useQuery<GetInvestmentQuotesResponse>({queryKey: ["investment", tenantId, "tracker", "market-quotes", quoteKeys], enabled: !!data.data && !!quoteKeys.length, staleTime: 30000, refetchInterval: 60000, queryFn: async () => {
    const result: GetInvestmentQuotesResponse = {
      $typeName: "budget.v1.GetInvestmentQuotesResponse", quotes: [],
    };
    for (let offset = 0; offset < quoteKeys.length; offset += 500) {
      const page: GetInvestmentQuotesResponse = await nav.getInvestmentQuotes({instrumentKeys: quoteKeys.slice(offset, offset + 500)});
      result.quotes.push(...page.quotes); result.readAt = page.readAt;
    }
    return result;
  }});
  const quotes = useMemo(() => portfolioQuotes(catalog.data?.quotes || new Map(), marketQuotes.data?.quotes || []), [catalog.data, marketQuotes.data]);
  // Absence is meaningful only after all first-load inputs have settled.
  // A background refresh keeps the last complete inputs visible.
  const valuationLoading = profiles.isPending || catalog.isPending ||
    (!!policy && !!data.data && rates.isPending) ||
    (!!data.data && quoteKeys.length > 0 && marketQuotes.isPending);
  const valuationUnavailable = !profiles.isPending && !policy;
  const valuationReady = !valuationLoading;
  const report = useMemo(() => portfolioReport(data.data?.snapshots || [], data.data?.operations || [], quotes, currency, rates.data,include), [data.data, quotes, currency, rates.data,include]);
  const positions = report.positions;
  const current = positions, held = current.filter(p => BigInt(p.quantity.numerator) !== 0n);
  const sourceHistoryIncomplete = (data.data?.snapshots || []).some(s => s.issues.includes("source_trades_pending"));
  const valueKnown = held.every(p => p.value), costKnown = held.every(p => p.cost), profitKnown = !sourceHistoryIncomplete && current.every(p => p.profit);
  const hasQuotes = held.some(p => p.value);
  const value = valueKnown || hasQuotes ? add(...held.map(p => p.value)) : undefined, cost = costKnown ? add(...held.map(p => p.cost)) : undefined;
  const profit = profitKnown || current.some(p => p.profit) ? add(...current.map(p => p.profit)) : undefined;
  const money = (amount?: Fraction, cur = currency) => amount ? `${new Intl.NumberFormat(locale, {maximumFractionDigits: 2}).format(Number(positionDecimal(amount)))} ${cur}` : "—";
  const quoteDetails=useMemo(()=>new Map((marketQuotes.data?.quotes||[]).map(q=>[q.instrumentKey,q])),[marketQuotes.data]);
  const name = (key: string) => key ? quoteDetails.get(key)?.instrument?.ticker || catalog.data?.names.get(key)?.ticker || key.replace(/^tinvest:/, "") : tr("Прочие выплаты", "Other payouts");
  const categoryRows=useMemo(()=>{
    if(groupIDs.length||instrumentKeys.length)return current.filter(p=>BigInt(p.quantity.numerator)!==0n).map(p=>({id:p.key,name:quoteDetails.get(p.key)?.instrument?.ticker||catalog.data?.names.get(p.key)?.ticker||p.key,count:1,quantity:positionDecimal(p.quantity),estimateDate:p.quoteSource==="estimate"&&p.quoteAt?new Date(p.quoteAt).toLocaleDateString(locale):undefined,value:p.value,cost:p.cost,profit:p.profit,partial:!p.value}));
    if(!groups.data?.classification)return [];
    return [...groups.data.classification.groups,{id:"",name:tr("Без категории","Uncategorized")}].flatMap(g=>{
      const sub=portfolioReport(data.data?.snapshots||[],data.data?.operations||[],quotes,currency,rates.data,groupPredicate(groups.data?.classification,[g.id]));
      if(!sub.positions.length)return [];
      const open=sub.positions.filter(p=>BigInt(p.quantity.numerator)!==0n);
      if(!open.length)return [];
      const estimateDates=open.filter(p=>p.quoteSource==="estimate"&&p.quoteAt).map(p=>p.quoteAt!).sort((a,b)=>a-b);
      return [{id:g.id,name:g.name,count:open.length,estimateDate:estimateDates.length?new Date(estimateDates[0]).toLocaleDateString(locale):undefined,value:open.some(p=>p.value)||!open.length?add(...open.map(p=>p.value)):undefined,cost:open.every(p=>p.cost)?add(...open.map(p=>p.cost)):undefined,profit:sub.positions.every(p=>p.profit)?add(...sub.positions.map(p=>p.profit)):undefined,partial:open.some(p=>!p.value)}];
    });
  },[groupIDs,instrumentKeys,current,quoteDetails,catalog.data,groups.data,data.data,quotes,currency,rates.data,sourceHistoryIncomplete,tr,locale]);
  const categoryTitle=groupIDs.length===1?(groups.data?.classification?.groups.find(g=>g.id===groupIDs[0])?.name||tr("Без категории","Uncategorized")):tr("Портфель","Portfolio");
  const visibleOperations = operations.filter(op => op.status === "posted" && (view === "payments" ? payoutKinds.has(op.kind) : ["buy", "sell"].includes(op.kind))).sort((a, b) => Number((b.occurredAt?.seconds || 0n) - (a.occurredAt?.seconds || 0n)));
  const pageSize=[25,50,100].includes(Number(params.get("page_size")))?Number(params.get("page_size")):50;
  const pageCount=Math.max(1,Math.ceil(visibleOperations.length/pageSize));
  const requestedPage=Number(params.get("page"));
  const operationPage=Math.min(pageCount,Number.isSafeInteger(requestedPage)&&requestedPage>0?requestedPage:1);
  const pageOperations=visibleOperations.slice((operationPage-1)*pageSize,operationPage*pageSize);
  const kind = (k: string) => ({buy: tr("Покупка", "Buy"), sell: tr("Продажа", "Sell"), dividend: tr("Дивиденды", "Dividend"), coupon: tr("Купон", "Coupon"), repo_income: tr("Доход РЕПО", "Repo income"), cash_interest: tr("Проценты", "Interest")}[k] || k);
  const refresh = async () => { await qc.invalidateQueries({queryKey: ["investment", tenantId]}); await qc.invalidateQueries({queryKey: ["investment-import", tenantId]}); };
  const openOperation = (op?: InvestmentOperation) => {
    setEditing(op || null); setWriteError(""); writeKey.current = crypto.randomUUID(); pendingWrite.current = null;
    setDraft({ledger: op?.ledgerId || (selected.length===1?selected[0].id:""), kind: op?.kind || "buy", instrument: op ? catalog.data?.names.get(op.instrumentKey)?.ticker || op.instrumentKey : "", exchange: op?.instrumentKey.includes("@") ? op.instrumentKey.split("@").at(-1)! : "MCX", quantity: op?.quantity || "1", price: op && ["buy", "sell"].includes(op.kind) ? positionDecimal(divide(decimal(op.amount) || zero, decimal(op.quantity) || zero), 28) : "", amount: op?.amount || "", fee: op?.fee || "0", feeCurrency: op?.feeCurrency || "", accrued: op?.accrued || "0", currency: op?.currency || "RUB", date: op?.occurredAt ? new Date(Number(op.occurredAt.seconds) * 1000).toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"}) : new Date().toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"}), note: op?.note || ""});
    setDialog("operation");
  };
  const save = async () => {
    setBusy(true); setWriteError("");
    try {
      if (dialog === "account") {
        await api.createInvestmentLedger({name: newAccount.name, broker: newAccount.broker, accountType: newAccount.type, defaultCurrency: currency, requestKey: writeKey.current});
      } else {
        const signature = JSON.stringify([editing?.id, draft]);
        if (pendingWrite.current) {
          if (pendingWrite.current.signature !== signature) throw new Error("pending_write_changed");
          await api.saveInvestmentOperation(pendingWrite.current.request);
          pendingWrite.current = null; setDialog(null); await refresh(); return;
        }
        const snapshot = data.data?.snapshots.find(s => s.ledger?.id === draft.ledger);
        if (!snapshot?.ledger) throw new Error("snapshot_missing");
        const trade = ["buy", "sell"].includes(draft.kind), normalized = draft.instrument.trim();
        const originalLabel = editing ? catalog.data?.names.get(editing.instrumentKey)?.ticker || editing.instrumentKey : "";
        const instrumentKey = editing && normalized === originalLabel ? editing.instrumentKey : portfolioInstrumentKey(catalog.data, normalized, draft.exchange);
        const originalPrice = editing && trade ? positionDecimal(divide(decimal(editing.amount) || zero, decimal(editing.quantity) || zero), 28) : "";
        const amount = trade ? editing && draft.quantity === editing.quantity && draft.price === originalPrice ? editing.amount : tradeAmount(draft.quantity, draft.price) : draft.amount.replace(",", ".");
        const sameDate = !!editing?.occurredAt && draft.date === new Date(Number(editing.occurredAt.seconds) * 1000).toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"});
        const sameMoney = !!editing && editing.kind === draft.kind && editing.instrumentKey === instrumentKey && amount === editing.amount && draft.quantity === editing.quantity && draft.currency === editing.currency && draft.fee === editing.fee && draft.feeCurrency === editing.feeCurrency && draft.accrued === editing.accrued;
        const operation = {...editing, id: editing?.id || "", ledgerId: draft.ledger, kind: draft.kind, instrumentKey, quantity: trade ? draft.quantity.replace(",", ".") : "0", amount, fee: trade ? draft.fee.replace(",", ".") : "0", feeCurrency: trade ? draft.feeCurrency : "", accrued: trade ? draft.accrued.replace(",", ".") : "0", tax: editing && amount === editing.amount ? editing.tax : "0", currency: draft.currency, occurredAt: sameDate ? editing?.occurredAt : undefined, settlementAt: sameDate ? editing?.settlementAt : undefined, timing: sameDate ? editing?.timing : {timezone: "Europe/Moscow", occurredDate: draft.date}, tradeFinancials: sameMoney && sameDate ? editing?.tradeFinancials : undefined, lotConversion: sameMoney && sameDate ? editing?.lotConversion : undefined, note: draft.note};
        const request = {operation, action: editing ? "amend" : "create", expectedLedgerVersion: snapshot.ledger.version, expectedOperationVersion: editing?.version || 0n, requestKey: writeKey.current, reason: editing ? "User edited portfolio operation" : ""};
        const preview = await api.previewInvestmentOperation(request);
        if (!preview.valid) throw new Error(preview.issues.some((i: {code: string}) => /position|short/.test(i.code)) ? "short sale" : "invalid_operation");
        pendingWrite.current = {signature, request: {...request, previewHash: preview.previewHash}};
        await api.saveInvestmentOperation(pendingWrite.current.request); pendingWrite.current = null;
      }
      setDialog(null); await refresh();
    } catch (error) {
      if ([Code.InvalidArgument, Code.FailedPrecondition, Code.PermissionDenied].includes(ConnectError.from(error).code)) { pendingWrite.current = null; writeKey.current = crypto.randomUUID(); await refresh(); }
      setWriteError(trackerError(error, ru));
    } finally { setBusy(false); }
  };
  const availableInstruments=[...new Set([...(data.data?.operations||[]).filter(o=>o.status==="posted").flatMap(o=>[o.instrumentKey,o.distribution?.targetInstrumentKey||""]),...(data.data?.snapshots||[]).flatMap(s=>s.positions.map(p=>p.instrumentKey))].filter(Boolean))].sort();
  const invalidInstrument=!emptySelection&&!!data.data&&instrumentKeys.some(key=>!availableInstruments.includes(key));
  const error = ledgers.error || data.error || roles.error || (!analyticsOnly||groupIDs.length?groups.error:undefined);
  return <div className="min-h-screen bg-slate-50/70 text-slate-800 dark:bg-[#272832] dark:text-slate-100">
    <div className="mx-auto grid max-w-[1480px] gap-6 px-4 py-7 lg:grid-cols-[210px_minmax(0,1fr)] lg:px-7">
      <aside className="min-w-0 space-y-5 lg:pt-2"><div className="flex items-center gap-2 text-lg font-semibold"><Wallet size={20} className="text-emerald-600"/>{tr("Мои портфели", "My portfolios")}</div>
        <div className="flex gap-2 overflow-x-auto lg:flex-col"><button onClick={() => setAccount("")} className={`whitespace-nowrap rounded-xl px-3 py-3 text-left text-sm ${!accountScope.explicit ? "bg-emerald-50 font-semibold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300" : "hover:bg-slate-100 dark:hover:bg-slate-800"}`}>{tr("Все счета", "All accounts")}</button>{ledgers.data?.map(l => <button key={l.id} onClick={() => setAccount(l.id)} className={`flex items-center justify-between gap-3 whitespace-nowrap rounded-xl px-3 py-3 text-left text-sm ${accountScope.ids.length===1&&account === l.id ? "bg-emerald-50 font-semibold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300" : "hover:bg-slate-100 dark:hover:bg-slate-800"}`}><span>{l.name}</span>{l.accountType === "iis" && <span className="text-[10px] text-slate-400">{tr("ИИС", "IIS")}</span>}</button>)}</div>
        <PortfolioAccountSelection tenantId={tenantId} accounts={ledgers.data||[]} ru={ru} canSave={!readOnly&&!!roles.data?.memberships.some(m=>m.tenant?.id===tenantId&&[1,2,3].includes(m.role))}/>
        {canWrite && !analyticsOnly && <button className={secondary + " w-full"} onClick={() => {setNewAccount({name: "", broker: "", type: "regular"}); setWriteError(""); writeKey.current = crypto.randomUUID(); setDialog("account");}}><Plus size={15}/>{tr("Добавить счёт", "Add account")}</button>}

      </aside>
      <div className="min-w-0 space-y-6">
        <header className="flex flex-wrap items-center justify-between gap-4"><div><p className="text-sm text-slate-500">{tr("Инвестиции", "Investments")}</p><h1 className="mt-1 text-3xl font-semibold tracking-tight">{analyticsOnly?tr("Рост","Growth"):accountScope.explicit ? selected.length===1?selected[0]?.name:tr(`Выбранные счета: ${selected.length}`,`Selected accounts: ${selected.length}`) : tr("Мой портфель", "My portfolio")}</h1></div><div className="flex flex-wrap gap-2"><button className={secondary} disabled={data.isFetching} onClick={() => void refresh()} aria-label={tr("Обновить портфель", "Refresh portfolio")}><RefreshCw size={16} className={data.isFetching ? "animate-spin" : ""}/></button>{canWrite && !analyticsOnly && <><Link href="/investments/import" className={secondary}><Upload size={15}/>{tr("Импорт", "Import")}</Link><button className={primary} disabled={!selected.length || !data.data} onClick={() => openOperation()}><Plus size={16}/>{tr("Добавить операцию", "Add operation")}</button></>}</div></header>
        {analyticsOnly&&<label className="flex items-center gap-3 text-sm">{tr("Категория","Category")}<select className={secondary} aria-label={tr("Категория","Category")} value={groupIDs.length>1?"__multiple":groupIDs.length===1?groupIDs[0]:"__all"} onChange={e=>update({group:e.target.value==="__all"?[]:[e.target.value]})}><option value="__all">{tr("Все категории","All categories")}</option>{groupIDs.length>1&&<option value="__multiple" disabled>{tr("Выбрано категорий","Selected categories")}: {groupIDs.length}</option>}{[...(groups.data?.classification?.groups||[]),{id:"",name:tr("Без категории","Uncategorized")}].map(g=><option key={g.id} value={g.id}>{g.name}</option>)}</select></label>}
        {analyticsOnly&&<label className="flex items-center gap-2 text-sm">{tr("Бумага","Security")}<select className={secondary+" max-w-full"} aria-label={tr("Фильтр бумаги","Security filter")} value={instrumentKeys.length>1?"__multiple":instrumentKeys[0]||""} onChange={e=>update({instrument:e.target.value?[e.target.value]:[]})}><option value="">{tr("Все бумаги","All securities")}</option>{instrumentKeys.length>1&&<option value="__multiple" disabled>{tr("Выбрано бумаг","Securities selected")}: {instrumentKeys.length}</option>}{instrumentKeys.filter(key=>!availableInstruments.includes(key)).map(key=><option disabled value={key} key={key}>{name(key)} · {tr("недоступна","unavailable")}</option>)}{availableInstruments.map(key=><option key={key} value={key}>{name(key)}</option>)}</select></label>}
        {invalidInstrument&&<p role="alert">{tr("Выбранная бумага недоступна на этих счетах. Измените фильтр бумаги.","The selected security is unavailable in these accounts. Change the security filter.")}</p>}
        {invalidAccount&&<p role="alert">{tr("Выбранный счёт недоступен. Выберите другой счёт.","The selected account is unavailable. Choose another account.")}</p>}
        {invalidGroup&&<p role="alert">{tr("Выбранная группа удалена. Выберите другую группу или сбросьте фильтр.","A selected group was deleted. Choose another group or clear the filter.")}</p>}

        {error && <p role="alert" className="rounded-xl bg-rose-50 p-4 text-sm text-rose-800 dark:bg-rose-950 dark:text-rose-200">{tr("Не удалось загрузить портфель. Попробуйте обновить страницу.", "Could not load the portfolio. Try refreshing the page.")}</p>}
        {(!emptySelection&&!invalidAccount&&(data.isPending || valuationLoading)) && <p role="status" className="py-8 text-slate-500">{tr("Загружаем портфель…", "Loading portfolio…")}</p>}
        {valuationUnavailable && <p role="alert">{tr("Для этого режима источник валютных курсов недоступен. Суммы в других валютах могут быть неизвестны.", "The exchange rate source is unavailable for this mode. Foreign-currency amounts may be unknown.")}</p>}
        {emptySelection&&<p role="status">{tr("Выберите хотя бы один счёт.","Select at least one account.")}</p>}
        {data.data && !emptySelection && (valuationReady || view !== "overview") && groupReady && !invalidAccount && !invalidInstrument && <>

          {!analyticsOnly&&valuationReady&&<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">{[
            {title:tr("Стоимость","Value"),amount:value,icon:Wallet,partial:!valueKnown},
            {title:tr("Вложено","Invested"),amount:cost,icon:PieChart,partial:!costKnown},
            {title:tr("Прибыль","Profit"),amount:profit,icon:ArrowUpRight,partial:!profitKnown},
          ].map(card=><section className={panel+" p-5"} key={card.title}><div className="flex items-center justify-between text-sm text-slate-500 dark:text-slate-400"><h2>{card.title}</h2><card.icon size={17}/></div><p className={"mt-4 text-2xl font-semibold tabular-nums "+(card.title===tr("Прибыль","Profit")&&card.amount?(BigInt(card.amount.numerator)>=0n?"text-emerald-500":"text-rose-500"):"")}>{money(card.amount)}</p>{card.partial&&<span className="mt-2 inline-block text-xs text-amber-600" title={tr("Не все исходные данные доступны","Some source data is unavailable")}>{tr("Неполные данные","Incomplete data")}</span>}</section>)}
          {policy&&<PortfolioDayResult tenantId={tenantId} ledgers={selected} currency={currency} policyKey={policy.key} policyVersion={policy.version} fxMode={fxMode} groupIds={groupIDs} instrumentKeys={instrumentKeys} classificationVersion={groups.data?.classification?.version||0n} ru={ru}/>}
          </div>}
          {!analyticsOnly&&valuationReady&&(report.missingFX.length > 0 || rates.error) && <p role="status" className="text-sm text-amber-700 dark:text-amber-400">{tr("Для части сумм нет валютного курса. Они отмечены как неизвестные; итог может быть неполным.", "Exchange rates are unavailable for some amounts. They remain unknown; totals may be incomplete.")}</p>}
          {view === "overview" ? <>
            {analyticsOnly && policy && selected.length > 0 && selected.every(l => l.portfolioTracking) && <PortfolioHistory instrumentKeys={instrumentKeys} instrumentNames={new Map([...(catalog.data?.names||[])].map(([key,value])=>[key,value.ticker||value.name]))} historyStart={operations.filter(o=>o.status==="posted"&&o.occurredAt).map(o=>new Date(Number(o.occurredAt!.seconds)*1000).toLocaleDateString("sv-SE",{timeZone:"Europe/Moscow"})).sort()[0]} groupIds={groupIDs} classificationVersion={groupIDs.length?groups.data?.classification?.version||0n:0n} tenantId={tenantId} ledgers={selected} currency={currency} policyKey={policy.key} policyVersion={policy.version} fxMode={policy.mode}/>}
            {!analyticsOnly&&<><div className="flex items-center justify-between gap-3">{(groupIDs.length>0||instrumentKeys.length>0)&&<button className="text-sm text-sky-500" onClick={()=>update({group:[],instrument:[]})}><span aria-hidden="true">← </span>{tr("Все категории","All categories")}</button>}</div><PortfolioCategories rows={categoryRows} currency={currency} ru={ru} title={categoryTitle} onSelect={id=>{if(groupIDs.length||instrumentKeys.length)router.push(instrumentHref(id,params.toString()));else update({group:[id],instrument:[]});}}/><PortfolioFX tenantId={tenantId} policy={policy} ru={ru}/></>}
          </> : <section className={panel + " overflow-hidden"}><div className="overflow-x-auto"><table className="w-full whitespace-nowrap text-left text-sm"><thead className="bg-slate-50 text-xs text-slate-400 dark:bg-slate-900"><tr>{[tr("Дата", "Date"), tr("Бумага", "Security"), tr("Операция", "Operation"), tr("Счёт", "Account"), tr("Количество", "Quantity"), tr("Сумма", "Amount"), ""].map((x, i) => <th className="px-5 py-4 font-medium" key={i}>{x}</th>)}</tr></thead><tbody className="divide-y divide-slate-100 dark:divide-slate-800">{pageOperations.map(op => <tr key={op.id}><td className="px-5 py-4 text-slate-500">{op.occurredAt && new Date(Number(op.occurredAt.seconds) * 1000).toLocaleDateString(locale, {timeZone: "Europe/Moscow"})}</td><td className="px-5 py-4 font-medium"><Link className="text-sky-600 hover:underline dark:text-sky-400" href={instrumentHref(op.instrumentKey,params.toString())}>{name(op.instrumentKey)}</Link></td><td className={"px-5 py-4 "+(op.kind==="buy"?"text-emerald-600 dark:text-emerald-400":op.kind==="sell"?"text-rose-600 dark:text-rose-400":"")}>{kind(op.kind)}</td><td className="px-5 py-4 text-slate-500">{ledgers.data?.find(l => l.id === op.ledgerId)?.name}</td><td className="px-5 py-4 tabular-nums">{["buy", "sell"].includes(op.kind) ? (op.kind==="buy"?"+":"−")+op.quantity : "—"}</td><td className="px-5 py-4 tabular-nums">{money(decimal(op.amount), op.currency)}</td><td className="px-5 py-4">{canWrite && trackerKinds.has(op.kind) && <button className="text-xs text-emerald-700 dark:text-emerald-400" onClick={() => openOperation(op)}>{tr("Изменить", "Edit")}</button>}</td></tr>)}</tbody></table></div><div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 px-5 py-4 text-sm dark:border-slate-700"><span>{visibleOperations.length ? `${(operationPage-1)*pageSize+1}–${Math.min(operationPage*pageSize,visibleOperations.length)} / ${visibleOperations.length}` : "0"}</span><div className="flex items-center gap-3"><select className={secondary} aria-label={tr("Операций на странице","Operations per page")} value={pageSize} onChange={e=>update({page_size:e.target.value,page:""})}>{[25,50,100].map(n=><option key={n} value={n}>{n}</option>)}</select><button className={secondary} disabled={operationPage<=1} onClick={()=>update({page:String(operationPage-1)})}>{tr("Назад","Previous")}</button><span>{operationPage} / {pageCount}</span><button className={secondary} disabled={operationPage>=pageCount} onClick={()=>update({page:String(operationPage+1)})}>{tr("Далее","Next")}</button></div></div>{!visibleOperations.length && <p className="p-8 text-center text-sm text-slate-400">{tr("Операций пока нет", "No operations yet")}</p>}</section>}
        </>}
      </div>
    </div>
    <Modal open={!!dialog} title={dialog === "account" ? tr("Новый счёт", "New account") : editing ? tr("Изменить операцию", "Edit operation") : tr("Новая операция", "New operation")} onClose={() => {if (!busy) setDialog(null);}}><form className="space-y-4" onSubmit={e => {e.preventDefault(); void save();}}><fieldset disabled={busy} className="space-y-4">
      {dialog === "account" ? <><label className="block text-sm">{tr("Название", "Name")}<input className={input} required maxLength={200} value={newAccount.name} onChange={e => setNewAccount({...newAccount, name: e.target.value})}/></label><label className="block text-sm">{tr("Брокер", "Broker")}<input className={input} maxLength={200} value={newAccount.broker} onChange={e => setNewAccount({...newAccount, broker: e.target.value})}/></label><label className="block text-sm">{tr("Тип счёта", "Account type")}<select className={input} value={newAccount.type} onChange={e => setNewAccount({...newAccount, type: e.target.value})}><option value="regular">{tr("Обычный", "Regular")}</option><option value="iis">{tr("ИИС", "IIS")}</option></select></label></> : <>
        <div className="grid grid-cols-2 gap-3"><label className="text-sm">{tr("Счёт", "Account")}<select required className={input} value={draft.ledger} disabled={!!editing} onChange={e => setDraft({...draft, ledger: e.target.value})}><option value="">{tr("Выберите счёт","Choose account")}</option>{selected.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</select></label><label className="text-sm">{tr("Операция", "Operation")}<select className={input} value={draft.kind} onChange={e => setDraft({...draft, kind: e.target.value})}>{["buy", "sell", "dividend", "coupon"].map(k => <option key={k} value={k}>{kind(k)}</option>)}</select></label></div>
        <label className="block text-sm">{tr("Бумага", "Security")}<input className={input} required list="tracker-securities" value={draft.instrument} placeholder={tr("Тикер или ISIN", "Ticker or ISIN")} onChange={e => setDraft({...draft, instrument: e.target.value})}/><datalist id="tracker-securities">{[...new Set([...positions.map(p => p.key), ...(catalog.data?.symbols.keys() || [])])].map(key => <option key={key} value={key}>{name(key)}</option>)}</datalist></label>
        <label className="block text-sm">{tr("Биржа для новой бумаги", "Exchange for a new security")}<select className={input} value={draft.exchange} onChange={e => setDraft({...draft, exchange: e.target.value})}>{["MCX", "NYSE", "NASDAQ", "OTC"].map(exchange => <option key={exchange}>{exchange}</option>)}</select></label>
        <div className="grid grid-cols-2 gap-3"><label className="text-sm">{tr("Дата", "Date")}<input required className={input} type="date" value={draft.date} onChange={e => setDraft({...draft, date: e.target.value})}/></label><label className="text-sm">{tr("Валюта", "Currency")}<select className={input} value={draft.currency} onChange={e => setDraft({...draft, currency: e.target.value})}>{["RUB", "USD", "EUR"].map(c => <option key={c}>{c}</option>)}</select></label></div>
        {["buy", "sell"].includes(draft.kind) ? <><div className="grid grid-cols-2 gap-3"><label className="text-sm">{tr("Количество, шт.", "Quantity, units")}<input className={input} inputMode="decimal" required value={draft.quantity} onChange={e => setDraft({...draft, quantity: e.target.value})}/></label><label className="text-sm">{tr("Цена за штуку", "Price per unit")}<input className={input} inputMode="decimal" required value={draft.price} onChange={e => setDraft({...draft, price: e.target.value})}/></label></div><div className="grid grid-cols-2 gap-3"><label className="text-sm">{tr("Комиссия", "Fee")}<div className="flex gap-2"><input className={input + " min-w-0"} aria-label={tr("Комиссия", "Fee")} inputMode="decimal" value={draft.fee} onChange={e => setDraft({...draft, fee: e.target.value})}/><select className={input + " !w-24 shrink-0"} aria-label={tr("Валюта комиссии", "Fee currency")} value={draft.feeCurrency} onChange={e => setDraft({...draft, feeCurrency: e.target.value})}><option value="">{draft.currency}</option>{["RUB", "USD", "EUR"].map(c => <option key={c} value={c}>{c}</option>)}</select></div></label><label className="text-sm">{tr("НКД всего, для облигаций", "Total accrued interest, bonds")}<input className={input} inputMode="decimal" value={draft.accrued} onChange={e => setDraft({...draft, accrued: e.target.value})}/></label></div></> : <label className="block text-sm">{tr("Сумма выплаты", "Payout amount")}<input className={input} inputMode="decimal" required value={draft.amount} onChange={e => setDraft({...draft, amount: e.target.value})}/></label>}
        <label className="block text-sm">{tr("Комментарий", "Note")}<input className={input} maxLength={5000} value={draft.note} onChange={e => setDraft({...draft, note: e.target.value})}/></label>
      </>}
      {writeError && <p role="alert" className="text-sm text-rose-600">{writeError}</p>}<button className={primary} disabled={busy}>{busy ? tr("Сохраняем…", "Saving…") : tr("Сохранить", "Save")}</button>
    </fieldset></form></Modal>
  </div>;
}

export default function PortfolioTracker({analyticsOnly=false}:{analyticsOnly?:boolean}={}) {const tenant = useAssetTenant(); return tenant ? <Tracker key={tenant} tenantId={tenant} analyticsOnly={analyticsOnly}/> : null;}
