"use client";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {usePortfolioFilters} from "@/lib/portfolio-filters";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import {lastHistoryDate,loadPortfolioPerformanceHistory} from "@/lib/portfolio-history";
import {portfolioDayResult} from "@/lib/portfolio-day-result";
import {positionDecimal,type Fraction} from "@/lib/investment-positions";
import type {InvestmentLedger} from "@/proto/budget/v1/investment_pb";
export default function PortfolioDayResult({tenantId,ledgers,currency,policyKey,policyVersion,fxMode,groupIds,instrumentKeys,classificationVersion,ru}:{tenantId:string;ledgers:InvestmentLedger[];currency:string;policyKey:string;policyVersion:bigint;fxMode:string;groupIds:string[];instrumentKeys:string[];classificationVersion:bigint;ru:boolean}){
 const {investmentNAV:api}=useClients(),{params}=usePortfolioFilters(),tr=(r:string,e:string)=>ru?r:e,date=params.get("to")||lastHistoryDate();
 const valid=/^\d{4}-\d{2}-\d{2}$/.test(date)&&date>="1900-01-01"&&date<=lastHistoryDate()&&Number.isFinite(Date.parse(date+"T00:00:00Z"))&&new Date(date+"T00:00:00Z").toISOString().slice(0,10)===date;
 const scope={ledgerIds:ledgers.map(l=>l.id),currency,fxPolicyKey:policyKey,fxPolicyVersion:policyVersion,fxMode,groupIds,instrumentKeys,classificationVersion,fromDate:date,toDate:date};
 const query=useQuery({queryKey:["investment",tenantId,"tracker-day-result",ledgers.map(l=>[l.id,l.version.toString()]),currency,policyKey,policyVersion.toString(),fxMode,groupIds,instrumentKeys,classificationVersion.toString(),date],enabled:valid&&ledgers.length>0,retry:false,staleTime:60000,queryFn:async({signal})=>{
  const task=tenantRequestScope(tenantId,signal);try{return await task.run(()=>loadPortfolioPerformanceHistory(api,scope,task.signal,()=>{}));}finally{task.dispose();}
 }});
 const result=!query.error&&!query.isFetching?portfolioDayResult(query.data?.performance,date):undefined;
 const money=(v?:Fraction)=>v?new Intl.NumberFormat(ru?"ru":"en",{style:"currency",currency,maximumFractionDigits:2}).format(Number(positionDecimal(v))):"—";
 return <section aria-label={tr("Дневной результат","Daily result")} className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900"><div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="text-sm text-slate-500">{tr("Результат за завершённый день","Completed-day result")}: {date}</h2><p className="mt-3 text-2xl font-semibold tabular-nums">{query.isFetching?"…":money(result?.profit)}</p></div><p className="max-w-xl text-xs leading-5 text-slate-500">{tr("Сравнение закрытых дневных оценок в выбранной валюте. Покупки, продажи и выплаты учтены; пополнения не требуются. Это ретроспективная оценка, а не изменение текущей цены за сегодня.","Comparison of completed daily valuations in the selected currency. Purchases, sales and payouts are included; deposits are not required. This is retrospective valuation, not today's live price change.")}</p></div>{!query.isFetching&&!result&&<p role="status" className="mt-2 text-xs text-amber-700 dark:text-amber-400">{tr("Для дневного результата нужны полные оценки обеих дат и все движения выбранных бумаг.","Daily profit requires complete valuations for both dates and all flows for the selected securities.")}</p>}{result&&<details className="mt-3 text-xs text-slate-500"><summary>{tr("Расчёт за день","Daily calculation")}</summary><p className="mt-2">{tr("Стоимость на конец дня","End-of-day value")}: {money(result.after)} − {tr("предыдущая стоимость","previous value")}: {money(result.before)} − {tr("чистые вложения в бумаги за день","net capital into securities that day")}: {money(result.flow)} = {money(result.profit)}</p></details>}</section>;
}
