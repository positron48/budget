"use client";
import {useState} from "react";
import {useSearchParams} from "next/navigation";
import {useTranslations} from "next-intl";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import SourceMarket from "@/components/InvestmentSourceMarket";
import type {ListMarketSourcesResponse,GetInvestmentMarketMappingResponse} from "@/proto/budget/v1/market_data_pb";
export default function InstrumentListing({tenantId,instrumentKey}:{tenantId:string;instrumentKey:string}){
 const t=useTranslations("investmentInstrument"),{marketData:api}=useClients(),params=useSearchParams(),[chosen,setChosen]=useState<string>();
 const sources=useQuery<ListMarketSourcesResponse>({queryKey:["market",tenantId,"sources"],queryFn:()=>api.listMarketSources({})});
 const mapping=useQuery<GetInvestmentMarketMappingResponse>({queryKey:["marketMapping",tenantId,instrumentKey],queryFn:async()=>{try{return await api.getInvestmentMarketMapping({instrumentKey})}catch(e){if((e as {code?:number}).code===5)return {} as GetInvestmentMarketMappingResponse;throw e;}}});
 const now=BigInt(Math.floor(Date.now()/1000)),confirmed=mapping.data?.mapping?.periods.find(p=>p.from&&p.from.seconds<=now&&(!p.to||p.to.seconds>now));
 const requested=params.get("provider")&&params.get("scope")?JSON.stringify([params.get("provider"),params.get("scope")]):undefined;
 const sourceKey=chosen??requested??(confirmed?.source?JSON.stringify([confirmed.source.provider,confirmed.source.accessScope]):"");
 const current=sources.data?.sources.find(s=>JSON.stringify([s.source?.provider,s.source?.accessScope])===sourceKey);
 const listing=chosen===undefined&&requested===sourceKey?params.get("listing")||"":confirmed?.source&&JSON.stringify([confirmed.source.provider,confirmed.source.accessScope])===sourceKey?confirmed.instrumentId:"";
 return <section className="space-y-4 border-t border-gray-200 pt-6 dark:border-gray-800"><h2 className="text-xl font-semibold">{t("listing")}</h2><p className="text-sm text-gray-500">{t("listingPolicy")}</p>{(sources.error||mapping.error)&&<p role="alert">{t("error")}</p>}<label className="block">{t("source")}<select className="mt-1 block max-w-full rounded-lg border bg-transparent p-2" value={sourceKey} onChange={e=>setChosen(e.target.value)}><option value="">{t("chooseSource")}</option>{sources.data?.sources.map(s=><option key={JSON.stringify([s.source?.provider,s.source?.accessScope])} value={JSON.stringify([s.source?.provider,s.source?.accessScope])}>{s.title}</option>)}</select></label>{current?.source&&<SourceMarket key={`${sourceKey}:${listing}`} tenantId={tenantId} source={current.source} initialListing={listing} lazy/>}</section>;
}
