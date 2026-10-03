"use client";
import Link from "next/link";
import { useLocale, useTranslations } from "next-intl";
import { Button, Icon } from "@/components";
import type { AssetAccount } from "@/proto/budget/v1/asset_pb";
import {
  ASSET_ICONS,
  AssetKind,
  assetDate,
  assetMoney,
  hasNegativeBalance,
} from "@/lib/assets";
import type { AssetAction } from "./AssetDialogs";

export default function AssetAccountCard({
  account,
  onAction,
}: {
  account: AssetAccount;
  onAction: (action: AssetAction) => void;
}) {
  const t = useTranslations("assets");
  const locale = useLocale();
  const negative = hasNegativeBalance(account);
  const latest = account.balances.reduce<bigint>(
    (current, b) =>
      (b.confirmedAt?.seconds ?? 0n) > current
        ? b.confirmedAt!.seconds
        : current,
    0n,
  );
  return (
    <article
      className={`asset-account-card ${account.archived ? "opacity-70" : ""}`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <span className={`asset-kind-icon asset-kind-${account.kind}`}>
            <Icon name={ASSET_ICONS[account.kind as AssetKind]} size={21} />
          </span>
          <div className="min-w-0">
            <Link
              className="block break-words font-semibold hover:text-[hsl(var(--primary))]"
              href={`/assets/${account.id}`}
            >
              {account.name}
            </Link>
            <p className="mt-0.5 text-xs text-[hsl(var(--muted-foreground))]">
              {account.institution || t(`kinds.${account.kind}`)}
            </p>
          </div>
        </div>
        {account.archived && <span className="asset-tag">{t("archived")}</span>}
      </div>
      <div className="my-5 space-y-1.5">
        {account.balances.length ? (
          account.balances.map((b) => (
            <div
              key={b.amount?.currencyCode}
              className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1"
            >
              <span className="text-xs font-medium text-[hsl(var(--muted-foreground))]">
                {b.amount?.currencyCode}
              </span>
              <span
                className={`text-xl font-semibold tracking-tight tabular-nums ${(b.amount?.minorUnits ?? 0n) < 0n ? "text-[hsl(var(--negative))]" : ""}`}
              >
                {assetMoney(
                  b.amount?.minorUnits,
                  b.amount?.currencyCode,
                  locale,
                )}
              </span>
            </div>
          ))
        ) : (
          <p className="text-sm text-[hsl(var(--muted-foreground))]">
            {t("unknownBalance")}
          </p>
        )}
        {account.kind === "investment" && (
          <p className="pt-1 text-xs text-[hsl(var(--muted-foreground))]">
            {t("wholeAccount")}
          </p>
        )}
      </div>
      {account.kind === "deposit" && (
        <div className="mb-4 flex flex-wrap gap-2">
          {account.depositRateDecimal && (
            <span className="asset-tag">
              {account.depositRateDecimal.replace(/\.?0+$/, "") || "0"}%{" "}
              {t("annual")}
            </span>
          )}
          {account.depositMaturesOn && (
            <span className="asset-tag">
              {t("until", {
                date: new Intl.DateTimeFormat(locale, {
                  dateStyle: "medium",
                }).format(new Date(`${account.depositMaturesOn}T12:00:00`)),
              })}
            </span>
          )}
        </div>
      )}
      {negative && (
        <p className="mb-3 flex items-center gap-1.5 text-xs text-[hsl(var(--warning))]">
          <Icon name="alert-circle" size={14} />
          {t("negativeWarning")}
        </p>
      )}
      <div className="mt-auto flex items-center justify-between gap-2 border-t border-[hsl(var(--border))] pt-3">
        <p className="text-xs text-[hsl(var(--muted-foreground))]">
          {latest > 0n
            ? t("checked", { date: assetDate({ seconds: latest }, locale) })
            : t("notChecked")}
        </p>
        {!account.archived ? (
          <Button
            size="sm"
            variant="ghost"
            icon="fx"
            onClick={() => onAction({ mode: "snapshot", account })}
          >
            {t("updateShort")}
          </Button>
        ) : (
          <Link
            className="text-xs text-[hsl(var(--primary))]"
            href={`/assets/${account.id}`}
          >
            {t("history")}
          </Link>
        )}
      </div>
    </article>
  );
}
