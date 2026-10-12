"use client";
import {create} from "@bufbuild/protobuf";
import {InvestmentNAVSeriesSchema} from "@/proto/budget/v1/investment_nav_pb";
import {useState} from "react";
import Link from "next/link";
import {useSearchParams,usePathname,useRouter} from "next/navigation";
import {useQuery,useInfiniteQuery} from "@tanstack/react-query";
import {useTranslations,useLocale} from "next-intl";
import {useClients} from "@/app/providers";
import InvestmentCurrent from "@/components/InvestmentCurrent";
import InvestmentVariant from "@/components/InvestmentVariant";
import InvestmentSeriesExport from "@/components/InvestmentSeriesExport";
import InvestmentPeriod,{useInvestmentRange} from "@/components/InvestmentPeriod";
import InvestmentPortfolios from "@/components/InvestmentPortfolios";
import {positionDecimal} from "@/lib/investment-positions";
import {exactReturnPercent,annualReturnPercent} from "@/lib/investment-performance";
import type {InvestmentNAVSeries,GetInvestmentOverviewResponse} from "@/proto/budget/v1/investment_nav_pb";
const field="rounded-lg border border-gray-300 bg-transparent px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";

function OverviewValues({tenantId,series}:{tenantId:string;series:InvestmentNAVSeries}){
 const t=useTranslations("investmentOverview"),quality=useTranslations("investmentNAV"),reasons=useTranslations("investmentPerformance"),locale=useLocale(),{investmentNAV:api}=useClients(),params=useSearchParams();
 const range=useInvestmentRange(series.key,series.fromDate,series.toDate),{from,to,valid}=range;
 const query=useQuery<GetInvestmentOverviewResponse>({queryKey:["investmentNAV",tenantId,"overview",series.key,from,to],enabled:valid,queryFn:()=>api.getInvestmentOverview({seriesKey:series.key,fromDate:from,toDate:to}),refetchInterval:15000});
 const data=query.error?undefined:query.data,p=data?.performance,nav=data?.nav;
 const money=(value?:{numerator:string;denominator:string})=>value?`${positionDecimal(value)} ${series.currency}`:"—";
 const why=(code?:string)=>code?(reasons.has(`reasons.${code}`)?reasons(`reasons.${code}`):reasons("unknownReason",{code})):"";
 const stamp=(at?:{seconds:bigint})=>at?new Date(Number(at.seconds)*1000).toLocaleString(locale,{timeZone:"Europe/Moscow"}):"—";
 const ready=nav?.state==="ready"||nav?.state==="incomplete";
 const cards=[
  {key:"nav",value:money(ready?nav?.knownValue:undefined),partial:nav?.state==="incomplete"},
  {key:"securities",value:money(ready?data?.securitiesKnown:undefined),partial:!!data?.securitiesKnown&&!data.securitiesComplete},
  {key:"cash",value:money(ready?data?.cashKnown:undefined),partial:!!data?.cashKnown&&!data.cashComplete},
  {key:"profit",value:money(p&&!p.reason?p.profit:undefined),reason:p?.reason},
  {key:"contributions",value:money(p&&!p.netContributionsReason?p.netContributions:undefined),reason:p?.netContributionsReason},
  {key:"twr",value:p&&!p.twrReason?exactReturnPercent(p.twr,locale):"—",reason:p?.twrReason},
  {key:"xirr",value:p&&!p.xirrReason?annualReturnPercent(p.xirr,locale):"—",reason:p?.xirrReason},
  {key:"daily",value:money(data&&!data.dailyReason?data.dailyProfit:undefined),reason:data?.dailyReason},
 ];
 const details=new URLSearchParams(params.toString());details.set("series",series.key);details.set("from",from);details.set("to",to);
 return <section className="space-y-4" aria-label={t("summary")}>
  <div className="flex flex-wrap items-end gap-3"><label>{t("from")}<input type="date" className={field+" block"} min={series.fromDate} max={to} value={from} onChange={e=>range.update(e.target.value,to)}/></label><label>{t("to")}<input type="date" className={field+" block"} min={from} max={series.toDate} value={to} onChange={e=>range.update(from,e.target.value)}/></label><button className={field} disabled={!valid||query.isFetching} onClick={()=>query.refetch()}>{t("reload")}</button></div>
  <InvestmentVariant key={`variant:${series.key}`} tenantId={tenantId} series={series}/>
  <InvestmentCurrent tenantId={tenantId} series={series} enabled={valid}/>
  <InvestmentPeriod period={range.period} select={range.select}/>
  <InvestmentSeriesExport key={`export:${series.key}:${from}:${to}`} tenantId={tenantId} seriesKey={series.key} from={from} to={to} valid={valid}/>
  {!valid&&<p role="alert">{quality("invalidRange")}</p>}
  {query.isFetching&&valid&&<p role="status">{t("loading")}</p>}
  {query.error&&<p role="alert">{t("error")}</p>}
  <p className="text-sm text-gray-500">{t("scope",{count:series.ledgerIds.length,currency:series.currency,policy:series.fxPolicyKey})}</p>
  {data&&<><p className="text-sm">{t("asOf",{date:to})} · {nav?.state&&quality.has(`states.${nav.state}`)?quality(`states.${nav.state}`):quality("unknown")} · {t("computed",{date:stamp(nav?.computedAt)})} MSK</p>
  <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{cards.map(card=><article key={card.key} data-testid={`overview-${card.key}`} className="min-w-0 rounded-xl bg-gray-50 p-4 dark:bg-gray-900"><h3 className="text-sm font-medium">{t(card.key)}</h3><p className="mt-2 break-all text-2xl font-semibold tabular-nums">{card.value}</p>{card.partial&&<p className="mt-2 text-sm text-amber-800 dark:text-amber-300">{t("subtotal")}</p>}{card.reason&&<p className="mt-2 text-xs text-gray-500">{why(card.reason)}</p>}</article>)}</div>
  <p className="text-xs text-gray-500">{t("method")}</p>
  {[...(nav?.issues||[]),...(nav?.warnings||[]),...data.breakdownIssues].map((issue,i)=><p key={i} className="break-all text-sm text-amber-800 dark:text-amber-300">{quality.has(`issues.${issue.code}`)?quality(`issues.${issue.code}`):quality("issue",{code:issue.code})}</p>)}
  <section className="space-y-3"><h3 className="font-semibold">{t("fxTitle")}</h3><p className="text-xs text-gray-500">{t("fxPolicy")}</p><div className="grid gap-3 sm:grid-cols-2">{data.fx.map(quote=><article key={quote.fromCurrency} className="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-gray-800"><h4 className="font-medium">{quote.fromCurrency}/{quote.toCurrency}</h4><p className="break-all font-mono text-xl">{quote.rate?positionDecimal(quote.rate):"—"}</p><p className="text-sm">{t("fxChange")}: {exactReturnPercent(quote.change,locale)}</p><p className={quote.state==="fresh"?"text-sm text-emerald-800 dark:text-emerald-300":"text-sm text-amber-800 dark:text-amber-300"}>{t.has(`fxStates.${quote.state}`)?t(`fxStates.${quote.state}`):t("fxStates.unavailable")}</p><p className="break-all text-xs text-gray-500">{quote.source||"—"} · {quote.mode==="cbr"?t("cbr"):quote.mode==="market"?t("market"):"—"} · {stamp(quote.asOf)} MSK</p>{quote.asOf&&data.readAt&&<p className="text-xs text-gray-500">{t("age",{hours:Math.max(0,Math.floor(Number(data.readAt.seconds-quote.asOf.seconds)/3600))})}</p>}</article>)}</div></section>
  </>}
  {valid&&<Link className="inline-block text-blue-600" href={`/investments/value?${details}`}>{t("details")}</Link>}
 </section>;
}
export default function InvestmentOverview({tenantId}:{tenantId:string}){
 const t=useTranslations("investmentOverview"),{investmentNAV:api}=useClients(),params=useSearchParams(),router=useRouter(),pathname=usePathname();
 const [saved,setSaved]=useState<InvestmentNAVSeries>();
 const requested=params.get("series")||"";
 const list=useInfiniteQuery({queryKey:["investmentNAV",tenantId,"overviewSeries"],initialPageParam:"",queryFn:({pageParam})=>api.listInvestmentNAVSeries({afterKey:pageParam}),getNextPageParam:last=>last.nextAfterKey||undefined,refetchInterval:30000});
 const explicit=useQuery({queryKey:["investmentNAV",tenantId,"seriesInfo",requested],enabled:!!requested,queryFn:()=>api.getInvestmentNAVSeries({seriesKey:requested}),refetchInterval:30000});
 const available=list.data?.pages.flatMap(p=>p.series)||[];
 const series=requested?(explicit.isError?undefined:explicit.data?.series||(saved?.key===requested?saved:undefined)):available[0];
 const select=(key:string)=>{const q=new URLSearchParams(params.toString());q.set("series",key);router.replace(`${pathname}?${q}`,{scroll:false});};
 return <section className="space-y-5 rounded-xl border border-gray-200 p-4 dark:border-gray-800"><h2 className="text-2xl font-semibold">{t("summary")}</h2>
  <InvestmentPortfolios tenantId={tenantId} onView={p=>{setSaved(create(InvestmentNAVSeriesSchema,{key:p.seriesKey,ledgerIds:p.ledgerIds,currency:p.currency,fxPolicyKey:p.fxPolicyKey,fromDate:p.fromDate,toDate:p.toDate}));select(p.seriesKey);}}/>
  {(list.isFetching||explicit.isFetching)&&<p role="status">{t("loading")}</p>}
  {(list.error||explicit.error)&&<p role="alert">{t("error")}</p>}
  {!series&&!list.isPending&&!requested&&<p>{t("empty")}</p>}
  {series&&<><label className="block max-w-xl">{t("series")}<select aria-label={t("series")} className={field+" mt-1 block w-full"} value={series.key} onChange={e=>select(e.target.value)}>{!available.some(s=>s.key===series.key)&&<option value={series.key}>{series.currency} · {series.fxPolicyKey} · {series.ledgerIds.length}</option>}{available.map(s=><option key={s.key} value={s.key}>{s.currency} · {s.fxPolicyKey} · {t("accounts",{count:s.ledgerIds.length})} · {s.fromDate}—{s.toDate}</option>)}</select></label><OverviewValues key={series.key} tenantId={tenantId} series={series}/></>}
  {list.hasNextPage&&<button className={field} disabled={list.isFetchingNextPage} onClick={()=>list.fetchNextPage()}>{t("more")}</button>}
 </section>;
}
