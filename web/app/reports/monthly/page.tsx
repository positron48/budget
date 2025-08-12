"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { TransactionType } from "@/proto/budget/v1/common_pb";
import { useTranslations } from "next-intl";
import LoadingSpinner from "@/components/LoadingSpinner";
import { Card, CardContent, CardHeader, CardTitle, CardDescription, DonutChart, Button, Icon, Select } from "@/components";
import { formatAmountWithSpaces } from "@/lib/utils";
import { useLocale } from "next-intl";

function MonthlyReportInner() {
  const { report } = useClients();
  const t = useTranslations("reports");
  const tc = useTranslations("common");
  const today = new Date();
  const [year, setYear] = useState<number>(today.getFullYear());
  const [month, setMonth] = useState<number>(today.getMonth() + 1); // 1..12
  const [currency, setCurrency] = useState<string>("");
  const req = useMemo(() => ({ year, month, targetCurrencyCode: currency } as any), [year, month, currency]);

  const locale = useLocale();
  const years = useMemo(() => {
    const current = today.getFullYear();
    const start = current - 5;
    return Array.from({ length: 11 }, (_, i) => start + i);
  }, [today]);
  const months = useMemo(() => {
    return Array.from({ length: 12 }, (_, i) => {
      const d = new Date(2000, i, 1);
      let label = d.toLocaleString(locale, { month: "long" });
      if ((locale || "").toLowerCase().startsWith("ru") && label) {
        label = label.charAt(0).toUpperCase() + label.slice(1);
      }
      return { value: i + 1, label };
    });
  }, [locale]);
  const currencySuggestions: string[] = [""]; // hidden for now

  const changeMonth = (delta: number) => {
    let m = month + delta;
    let y = year;
    if (m < 1) {
      m = 12;
      y -= 1;
    } else if (m > 12) {
      m = 1;
      y += 1;
    }
    setYear(y);
    setMonth(m);
  };
  const { data, isLoading, error } = useQuery({
    queryKey: ["monthly", req],
    queryFn: async () => (await report.getMonthlySummary(req)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const items = useMemo(() => (data?.items ?? []) as any[], [data]);
  const expenses = useMemo(() => items.filter((it) => it?.type === TransactionType.EXPENSE), [items]);
  const incomes = useMemo(() => items.filter((it) => it?.type === TransactionType.INCOME), [items]);
  const currencyCode = useMemo(() => data?.totalIncome?.currencyCode || data?.totalExpense?.currencyCode || "", [data]);
  const sumAmount = (arr: any[]) => arr.reduce((s, it) => s + Math.abs(Number(it?.total?.minorUnits ?? 0) / 100), 0);
  const totalExpenses = useMemo(() => sumAmount(expenses), [expenses]);
  const totalIncomes = useMemo(() => sumAmount(incomes), [incomes]);

  const coolPalette = ["#3b82f6", "#22c55e", "#06b6d4", "#6366f1", "#14b8a6", "#0ea5e9", "#10b981", "#60a5fa"];
  const warmPalette = ["#ef4444", "#f59e0b", "#f97316", "#e11d48", "#fb7185", "#f43f5e", "#ea580c", "#fbbf24"];
  
  return (
    <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8">
      {/* Header */}
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-foreground">{t("monthlyTitle")}</h1>
        <p className="text-muted-foreground mt-1">{t("description")}</p>
      </div>

      {/* Filters */}
      <Card className="mb-6 bg-white dark:bg-gray-900/40 border border-gray-200 dark:border-gray-800">
        <CardContent>
          <div className="grid grid-cols-1 gap-4">
            {/* Year + Month steppers side-by-side */}
            <div className="flex flex-wrap items-end gap-6">
              {/* Year */}
              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">{t("year")}</label>
                <div className="flex items-center gap-2">
                  <Button variant="outline" size="md" onClick={() => setYear((y) => (years.includes(y - 1) ? y - 1 : y - 1))}>
                    <Icon name="chevron-left" />
                  </Button>
                  <Select
                    className="w-32"
                    value={year}
                    onChange={(v) => setYear(Number(v))}
                    options={years.map((y) => ({ value: y, label: String(y) }))}
                  />
                  <Button variant="outline" size="md" onClick={() => setYear((y) => (years.includes(y + 1) ? y + 1 : y + 1))}>
                    <Icon name="chevron-right" />
                  </Button>
                </div>
              </div>

              {/* Month */}
              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">{t("month")}</label>
                <div className="flex items-center gap-2">
                  <Button variant="outline" size="md" onClick={() => changeMonth(-1)}>
                    <Icon name="chevron-left" />
                  </Button>
                  <Select
                    className="w-44"
                    value={month}
                    onChange={(v) => setMonth(Number(v))}
                    options={months.map((m) => ({ value: m.value, label: m.label }))}
                  />
                  <Button variant="outline" size="md" onClick={() => changeMonth(1)}>
                    <Icon name="chevron-right" />
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Content */}
      {isLoading && (
        <LoadingSpinner text={tc("loading")} className="py-12" />
      )}

      {error && (
        <Card className="border-destructive/20 bg-destructive/5">
          <CardContent>
            <p className="text-destructive">{(error as any).message}</p>
          </CardContent>
        </Card>
      )}

      {!isLoading && !error && data && (
        <div className="space-y-6">
          {/* Summary */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Card>
              <CardContent className="text-center pt-6">
                <div className="text-2xl font-bold text-green-600">
                  {formatAmountWithSpaces(Number(data?.totalIncome?.minorUnits ?? 0) / 100)} {data?.totalIncome?.currencyCode ?? ""}
                </div>
                <div className="text-sm text-muted-foreground">{t("totalIncome")}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="text-center pt-6">
                <div className="text-2xl font-bold text-red-600">
                  {formatAmountWithSpaces(Number(data?.totalExpense?.minorUnits ?? 0) / 100)} {data?.totalExpense?.currencyCode ?? ""}
                </div>
                <div className="text-sm text-muted-foreground">{t("totalExpense")}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="text-center pt-6">
                <div
                  className={`text-2xl font-bold ${
                    (Number(data?.totalIncome?.minorUnits ?? 0) - Number(data?.totalExpense?.minorUnits ?? 0)) >= 0
                      ? "text-green-600"
                      : "text-red-600"
                  }`}
                >
                  {formatAmountWithSpaces((Number(data?.totalIncome?.minorUnits ?? 0) - Number(data?.totalExpense?.minorUnits ?? 0)) / 100)} {data?.totalIncome?.currencyCode ?? ""}
                </div>
                <div className="text-sm text-muted-foreground">{t("netIncome")}</div>
              </CardContent>
            </Card>
          </div>

          {/* Charts */}
          {(expenses.length === 0 && incomes.length === 0) ? (
            <Card>
              <CardContent>
                <div className="text-sm text-muted-foreground">{t("noData")}</div>
              </CardContent>
            </Card>
          ) : (
            <>
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                {expenses.length > 0 && (
                  <Card>
                    <CardHeader>
                      <CardTitle className="text-base">{`${t("categoryBreakdown")} — ${t("expense")}`}</CardTitle>
                      <CardDescription className="text-sm">{t("description")}</CardDescription>
                    </CardHeader>
                    <CardContent>
                    <DonutChart
                        data={expenses.map((it: any, idx: number) => ({
                          label: `${it?.categoryName ?? it?.categoryId}`,
                          value: Math.abs(Number(it?.total?.minorUnits ?? 0) / 100),
                          color: warmPalette[idx % warmPalette.length],
                      }))}
                      centerLabel={currencyCode}
                      centerSubLabel={totalExpenses ? formatAmountWithSpaces(totalExpenses) : ""}
                        className="flex flex-col items-center"
                      valueFormatter={(v) => formatAmountWithSpaces(v)}
                      />
                    </CardContent>
                  </Card>
                )}

                {incomes.length > 0 && (
                  <Card>
                    <CardHeader>
                      <CardTitle className="text-base">{`${t("categoryBreakdown")} — ${t("income")}`}</CardTitle>
                      <CardDescription className="text-sm">{t("description")}</CardDescription>
                    </CardHeader>
                    <CardContent>
                    <DonutChart
                        data={incomes.map((it: any, idx: number) => ({
                          label: `${it?.categoryName ?? it?.categoryId}`,
                          value: Math.abs(Number(it?.total?.minorUnits ?? 0) / 100),
                          color: coolPalette[idx % coolPalette.length],
                      }))}
                      centerLabel={currencyCode}
                      centerSubLabel={totalIncomes ? formatAmountWithSpaces(totalIncomes) : ""}
                        className="flex flex-col items-center"
                      valueFormatter={(v) => formatAmountWithSpaces(v)}
                      />
                    </CardContent>
                  </Card>
                )}
              </div>

              
            </>
          )}

          {/* Details Table */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{t("categoryBreakdown")}</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full">
                <thead>
                  <tr className="border-b">
                    <th className="text-left py-2 font-medium">{t("category")}</th>
                    <th className="text-left py-2 font-medium">{t("type")}</th>
                    <th className="text-right py-2 font-medium">{t("total")}</th>
                  </tr>
                </thead>
                <tbody>
                  {(items).map((it: any, i: number) => (
                    <tr key={i} className="border-b">
                      <td className="py-2">{it?.categoryName ?? it?.categoryId}</td>
                      <td className="py-2">
                        {it?.type === TransactionType.EXPENSE ? t("expense") : it?.type === TransactionType.INCOME ? t("income") : ""}
                      </td>
                      <td className="py-2 text-right">
                        {formatAmountWithSpaces(Number(it?.total?.minorUnits ?? 0) / 100)} {it?.total?.currencyCode ?? ""}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardContent>
          </Card>
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

