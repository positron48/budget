"use client";
import InvestmentChart from "@/components/InvestmentChart";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";

import { create } from "@bufbuild/protobuf";
import { InvestmentNAVSeriesSchema } from "@/proto/budget/v1/investment_nav_pb";
import InvestmentCurrent,{useInvestmentCurrent} from "@/components/InvestmentCurrent";
import InvestmentVariant from "@/components/InvestmentVariant";
import InvestmentSeriesExport from "@/components/InvestmentSeriesExport";
import InvestmentBenchmarkComparison from "@/components/InvestmentBenchmarkComparison";
import InvestmentPerformance from "@/components/InvestmentPerformance";
import InvestmentPositions from "@/components/InvestmentPositions";
import InvestmentComposition from "@/components/InvestmentComposition";
import InvestmentPortfolios from "@/components/InvestmentPortfolios";
import { useState,useEffect } from "react";
import {useSearchParams,useRouter,usePathname} from "next/navigation";
import InvestmentPeriod,{useInvestmentRange} from "@/components/InvestmentPeriod";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { Protected } from "@/components";
import { useClients } from "@/app/providers";
import { useAssetTenant } from "@/lib/assets";
import { navDecimal, currentNAVGeometry } from "@/lib/investment-nav";
import type { InvestmentNAVSeries, InvestmentNAVPoint } from "@/proto/budget/v1/investment_nav_pb";

const field = "mt-1 w-full rounded-lg border border-gray-300 bg-white p-2 dark:border-gray-700 dark:bg-gray-900";
const button = "rounded-lg border border-gray-300 px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";

function SeriesValue({ tenantId, series, archived=false }: { tenantId: string; series: InvestmentNAVSeries; archived?:boolean }) {
  const t = useTranslations("investmentNAV");
  const { investmentNAV: api } = useClients();
  const queryClient=useQueryClient();
  const reloadPerformance=()=>Promise.all([queryClient.invalidateQueries({queryKey:["investmentNAV",tenantId,"profit",series.key]}),queryClient.invalidateQueries({queryKey:["investmentPerformance",tenantId,series.key]}),queryClient.invalidateQueries({queryKey:["investmentBenchmark",tenantId,series.key]})]);
  const range=useInvestmentRange(series.key,series.fromDate,series.toDate);
  const {from,to,valid}=range;
  const [offset, setOffset] = useState(0);
  useEffect(()=>setOffset(0),[from,to]);
  const history = useQuery<{ points: InvestmentNAVPoint[] }>({
    queryKey: ["investmentNAV", tenantId, series.key, from, to], enabled: valid,
    queryFn: () => api.getInvestmentNAVHistory({ seriesKey: series.key, fromDate: from, toDate: to }),
    refetchInterval: 15000,
  });
  const readOnly=useInvestmentReadOnly();
  const refresh = useMutation({ mutationFn: () => api.requestInvestmentNAVRefresh({ seriesKey: series.key }), onSuccess: () => Promise.all([history.refetch(),reloadPerformance()]) });
  const points = history.data?.points || [];
  const current=useInvestmentCurrent(tenantId,series,valid&&!archived);
  const geometry = currentNAVGeometry(points,current.data);
  const currentText=useTranslations("investmentCurrent");
  const latest = points.at(-1);
  const state = (value: string) => t.has(`states.${value}`) ? t(`states.${value}`) : t("unknown");
  const error = history.error || refresh.error;
  return <section className="space-y-5 rounded-xl border border-gray-200 p-4 dark:border-gray-800">
    <div className="flex flex-wrap items-end gap-3">
      <label>{t("from")}<input type="date" className={field} min={series.fromDate} max={to} value={from} onChange={e => {range.update(e.target.value,to); setOffset(0);}} /></label>
      <label>{t("to")}<input type="date" className={field} min={from} max={series.toDate} value={to} onChange={e => {range.update(from,e.target.value); setOffset(0);}} /></label>
      <button className={button} disabled={readOnly||refresh.isPending||archived} onClick={() => refresh.mutate()}>{t("recalculate")}</button>
      <button className={button} disabled={!valid || history.isFetching} onClick={() => {void history.refetch();void reloadPerformance();}}>{t("reload")}</button>
    </div>
    <InvestmentVariant key={`variant:${series.key}`} tenantId={tenantId} series={series} archived={archived}/>
    <InvestmentCurrent tenantId={tenantId} series={series} enabled={valid&&!archived}/>
    <InvestmentPeriod period={range.period} select={p=>{range.select(p);setOffset(0);}}/>
    <InvestmentSeriesExport key={`export:${series.key}:${from}:${to}`} tenantId={tenantId} seriesKey={series.key} from={from} to={to} valid={valid}/>
    {series.extensionLimited&&<p role="status" className="text-amber-700 dark:text-amber-400">{t("extensionLimited")}</p>}
    {archived&&<p>{t("archivedNote")}</p>}
    {!valid && <p role="alert">{t("invalidRange")}</p>}
    {error && <p role="alert" className="text-red-600">{(error as {code?: number}).code === 8 ? t("quota") : t("error")}</p>}
    {refresh.isSuccess && <p role="status">{t("requested")}</p>}
    {history.isPending && valid && <p role="status">{t("loading")}</p>}
    {valid&&<InvestmentPositions key={`positions:${series.key}:${to}`} tenantId={tenantId} seriesKey={series.key} date={to} generation={latest?.generation}/>}
    {valid&&<InvestmentComposition key={`composition:${series.key}:${from}:${to}`} tenantId={tenantId} seriesKey={series.key} from={from} to={to}/>}
    {valid&&<InvestmentPerformance key={`${series.key}:${from}:${to}`} tenantId={tenantId} seriesKey={series.key} from={from} to={to} currency={series.currency}/>}
    {valid&&<InvestmentBenchmarkComparison tenantId={tenantId} seriesKey={series.key} from={from} to={to}/>}
    {points.some(p => p.warnings.some(w => w.code === "reviewed_native_bond_valuation")) && <p role="status" className="text-sm text-amber-800 dark:text-amber-300">{t("issues.reviewed_native_bond_valuation")}</p>}
    {points.some(p => p.warnings.some(w => w.code === "reconstructed_price_valuation")) && <p role="status" className="text-sm text-amber-800 dark:text-amber-300">{t("issues.reconstructed_price_valuation")}</p>}
    {points.some(p => p.warnings.some(w => w.code === "retrospective_price_valuation")) && <p role="status" className="text-sm text-amber-800 dark:text-amber-300">{t("issues.retrospective_price_valuation")}</p>}
    <p className="text-sm text-gray-500">{t("method", {currency: series.currency, policy: series.fxPolicyKey})}</p>
    {valid && !history.isPending && !history.error && <>
      <div className="rounded-xl bg-gray-50 p-4 dark:bg-gray-900">
        <p className="text-sm">{t("lastDate", {date: latest?.date || to})}</p>
        <p className="mt-1 break-all text-2xl font-semibold tabular-nums">{latest?.state === "ready" && latest.knownValue ? `${navDecimal(latest.knownValue.decimal)} ${series.currency}` : "—"}</p>
        <p className="text-sm">{state(latest?.state || "unavailable")}</p>
      </div>
      <p className="text-sm">{t("gaps")}</p>
      {(geometry.dots.length > 0 || geometry.current) ? <figure>
        <div className="flex flex-wrap justify-between gap-2 text-xs tabular-nums"><span>{t("minimum")}: {navDecimal(geometry.min)} {series.currency}</span><span>{t("maximum")}: {navDecimal(geometry.max)} {series.currency}</span></div>
        <InvestmentChart role="img" aria-label={t("chart")} viewBox="0 0 800 260" className="w-full text-blue-600" data-testid="nav-chart">
          <title>{t("chart")}</title><desc>{t("gaps")}</desc>
          <path d="M20,240 L780,240" stroke="currentColor" opacity="0.2" />
          {geometry.paths.map((path, i) => <path data-testid="nav-segment" key={i} d={path} fill="none" stroke="currentColor" strokeWidth="2" />)}
          {geometry.connector&&<path data-testid="nav-current-connector" d={geometry.connector} fill="none" stroke="currentColor" strokeDasharray="5 5"/>}
          {geometry.current&&<circle data-testid="nav-current-point" cx={geometry.current.x} cy={geometry.current.y} r="7" fill="none" stroke="currentColor" strokeWidth="2"><title>{currentText("chartNow")}: {navDecimal(current.data?.knownValue?.decimal||"")} {series.currency}</title></circle>}
          {geometry.dots.map(p => <circle key={p.index} cx={p.x} cy={p.y} r="5" fill="currentColor"><title>{points[p.index].date}: {navDecimal(points[p.index].knownValue!.decimal)} {series.currency}</title></circle>)}
        </InvestmentChart>
        <figcaption className="flex justify-between text-xs text-gray-500"><span>{from}</span><span>{geometry.current?currentText("chartNow"):to}</span></figcaption>
      </figure> : <p>{t("noComplete")}</p>}
      <div className="hidden overflow-x-auto sm:block"><table className="w-full text-left text-sm"><caption className="py-3 text-left font-medium">{t("daily")}</caption><thead><tr>{["date", "value", "state", "details"].map(k => <th key={k} className="p-2">{t(k)}</th>)}</tr></thead><tbody>
        {points.slice(offset, offset + 50).map(p => <tr key={p.date} className="border-t border-gray-200 dark:border-gray-800">
          <td className="whitespace-nowrap p-2">{p.date}</td>
          <td className="p-2 font-mono tabular-nums">{["ready", "incomplete"].includes(p.state) && p.knownValue ? <>{navDecimal(p.knownValue.decimal)} {series.currency}{p.state === "incomplete" && <span className="block font-sans text-xs">{t("subtotal")}</span>}</> : "—"}</td>
          <td className="p-2">{state(p.state)}</td>
          <td className="max-w-xs break-words p-2">{p.issues.concat(p.warnings).map((i, n) => <p key={n}>{t.has(`issues.${i.code}`) ? t(`issues.${i.code}`) : t("issue", {code: i.code})}<span className="block break-all text-xs text-gray-500">{i.componentId}</span></p>)}</td>
        </tr>)}
      </tbody></table></div>
      <div className="space-y-3 sm:hidden" aria-label={t("daily")}>
        {points.slice(offset, offset + 50).map(p => <article key={p.date} className="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-800">
          <div className="flex flex-wrap justify-between gap-2"><span>{p.date}</span><span className="text-sm">{state(p.state)}</span></div>
          <p className="break-all font-mono tabular-nums">{["ready", "incomplete"].includes(p.state) && p.knownValue ? `${navDecimal(p.knownValue.decimal)} ${series.currency}` : "—"}</p>
          {p.state === "incomplete" && <p className="text-xs text-gray-500">{t("subtotal")}</p>}
          {p.issues.concat(p.warnings).map((i, n) => <p key={n} className="text-sm">{t.has(`issues.${i.code}`) ? t(`issues.${i.code}`) : t("issue", {code: i.code})}<span className="block break-all text-xs text-gray-500">{i.componentId}</span></p>)}
        </article>)}
      </div>
      <div className="flex gap-3"><button className={button} disabled={offset === 0} onClick={() => setOffset(offset - 50)}>{t("previous")}</button><button className={button} disabled={offset + 50 >= points.length} onClick={() => setOffset(offset + 50)}>{t("next")}</button></div>
    </>}
  </section>;
}

function Value({ tenantId }: { tenantId: string }) {
  const params=useSearchParams(),router=useRouter(),pathname=usePathname();
  const requested=params.get("series")||"";
  const selectSeries=(key:string)=>{const q=new URLSearchParams(params.toString());q.set("series",key);router.replace(`${pathname}?${q}`,{scroll:false});};
  const operationsTitle=useTranslations("investmentOperations");
  const payoutsTitle=useTranslations("investmentPayouts");
  const overviewTitle=useTranslations("investmentOverview");
  const groupsTitle = useTranslations("investmentGroups");
  const t = useTranslations("investmentNAV");
  const { investmentNAV: api } = useClients();
  const [portfolioAutoExtend,setPortfolioAutoExtend]=useState(false);
  const [portfolioArchived,setPortfolioArchived]=useState(false);
  const [portfolioTitle,setPortfolioTitle]=useState("");
  const [portfolioSeries, setPortfolioSeries] = useState<InvestmentNAVSeries>();
  const [after, setAfter] = useState("");
  const list = useQuery<{series: InvestmentNAVSeries[]; nextAfterKey: string}>({queryKey: ["investmentNAV", tenantId, "series", after], queryFn: () => api.listInvestmentNAVSeries({afterKey: after}),refetchInterval:30000});
  const currentSeries=useQuery<{series?:InvestmentNAVSeries}>({queryKey:["investmentNAV",tenantId,"seriesInfo",portfolioSeries?.key],enabled:!!portfolioSeries&&portfolioAutoExtend&&!portfolioArchived,queryFn:()=>api.getInvestmentNAVSeries({seriesKey:portfolioSeries!.key}),refetchInterval:30000});
  const watched=currentSeries.data?.series||list.data?.series.find(s=>s.key===portfolioSeries?.key);
  const shownPortfolio=portfolioSeries&&portfolioAutoExtend&&!portfolioArchived&&watched?{...portfolioSeries,toDate:watched.toDate>portfolioSeries.toDate?watched.toDate:portfolioSeries.toDate,extensionLimited:watched.extensionLimited}:portfolioSeries;
  const explicit=useQuery<{series?:InvestmentNAVSeries}>({queryKey:["investmentNAV",tenantId,"seriesInfo",requested],enabled:!!requested,queryFn:()=>api.getInvestmentNAVSeries({seriesKey:requested}),refetchInterval:30000});
  const current = requested?(explicit.isError?undefined:shownPortfolio?.key===requested?shownPortfolio:explicit.data?.series):shownPortfolio||list.data?.series[0];
  const viewingPortfolio=!!portfolioSeries&&current?.key===portfolioSeries.key;
  return <main className="mx-auto max-w-6xl space-y-6 px-4 py-8"><Link className="inline-block text-blue-600" href={`/investments?${params}`}>{overviewTitle("back")}</Link><Link className={button} href={`/investments/groups?${params}`}>{groupsTitle("title")}</Link>
    <Link href="/investments/operations" className="text-blue-600">{operationsTitle("title")}</Link>
    <Link href="/investments/payouts" className="text-blue-600">{payoutsTitle("link")}</Link>
    <Link href="/investments/journal" className="text-blue-600">{t("journal")}</Link>
    <header><h1 className="text-3xl font-semibold">{t("title")}</h1><p className="mt-2 text-gray-500">{t("intro")}</p></header>
    <InvestmentPortfolios tenantId={tenantId} onView={p=>{selectSeries(p.seriesKey);setPortfolioAutoExtend(p.autoExtend);setPortfolioArchived(p.archived);setPortfolioTitle(p.name);setPortfolioSeries(create(InvestmentNAVSeriesSchema,{key:p.seriesKey,ledgerIds:p.ledgerIds,currency:p.currency,fxPolicyKey:p.fxPolicyKey,fromDate:p.fromDate,toDate:p.toDate}));}}/>
    {currentSeries.error&&<p role="alert">{t("error")} <button className={button} onClick={()=>currentSeries.refetch()}>{t("reload")}</button></p>}
    {explicit.error&&<p role="alert">{t("error")} <button className={button} onClick={()=>explicit.refetch()}>{t("reload")}</button></p>}
    {explicit.isFetching&&requested&&<p role="status">{t("loading")}</p>}
    {list.isPending && <p role="status">{t("loading")}</p>}
    {list.error && <p role="alert">{t("error")} <button className={button} onClick={() => list.refetch()}>{t("reload")}</button></p>}
    {list.data?.series.length === 0 && <p>{t("empty")}</p>}
    {current && <>{viewingPortfolio&&portfolioTitle&&<h2 className="break-all text-xl font-semibold">{portfolioTitle}</h2>}<label className="block max-w-lg">{t("series")}<select className={field} value={current.key} onChange={e => {setPortfolioSeries(undefined);setPortfolioTitle("");setPortfolioArchived(false);setPortfolioAutoExtend(false);selectSeries(e.target.value);}}>{!list.data?.series.some(s=>s.key===current.key)&&<option value={current.key}>{current.currency} · {current.fxPolicyKey}</option>}{list.data?.series.map((s, i) => <option key={s.key} value={s.key}>{t("seriesLabel", {index: i + 1, count: s.ledgerIds.length, currency: s.currency, policy: s.fxPolicyKey})}</option>)}</select></label><SeriesValue key={`${current.key}:${current.fromDate}:${current.toDate}:${portfolioArchived}`} series={current} archived={viewingPortfolio&&portfolioArchived} tenantId={tenantId} /></>}
    {(after || list.data?.nextAfterKey) && <div className="flex gap-3"><button className={button} disabled={!after} onClick={() => {setAfter("");}}>{t("first")}</button><button className={button} disabled={!list.data?.nextAfterKey} onClick={() => {setAfter(list.data!.nextAfterKey);}}>{t("nextSeries")}</button></div>}
  </main>;
}
export default function ValuePage() { const tenantId = useAssetTenant(); return <Protected>{tenantId && <Value key={tenantId} tenantId={tenantId} />}</Protected>; }
