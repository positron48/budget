"use client";
import {useEffect,useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import type {GetMarketDataJobResponse,GetMarketDividendFactsResponse,ListMarketSourcesResponse,MarketDividend} from "@/proto/budget/v1/market_data_pb";
import {formatForecastInstant} from "@/lib/investment-forecast";

type Selection={source:{provider:string;accessScope:string};instrumentId:string;fromDate:string;toDate:string};
export type DeclarationDraft={nature:string;currency:string;recordDate:string;paymentDate:string;timezone:string;sourceReference:string;evidence:string};
const field="block w-full rounded border border-gray-300 bg-white p-2 dark:border-gray-700 dark:bg-gray-900";
const button="rounded border border-blue-400 px-3 py-2 text-sm text-blue-600 disabled:opacity-50";
const day=(v?:{seconds:bigint;nanos:number})=>v?formatForecastInstant(v).slice(0,10):"";
const stamp=(value:string,end=false)=>({seconds:BigInt(Math.floor(Date.parse(value+"T00:00:00Z")/1000)+(end?86399:0)),nanos:0});
const request=(s:Selection)=>({source:s.source,instrumentId:s.instrumentId,from:stamp(s.fromDate),to:stamp(s.toDate,true)});

export default function InvestmentDividendSource({tenantId,canWrite,onDraft}:{tenantId:string;canWrite:boolean;onDraft:(d:DeclarationDraft)=>void}) {
 const t=useTranslations("investmentDividendSource"),{marketData:api}=useClients(),qc=useQueryClient();
 const [open,setOpen]=useState(false),[sourceIndex,setSourceIndex]=useState("");
 const [form,setForm]=useState(()=>({instrumentId:"",fromDate:new Date().toISOString().slice(0,10),toDate:new Date(Date.now()+364*86400000).toISOString().slice(0,10)}));
 const [selection,setSelection]=useState<Selection|null>(null),[offset,setOffset]=useState(0);
 const [job,setJob]=useState<{id:string;selection:Selection;until:number}|null>(null);
 const sources=useQuery<ListMarketSourcesResponse>({queryKey:["investment",tenantId,"dividend-sources"],queryFn:()=>api.listMarketSources({}),enabled:open,retry:false});
 const facts=useQuery<GetMarketDividendFactsResponse>({queryKey:["investment",tenantId,"dividend-source",selection],queryFn:()=>api.getMarketDividendFacts(request(selection!)),enabled:open&&!!selection,retry:false,refetchOnWindowFocus:false});
 const data=facts.error?undefined:facts.data;
 const load=useMutation({retry:false,mutationFn:(s:Selection)=>api.requestMarketRefresh({...request(s),kind:"dividend_facts"}),onSuccess:(value:any,s)=>{if(value.job)setJob({id:value.job.id,selection:s,until:Date.now()+60000})}});
 const sameJob=!!job&&JSON.stringify(job.selection)===JSON.stringify(selection);
 const status=useQuery<GetMarketDataJobResponse>({queryKey:["investment",tenantId,"dividend-source-job",job?.id],queryFn:()=>api.getMarketDataJob({id:job!.id}),enabled:open&&sameJob,refetchInterval:q=>Date.now()<(job?.until||0)&&["pending","running"].includes(q.state.data?.job?.status||"pending")?2000:false,retry:false});
 useEffect(()=>{if(sameJob&&status.data?.job?.status==="ready")void qc.invalidateQueries({queryKey:["investment",tenantId,"dividend-source",selection]})},[sameJob,status.data?.job?.status,qc,tenantId,selection]);
 const prepare=(d:MarketDividend)=>{
  if(!data||!selection)return;
  onDraft({nature:"announced",currency:d.dividendNet!.currency,recordDate:day(d.recordDate),paymentDate:day(d.paymentDate),timezone:"UTC",sourceReference:`tinvest:GetDividends:${selection.source.accessScope}:${data.instrument?.instrumentId}:${data.snapshotId}:${data.payloadHash}`,evidence:JSON.stringify({providerField:"dividendNet",providerAmount:d.dividendNet?.amount,recordDate:formatForecastInstant(d.recordDate),paymentDate:formatForecastInstant(d.paymentDate),declaredDate:formatForecastInstant(d.declaredDate),lastBuyDate:formatForecastInstant(d.lastBuyDate),dividendType:d.dividendType,regularity:d.regularity,asOf:formatForecastInstant(data.asOf)})});
 };
 return <details className="rounded-xl border p-4" onToggle={e=>setOpen(e.currentTarget.open)}><summary className="cursor-pointer font-semibold">{t("title")}</summary>{open&&<div className="space-y-4 pt-4">
  <p className="text-sm text-gray-500">{t("policy")}</p>
  {sources.error&&<p role="alert">{t("error")}</p>}
  <form className="grid gap-3 sm:grid-cols-2" onSubmit={e=>{e.preventDefault();const source=sources.data?.sources[Number(sourceIndex)]?.source;if(!source)return;setSelection({source:{provider:source.provider,accessScope:source.accessScope},...form});setOffset(0);load.reset()}}>
   <label>{t("source")}<select required className={field} value={sourceIndex} onChange={e=>setSourceIndex(e.target.value)}><option value="">{t("choose")}</option>{sources.data?.sources.map((s,i)=><option value={i} key={s.source?.provider+":"+s.source?.accessScope}>{s.title} · {s.source?.accessScope}</option>)}</select></label>
   <label>{t("uid")}<input required className={field} value={form.instrumentId} pattern="[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}" onChange={e=>setForm(f=>({...f,instrumentId:e.target.value}))}/></label>
   {(["fromDate","toDate"] as const).map(k=><label key={k}>{t(k)}<input required className={field} type="date" min="1900-01-01" max="2199-12-31" value={form[k]} onChange={e=>setForm(f=>({...f,[k]:e.target.value}))}/></label>)}
   <button className={button} disabled={sources.isPending}>{t("read")}</button>
  </form>
  {selection&&<div className="flex flex-wrap gap-3"><button type="button" className={button} disabled={facts.isFetching} onClick={()=>void facts.refetch()}>{t("readAgain")}</button>{canWrite&&<button type="button" className={button} disabled={load.isPending} onClick={()=>load.mutate(selection)}>{t("request")}</button>}</div>}
  {(facts.isFetching||load.isPending)&&<p role="status">{t("loading")}</p>}{(facts.error||load.error||status.error)&&<p role="alert">{t("error")}</p>}
  {sameJob&&status.data?.job&&<p role="status">{t("job")}: {t("states."+status.data.job.status)}</p>}
  {data&&<><p className="break-all text-sm">{data.instrument?.name} · {data.instrument?.instrumentId} · {t(data.coverageState==="unavailable"?"unavailable":"observations")}</p>{data.snapshotId&&<p className="break-all text-xs text-gray-500">{t("asOf")}: {formatForecastInstant(data.asOf)} · {data.snapshotId} · {data.payloadHash}</p>}{data.snapshotId&&data.dividends.length===0&&<p>{t("empty")}</p>}
   {data.dividends.slice(offset,offset+25).map((d,i)=><article className="space-y-2 rounded border p-3" key={offset+i}>
    <p className="break-all">{t("record")}: {formatForecastInstant(d.recordDate)||"—"} · {t("payment")}: {formatForecastInstant(d.paymentDate)||"—"}</p>
    <p className="break-all font-mono">{t("amount")}: {d.dividendNet?.amount??"—"} {d.dividendNet?.currency} · {d.dividendType||"—"} · {d.regularity||"—"}</p>
    <p className="break-all text-sm">{t("declared")}: {formatForecastInstant(d.declaredDate)||"—"} · {t("lastBuy")}: {formatForecastInstant(d.lastBuyDate)||"—"}</p>
    {canWrite&&<button className={button} type="button" disabled={d.dividendType!=="Regular Cash"||!d.declaredDate||!d.paymentDate||!d.dividendNet||!["RUB","USD","EUR"].includes(d.dividendNet.currency)} onClick={()=>prepare(d)}>{t("draft")}</button>}
   </article>)}
   {data.dividends.length>25&&<nav className="flex gap-3"><button className={button} disabled={offset===0} onClick={()=>setOffset(o=>Math.max(0,o-25))}>{t("previous")}</button><span>{offset+1}–{Math.min(offset+25,data.dividends.length)} / {data.dividends.length}</span><button className={button} disabled={offset+25>=data.dividends.length} onClick={()=>setOffset(o=>o+25)}>{t("next")}</button></nav>}
  </>}
 </div>}</details>;
}
