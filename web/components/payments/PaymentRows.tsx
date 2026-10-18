"use client";
import { useLocale, useTranslations } from "next-intl";
import { assetMoney } from "@/lib/assets";
import type { PaymentOccurrence } from "@/proto/budget/v1/recurring_payment_pb";
export default function PaymentRows({
  items,
  onAction,
}: {
  items: PaymentOccurrence[];
  onAction: (o: PaymentOccurrence, action: string) => void;
}) {
  const t = useTranslations("payments"),
    locale = useLocale();
  return (
    <div className="space-y-3">
      {items.map((o) => (
        <article
          key={o.id}
          className="rounded-xl border border-border p-4 space-y-3"
          data-testid="payment-row"
        >
          <div className="flex flex-wrap justify-between gap-2">
            <div>
              <h3 className="font-semibold">{o.name}</h3>
              <p className="text-sm">
                {t("forPeriod", { period: o.periodMonth })} ·{" "}
                {t("due", { date: o.dueDate })}
              </p>
            </div>
            <div>
              <strong>
                {o.expectedAmount === undefined
                  ? t("unknown")
                  : assetMoney(o.expectedAmount, o.currencyCode, locale)}
              </strong>
              {o.estimateSource !== "exact" &&
                o.expectedAmount !== undefined && (
                  <span> · {t("approximately")}</span>
                )}
              <p
                className={
                  o.status === "needs_review" || o.status === "overdue"
                    ? "text-amber-600"
                    : "text-sm"
                }
              >
                {t(o.status)}
              </p>
            </div>
          </div>
          {o.displayAmount &&
            o.displayAmount.currencyCode !== o.currencyCode && (
              <p className="text-sm">
                ≈{" "}
                {assetMoney(
                  o.displayAmount.minorUnits,
                  o.displayAmount.currencyCode,
                  locale,
                )}{" "}
                {o.fxDate && `· ${t("fxDate")}: ${o.fxDate}`}
              </p>
            )}
          {o.incomplete && <p>{t("missingFx")}</p>}
          {o.note && <p className="text-sm">{o.note}</p>}
          {o.transaction && (
            <p className="text-sm">
              {assetMoney(
                o.transaction.amount?.minorUnits,
                o.transaction.amount?.currencyCode,
                locale,
              )}{" "}
              ·{" "}
              {o.transaction.occurredAt
                ? new Date(
                    Number(o.transaction.occurredAt.seconds) * 1000,
                  ).toLocaleDateString(locale)
                : ""}{" "}
              ·{" "}
              <button className="underline" onClick={() => onAction(o, "view")}>
                {t("viewExpense")}
              </button>
            </p>
          )}
          <div className="flex flex-wrap gap-3 text-sm">
            {o.status === "paid" || o.status === "needs_review" ? (
              <>
                {o.status === "needs_review" && (
                  <button
                    className="btn btn-primary"
                    onClick={() => onAction(o, "pay")}
                  >
                    {t("reconfirm")}
                  </button>
                )}
                <button
                  className="underline"
                  onClick={() => onAction(o, "unlink")}
                >
                  {t("unlink")}
                </button>
              </>
            ) : o.status === "skipped" ? (
              <>
                <p>{o.skipReason}</p>
                <button
                  className="underline"
                  onClick={() => onAction(o, "skip")}
                >
                  {t("restore")}
                </button>
              </>
            ) : (
              <>
                <button
                  className="btn btn-primary"
                  onClick={() => onAction(o, "pay")}
                >
                  {t("pay")}
                </button>
                <button
                  className="underline"
                  onClick={() => onAction(o, "edit")}
                >
                  {t("edit")}
                </button>
                <button
                  className="underline"
                  onClick={() => onAction(o, "skip")}
                >
                  {t("skip")}
                </button>
              </>
            )}
          </div>
        </article>
      ))}
    </div>
  );
}
