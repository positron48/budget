"use client";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import {useSearchParams} from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import type { InvestmentPortfolio, InvestmentValuationProfileOption } from "@/proto/budget/v1/investment_nav_pb";
import type { InvestmentLedger } from "@/proto/budget/v1/investment_pb";

const field="mt-1 w-full rounded-lg border border-gray-300 bg-white p-2 dark:border-gray-700 dark:bg-gray-900";
const button="rounded-lg border border-gray-300 px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";
type Options={priceProfiles:InvestmentValuationProfileOption[];fxProfiles:InvestmentValuationProfileOption[];nextPriceKey:string;nextFxKey:string};
const yesterday=()=>{const parts=new Intl.DateTimeFormat("en-CA",{timeZone:"Europe/Moscow",year:"numeric",month:"2-digit",day:"2-digit"}).formatToParts(new Date());const get=(key:string)=>parts.find(p=>p.type===key)!.value;return new Date(Date.parse(`${get("year")}-${get("month")}-${get("day")}T00:00:00Z`)-86400000).toISOString().slice(0,10);};
export default function InvestmentPortfolios({tenantId,onView}:{tenantId:string;onView:(p:InvestmentPortfolio)=>void}){
 const readOnly=useInvestmentReadOnly(),params=useSearchParams();
 const initialAccount=params.get("newAccount")||"",initialCurrency=params.get("currency")||"RUB";
 const t=useTranslations("investmentPortfolios");const {investmentNAV:api,investment}=useClients();const qc=useQueryClient();
 const [autoExtend,setAutoExtend]=useState(false);
 const [open,setOpen]=useState(!!initialAccount&&!readOnly),[editing,setEditing]=useState<InvestmentPortfolio>();
 const [name,setName]=useState(""),[ids,setIDs]=useState<string[]>(initialAccount?[initialAccount]:[]),[currency,setCurrency]=useState(["RUB","USD","EUR"].includes(initialCurrency)?initialCurrency:"RUB"),[price,setPrice]=useState(""),[fx,setFX]=useState("");
 const [from,setFrom]=useState(yesterday),[to,setTo]=useState(yesterday);
 const retry=useRef({signature:"",key:""});
 const list=useInfiniteQuery({queryKey:["investmentNAV",tenantId,"portfolios"],initialPageParam:"",queryFn:({pageParam}):Promise<{portfolios:InvestmentPortfolio[];nextAfterId:string}>=>api.listInvestmentPortfolios({afterId:pageParam}),getNextPageParam:last=>last.nextAfterId||undefined});
 const options=useInfiniteQuery({queryKey:["investmentNAV",tenantId,"profiles"],enabled:open,initialPageParam:{price:"",fx:""},queryFn:({pageParam}):Promise<Options>=>api.listInvestmentValuationProfiles({afterPriceKey:pageParam.price,afterFxKey:pageParam.fx}),getNextPageParam:(last,_pages,param)=>last.nextPriceKey||last.nextFxKey?{price:last.nextPriceKey||last.priceProfiles.at(-1)?.key||param.price,fx:last.nextFxKey||last.fxProfiles.at(-1)?.key||param.fx}:undefined});
 const ledgers=useInfiniteQuery({queryKey:["investmentNAV",tenantId,"accountOptions"],enabled:open,initialPageParam:0,queryFn:({pageParam}):Promise<{ledgers:InvestmentLedger[]}>=>investment.listInvestmentLedgers({limit:50,offset:pageParam}),getNextPageParam:(last,_pages,param)=>last.ledgers.length===50?param+50:undefined});
 const prices=Array.from(new Map(options.data?.pages.flatMap(p=>p.priceProfiles).map(p=>[p.key,p])).values());
 const rates=Array.from(new Map(options.data?.pages.flatMap(p=>p.fxProfiles).map(p=>[p.key,p])).values());
 const defaultMarketFX=rates.find(p=>p.mode==="market")?.key||"";
 useEffect(()=>{
  // Creation defaults to an explicitly identified market policy. Never infer
  // its mode from its key, choose CBR as a fallback, or overwrite an edit.
  if(open&&!editing&&!fx&&defaultMarketFX)setFX(defaultMarketFX);
 },[open,editing,fx,defaultMarketFX]);
 const accounts=ledgers.data?.pages.flatMap(p=>p.ledgers)||[];
 const save=useMutation({mutationFn:()=>{
  const body={id:editing?.id||"",name,ledgerIds:[...ids].sort(),currency,conventionKey:price,fxPolicyKey:fx,fromDate:from,toDate:to,expectedVersion:editing?.version||0n,archived:editing?.archived||false,autoExtend};
  const signature=JSON.stringify({...body,expectedVersion:String(body.expectedVersion)});
  if(retry.current.signature!==signature)retry.current={signature,key:crypto.randomUUID()};
  return api.saveInvestmentPortfolio({...body,requestKey:retry.current.key}) as Promise<{portfolio?:InvestmentPortfolio}>;
 },onSuccess:result=>{if(result.portfolio){setOpen(false);onView(result.portfolio);qc.invalidateQueries({queryKey:["investmentNAV",tenantId]});}}});
 const archiveRetry=useRef({signature:"",key:""});
 const archive=useMutation({mutationFn:(p:InvestmentPortfolio)=>{
  const body={id:p.id,name:p.name,ledgerIds:p.ledgerIds,currency:p.currency,conventionKey:p.conventionKey,fxPolicyKey:p.fxPolicyKey,fromDate:p.fromDate,toDate:p.toDate,expectedVersion:p.version,archived:!p.archived,autoExtend:p.autoExtend};
  const signature=JSON.stringify({...body,expectedVersion:String(body.expectedVersion)});
  if(archiveRetry.current.signature!==signature)archiveRetry.current={signature,key:crypto.randomUUID()};
  return api.saveInvestmentPortfolio({...body,requestKey:archiveRetry.current.key}) as Promise<{portfolio?:InvestmentPortfolio}>;
 },onSuccess:result=>{if(result.portfolio){setOpen(false);onView(result.portfolio);qc.invalidateQueries({queryKey:["investmentNAV",tenantId]});}}});
 function edit(p?:InvestmentPortfolio){setEditing(p);setAutoExtend(p?.autoExtend||false);setName(p?.name||"");setIDs(p?.ledgerIds||[]);setCurrency(p?.currency||"RUB");setPrice(p?.conventionKey||"");setFX(p?.fxPolicyKey||"");setFrom(p?.fromDate||yesterday());setTo(p?.toDate||yesterday());retry.current={signature:"",key:""};save.reset();archive.reset();setOpen(true);}
 const valid=!!name.trim()&&ids.length>0&&ids.length<=20&&!!price&&!!fx&&!!from&&from<=to&&to<=yesterday()&&(Date.parse(to)-Date.parse(from))/86400000<=3660;
 const error=list.error||options.error||ledgers.error||save.error||archive.error;
 return <section className="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-gray-800">
  <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-xl font-semibold">{t("title")}</h2><button className={button} disabled={readOnly||save.isPending||archive.isPending} onClick={()=>edit()}>{t("create")}</button></div>
  {error&&<p role="alert" className="text-red-600">{(error as {code?:number}).code===10?t("conflict"):(error as {code?:number}).code===9?t("precondition"):t("error")}</p>}
  {error&&<button className={button} onClick={()=>{list.refetch();if(open){options.refetch();ledgers.refetch();}}}>{t("retry")}</button>}
  <div className="space-y-2">{list.data?.pages.flatMap(p=>p.portfolios).map(p=><div key={p.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-gray-50 p-3 dark:bg-gray-900"><button className="break-all text-left text-blue-600" onClick={()=>onView(p)}>{p.name} · {p.currency}{p.archived?` · ${t("archived")}`:""}</button><button className={button} disabled={readOnly||save.isPending||archive.isPending} onClick={()=>edit(p)}>{t("edit")}</button><button className={button} disabled={readOnly||save.isPending||archive.isPending} onClick={()=>{archive.reset();save.reset();archive.mutate(p);}}>{p.archived?t("restore"):t("archive")}</button></div>)}</div>
  {list.hasNextPage&&<button className={button} disabled={list.isFetchingNextPage} onClick={()=>list.fetchNextPage()}>{t("more")}</button>}
  {open&&!readOnly&&<form className="space-y-4 border-t border-gray-200 pt-4 dark:border-gray-800" onSubmit={e=>{e.preventDefault();if(valid)save.mutate();}}>
   <fieldset disabled={readOnly||save.isPending||archive.isPending} className="space-y-4">
    <legend className="mb-3 font-medium">{editing?t("editing"):t("creating")}</legend>
    <label className="block">{t("name")}<input className={field} value={name} maxLength={200} required onChange={e=>setName(e.target.value)}/></label>
    <fieldset className="space-y-2"><legend>{t("accounts",{count:ids.length})}</legend>{accounts.map(a=><label key={a.id} className="flex items-start gap-2"><input type="checkbox" className="mt-1" checked={ids.includes(a.id)} disabled={!ids.includes(a.id)&&ids.length>=20} onChange={e=>setIDs(e.target.checked?[...ids,a.id]:ids.filter(id=>id!==a.id))}/><span className="break-all">{a.name}</span></label>)}
     {ids.filter(id=>!accounts.some(a=>a.id===id)).map(id=><label key={id} className="flex gap-2"><input type="checkbox" checked onChange={()=>setIDs(ids.filter(v=>v!==id))}/><span className="break-all">{id}</span></label>)}
     {ledgers.hasNextPage&&<button type="button" className={button} disabled={ledgers.isFetchingNextPage} onClick={()=>ledgers.fetchNextPage()}>{t("moreAccounts")}</button>}
    </fieldset>
    <div className="grid gap-3 sm:grid-cols-3">
     <label>{t("currency")}<select aria-label={t("currency")} className={field} value={currency} onChange={e=>setCurrency(e.target.value)}>{["RUB","USD","EUR"].map(c=><option key={c}>{c}</option>)}</select></label>
     <label>{t("prices")}<select aria-label={t("prices")} className={field} value={price} required onChange={e=>setPrice(e.target.value)}><option value="">{t("choose")}</option>{price&&!prices.some(p=>p.key===price)&&<option value={price}>{price}</option>}{prices.map(p=><option key={p.key} value={p.key}>{p.title}</option>)}</select></label>
     <label>{t("fx")}<select aria-label={t("fx")} className={field} value={fx} required onChange={e=>setFX(e.target.value)}><option value="">{t("choose")}</option>{fx&&!rates.some(p=>p.key===fx)&&<option value={fx}>{fx}</option>}{rates.map(p=><option key={p.key} value={p.key}>{p.title} · {p.mode==="cbr"?t("cbr"):t("market")}</option>)}</select></label>
    </div>
    {options.hasNextPage&&<button type="button" className={button} disabled={options.isFetchingNextPage} onClick={()=>options.fetchNextPage()}>{t("moreProfiles")}</button>}
    {!options.isPending&&(!prices.length||!rates.length)&&<p className="text-sm text-amber-700 dark:text-amber-400">{t("noProfiles")}</p>}
    <div className="grid gap-3 sm:grid-cols-2"><label>{t("from")}<input className={field} type="date" required max={to} value={from} onChange={e=>setFrom(e.target.value)}/></label><label>{t("to")}<input className={field} type="date" required min={from} max={yesterday()} value={to} onChange={e=>setTo(e.target.value)}/></label></div>
    <label className="flex items-start gap-2"><input type="checkbox" className="mt-1" checked={autoExtend} onChange={e=>setAutoExtend(e.target.checked)}/><span>{t("autoExtend")}</span></label>
    <p className="text-sm text-gray-500">{t("coverage")}</p>
    <div className="flex gap-3"><button className={button} disabled={!valid||options.isPending||ledgers.isPending}>{save.isPending?t("saving"):t("save")}</button><button type="button" className={button} onClick={()=>setOpen(false)}>{t("cancel")}</button></div>
   </fieldset>
  </form>}
 </section>;
}
