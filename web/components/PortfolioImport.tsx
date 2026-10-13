"use client";

import {useState} from "react";
import Link from "next/link";
import {useLocale} from "next-intl";
import {useQuery, useQueryClient} from "@tanstack/react-query";
import {ArrowLeft, Upload, CheckCircle2, FileSpreadsheet} from "lucide-react";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "./InvestmentMode";
import {commitSimpleImport, loadBrokerCatalog, prepareSimpleImport, type ImportResult, type SimpleImportPlan} from "@/lib/portfolio-import";
import {trackerError} from "@/lib/portfolio-tracker";
import type {InvestmentLedger} from "@/proto/budget/v1/investment_pb";
import type {InvestmentImport} from "@/proto/budget/v1/investment_import_pb";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";

const field = "mt-2 w-full rounded-xl border border-slate-300 bg-white px-3 py-2.5 text-sm dark:border-slate-700 dark:bg-slate-900";
const button = "inline-flex items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-40";
function Import({tenantId}: {tenantId: string}) {
  const ru = useLocale() === "ru", tr = (r: string, e: string) => ru ? r : e;
  const {investment: api, investmentImport: imports, tenant} = useClients(), qc = useQueryClient(), readOnly = useInvestmentReadOnly();
  const [file, setFile] = useState<File | null>(null), [ledger, setLedger] = useState(""), [source, setSource] = useState<InvestmentImport | null>(null);
  const [bindings, setBindings] = useState<Record<string, string>>({}), [plan, setPlan] = useState<SimpleImportPlan | null>(null), [result, setResult] = useState<ImportResult | null>(null);
  const [preserveIdenticalRows, setPreserveIdenticalRows] = useState(true);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const ledgers = useQuery<InvestmentLedger[]>({queryKey: ["investment", tenantId, "tracker", "accounts"], queryFn: async () => {
    const all: InvestmentLedger[] = [];
    for (let offset = 0; ; offset += 200) {const page = await api.listInvestmentLedgers({limit: 200, offset}); all.push(...page.ledgers); if (page.ledgers.length < 200) return all;}
  }});
  const roles = useQuery<{memberships: TenantMembership[]}>({queryKey: ["investment", tenantId, "tracker", "roles"], queryFn: () => tenant.listMyTenants({})});
  const canWrite = !readOnly && roles.data?.memberships.some(m => m.tenant?.id === tenantId && [1, 2].includes(m.role));
  const history = useQuery<InvestmentImport[]>({queryKey: ["investment-import", tenantId, "simple-history"], queryFn: async () => {
    const all: InvestmentImport[] = [];
    for (let offset = 0; ; offset += 200) {const page = await imports.listInvestmentImports({limit: 200, offset}); all.push(...page.imports); if (page.imports.length < 200) return all;}
  }});
  const catalog = useQuery({queryKey: ["investment", tenantId, "tracker", "broker-quotes"], queryFn: () => loadBrokerCatalog(imports), staleTime: 60000});
  const open = (item: InvestmentImport) => {
    const mapped: Record<string, string> = {};
    for (const account of item.accounts) {
      const matches = ledgers.data?.filter(l => l.name === account.name) || [];
      if (matches.length === 1) mapped[account.id] = matches[0].id;
    }
    setBindings(mapped); setSource(item); setPlan(null); setResult(null); setError("");
  };
  const loadExisting = async (item: InvestmentImport) => {
    setBusy(true); setError("");
    try {
      const full = (await imports.getInvestmentImport({id: item.id})).import;
      if (!full) throw new Error("import_missing");
      open(full);
    } catch (cause) {setError(trackerError(cause, ru));} finally {setBusy(false);}
  };
  const upload = async () => {
    if (!file || !ledger) return;
    setBusy(true); setError("");
    try {
      if (file.size > 20 * 1024 * 1024) throw new Error("file_limit");
      const selected = ledgers.data?.find(l => l.id === ledger);
      const response = await imports.uploadInvestmentImport({filename: file.name, csv: new Uint8Array(await file.arrayBuffer()), requestKey: crypto.randomUUID(), accounts: [{sourceKey: `tracker:${ledger}`, name: selected?.name || ""}]});
      if (!response.import) throw new Error("import_missing");
      open(response.import);
      const mapped: Record<string, string> = {};
      for (const account of response.import.accounts) {
        const matches = ledgers.data?.filter(item => item.name === account.name) || [];
        if (matches.length === 1) mapped[account.id] = matches[0].id;
      }
      if (ledgers.data && catalog.data && response.import.accounts.every((account: {id: string}) => mapped[account.id])) setPlan(await prepareSimpleImport(imports, response.import, ledgers.data, mapped, catalog.data, ru, preserveIdenticalRows));
      await qc.invalidateQueries({queryKey: ["investment-import", tenantId]});
    } catch (cause) {
      setError(cause instanceof Error && cause.message === "file_limit" ? tr("Файл больше 20 МБ.", "The file exceeds 20 MiB.") : tr("Не удалось прочитать файл. Нужна CSV-выгрузка операций Snowball.", "Could not read the file. Select a Snowball operations CSV."));
    } finally {setBusy(false);}
  };
  const preview = async () => {
    if (!source || !ledgers.data || !catalog.data) return;
    setBusy(true); setError(""); setResult(null);
    try {
      const fresh = (await imports.getInvestmentImport({id: source.id})).import;
      if (!fresh) throw new Error("import_missing");
      const prepared = await prepareSimpleImport(imports, fresh, ledgers.data, bindings, catalog.data, ru, preserveIdenticalRows);
      setSource(fresh); setPlan(prepared);
    } catch (cause) {setError(trackerError(cause, ru));} finally {setBusy(false);}
  };
  const commit = async () => {
    if (!plan) return;
    setBusy(true); setError("");
    try {
      const applied = await commitSimpleImport(api, plan, ru, setResult);
      setResult(applied); if (applied.stopped) setError(applied.stopped);
      await qc.invalidateQueries({queryKey: ["investment", tenantId]});
      await qc.invalidateQueries({queryKey: ["investment-import", tenantId]});
    } catch (cause) {setError(trackerError(cause, ru));} finally {setBusy(false);}
  };
  const problems = result?.problems || plan?.problems || [];
  return <div className="min-h-screen bg-slate-50/70 px-4 py-8 text-slate-800 dark:bg-slate-950 dark:text-slate-100"><div className="mx-auto max-w-4xl space-y-6">
    <Link href="/investments" className="inline-flex items-center gap-2 text-sm text-slate-500"><ArrowLeft size={16}/>{tr("К портфелю", "Back to portfolio")}</Link>
    <header><h1 className="text-3xl font-semibold">{tr("Импорт операций", "Import operations")}</h1><p className="mt-2 text-sm text-slate-500">{tr("Выберите файл, проверьте счета и добавьте операции в портфель.", "Choose a file, check the accounts and add operations to your portfolio.")}</p></header>
    {(roles.error || ledgers.error || history.error || catalog.error) && <p role="alert" className="rounded-xl bg-rose-50 p-4 text-sm text-rose-700 dark:bg-rose-950 dark:text-rose-200">{tr("Не удалось загрузить данные. Обновите страницу.", "Could not load data. Refresh the page.")}</p>}
    {canWrite && !source && <form className="space-y-5 rounded-2xl border border-slate-200 bg-white p-6 dark:border-slate-800 dark:bg-slate-900" onSubmit={e => {e.preventDefault(); void upload();}}><div className="grid gap-5 sm:grid-cols-2"><label className="text-sm font-medium">{tr("Выгрузка Snowball", "Snowball export")}<input className={field} type="file" accept=".csv,text/csv" disabled={busy} onChange={e => setFile(e.target.files?.[0] || null)}/></label><label className="text-sm font-medium">{tr("Счёт для операций", "Account for operations")}<select required className={field} value={ledger} disabled={busy} onChange={e => setLedger(e.target.value)}><option value="">{tr("Выберите счёт", "Select an account")}</option>{ledgers.data?.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</select></label></div><p className="text-xs leading-5 text-slate-400">{tr("Покупки, продажи, дивиденды и купоны. Пополнения, снятия и налоговые строки не нужны. Повторная загрузка сохраняет исходное распределение и не добавляет проведённые операции второй раз.", "Purchases, sales, dividends and coupons. Deposits, withdrawals and tax rows are not needed. Re-uploading preserves the original allocation and does not duplicate imported operations.")}</p><button className={button} disabled={busy || !file || !ledger || !catalog.data}><Upload size={16}/>{busy ? tr("Загружаем…", "Uploading…") : tr("Предпросмотр", "Preview")}</button></form>}
    {history.data && history.data.length > 0 && <section><h2 className="mb-3 text-sm font-medium text-slate-500">{tr("Загруженные файлы", "Uploaded files")}</h2><div className="flex flex-wrap gap-2">{history.data.map(item => <button key={item.id} disabled={busy} onClick={() => void loadExisting(item)} className={`flex items-center gap-3 rounded-xl border bg-white px-4 py-3 text-sm dark:bg-slate-900 ${source?.id === item.id ? "border-emerald-500" : "border-slate-200 dark:border-slate-800"}`}><FileSpreadsheet size={18} className="text-emerald-600"/><span>{item.filename}<span className="ml-2 text-xs text-slate-400">{item.rowCount} {tr("строк", "rows")}</span></span></button>)}</div></section>}
    {source && <section className="space-y-5 rounded-2xl border border-slate-200 bg-white p-6 dark:border-slate-800 dark:bg-slate-900"><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-lg font-semibold">{source.filename}</h2><button className="text-sm text-slate-500 underline" disabled={busy} onClick={() => {setSource(null); setPlan(null); setResult(null); setError("");}}>{tr("Другой файл", "Choose another file")}</button></div><div className="grid gap-4 sm:grid-cols-2">{source.accounts.map(account => <label key={account.id} className="text-sm">{account.name}<span className="ml-2 text-xs text-slate-400">{account.rowCount} {tr("строк", "rows")}</span><select className={field} disabled={busy || !canWrite} value={bindings[account.id] || ""} onChange={e => {setBindings({...bindings, [account.id]: e.target.value}); setPlan(null); setResult(null);}}><option value="">{tr("Выберите счёт", "Select an account")}</option>{ledgers.data?.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</select></label>)}</div>
      <label className="flex items-start gap-2 text-sm"><input type="checkbox" className="mt-1" checked={preserveIdenticalRows} disabled={busy || !canWrite} onChange={e => {setPreserveIdenticalRows(e.target.checked); setPlan(null); setResult(null);}}/><span>{tr("Сохранять одинаковые строки CSV как отдельные операции", "Keep identical CSV rows as separate operations")}<span className="mt-1 block text-xs text-slate-500">{tr("Повторный импорт этого файла не создаёт копий. Денежные суммы округляются до 12 знаков после запятой; исходный файл сохраняется целиком.", "Re-importing this file does not create duplicates. Monetary amounts are rounded to 12 decimal places; the original file is retained in full.")}</span></span></label>
      {canWrite && <button className={button} disabled={busy || !catalog.data || source.accounts.some(a => !bindings[a.id])} onClick={() => void preview()}>{busy ? tr("Обрабатываем…", "Processing…") : tr("Предпросмотр", "Preview")}</button>}
      {plan && !result && <div className="space-y-4 border-t border-slate-100 pt-5 dark:border-slate-800"><p className="text-sm">{tr(`Распознано операций: ${plan.ready}. Уже добавлено: ${plan.alreadyImported}. Не нужны для портфеля: ${plan.ignored}.`, `Recognized ${plan.ready} operations. Already imported: ${plan.alreadyImported}. Not needed for the portfolio: ${plan.ignored}.`)}</p><div className="overflow-x-auto"><table className="w-full whitespace-nowrap text-left text-xs"><thead className="text-slate-400"><tr>{[tr("Строка", "Row"), tr("Операция", "Operation"), tr("Бумага", "Security"), tr("Количество", "Quantity"), tr("Сумма", "Amount")].map(x => <th className="px-2 py-2 font-medium" key={x}>{x}</th>)}</tr></thead><tbody>{plan.batch?.entries.slice(0, 12).map(entry => <tr key={entry.record} className="border-t border-slate-100 dark:border-slate-800"><td className="px-2 py-2">{entry.record}</td><td className="px-2 py-2">{{buy: tr("Покупка", "Buy"), sell: tr("Продажа", "Sell"), dividend: tr("Дивиденды", "Dividend"), coupon: tr("Купон", "Coupon"), split: tr("Сплит", "Split"), repo_income: tr("Доход РЕПО", "Repo income")}[entry.operation?.kind || ""]}</td><td className="max-w-52 truncate px-2 py-2">{catalog.data?.names.get(entry.operation?.instrumentKey || "")?.ticker || entry.operation?.instrumentKey}</td><td className="px-2 py-2">{entry.operation?.quantity}</td><td className="px-2 py-2">{entry.operation?.amount} {entry.operation?.currency}</td></tr>)}</tbody></table></div>{canWrite && plan.ready > 0 && <button className={button} disabled={busy} onClick={() => void commit()}>{tr(`Импортировать (${plan.ready})`, `Import ${plan.ready} operations`)}</button>}</div>}
      {result && <div role="status" className="flex items-start gap-3 rounded-xl bg-emerald-50 p-4 text-sm text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200"><CheckCircle2 size={20} className="shrink-0"/><div><p className="font-medium">{tr(`Добавлено операций: ${result.imported}`, `${result.imported} operation${result.imported === 1 ? "" : "s"} imported`)}</p><p className="mt-1">{busy ? tr("Продолжаем импорт…", "Import in progress…") : tr("Сохранённые операции уже в портфеле.", "Saved operations are now in your portfolio.")}</p>{!busy && <Link href="/investments" className="mt-3 inline-block underline">{tr("Открыть портфель", "Open portfolio")}</Link>}</div></div>}
      {problems.length > 0 && <details className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm dark:border-amber-900 dark:bg-amber-950"><summary className="cursor-pointer font-medium">{tr(`Строк для уточнения: ${problems.length}`, `Rows needing clarification: ${problems.length}`)}</summary><p className="mt-2 text-xs leading-5 text-amber-800 dark:text-amber-200">{tr("Эти строки сохранены в исходном файле. Они не мешают добавить остальные бумаги.", "These rows remain in the original file and do not block other securities.")}</p><ul className="mt-3 max-h-72 space-y-2 overflow-y-auto">{problems.map(p => <li key={`${p.record}:${p.message}`}><span className="font-medium">#{p.record} · {catalog.data?.names.get(p.symbol)?.ticker || p.symbol}</span><span className="ml-2 text-xs">{p.message}</span></li>)}</ul></details>}
    </section>}
    {error && <p role="alert" className="rounded-xl bg-rose-50 p-4 text-sm text-rose-700 dark:bg-rose-950 dark:text-rose-200">{error}</p>}
  </div></div>;
}
export default function PortfolioImport() {const tenant = useAssetTenant(); return tenant ? <Import key={tenant} tenantId={tenant}/> : null;}
