"use client";
import InvestmentChart from "@/components/InvestmentChart";
import {useState} from "react";
import {useInfiniteQuery,useQuery} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import type {GetInvestmentBenchmarkComparisonResponse,ListInvestmentBenchmarksResponse} from "@/proto/budget/v1/investment_nav_pb";
import {comparisonGeometry,navDecimal} from "@/lib/investment-nav";

type Selection={tenantId:string;seriesKey:string;from:string;to:string};
const button="rounded-lg border border-gray-300 px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";
function Comparison({tenantId,seriesKey,benchmarkKey,from,to}:Selection&{benchmarkKey:string}) {
 const t=useTranslations("investmentBenchmark"), {investmentNAV:api}=useClients();
 const [mode,setMode]=useState<"money"|"index">("money");
 const query=useQuery<GetInvestmentBenchmarkComparisonResponse>({queryKey:["investmentBenchmark",tenantId,seriesKey,benchmarkKey,from,to],queryFn:()=>api.getInvestmentBenchmarkComparison({seriesKey,benchmarkKey,fromDate:from,toDate:to}),refetchInterval:15000});
 const data=query.error?undefined:query.data;
 const points=data?.points||[];
 const left=points.map(p=>({date:p.date,state:(mode==="money"?p.portfolioValue:p.portfolioIndex)?"ready":"unavailable",knownValue:mode==="money"?p.portfolioValue:p.portfolioIndex}));
 const right=points.map(p=>({date:p.date,state:(mode==="money"?p.state==="ready"&&p.value:p.index)?"ready":"unavailable",knownValue:mode==="money"?p.value:p.index}));
 const graph=comparisonGeometry(left,right);
 const reason=(code:string)=>t.has(`reasons.${code}`)?t(`reasons.${code}`):t("unknownReason",{code});
 const unit=mode==="money"?data?.currency||"":t("indexUnit");
 return <div className="space-y-3" data-testid="benchmark-comparison">
  {query.isPending&&<p role="status">{t("loading")}</p>}
  {query.error&&<p role="alert">{t("error")} <button className={button} onClick={()=>query.refetch()}>{t("retry")}</button></p>}
  {data&&<>
   <p className="text-sm">{data.benchmark?.title} · {t(data.benchmark?.kind==="etf_price_proxy"?"proxy":"priceIndex")} · {data.currency} · {data.fxPolicyKey}</p>
   <div className="flex flex-wrap gap-2"><button className={button} aria-pressed={mode==="money"} onClick={()=>setMode("money")}>{t("money")}</button><button className={button} aria-pressed={mode==="index"} onClick={()=>setMode("index")}>{t("normalized")}</button></div>
   <p className="text-sm text-gray-500">{t(mode==="money"?"moneyMethod":"indexMethod")}</p>
   {data.reason&&<p role="status" className="text-sm text-amber-700 dark:text-amber-400">{reason(data.reason)}</p>}
   <div className="flex flex-wrap gap-4 text-sm"><span className="text-blue-600 dark:text-blue-400">● {t("portfolio")}</span><span className="text-amber-700 dark:text-amber-400">● {t("benchmark")}</span></div>
   {graph.left.dots.length+graph.right.dots.length>0?<figure>
    <div className="flex flex-wrap justify-between text-xs tabular-nums"><span>{navDecimal(graph.min)} {unit}</span><span>{navDecimal(graph.max)} {unit}</span></div>
    <InvestmentChart role="img" aria-label={t("chart")} viewBox="0 0 800 260" className="w-full" data-testid="benchmark-chart"><title>{t("chart")}</title><desc>{t("gaps")}</desc>
     {[{data:graph.left,color:"text-blue-600 dark:text-blue-400",series:left,key:"portfolio"},{data:graph.right,color:"text-amber-700 dark:text-amber-400",series:right,key:"benchmark"}].map(line=><g key={line.key} className={line.color}>
      {line.data.paths.map((path,i)=><path key={i} d={path} fill="none" stroke="currentColor" strokeWidth="2" data-testid={`comparison-${line.key}-segment`}/>)}
      {line.data.dots.map(dot=><circle key={dot.index} cx={dot.x} cy={dot.y} r="4" fill="currentColor"><title>{points[dot.index].date} · {t(line.key)}: {navDecimal(line.series[dot.index].knownValue!.decimal)} {unit}</title></circle>)}
     </g>)}
    </InvestmentChart><figcaption className="flex justify-between text-xs text-gray-500"><span>{points[0]?.date}</span><span>{points.at(-1)?.date}</span></figcaption>
   </figure>:<p>{t("emptyRange")}</p>}
   <p className="text-xs text-gray-500">{t("gaps")}</p>
   {points.some(p=>p.reason||p.issues.length)&&<details className="rounded-lg border border-gray-200 p-3 dark:border-gray-800"><summary className="cursor-pointer">{t("details")}</summary><div className="max-h-72 overflow-y-auto">{points.filter(p=>p.reason||p.issues.length).map(p=><p key={p.date} className="my-2 text-sm">{p.date}: {[...new Set([p.reason,...p.issues].filter(Boolean))].map(reason).join(" · ")}</p>)}</div></details>}
  </>}
 </div>;
}
export default function InvestmentBenchmarkComparison(props:Selection) {
 const t=useTranslations("investmentBenchmark"),{investmentNAV:api}=useClients();
 const [selected,setSelected]=useState("");
 const list=useInfiniteQuery<ListInvestmentBenchmarksResponse>({queryKey:["investmentBenchmarks",props.tenantId],initialPageParam:"",queryFn:({pageParam})=>api.listInvestmentBenchmarks({afterKey:String(pageParam)}),getNextPageParam:last=>last.nextAfterKey||undefined});
 const options=list.data?.pages.flatMap(p=>p.benchmarks)||[];
 return <section className="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-gray-800">
  <h2 className="text-xl font-semibold">{t("title")}</h2>
  {list.isPending&&<p role="status">{t("loading")}</p>}
  {list.error&&<p role="alert">{t("error")} <button className={button} onClick={()=>list.refetch()}>{t("retry")}</button></p>}
  {list.data&&options.length===0&&<p className="text-sm text-gray-500">{t("noBenchmarks")}</p>}
  {options.length>0&&<label className="block text-sm">{t("choose")}<select aria-label={t("choose")} className="mt-1 w-full rounded-lg border border-gray-300 bg-white p-2 dark:border-gray-700 dark:bg-gray-900" value={selected} onChange={e=>setSelected(e.target.value)}><option value="">{t("off")}</option>{options.map(p=><option key={p.key} value={p.key}>{p.title} · {t(p.kind==="etf_price_proxy"?"proxy":"priceIndex")}</option>)}</select></label>}
  {list.hasNextPage&&<button className={button} disabled={list.isFetchingNextPage} onClick={()=>list.fetchNextPage()}>{t("more")}</button>}
  {selected&&<Comparison key={`${selected}:${props.from}:${props.to}`} {...props} benchmarkKey={selected}/>}
 </section>;
}
