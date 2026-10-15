"use client";
import type {TrackerInstrumentPricePoint} from "@/proto/budget/v1/investment_nav_pb";
import type {InvestmentOperation} from "@/proto/budget/v1/investment_pb";
import {reportingDate} from "@/lib/portfolio-report";
import {shiftHistoryDate} from "@/lib/portfolio-history";
type Mode="quote"|"nativeClean"|"clean"|"nativeDirty"|"dirty";
export default function PortfolioInstrumentChart({points,operations,mode,from,to,ru}:{points:TrackerInstrumentPricePoint[];operations:InvestmentOperation[];mode:Mode;from:string;to:string;ru:boolean}){
 const tr=(r:string,e:string)=>ru?r:e;
 const value=(p:TrackerInstrumentPricePoint)=>{const v=mode==="quote"?p.raw:p[mode];if(!v)return undefined;const n=Number(v.numerator)/Number(v.denominator);return Number.isFinite(n)?n:undefined;};
 const units=new Set(points.filter(p=>value(p)!==undefined).map(p=>mode==="clean"||mode==="dirty"?"display":mode==="quote"&&p.unit==="percent_of_nominal"?"%":p.nativeCurrency+"/"+(mode==="quote"?p.unit:"money")));
 if(units.size>1)return <p role="status" className="py-10 text-sm text-amber-600">{tr("В ряду меняются валюта или единицы. Выберите отдельный листинг или пересчёт в выбранную валюту.","The series changes currency or units. Select a listing or convert into the display currency.")}</p>;
 const known=points.map((p,i)=>({p,i,v:value(p)})).filter(p=>p.v!==undefined),numbers=known.map(p=>p.v!),min=numbers.length?Math.min(...numbers):0,max=numbers.length?Math.max(...numbers):1;
 const xy=(p:TrackerInstrumentPricePoint)=>`${30+(Date.parse(p.date)-Date.parse(from))/86400000/Math.max(1,(Date.parse(to)-Date.parse(from))/86400000)*740},${20+(max-value(p)!)/(max-min||1)*180}`;
 const segments:TrackerInstrumentPricePoint[][]=[];for(const {p} of known){const last=segments.at(-1);if(last&&shiftHistoryDate(last.at(-1)!.date,1)===p.date&&last.at(-1)!.source===p.source)last.push(p);else segments.push([p]);}
 const trades=operations.filter(o=>["buy","sell"].includes(o.kind)&&o.occurredAt).map(o=>({o,date:reportingDate(o.occurredAt!.seconds)})).filter(p=>p.date>=from&&p.date<=to);
 return !known.length?<p role="status" className="py-10 text-center text-sm text-slate-500">{tr("Нет подтверждённых значений для выбранного вида цены.","No confirmed values are available for this price view.")}</p>:<div><div className="flex justify-between text-xs text-slate-500"><span>{min.toFixed(4)}</span><span>{max.toFixed(4)}</span></div><svg className="h-56 w-full" viewBox="0 0 800 240" preserveAspectRatio="none" role="img" aria-label={tr("Дневная история цены","Daily price history")}>
 {segments.map((s,n)=><g key={n}><polyline points={s.map(xy).join(" ")} fill="none" stroke="#10b981" strokeWidth="2" vectorEffect="non-scaling-stroke"/>{s.map(p=>{const[cx,cy]=xy(p).split(",");return <circle key={p.date} cx={cx} cy={cy} r={s.length===1?3:2} fill="#10b981"><title>{p.date}: {value(p)!.toFixed(6)} · {p.source} · {p.candleDate}</title></circle>;})}</g>)}
 {trades.map(({o,date})=><circle key={o.id} cx={30+(Date.parse(date)-Date.parse(from))/86400000/Math.max(1,(Date.parse(to)-Date.parse(from))/86400000)*740} cy={228} r={4} fill={o.kind==="buy"?"#38bdf8":"#f97316"}><title>{date} · {o.kind==="buy"?tr("Покупка","Buy"):tr("Продажа","Sell")} · {o.quantity} · {o.amount} {o.currency}</title></circle>)}
 </svg><div className="flex justify-between text-xs text-slate-500"><span>{from}</span><span>{to}</span></div>{trades.length>0&&<p className="mt-2 text-xs text-slate-500">{tr("Метки внизу: покупки — синие, продажи — оранжевые. Их высота не означает цену сделки.","Bottom markers: buys are blue, sells orange. Their height is not the execution price.")}</p>}</div>;
}
