"use client";
import InvestmentChart from "@/components/InvestmentChart";

import InvestmentProfit from "@/components/InvestmentProfit";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useLocale, useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import type { GetInvestmentPerformanceResponse } from "@/proto/budget/v1/investment_nav_pb";
import { navDecimal } from "@/lib/investment-nav";
import {positionDecimal} from "@/lib/investment-positions";
import { exactReturnPercent, annualReturnPercent, returnGeometry } from "@/lib/investment-performance";

export default function InvestmentPerformance({ tenantId, seriesKey, from, to, currency }: { tenantId:string; seriesKey:string; from:string; to:string; currency:string }) {
 const t=useTranslations("investmentPerformance"), locale=useLocale();
 const {investmentNAV:api}=useClients();
 const query=useQuery<GetInvestmentPerformanceResponse>({queryKey:["investmentPerformance",tenantId,seriesKey,from,to],queryFn:()=>api.getInvestmentPerformance({seriesKey,fromDate:from,toDate:to}),refetchInterval:15000});
 const data=query.error?undefined:query.data;
 const [offset,setOffset]=useState(0);
 const points=data?.twrPoints||[];
 const graph=returnGeometry(points,true);
 const state=(value:string)=>t.has(`states.${value}`)?t(`states.${value}`):t("unknownState");
 const reason=(code:string)=>code?(t.has(`reasons.${code}`)?t(`reasons.${code}`):t("unknownReason",{code})):"";
 const money=(value?:{decimal:string})=>value?`${navDecimal(value.decimal)} ${currency}`:"—";
 const cards=[
  {key:"profit",value:data?.reason?"—":money(data?.profit),reason:data?.reason},
  {key:"contributions",value:data?.netContributionsReason?"—":money(data?.netContributions),reason:data?.netContributionsReason},
  {key:"twr",value:data?.twrReason?"—":exactReturnPercent(data?.twr,locale),reason:data?.twrReason},
  {key:"xirr",value:data?.xirrReason?"—":annualReturnPercent(data?.xirr,locale),reason:data?.xirrReason},
 ];
 return <section aria-label={t("title")} className="space-y-3" data-testid="investment-performance">
  <h2 className="text-xl font-semibold">{t("title")}</h2>
  <p className="text-sm text-gray-500">{t("method")}</p>
  {query.isPending&&<p role="status">{t("loading")}</p>}
  {query.error&&<p role="alert">{t("error")} <button className="underline" onClick={()=>query.refetch()}>{t("retry")}</button></p>}
  <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">{cards.map(card=><article key={card.key} data-testid={`performance-${card.key}`} className="min-w-0 rounded-xl bg-gray-50 p-4 dark:bg-gray-900">
   <h3 className="text-sm">{t(card.key)}</h3><p className="mt-1 break-all text-xl font-semibold tabular-nums">{card.value}</p>
   {card.reason&&<p className="mt-2 text-sm text-gray-500">{reason(card.reason)}</p>}
  </article>)}</div>
  {data&&<div className="space-y-3">
   <h3 className="font-medium">{t("chartTitle")}</h3>
   <p className="text-sm text-gray-500">{t("segments")}</p>
   {graph.dots.length>0?<figure>
    <div className="flex flex-wrap justify-between gap-2 text-xs tabular-nums"><span>{t("minimum")}: {positionDecimal(graph.min)}</span><span>{t("maximum")}: {positionDecimal(graph.max)}</span></div>
    <InvestmentChart role="img" aria-label={t("chartTitle")} viewBox="0 0 800 260" className="w-full text-emerald-600 dark:text-emerald-400" data-testid="twr-chart">
     <title>{t("chartTitle")}</title><desc>{t("segments")}</desc>
     <path d="M20,240 L780,240" stroke="currentColor" opacity="0.2"/>
     {graph.paths.map((part,i)=><path key={i} data-testid="twr-segment" data-segment={part.segment} d={part.path} fill="none" stroke="currentColor" strokeWidth="2"/>)}
     {graph.dots.map(dot=>{const point=points[dot.index];return <circle key={dot.index} cx={dot.x} cy={dot.y} r="5" fill="currentColor"><title>{point.date} · {t("segment",{number:point.segment})}: {positionDecimal(point.index)}</title></circle>;})}
    </InvestmentChart>
    <figcaption className="flex justify-between text-xs text-gray-500"><span>{points[0]?.date}</span><span>{points.at(-1)?.date}</span></figcaption>
   </figure>:<p>{t("noIntervals")}</p>}
   <details className="rounded-lg border border-gray-200 p-3 dark:border-gray-800">
    <summary className="cursor-pointer font-medium">{t("dailyDetails")}</summary>
    <div className="max-w-full overflow-x-auto"><table className="w-full text-left text-sm"><caption className="sr-only">{t("dailyDetails")}</caption>
     <thead><tr>{["date","daily","segmentReturn","index","status"].map(key=><th key={key} className="p-2">{t(key)}</th>)}</tr></thead>
     <tbody>{points.slice(offset,offset+50).map((point,i)=><tr key={`${point.date}:${i}`} className="border-t border-gray-200 dark:border-gray-800">
      <td className="whitespace-nowrap p-2">{point.date}</td>
      <td className="p-2 tabular-nums">{point.state==="calculated"?exactReturnPercent(point.dailyReturn,locale):"—"}</td>
      <td className="p-2 tabular-nums">{point.state==="calculated"?exactReturnPercent(point.segmentReturn,locale):point.state==="baseline"?t("baseline"):"—"}</td>
      <td className="p-2 tabular-nums">{["baseline","calculated"].includes(point.state)?positionDecimal(point.index):"—"}</td>
      <td className="min-w-40 p-2">{state(point.state)}{point.segment>0&&<span className="block text-xs">{t("segment",{number:point.segment})}</span>}{point.reason&&<p className="text-xs text-gray-500">{reason(point.reason)}</p>}</td>
     </tr>)}</tbody>
    </table></div>
    {points.length>50&&<div className="mt-3 flex gap-3"><button className="rounded border px-3 py-2 disabled:opacity-50" disabled={offset===0} onClick={()=>setOffset(Math.max(0,offset-50))}>{t("previous")}</button><button className="rounded border px-3 py-2 disabled:opacity-50" disabled={offset+50>=points.length} onClick={()=>setOffset(offset+50)}>{t("next")}</button></div>}
   </details>
  </div>}
  <InvestmentProfit tenantId={tenantId} seriesKey={seriesKey} from={from} to={to}/>
  <p className="text-xs text-gray-500">{t("journalScope")}</p>
 </section>;
}
