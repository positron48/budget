"use client";

import {useState} from "react";
import Link from "next/link";
import {create} from "@bufbuild/protobuf";
import {useLocale} from "next-intl";
import {useQuery, useQueryClient} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import Modal from "@/components/Modal";
import {assetMoney} from "@/lib/assets";
import {trackerError} from "@/lib/portfolio-tracker";
import {InvestmentAssetActivationSchema} from "@/proto/budget/v1/investment_pb";
import type {InvestmentLedger, InvestmentAssetActivation, InvestmentAssetActivationPreview} from "@/proto/budget/v1/investment_pb";
import type {ListAccountsResponse} from "@/proto/budget/v1/asset_pb";

const button = "rounded-lg border border-slate-300 px-4 py-2 text-sm dark:border-slate-700 disabled:opacity-40";

export default function PortfolioAssetLink({ledger, tenantId}: {ledger: InvestmentLedger; tenantId: string}) {
  const locale = useLocale(), ru = locale === "ru", tr = (r: string, e: string) => ru ? r : e;
  const {asset, investment} = useClients(), qc = useQueryClient();
  const [open, setOpen] = useState(false), [accountId, setAccountId] = useState("");
  const [preview, setPreview] = useState<InvestmentAssetActivationPreview>();
  const [request, setRequest] = useState<InvestmentAssetActivation>();
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const accounts = useQuery<ListAccountsResponse>({queryKey: ["assets", "portfolio-links", tenantId], queryFn: () => asset.listAccounts({includeArchived: false})});
  const linked = accounts.data?.accounts.find(a => a.investmentLedgerId === ledger.id);
  if (linked) return <Link className={button} href={`/assets/${linked.id}`}>{tr("В накоплениях", "View in savings")}</Link>;
  const available = accounts.data?.accounts.filter(a => a.kind === "investment" && !a.archived && a.investmentMode !== "ledger") || [];
  const prepare = async () => {
    setBusy(true); setError(""); setPreview(undefined); setRequest(undefined);
    try {
      const milliseconds = Date.now();
      const next = create(InvestmentAssetActivationSchema, {accountId, ledgerId: ledger.id,
        valuationFrom: {seconds: BigInt(Math.floor(milliseconds / 1000)), nanos: milliseconds % 1000 * 1000000},
        requestKey: crypto.randomUUID(), evidence: "User reviewed replacement of manual savings estimate with tracker securities value"});
      const response = await investment.prepareInvestmentAssetActivation({activation: next});
      if (!response.preview?.account || !response.preview.ledger || !response.preview.manualValue || !response.preview.ledgerKnownValue || response.preview.previewHash.length !== 64) throw new Error("missing_preview");
      setRequest(next); setPreview(response.preview);
    } catch (e) { setError(trackerError(e, ru)); } finally { setBusy(false); }
  };
  const commit = async () => {
    if (!preview || !request) return;
    setBusy(true); setError("");
    try {
      await investment.activateInvestmentAsset({activation: {...request, previewHash: preview.previewHash,
        expectedAccountVersion: preview.account?.version, expectedLedgerVersion: preview.ledger?.version}});
      await qc.invalidateQueries({queryKey: ["assets"]});
      setOpen(false); setPreview(undefined); setRequest(undefined);
    } catch (e) { setPreview(undefined); setRequest(undefined); setError(trackerError(e, ru)); } finally { setBusy(false); }
  };
  return <>
    <button className={button} onClick={() => {setOpen(true); setError(""); setPreview(undefined);}}>{tr("Включить в накопления", "Include in savings")}</button>
    {open && <Modal open title={tr("Оценка в накоплениях", "Savings valuation")} onClose={() => {if (!busy) setOpen(false);}}>
      <div className="space-y-4">
        <p>{tr("Выберите инвестиционный счёт в накоплениях. Его ручная оценка будет заменена стоимостью бумаг этого портфеля, а не прибавлена к ней.", "Choose an investment account in savings. Its manual estimate will be replaced by this portfolio's securities value, rather than added to it.")}</p>
        <p className="text-sm text-slate-500">{tr("В накоплениях используются курсы ЦБ с ручными корректировками. Выплаты и свободные деньги брокерского счёта не прибавляются к стоимости бумаг.", "Savings use CBR rates with manual overrides. Payouts and uninvested brokerage cash are not added to securities value.")}</p>
        {accounts.isPending ? <p>{tr("Загрузка счетов…", "Loading accounts…")}</p> : available.length ? <label className="block">{tr("Счёт в накоплениях", "Savings account")}
          <select className="mt-1 w-full rounded-lg border bg-transparent px-3 py-2" disabled={busy} value={accountId} onChange={e => {setAccountId(e.target.value); setPreview(undefined); setRequest(undefined);}}>
            <option value="">{tr("Выберите счёт", "Choose an account")}</option>
            {available.map(a => <option key={a.id} value={a.id}>{a.name} · {a.fixedCurrencyCode}</option>)}
          </select></label> : <p>{tr("Сначала создайте инвестиционный счёт в разделе «Накопления».", "First create an investment account in Savings.")} <Link href="/assets">{tr("Открыть накопления", "Open savings")}</Link></p>}
        {(error || accounts.error) && <p role="alert">{error || trackerError(accounts.error, ru)}</p>}
        {preview && <div className="space-y-2 rounded-lg border p-4">
          <p>{tr("Ручная оценка", "Manual estimate")}: {assetMoney(preview.manualValue?.minorUnits, preview.manualValue?.currencyCode, locale)}</p>
          <p>{preview.complete ? tr("Стоимость бумаг", "Securities value") : tr("Стоимость известной части", "Known securities value")}: {assetMoney(preview.ledgerKnownValue?.minorUnits, preview.ledgerKnownValue?.currencyCode, locale)}</p>
          {!preview.complete && <p role="status">{tr("Оценка неполная: для части бумаг нет цены или курса. В накоплениях также будет показано это предупреждение.", "Valuation is partial: some securities lack a price or exchange rate. Savings will show the same warning.")}</p>}
          {preview.issues.includes("manual_facts_after_cutover") && <p role="alert">{tr("После выбранной даты уже есть ручные изменения. Обновите предварительную оценку.", "Manual changes already exist after the selected date. Refresh the preview.")}</p>}
        </div>}
        {preview ? <button className={button} disabled={busy || preview.issues.includes("manual_facts_after_cutover")} onClick={() => void commit()}>{tr("Заменить ручную оценку", "Replace manual estimate")}</button>
          : <button className={button} disabled={busy || !accountId} onClick={() => void prepare()}>{tr("Посмотреть оценку", "Preview value")}</button>}
      </div>
    </Modal>}
  </>;
}
