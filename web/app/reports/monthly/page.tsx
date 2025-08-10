"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { TransactionType } from "@/proto/budget/v1/common_pb";
import { useTranslations } from "next-intl";

function MonthlyReportInner() {
  const { report } = useClients();
  const t = useTranslations("reports");
  const tc = useTranslations("common");
  const today = new Date();
  const [year, setYear] = useState<number>(today.getFullYear());
  const [month, setMonth] = useState<number>(today.getMonth() + 1); // 1..12
  const [currency, setCurrency] = useState<string>("");
  const req = useMemo(() => ({ year, month, targetCurrencyCode: currency } as any), [year, month, currency]);
  const { data, isLoading, error } = useQuery({
    queryKey: ["monthly", req],
    queryFn: async () => (await report.getMonthlySummary(req)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold mb-2">{t("monthlyTitle")}</h1>
      <div className="flex gap-3 items-end mb-3">
        <div>
          <label className="block text-xs">{t("year")}</label>
          <input className="border rounded px-2 py-1 w-24" type="number" value={year} onChange={(e) => setYear(Number(e.target.value))} />
        </div>
        <div>
          <label className="block text-xs">{t("month")}</label>
          <input className="border rounded px-2 py-1 w-20" type="number" min={1} max={12} value={month} onChange={(e) => setMonth(Number(e.target.value))} />
        </div>
        <div>
          <label className="block text-xs">{t("currency")}</label>
          <input className="border rounded px-2 py-1 w-28" value={currency} onChange={(e) => setCurrency(e.target.value)} />
        </div>
      </div>
      {isLoading && <div className="text-sm text-gray-500">{tc("loading")}</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}
      <div className="space-y-3">
        <div className="text-sm">{t("totalIncome")}: {(data?.totalIncome?.minorUnits ?? 0)} {data?.totalIncome?.currencyCode ?? ""}</div>
        <div className="text-sm">{t("totalExpense")}: {(data?.totalExpense?.minorUnits ?? 0)} {data?.totalExpense?.currencyCode ?? ""}</div>
        <table className="text-sm w-full border-collapse">
          <thead>
            <tr className="border-b">
              <th className="text-left py-1">{t("category")}</th>
              <th className="text-left py-1">{t("type")}</th>
              <th className="text-right py-1">{t("total")}</th>
            </tr>
          </thead>
          <tbody>
            {(data?.items ?? []).map((it: any, i: number) => (
              <tr key={i} className="border-b">
                <td className="py-1">{it?.categoryName ?? it?.categoryId}</td>
                <td className="py-1">{it?.type === TransactionType.EXPENSE ? "Expense" : it?.type === TransactionType.INCOME ? "Income" : ""}</td>
                <td className="py-1 text-right">{it?.total?.minorUnits ?? 0} {it?.total?.currencyCode ?? ""}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default function MonthlyReportPage() {
  return (
    <ClientsProvider>
      <MonthlyReportInner />
    </ClientsProvider>
  );
}

