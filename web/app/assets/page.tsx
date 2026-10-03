"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useLocale, useTranslations } from "next-intl";
import { ClientsProvider, useClients } from "@/app/providers";
import { Button, Icon, Protected } from "@/components";
import type {
  AssetAccount,
  GetOverviewResponse,
} from "@/proto/budget/v1/asset_pb";
import {
  ASSET_CURRENCIES,
  ASSET_KINDS,
  ASSET_ICONS,
  assetDate,
  assetMoney,
  useAssetTenant,
} from "@/lib/assets";
import AssetAccountCard from "@/components/assets/AssetAccountCard";
import AssetDialogs, { AssetAction } from "@/components/assets/AssetDialogs";

function AssetsOverview() {
  const { asset } = useClients();
  const t = useTranslations("assets");
  const locale = useLocale();
  const tenant = useAssetTenant();
  const [target, setTarget] = useState("");
  const [kind, setKind] = useState("");
  const [institution, setInstitution] = useState("");
  const [archived, setArchived] = useState(false);
  const [action, setAction] = useState<AssetAction | null>(null);
  const overview = useQuery<GetOverviewResponse>({
    queryKey: ["assets", "overview", tenant, target],
    enabled: !!tenant,
    queryFn: () => asset.getOverview({ targetCurrencyCode: target }),
  });
  const all = useQuery<{ accounts: AssetAccount[] }>({
    queryKey: ["assets", "accounts", tenant, archived],
    enabled: !!tenant,
    queryFn: () => asset.listAccounts({ includeArchived: archived }),
  });
  const accounts = all.data?.accounts || overview.data?.accounts || [];
  const activeAccounts = accounts.filter((a) => !a.archived);
  const visible = accounts.filter(
    (a) =>
      (archived ? a.archived : !a.archived) &&
      (!kind || a.kind === kind) &&
      (!institution || a.institution === institution),
  );
  const institutions = Array.from(
    new Set(accounts.map((a) => a.institution).filter(Boolean)),
  ).sort();
  const currency = overview.data?.total?.currencyCode || target || "RUB";
  const summaries = useMemo(() => {
    const rates = new Map(
      overview.data?.rates.map((r) => [r.fromCurrencyCode, r.rateDecimal]) ||
        [],
    );
    return ASSET_KINDS.map((k) => {
      const group = (overview.data?.accounts || []).filter((a) => a.kind === k);
      let total = 0n;
      let incomplete = false;
      for (const a of group)
        for (const b of a.balances) {
          if (!b.amount) continue;
          if (b.amount.currencyCode === currency) {
            total += b.amount.minorUnits;
            continue;
          }
          const rate = rates.get(b.amount.currencyCode);
          if (!rate) {
            incomplete ||= b.amount.minorUnits !== 0n;
            continue;
          }
          const [whole, fraction = ""] = rate.split(".");
          const numerator = BigInt(whole + fraction);
          const denominator = 10n ** BigInt(fraction.length);
          const signed = b.amount.minorUnits * numerator;
          const absolute = signed < 0n ? -signed : signed;
          const converted = (absolute + denominator / 2n) / denominator;
          total += signed < 0n ? -converted : converted;
        }
      return { kind: k, count: group.length, total, incomplete };
    });
  }, [overview.data, currency]);
  const loading = !tenant || overview.isLoading || all.isLoading;
  return (
    <main className="asset-page container mx-auto max-w-7xl px-4 py-7 sm:px-6 lg:px-8">
      <div className="mb-7 flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-[hsl(var(--primary))]">
            <Icon name="wallet" size={17} />
            {t("eyebrow")}
          </div>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">
            {t("title")}
          </h1>
          <p className="mt-2 max-w-xl text-sm text-[hsl(var(--muted-foreground))]">
            {t("subtitle")}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {activeAccounts.length > 0 && (
            <Button
              variant="outline"
              icon="arrow-right"
              onClick={() => setAction({ mode: "transfer" })}
            >
              {t("transfer")}
            </Button>
          )}
          <Button icon="plus" onClick={() => setAction({ mode: "account" })}>
            {t("createAccount")}
          </Button>
        </div>
      </div>
      {(overview.error || all.error) && (
        <div
          role="alert"
          className="mb-5 rounded-xl border border-[hsl(var(--negative)/0.3)] bg-[hsl(var(--negative)/0.07)] p-4"
        >
          <p>{t("errors.load")}</p>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              void overview.refetch();
              void all.refetch();
            }}
          >
            {t("retry")}
          </Button>
        </div>
      )}
      <section className="asset-total-panel" aria-label={t("total")}>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <p className="text-sm text-[hsl(var(--muted-foreground))]">
              {t("total")}
              {overview.data?.incomplete && (
                <span className="ml-2 text-[hsl(var(--warning))]">
                  · {t("partial")}
                </span>
              )}
            </p>
            <p className="mt-3 break-words text-3xl font-semibold tracking-tight tabular-nums sm:text-5xl">
              {loading || overview.error
                ? "—"
                : assetMoney(
                    overview.data?.total?.minorUnits,
                    currency,
                    locale,
                  )}
            </p>
            <p className="mt-3 text-xs text-[hsl(var(--muted-foreground))]">
              {t("accountsCount", { count: activeAccounts.length })} ·{" "}
              {t("manualValuations")}
            </p>
          </div>
          <label className="min-w-28 text-xs text-[hsl(var(--muted-foreground))]">
            <span className="mb-1.5 block">{t("displayCurrency")}</span>
            <select
              className="input !bg-[hsl(var(--card))]"
              value={target || currency}
              onChange={(e) => setTarget(e.target.value)}
            >
              {Array.from(new Set([currency, ...ASSET_CURRENCIES])).map((c) => (
                <option key={c}>{c}</option>
              ))}
            </select>
          </label>
        </div>
        {!!overview.data?.rates.length && (
          <details className="mt-5 border-t border-[hsl(var(--border))] pt-3 text-xs text-[hsl(var(--muted-foreground))]">
            <summary className="cursor-pointer">{t("ratesUsed")}</summary>
            <div className="mt-2 flex flex-wrap gap-x-5 gap-y-2">
              {overview.data.rates.map((r) => (
                <span key={r.fromCurrencyCode}>
                  {r.fromCurrencyCode} → {currency}: {r.rateDecimal} ·{" "}
                  {assetDate(r.asOf, locale)}
                </span>
              ))}
            </div>
          </details>
        )}
        {overview.data?.incomplete && (
          <p className="mt-4 flex items-start gap-2 rounded-lg bg-[hsl(var(--warning)/0.1)] p-3 text-sm">
            <Icon name="info" size={17} className="mt-0.5 shrink-0" />
            {t("missingRates", {
              currencies: overview.data.missingCurrencies.join(", "),
            })}{" "}
            <a href="/fx" className="shrink-0 underline">
              {t("setRates")}
            </a>
          </p>
        )}
      </section>
      <div className="my-5 grid grid-cols-2 gap-3 lg:grid-cols-4">
        {summaries.map((s) => (
          <button
            key={s.kind}
            type="button"
            className={`asset-summary ${kind === s.kind ? "asset-summary-selected" : ""}`}
            onClick={() => setKind((old) => (old === s.kind ? "" : s.kind))}
            aria-pressed={kind === s.kind}
          >
            <span className={`asset-kind-icon asset-kind-${s.kind}`}>
              <Icon name={ASSET_ICONS[s.kind]} size={18} />
            </span>
            <span className="block min-w-0">
              <span className="block text-xs text-[hsl(var(--muted-foreground))]">
                {t(`kinds.${s.kind}`)}
              </span>
              <span className="mt-1 block break-words text-sm font-semibold tabular-nums sm:text-base">
                {loading || overview.error ? "—" : assetMoney(s.total, currency, locale)}
                {s.incomplete ? " *" : ""}
              </span>
            </span>
          </button>
        ))}
      </div>
      <div className="mb-5 mt-8 flex flex-wrap items-center justify-between gap-3">
        <div className="flex gap-1 rounded-lg bg-[hsl(var(--secondary))] p-1">
          <button
            type="button"
            onClick={() => setArchived(false)}
            aria-pressed={!archived}
            className={`rounded-md px-3 py-1.5 text-sm ${!archived ? "bg-[hsl(var(--card))] shadow-sm" : "text-[hsl(var(--muted-foreground))]"}`}
          >
            {t("yourAccounts")}
          </button>
          <button
            type="button"
            onClick={() => setArchived(true)}
            aria-pressed={archived}
            className={`rounded-md px-3 py-1.5 text-sm ${archived ? "bg-[hsl(var(--card))] shadow-sm" : "text-[hsl(var(--muted-foreground))]"}`}
          >
            {t("archive")}
          </button>
        </div>
        <div className="flex flex-wrap gap-2">
          <select
            className="input !w-auto"
            aria-label={t("kind")}
            value={kind}
            onChange={(e) => setKind(e.target.value)}
          >
            <option value="">{t("allKinds")}</option>
            {ASSET_KINDS.map((k) => (
              <option key={k} value={k}>
                {t(`kinds.${k}`)}
              </option>
            ))}
          </select>
          {institutions.length > 0 && (
            <select
              className="input !w-auto max-w-52"
              aria-label={t("institution")}
              value={institution}
              onChange={(e) => setInstitution(e.target.value)}
            >
              <option value="">{t("allInstitutions")}</option>
              {institutions.map((i) => (
                <option key={i}>{i}</option>
              ))}
            </select>
          )}
        </div>
      </div>
      {loading ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="asset-account-card min-h-56 animate-pulse">
              <div className="h-5 w-1/2 rounded bg-[hsl(var(--muted))]" />
              <div className="mt-8 h-8 rounded bg-[hsl(var(--muted))]" />
            </div>
          ))}
        </div>
      ) : visible.length ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visible.map((a) => (
            <AssetAccountCard key={a.id} account={a} onAction={setAction} />
          ))}
        </div>
      ) : (
        <div className="asset-empty">
          <span className="asset-kind-icon asset-kind-bank !h-14 !w-14">
            <Icon name="wallet" size={28} />
          </span>
          <h2 className="mt-5 text-xl font-semibold">
            {archived
              ? t("archiveEmpty")
              : accounts.length
                ? t("filterEmpty")
                : t("emptyTitle")}
          </h2>
          <p className="mt-2 max-w-md text-sm text-[hsl(var(--muted-foreground))]">
            {!archived && !accounts.length
              ? t("emptyDescription")
              : t("emptyFilterHint")}
          </p>
          {!archived && !accounts.length && (
            <Button
              className="mt-5"
              icon="plus"
              onClick={() => setAction({ mode: "account" })}
            >
              {t("createFirst")}
            </Button>
          )}
        </div>
      )}
      <AssetDialogs
        key={
          action ? `${action.mode}-${action.account?.id || "new"}` : "closed"
        }
        action={action}
        accounts={accounts}
        onClose={() => setAction(null)}
      />
    </main>
  );
}
export default function AssetsPage() {
  return (
    <Protected>
      <ClientsProvider>
        <AssetsOverview />
      </ClientsProvider>
    </Protected>
  );
}
