"use client";
import {useQuery} from "@tanstack/react-query";
import {useTranslations,useLocale} from "next-intl";
import {useClients} from "@/app/providers";
import type {InvestmentNAVSeries,GetInvestmentCurrentNAVResponse} from "@/proto/budget/v1/investment_nav_pb";
import {positionDecimal} from "@/lib/investment-positions";

export function useInvestmentCurrent(tenantId:string,series:InvestmentNAVSeries,enabled=true){
 const {investmentNAV:api}=useClients();
 const query=useQuery<GetInvestmentCurrentNAVResponse>({queryKey:["investmentNAV",tenantId,"current",series.key],enabled,queryFn:()=>api.getInvestmentCurrentNAV({seriesKey:series.key}),refetchInterval:30000,refetchIntervalInBackground:false});
 const candidate=query.data;
 const data=!query.isError&&candidate?.provisional&&candidate.seriesKey===series.key&&candidate.currency===series.currency&&candidate.ledgerIds.length===series.ledgerIds.length&&[...candidate.ledgerIds].sort().every((id,i)=>id===[...series.ledgerIds].sort()[i])?candidate:undefined;
 return {...query,data};
}
export default function InvestmentCurrent({tenantId,series,enabled=true}:{tenantId:string;series:InvestmentNAVSeries;enabled?:boolean}){
 const t=useTranslations("investmentCurrent"),quality=useTranslations("investmentNAV"),locale=useLocale(),query=useInvestmentCurrent(tenantId,series,enabled),data=query.data;
 const stamp=(value?:{seconds:bigint})=>value?new Intl.DateTimeFormat(locale,{timeZone:"Europe/Moscow",dateStyle:"medium",timeStyle:"medium"}).format(new Date(Number(value.seconds)*1000)):"—";
 const issue=(code:string)=>quality.has(`issues.${code}`)?quality(`issues.${code}`):quality("issue",{code});
 if(!enabled)return null;
 return <section aria-label={t("title")} data-testid="investment-current" className="space-y-3 rounded-xl border border-dashed border-amber-500 p-4">
  <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="font-semibold">{t("title")}</h3><button className="rounded-lg border px-3 py-2 text-sm disabled:opacity-50" disabled={query.isFetching} onClick={()=>query.refetch()}>{t("reload")}</button></div>
  <p className="text-xs text-gray-500">{t("method")}</p>
  {query.isPending&&<p role="status">{t("loading")}</p>}{query.error&&<p role="alert">{t("error")}</p>}
  {!query.isPending&&!query.error&&!data&&<p>{t("unavailable")}</p>}
  {data&&<><p className="break-all text-2xl font-semibold tabular-nums" data-testid="current-known">{data.knownValue?`${positionDecimal(data.knownValue)} ${data.currency}`:"—"}</p><p className="text-sm">{t(data.complete?"complete":"subtotal")}</p><p className="text-xs text-gray-500">{t("asOf",{date:stamp(data.asOf)})} MSK · {data.fxPolicy} · {data.priceProfileKey} v{String(data.priceProfileVersion)}</p>
   {[...data.issues,...data.warnings].map((row,i)=><p className="break-all text-sm text-amber-800 dark:text-amber-300" key={i}>{issue(row.code)} <span className="text-xs">{row.componentId}</span></p>)}
   <details><summary className="cursor-pointer text-sm">{t("evidence")}</summary><div className="mt-3 grid gap-3 sm:grid-cols-2">{data.quotes.map(q=><article key={q.instrumentKey} className="min-w-0 space-y-1 rounded-lg bg-gray-50 p-3 dark:bg-gray-900"><h4 className="break-all font-medium">{q.instrumentKey}</h4><p className="break-all font-mono">{q.candidatePrice?positionDecimal(q.candidatePrice):"—"} {q.currency} · {t(q.unit==="currency_per_unit"?"unitSecurity":q.unit==="percent_of_nominal"?"unitPercent":"unitUnknown")}</p><p className="text-sm">{t(q.state==="fresh"?"fresh":"excluded")}</p><p className="break-all text-xs text-gray-500">{q.provider}/{q.accessScope} · {t(q.sourceKind==="exchange"?"exchange":q.sourceKind==="dealer"?"dealer":"sourceUnknown")} · {t(q.adjustment==="unadjusted"?"unadjusted":"adjustmentUnknown")}</p><p className="text-xs">{t("priceAt",{date:stamp(q.priceAt)})} MSK</p><p className="text-xs">{t("received",{date:stamp(q.receivedAt)})} MSK</p><p className="text-xs">{t("validUntil",{date:stamp(q.validUntil)})} MSK</p><p className="break-all text-xs">{q.observationId} · v{String(q.revision)}</p>{q.issues.map(code=><p key={code} className="break-all text-xs text-amber-800 dark:text-amber-300">{issue(code)}</p>)}</article>)}</div>
    {data.fxLegs.map((leg,i)=><p className="mt-2 break-all text-xs" key={i}>{leg.nativeCurrency}: {leg.fromCurrency}/{leg.toCurrency}: {leg.observedRate?positionDecimal(leg.observedRate):"—"} · {leg.policy} · {t(leg.inverse?"inverse":"direct")} · {stamp(leg.asOf)} MSK · {t("validUntil",{date:stamp(leg.validUntil)})} MSK · {leg.id} v{String(leg.revision)}</p>)}
   </details>
  </>}
 </section>;
}
