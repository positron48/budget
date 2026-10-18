"use client";
import { useLocale, useTranslations } from "next-intl";
import Icon from "@/components/Icon";
import { assetMoney } from "@/lib/assets";
import type { PaymentSummary as Summary } from "@/proto/budget/v1/recurring_payment_pb";
export default function PaymentSummary({
  summary,
  month,
  showPaid = true,
}: {
  summary?: Summary;
  month: string;
  showPaid?: boolean;
}) {
  const t = useTranslations("payments"),
    locale = useLocale();
  if (!summary) return null;
  const amount = (m?: { minorUnits: bigint; currencyCode: string }) =>
    assetMoney(m?.minorUnits ?? 0n, m?.currencyCode || "RUB", locale);
  const remaining =
    (summary.exact?.minorUnits ?? 0n) + (summary.estimated?.minorUnits ?? 0n);
  const label = /^\d{4}-\d{2}$/.test(month)
    ? new Date(`${month}-01T12:00:00`).toLocaleDateString(locale, {
        month: "long",
        year: "numeric",
      })
    : month;
  if (summary.total === 0)
    return (
      <div
        data-testid="payment-summary"
        className="flex items-center gap-3 rounded-lg border border-dashed border-border bg-card px-4 py-3"
      >
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-secondary text-muted-foreground">
          <Icon name="calendar" size={16} />
        </span>
        <div>
          <p className="text-sm capitalize text-muted-foreground">{label}</p>
          <p className="mt-1 font-medium">{t("emptyMonth")}</p>
        </div>
      </div>
    );
  return (
    <div
      className="rounded-lg border border-border bg-secondary/40 px-4 py-3 space-y-1.5"
      data-testid="payment-summary"
    >
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className="text-sm text-muted-foreground">{t("remaining")}</span>
        <strong className="text-xl tabular-nums">
          {summary.estimated?.minorUnits ? "≈ " : ""}
          {assetMoney(remaining, summary.exact?.currencyCode || "RUB", locale)}
        </strong>
        <span className="ml-auto text-xs capitalize text-muted-foreground">
          {label}
        </span>
      </div>
      <div className="text-xs text-muted-foreground flex flex-wrap gap-x-4 gap-y-1">
        {summary.outstanding > 0 && (
          <>
            <span>
              {t("exact")}: {amount(summary.exact)}
            </span>
            <span>
              {t("estimate")}: {amount(summary.estimated)}
            </span>
          </>
        )}
        {showPaid && (
          <span>
            {t("paidForMonth")}: {amount(summary.paid)}
          </span>
        )}
        {summary.outstanding === 0 && summary.needsReview === 0 && (
          <span className="text-emerald-700 dark:text-emerald-400">
            {t("allPaid")}
          </span>
        )}
      </div>
      {(summary.unknown > 0 ||
        summary.needsReview > 0 ||
        summary.missingFx > 0) && (
        <p className="text-sm text-amber-600">{t("incompleteTotal")}</p>
      )}
      {summary.unknown > 0 && (
        <p>{t("unknownCount", { count: summary.unknown })}</p>
      )}
      {summary.needsReview > 0 && (
        <p className="text-amber-600">
          {t("reviewCount", { count: summary.needsReview })}
        </p>
      )}
      {summary.missingFx > 0 && (
        <p className="text-amber-600">{t("missingFx")}</p>
      )}
    </div>
  );
}
