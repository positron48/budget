"use client";
import {useState} from "react";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import {authStore} from "@/lib/auth/store";
import {decodeInvestmentSeriesCSV} from "@/lib/investment-series-csv";
export default function InvestmentSeriesExport({tenantId,seriesKey,from,to,valid}:{tenantId:string;seriesKey:string;from:string;to:string;valid:boolean}){
 const t=useTranslations("investmentSeriesExport"),{investmentNAV:api}=useClients();
 const [pending,setPending]=useState(false),[error,setError]=useState(""),[rows,setRows]=useState<number>();
 const download=async()=>{
  setPending(true);setError("");setRows(undefined);
  try{
   const result=await api.exportInvestmentSeries({seriesKey,fromDate:from,toDate:to});
   const raw=await decodeInvestmentSeriesCSV(result);
   if(authStore.getTenant()!==tenantId)return;
   const url=URL.createObjectURL(new Blob([raw.buffer],{type:"text/csv;charset=utf-8"}));
   const anchor=document.createElement("a");anchor.href=url;anchor.download=result.filename;document.body.appendChild(anchor);anchor.click();anchor.remove();setTimeout(()=>URL.revokeObjectURL(url),1000);setRows(result.rowCount);
  }catch(e){if(authStore.getTenant()===tenantId)setError((e as {code?:number}).code===8?t("limit"):t("error"));}
  finally{setPending(false);}
 };
 return <div className="space-y-1"><button className="rounded-lg border border-gray-300 px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700" disabled={!valid||pending} onClick={()=>void download()}>{pending?t("loading"):t("download")}</button><p className="text-xs text-gray-500">{t("method")}</p>{error&&<p role="alert" className="text-sm text-red-600">{error}</p>}{rows!==undefined&&<p role="status" className="text-sm">{t("ready",{count:rows})}</p>}</div>;
}
