"use client";
import {useState} from "react";
import type {GetInvestmentPerformanceResponse} from "@/proto/budget/v1/investment_nav_pb";
import {shiftHistoryDate} from "@/lib/portfolio-history";

type Point = {date: string; value?: number; segment?: number};
export default function PortfolioReturns({performance,from,to,ru,benchmarkIds,onBenchmarksChange}:{benchmarkIds?:string[];onBenchmarksChange?:(ids:string[])=>void;performance?:GetInvestmentPerformanceResponse;from:string;to:string;ru:boolean}) {
 const [local,setLocal]=useState(["imoex","spy"]),selected=benchmarkIds||local,setSelected=onBenchmarksChange||setLocal,tr=(r:string,e:string)=>ru?r:e;
 const numeric=(r?:{numerator:string;denominator:string})=>{if(!r)return undefined;const n=Number(r.numerator)/Number(r.denominator);return Number.isFinite(n)?n:undefined;};
 const portfolio:Point[]=(performance?.twrPoints||[]).map(p=>({date:p.date,segment:p.segment,value:p.segment===1?numeric(p.index):undefined}));
 const lines=[{key:"portfolio",title:tr("Портфель · TWR","Portfolio · TWR"),color:"#10b981",points:portfolio},...(performance?.trackerBenchmarks||[]).filter(b=>selected.includes(b.key)).map(b=>({key:b.key,title:b.title,color:b.key==="imoex"?"#38bdf8":"#a78bfa",points:b.points.map(p=>({date:p.date,value:numeric(p.index)}))}))];
 const values=lines.flatMap(l=>l.points.flatMap(p=>p.value===undefined?[]:[p.value]));
 const min=values.length?Math.min(...values):100,max=values.length?Math.max(...values):100;
 const first=shiftHistoryDate(from,-1),span=Math.max(1,(Date.parse(to)-Date.parse(first))/86400000);
 const xy=(p:Point)=>`${30+(Date.parse(p.date)-Date.parse(first))/86400000/span*740},${20+(max-p.value!)/(max-min||1)*180}`;
 const segments=(points:Point[])=>{const result:Point[][]=[];for(const p of points){if(p.value===undefined)continue;const last=result.at(-1);if(last&&shiftHistoryDate(last.at(-1)!.date,1)===p.date&&last.at(-1)!.segment===p.segment)last.push(p);else result.push([p]);}return result;};
 const base=performance?.twrPoints.find(p=>p.state==="baseline")?.date||first;
 return <div className="space-y-3" aria-label={tr("График доходности","Return chart")}>
 <div className="flex flex-wrap gap-3 text-xs"><span className="font-medium text-emerald-500">{tr("Портфель · TWR","Portfolio · TWR")}</span>{(performance?.trackerBenchmarks||[]).map(b=><label key={b.key} className="flex items-center gap-1"><input type="checkbox" checked={selected.includes(b.key)} onChange={e=>setSelected(e.target.checked?[...selected,b.key]:selected.filter(k=>k!==b.key))}/>{b.key==="imoex"?"IMOEX":tr("SPY · ETF на S&P 500","SPY · S&P 500 ETF")}</label>)}</div>
 <p className="text-xs text-slate-500">{tr("База 100","Base 100")}: {base}. {tr("Сравнение цен без дивидендов, с дневным пересчётом в выбранную валюту. После разрыва TWR новая цепочка здесь не соединяется с прежней.","Price-only comparisons excluding dividends, converted daily into the selected currency. A restarted TWR chain is not joined to the previous one.")}</p>
 {!values.length?<p role="status" className="py-10 text-center text-sm text-slate-500">{tr("Недостаточно истории для графика доходности.","Insufficient history for the return chart.")}</p>:<><div className="flex justify-between text-xs text-slate-500"><span>{min.toFixed(2)}</span><span>{max.toFixed(2)}</span></div><svg viewBox="0 0 800 220" preserveAspectRatio="none" className="h-52 w-full" role="img" aria-label={tr("Доходность от базы 100","Returns normalized to 100")}>
 {lines.flatMap(l=>segments(l.points).map((s,i)=><g key={l.key+":"+i}><polyline points={s.map(xy).join(" ")} fill="none" stroke={l.color} strokeWidth="2" vectorEffect="non-scaling-stroke"/>{s.map(p=>{const[cx,cy]=xy(p).split(",");return <circle key={p.date} cx={cx} cy={cy} r={s.length===1?3:2} fill={l.color}><title>{l.title} · {p.date}: {p.value!.toFixed(4)}</title></circle>;})}</g>))}
 </svg><div className="flex justify-between text-xs text-slate-500"><span>{first}</span><span>{to}</span></div></>}
 <div className="space-y-1 text-xs text-slate-500">{(performance?.trackerBenchmarks||[]).map(b=><p key={b.key}><a className="underline" href={b.sourceUrl} target="_blank" rel="noreferrer">{b.key==="imoex"?tr("Источник: ПАО Московская Биржа","Source: Moscow Exchange"):"SPDR S&P 500 ETF Trust"}</a> · {b.nativeCurrency} · {b.kind==="price_index"?tr("ценовой индекс","price index"):tr("ценовой ряд ETF, не индекс","ETF price series, not the index")}{b.reason==="benchmark_fx_missing"?tr(" · нет курса на базовую дату"," · base-date FX missing"):b.reason==="benchmark_price_missing"?tr(" · нет цены на базовую дату"," · base-date price missing"):b.reason?tr(" · история неполная"," · incomplete history"):""}</p>)}</div>
 </div>;
}
