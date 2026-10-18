"use client";
import { useLocale, useTranslations } from "next-intl";
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
  return (
    <div
      className="rounded-xl bg-surface-elevated p-4 space-y-2"
      data-testid="payment-summary"
    >
      <div className="text-sm">{month}</div>
      {(summary.unknown > 0 ||
        summary.needsReview > 0 ||
        summary.missingFx > 0) && (
        <p className="text-amber-600">{t("incompleteTotal")}</p>
      )}
      <div className="text-2xl font-semibold">
        {summary.estimated?.minorUnits ? `${t("approximately")} ` : ""}
        {assetMoney(remaining, summary.exact?.currencyCode || "RUB", locale)}
      </div>
      <div className="text-sm flex flex-wrap gap-3">
        <span>
          {t("exact")}: {amount(summary.exact)}
        </span>
        <span>
          {t("estimate")}: {amount(summary.estimated)}
        </span>
        {showPaid && (
          <span>
            {t("paidForMonth")}: {amount(summary.paid)}
          </span>
        )}
      </div>
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
      {summary.total === 0 ? (
        <p>{t("emptyMonth")}</p>
      ) : (
        summary.outstanding === 0 &&
        summary.needsReview === 0 && <p>{t("allPaid")}</p>
      )}
    </div>
  );
}
