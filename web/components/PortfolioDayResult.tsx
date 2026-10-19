"use client";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {usePortfolioFilters} from "@/lib/portfolio-filters";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import {lastHistoryDate,loadPortfolioPerformanceHistory,shiftHistoryDate,historySelectionEqual} from "@/lib/portfolio-history";
import {portfolioDayResult,latestCompletedTradingValuation} from "@/lib/portfolio-day-result";
import {divide} from "@/lib/portfolio-tracker";
import {positionDecimal,type Fraction} from "@/lib/investment-positions";
import type {InvestmentLedger} from "@/proto/budget/v1/investment_pb";
export default function PortfolioDayResult({tenantId,ledgers,currency,policyKey,policyVersion,fxMode,groupIds,instrumentKeys,classificationVersion,ru}:{tenantId:string;ledgers:InvestmentLedger[];currency:string;policyKey:string;policyVersion:bigint;fxMode:string;groupIds:string[];instrumentKeys:string[];classificationVersion:bigint;ru:boolean}){
 const {investmentNAV:api}=useClients(),{params}=usePortfolioFilters(),tr=(r:string,e:string)=>ru?r:e,date=params.get("to")||lastHistoryDate();
 const valid=/^\d{4}-\d{2}-\d{2}$/.test(date)&&date>="1900-01-01"&&date<=lastHistoryDate()&&Number.isFinite(Date.parse(date+"T00:00:00Z"))&&new Date(date+"T00:00:00Z").toISOString().slice(0,10)===date;
 const scope={ledgerIds:ledgers.map(l=>l.id),currency,fxPolicyKey:policyKey,fxPolicyVersion:policyVersion,fxMode,groupIds,instrumentKeys,classificationVersion,fromDate:date,toDate:date};
 const query=useQuery({queryKey:["investment",tenantId,"tracker-day-result",ledgers.map(l=>[l.id,l.version.toString()]),currency,policyKey,policyVersion.toString(),fxMode,groupIds,instrumentKeys,classificationVersion.toString(),date],enabled:valid&&ledgers.length>0,retry:false,staleTime:60000,queryFn:async({signal})=>{
  const task=tenantRequestScope(tenantId,signal);try{return await task.run(async()=>{
   const {fxPolicyVersion:_v,fxMode:_m,classificationVersion:_c,...request}=scope;
   const evidence=await api.getTrackerHistory({...request,detailDate:date},{signal:task.signal});
   if(evidence.fxPolicyVersion!==policyVersion||evidence.fxMode!==fxMode||!historySelectionEqual(evidence.groupIds,groupIds)||!historySelectionEqual(evidence.instrumentKeys,instrumentKeys)||groupIds.length>0&&evidence.classificationVersion!==classificationVersion||evidence.currency!==currency||evidence.fxPolicyKey!==policyKey||evidence.priceConvention!=="retrospective_close_next_utc_day_v1"||!evidence.detailsIncluded||evidence.points.length!==1||evidence.points[0].date!==date)throw Error("invalid_session_evidence");
   const valuationDate=latestCompletedTradingValuation(evidence.points[0].details,date);
   if(!valuationDate)return null;
   const history=await loadPortfolioPerformanceHistory(api,{...scope,fromDate:valuationDate,toDate:valuationDate},task.signal,()=>{});
   const candidates=[history.performance,history.partialPerformance,history.growthPerformance];
   for(const p of candidates){const result=portfolioDayResult(p,valuationDate);if(result)return {result,valuationDate,tradingDate:shiftHistoryDate(valuationDate,-1),partial:p!==history.performance};}
   return null;
  });}finally{task.dispose();}
 }});
 const result=!query.error?query.data?.result:undefined;
 const displayDate=query.data?.tradingDate;
 const money=(v?:Fraction)=>v?new Intl.NumberFormat(ru?"ru":"en",{style:"currency",currency,maximumFractionDigits:2}).format(Number(positionDecimal(v))):"—";
 const relative=result&&BigInt(result.before.numerator)>0n?divide(result.profit,result.before):undefined;
 const positive=result&&BigInt(result.profit.numerator)>=0n;
 return <section aria-label={tr("Дневной результат","Daily result")} className="rounded-2xl border border-slate-200 bg-[hsl(var(--card))] p-5 dark:border-slate-700/40 " title={tr("Последняя завершённая торговая сессия; ретроспективная оценка на ","Last completed trading session; retrospective valuation on ")+(query.data?.valuationDate||"—")}>
 <div className="flex flex-wrap items-center justify-between gap-2 text-slate-500 dark:text-slate-400"><h2 className="text-sm">{tr("За последнюю торговую сессию","Last trading session")}</h2><time className="text-xs" dateTime={displayDate}>{displayDate?new Intl.DateTimeFormat(ru?"ru":"en",{timeZone:"UTC",day:"numeric",month:"short",...(displayDate.slice(0,4)!==String(new Date().getUTCFullYear())?{year:"2-digit"}:{})}).format(new Date(displayDate+"T00:00:00Z")):"—"}</time></div>
 <p className={"mt-4 text-2xl font-semibold tabular-nums "+(result?positive?"text-emerald-500":"text-rose-500":"")}>{query.isFetching&&!query.data?"…":money(result?.profit)}</p>
 <p className={"mt-2 text-sm tabular-nums "+(result?positive?"text-emerald-500":"text-rose-500":"text-slate-400")}>{relative?new Intl.NumberFormat(ru?"ru":"en",{style:"percent",signDisplay:"always",maximumFractionDigits:2}).format(Number(positionDecimal(relative))):"—"}</p>
 {query.data?.partial&&<span className="text-xs text-amber-600">{tr("По доступным бумагам","Available instruments")}</span>}
 </section>;
}
