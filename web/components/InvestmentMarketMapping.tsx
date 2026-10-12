"use client";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import type { InvestmentMarketMapping, InvestmentMarketPeriod, MarketInstrument, MarketSourceKey } from "@/proto/budget/v1/market_data_pb";
import type { TenantMembership } from "@/proto/budget/v1/tenant_pb";

const field = "mt-1 w-full min-w-0 rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-900";
const button = "rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50";
type Stamp = { seconds: bigint; nanos: number };
// Retain PostgreSQL microseconds when editing a previously saved boundary.
function formatStamp(s?: Stamp) {
  if (!s) return "";
  return new Date(Number(s.seconds) * 1000).toISOString().slice(0, 19) + (s.nanos ? "." + String(s.nanos).padStart(9, "0").replace(/0+$/, "") : "") + "Z";
}
function parseStamp(s: string): Stamp | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,6})?Z$/.test(s)) return undefined;
  const base = s.slice(0, 19) + "Z";
  const ms = Date.parse(base);
  if (!Number.isFinite(ms) || new Date(ms).toISOString().slice(0, 19) !== s.slice(0, 19)) return undefined;
  return { seconds: BigInt(ms / 1000), nanos: Number((s.match(/\.(\d+)Z$/)?.[1] || "").padEnd(9, "0")) };
}
type Draft = Omit<InvestmentMarketPeriod, "$typeName" | "from" | "to"> & { fromText: string; toText: string };
const draftOf = (p: InvestmentMarketPeriod): Draft => {
  // A form draft is a protobuf initializer, not a tagged message instance.
  // Keeping $typeName would skip rebuilding its edited Timestamp children.
  const { $typeName: _typeName, from, to, ...values } = p;
  return { ...values, fromText: formatStamp(from), toText: formatStamp(to) };
};

function Editor({ tenantId, instrumentKey, mapping, candidate, source, canWrite, reload }: { tenantId: string; instrumentKey: string; mapping?: InvestmentMarketMapping; candidate: MarketInstrument; source: MarketSourceKey; canWrite: boolean; reload: () => void }) {
  const t = useTranslations("marketMapping");
  const { marketData: api } = useClients();
  const qc = useQueryClient();
  const [rows, setRows] = useState<Draft[]>(() => (mapping?.periods || []).map(draftOf));
  const [reason, setReason] = useState("");
  const [invalid, setInvalid] = useState(false);
  const [preview, setPreview] = useState<{ requestKey: string; periods: Record<string, unknown>[]; reason: string }>();
  const save = useMutation({
    mutationFn: () => api.saveInvestmentMarketMapping({ instrumentKey, expectedVersion: mapping?.version || 0n, ...preview }),
    onSuccess: (result: { mapping?: InvestmentMarketMapping }) => {
      qc.setQueryData(["investment-mapping", tenantId, instrumentKey], result);
      setPreview(undefined);
    },
  });
  const update = (index: number, key: keyof Draft, value: string) => { setRows(old => old.map((r, i) => i === index ? { ...r, [key]: value } : r)); setInvalid(false); };
  function prepare() {
    let previousEnd: bigint | undefined;
    const periods = [];
    for (const [i, r] of rows.entries()) {
      const from = parseStamp(r.fromText), to = r.toText ? parseStamp(r.toText) : undefined;
      const ticks = (s: Stamp) => s.seconds * 1000000000n + BigInt(s.nanos);
      if (!from || (r.toText && !to) || (to && ticks(to) <= ticks(from)) || (i > 0 && (previousEnd === undefined || ticks(from) < previousEnd)) || !r.evidence.trim() || !/^[A-Z]{3}$/.test(r.currency)) { setInvalid(true); return; }
      previousEnd = to ? ticks(to) : undefined;
      const { fromText: _fromText, toText: _toText, ...values } = r;
      periods.push({ ...values, from, to, evidence: r.evidence.trim() });
    }
    if (!reason.trim()) { setInvalid(true); return; }
    setPreview({ requestKey: crypto.randomUUID(), periods, reason: reason.trim() });
    save.reset(); setInvalid(false);
  }
  return <div className="space-y-4">
    <p className="text-sm">{mapping ? t("version", { version: String(mapping.version) }) : t("new")}</p>
    {!canWrite && <p className="text-sm text-gray-500">{t("readOnly")}</p>}
    <form className="space-y-4" onSubmit={e => { e.preventDefault(); prepare(); }}>
      <fieldset disabled={!canWrite || !!preview || save.isPending} className="min-w-0 space-y-4">
        {rows.map((r, i) => <div key={i} className="min-w-0 space-y-3 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
          <p className="break-all text-xs text-gray-500">{r.source?.provider} / {r.source?.accessScope} · {r.instrumentId}</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="text-sm">{t("from")}<input className={field} value={r.fromText} placeholder="2020-01-01T00:00:00Z" onChange={e => update(i, "fromText", e.target.value)} required /></label>
            <label className="text-sm">{t("to")}<input className={field} value={r.toText} placeholder={t("openEnd")} onChange={e => update(i, "toText", e.target.value)} /></label>
            <label className="text-sm">{t("currency")}<input className={field} value={r.currency} maxLength={3} pattern="[A-Z]{3}" onChange={e => update(i, "currency", e.target.value.toUpperCase())} required /></label>
            <label className="text-sm">{t("unit")}<select className={field} value={r.unit} onChange={e => update(i, "unit", e.target.value)}>{(r.kind === "bond" ? ["percent_of_nominal", "currency_per_unit"] : r.kind === "currency" ? ["currency_per_base_nominal"] : ["currency_per_unit"]).map(v => <option key={v} value={v}>{t(`units.${v}`)}</option>)}</select></label>
            {(r.kind === "bond" || r.kind === "currency") && <>
              <label className="text-sm">{t("nominal")}<input className={field} value={r.nominal} inputMode="decimal" pattern="[0-9]+(\.[0-9]+)?" onChange={e => update(i, "nominal", e.target.value)} required={r.unit !== "currency_per_unit"} /></label>
              <label className="text-sm">{t("nominalCurrency")}<input className={field} value={r.nominalCurrency} maxLength={3} pattern="[A-Z]{3}" onChange={e => update(i, "nominalCurrency", e.target.value.toUpperCase())} required={r.unit !== "currency_per_unit"} /></label>
            </>}
            <label className="text-sm">{t("lot")}<input className={field} value={r.lotSize} inputMode="decimal" pattern="[0-9]+(\.[0-9]+)?" onChange={e => update(i, "lotSize", e.target.value)} /></label>
          </div>
          <label className="block text-sm">{t("evidence")}<textarea className={field} value={r.evidence} maxLength={2000} onChange={e => update(i, "evidence", e.target.value)} required /></label>
          {canWrite && <button type="button" className="text-sm text-red-600" onClick={() => setRows(old => old.filter((_, index) => index !== i))}>{t("remove")}</button>}
        </div>)}
        {!rows.length && <p className="text-sm text-gray-500">{t("empty")}</p>}
        {canWrite && <>
          <button type="button" className={button} disabled={rows.length >= 100 || !["share", "bond", "etf", "currency"].includes(candidate.kind)} onClick={() => setRows(old => [...old, { source, instrumentId: candidate.instrumentId, kind: candidate.kind, currency: candidate.currency, unit: candidate.kind === "bond" ? "percent_of_nominal" : candidate.kind === "currency" ? "currency_per_base_nominal" : "currency_per_unit", nominal: "", nominalCurrency: "", lotSize: "", fromText: "", toText: "", evidence: "" }])}>{t("add", { ticker: candidate.ticker, venue: candidate.classCode })}</button>
          <label className="block text-sm">{t("reason")}<textarea className={field} value={reason} maxLength={2000} onChange={e => setReason(e.target.value)} required /></label>
          <button className={button}>{t("preview")}</button>
        </>}
      </fieldset>
    </form>
    {invalid && <p role="alert" className="text-red-600">{t("invalid")}</p>}
    {preview && <section className="space-y-3 rounded-lg bg-amber-50 p-4 dark:bg-amber-950">
      <h4 className="font-semibold">{t("review")}</h4>
      <p className="break-words text-sm">{t("summary", { key: instrumentKey, count: rows.length, version: String(mapping?.version || 0n) })}</p>
      {!rows.length && <p className="font-semibold">{t("revoke")}</p>}
      <p className="text-sm">{t("consequence")}</p>
      <div className="flex flex-wrap gap-3"><button className={button} disabled={save.isPending} onClick={() => save.mutate()}>{t("confirm")}</button><button className="text-sm underline" disabled={save.isPending} onClick={() => { setPreview(undefined); save.reset(); }}>{t("back")}</button></div>
    </section>}
    {save.error && <div role="alert" className="space-y-2 text-red-600"><p>{(save.error as { code?: number }).code === 10 ? t("conflict") : (save.error as { code?: number }).code === 7 ? t("forbidden") : t("saveError")}</p><button className="underline" onClick={reload}>{t("reload")}</button></div>}
  </div>;
}

export default function InvestmentMarketMappingEditor({ tenantId, candidate, source }: { tenantId: string; candidate: MarketInstrument; source: MarketSourceKey }) {
 const readOnly=useInvestmentReadOnly();
  const t = useTranslations("marketMapping");
  const { marketData: api, tenant } = useClients();
  const [draft, setDraft] = useState("");
  const [key, setKey] = useState("");
  const [reload, setReload] = useState(0);
  const roles = useQuery<{ memberships: TenantMembership[] }>({ queryKey: ["investment", tenantId, "roles"], queryFn: () => tenant.listMyTenants({}) });
  const role = roles.data?.memberships.find(m => m.tenant?.id === tenantId)?.role;
  const mapping = useQuery<{ mapping?: InvestmentMarketMapping }>({ queryKey: ["investment-mapping", tenantId, key], enabled: !!key, retry: false, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false, queryFn: async () => {
    try { return await api.getInvestmentMarketMapping({ instrumentKey: key }); }
    catch (error) { if ((error as { code?: number }).code === 5) return {}; throw error; }
  } });
  return <section className="space-y-4 border-t border-gray-200 pt-4 dark:border-gray-700">
    <h3 className="text-lg font-semibold">{t("title")}</h3><p className="text-sm text-gray-500">{t("intro")}</p>
    <form className="flex flex-wrap items-end gap-3" onSubmit={e => { e.preventDefault(); setKey(draft.trim()); }}>
      <label className="min-w-0 flex-1 text-sm">{t("key")}<input className={field} value={draft} onChange={e => setDraft(e.target.value)} required maxLength={200} /></label><button className={button}>{t("load")}</button>
    </form>
    {key && mapping.isFetching && <p role="status">{t("loading")}</p>}
    {(mapping.error || roles.error) && <p role="alert">{t("loadError")} <button className="underline" onClick={() => { mapping.refetch(); roles.refetch(); }}>{t("reload")}</button></p>}
    {key && mapping.isSuccess && <Editor key={`${tenantId}:${key}:${mapping.data.mapping?.version || 0n}:${reload}`} tenantId={tenantId} instrumentKey={key} mapping={mapping.data.mapping} candidate={candidate} source={source} canWrite={!readOnly && (role === 1 || role === 2)} reload={() => { setReload(x => x + 1); mapping.refetch(); }} />}
  </section>;
}
