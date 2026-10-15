"use client";
import {useEffect,useRef,useState} from "react";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import Link from "next/link";
import {useLocale} from "next-intl";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import {instrumentHref} from "@/lib/portfolio-instrument";
import {trackerError} from "@/lib/portfolio-tracker";
import type {ListMarketSourcesResponse,SearchMarketInstrumentsResponse} from "@/proto/budget/v1/market_data_pb";
const button="rounded-lg border border-slate-300 bg-transparent px-3 py-2 text-sm dark:border-slate-700 disabled:opacity-40";
function Finder({tenantId}:{tenantId:string}){
 const ru=useLocale()==="ru",tr=(r:string,e:string)=>ru?r:e,{marketData:api}=useClients(),readOnly=useInvestmentReadOnly();
 const [sourceIndex,setSourceIndex]=useState(0),[query,setQuery]=useState(""),[kind,setKind]=useState("share"),[busy,setBusy]=useState(false),[message,setMessage]=useState("");
 const sources=useQuery<ListMarketSourcesResponse>({queryKey:["market",tenantId,"card-sources"],queryFn:()=>api.listMarketSources({})});
 const source=sources.data?.sources[sourceIndex]?.source;
 const matches=useQuery<SearchMarketInstrumentsResponse>({queryKey:["market",tenantId,"card-search",source?.provider,source?.accessScope,query],enabled:!!source&&query.trim().length>=2,queryFn:()=>api.searchMarketInstruments({source,query:query.trim(),limit:50,offset:0}),staleTime:60000});
 const searchTask=useRef<ReturnType<typeof tenantRequestScope>>(undefined);
 useEffect(()=>()=>searchTask.current?.dispose(),[]);
 const supported=matches.data?.instruments.filter(i=>["share","bond","etf"].includes(i.kind))||[];
 const search=async()=>{if(!source||readOnly||query.trim().length<2)return;searchTask.current?.dispose();const task=tenantRequestScope(tenantId);searchTask.current=task;setBusy(true);setMessage("");try{await task.run(options=>api.requestMarketRefresh({source,kind:"catalog_search",searchQuery:query.trim(),instrumentKind:kind},options));setMessage(tr("Поиск у поставщика запрошен. Обновите результаты после загрузки.","Provider search requested. Refresh results after loading."));}catch(e){if(!task.signal.aborted)setMessage(trackerError(e,ru));}finally{if(!task.signal.aborted)setBusy(false);task.dispose();}};
 return <div className="mx-auto max-w-4xl space-y-5 px-4 py-8"><header className="flex flex-wrap items-center justify-between gap-3"><h1 className="text-2xl font-semibold">{tr("Найти бумагу","Find security")}</h1><Link className={button} href="/investments">{tr("К портфелю","Back to portfolio")}</Link></header><p className="text-sm text-slate-500">{tr("Выберите источник и конкретный листинг. Открытие карточки не добавляет покупку и не меняет ваши позиции.","Choose a source and a specific listing. Opening a card does not create a purchase or change your holdings.")}</p>
 <div className="flex flex-wrap gap-3"><select className={button} aria-label={tr("Источник котировок","Price source")} value={sourceIndex} onChange={e=>{setSourceIndex(Number(e.target.value));setMessage("");}}>{sources.data?.sources.map((s,i)=><option key={s.source?.provider+":"+s.source?.accessScope} value={i}>{s.title} · {s.source?.provider}/{s.source?.accessScope}</option>)}</select><input className={button+" min-w-0 flex-1"} aria-label={tr("Тикер, название или ISIN","Ticker, name or ISIN")} value={query} onChange={e=>setQuery(e.target.value)}/><button className={button} disabled={!source||query.trim().length<2||matches.isFetching} onClick={()=>void matches.refetch()}>{tr("Обновить результаты","Refresh results")}</button></div>
 {!readOnly&&source?.provider==="tinvest"&&<div className="flex flex-wrap gap-2"><select className={button} aria-label={tr("Тип бумаги","Security type")} value={kind} onChange={e=>setKind(e.target.value)}><option value="share">{tr("Акции","Shares")}</option><option value="bond">{tr("Облигации","Bonds")}</option><option value="etf">{tr("Фонды","Funds")}</option></select><button className={button} disabled={busy||query.trim().length<2} onClick={()=>void search()}>{tr("Искать у поставщика","Search provider")}</button></div>}
 {sources.error||matches.error?<p role="alert">{trackerError(sources.error||matches.error,ru)}</p>:null}{message&&<p role="status" className="text-sm text-slate-500">{message}</p>}
 {sources.data&&!sources.data.sources.length&&<p role="status" className="text-sm text-slate-500">{tr("Источники котировок пока не настроены. Личные операции доступны в портфеле.","Price sources are not configured yet. Personal operations remain available in the portfolio.")}</p>}
 {matches.data&&<ul className="divide-y divide-slate-200 rounded-2xl border border-slate-200 dark:divide-slate-800 dark:border-slate-800">{supported.map(i=>{const params=new URLSearchParams({provider:source!.provider,source_scope:source!.accessScope,instrument_id:i.instrumentId});return <li key={i.instrumentId}><Link className="block space-y-1 p-4 hover:bg-slate-50 dark:hover:bg-slate-800" href={instrumentHref(source!.provider+":"+i.instrumentId,params.toString())}><span className="font-semibold">{i.ticker} · {i.name}</span><span className="block text-xs text-slate-500">{i.isin||"ISIN —"} · {i.classCode||"—"} · {i.kind} · {i.currency||"—"}</span></Link></li>;})}</ul>}
 {matches.data&&!supported.length&&<p className="text-sm text-slate-500">{tr("В кэше нет совпадений. Для Т‑Инвест можно запросить поиск у поставщика; другие источники показывают только уже известные листинги.","No cached matches. T-Invest can search the provider; other sources show already known listings only.")}</p>}
 </div>;
}
export default function PortfolioInstrumentFinder(){const tenant=useAssetTenant();return tenant?<Finder key={tenant} tenantId={tenant}/>:null;}
