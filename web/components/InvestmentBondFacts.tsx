"use client";
import {useEffect,useRef,useState} from "react";
import {useQuery,useMutation,useQueryClient} from "@tanstack/react-query";
import {useLocale,useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import type {GetMarketBondFactsResponse,MarketSourceKey,MarketBondAmount,MarketDataJob} from "@/proto/budget/v1/market_data_pb";
const field="rounded-lg border border-gray-300 bg-transparent px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";
const amount=(a?:MarketBondAmount)=>a?`${a.amount} ${a.currency}`:"—";
const stamp=(date:string,end=false)=>({seconds:BigInt(Math.floor(Date.parse(date+"T00:00:00Z")/1000))+(end?86399n:0n),nanos:0});
export default function InvestmentBondFacts({tenantId,source,instrumentId,from,to,rangeValid}:{tenantId:string;source:MarketSourceKey;instrumentId:string;from:string;to:string;rangeValid:boolean}){
 const t=useTranslations("investmentBond"),locale=useLocale(),readOnly=useInvestmentReadOnly(),{marketData:api}=useClients(),qc=useQueryClient();
 const key=["market",tenantId,source.provider,source.accessScope,"bond",instrumentId,from,to];
 const [jobId,setJobId]=useState(""),[tab,setTab]=useState<"coupons"|"events"|"accrued">("coupons"),[page,setPage]=useState(0),completed=useRef("");
 const query=useQuery<GetMarketBondFactsResponse>({queryKey:key,enabled:rangeValid,queryFn:()=>api.getMarketBondFacts({source,instrumentId,from:stamp(from),to:stamp(to,true)})});
 const refresh=useMutation<{job?:MarketDataJob}>({mutationFn:()=>api.requestMarketRefresh({source,instrumentId,kind:"bond_facts",from:stamp(from),to:stamp(to,true)}),onSuccess:result=>{setJobId(result.job?.id||"");if(result.job?.status==="ready")void qc.invalidateQueries({queryKey:key});}});
 const job=useQuery<{job?:MarketDataJob}>({queryKey:[...key,"job",jobId],enabled:!!jobId,queryFn:()=>api.getMarketDataJob({id:jobId}),refetchInterval:s=>["pending","running"].includes(s.state.data?.job?.status||"")?1500:false});
 useEffect(()=>{if(job.data?.job?.status==="ready"&&completed.current!==jobId){completed.current=jobId;void qc.invalidateQueries({queryKey:["market",tenantId,source.provider,source.accessScope,"bond",instrumentId]});}},[jobId,job.data?.job?.status,qc,tenantId,source.provider,source.accessScope,instrumentId]);
 const date=(s?:{seconds:bigint})=>s?new Date(Number(s.seconds)*1000).toLocaleDateString(locale,{timeZone:"UTC"}):"—";
 const timeLabel=(s?:{seconds:bigint})=>s?new Date(Number(s.seconds)*1000).toLocaleString(locale,{timeZone:"UTC"}):"—";
 const flag=(v?:boolean)=>v===undefined?"—":t(v?"yes":"no");
 const data=query.error?undefined:query.data;
 const mismatched=!!data?.snapshotId&&(data.instrumentId!==instrumentId||data.source?.provider!==source.provider||data.source?.accessScope!==source.accessScope);
 const facts=data?.snapshotId&&!mismatched?data:undefined,terms=facts?.terms;
 const busy=refresh.isPending||["pending","running"].includes(job.data?.job?.status||"");
 const rows=facts?.[tab]||[],visible=rows.slice(page*20,page*20+20);
 return <section className="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-gray-800" aria-label={t("title")}>
 <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-xl font-semibold">{t("title")}</h2><button className={field} disabled={readOnly||busy||!rangeValid} onClick={()=>refresh.mutate()}>{t("refresh")}</button></div>
 <p className="text-sm text-gray-500">{t("policy")}</p>
 {(query.error||refresh.error||job.error||mismatched)&&<p role="alert">{t("error")}</p>}{query.isFetching&&<p role="status">{t("loading")}</p>}{busy&&<p role="status">{t("queued")}</p>}{job.data?.job?.status==="error"&&<p role="alert">{t("jobError")}</p>}
 {!facts&&!query.isPending&&!query.error&&<p>{t("missing")}</p>}
 {facts&&<><p className="text-sm">{t("received")} {timeLabel(facts.asOf)} UTC · {t("range")} {date(facts.from)}—{date(facts.to)} UTC</p>
 <dl className="grid grid-cols-2 gap-3 text-sm md:grid-cols-3">{terms&&[["nominal",amount(terms.nominal)],["initialNominal",amount(terms.initialNominal)],["currentAccrued",amount(terms.currentAccrued)],["maturity",date(terms.maturity)],["callDate",date(terms.callDate)],["placement",date(terms.placement)],["floating",flag(terms.floating)],["perpetual",flag(terms.perpetual)],["amortizing",flag(terms.amortizing)],["frequency",terms.couponFrequency===undefined?"—":String(terms.couponFrequency)]].map(([k,v])=><div key={k}><dt className="text-gray-500">{t(k)}</dt><dd className="break-all font-mono">{v}</dd></div>)}</dl>
 <div className="flex flex-wrap gap-2">{(["coupons","events","accrued"] as const).map(k=><button key={k} className={field} aria-pressed={tab===k} onClick={()=>{setTab(k);setPage(0);}}>{t(k)} ({facts[k].length})</button>)}</div>
 {tab==="accrued"&&<p className="text-sm text-amber-800 dark:text-amber-300">{t("accruedPolicy")}</p>}
 {!rows.length&&<p>{t("empty")}</p>}
 <div className="grid gap-3 md:grid-cols-2">{visible.map((row,i)=><article key={i} className="min-w-0 space-y-2 rounded-lg border border-gray-200 p-3 text-sm dark:border-gray-800"><p>{date(row.date)} UTC</p>{"kind" in row&&<p>{t.has(`kinds.${row.kind}`)?t(`kinds.${row.kind}`):row.kind} · {String(row.number)}</p>}
 {tab!=="accrued"&&<p className="break-all font-mono">{t("pay")}: {amount("pay" in row?row.pay:undefined)}</p>}
 {tab!=="accrued"&&<p>{t("fixDate")}: {date("fixDate" in row?row.fixDate:undefined)}</p>}
 {tab!=="accrued"&&<p>{t("couponPeriod")}: {date("start" in row?row.start:undefined)}—{date("end" in row?row.end:undefined)}</p>}
 {"note" in row&&<><p>{t("payDate")}: {date(row.payDate)}</p><p>{t("actualPayDate")}: {date(row.actualPayDate)}</p>{row.annualRate!==undefined&&<p>{t("annualRate")}: {row.annualRate}%</p>}{row.value!==undefined&&<p>{t("rawValue")}: {row.value}</p>}{row.note&&<p className="break-words">{row.note}</p>}{(row.execution||row.operationType||row.convertTo)&&<details className="break-all"><summary>{t("eventDetails")}</summary>{row.execution||"—"} · {row.operationType||"—"} · {row.convertTo||"—"}</details>}</>}
 {tab==="accrued"&&<> <p>{t("value")}: {"value" in row?row.value??"—":"—"}</p><p>{t("percent")}: {"percent" in row?row.percent??"—":"—"}</p><p>{t("pointNominal")}: {"nominal" in row?row.nominal??"—":"—"}</p></>}
 </article>)}</div>
 <div className="flex gap-3"><button className={field} disabled={!page} onClick={()=>setPage(p=>p-1)}>{t("previous")}</button><button className={field} disabled={(page+1)*20>=rows.length} onClick={()=>setPage(p=>p+1)}>{t("next")}</button></div>
 <details className="break-all text-xs text-gray-500"><summary>{t("source")}</summary>{source.provider} / {source.accessScope} · {facts.instrumentId}<br/>{facts.snapshotId}<br/>{facts.payloadHash}<br/>{t("recorded")}: {timeLabel(facts.recordedAt)} UTC</details>
 </>}
 </section>;
}
