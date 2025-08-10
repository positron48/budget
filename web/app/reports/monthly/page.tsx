"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { TransactionType } from "@/proto/budget/v1/common_pb";
import { useTranslations } from "next-intl";
import LoadingSpinner from "@/components/LoadingSpinner";

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
    <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8">
      {/* Header */}
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-foreground">{t("monthlyTitle")}</h1>
        <p className="text-muted-foreground mt-1">{t("description")}</p>
      </div>

      {/* Filters */}
      <div className="card mb-6">
        <div className="card-content">
          <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-end">
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">{t("year")}</label>
              <input 
                className="input w-24" 
                type="number" 
                value={year} 
                onChange={(e) => setYear(Number(e.target.value))} 
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">{t("month")}</label>
              <input 
                className="input w-20" 
                type="number" 
                min={1} 
                max={12} 
                value={month} 
                onChange={(e) => setMonth(Number(e.target.value))} 
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">{t("currency")}</label>
              <input 
                className="input w-28" 
                value={currency} 
                onChange={(e) => setCurrency(e.target.value)}
                placeholder="RUB"
              />
            </div>
          </div>
        </div>
      </div>

      {/* Content */}
      {isLoading && (
        <LoadingSpinner text={tc("loading")} className="py-12" />
      )}

      {error && (
        <div className="card border-destructive/20 bg-destructive/5">
          <div className="card-content">
            <p className="text-destructive">{(error as any).message}</p>
          </div>
        </div>
      )}

      {!isLoading && !error && data && (
        <div className="space-y-6">
          {/* Summary */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="card">
              <div className="card-content text-center">
                <div className="text-2xl font-bold text-green-600">
                  {(Number(data?.totalIncome?.minorUnits ?? 0) / 100).toFixed(2)} {data?.totalIncome?.currencyCode ?? ""}
                </div>
                <div className="text-sm text-muted-foreground">{t("totalIncome")}</div>
              </div>
            </div>
            <div className="card">
              <div className="card-content text-center">
                <div className="text-2xl font-bold text-red-600">
                  {(Number(data?.totalExpense?.minorUnits ?? 0) / 100).toFixed(2)} {data?.totalExpense?.currencyCode ?? ""}
                </div>
                <div className="text-sm text-muted-foreground">{t("totalExpense")}</div>
              </div>
            </div>
            <div className="card">
              <div className="card-content text-center">
                <div className={`text-2xl font-bold ${
                  (Number(data?.totalIncome?.minorUnits ?? 0) - Number(data?.totalExpense?.minorUnits ?? 0)) >= 0 
                    ? 'text-green-600' 
                    : 'text-red-600'
                }`}>
                  {((Number(data?.totalIncome?.minorUnits ?? 0) - Number(data?.totalExpense?.minorUnits ?? 0)) / 100).toFixed(2)} {data?.totalIncome?.currencyCode ?? ""}
                </div>
                <div className="text-sm text-muted-foreground">{t("netIncome")}</div>
              </div>
            </div>
          </div>

          {/* Details Table */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">{t("categoryBreakdown")}</h3>
            </div>
            <div className="card-content">
              <table className="w-full">
                <thead>
                  <tr className="border-b">
                    <th className="text-left py-2 font-medium">{t("category")}</th>
                    <th className="text-left py-2 font-medium">{t("type")}</th>
                    <th className="text-right py-2 font-medium">{t("total")}</th>
                  </tr>
                </thead>
                <tbody>
                  {(data?.items ?? []).map((it: any, i: number) => (
                    <tr key={i} className="border-b">
                      <td className="py-2">{it?.categoryName ?? it?.categoryId}</td>
                      <td className="py-2">
                        {it?.type === TransactionType.EXPENSE ? t("expense") : it?.type === TransactionType.INCOME ? t("income") : ""}
                      </td>
                      <td className="py-2 text-right">
                        {(Number(it?.total?.minorUnits ?? 0) / 100).toFixed(2)} {it?.total?.currencyCode ?? ""}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
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

