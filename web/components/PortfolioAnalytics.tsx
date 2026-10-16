"use client";
import type {GetInvestmentPerformanceResponse, TrackerHistoryPoint} from "@/proto/budget/v1/investment_nav_pb";
import {analyticsNumber, portfolioGrowthSeries, type AnalyticsMode} from "@/lib/portfolio-analytics";
import {shiftHistoryDate} from "@/lib/portfolio-history";
import type {ChartPoint} from "@/lib/portfolio-chart";
import PortfolioLineChart from "./PortfolioLineChart";

export default function PortfolioAnalytics({performance, comparisonPerformance, historyPoints = [], mode, from, to, ru, locale, benchmarkIds, onBenchmarksChange, onValueInspect}: {
  comparisonPerformance?:GetInvestmentPerformanceResponse; onValueInspect?: (date: string) => void; performance?: GetInvestmentPerformanceResponse; historyPoints?: readonly TrackerHistoryPoint[];
  mode: AnalyticsMode; from: string; to: string; ru: boolean; locale: string; benchmarkIds: string[]; onBenchmarksChange: (ids: string[]) => void;
}) {
  const tr = (r: string, e: string) => ru ? r : e;
  if (!performance?.analyticsPoints.length) return <p role="status" className="py-10 text-sm text-slate-500">{tr("Нет данных за этот период", "No data for this period")}</p>;
  const p = performance, indices = mode === "indices", bars = mode === "months" || mode === "years";
  const format = (n: number, compact = false) => new Intl.NumberFormat(locale, {style: "currency", currency: p.currency, maximumFractionDigits: compact ? 1 : 2, notation: compact ? "compact" : "standard"}).format(n);
  const known = new Map(historyPoints.map(d => [d.date, d]));
  const series: {key:string;title:string;color:string;dashed?:boolean;points:ChartPoint[]}[] = [{key: "portfolio", title: tr("Портфель", "Portfolio"), color: mode === "profit" ? "#55d0ad" : "#13b5ea", points: p.analyticsPoints.map(d => {
    let value = analyticsNumber(mode === "profit" ? d.profit : d.value);
    const partial = known.get(d.date);
    if (mode === "value" && value === undefined && partial?.state === "incomplete" && partial.pricedPositions > 0) value = analyticsNumber(partial.knownValue);
    return {date: d.date, value, segment: partial?.state === "incomplete" ? "partial:" + partial.coverageKey : "complete"};
  })}, ...(mode === "value" ? [{key: "invested", title: tr("Вложения", "Invested"), color: "#a78bfa", dashed: true, points: p.analyticsPoints.map(d => ({date: d.date, value: analyticsNumber(d.invested)}))}] : []),
  ...p.trackerBenchmarks.filter(b => benchmarkIds.includes(b.key)).map(b => ({key: b.key, title: b.key === "imoex" ? "IMOEX" : "SPY · S&P 500", color: b.key === "imoex" ? "#ffa638" : "#ec70ad", points: b.points.map(d => ({date: d.date, value: analyticsNumber(mode === "profit" ? d.virtualProfit : d.virtualValue)}))}))];
  const growth=indices?portfolioGrowthSeries(p,benchmarkIds,shiftHistoryDate(from,-1),comparisonPerformance):undefined;
  if(growth)series.splice(0,series.length,...growth.lines.map(s=>({...s,title:s.key==="portfolio"?tr("Портфель","Portfolio"):s.title})));

  const reasonText=(reason:string)=>(reason.includes("flow")||reason==="benchmark_baseline_unavailable"&&p.reason.includes("flow"))?tr("Неполная история сделок или выплат","Incomplete trade or payout history"):reason.includes("fx")?tr("Нет исторического курса","Historical exchange rate missing"):reason.includes("reset")?tr("Нет истории до начального остатка","History before opening balance missing"):tr("Не хватает исторической цены или курса","Historical price or exchange rate missing");
  const empty=bars?!amountsAvailable():!series.some(s=>s.points.some(d=>d.value!==undefined&&d.date>=from));
  function amountsAvailable(){return (mode==="years"?p.yearlyProfit:p.monthlyProfit).some(d=>d.profit!==undefined);}
  const periods = mode === "years" ? p.yearlyProfit : p.monthlyProfit;
  const amounts = periods.map(d => analyticsNumber(d.profit));
  const max = Math.max(1, ...amounts.map(n => Math.abs(n || 0)));
  return <div aria-label={tr("Денежная аналитика", "Monetary analytics")} className="space-y-4">
    {!bars && <div className="flex flex-wrap items-center gap-4 text-sm text-slate-500"><span>{tr("Сравнить:", "Compare:")}</span>{p.trackerBenchmarks.map(b => <label key={b.key} className="flex items-center gap-2"><input type="checkbox" checked={benchmarkIds.includes(b.key)} onChange={e => onBenchmarksChange(e.target.checked ? [...benchmarkIds, b.key] : benchmarkIds.filter(id => id !== b.key))}/>{b.key === "imoex" ? "IMOEX" : "SPY · S&P 500"}{!indices&&b.points.some(d=>d.date>=from&&d.virtualReason)&&<span className="text-xs" title={reasonText(b.points.find(d=>d.date>=from&&d.virtualReason)?.virtualReason||b.reason)}>{tr("· неполно","· incomplete")}</span>}</label>)}</div>}
    {!bars&&p.trackerBenchmarks.filter(b=>benchmarkIds.includes(b.key)).map(b=>{const visible=b.points.some(d=>analyticsNumber(indices?d.index:mode==="profit"?d.virtualProfit:d.virtualValue)!==undefined&&d.date>=from);return !visible?<p key={b.key} className="text-xs text-slate-500">{b.key.toUpperCase()}: {reasonText(b.reason==="benchmark_fx_missing"?b.reason:b.points.find(d=>d.date>=from)?.virtualReason||b.reason)}</p>:null;})}
    {indices&&growth?.baseline&&<p className="text-xs text-slate-500">{tr("Начало сравнения:","Comparison starts:")} {growth.baseline}</p>}
    {empty?<p role="status" className="flex h-80 items-center justify-center text-sm text-slate-500">{indices?tr("Нет общих данных для выбранных индексов","No common data for the selected indices"):reasonText(p.analyticsPoints.find(d=>d.date>=from&&d.reason)?.reason||p.reason)}</p>:bars ? <div className="overflow-x-auto"><svg viewBox={`0 0 ${Math.max(1000, periods.length * 45)} 350`} className="h-80 min-w-full" style={{width:Math.max(1000,periods.length*45)}} role="img" aria-label={mode === "years" ? tr("Прибыль по годам", "Yearly profit") : tr("Прибыль по месяцам", "Monthly profit")}>
      {[-1, -.5, 0, .5, 1].map(n => <g key={n}><line x1="80" x2={Math.max(1000, periods.length * 45) - 20} y1={162 - n * 132} y2={162 - n * 132} stroke="currentColor" strokeOpacity=".16" strokeDasharray="4 5"/><text x="68" y={166 - n * 132} textAnchor="end" fontSize="11" className="fill-slate-500 dark:fill-slate-400">{format(n * max, true)}</text></g>)}
      {periods.map((d, i) => {const value = amounts[i], width = (Math.max(1000, periods.length * 45) - 100) / Math.max(1, periods.length), center = 80 + (i + .5) * width; return <g key={d.key}>
        {value === undefined ? <text x={center} y="158" textAnchor="middle" fontSize="11" className="fill-slate-500">—</text> : <><rect x={center - width * .3} y={value >= 0 ? 162 - value / max * 132 : 162} width={width * .6} height={Math.max(1, Math.abs(value) / max * 132)} rx="4" fill={value >= 0 ? "#55d0ad" : "#f36368"}><title>{d.key}: {format(value)}</title></rect>{periods.length <= 24 && <text x={center} y={value >= 0 ? 152 - value / max * 132 : 180 - value / max * 132} textAnchor="middle" fontSize="10" fill={value >= 0 ? "#55d0ad" : "#f36368"}>{format(value, true)}</text>}</>}
        {i % Math.max(1, Math.ceil(periods.length / 12)) === 0 && <text x={center} y="332" textAnchor="middle" fontSize="11" className="fill-slate-500 dark:fill-slate-400">{mode === "years" ? d.key : new Intl.DateTimeFormat(locale, {month: "short", year: "2-digit", timeZone: "UTC"}).format(new Date(d.fromDate + "T00:00:00Z"))}</text>}
      </g>;})}
    </svg><div className="sr-only"><table><caption>{tr("Прибыль по периодам", "Profit by period")}</caption><thead><tr><th>{tr("Период", "Period")}</th><th>{tr("Прибыль", "Profit")}</th></tr></thead><tbody>{periods.map((d,i)=><tr key={d.key}><td>{d.fromDate} — {d.toDate}</td><td>{amounts[i]===undefined?reasonText(d.reason):format(amounts[i]!)}</td></tr>)}</tbody></table></div></div> : <PortfolioLineChart series={series} from={shiftHistoryDate(from, -1)} to={to} locale={locale} currency={indices?undefined:p.currency} unit={indices?"%":undefined} title={indices?tr("Сравнение роста, %","Growth comparison, %"):mode === "profit" ? tr("Накопленная прибыль", "Cumulative profit") : tr("Стоимость и вложения", "Value and invested")} onInspect={onValueInspect}/>}
    {!!series[0]&&series[0].points.some(d => d.value === undefined || d.segment?.startsWith("partial:")) && !bars && !indices && <span className="inline-block rounded-full bg-amber-50 px-2 py-1 text-xs text-amber-700 dark:bg-amber-950 dark:text-amber-300" title={tr("Неполная стоимость показана пунктиром; отсутствующие значения не соединяются.", "Partial value is dashed; missing values are not connected.")}>{tr("Неполные данные", "Incomplete data")}</span>}
  </div>;
}
