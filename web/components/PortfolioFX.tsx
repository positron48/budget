"use client";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import {currentFXRow,currentFXChange} from "@/lib/portfolio-current-fx";
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
 const rows=["USD","EUR"].map(from=>({from,value:currentFXRow(p,from),change:currentFXChange(p,from)}));
 const date=(seconds:bigint)=>new Date(Number(seconds)*1000).toISOString().replace("T"," ").replace(".000Z"," UTC");
 return <section aria-label={tr("Валютные курсы портфеля","Portfolio exchange rates")} className="rounded-2xl border border-slate-200 bg-[hsl(var(--card))] px-6 py-5 dark:border-slate-700/40 ">
 <h2 className="mb-4 font-semibold">{tr("Мои котировки","My quotes")}</h2>
 <div className="flex flex-wrap gap-x-12 gap-y-4">{rows.map(({from,value,change})=><div key={from} className="flex items-center gap-3" title={[source,value?.legs.map(l=>date(l.asOf!.seconds)).join(" · "),change?tr("Сравнение с ","Compared with ")+date(change.legs[0].asOf!.seconds):""].filter(Boolean).join(" · ")}>
 <span className="text-2xl" aria-hidden="true">{from==="USD"?"🇺🇸":"🇪🇺"}</span><span className="text-sm font-semibold">{from}RUB</span>
 <span className="tabular-nums">{query.isPending?"…":value?new Intl.NumberFormat(ru?"ru":"en",{maximumFractionDigits:4}).format(Number(positionDecimal(value.rate))):"—"} ₽</span>
 <span className={"text-sm tabular-nums "+(change?BigInt(change.change.numerator)>=0n?"text-emerald-500":"text-rose-500":"text-slate-400")}>{change?(BigInt(change.change.numerator)>=0n?"▲ ":"▼ ")+new Intl.NumberFormat(ru?"ru":"en",{style:"percent",maximumFractionDigits:2}).format(Math.abs(Number(positionDecimal(change.relative)))):"—"}</span>
 </div>)}</div>
 </section>;
}
