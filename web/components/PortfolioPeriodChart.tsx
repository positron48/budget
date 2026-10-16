"use client";
import {useState,useRef,useEffect} from "react";
import type {TrackerAnalyticsPeriod} from "@/proto/budget/v1/investment_nav_pb";
import {analyticsNumber} from "@/lib/portfolio-analytics";
import {chartScale} from "@/lib/portfolio-chart";

export default function PortfolioPeriodChart({periods,currency,locale,yearly,names,title:customTitle}:{periods:TrackerAnalyticsPeriod[];currency:string;locale:string;yearly:boolean;names?:Map<string,string>;title?:string}) {
 const ru=locale==="ru",tr=(r:string,e:string)=>ru?r:e;
 const [pointer,setPointer]=useState<number>(),[focused,setFocused]=useState<number>(),[viewport,setViewport]=useState(640),plot=useRef<HTMLDivElement>(null);
 useEffect(()=>{if(!plot.current||typeof ResizeObserver==="undefined")return;const observer=new ResizeObserver(([entry])=>setViewport(Math.max(1,Math.round(entry.contentRect.width))));observer.observe(plot.current);return()=>observer.disconnect();},[]);
 const periodKey=periods.map(p=>p.key).join("|");
 useEffect(()=>{setPointer(undefined);setFocused(undefined);if(plot.current)plot.current.scrollLeft=plot.current.scrollWidth;},[periodKey,yearly]);
 const hover=focused??pointer;
 const values=periods.map(p=>analyticsNumber(p.profit||p.knownProfit));
 const scale=chartScale(Math.min(0,...values.map(n=>n??0)),Math.max(0,...values.map(n=>n??0)));
 const y=(n:number)=>20+(scale.max-n)/(scale.max-scale.min)*250;
 const format=(n:number,compact=false)=>new Intl.NumberFormat(locale,{style:"currency",currency,maximumFractionDigits:compact?1:2,notation:compact?"compact":"standard"}).format(n);
 const width=Math.max(viewport,periods.length*64),slot=width/Math.max(1,periods.length);
 const title=customTitle||(yearly?tr("Прибыль по годам","Yearly profit"):tr("Прибыль по месяцам","Monthly profit"));
 const label=(p:TrackerAnalyticsPeriod)=>yearly?p.key:new Intl.DateTimeFormat(locale,{month:"short",year:"2-digit",timeZone:"UTC"}).format(new Date(p.fromDate+"T00:00:00Z"));
 const partial=periods.filter(p=>!p.profit&&p.knownProfit).length;
 const description=(i:number)=>{const p=periods[i],v=values[i];return `${label(p)}: ${v===undefined?tr("Нет оценки на границе периода","Boundary valuation missing"):format(v)}${!p.profit&&p.knownProfit?" · "+tr("известная часть","known portion"):""}`;};
 return <div className="relative space-y-3">
  {partial>0&&<p className="text-xs text-amber-700 dark:text-amber-300">{tr("Частичная оценка месяцев:","Partially valued periods:")} {partial}</p>}
  <div className="flex min-w-0" aria-label={title}>
   <svg viewBox="0 0 100 320" className="h-80 w-[100px] shrink-0" aria-label={tr("Шкала прибыли","Profit scale")}>
    {scale.ticks.map(n=><text key={n} x="92" y={y(n)+4} textAnchor="end" fontSize="11" className="fill-slate-500 dark:fill-slate-400">{format(n,true)}</text>)}
   </svg>
   <div ref={plot} className="min-w-0 flex-1 overflow-x-auto" tabIndex={0} aria-label={tr("Прокрутить периоды","Scroll periods")}>
    <svg viewBox={`0 0 ${width} 320`} style={{width}} className="h-80" role="img" aria-label={title} onPointerLeave={()=>setPointer(undefined)}>
     {scale.ticks.map(n=><line key={n} x1="0" x2={width} y1={y(n)} y2={y(n)} stroke="currentColor" strokeOpacity={n===0 ? .3 : .16} strokeDasharray={n===0?undefined:"4 5"}/>)}
     {periods.map((p,i)=>{const v=values[i],x=(i+.5)*slot,partial=!p.profit&&!!p.knownProfit;return <g key={p.key} data-period={p.key} tabIndex={0} aria-label={description(i)} onFocus={()=>setFocused(i)} onBlur={()=>setFocused(undefined)} onPointerEnter={()=>setPointer(i)}>
      <rect x={i*slot} y="0" width={slot} height="280" fill="transparent"/>
      {v===undefined?<text x={x} y={y(0)-8} textAnchor="middle" fontSize="12" className="fill-slate-500">—</text>:<rect x={x-slot*.3} y={v>=0?y(v):y(0)} width={slot*.6} height={Math.max(2,Math.abs(y(v)-y(0)))} rx="4" fill={v>=0?"#55d0ad":"#f36368"} fillOpacity={partial ? .65 : 1} stroke={partial?"currentColor":undefined} strokeDasharray={partial?"3 3":undefined}><title>{description(i)}</title></rect>}
      <text x={x} y="304" textAnchor="middle" fontSize="11" className="fill-slate-500 dark:fill-slate-400">{label(p)}</text>
     </g>;})}
    </svg>
   </div>
  </div>
  {hover!==undefined&&periods[hover]&&<div role="tooltip" className="pointer-events-none absolute right-3 top-8 z-10 max-w-sm rounded-xl border border-slate-200 bg-white/95 p-3 text-sm shadow dark:border-slate-600 dark:bg-slate-800/95">
   <p className="font-medium">{description(hover)}</p><p className="mt-1 text-xs text-slate-500">{periods[hover].fromDate} — {periods[hover].toDate}</p>
   {!!periods[hover].excludedInstrumentKeys.length&&<p className="mt-2 text-xs">{tr("Без оценки:","Unvalued:")} {periods[hover].excludedInstrumentKeys.map(k=>names?.get(k)||k).join(", ")}</p>}
  </div>}
  <div className="sr-only"><table><caption>{tr("Прибыль по периодам","Profit by period")}</caption><thead><tr><th>{tr("Период","Period")}</th><th>{tr("Прибыль","Profit")}</th></tr></thead><tbody>{periods.map((p,i)=><tr key={p.key}><td>{p.fromDate} — {p.toDate}</td><td>{description(i)}</td></tr>)}</tbody></table></div>
 </div>;
}
