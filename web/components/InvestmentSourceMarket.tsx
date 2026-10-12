"use client";
import InvestmentChart from "@/components/InvestmentChart";
import InvestmentBondFacts from "@/components/InvestmentBondFacts";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocale, useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import {navGeometry} from "@/lib/investment-nav";
import {decimalFraction,positionDecimal} from "@/lib/investment-positions";
import InvestmentMarketMappingEditor from "@/components/InvestmentMarketMapping";
import type { MarketSourceKey, MarketInstrument, MarketObservation, MarketDataJob } from "@/proto/budget/v1/market_data_pb";

const field = "mt-1 w-full rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-900";
const button = "rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50";
const panel = "space-y-4 rounded-xl border border-gray-200 p-4 dark:border-gray-800";
const day = (d: Date) => d.toISOString().slice(0, 10);
const stamp = (s: string) => ({ seconds: BigInt(Math.floor(new Date(s + "T00:00:00Z").getTime() / 1000)), nanos: 0 });
const requestEnd=(date:string)=>({seconds:BigInt(Math.min(Number(stamp(date).seconds+86400n),Math.floor(Date.now()/1000))),nanos:0});
const exact = (s: string) => s ? s.replace(/(\.\d*?[1-9])0+$|\.0+$/, "$1") : "—";

export default function SourceMarket({ tenantId, source, initialListing="", lazy=false }: { tenantId: string; source: MarketSourceKey; initialListing?:string; lazy?:boolean }) {
  const readOnly=useInvestmentReadOnly();
  const t = useTranslations("market"),card=useTranslations("investmentInstrument");
  const locale = useLocale();
  const { marketData: api } = useClients();
  const qc = useQueryClient();
  const prefix = ["market", tenantId, source.provider, source.accessScope];
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState(initialListing);
  const [assetKind, setAssetKind] = useState("share");
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<MarketInstrument>();
  const [kind, setKind] = useState("daily_candle");
  const [previewSeries,setPreviewSeries]=useState("");
  const [from, setFrom] = useState(() => day(new Date(Date.now() - 30 * 86400000)));
  const [to, setTo] = useState(() => day(new Date()));
  const [activeJob, setActiveJob] = useState<MarketDataJob>();
  const [historyBatch, setHistoryBatch] = useState<{key:string;jobs:MarketDataJob[]}>();
  const completed = useRef("");
  const initialSelected=useRef(false),requested=useRef(new Set<string>());
  const rangeValid = !!from && !!to && from >= "1900-01-01" && from <= to && to <= day(new Date()) && Date.parse(to+"T23:59:59Z") < Date.UTC(Number(from.slice(0,4))+30,Number(from.slice(5,7))-1,Number(from.slice(8,10)));
  const longHistory=kind==="daily_candle"&&(Date.parse(to)-Date.parse(from))>364*86400000;
  const historyKey=JSON.stringify([tenantId,source.provider,source.accessScope,selected?.instrumentId,kind,from,to]);
  const currentBatch=historyBatch?.key===historyKey?historyBatch:undefined;
  const dateLabel = (s?: { seconds: bigint; nanos: number }) => s ? new Date(Number(s.seconds) * 1000 + s.nanos / 1e6).toLocaleString(locale, { timeZone: "UTC" }) + " UTC" : "—";
  const catalog = useQuery<{ instruments: MarketInstrument[] }>({ queryKey: [...prefix, "catalog", query, offset], queryFn: () => api.searchMarketInstruments({ source, query, limit: 25, offset }) });
  useEffect(()=>{if(initialListing&&!initialSelected.current&&catalog.data){initialSelected.current=true;const exact=catalog.data.instruments.find(i=>i.instrumentId===initialListing);if(exact)setSelected(exact);}},[catalog.data,initialListing]);
  const history = useQuery<{ observations: MarketObservation[] }>({
    queryKey: [...prefix, "history", selected?.instrumentId, kind, from, to], enabled: !!selected && rangeValid,
    queryFn: () => api.getMarketHistory({ source, instrumentId: selected!.instrumentId, kind, from: stamp(from), to: { seconds: stamp(to).seconds + 86399n, nanos: 999999000 } }),
  });
  const job = useQuery<{ job?: MarketDataJob }>({
    queryKey: [...prefix, "job", activeJob?.id], enabled: !!activeJob,
    queryFn: () => api.getMarketDataJob({ id: activeJob!.id }),
    initialData: activeJob ? { job: activeJob } : undefined,
    refetchInterval: state => ["pending", "running"].includes(state.state.data?.job?.status || "") ? 1500 : false,
  });
  const currentJob = job.data?.job;
  const batchJobs = useQuery<MarketDataJob[]>({
    queryKey:[...prefix,"history-batch",currentBatch?.jobs.map(j=>j.id)],enabled:!!currentBatch,
    initialData:currentBatch?.jobs,
    queryFn:()=>Promise.all(currentBatch!.jobs.map(async j=>(await api.getMarketDataJob({id:j.id}) as {job?:MarketDataJob}).job||j)),
    refetchInterval:state=>state.state.data?.some(j=>["pending","running"].includes(j.status))?1500:false,
  });
  const batchRecords=currentBatch?batchJobs.data||currentBatch.jobs:[];
  const batchProgress=JSON.stringify(batchRecords.map(j=>[j.id,j.status,j.coverageState]));
  useEffect(()=>{if(currentBatch)void qc.invalidateQueries({queryKey:["market",tenantId,source.provider,source.accessScope,"history"]});},[batchProgress,currentBatch,qc,tenantId,source.provider,source.accessScope]);
  useEffect(() => {
    if (currentJob?.status === "ready" && completed.current !== currentJob.id) {
      completed.current = currentJob.id;
      qc.invalidateQueries({ queryKey: ["market", tenantId, source.provider, source.accessScope, "catalog"] });
      qc.invalidateQueries({ queryKey: ["market", tenantId, source.provider, source.accessScope, "history"] });
    }
  }, [currentJob?.id, currentJob?.status, qc, tenantId, source.provider, source.accessScope]);
  const refresh = useMutation({
    mutationFn: (request: Record<string, unknown>) => api.requestMarketRefresh({ source, ...request }) as Promise<{ job?: MarketDataJob }>,
    onSuccess: result => {
      setActiveJob(result.job);
      if (result.job?.status === "ready") qc.invalidateQueries({ queryKey: prefix });
    },
  });
  const loadHistory=useMutation({
    mutationFn:({instrumentId,fromDate,toDate}:{key:string;instrumentId:string;fromDate:string;toDate:string})=>api.requestMarketHistory({source,instrumentId,from:stamp(fromDate),to:requestEnd(toDate)}) as Promise<{jobs:MarketDataJob[]}>,
    onSuccess:(result,input)=>{setHistoryBatch({key:input.key,jobs:result.jobs});setActiveJob(undefined);},
  });
  const retryWindow=useMutation({
    mutationFn:({job}:{key:string;job:MarketDataJob})=>api.requestMarketRefresh({source,instrumentId:job.instrumentId,kind:"daily_candle",from:job.from,to:job.to}) as Promise<{job?:MarketDataJob}>,
    onSuccess:(result,input)=>{if(result.job)setHistoryBatch(previous=>previous?.key===input.key?{...previous,jobs:previous.jobs.map(j=>j.id===input.job.id?result.job!:j)}:previous);},
  });
  const mutateHistory=loadHistory.mutate,mutateRefresh=refresh.mutate;
  const requestSelected=useCallback(()=>{if(!selected)return;if(longHistory)mutateHistory({key:historyKey,instrumentId:selected.instrumentId,fromDate:from,toDate:to});else mutateRefresh({kind,instrumentId:selected.instrumentId,...(kind==="daily_candle"?{from:stamp(from),to:requestEnd(to)}:{})});},[selected,longHistory,historyKey,from,to,kind,mutateHistory,mutateRefresh]);
  useEffect(()=>{if(lazy&&!readOnly&&selected&&rangeValid&&history.isSuccess&&(longHistory||!history.data?.observations.length)&&!requested.current.has(historyKey)){requested.current.add(historyKey);requestSelected();}},[lazy,readOnly,selected,rangeValid,history.isSuccess,history.data,historyKey,longHistory,requestSelected]);
  const busy = refresh.isPending || loadHistory.isPending || retryWindow.isPending || ["pending", "running"].includes(currentJob?.status || "") || batchRecords.some(j=>["pending","running"].includes(j.status));
  const error = catalog.error || history.error || job.error || refresh.error || loadHistory.error || retryWindow.error || (currentBatch&&batchJobs.error);
  const records = history.data?.observations || [];
  const seriesID=(o:MarketObservation)=>JSON.stringify([o.currency,o.priceUnit,o.sourceKind,o.adjustment]);
  const seriesKeys=Array.from(new Set(records.map(seriesID))),currentSeries=seriesKeys.includes(previewSeries)?previewSeries:seriesKeys[0]||"";
  const preview=records.filter(o=>seriesID(o)===currentSeries),byDate=new Map<string,MarketObservation>();
  for(const o of preview){if(!o.observedAt)continue;const date=day(new Date(Number(o.observedAt.seconds)*1000)),prior=byDate.get(date);if(!prior?.observedAt||prior.observedAt.seconds<o.observedAt.seconds)byDate.set(date,o);}
  const chartPoints:{date:string;state:string;knownValue?:{decimal:string}}[]=[];
  if(rangeValid&&kind==="daily_candle")for(let date=from;date<=to;date=day(new Date(Date.parse(date+"T00:00:00Z")+86400000))){const o=byDate.get(date),value=o?.complete?decimalFraction(o.price):undefined;chartPoints.push({date,state:value?"ready":"incomplete",knownValue:value?{decimal:positionDecimal(value)}:undefined});}
  const geometry=navGeometry(chartPoints);

  return <>
    <section className={panel}>
      <form className="grid items-end gap-3 sm:grid-cols-[1fr_12rem_auto]" onSubmit={e => { e.preventDefault(); setQuery(draft.trim()); setOffset(0); setSelected(undefined); if(!readOnly)refresh.mutate({ kind: "catalog_search", searchQuery: draft.trim(), instrumentKind: assetKind }); }}>
        <label className="text-sm">{t("query")}<input className={field} value={draft} onChange={e => setDraft(e.target.value)} required maxLength={100} /></label>
        <label className="text-sm">{t("searchKind")}<select className={field} value={assetKind} onChange={e => setAssetKind(e.target.value)}>{["share", "bond", "etf", "currency"].map(k => <option key={k} value={k}>{t(`kinds.${k}`)}</option>)}</select></label>
        <button className={button} disabled={busy || !draft.trim() || new TextEncoder().encode(draft.trim()).length > 100}>{t("search")}</button>
      </form>
      <p className="text-sm text-gray-500">{t("cached")}</p>
      {error && <p role="alert" className="text-red-600">{(error as { code?: number }).code === 8 ? t("quota") : t("requestError")}</p>}
      {currentJob && <div role="status" className="rounded-lg bg-blue-50 p-3 text-sm text-blue-950 dark:bg-blue-950 dark:text-blue-100">
        {t("job")} · {t.has(`statuses.${currentJob.status}`) ? t(`statuses.${currentJob.status}`) : currentJob.status}
        {currentJob.searchQuery && <span> · {currentJob.searchQuery}</span>}
        {currentJob.coverageState === "catalog_truncated" && <p className="mt-1">{t("truncated")}</p>}
        {currentJob.coverageState === "empty_unexplained" && <p className="mt-1">{t("emptyProvider")}</p>}
        {currentJob.status === "pending" && currentJob.attempts > 0 && <p className="mt-1">{t("retryAt")} {dateLabel(currentJob.availableAt)}</p>}
        {currentJob.status === "error" && <p className="mt-1">{t("jobError")}</p>}
      </div>}
      {catalog.isPending ? <p>{t("loading")}</p> : !catalog.data?.instruments.length ? <p>{t("noInstruments")}</p> : <div className="grid gap-3 md:grid-cols-2">{catalog.data.instruments.map(i => <div key={i.instrumentId} className="min-w-0"><button type="button" aria-pressed={selected?.instrumentId === i.instrumentId} onClick={() => setSelected(i)} className={`min-w-0 rounded-lg border p-3 text-left ${selected?.instrumentId === i.instrumentId ? "border-blue-500 bg-blue-50 dark:bg-blue-950" : "border-gray-200 dark:border-gray-700"}`}>
        <span className="block font-semibold">{i.ticker} · {i.classCode || t("venueUnknown")}</span><span className="block break-words text-sm">{i.name}</span>
        <span className="mt-1 block text-xs text-gray-500">{i.isin || t("isinUnknown")} · {t.has(`kinds.${i.kind}`) ? t(`kinds.${i.kind}`) : i.kind}</span>
      </button><Link className="mt-1 block text-sm text-blue-600" href={`/investments/instruments/${encodeURIComponent(source.provider+":"+i.instrumentId)}?${new URLSearchParams({provider:source.provider,scope:source.accessScope,listing:i.instrumentId})}`}>{card("open")}</Link></div>)}</div>}
      <div className="flex justify-between"><button className={button} disabled={!offset || catalog.isFetching} onClick={() => setOffset(x => Math.max(0, x - 25))}>{t("previous")}</button><button className={button} disabled={catalog.data?.instruments.length !== 25 || catalog.isFetching} onClick={() => setOffset(x => x + 25)}>{t("next")}</button></div>
    </section>
    {selected && <section className={panel}>
      <header><h2 className="text-xl font-semibold">{selected.name}</h2><p className="mt-1 text-sm text-gray-500">{selected.ticker} · {selected.classCode} · {selected.isin || "—"}</p></header>
      <p className="text-sm text-amber-800 dark:text-amber-300">{t("unconfirmed")}</p>
      <div className="grid items-end gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <label className="text-sm">{t("dataKind")}<select className={field} value={kind} onChange={e => setKind(e.target.value)}><option value="daily_candle">{t("daily")}</option><option value="last_price">{t("quotes")}</option></select></label>
        <label className="text-sm">{t("from")}<input type="date" className={field} value={from} onChange={e => setFrom(e.target.value)} max={to} /></label>
        <label className="text-sm">{t("to")}<input type="date" className={field} value={to} onChange={e => setTo(e.target.value)} min={from} max={day(new Date())} /></label>
        <button className={button} disabled={readOnly || busy || !rangeValid} onClick={requestSelected}>{t("refresh")}</button>
      </div>
      {currentBatch&&<div role="status" className="space-y-2 rounded-lg bg-blue-50 p-3 text-sm text-blue-950 dark:bg-blue-950 dark:text-blue-100">
        <p>{t("historyBatch",{count:batchRecords.length,ready:batchRecords.filter(j=>j.status==="ready").length,errors:batchRecords.filter(j=>j.status==="error").length})}</p>
        <p>{t("historyRequestsOnly")}</p>
        <ul>{batchRecords.map(j=><li key={j.id}>{dateLabel(j.from)} → {dateLabel(j.to)} · {t.has(`statuses.${j.status}`)?t(`statuses.${j.status}`):t("unknown")} · {j.coverageState==="empty_unexplained"?t("noPrices"):j.errorCode?t("statuses.error"):j.coverageState==="observed"?t("historyObserved"):"—"}{(j.status==="error"||j.coverageState==="empty_unexplained")&&<button className="ml-2 text-blue-700 underline disabled:opacity-50 dark:text-blue-300" disabled={readOnly||busy} onClick={()=>retryWindow.mutate({key:historyKey,job:j})}>{t("retry")}</button>}</li>)}</ul>
      </div>}
      {!rangeValid && <p role="alert" className="text-red-600">{t("rangeError")}</p>}
      <p className="text-sm text-gray-500">{t("coverage")}</p>
      {history.isFetching && <p role="status">{t("loading")}</p>}
      {!history.isFetching && !records.length && <p>{t("noPrices")}</p>}
      {records.length>0&&kind==="daily_candle"&&<figure className="space-y-3"><label className="block text-sm">{card("sourceSeries")}<select aria-label={card("sourceSeries")} className={field} value={currentSeries} onChange={e=>setPreviewSeries(e.target.value)}>{seriesKeys.map(key=><option key={key} value={key}>{(JSON.parse(key) as string[]).join(" · ")}</option>)}</select></label><p className="text-sm text-gray-500">{card("rawPolicy")}</p>{geometry.dots.length>0?<InvestmentChart role="img" aria-label={card("rawChart")} viewBox="0 0 800 270" className="w-full"><title>{card("rawChart")}</title>{geometry.paths.map((d,i)=><path key={i} d={d} stroke="#2563eb" strokeWidth="2" fill="none"/>)}{geometry.dots.map(p=><circle key={p.index} cx={p.x} cy={p.y} r="3" fill="#2563eb"><title>{chartPoints[p.index].date}: {chartPoints[p.index].knownValue?.decimal} · {currentSeries}</title></circle>)}</InvestmentChart>:<p>{card("noPrices")}</p>}</figure>}
      {!!records.length && <div className="space-y-3 sm:hidden">{records.map(o => <article key={o.id} className="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
        <p className="text-xs text-gray-500">{dateLabel(o.observedAt)}</p>
        <p className="break-all font-mono text-lg font-semibold tabular-nums">{exact(o.price)} <span className="text-sm font-normal">{o.currency || ""}</span></p>
        <dl className="grid grid-cols-2 gap-2 text-sm"><dt className="text-gray-500">{t("units")}</dt><dd>{t.has(`priceUnits.${o.priceUnit}`) ? t(`priceUnits.${o.priceUnit}`) : t("unknown")}</dd><dt className="text-gray-500">{t("quality")}</dt><dd>{o.complete ? t("complete") : t("incomplete")}</dd></dl>
        <p className="text-xs text-gray-500">{o.sourceKind === "exchange" ? t("exchange") : t("dealer")} · {t("revision", { value: String(o.revision) })}</p>
      </article>)}</div>}
      {!!records.length && <div className="hidden overflow-x-auto sm:block"><table className="w-full text-left text-sm"><thead><tr>{["observed", "price", "units", "currency", "quality"].map(k => <th key={k} className="whitespace-nowrap px-3 py-2">{t(k)}</th>)}</tr></thead><tbody>{records.map(o => <tr key={o.id} className="border-t border-gray-200 dark:border-gray-700">
        <td className="whitespace-nowrap px-3 py-3">{dateLabel(o.observedAt)}</td><td className="whitespace-nowrap px-3 py-3 font-mono tabular-nums">{exact(o.price)}</td>
        <td className="px-3 py-3">{t.has(`priceUnits.${o.priceUnit}`) ? t(`priceUnits.${o.priceUnit}`) : t("unknown")}</td><td className="px-3 py-3">{o.currency || "—"}</td>
        <td className="px-3 py-3"><span className="block">{o.complete ? t("complete") : t("incomplete")}</span><span className="block text-xs text-gray-500">{o.sourceKind === "exchange" ? t("exchange") : t("dealer")} · {t("revision", { value: String(o.revision) })}</span></td>
      </tr>)}</tbody></table></div>}
      {selected.kind==="bond"&&<InvestmentBondFacts key={`${selected.instrumentId}:${from}:${to}`} tenantId={tenantId} source={source} instrumentId={selected.instrumentId} from={from} to={to} rangeValid={rangeValid}/>}
      <InvestmentMarketMappingEditor key={selected.instrumentId} tenantId={tenantId} candidate={selected} source={source} />
    </section>}
  </>;
}
