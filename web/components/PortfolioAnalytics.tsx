"use client";
import type {GetInvestmentPerformanceResponse, TrackerHistoryPoint} from "@/proto/budget/v1/investment_nav_pb";
import {analyticsNumber, portfolioGrowthSeries, type AnalyticsMode} from "@/lib/portfolio-analytics";
import {historyPointVisible,shiftHistoryDate} from "@/lib/portfolio-history";
import type {ChartPoint} from "@/lib/portfolio-chart";
import PortfolioLineChart from "./PortfolioLineChart";
import PortfolioChartLoading from "./PortfolioChartLoading";
import PortfolioPeriodChart from "./PortfolioPeriodChart";

export default function PortfolioAnalytics({performance, comparisonPerformance, historyPoints = [], mode, from, to, ru, locale, benchmarkIds, onBenchmarksChange, onValueInspect, loading=false, instrumentNames, currency}: {
  loading?:boolean; instrumentNames?:Map<string,string>; currency?:string;
  comparisonPerformance?:GetInvestmentPerformanceResponse; onValueInspect?: (date: string) => void; performance?: GetInvestmentPerformanceResponse; historyPoints?: readonly TrackerHistoryPoint[];
  mode: AnalyticsMode; from: string; to: string; ru: boolean; locale: string; benchmarkIds: string[]; onBenchmarksChange: (ids: string[]) => void;
}) {
  const tr = (r: string, e: string) => ru ? r : e;
  if (loading) {
    if(mode==="value"&&historyPoints.some(historyPointVisible))return <div className="space-y-3">
      <p className="text-xs text-slate-500">{tr("Загруженная часть истории. Вложения и доходность рассчитываются.","Loaded history window. Invested capital and returns are being calculated.")}</p>
      <PortfolioLineChart from={from} to={to} currency={currency} locale={locale} title={tr("Стоимость портфеля · загрузка","Portfolio value · loading")} series={[{key:"portfolio",title:tr("Портфель","Portfolio"),color:"#13b5ea",points:historyPoints.map(p=>({date:p.date,value:historyPointVisible(p)?analyticsNumber(p.knownValue):undefined,segment:p.state==="incomplete"?"partial:"+p.coverageKey:"complete"}))}]}/>
    </div>;
    return <PortfolioChartLoading ru={ru}/>;
  }
  if (!performance?.analyticsPoints.length) return <p role="status" className="py-10 text-sm text-slate-500">{tr("Нет данных за этот период", "No data for this period")}</p>;
  const p = performance, indices = mode === "indices", bars = mode === "months" || mode === "years";

  const known = new Map(historyPoints.map(d => [d.date, d]));
  const series: {key:string;title:string;color:string;dashed?:boolean;points:ChartPoint[]}[] = [{key: "portfolio", title: tr("Портфель", "Portfolio"), color: mode === "profit" ? "#55d0ad" : "#13b5ea", points: p.analyticsPoints.map(d => {
    let value = analyticsNumber(mode === "profit" ? d.profit : d.value);
    const partial = known.get(d.date);
    if (mode === "value" && value === undefined && partial?.state === "incomplete" && partial.pricedPositions > 0) value = analyticsNumber(partial.knownValue);
    return {date: d.date, value, segment: partial?.state === "incomplete" ? "partial:" + partial.coverageKey : "complete"};
  })}, ...(mode === "value" ? [{key: "invested", title: tr("Чистые вложения", "Net invested"), color: "#a78bfa", dashed: true, points: p.analyticsPoints.map(d => ({date: d.date, value: analyticsNumber(d.recordedCapital)}))}] : []),
  ...p.trackerBenchmarks.filter(b => benchmarkIds.includes(b.key)).map(b => ({key: b.key, title: b.key === "imoex" ? "IMOEX" : "SPY · S&P 500", color: b.key === "imoex" ? "#ffa638" : "#ec70ad", points: b.points.map(d => ({date: d.date, value: analyticsNumber(mode === "profit" ? d.virtualProfit : d.virtualValue)}))}))];
  const growth=indices?portfolioGrowthSeries(comparisonPerformance||p,benchmarkIds,shiftHistoryDate(from,-1),comparisonPerformance):undefined;
  if(growth)series.splice(0,series.length,...growth.lines.map(s=>({...s,title:s.key==="portfolio"?tr("Портфель","Portfolio"):s.title})));

  const reasonText=(reason:string)=>(reason.includes("flow")||reason==="benchmark_baseline_unavailable"&&p.reason.includes("flow"))?tr("Неполная история сделок или выплат","Incomplete trade or payout history"):reason.includes("fx")?tr("Нет исторического курса","Historical exchange rate missing"):reason.includes("reset")?tr("Нет истории до начального остатка","History before opening balance missing"):tr("Не хватает исторической цены или курса","Historical price or exchange rate missing");
  const empty=bars?!amountsAvailable():!series.some(s=>s.points.some(d=>d.value!==undefined&&d.date>=from));
  function amountsAvailable(){return (mode==="years"?p.yearlyProfit:p.monthlyProfit).some(d=>d.profit!==undefined||d.knownProfit!==undefined);}
  const periods = mode === "years" ? p.yearlyProfit : p.monthlyProfit;
  return <div aria-label={tr("Денежная аналитика", "Monetary analytics")} className="space-y-4">
    {!bars && <div className="flex flex-wrap items-center gap-4 text-sm text-slate-500"><span>{tr("Сравнить:", "Compare:")}</span>{p.trackerBenchmarks.map(b => <label key={b.key} className="flex items-center gap-2"><input type="checkbox" checked={benchmarkIds.includes(b.key)} onChange={e => onBenchmarksChange(e.target.checked ? [...benchmarkIds, b.key] : benchmarkIds.filter(id => id !== b.key))}/>{b.key === "imoex" ? "IMOEX" : "SPY · S&P 500"}{!indices&&b.points.some(d=>d.date>=from&&d.virtualReason)&&<span className="text-xs" title={reasonText(b.points.find(d=>d.date>=from&&d.virtualReason)?.virtualReason||b.reason)}>{tr("· неполно","· incomplete")}</span>}</label>)}</div>}
    {!bars&&p.trackerBenchmarks.filter(b=>benchmarkIds.includes(b.key)).map(b=>{const visible=b.points.some(d=>analyticsNumber(indices?d.index:mode==="profit"?d.virtualProfit:d.virtualValue)!==undefined&&d.date>=from);return !visible?<p key={b.key} className="text-xs text-slate-500">{b.key.toUpperCase()}: {reasonText(b.reason==="benchmark_fx_missing"?b.reason:b.points.find(d=>d.date>=from)?.virtualReason||b.reason)}</p>:null;})}
    {indices&&comparisonPerformance?.twrReason&&growth?.lines.some(s=>s.key==="portfolio")&&<p className="text-xs text-amber-700 dark:text-amber-300">{tr("Показан доступный непрерывный участок доходности портфеля; разрыв истории не соединяется.","Showing the available continuous portfolio-return segment; history gaps are not joined.")}</p>}
    {!bars&&<div className="flex flex-wrap gap-x-6 gap-y-2 text-sm" aria-label={tr("Результат за период","Period result")}>{series.map(s=>{const last=s.points.findLast(d=>d.date<=to&&d.value!==undefined);return last?<span key={s.key} style={{color:s.color}} title={s.key==="invested"?tr("Покупки и расходы минус продажи и полученные выплаты за всю историю","Purchases and expenses minus sales and received payouts over the full recorded history"):undefined}>{s.title}: <strong className="tabular-nums">{new Intl.NumberFormat(locale,{maximumFractionDigits:2,...(indices?{}:{style:"currency",currency:p.currency})}).format(last.value!)}{indices?" %":""}</strong>{last.date!==to&&<span className="ml-1 text-xs text-slate-500">{tr("на","as of")} {last.date}</span>}</span>:null;})}</div>}
    {indices&&growth?.baseline&&<p className="text-xs text-slate-500">{tr("Начало сравнения:","Comparison starts:")} {growth.baseline}</p>}
    {empty?<p role="status" className="flex h-80 items-center justify-center text-sm text-slate-500">{indices?tr("Нет общих данных для выбранных индексов","No common data for the selected indices"):reasonText(p.analyticsPoints.find(d=>d.date>=from&&d.reason)?.reason||p.reason)}</p>:bars ? <PortfolioPeriodChart periods={periods} currency={p.currency} locale={locale} yearly={mode==="years"} names={instrumentNames}/> : <PortfolioLineChart series={series} from={shiftHistoryDate(from, -1)} to={to} locale={locale} currency={indices?undefined:p.currency} unit={indices?"%":undefined} title={indices?tr("Сравнение роста, %","Growth comparison, %"):mode === "profit" ? tr("Накопленная прибыль", "Cumulative profit") : tr("Стоимость и вложения", "Value and invested")} onInspect={onValueInspect}/>}
    {!!series[0]&&series[0].points.some(d => d.value === undefined || d.segment?.startsWith("partial:")) && !bars && !indices && <span className="inline-block rounded-full bg-amber-50 px-2 py-1 text-xs text-amber-700 dark:bg-amber-950 dark:text-amber-300" title={tr("Показана известная часть стоимости; отсутствующие значения не соединяются.", "Known value is shown; missing values are not connected.")}>{tr("Неполные данные", "Incomplete data")}</span>}
  </div>;
}
