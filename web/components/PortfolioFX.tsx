"use client";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import {currentFXRow} from "@/lib/portfolio-current-fx";
import {positionDecimal} from "@/lib/investment-positions";
import type {InvestmentValuationProfileOption} from "@/proto/budget/v1/investment_nav_pb";
export default function PortfolioFX({tenantId,policy,ru}:{tenantId:string;policy?:InvestmentValuationProfileOption;ru:boolean}){
 const {investmentNAV:api}=useClients(),tr=(r:string,e:string)=>ru?r:e;
 const query=useQuery({queryKey:["investment",tenantId,"tracker-current-fx",policy?.key,policy?.version.toString(),policy?.mode],enabled:!!policy,staleTime:60000,refetchInterval:60000,retry:false,queryFn:async({signal})=>{
  const task=tenantRequestScope(tenantId,signal);
  try{return await task.run(async options=>{
   const p=await api.getInvestmentExchangeRates({fxPolicyKey:policy!.key,currency:"RUB",fromCurrencies:["USD","EUR"],dates:[]},options);
   if(p.currency!=="RUB"||p.fxPolicyKey!==policy!.key||p.policyVersion!==policy!.version||p.mode!==policy!.mode)throw new Error("fx_policy_changed");
   return p;
  });}finally{task.dispose();}
 }});
 if(!policy)return null;
 const p=query.data,source=p?.source==="alphavantage"?"Alpha Vantage":p?.source==="cbr"?tr("Банк России","Bank of Russia"):p?.source;
 const rows=["USD","EUR"].map(from=>({from,value:currentFXRow(p,from)}));
 const date=(seconds:bigint)=>new Date(Number(seconds)*1000).toISOString().replace("T"," ").replace(".000Z"," UTC");
 return <section aria-label={tr("Валютные курсы портфеля","Portfolio exchange rates")} className="flex flex-wrap items-start gap-x-6 gap-y-2 text-xs text-slate-500">
 {rows.map(({from,value})=><div key={from}><span className="font-medium text-slate-700 dark:text-slate-300">{from}/RUB: {query.isPending?"…":value?positionDecimal(value.rate,12).replace(/(\.\d*?[1-9])0+$|\.0+$/,"$1").replace(".",ru?",":"."):"—"}</span>{value&&<span className="block mt-1">{tr("Для оценки с ","Eligible from ")}{[...new Set(value.legs.map(l=>date(l.availableAt!.seconds)))].join(" · ")}</span>}</div>)}
 {source&&<div>{source}{p?.source==="alphavantage"&&<span className="block mt-1">{tr("Дневное закрытие · ретроспективная оценка","Daily close · retrospective valuation")}</span>}{p?.source==="cbr"&&<span className="block mt-1">{tr("Официальный курс","Official rate")}</span>}</div>}
 {!query.isPending&&rows.some(r=>!r.value)&&<p role="status" className="text-amber-700 dark:text-amber-400">{tr("Для части пар нет актуального курса с подтверждённым источником.","Some pairs lack a current rate with verified source evidence.")}</p>}

 </section>;
}
