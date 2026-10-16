"use client";
import {useState,type ReactNode} from "react";
export type DonutSlice={key:string;name:string;value:number;color:string};
export default function PortfolioDonut({slices,currency,locale,title,onSelect,children}:{slices:DonutSlice[];currency:string;locale:string;title:string;onSelect?:(key:string)=>void;children?:ReactNode}){
 const [active,setActive]=useState<string>();
 const total=slices.reduce((sum,s)=>sum+Math.max(0,s.value),0);
 let angle=-Math.PI/2;
 const point=(a:number,r:number)=>`${100+Math.cos(a)*r} ${100+Math.sin(a)*r}`;
 const money=(v:number)=>new Intl.NumberFormat(locale,{style:"currency",currency,maximumFractionDigits:2}).format(v);
 const share=(v:number)=>new Intl.NumberFormat(locale,{style:"percent",maximumFractionDigits:2}).format(total>0?v/total:0);
 const row=slices.find(s=>s.key===active);
 return <div className="relative aspect-square w-full max-w-72">
  <svg viewBox="0 0 200 200" role="img" aria-label={title} className="h-full w-full" onPointerLeave={()=>setActive(undefined)}>
   <circle cx="100" cy="100" r="76" fill="none" stroke="#94a3b8" strokeOpacity=".15" strokeWidth="36"/>
   {slices.filter(s=>s.value>0).map(s=>{const start=angle,span=s.value/total*Math.PI*2,mid=start+span/2,end=start+span;angle=end;
    const path=`M ${point(start,94)} A 94 94 0 0 1 ${point(mid,94)} A 94 94 0 0 1 ${point(end,94)} L ${point(end,60)} A 60 60 0 0 0 ${point(mid,60)} A 60 60 0 0 0 ${point(start,60)} Z`;
    return <path key={s.key} d={path} fill={s.color} fillOpacity={active&&active!==s.key ? .65 : 1} tabIndex={0} aria-label={`${s.name}: ${money(s.value)} · ${share(s.value)}`} onFocus={()=>setActive(s.key)} onBlur={()=>setActive(undefined)} onPointerEnter={()=>setActive(s.key)} onClick={()=>onSelect?.(s.key)} onKeyDown={e=>{if(onSelect&&["Enter"," "].includes(e.key)){e.preventDefault();onSelect(s.key);}}} className={onSelect?"cursor-pointer outline-none focus:stroke-slate-600":"outline-none focus:stroke-slate-600"}><title>{s.name}: {money(s.value)} · {share(s.value)}</title></path>;
   })}
  </svg>
  {children&&<div className="pointer-events-none absolute inset-[32%] flex flex-col items-center justify-center">{children}</div>}
  {row&&<div role="tooltip" className="pointer-events-none absolute right-0 top-0 z-10 rounded-xl bg-slate-900 px-3 py-2 text-xs text-white shadow"><p className="font-medium">{row.name}</p><p className="mt-1">{money(row.value)} · {share(row.value)}</p></div>}
 </div>;
}
