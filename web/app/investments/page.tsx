"use client";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";

import { useState } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { Protected } from "@/components";
import InvestmentOverview from "@/components/InvestmentOverview";
import InvestmentBrokerSource from "@/components/InvestmentBrokerSource";
import InvestmentFinancialPreparation from "@/components/InvestmentFinancialPreparation";
import Modal from "@/components/Modal";
import { useAssetTenant } from "@/lib/assets";
import type { InvestmentImport, InvestmentImportRow } from "@/proto/budget/v1/investment_import_pb";
import type { TenantMembership } from "@/proto/budget/v1/tenant_pb";

const inputStyle = "w-full rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-900";
const buttonStyle = "rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50";

type Mapping = {
  source_sha256: string;
  accounts: { id: string; name: string }[];
  rows: { source_record: number; account_id: string; status: string; evidence?: string; broker_operation_id?: string }[];
};

function Imports({ tenantId }: { tenantId: string }) {
  const exportT=useTranslations("investmentExport");
 const accountsT=useTranslations("investmentAccounts");
 const forecastT=useTranslations("investmentForecast");
 const t = useTranslations("investments"), payoutsT=useTranslations("investmentPayouts"),operationsT=useTranslations("investmentOperations");
  const { investmentImport: api, tenant } = useClients();
  const qc = useQueryClient();
  const [selected, setSelected] = useState("");
  const [offset, setOffset] = useState(0);
  const [listOffset, setListOffset] = useState(0);
  const [csv, setCSV] = useState<File | null>(null);
  const [mapping, setMapping] = useState<File | null>(null);
  const [requestKey, setRequestKey] = useState("");
  const [editing, setEditing] = useState<InvestmentImportRow | null>(null);
  const [account, setAccount] = useState("");
  const [reason, setReason] = useState("");
  const memberships = useQuery<{ memberships: TenantMembership[] }>({
    queryKey: ["investment-import", tenantId, "roles"], queryFn: () => tenant.listMyTenants({}),
  });
  const role = memberships.data?.memberships.find(m => m.tenant?.id === tenantId)?.role;
  const readOnly=useInvestmentReadOnly();
  const canWrite = !readOnly && (role === 1 || role === 2);
  const imports = useQuery<{ imports: InvestmentImport[] }>({
    queryKey: ["investment-import", tenantId, "list", listOffset],
    queryFn: () => api.listInvestmentImports({ limit: 50, offset: listOffset }),
  });
  const detail = useQuery<{ import?: InvestmentImport }>({
    queryKey: ["investment-import", tenantId, selected, "detail"], enabled: !!selected,
    queryFn: () => api.getInvestmentImport({ id: selected }),
  });
  const rows = useQuery<{ rows: InvestmentImportRow[] }>({
    queryKey: ["investment-import", tenantId, selected, "rows", offset], enabled: !!selected,
    queryFn: () => api.listInvestmentImportRows({ importId: selected, limit: 50, offset }),
  });
  const upload = useMutation({
    mutationFn: async () => {
      if (!csv || csv.size > 20 * 1024 * 1024) throw new Error(t("fileLimit"));
      const bytes = new Uint8Array(await csv.arrayBuffer());
      let accounts = [{ sourceKey: "other:unassigned", name: t("unassigned") }];
      let assignments: { record: number; accountKey: string; evidence: string }[] = [];
      let mappingSourceSha256 = "";
      if (mapping) {
        if (mapping.size > 20 * 1024 * 1024) throw new Error(t("fileLimit"));
        const data = JSON.parse(await mapping.text()) as Mapping;
        if (!Array.isArray(data.accounts) || !Array.isArray(data.rows) || typeof data.source_sha256 !== "string") throw new Error(t("invalidMapping"));
        accounts = data.accounts.map(a => ({ sourceKey: a.id, name: a.name }));
        assignments = data.rows.map(r => ({ record: r.source_record, accountKey: r.account_id, evidence: JSON.stringify({status: r.status, evidence: r.evidence, broker_operation_id: r.broker_operation_id}) }));
        mappingSourceSha256 = data.source_sha256;
      }
      return api.uploadInvestmentImport({ filename: csv.name, csv: bytes, requestKey, accounts, assignments, mappingSourceSha256 }) as Promise<{ import: InvestmentImport }>;
    },
    onSuccess: result => {
      setSelected(result.import.id); setOffset(0);
      qc.invalidateQueries({ queryKey: ["investment-import", tenantId] });
    },
  });
  const reassign = useMutation({
    mutationFn: () => api.reassignInvestmentImportRow({ importId: selected, record: editing!.record, accountId: account, expectedVersion: editing!.version, reason }),
    onSuccess: () => { setEditing(null); setReason(""); qc.invalidateQueries({ queryKey: ["investment-import", tenantId] }); },
  });
  const current = detail.data?.import;
  const names = new Map(current?.accounts.map(a => [a.id, a.name]));
  const error = imports.error || detail.error || rows.error || memberships.error || upload.error || reassign.error;
  const issueLabel = (code: string) => t.has(`issues.${code}`) ? t(`issues.${code}`) : t("issues.other");
  return <main className="mx-auto max-w-7xl space-y-6 px-4 py-8">
<Link className="inline-block text-blue-600 underline" href="/investments/export">{exportT("title")}</Link>

    <div className="flex flex-wrap gap-4"><Link className="inline-block text-blue-600" href="/investments/journal">{t("journal")}</Link><Link className="text-blue-600" href="/investments/accounts">{accountsT("title")}</Link><Link className="text-blue-600" href="/investments/operations">{operationsT("title")}</Link><Link className="text-blue-600" href="/investments/forecast">{forecastT("title")}</Link><Link className="text-blue-600" href="/investments/payouts">{payoutsT("link")}</Link><Link className="text-blue-600" href="/investments/market">{t("market")}</Link><Link className="text-blue-600" href="/investments/value">{t("value")}</Link></div>
    <header><h1 className="text-3xl font-semibold">{t("title")}</h1><p className="mt-2 text-gray-600 dark:text-gray-400">{t("intro")}</p></header>
    <InvestmentOverview tenantId={tenantId}/>
    <div className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100">{t("preparationNotice")}</div>
    {error && <p role="alert" className="rounded-lg bg-red-50 p-3 text-red-800">{error instanceof Error ? error.message : t("error")}</p>}
    <InvestmentBrokerSource canWrite={canWrite} tenantId={tenantId}/>
    {canWrite && <form className="grid gap-4 rounded-xl border border-gray-200 p-5 dark:border-gray-800 md:grid-cols-2" onSubmit={e => { e.preventDefault(); upload.mutate(); }}>
      <label className="space-y-2 text-sm font-medium">{t("csv")}<input className={inputStyle} type="file" accept=".csv,text/csv" disabled={upload.isPending} onChange={e => { setCSV(e.target.files?.[0] || null); setRequestKey(crypto.randomUUID()); upload.reset(); }} /></label>
      <label className="space-y-2 text-sm font-medium">{t("mapping")}<input className={inputStyle} type="file" accept=".json,application/json" disabled={upload.isPending} onChange={e => { setMapping(e.target.files?.[0] || null); setRequestKey(crypto.randomUUID()); upload.reset(); }} /></label>
      <p className="text-sm text-gray-500">{t("uploadHelp")}</p>
      <div className="text-right"><button className={buttonStyle} disabled={!csv || !requestKey || upload.isPending}>{upload.isPending ? t("uploading") : t("upload")}</button></div>
    </form>}
    <section className="space-y-3">
      <h2 className="text-xl font-semibold">{t("imports")}</h2>
      {imports.isPending ? <p>{t("loading")}</p> : imports.data?.imports.length === 0 ? <p className="text-gray-500">{t("empty")}</p> : <div className="flex flex-wrap gap-2">{imports.data?.imports.map(item => <button key={item.id} className={`rounded-lg border px-4 py-3 text-left text-sm ${selected === item.id ? "border-blue-500 bg-blue-50 text-blue-950 dark:bg-blue-950 dark:text-blue-100" : "border-gray-300 dark:border-gray-700"}`} onClick={() => { setSelected(item.id); setOffset(0); setEditing(null); }}><span className="block font-medium">{item.filename}</span><span>{t("rowCount", { count: item.rowCount })}</span></button>)}</div>}
      {(listOffset > 0 || imports.data?.imports.length === 50) && <div className="flex gap-2"><button className={buttonStyle} disabled={!listOffset} onClick={() => setListOffset(x => Math.max(0, x - 50))}>{t("previous")}</button><button className={buttonStyle} disabled={imports.data?.imports.length !== 50} onClick={() => setListOffset(x => x + 50)}>{t("next")}</button></div>}
    </section>
    {current && <section className="space-y-4">
      {canWrite&&<InvestmentFinancialPreparation key={`${current.id}:${current.version}`} source={current}/>}
      <div className="grid gap-3 md:grid-cols-3">{current.accounts.map(a => <div key={a.id} className="rounded-xl border border-gray-200 p-4 dark:border-gray-800"><p className="font-medium">{a.name}</p><p className="mt-2 text-2xl font-semibold">{a.rowCount}<span className="ml-2 text-sm font-normal text-gray-500">{t("rows")}</span></p></div>)}</div>
      <div className="overflow-x-auto rounded-xl border border-gray-200 dark:border-gray-800"><table className="w-full text-left text-sm"><thead className="bg-gray-50 dark:bg-gray-900"><tr>{["date", "event", "instrument", "quantity", "price", "account", "review"].map(key => <th key={key} className="px-3 py-3 font-medium">{t(key)}</th>)}</tr></thead><tbody>
        {rows.data?.rows.map(row => <tr key={row.sourceRowId} className="border-t border-gray-100 align-top dark:border-gray-800">
          <td className="whitespace-nowrap px-3 py-3">{row.sourceFields.Date}</td>
          <td className="px-3 py-3">{t.has(`events.${row.sourceFields.Event}`) ? t(`events.${row.sourceFields.Event}`) : row.sourceFields.Event}</td>
          <td className="px-3 py-3 font-medium">{row.sourceFields.Symbol || "—"}</td>
          <td className="px-3 py-3 tabular-nums">{row.decimals.Quantity}</td><td className="whitespace-nowrap px-3 py-3 tabular-nums">{row.decimals.Price} {row.sourceFields.Currency}</td>
          <td className="px-3 py-3">{names.get(row.accountId)}{row.operationId && <p className="mt-1 text-xs">{t("linkedOperation")} · {row.operationStatus}</p>}{canWrite && !row.operationId && <button className="mt-1 block text-blue-600" onClick={() => { setEditing(row); setAccount(row.accountId); setReason(""); reassign.reset(); }}>{t("changeAccount")}</button>}</td>
          <td className="min-w-60 px-3 py-3"><details><summary className="cursor-pointer text-amber-700 dark:text-amber-400">{t("reviewCount", { count: row.issues.length })}</summary><ul className="mt-2 list-inside list-disc space-y-1 text-gray-600 dark:text-gray-400">{row.issues.map(code => <li key={code}>{issueLabel(code)}</li>)}</ul>{row.sourceFields.Note && <p className="mt-2 whitespace-pre-wrap">{row.sourceFields.Note}</p>}</details></td>
        </tr>)}
      </tbody></table>{rows.isFetching && <p className="p-3" role="status">{t("loading")}</p>}</div>
      <div className="flex items-center justify-between"><button className={buttonStyle} disabled={offset === 0 || rows.isFetching} onClick={() => setOffset(x => Math.max(0, x - 50))}>{t("previous")}</button><span className="text-sm">{Math.min(offset + 1, current.rowCount)}–{Math.min(offset + 50, current.rowCount)} / {current.rowCount}</span><button className={buttonStyle} disabled={offset + 50 >= current.rowCount || rows.isFetching} onClick={() => setOffset(x => x + 50)}>{t("next")}</button></div>
    </section>}
    {editing && <Modal open title={t("changeAccount")} onClose={() => { if (!reassign.isPending) setEditing(null); }}><form className="space-y-4 p-6" onSubmit={e => { e.preventDefault(); reassign.mutate(); }}><p>{editing.sourceFields.Date} · {editing.sourceFields.Symbol}</p><label className="block text-sm">{t("account")}<select className={inputStyle} value={account} disabled={reassign.isPending} onChange={e => setAccount(e.target.value)}>{current?.accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</select></label><label className="block text-sm">{t("reason")}<textarea autoFocus className={inputStyle} maxLength={2000} required value={reason} disabled={reassign.isPending} onChange={e => setReason(e.target.value)} /></label>{reassign.error && <p role="alert" className="text-red-600">{String(reassign.error)}</p>}<div className="flex justify-end gap-3"><button type="button" disabled={reassign.isPending} onClick={() => setEditing(null)}>{t("cancel")}</button><button className={buttonStyle} disabled={!reason.trim() || account === editing.accountId || reassign.isPending}>{t("save")}</button></div></form></Modal>}
  </main>;
}
export default function InvestmentsPage() {
  const tenantId = useAssetTenant();
  return <Protected>{tenantId && <Imports key={tenantId} tenantId={tenantId} />}</Protected>;
}
