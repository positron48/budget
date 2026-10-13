"use client";

import {Code, ConnectError} from "@connectrpc/connect";
import {useMemo, useRef, useState} from "react";
import Link from "next/link";
import {useLocale} from "next-intl";
import {useQuery, useQueryClient} from "@tanstack/react-query";
import {Plus, Upload, Wallet, ArrowUpRight, Coins, RefreshCw, ChevronRight, PieChart} from "lucide-react";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import Modal from "@/components/Modal";
import {allOperations, loadBrokerCatalog} from "@/lib/portfolio-import";
import {add, decimal, divide, negate, payoutKinds, payoutMonths, trackerError, trackerKinds, trackerPositions, tradeAmount, zero} from "@/lib/portfolio-tracker";
import {positionDecimal, type Fraction} from "@/lib/investment-positions";
import type {InvestmentLedger, InvestmentOperation, InvestmentSnapshot} from "@/proto/budget/v1/investment_pb";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";

const panel = "rounded-2xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900";
const input = "mt-1 w-full rounded-lg border border-slate-300 bg-transparent px-3 py-2 dark:border-slate-700";
const secondary = "inline-flex items-center justify-center gap-2 rounded-lg border border-slate-200 px-3 py-2 text-sm hover:bg-slate-50 disabled:opacity-40 dark:border-slate-700 dark:hover:bg-slate-800";
const primary = "inline-flex items-center justify-center gap-2 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-40";
const colors = ["#10b981", "#6366f1", "#f59e0b", "#38bdf8", "#f472b6", "#94a3b8"];

function Tracker({tenantId}: {tenantId: string}) {
  const locale = useLocale(), ru = locale === "ru", {investment: api, investmentImport: imports, tenant} = useClients(), qc = useQueryClient();
  const readOnly = useInvestmentReadOnly();
  const writeKey = useRef(crypto.randomUUID()), pendingWrite = useRef<{signature: string; request: Record<string, unknown>} | null>(null);
  const tr = (r: string, e: string) => ru ? r : e;
  const [account, setAccount] = useState(""), [currency, setCurrency] = useState("RUB"), [view, setView] = useState("overview"), [closed, setClosed] = useState(false);
  const [dialog, setDialog] = useState<"operation" | "account" | null>(null), [editing, setEditing] = useState<InvestmentOperation | null>(null);
  const [draft, setDraft] = useState({ledger: "", kind: "buy", instrument: "", exchange: "MCX", quantity: "1", price: "", amount: "", fee: "0", accrued: "0", currency: "RUB", date: new Date().toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"}), note: ""});
  const [newAccount, setNewAccount] = useState({name: "", broker: "", type: "regular"}), [busy, setBusy] = useState(false), [writeError, setWriteError] = useState("");
  const ledgers = useQuery<InvestmentLedger[]>({queryKey: ["investment", tenantId, "tracker", "accounts"], queryFn: async () => {
    const all: InvestmentLedger[] = [];
    for (let offset = 0; ; offset += 200) { const page = await api.listInvestmentLedgers({limit: 200, offset}); all.push(...page.ledgers); if (page.ledgers.length < 200) return all; }
  }});
  const roles = useQuery<{memberships: TenantMembership[]}>({queryKey: ["investment", tenantId, "tracker", "roles"], queryFn: () => tenant.listMyTenants({})});
  const canWrite = !readOnly && roles.data?.memberships.some(m => m.tenant?.id === tenantId && [1, 2].includes(m.role));
  const selected = ledgers.data?.filter(l => !account || l.id === account) || [];
  const data = useQuery<{snapshots: InvestmentSnapshot[]; operations: InvestmentOperation[]}>({queryKey: ["investment", tenantId, "tracker", "data", selected.map(l => l.id)], enabled: !!ledgers.data, queryFn: async () => {
    const snapshots: InvestmentSnapshot[] = [], operations: InvestmentOperation[] = [];
    for (const ledger of selected) {
      const state = await api.getInvestmentSnapshot({ledgerId: ledger.id});
      if (!state.snapshot) throw new Error("snapshot_missing");
      snapshots.push(state.snapshot); operations.push(...await allOperations(api, ledger.id));
    }
    return {snapshots, operations};
  }});
  const catalog = useQuery({queryKey: ["investment", tenantId, "tracker", "broker-quotes"], queryFn: () => loadBrokerCatalog(imports), staleTime: 60000});
  const operations = data.data?.operations || [];
  const positions = useMemo(() => trackerPositions(data.data?.snapshots || [], data.data?.operations || [], catalog.data?.quotes), [data.data, catalog.data]);
  const current = positions.filter(p => p.currency === currency), held = current.filter(p => BigInt(p.quantity.numerator) !== 0n);
  const valueKnown = held.every(p => p.value), costKnown = held.every(p => p.cost), profitKnown = current.every(p => p.profit);
  const hasQuotes = held.some(p => p.value);
  const value = valueKnown || hasQuotes ? add(...held.map(p => p.value)) : undefined, cost = costKnown ? add(...held.map(p => p.cost)) : undefined;
  const income = add(...current.map(p => p.income)), realized = current.every(p => p.realized) ? add(...current.map(p => p.realized)) : undefined;
  const profit = add(...current.map(p => p.profit || (p.realized ? add(p.realized, p.income, negate(p.recoveredAccrued)) : add(p.income, negate(p.recoveredAccrued)))));
  const money = (amount?: Fraction, cur = currency) => amount ? `${new Intl.NumberFormat(locale, {maximumFractionDigits: 2}).format(Number(positionDecimal(amount)))} ${cur}` : "—";
  const quantity = (amount?: Fraction) => amount ? new Intl.NumberFormat(locale, {maximumFractionDigits: 10}).format(Number(positionDecimal(amount))) : "—";
  const name = (key: string) => catalog.data?.names.get(key)?.ticker || key.replace(/^tinvest:/, "");
  const months = payoutMonths(operations, currency), maxIncome = Math.max(1, ...months.map(([, amount]) => Number(positionDecimal(amount))));
  const totalCost = Number(positionDecimal(cost || zero));
  const composition = [...held].sort((a, b) => Number(positionDecimal(b.cost || zero)) - Number(positionDecimal(a.cost || zero))).slice(0, 5);
  const groupedCosts = composition.map(p => Math.max(0, Number(positionDecimal(p.cost || zero))));
  const remainder = Math.max(0, totalCost - groupedCosts.reduce((a, b) => a + b, 0));
  let angle = 0;
  const gradient = [...groupedCosts, remainder].map((amount, i) => { const start = angle; angle += totalCost ? amount / totalCost * 360 : 0; return `${colors[i]} ${start}deg ${angle}deg`; }).join(", ");
  const visibleOperations = operations.filter(op => op.status === "posted" && op.currency === currency && (view === "payments" ? payoutKinds.has(op.kind) : ["buy", "sell"].includes(op.kind))).sort((a, b) => Number((b.occurredAt?.seconds || 0n) - (a.occurredAt?.seconds || 0n)));
  const kind = (k: string) => ({buy: tr("Покупка", "Buy"), sell: tr("Продажа", "Sell"), dividend: tr("Дивиденды", "Dividend"), coupon: tr("Купон", "Coupon"), repo_income: tr("Доход РЕПО", "Repo income"), cash_interest: tr("Проценты", "Interest")}[k] || k);
  const refresh = async () => { await qc.invalidateQueries({queryKey: ["investment", tenantId]}); await qc.invalidateQueries({queryKey: ["investment-import", tenantId]}); };
  const openOperation = (op?: InvestmentOperation) => {
    setEditing(op || null); setWriteError(""); writeKey.current = crypto.randomUUID(); pendingWrite.current = null;
    setDraft({ledger: op?.ledgerId || account || selected[0]?.id || "", kind: op?.kind || "buy", instrument: op ? catalog.data?.names.get(op.instrumentKey)?.ticker || op.instrumentKey : "", exchange: op?.instrumentKey.includes("@") ? op.instrumentKey.split("@").at(-1)! : "MCX", quantity: op?.quantity || "1", price: op && ["buy", "sell"].includes(op.kind) ? positionDecimal(divide(decimal(op.amount) || zero, decimal(op.quantity) || zero), 28) : "", amount: op?.amount || "", fee: op?.fee || "0", accrued: op?.accrued || "0", currency: op?.currency || currency, date: op?.occurredAt ? new Date(Number(op.occurredAt.seconds) * 1000).toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"}) : new Date().toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"}), note: op?.note || ""});
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
        const instrumentKey = editing && normalized === originalLabel ? editing.instrumentKey : catalog.data?.symbols.get(normalized) || (normalized.includes(":") || normalized.includes("@") ? normalized : `${normalized}@${draft.exchange}`);
        const originalPrice = editing && trade ? positionDecimal(divide(decimal(editing.amount) || zero, decimal(editing.quantity) || zero), 28) : "";
        const amount = trade ? editing && draft.quantity === editing.quantity && draft.price === originalPrice ? editing.amount : tradeAmount(draft.quantity, draft.price) : draft.amount.replace(",", ".");
        const sameDate = !!editing?.occurredAt && draft.date === new Date(Number(editing.occurredAt.seconds) * 1000).toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"});
        const sameMoney = !!editing && amount === editing.amount && draft.quantity === editing.quantity && draft.currency === editing.currency && draft.fee === editing.fee && draft.accrued === editing.accrued;
        const operation = {...editing, id: editing?.id || "", ledgerId: draft.ledger, kind: draft.kind, instrumentKey, quantity: trade ? draft.quantity.replace(",", ".") : "0", amount, fee: trade ? draft.fee.replace(",", ".") : "0", accrued: trade ? draft.accrued.replace(",", ".") : "0", tax: editing && amount === editing.amount ? editing.tax : "0", currency: draft.currency, occurredAt: sameDate ? editing?.occurredAt : undefined, settlementAt: sameDate ? editing?.settlementAt : undefined, timing: sameDate ? editing?.timing : {timezone: "Europe/Moscow", occurredDate: draft.date}, tradeFinancials: sameMoney && sameDate ? editing?.tradeFinancials : undefined, lotConversion: sameMoney && sameDate ? editing?.lotConversion : undefined, note: draft.note};
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
  const error = ledgers.error || data.error || roles.error;
  const chartEmpty = !months.length;
  return <div className="min-h-screen bg-slate-50/70 text-slate-800 dark:bg-slate-950 dark:text-slate-100">
    <div className="mx-auto grid max-w-[1480px] gap-6 px-4 py-7 lg:grid-cols-[210px_minmax(0,1fr)] lg:px-7">
      <aside className="space-y-5 lg:pt-2"><div className="flex items-center gap-2 text-lg font-semibold"><Wallet size={20} className="text-emerald-600"/>{tr("Мои портфели", "My portfolios")}</div>
        <div className="flex gap-2 overflow-x-auto lg:flex-col"><button onClick={() => setAccount("")} className={`whitespace-nowrap rounded-xl px-3 py-3 text-left text-sm ${!account ? "bg-emerald-50 font-semibold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300" : "hover:bg-slate-100 dark:hover:bg-slate-800"}`}>{tr("Все счета", "All accounts")}</button>{ledgers.data?.map(l => <button key={l.id} onClick={() => setAccount(l.id)} className={`flex items-center justify-between gap-3 whitespace-nowrap rounded-xl px-3 py-3 text-left text-sm ${account === l.id ? "bg-emerald-50 font-semibold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300" : "hover:bg-slate-100 dark:hover:bg-slate-800"}`}><span>{l.name}</span>{l.accountType === "iis" && <span className="text-[10px] text-slate-400">{tr("ИИС", "IIS")}</span>}</button>)}</div>
        {canWrite && <button className={secondary + " w-full"} onClick={() => {setNewAccount({name: "", broker: "", type: "regular"}); setWriteError(""); writeKey.current = crypto.randomUUID(); setDialog("account");}}><Plus size={15}/>{tr("Добавить счёт", "Add account")}</button>}
        <Link className="flex items-center justify-between px-3 text-sm text-slate-500" href="/investments/accounts">{tr("Настройки счетов", "Account settings")}<ChevronRight size={15}/></Link>
      </aside>
      <div className="min-w-0 space-y-6">
        <header className="flex flex-wrap items-center justify-between gap-4"><div><p className="text-sm text-slate-500">{tr("Инвестиции", "Investments")}</p><h1 className="mt-1 text-3xl font-semibold tracking-tight">{account ? selected[0]?.name : tr("Мой портфель", "My portfolio")}</h1></div><div className="flex flex-wrap gap-2"><button className={secondary} disabled={data.isFetching} onClick={() => void refresh()} aria-label={tr("Обновить портфель", "Refresh portfolio")}><RefreshCw size={16} className={data.isFetching ? "animate-spin" : ""}/></button>{canWrite && <><Link href="/investments/import" className={secondary}><Upload size={15}/>{tr("Импорт", "Import")}</Link><button className={primary} disabled={!selected.length || !data.data} onClick={() => openOperation()}><Plus size={16}/>{tr("Добавить операцию", "Add operation")}</button></>}<select aria-label={tr("Валюта сумм", "Amount currency")} className={secondary} value={currency} onChange={e => setCurrency(e.target.value)}>{["RUB", "USD", "EUR"].map(c => <option key={c}>{c}</option>)}</select></div></header>
        <nav className="flex gap-6 border-b border-slate-200 dark:border-slate-800" aria-label={tr("Разделы портфеля", "Portfolio sections")}>{[["overview", tr("Обзор", "Overview")], ["trades", tr("Сделки", "Trades")], ["payments", tr("Дивиденды и купоны", "Dividends and coupons")]].map(([id, title]) => <button key={id} onClick={() => setView(id)} className={`border-b-2 pb-3 text-sm font-medium ${view === id ? "border-emerald-600 text-emerald-700 dark:text-emerald-400" : "border-transparent text-slate-500"}`}>{title}</button>)}</nav>
        {error && <p role="alert" className="rounded-xl bg-rose-50 p-4 text-sm text-rose-800 dark:bg-rose-950 dark:text-rose-200">{tr("Не удалось загрузить портфель. Попробуйте обновить страницу.", "Could not load the portfolio. Try refreshing the page.")}</p>}
        {data.isPending && <p role="status" className="py-8 text-slate-500">{tr("Загружаем портфель…", "Loading portfolio…")}</p>}
        {data.data && <>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">{[
            {title: valueKnown ? tr("Стоимость портфеля", "Portfolio value") : tr("Стоимость известной части", "Known portfolio value"), amount: value, detail: valueKnown ? tr("По котировкам из брокерских выгрузок", "Using prices from broker snapshots") : tr(`Нет котировок для ${held.filter(p => !p.value).length} бумаг`, `Quotes unavailable for ${held.filter(p => !p.value).length} securities`), icon: Wallet},
            {title: tr("Вложено в бумаги", "Cost of holdings"), amount: cost, detail: tr("Стоимость оставшихся покупок", "Cost of remaining purchases"), icon: PieChart},
            {title: profitKnown ? tr("Общая прибыль", "Total profit") : hasQuotes ? tr("Известная прибыль", "Known profit") : tr("Полученная прибыль", "Realized profit"), amount: profit, detail: tr(`От продаж: ${money(realized)}${profitKnown ? "" : "; без неизвестного роста стоимости"}`, `Realized: ${money(realized)}${profitKnown ? "" : "; excludes unknown price gains"}`), icon: ArrowUpRight},
            {title: tr("Выплаты", "Payouts"), amount: income, detail: tr("Полученные выплаты за всё время", "Payouts received, all time"), icon: Coins},
          ].map(card => <section className={panel + " p-5"} key={card.title}><div className="flex justify-between gap-2 text-sm text-slate-500"><h2>{card.title}</h2><card.icon size={17}/></div><p className="mt-4 text-2xl font-semibold tabular-nums">{money(card.amount)}</p><p className="mt-2 text-xs leading-5 text-slate-400">{card.detail}</p></section>)}</div>
          <p className="text-xs text-slate-500">{tr(`Суммы в ${currency}; другие валюты доступны в переключателе. Пополнения, снятия и налоги не входят в расчёт.`, `Amounts in ${currency}; choose other currencies above. Deposits, withdrawals and taxes are excluded.`)}</p>
          {view === "overview" ? <>
            <div className="grid gap-5 xl:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]"><section className={panel + " p-5"}><div className="flex items-center justify-between"><h2 className="font-semibold">{tr("Полученные выплаты", "Payouts received")}</h2><span className="text-xs text-slate-400">{tr("По месяцам", "By month")}</span></div>{chartEmpty ? <p className="flex h-52 items-center justify-center text-sm text-slate-400">{tr("Здесь появятся ваши дивиденды и купоны", "Your dividends and coupons will appear here")}</p> : <div className="mt-6 flex h-52 items-end gap-2" role="img" aria-label={tr("Выплаты по месяцам", "Monthly payouts")}>{months.map(([month, amount]) => <div className="flex h-full min-w-0 flex-1 flex-col justify-end gap-2 text-center" key={month}><div className="group relative mx-auto w-full max-w-12 rounded-t-md bg-emerald-400 dark:bg-emerald-600" style={{height: `${Math.max(2, Number(positionDecimal(amount)) / maxIncome * 85)}%`}} title={`${month}: ${money(amount)}`}><span className="absolute bottom-full left-1/2 mb-2 hidden -translate-x-1/2 whitespace-nowrap rounded bg-slate-800 px-2 py-1 text-[10px] text-white group-hover:block">{money(amount)}</span></div><span className="truncate text-[10px] text-slate-400">{month}</span></div>)}</div>}</section>
              <section className={panel + " p-5"}><h2 className="font-semibold">{tr("Состав портфеля", "Portfolio allocation")}</h2><p className="mt-1 text-xs text-slate-400">{tr("По стоимости приобретения", "By acquisition cost")}</p><div className="mt-5 flex flex-wrap items-center justify-center gap-6"><div className="flex h-36 w-36 shrink-0 items-center justify-center rounded-full" role="img" aria-label={tr("Распределение по бумагам", "Allocation by security")} style={{background: totalCost > 0 ? `conic-gradient(${gradient})` : "#e2e8f0"}}><div className="flex h-24 w-24 flex-col items-center justify-center rounded-full bg-white dark:bg-slate-900"><span className="text-2xl font-semibold">{held.length}</span><span className="text-xs text-slate-400">{tr("бумаг", "securities")}</span></div></div><ul className="min-w-32 space-y-2 text-xs">{composition.map((p, i) => <li className="flex items-center gap-2" key={p.key}><span className="h-2 w-2 rounded-full" style={{background: colors[i]}}/><span className="max-w-36 truncate">{name(p.key)}</span><span className="ml-auto text-slate-400">{totalCost ? Math.round(groupedCosts[i] / totalCost * 100) : 0}%</span></li>)}{remainder > 0 && <li className="text-slate-400">{tr("Остальные", "Other")}</li>}</ul></div></section></div>
            <section className={panel + " overflow-hidden"}><div className="flex flex-wrap items-center justify-between gap-3 p-5"><h2 className="font-semibold">{tr("Мои активы", "My holdings")}</h2><label className="flex items-center gap-2 text-xs text-slate-500"><input type="checkbox" checked={closed} onChange={e => setClosed(e.target.checked)}/>{tr("Показать проданные", "Show closed positions")}</label></div><div className="overflow-x-auto"><table className="w-full whitespace-nowrap text-left text-sm"><thead className="border-y border-slate-100 bg-slate-50 text-xs text-slate-400 dark:border-slate-800 dark:bg-slate-900"><tr>{[tr("Бумага", "Security"), tr("Количество", "Quantity"), tr("Цена", "Price"), tr("Стоимость / вложено", "Value / cost"), tr("Прибыль", "Profit"), tr("Выплаты", "Payouts")].map(x => <th className="px-5 py-3 font-medium" key={x}>{x}</th>)}</tr></thead><tbody className="divide-y divide-slate-100 dark:divide-slate-800">{current.filter(p => closed || BigInt(p.quantity.numerator) !== 0n).map(p => <tr key={`${p.key}:${p.currency}`}><td className="max-w-60 px-5 py-4"><div className="flex items-center gap-3"><span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-slate-100 text-xs font-semibold text-slate-500 dark:bg-slate-800">{name(p.key).slice(0, 2)}</span><div className="min-w-0"><p className="truncate font-semibold" title={p.name || name(p.key)}>{name(p.key)}</p><p className="mt-1 max-w-48 truncate text-xs text-slate-400">{p.accounts.join(", ") || tr("Выплаты", "Payouts")}</p></div></div></td><td className="px-5 py-4 tabular-nums">{quantity(p.quantity)}</td><td className="px-5 py-4 tabular-nums">{money(p.price)}{p.quoteAt && <p className="mt-1 text-[10px] text-slate-400">{new Date(p.quoteAt).toLocaleDateString(locale)}</p>}</td><td className="px-5 py-4 tabular-nums"><p>{money(p.value)}</p><p className="mt-1 text-xs text-slate-400">{money(p.cost)}</p></td><td className={`px-5 py-4 tabular-nums ${p.profit && BigInt(p.profit.numerator) >= 0n ? "text-emerald-600" : p.profit ? "text-rose-500" : "text-slate-400"}`}>{money(p.profit)}</td><td className="px-5 py-4 tabular-nums">{money(p.income)}</td></tr>)}</tbody></table></div>{!held.length && !closed && <p className="p-8 text-center text-sm text-slate-400">{tr("Добавьте покупку или импортируйте историю — здесь появятся ваши бумаги.", "Add a purchase or import your history to see your holdings.")}</p>}</section>
          </> : <section className={panel + " overflow-hidden"}><div className="overflow-x-auto"><table className="w-full whitespace-nowrap text-left text-sm"><thead className="bg-slate-50 text-xs text-slate-400 dark:bg-slate-900"><tr>{[tr("Дата", "Date"), tr("Бумага", "Security"), tr("Операция", "Operation"), tr("Счёт", "Account"), tr("Количество", "Quantity"), tr("Сумма", "Amount"), ""].map((x, i) => <th className="px-5 py-4 font-medium" key={i}>{x}</th>)}</tr></thead><tbody className="divide-y divide-slate-100 dark:divide-slate-800">{visibleOperations.map(op => <tr key={op.id}><td className="px-5 py-4 text-slate-500">{op.occurredAt && new Date(Number(op.occurredAt.seconds) * 1000).toLocaleDateString(locale, {timeZone: "Europe/Moscow"})}</td><td className="px-5 py-4 font-medium">{name(op.instrumentKey)}</td><td className="px-5 py-4">{kind(op.kind)}</td><td className="px-5 py-4 text-slate-500">{ledgers.data?.find(l => l.id === op.ledgerId)?.name}</td><td className="px-5 py-4 tabular-nums">{["buy", "sell"].includes(op.kind) ? op.quantity : "—"}</td><td className="px-5 py-4 tabular-nums">{money(decimal(op.amount), op.currency)}</td><td className="px-5 py-4">{canWrite && trackerKinds.has(op.kind) && <button className="text-xs text-emerald-700 dark:text-emerald-400" onClick={() => openOperation(op)}>{tr("Изменить", "Edit")}</button>}</td></tr>)}</tbody></table></div>{!visibleOperations.length && <p className="p-8 text-center text-sm text-slate-400">{tr("Операций пока нет", "No operations yet")}</p>}</section>}
        </>}
      </div>
    </div>
    <Modal open={!!dialog} title={dialog === "account" ? tr("Новый счёт", "New account") : editing ? tr("Изменить операцию", "Edit operation") : tr("Новая операция", "New operation")} onClose={() => {if (!busy) setDialog(null);}}><form className="space-y-4" onSubmit={e => {e.preventDefault(); void save();}}><fieldset disabled={busy} className="space-y-4">
      {dialog === "account" ? <><label className="block text-sm">{tr("Название", "Name")}<input className={input} required maxLength={200} value={newAccount.name} onChange={e => setNewAccount({...newAccount, name: e.target.value})}/></label><label className="block text-sm">{tr("Брокер", "Broker")}<input className={input} maxLength={200} value={newAccount.broker} onChange={e => setNewAccount({...newAccount, broker: e.target.value})}/></label><label className="block text-sm">{tr("Тип счёта", "Account type")}<select className={input} value={newAccount.type} onChange={e => setNewAccount({...newAccount, type: e.target.value})}><option value="regular">{tr("Обычный", "Regular")}</option><option value="iis">{tr("ИИС", "IIS")}</option></select></label></> : <>
        <div className="grid grid-cols-2 gap-3"><label className="text-sm">{tr("Счёт", "Account")}<select className={input} value={draft.ledger} disabled={!!editing} onChange={e => setDraft({...draft, ledger: e.target.value})}>{selected.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</select></label><label className="text-sm">{tr("Операция", "Operation")}<select className={input} value={draft.kind} onChange={e => setDraft({...draft, kind: e.target.value})}>{["buy", "sell", "dividend", "coupon"].map(k => <option key={k} value={k}>{kind(k)}</option>)}</select></label></div>
        <label className="block text-sm">{tr("Бумага", "Security")}<input className={input} required list="tracker-securities" value={draft.instrument} placeholder={tr("Тикер или ISIN", "Ticker or ISIN")} onChange={e => setDraft({...draft, instrument: e.target.value})}/><datalist id="tracker-securities">{[...new Set([...positions.map(p => p.key), ...(catalog.data?.symbols.keys() || [])])].map(key => <option key={key} value={key}>{name(key)}</option>)}</datalist></label>
        <label className="block text-sm">{tr("Биржа для новой бумаги", "Exchange for a new security")}<select className={input} value={draft.exchange} onChange={e => setDraft({...draft, exchange: e.target.value})}>{["MCX", "NYSE", "NASDAQ", "OTC"].map(exchange => <option key={exchange}>{exchange}</option>)}</select></label>
        <div className="grid grid-cols-2 gap-3"><label className="text-sm">{tr("Дата", "Date")}<input required className={input} type="date" value={draft.date} onChange={e => setDraft({...draft, date: e.target.value})}/></label><label className="text-sm">{tr("Валюта", "Currency")}<select className={input} value={draft.currency} onChange={e => setDraft({...draft, currency: e.target.value})}>{["RUB", "USD", "EUR"].map(c => <option key={c}>{c}</option>)}</select></label></div>
        {["buy", "sell"].includes(draft.kind) ? <><div className="grid grid-cols-2 gap-3"><label className="text-sm">{tr("Количество, шт.", "Quantity, units")}<input className={input} inputMode="decimal" required value={draft.quantity} onChange={e => setDraft({...draft, quantity: e.target.value})}/></label><label className="text-sm">{tr("Цена за штуку", "Price per unit")}<input className={input} inputMode="decimal" required value={draft.price} onChange={e => setDraft({...draft, price: e.target.value})}/></label></div><div className="grid grid-cols-2 gap-3"><label className="text-sm">{tr("Комиссия", "Fee")}<input className={input} inputMode="decimal" value={draft.fee} onChange={e => setDraft({...draft, fee: e.target.value})}/></label><label className="text-sm">{tr("НКД всего, для облигаций", "Total accrued interest, bonds")}<input className={input} inputMode="decimal" value={draft.accrued} onChange={e => setDraft({...draft, accrued: e.target.value})}/></label></div></> : <label className="block text-sm">{tr("Сумма выплаты", "Payout amount")}<input className={input} inputMode="decimal" required value={draft.amount} onChange={e => setDraft({...draft, amount: e.target.value})}/></label>}
        <label className="block text-sm">{tr("Комментарий", "Note")}<input className={input} maxLength={5000} value={draft.note} onChange={e => setDraft({...draft, note: e.target.value})}/></label>
      </>}
      {writeError && <p role="alert" className="text-sm text-rose-600">{writeError}</p>}<button className={primary} disabled={busy}>{busy ? tr("Сохраняем…", "Saving…") : tr("Сохранить", "Save")}</button>
    </fieldset></form></Modal>
  </div>;
}

export default function PortfolioTracker() {const tenant = useAssetTenant(); return tenant ? <Tracker key={tenant} tenantId={tenant}/> : null;}
