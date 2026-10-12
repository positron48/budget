"use client";
import {useState} from "react";
import Link from "next/link";
import {useParams,useSearchParams,useRouter,usePathname} from "next/navigation";
import {useQuery,keepPreviousData} from "@tanstack/react-query";
import {useLocale,useTranslations} from "next-intl";
import {Protected} from "@/components";
import {useClients} from "@/app/providers";
import {operationTime} from "@/lib/investment-operation-time";
import {validInvestmentDate} from "@/lib/investment-period";
import {useAssetTenant} from "@/lib/assets";
import {AccountPosition} from "@/components/InvestmentPositions";
import InvestmentLots from "@/components/InvestmentLots";
import InvestmentVariant from "@/components/InvestmentVariant";
import InvestmentInstrumentPrices from "@/components/InvestmentInstrumentPrices";
import InstrumentListing from "@/components/InvestmentInstrumentListing";
import type {InvestmentNAVSeries,GetInvestmentInstrumentResponse,GetInvestmentNAVSeriesResponse,ListInvestmentNAVSeriesResponse} from "@/proto/budget/v1/investment_nav_pb";
const field="rounded-lg border border-gray-300 bg-transparent px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";
function Holding({tenantId,instrumentKey,series,date,groups}:{tenantId:string;instrumentKey:string;series:InvestmentNAVSeries;date:string;groups:string[]}){
 const t=useTranslations("investmentInstrument"),payouts=useTranslations("investmentPayouts"),locale=useLocale(),{investmentNAV:api}=useClients(),[cursor,setCursor]=useState(""),[previous,setPrevious]=useState<string[]>([]);
 const query=useQuery<GetInvestmentInstrumentResponse>({queryKey:["investmentNAV",tenantId,"instrument",series.key,instrumentKey,date,groups,cursor],placeholderData:keepPreviousData,queryFn:()=>api.getInvestmentInstrument({seriesKey:series.key,instrumentKey,date,groupIds:groups,cursor})});
 const v=query.data,meta=v?.metadata,rows=v?.holdings?.positions||[],bond=meta?.kind==="bond"||rows.some(r=>r.instrumentType==="bond");
 return <>{query.isFetching&&<p role="status">{t("loading")}</p>}{query.error&&<p role="alert">{(query.error as {code?:number}).code===10?t("changed"):t("error")} <button className={field} onClick={()=>{setCursor("");setPrevious([]);void query.refetch();}}>{t("retry")}</button></p>}{v&&<>
 <header className="space-y-2"><h1 className="break-words text-3xl font-semibold">{meta?.name||instrumentKey}</h1><p className="break-all text-gray-500">{meta?.ticker||"—"} · {meta?.isin||"—"} · {meta?.classCode||"—"} · {meta?.kind||"—"} · {meta?.currency||"—"}</p><p className="text-sm">{t("asOf",{date})} · {t("metadataPolicy")}</p></header>
 <section className="space-y-4"><h2 className="text-xl font-semibold">{t("holdings")}</h2><p className="text-sm text-gray-500">{t("holdingsPolicy")}</p>{!rows.length&&<p>{t("unowned")}</p>}<div className="grid gap-3 md:grid-cols-2">{rows.map(row=><AccountPosition key={row.ledgerId} row={row} currency={series.currency}/>)}</div>{bond&&<p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-100">{t("bondUnknown")}</p>}</section>
 <InvestmentLots key={JSON.stringify([series.key,instrumentKey,date,groups])} tenantId={tenantId} seriesKey={series.key} instrumentKey={instrumentKey} date={date} groups={groups}/>
 <InvestmentInstrumentPrices key={`${series.key}:${instrumentKey}:${date}`} tenantId={tenantId} seriesKey={series.key} instrumentKey={instrumentKey} fromDate={series.fromDate} toDate={series.toDate} bond={bond} groups={groups}/>
 <section className="space-y-3"><h2 className="text-xl font-semibold">{t("operations")}</h2><p className="text-sm text-gray-500">{t("operationsPolicy")}</p>{!v.operations.length&&<p>{t("noOperations")}</p>}<div className="space-y-3">{v.operations.map(op=><article key={op.id} className="min-w-0 space-y-2 rounded-lg border border-gray-200 p-3 text-sm dark:border-gray-700"><div className="flex flex-wrap justify-between gap-2"><span>{operationTime(op,false,locale)}{!op.timing?.occurredDate&&" MSK"}</span><strong>{t.has(`kinds.${op.kind}`)?t(`kinds.${op.kind}`):op.kind} · {op.status}</strong></div><p className="break-all">{rows.find(r=>r.ledgerId===op.ledgerId)?.ledgerName||op.ledgerId}</p><dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">{[["quantity",op.quantity],[op.taxReviewRequired?"netAmount":"amount",op.kind==="opening_position"&&!op.openingCostKnown?"—":`${op.amount} ${op.currency}`],["fee",`${op.fee} ${op.currency}`],["tax",op.taxReviewRequired?"—":`${op.tax} ${op.currency}`],["accrued",op.kind==="opening_position"&&!op.openingAccruedKnown?"—":`${op.accrued} ${op.currency}`],["settlement",operationTime(op,true,locale)]].map(([k,value])=><div key={k}><dt className="text-gray-500">{t(k)}</dt><dd className="break-all font-mono">{value||"—"}</dd></div>)}</dl>{op.taxReviewRequired&&<p className="text-sm text-amber-700 dark:text-amber-300">{payouts("taxUnknown")}</p>}{op.note&&<p className="break-words">{op.note}</p>}</article>)}</div><div className="flex gap-3"><button className={field} disabled={!previous.length||query.isFetching} onClick={()=>{setCursor(previous.at(-1)||"");setPrevious(v=>v.slice(0,-1));}}>{t("previous")}</button><button className={field} disabled={!v.nextCursor||query.isFetching} onClick={()=>{setPrevious(v=>[...v,cursor]);setCursor(v.nextCursor);}}>{t("next")}</button></div></section>
 </>}</>;
}
function Instrument({tenantId,instrumentKey}:{tenantId:string;instrumentKey:string}){
 const t=useTranslations("investmentInstrument"),{investmentNAV:api}=useClients(),params=useSearchParams(),router=useRouter(),pathname=usePathname();
 const requested=params.get("series")||"",groups=Array.from(new Set(params.getAll("group"))).sort();
 const [after,setAfter]=useState("");
 const listing=useQuery<ListInvestmentNAVSeriesResponse>({queryKey:["investmentNAV",tenantId,"series",after],queryFn:()=>api.listInvestmentNAVSeries({afterKey:after})});
 const explicit=useQuery<GetInvestmentNAVSeriesResponse>({queryKey:["investmentNAV",tenantId,"series",requested],enabled:!!requested,queryFn:()=>api.getInvestmentNAVSeries({seriesKey:requested})});
 const series=requested?explicit.data?.series:listing.data?.series[0],date=params.get("to")??params.get("date")??series?.toDate??"",valid=!!series&&validInvestmentDate(date)&&date>=series.fromDate&&date<=series.toDate;
 const update=(key:string,value:string)=>{const q=new URLSearchParams(params.toString());q.set(key,value);if(key==="series")q.delete("date");if(key==="date"){q.set("to",value);q.set("period","custom");}router.replace(`${pathname}?${q}`,{scroll:false});};
 const back=new URLSearchParams(params.toString());back.delete("date");back.delete("provider");back.delete("scope");back.delete("listing");
 return <main className="mx-auto max-w-6xl space-y-6 px-4 py-8"><Link className="text-blue-600" href={`/investments/value?${back}`}>{t("back")}</Link><div className="flex flex-wrap items-end gap-3"><label className="min-w-0 max-w-full">{t("series")}<select className={field+" block max-w-full"} value={series?.key||""} onChange={e=>update("series",e.target.value)}>{series&&!listing.data?.series.some(s=>s.key===series.key)&&<option value={series.key}>{series.currency} · {series.fxPolicyKey}</option>}{listing.data?.series.map(s=><option key={s.key} value={s.key}>{s.currency} · {s.fxPolicyKey} · {s.ledgerIds.length} · {s.fromDate}—{s.toDate}</option>)}</select></label>{listing.data?.nextAfterKey&&<button className={field} onClick={()=>setAfter(listing.data!.nextAfterKey)}>{t("moreSeries")}</button>}{series&&<label>{t("date")}<input type="date" className={field+" block"} min={series.fromDate} max={series.toDate} value={date} onChange={e=>update("date",e.target.value)}/></label>}</div>
 {(listing.isFetching||explicit.isFetching)&&<p role="status">{t("loading")}</p>}{(listing.error||explicit.error)&&<p role="alert">{t("error")}</p>}{series&&!valid&&<p role="alert">{t("rangeError")}</p>}{!series&&!listing.isPending&&!requested&&<><h1 className="break-all text-3xl font-semibold">{instrumentKey}</h1><p>{t("noSeries")}</p></>}{valid&&series&&<InvestmentVariant key={`variant:${series.key}`} tenantId={tenantId} series={series}/>}
 {valid&&series&&<Holding key={JSON.stringify([tenantId,instrumentKey,series.key,date,groups])} tenantId={tenantId} instrumentKey={instrumentKey} series={series} date={date} groups={groups}/>}
 <InstrumentListing tenantId={tenantId} instrumentKey={instrumentKey}/>
 </main>;
}
export default function InstrumentPage(){const tenantId=useAssetTenant(),params=useParams<{id:string}>();return <Protected>{tenantId&&params.id&&<Instrument key={`${tenantId}:${params.id}`} tenantId={tenantId} instrumentKey={params.id}/>}</Protected>;}
