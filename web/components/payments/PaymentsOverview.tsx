"use client";
import Link from "next/link";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { useDisplayCurrency } from "@/lib/display-currency";
import { usePaymentMonth } from "@/lib/payments";
import type {
  GetPaymentOverviewResponse,
  PaymentOccurrence,
} from "@/proto/budget/v1/recurring_payment_pb";
import PaymentSummary from "./PaymentSummary";
import PaymentRows from "./PaymentRows";
import PaymentDialogs from "./PaymentDialogs";
export default function PaymentsOverview() {
  const t = useTranslations("payments"),
    { recurringPayment: api } = useClients(),
    { currency, tenant } = useDisplayCurrency(),
    month = usePaymentMonth();
  const [action, setAction] = useState<{
    o: PaymentOccurrence;
    action: string;
  }>();
  const q = useQuery<GetPaymentOverviewResponse>({
    queryKey: ["payments", "overview", tenant, month, currency],
    enabled: !!tenant,
    queryFn: () =>
      api.getPaymentOverview({ month, targetCurrencyCode: currency }),
    refetchOnWindowFocus: true,
  });
  return (
    <section className="rounded-xl border border-border bg-card p-4 space-y-3">
      <div className="flex flex-wrap justify-between gap-2">
        <h2 className="text-base font-semibold">{t("remainingThisMonth")}</h2>
        <Link
          className="text-sm font-medium text-primary hover:underline"
          href={`/payments?month=${month}`}
        >
          {t("allPayments")}
        </Link>
      </div>
      {q.isError ? (
        <p role="alert">
          {t("loadError")}{" "}
          <button className="underline" onClick={() => q.refetch()}>
            {t("retry")}
          </button>
        </p>
      ) : q.isPending ? (
        <p>{t("loading")}</p>
      ) : (
        <>
          <PaymentSummary
            month={month}
            summary={q.data.summary}
            showPaid={false}
          />
          <PaymentRows
            items={q.data.occurrences}
            onAction={(o, action) => setAction({ o, action })}
          />
          {(q.data.previousSummary?.total ?? 0) > 0 && (
            <div className="border-t border-border pt-4 space-y-2">
              <h3 className="font-semibold">{t("previousDebts")}</h3>
              {q.data.previousDebts.map((o) => (
                <Link
                  className="block underline"
                  key={o.id}
                  href={`/payments?month=${o.dueDate.slice(0, 7)}&status=${o.status}`}
                >
                  {o.name} · {o.dueDate}
                </Link>
              ))}
              <Link className="underline" href="/payments?debts=1">
                {t("allDebts", { count: q.data.previousSummary?.total ?? 0 })}
              </Link>
            </div>
          )}
        </>
      )}
      {action && (
        <PaymentDialogs
          key={`${tenant}:${action.o.id}:${action.action}`}
          occurrence={action.o}
          action={action.action}
          onClose={() => setAction(undefined)}
        />
      )}
    </section>
  );
}
