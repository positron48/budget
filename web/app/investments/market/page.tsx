"use client";
import SourceMarket from "@/components/InvestmentSourceMarket";

import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { Protected } from "@/components";
import { useAssetTenant } from "@/lib/assets";
import type { MarketSource, MarketSourceKey } from "@/proto/budget/v1/market_data_pb";

const field = "mt-1 w-full rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-900";
const panel = "space-y-4 rounded-xl border border-gray-200 p-4 dark:border-gray-800";
const sourceID = (s?: MarketSourceKey) => JSON.stringify([s?.provider, s?.accessScope]);


function Market({ tenantId }: { tenantId: string }) {
  const t = useTranslations("market");
  const { marketData: api } = useClients();
  const [selected, setSelected] = useState("");
  const sources = useQuery<{ sources: MarketSource[] }>({ queryKey: ["market", tenantId, "sources"], queryFn: () => api.listMarketSources({}) });
  const current = sources.data?.sources.find(s => sourceID(s.source) === selected) || sources.data?.sources[0];
  return <main className="mx-auto max-w-6xl space-y-6 px-4 py-8">
    <Link href="/investments/journal" className="text-blue-600">{t("journal")}</Link>
    <header><h1 className="text-3xl font-semibold">{t("title")}</h1><p className="mt-2 text-gray-600 dark:text-gray-400">{t("intro")}</p></header>
    {sources.isPending && <p role="status">{t("loading")}</p>}
    {sources.error && <p role="alert" className="text-red-600">{t("requestError")} <button className="underline" onClick={() => sources.refetch()}>{t("retry")}</button></p>}
    {sources.data?.sources.length === 0 && <p className={panel}>{t("noSources")}</p>}
    {current?.source && <><label className="block max-w-md text-sm">{t("source")}<select className={field} value={sourceID(current.source)} onChange={e => setSelected(e.target.value)}>{sources.data?.sources.map(s => <option key={sourceID(s.source)} value={sourceID(s.source)}>{s.title}</option>)}</select></label><SourceMarket key={sourceID(current.source)} tenantId={tenantId} source={current.source} /></>}
  </main>;
}
export default function MarketPage() { const tenantId = useAssetTenant(); return <Protected>{tenantId && <Market key={tenantId} tenantId={tenantId} />}</Protected>; }
