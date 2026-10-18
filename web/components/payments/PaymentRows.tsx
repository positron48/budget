"use client";
import { useQuery } from "@tanstack/react-query";
import { useClients } from "@/app/providers";
import { useDisplayCurrency } from "@/lib/display-currency";
import { useLocale, useTranslations } from "next-intl";
import { assetMoney } from "@/lib/assets";
import type { PaymentOccurrence, ListRecurringPaymentTemplatesResponse } from "@/proto/budget/v1/recurring_payment_pb";
export default function PaymentRows({
  items,
  onAction,
}: {
  items: PaymentOccurrence[];
  onAction: (o: PaymentOccurrence, action: string) => void;
}) {
  const t = useTranslations("payments"),
    locale = useLocale();
  const { recurringPayment: api } = useClients();
  const { tenant } = useDisplayCurrency();
  const { data } = useQuery<ListRecurringPaymentTemplatesResponse>({
    queryKey: ["payments", "templates", tenant],
    enabled: !!tenant && items.length > 0,
    queryFn: () => api.listRecurringPaymentTemplates({}),
  });
  const intervals = new Map(
    data?.templates.map((template) => [template.id, template.intervalMonths]),
  );
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card divide-y divide-border">
      {items.map((o) => (
        <article
          key={o.id}
          className="px-4 py-3 space-y-2"
          data-testid="payment-row"
        >
          <div className="flex flex-wrap justify-between gap-x-4 gap-y-1">
            <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
              <h3 className="font-semibold break-words">{o.name}</h3>
              {[1, 3, 12].includes(intervals.get(o.templateId) ?? 0) && (
                <span className="text-xs text-muted-foreground">
                  {t(`frequency${intervals.get(o.templateId)}`)}
                </span>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2 sm:justify-end">
              <strong className="text-base tabular-nums">
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
                    ? "inline-flex rounded-full bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-400"
                    : o.status === "paid"
                      ? "inline-flex rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:text-emerald-400"
                      : "inline-flex rounded-full bg-secondary px-2 py-0.5 text-xs font-medium text-muted-foreground"
                }
              >
                {t(o.status)}
              </p>
            </div>
          </div>
          {o.displayAmount &&
            o.displayAmount.currencyCode !== o.currencyCode && (
              <p className="text-sm text-muted-foreground">
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
          {o.note && <p className="text-sm text-muted-foreground">{o.note}</p>}
          <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
            {o.transaction && (
              <p className="text-sm text-muted-foreground">
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
                <button
                  className="rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-secondary hover:text-foreground"
                  onClick={() => onAction(o, "view")}
                >
                  {t("viewExpense")}
                </button>
              </p>
            )}
            <div className="flex flex-wrap items-center gap-1 text-sm">
              {o.status === "paid" || o.status === "needs_review" ? (
                <>
                  {o.status === "needs_review" && (
                    <button
                      className="btn btn-primary btn-sm"
                      onClick={() => onAction(o, "pay")}
                    >
                      {t("reconfirm")}
                    </button>
                  )}
                  <button
                    className="rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-secondary hover:text-foreground"
                    onClick={() => onAction(o, "unlink")}
                  >
                    {t("unlink")}
                  </button>
                </>
              ) : o.status === "skipped" ? (
                <>
                  <p>{o.skipReason}</p>
                  <button
                    className="rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-secondary hover:text-foreground"
                    onClick={() => onAction(o, "skip")}
                  >
                    {t("restore")}
                  </button>
                </>
              ) : (
                <>
                  <button
                    className="btn btn-primary btn-sm"
                    onClick={() => onAction(o, "pay")}
                  >
                    {t("pay")}
                  </button>
                  <button
                    className="rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-secondary hover:text-foreground"
                    onClick={() => onAction(o, "edit")}
                  >
                    {t("edit")}
                  </button>
                  <button
                    className="rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-secondary hover:text-foreground"
                    onClick={() => onAction(o, "skip")}
                  >
                    {t("skip")}
                  </button>
                </>
              )}
            </div>
          </div>
        </article>
      ))}
    </div>
  );
}
