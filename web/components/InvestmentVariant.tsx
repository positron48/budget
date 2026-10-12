"use client";
import {useEffect,useRef,useState} from "react";
import {useInfiniteQuery,useQueryClient} from "@tanstack/react-query";
import {useSearchParams,usePathname,useRouter} from "next/navigation";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import {authStore} from "@/lib/auth/store";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import {useInvestmentRange} from "@/components/InvestmentPeriod";
import type {InvestmentNAVSeries} from "@/proto/budget/v1/investment_nav_pb";
const field="rounded-lg border border-gray-300 bg-transparent px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";
export default function InvestmentVariant({tenantId,series,archived=false}:{tenantId:string;series:InvestmentNAVSeries;archived?:boolean}){
 const t=useTranslations("investmentVariant"),{investmentNAV:api}=useClients(),readOnly=useInvestmentReadOnly(),qc=useQueryClient(),params=useSearchParams(),pathname=usePathname(),router=useRouter();
 const range=useInvestmentRange(series.key,series.fromDate,series.toDate);
 const [currency,setCurrency]=useState(series.currency),[fx,setFX]=useState(series.fxPolicyKey),[pending,setPending]=useState(false),[error,setError]=useState("");
 const mounted=useRef(true),latest=useRef("");latest.current=`${series.key}:${params}:${pathname}`;
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[]);
 const options=useInfiniteQuery({queryKey:["investmentNAV",tenantId,"profiles"],initialPageParam:{price:"",fx:""},queryFn:({pageParam})=>api.listInvestmentValuationProfiles({afterPriceKey:pageParam.price,afterFxKey:pageParam.fx}),getNextPageParam:(last,_pages,param)=>last.nextPriceKey||last.nextFxKey?{price:last.nextPriceKey||last.priceProfiles.at(-1)?.key||param.price,fx:last.nextFxKey||last.fxProfiles.at(-1)?.key||param.fx}:undefined,refetchInterval:30000});
 const rates=Array.from(new Map(options.data?.pages.flatMap(p=>p.fxProfiles).map(p=>[p.key,p])).values());
 const apply=async()=>{
  const signature=latest.current;
  const current=()=>mounted.current&&authStore.getTenant()===tenantId&&latest.current===signature;
  setPending(true);setError("");
  try{
   const req={sourceSeriesKey:series.key,currency,fxPolicyKey:fx};
   let response=await api.getInvestmentNAVVariant(req);
   if(!current())return;
   if(!response.registered){if(readOnly||archived){setError(t(readOnly?"readOnly":"archived"));return;}response=await api.prepareInvestmentNAVVariant(req);}
   if(!current())return;
   const target=response.series;
   if(!response.registered||!target||target.currency!==currency||target.fxPolicyKey!==fx||target.ledgerIds.length!==series.ledgerIds.length||[...target.ledgerIds].sort().some((id,i)=>id!==[...series.ledgerIds].sort()[i])||target.fromDate>range.from||target.toDate<range.to)throw new Error("Incompatible valuation scope");
   qc.setQueryData(["investmentNAV",tenantId,"seriesInfo",target.key],{series:target});
   void qc.invalidateQueries({queryKey:["investmentNAV",tenantId,"overviewSeries"]});
   void qc.invalidateQueries({queryKey:["investmentNAV",tenantId,"series"]});
   const next=new URLSearchParams(params.toString());next.set("series",target.key);next.set("from",range.from);next.set("to",range.to);if(pathname.includes("/instruments/"))next.set("date",range.to);
   router.replace(`${pathname}?${next}`,{scroll:false});
  }catch(e){if(current())setError((e as {code?:number}).code===9?t("precondition"):t("error"));}
  finally{if(mounted.current)setPending(false);}
 };
 return <section className="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-gray-900" data-testid="investment-variant" aria-label={t("title")}><h3 className="text-sm font-semibold">{t("title")}</h3><div className="flex flex-wrap items-end gap-3"><label>{t("currency")}<select aria-label={t("currency")} className={field+" block"} value={currency} disabled={pending} onChange={e=>{setCurrency(e.target.value);setError("")}}>{["RUB","USD","EUR"].map(value=><option key={value} value={value}>{value}</option>)}</select></label><label className="min-w-0 max-w-full">{t("fx")}<select aria-label={t("fx")} className={field+" block max-w-full"} value={fx} disabled={pending} onChange={e=>{setFX(e.target.value);setError("")}}>{!rates.some(p=>p.key===fx)&&<option value={fx}>{fx}</option>}{rates.map(p=><option key={p.key} value={p.key}>{t(p.mode==="cbr"?"cbr":"market")} · {p.key} · v{String(p.version)}</option>)}</select></label><button className={field} disabled={pending||!range.valid||!fx||(currency===series.currency&&fx===series.fxPolicyKey)} onClick={()=>void apply()}>{pending?t("loading"):t("apply")}</button></div><p className="text-xs text-gray-500">{t("method")}</p>{readOnly&&<p className="text-xs text-gray-500">{t("readOnlyNote")}</p>}{options.hasNextPage&&<button className={field} disabled={options.isFetchingNextPage} onClick={()=>options.fetchNextPage()}>{t("more")}</button>}{options.error&&<p role="alert" className="text-sm text-red-600">{t("optionsError")} <button className="underline" onClick={()=>options.refetch()}>{t("retry")}</button></p>}{error&&<p role="alert" className="text-sm text-red-600">{error}</p>}</section>;
}
