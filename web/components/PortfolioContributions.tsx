"use client";
import PortfolioChartLoading from "./PortfolioChartLoading";
import {useEffect,useState} from "react";
import {Code,ConnectError} from "@connectrpc/connect";
import Link from "next/link";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {usePortfolioFilters} from "@/lib/portfolio-filters";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import {loadPortfolioContributions,contributionCSV,contributionViews,type ContributionScope} from "@/lib/portfolio-contributions";
import {comparePositionValues,type Fraction} from "@/lib/investment-positions";
import {analyticsNumber} from "@/lib/portfolio-analytics";
import {instrumentHref} from "@/lib/portfolio-instrument";
import {shiftHistoryDate} from "@/lib/portfolio-history";
export default function PortfolioContributions({tenantId,scope,valid,ru,locale,instrumentNames,ledgerVersionKey,loading=false}:{loading?:boolean;ledgerVersionKey:string;instrumentNames?:Map<string,string>;tenantId:string;scope:ContributionScope;valid:boolean;ru:boolean;locale:string}){
 const {investmentNAV:api}=useClients(),{params,update}=usePortfolioFilters(),tr=(r:string,e:string)=>ru?r:e;
 const [search,setSearch]=useState(""),[page,setPage]=useState(0),[closed,setClosed]=useState(false);
 const mode=params.get("contribution")==="group"?"group":"instrument",key=JSON.stringify([scope,ledgerVersionKey],(_,v)=>typeof v==="bigint"?v.toString():v);
 useEffect(()=>setPage(0),[key,mode]);
 const query=useQuery({queryKey:["investment",tenantId,"tracker-contributions",key],enabled:valid&&!loading&&!!scope.ledgerIds.length,queryFn:async({signal})=>{const task=tenantRequestScope(tenantId,signal);try{return await task.run(options=>loadPortfolioContributions(api,scope,options.signal));}finally{task.dispose();}},retry:false,staleTime:60000});
 const rows=query.data?contributionViews(query.data,mode).filter(r=>(closed||r.active||!r.profit||BigInt(r.profit.numerator)!==0n||r.quantity&&BigInt(r.quantity.numerator)!==0n||r.incomplete)&&(!search||[r.instrument,r.instrument&&instrumentNames?.get(r.instrument),...r.accounts,...r.groups].filter(Boolean).some(s=>s!.toLocaleLowerCase().includes(search.toLocaleLowerCase())))).sort((a,b)=>comparePositionValues(a.profit,b.profit)):[];
 const money=(v?:Fraction)=>{const n=analyticsNumber(v);return n===undefined?"—":new Intl.NumberFormat(locale,{style:"currency",currency:scope.currency,maximumFractionDigits:2}).format(n);};
 const share=(v?:Fraction)=>{const n=analyticsNumber(v);return n===undefined?"—":new Intl.NumberFormat(locale,{style:"percent",maximumFractionDigits:1}).format(n);};
 const max=Math.max(1,...rows.map(r=>Math.abs(analyticsNumber(r.profit)||0))),button="rounded-lg border border-slate-300 px-3 py-2 text-xs dark:border-slate-700 disabled:opacity-40";
 const csv=()=>{if(!query.data)return;const blob=new Blob([contributionCSV(query.data)],{type:"text/csv;charset=utf-8"}),url=URL.createObjectURL(blob),a=document.createElement("a");a.href=url;a.download=`portfolio-contributions-${scope.fromDate}-${scope.toDate}.csv`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);};
 const label=(r:typeof rows[number])=>r.id==="unallocated"?tr("Без бумаги","Unallocated"):r.instrument?instrumentNames?.get(r.instrument)||r.instrument:r.groups.map(g=>g||tr("Без категории","Uncategorized")).join(", ");
 const shown=rows.slice(page*50,page*50+50),height=Math.max(160,shown.length*36+50),negative=shown.some(r=>(analyticsNumber(r.profit)||0)<0),zero=negative?590:280,span=negative?300:640;
 return <section className="space-y-5 rounded-xl bg-white p-5 sm:p-7 dark:bg-[#30313d]" aria-label={tr("Прибыльность активов","Asset profitability")}>
 <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-lg font-semibold">{tr("Прибыльность активов","Asset profitability")}</h2><details className="relative"><summary className={button+" cursor-pointer list-none"} aria-label={tr("Действия с прибыльностью","Profitability actions")}>•••</summary><div className="absolute right-0 z-10 min-w-48 space-y-2 rounded-xl bg-white p-3 shadow-lg dark:bg-slate-800"><button className={button} disabled={query.isFetching||!valid} onClick={()=>void query.refetch()}>{tr("Обновить","Refresh")}</button><button className={button} disabled={!query.data||query.isFetching||!!query.error} onClick={csv}>CSV</button><select className={button} aria-label={tr("Вклад по","Contributions by")} value={mode} onChange={e=>update({contribution:e.target.value})}><option value="instrument">{tr("Бумагам","Security")}</option><option value="group">{tr("Категориям","Category")}</option></select><label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={closed} onChange={e=>{setClosed(e.target.checked);setPage(0);}}/>{tr("Включить нулевой результат","Include zero results")}</label></div></details></div>
 <input className="w-full max-w-xs rounded-lg bg-slate-50 px-3 py-2 text-sm dark:bg-white/5" aria-label={tr("Поиск вклада","Search contributions")} placeholder={tr("Найти бумагу","Find security")} value={search} onChange={e=>{setSearch(e.target.value);setPage(0);}}/>
 {(loading||query.isFetching)&&<PortfolioChartLoading ru={ru}/>}
 {query.error&&<p role="alert" className="text-sm text-amber-600">{query.error.message==="history_changed"?tr("Категории изменились. Обновите историю.","Categories changed. Refresh history."):query.error instanceof ConnectError&&query.error.code===Code.ResourceExhausted?tr("Выберите меньший период или один счёт.","Choose a shorter period or one account."):tr("Не удалось загрузить прибыльность активов.","Could not load asset profitability.")}</p>}
 {query.data&&!loading&&!query.isFetching&&!query.error&&<>
 <div className="overflow-x-auto"><svg viewBox={`0 0 1050 ${height}`} style={{height}} className="min-w-[600px] w-full" role="img" aria-label={tr("Прибыль по бумагам","Profit by security")}>
 {Array.from({length:6},(_,i)=><line key={i} x1={zero+(i/5)*(negative?span*2:span)-(negative?span:0)} x2={zero+(i/5)*(negative?span*2:span)-(negative?span:0)} y1="0" y2={height-30} stroke="currentColor" strokeOpacity=".15" strokeDasharray="4 5"/>)}
 {shown.map((r,i)=>{const n=analyticsNumber(r.profit),w=n===undefined?0:Math.abs(n)/max*span,y=14+i*36;return <g key={r.id}>
 <text x="260" y={y+12} textAnchor="end" fontSize="12" className="fill-slate-700 dark:fill-slate-200">{label(r).length>30?label(r).slice(0,28)+"…":label(r)}<title>{label(r)}</title></text>
 {n===undefined?<text x={zero+8} y={y+12} fontSize="12" className="fill-slate-500">—<title>{tr("Неполные данные","Incomplete data")}</title></text>:<><rect x={n<0?zero-w:zero} y={y+2} width={Math.max(1,w)} height="12" rx="5" fill={n<0?"#f36368":"#55d0ad"}><title>{label(r)}: {money(r.profit)}</title></rect><text x={n<0?zero-w-6:zero+w+6} y={y+12} textAnchor={n<0?"end":"start"} fontSize="11" fill={n<0?"#f36368":"#55d0ad"}>{new Intl.NumberFormat(locale,{notation:"compact",maximumFractionDigits:1,style:"currency",currency:scope.currency}).format(n)}</text></>}
 </g>;})}</svg></div>
 {!rows.length&&<p className="text-sm text-slate-500">{tr("Нет результатов за этот период.","No results for this period.")}</p>}
 <details className="border-t border-slate-200 pt-4 text-sm dark:border-white/10"><summary className="cursor-pointer text-slate-500">{tr("Подробнее","Details")}</summary><div className="mt-3 overflow-x-auto"><table className="w-full whitespace-nowrap text-left text-sm"><thead><tr>{[tr("Бумага","Security"),tr("Прибыль","Profit"),tr("Доля результата","Profit share"),tr("На начало","Opening value"),tr("На конец","Closing value"),tr("Вложения − возвраты","Investment − proceeds")].map(s=><th className="p-3" key={s}>{s}</th>)}</tr></thead><tbody>{shown.map(r=><tr key={r.id} className="border-t dark:border-white/10"><td className="p-3">{r.instrument?<Link href={instrumentHref(r.instrument,params.toString())}>{label(r)}</Link>:label(r)}{r.incomplete&&<span className="ml-2 text-xs text-amber-600">{tr("Неполные данные","Incomplete data")}</span>}</td><td className="p-3">{money(r.profit)}</td><td className="p-3">{share(r.share)}</td><td className="p-3">{money(r.opening)}</td><td className="p-3">{money(r.closing)}</td><td className="p-3">{money(r.flow)}</td></tr>)}</tbody></table></div><p className="mt-2 text-xs text-slate-500">{scope.fromDate} — {scope.toDate} · {tr("База","Baseline")}: {valid?shiftHistoryDate(scope.fromDate,-1):"—"}</p></details>
 {rows.length>50&&<div className="flex items-center gap-3 text-xs"><button className={button} disabled={!page} onClick={()=>setPage(page-1)}>{tr("Назад","Previous")}</button><span>{page+1} / {Math.ceil(rows.length/50)}</span><button className={button} disabled={(page+1)*50>=rows.length} onClick={()=>setPage(page+1)}>{tr("Далее","Next")}</button></div>}
 </>}
 </section>;
}
