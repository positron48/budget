"use client";
import {useEffect,useRef,useState} from "react";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import {useSearchParams} from "next/navigation";
import PortfolioSourceReview from "@/components/PortfolioSourceReview";
import type {MarketInstrument} from "@/proto/budget/v1/market_data_pb";
import Link from "next/link";
import {useLocale} from "next-intl";
import {useQuery,useQueries} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import {loadBrokerCatalog} from "@/lib/portfolio-import";
import {instrumentHref} from "@/lib/portfolio-instrument";
import {trackerError} from "@/lib/portfolio-tracker";
import type {ListMarketSourcesResponse,SearchMarketInstrumentsResponse} from "@/proto/budget/v1/market_data_pb";
const button="rounded-lg border border-slate-300 bg-transparent px-3 py-2 text-sm dark:border-slate-700 disabled:opacity-40";
function Finder({tenantId}:{tenantId:string}){
 const ru=useLocale()==="ru",tr=(r:string,e:string)=>ru?r:e,{marketData:api}=useClients(),readOnly=useInvestmentReadOnly();
 const params=useSearchParams(),connect=params.get("connect")||"",securityName=params.get("security")?.slice(0,200)||connect;
 const [purpose,setPurpose]=useState<"current"|"history">("history"),[chosen,setChosen]=useState<MarketInstrument>();
 const [sourceIndex,setSourceIndex]=useState(0),[query,setQuery]=useState(""),[kind,setKind]=useState("share"),[busy,setBusy]=useState(false),[message,setMessage]=useState("");
 const sources=useQuery<ListMarketSourcesResponse>({queryKey:["market",tenantId,"card-sources"],queryFn:()=>api.listMarketSources({})});
 const availableSources=(sources.data?.sources||[]).filter(s=>!connect||(purpose==="current"?s.source?.provider==="tinvest":["moex","twelvedata"].includes(s.source?.provider||"")));
 const source=availableSources[sourceIndex]?.source;
 const matches=useQuery<SearchMarketInstrumentsResponse>({queryKey:["market",tenantId,"card-search",source?.provider,source?.accessScope,query],enabled:!!source&&query.trim().length>=2,queryFn:()=>api.searchMarketInstruments({source,query:query.trim(),limit:50,offset:0}),staleTime:60000});
 const searchTask=useRef<ReturnType<typeof tenantRequestScope>>(undefined);
 useEffect(()=>()=>searchTask.current?.dispose(),[]);
 const supported=matches.data?.instruments.filter(i=>["share","bond","etf"].includes(i.kind))||[];
 const search=async()=>{if(!source||readOnly||query.trim().length<2)return;searchTask.current?.dispose();const task=tenantRequestScope(tenantId);searchTask.current=task;setBusy(true);setMessage("");try{await task.run(options=>api.requestMarketRefresh({source,kind:"catalog_search",searchQuery:query.trim(),instrumentKind:kind},options));setMessage(tr("Поиск у поставщика запрошен. Обновите результаты после загрузки.","Provider search requested. Refresh results after loading."));}catch(e){if(!task.signal.aborted)setMessage(trackerError(e,ru));}finally{if(!task.signal.aborted)setBusy(false);task.dispose();}};
 return <div className="mx-auto max-w-4xl space-y-5 px-4 py-8"><header className="flex flex-wrap items-center justify-between gap-3"><h1 className="text-2xl font-semibold">{tr("Найти бумагу","Find security")}</h1><Link className={button} href="/investments">{tr("К портфелю","Back to portfolio")}</Link></header><p className="text-sm text-slate-500">{tr("Выберите источник и конкретный листинг. Открытие карточки не добавляет покупку и не меняет ваши позиции.","Choose a source and a specific listing. Opening a card does not create a purchase or change your holdings.")}</p>
 {connect&&<><p className="break-all text-sm">{tr("Источник цен для бумаги","Price source for security")}: {securityName}</p><div className="flex flex-wrap gap-2">{([["history",tr("История цен","Historical prices")],["current",tr("Текущие цены","Current prices")]] as const).map(([key,label])=><button className={button+(purpose===key?" bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200":"")} aria-pressed={purpose===key} key={key} onClick={()=>{setPurpose(key);setSourceIndex(0);setChosen(undefined);setMessage("");}}>{label}</button>)}</div><p className="text-xs text-slate-500">{tr("История использует MOEX или Twelve Data; текущие цены — Т‑Инвест. Выбор бумаги требует вашего подтверждения.","History uses MOEX or Twelve Data; current prices use T-Invest. Connecting a security requires your confirmation.")}</p></>}
 <div className="flex flex-wrap gap-3"><select className={button} aria-label={tr("Источник котировок","Price source")} value={sourceIndex} onChange={e=>{setSourceIndex(Number(e.target.value));setChosen(undefined);setMessage("");}}>{availableSources.map((s,i)=><option key={s.source?.provider+":"+s.source?.accessScope} value={i}>{s.title} · {s.source?.provider}/{s.source?.accessScope}</option>)}</select><input className={button+" min-w-0 flex-1"} aria-label={tr("Тикер, название или ISIN","Ticker, name or ISIN")} value={query} onChange={e=>{setQuery(e.target.value);setChosen(undefined);}}/><button className={button} disabled={!source||query.trim().length<2||matches.isFetching} onClick={()=>void matches.refetch()}>{tr("Обновить результаты","Refresh results")}</button></div>
 {!readOnly&&source?.provider==="tinvest"&&<div className="flex flex-wrap gap-2"><select className={button} aria-label={tr("Тип бумаги","Security type")} value={kind} onChange={e=>setKind(e.target.value)}><option value="share">{tr("Акции","Shares")}</option><option value="bond">{tr("Облигации","Bonds")}</option><option value="etf">{tr("Фонды","Funds")}</option></select><button className={button} disabled={busy||query.trim().length<2} onClick={()=>void search()}>{tr("Искать у поставщика","Search provider")}</button></div>}
 {sources.error||matches.error?<p role="alert">{trackerError(sources.error||matches.error,ru)}</p>:null}{message&&<p role="status" className="text-sm text-slate-500">{message}</p>}
 {sources.data&&!sources.data.sources.length&&<p role="status" className="text-sm text-slate-500">{tr("Источники котировок пока не настроены. Личные операции доступны в портфеле.","Price sources are not configured yet. Personal operations remain available in the portfolio.")}</p>}
 {matches.data&&<ul className="divide-y divide-slate-200 rounded-2xl border border-slate-200 dark:divide-slate-800 dark:border-slate-800">{supported.map(i=>{const params=new URLSearchParams({provider:source!.provider,source_scope:source!.accessScope,instrument_id:i.instrumentId});return <li key={i.instrumentId}><Link className="block space-y-1 p-4 hover:bg-slate-50 dark:hover:bg-slate-800" href={instrumentHref(source!.provider+":"+i.instrumentId,params.toString())}><span className="font-semibold">{i.ticker} · {i.name}</span><span className="block text-xs text-slate-500">{i.isin||"ISIN —"} · {i.classCode||"—"} · {i.kind} · {i.currency||"—"}</span></Link>{connect&&!readOnly&&<button className={button+" mx-4 mb-4"} disabled={!i.detailed&&source?.provider!=="tinvest"} onClick={()=>setChosen(i)}>{tr("Выбрать этот листинг","Select this listing")}</button>}</li>;})}</ul>}
 {connect&&chosen&&source&&<PortfolioSourceReview key={[connect,purpose,source.provider,source.accessScope,chosen.instrumentId].join("|")} tenantId={tenantId} securityName={securityName} instrumentKey={connect} instrument={chosen} source={source} purpose={purpose} ru={ru} onClose={()=>setChosen(undefined)}/>}
 {matches.data&&!supported.length&&<p className="text-sm text-slate-500">{tr("В кэше нет совпадений. Для Т‑Инвест можно запросить поиск у поставщика; другие источники показывают только уже известные листинги.","No cached matches. T-Invest can search the provider; other sources show already known listings only.")}</p>}
 </div>;
}
function Search({tenantId}:{tenantId:string}){
 const locale=useLocale(),ru=locale==="ru",tr=(r:string,e:string)=>ru?r:e,{marketData:api,investmentImport:imports}=useClients(),readOnly=useInvestmentReadOnly();
 const params=useSearchParams(),[input,setInput]=useState(""),[query,setQuery]=useState(""),[kind,setKind]=useState(""),[message,setMessage]=useState(""),[busy,setBusy]=useState(false);
 const taskRef=useRef<ReturnType<typeof tenantRequestScope>>(undefined);
 useEffect(()=>{taskRef.current?.dispose();setBusy(false);setMessage("");},[input,kind]);
 useEffect(()=>{const timer=setTimeout(()=>setQuery(input.trim()),250);return()=>clearTimeout(timer);},[input]);
 useEffect(()=>()=>taskRef.current?.dispose(),[]);
 const catalog=useQuery({queryKey:["investment",tenantId,"tracker","broker-quotes"],queryFn:({signal})=>loadBrokerCatalog(imports,{signal}),staleTime:60000});
 const sources=useQuery<ListMarketSourcesResponse>({queryKey:["market",tenantId,"card-sources"],queryFn:({signal})=>api.listMarketSources({},{signal})});
 const available=(sources.data?.sources||[]).flatMap(s=>s.source?[s.source]:[]).slice(0,20);
 const matches=useQueries({queries:available.map(source=>({queryKey:["market",tenantId,"simple-search",source.provider,source.accessScope,query],enabled:query.length>=2,queryFn:({signal}:{signal:AbortSignal})=>api.searchMarketInstruments({source,query,limit:50,offset:0},{signal}) as Promise<SearchMarketInstrumentsResponse>,staleTime:60000}))});
 const q=query.toLocaleLowerCase(),type=(k:string)=>({share:tr("Акции","Shares"),bond:tr("Облигации","Bonds"),etf:tr("Фонды","Funds")}[k]||tr("Бумага","Security"));
 const local=[...(catalog.data?.names||[])].filter(([,i])=>(!q||[i.ticker,i.name].some(s=>s.toLocaleLowerCase().includes(q)))&&(!kind||i.instrumentType===kind)).map(([key,i])=>({key,ticker:i.ticker,name:i.name,kind:i.instrumentType||"",currency:"",isin:"",exchange:i.classCode||"",href:instrumentHref(key,params.toString()),own:true}));
 const seen=new Set(local.map(r=>r.key));
 const rows=[...local,...matches.flatMap((m,index)=>(m.data?.instruments||[]).filter(i=>["share","bond","etf"].includes(i.kind)&&(!kind||i.kind===kind)).flatMap(i=>{const source=available[index],key=source.provider+":"+i.instrumentId;if(seen.has(key))return [];seen.add(key);const p=new URLSearchParams(params);p.set("provider",source.provider);p.set("source_scope",source.accessScope);p.set("instrument_id",i.instrumentId);p.delete("fx");return [{key:source.provider+":"+i.instrumentId,ticker:i.ticker,name:i.name,kind:i.kind,currency:i.currency,isin:i.isin,exchange:i.classCode,href:instrumentHref(source.provider+":"+i.instrumentId,p.toString()),own:false}];}))].slice(0,100);
 const loading=catalog.isPending||sources.isPending||matches.some(m=>m.isFetching)||busy;
 const search=async()=>{setQuery(input.trim());const source=available.find(s=>s.provider==="tinvest");if(!source||readOnly||input.trim().length<2)return;taskRef.current?.dispose();const task=tenantRequestScope(tenantId);taskRef.current=task;setBusy(true);setMessage("");try{
  for(const instrumentKind of kind?[kind]:["share","bond","etf"])await task.run(options=>api.requestMarketRefresh({source,kind:"catalog_search",searchQuery:input.trim(),instrumentKind},options));
  for(let i=0;i<6&&!task.signal.aborted;i++){await new Promise(resolve=>setTimeout(resolve,1500));if(task.signal.aborted)break;await Promise.all(matches.map(m=>m.refetch()));}
 }catch(e){if(!task.signal.aborted)setMessage(trackerError(e,ru));}finally{if(!task.signal.aborted)setBusy(false);task.dispose();}};
 return <div className="mx-auto max-w-5xl space-y-6 px-4 py-8"><h1 className="text-2xl font-semibold">{tr("Найти бумагу","Find security")}</h1>
 <form className="flex flex-wrap gap-3" onSubmit={e=>{e.preventDefault();void search();}}><input autoFocus className={button+" min-w-0 flex-1"} aria-label={tr("Тикер, название или ISIN","Ticker, name or ISIN")} placeholder={tr("Название, тикер или ISIN","Name, ticker or ISIN")} maxLength={100} value={input} onChange={e=>{setInput(e.target.value);setMessage("");}}/><select className={button} aria-label={tr("Тип бумаги","Security type")} value={kind} onChange={e=>setKind(e.target.value)}><option value="">{tr("Все бумаги","All securities")}</option>{["share","bond","etf"].map(k=><option key={k} value={k}>{type(k)}</option>)}</select><button className={button+" bg-emerald-600 text-white"} disabled={busy||input.trim().length<2}>{tr("Найти","Search")}</button></form>
 {message&&<p role="alert">{message}</p>}
 {sources.error||catalog.error||matches.some(m=>m.error)?<p role="alert" className="text-sm text-rose-600">{tr("Часть результатов недоступна. Повторите поиск.","Some results are unavailable. Try searching again.")}</p>:null}
 {loading&&<p role="status" className="text-sm text-slate-500">{tr("Ищем бумаги…","Searching securities…")}</p>}
 {!query&&<h2 className="text-sm font-medium text-slate-500">{tr("Бумаги из вашей истории","Securities from your history")}</h2>}
 <ul className="divide-y divide-slate-100 overflow-hidden rounded-xl bg-[hsl(var(--card))] dark:divide-slate-700 ">{rows.map(r=><li key={r.key}><Link className="flex items-center justify-between gap-4 p-4 hover:bg-slate-50 dark:hover:bg-white/5" href={r.href}><div className="min-w-0"><p className="font-medium"><span className="mr-3 text-sky-600 dark:text-sky-400">{r.ticker}</span>{r.name}</p>{(r.isin||r.exchange)&&<p className="mt-1 text-xs text-slate-500">{[r.exchange,r.isin].filter(Boolean).join(" · ")}</p>}</div><span className="shrink-0 text-xs text-slate-500">{type(r.kind)}{r.currency?" · "+r.currency:""}</span></Link></li>)}</ul>
 {!loading&&!rows.length&&<p className="py-8 text-center text-sm text-slate-500">{tr("Бумаги не найдены. Проверьте название или тикер и нажмите «Найти».","No securities found. Check the name or ticker and select Search.")}</p>}
 </div>;
}
export default function PortfolioInstrumentFinder(){const tenant=useAssetTenant(),params=useSearchParams();return tenant?params.has("connect")?<Finder key={tenant} tenantId={tenant}/>:<Search key={tenant} tenantId={tenant}/>:null;}
