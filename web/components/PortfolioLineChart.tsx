"use client";
import {useEffect,useRef,useState} from "react";
import {chartDateTicks, chartSegments, type ChartPoint} from "@/lib/portfolio-chart";

type Series = {key: string; title: string; color: string; points: readonly ChartPoint[]; dashed?: boolean};
type Marker = {date: string; kind: "buy" | "sell"; label: string; value?: number};

export default function PortfolioLineChart({series, from, to, locale, currency, title, markers = [], reference, onInspect, precision=2, unit}: {
  series: Series[]; from: string; to: string; locale: string; currency?: string; title: string;
  precision?:number; unit?:string; markers?: Marker[]; reference?: {value: number; label: string}; onInspect?: (date: string) => void;
}) {
  const [hover, setHover] = useState<string>();
  const [width,setWidth]=useState(1000),container=useRef<HTMLDivElement>(null);
  const hasData=series.some(s=>s.points.some(p=>p.value!==undefined&&Number.isFinite(p.value)));
  useEffect(()=>{if(!container.current||typeof ResizeObserver==="undefined")return;const observer=new ResizeObserver(([entry])=>setWidth(Math.max(320,Math.round(entry.contentRect.width))));observer.observe(container.current);return()=>observer.disconnect();},[hasData]);
  useEffect(()=>setHover(undefined),[from,to]);
  const plotWidth=width-100;

  const values = series.flatMap(s => s.points.flatMap(p => p.value === undefined || !Number.isFinite(p.value) ? [] : [p.value]));
  if (reference && Number.isFinite(reference.value)) values.push(reference.value);
  if (!values.length) return <p role="status" className="flex h-80 items-center justify-center text-sm text-slate-500">{locale === "ru" ? "Нет данных за этот период" : "No data for this period"}</p>;
  const low = Math.min(...values), high = Math.max(...values), padding = (high - low || Math.abs(high) || 1) * .07;
  const min = low - padding, max = high + padding;
  const start = Date.parse(from), days = Math.max(1, (Date.parse(to) - start) / 86400000);
  const x = (date: string) => 76 + (Date.parse(date) - start) / 86400000 / days * plotWidth;
  const y = (n: number) => 18 + (max - n) / (max - min) * 278;
  const format = (n: number, compact = false) => new Intl.NumberFormat(locale, {maximumFractionDigits: compact ? 1 : precision, notation: compact ? "compact" : "standard", ...(currency ? {style: "currency", currency} : {})}).format(n)+(unit?" "+unit:"");
  const ticks = chartDateTicks(from, to,width<600?3:6), long = days > 1825;
  const hoverRows = hover ? series.flatMap(s => {const p = s.points.find(p => p.date === hover && p.value !== undefined); return p ? [{s, p}] : [];}) : [];
  const markerPoint = (marker: Marker) => marker.value ?? series[0]?.points.find(p => p.date === marker.date)?.value;
  const displayDate=(date:string)=>new Intl.DateTimeFormat(locale,{timeZone:"UTC",day:"numeric",month:"short",year:"2-digit"}).format(new Date(date+"T00:00:00Z"));
  return <div ref={container} className="relative" aria-label={title}>
    <p className="mb-2 text-right text-xs text-slate-500"><time dateTime={from}>{displayDate(from)}</time> — <time dateTime={to}>{displayDate(to)}</time></p>
    <svg viewBox={`0 0 ${width} 340`} className="h-80 w-full sm:h-96" preserveAspectRatio="none" role="img" aria-label={title} tabIndex={0}
      onKeyDown={e=>{if(["ArrowLeft","ArrowRight","Home","End"].includes(e.key)){e.preventDefault();const offset=hover?Math.round((Date.parse(hover)-start)/86400000):0;const next=e.key==="Home"?0:e.key==="End"?days:Math.max(0,Math.min(days,offset+(e.key==="ArrowLeft"?-1:1)));setHover(new Date(start+next*86400000).toISOString().slice(0,10));}else if(e.key==="Enter"&&hover){onInspect?.(hover);}}}
      onPointerLeave={() => setHover(undefined)}
      onPointerMove={e => {const bounds = e.currentTarget.getBoundingClientRect(); const position = (e.clientX - bounds.left) / bounds.width * width; if (position >= 76 && position <= width-24) setHover(new Date(start + Math.round((position - 76) / plotWidth * days) * 86400000).toISOString().slice(0, 10));}}
      onClick={() => {if (hover) onInspect?.(hover);}}>
      {Array.from({length: 5}, (_, i) => min + (max - min) * i / 4).map(n => <g key={n}>
        <line x1="76" x2={width-24} y1={y(n)} y2={y(n)} stroke="currentColor" strokeOpacity=".16" strokeDasharray="4 5" vectorEffect="non-scaling-stroke"/>
        <text x="65" y={y(n) + 4} textAnchor="end" className="fill-slate-500 dark:fill-slate-400" fontSize="11">{format(n, true)}</text>
      </g>)}
      {min < 0 && max > 0 && <line x1="76" x2={width-24} y1={y(0)} y2={y(0)} stroke="currentColor" strokeOpacity=".3" vectorEffect="non-scaling-stroke"/>}
      {series.flatMap((s,seriesIndex) => chartSegments(s.points).map((segment, i) => <g key={s.key+i}>
        {seriesIndex===0&&segment.length>1&&<polygon points={`${x(segment[0].date)},296 `+segment.map(p=>`${x(p.date)},${y(p.value!)}`).join(" ")+` ${x(segment.at(-1)!.date)},296`} fill={s.color} fillOpacity=".07"/>}
        <polyline points={segment.length === 1 ? `${x(segment[0].date) - 1},${y(segment[0].value!)} ${x(segment[0].date) + 1},${y(segment[0].value!)}` : segment.map(p => `${x(p.date)},${y(p.value!)}`).join(" ")} fill="none" stroke={s.color} strokeWidth="2" strokeDasharray={s.dashed ? "5 4" : undefined} vectorEffect="non-scaling-stroke"><title>{s.title}{segment[0].segment?.startsWith("partial:") ? (locale === "ru" ? " · известная часть" : " · known portion") : ""} · {segment[0].date}: {format(segment[0].value!)} — {segment.at(-1)!.date}: {format(segment.at(-1)!.value!)}</title></polyline>
      </g>))}

      {reference && <g><line x1="76" x2={width-24} y1={y(reference.value)} y2={y(reference.value)} stroke="#a78bfa" strokeDasharray="4 4" vectorEffect="non-scaling-stroke"/><text x="80" y={y(reference.value) - 7} fontSize="11" className="fill-violet-600 dark:fill-violet-300">{reference.label}: {format(reference.value)}</text></g>}
      {markers.filter(m => m.date >= from && m.date <= to).map((m, i) => {const price = markerPoint(m); return <circle key={i} cx={x(m.date)} cy={price === undefined ? 307 : y(price)} r="4" fill={m.kind === "buy" ? "#38bdf8" : "#fb7185"} stroke="white" strokeWidth="1.5"><title>{m.label}</title></circle>;})}
      {ticks.map((date, i) => <text key={date} x={x(date)} y="326" textAnchor={i === 0 ? "start" : i === ticks.length - 1 ? "end" : "middle"} fontSize="11" className="fill-slate-500 dark:fill-slate-400">{new Intl.DateTimeFormat(locale, {timeZone: "UTC", ...(long ? {year: "numeric"} : {day: "numeric", month: "short", ...(days > 180 ? {year: "2-digit"} : {})})}).format(new Date(date + "T00:00:00Z"))}</text>)}
      {hover && <line x1={x(hover)} x2={x(hover)} y1="18" y2="296" stroke="currentColor" strokeOpacity=".3" vectorEffect="non-scaling-stroke"/>}
    </svg>
    {hover && hoverRows.length > 0 && <div role="status" className="pointer-events-none absolute right-4 top-3 rounded-xl border border-slate-200 bg-white/95 px-4 py-3 text-xs shadow-sm dark:border-slate-600 dark:bg-slate-800/95"><p className="mb-2 text-slate-500">{hover}</p>{hoverRows.map(({s, p}) => <p key={s.key} style={{color: s.color}}>{s.title}: {format(p.value!)}</p>)}</div>}
    {series.length > 1 && <div className="flex flex-wrap justify-center gap-5 text-xs text-slate-500">{series.map(s => <span key={s.key} className="flex items-center gap-2"><span className="h-2.5 w-2.5 rounded-full" style={{background: s.color}}/>{s.title}</span>)}</div>}
  </div>;
}
