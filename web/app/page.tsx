"use client";

import Link from "next/link";
import { useMemo, useRef, useState } from "react";
import { useTranslations, useLocale } from "next-intl";
import { useQuery } from "@tanstack/react-query";
import { ClientsProvider, useClients } from "@/app/providers";
import {
  Icon,
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  StatCard,
  DonutChart,
  Button,
  Modal,
  LoadingSpinner,
} from "@/components";
import type { IconName } from "@/components/Icon";
import { TransactionType } from "@/proto/budget/v1/common_pb";
import { formatCurrency, formatAmountWithSpaces } from "@/lib/utils";
import { chartPalettes } from "@/lib/theme/colors";
import NewTransactionForm, { NewTxFormRef } from "./transactions/NewTransactionForm";

function DashboardInner() {
  const { report, transaction } = useClients();
  const t = useTranslations("home");
  const tt = useTranslations("transactions");
  const tc = useTranslations("common");
  const locale = useLocale();
  const formRef = useRef<NewTxFormRef>(null);
  const [showCreate, setShowCreate] = useState(false);

  const now = useMemo(() => new Date(), []);
  const monthReq = useMemo(
    () => ({
      year: now.getFullYear(),
      month: now.getMonth() + 1,
      targetCurrencyCode: "",
      timezoneOffsetMinutes: now.getTimezoneOffset(),
      excludeExtraordinary: false,
    }),
    [now]
  );

  const { data: monthly, isLoading: monthlyLoading, refetch: refetchMonthly } = useQuery({
    queryKey: ["dashboard-monthly", monthReq],
    queryFn: async () => (await report.getMonthlySummary(monthReq as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const recentReq = useMemo(
    () => ({ page: { page: 1, pageSize: 5, sort: "occurred_at desc" } }),
    []
  );
  const { data: recent, isLoading: recentLoading, refetch: refetchRecent } = useQuery({
    queryKey: ["dashboard-recent", recentReq],
    queryFn: async () => (await transaction.listTransactions(recentReq as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const items = useMemo(() => (monthly?.items ?? []) as any[], [monthly]);
  const currencyCode = useMemo(
    () => monthly?.totalIncome?.currencyCode || monthly?.totalExpense?.currencyCode || "RUB",
    [monthly]
  );
  const sum = (arr: any[]) =>
    arr.reduce((s, it) => s + Math.abs(Number(it?.total?.minorUnits ?? 0) / 100), 0);
  const expensesItems = useMemo(
    () => items.filter((it) => it?.type === TransactionType.EXPENSE),
    [items]
  );
  const totalIncome = useMemo(
    () => sum(items.filter((it) => it?.type === TransactionType.INCOME)),
    [items]
  );
  const totalExpenses = useMemo(() => sum(expensesItems), [expensesItems]);
  const net = totalIncome - totalExpenses;

  const warm = chartPalettes.warm;
  const donutData = useMemo(
    () =>
      [...expensesItems]
        .sort(
          (a, b) =>
            Math.abs(Number(b?.total?.minorUnits ?? 0)) -
            Math.abs(Number(a?.total?.minorUnits ?? 0))
        )
        .slice(0, 8)
        .map((it, idx) => ({
          label: `${it?.categoryName ?? it?.categoryId}`,
          value: Math.abs(Number(it?.total?.minorUnits ?? 0) / 100),
          color: warm[idx % warm.length],
        })),
    [expensesItems, warm]
  );

  const recentItems = useMemo(() => (recent?.items ?? []) as any[], [recent]);

  const sections: { title: string; href: string; icon: IconName; color: string }[] = [
    { title: t("transactions.title"), href: "/transactions", icon: "transactions", color: "bg-[hsl(var(--info))]" },
    { title: t("reports.title"), href: "/reports", icon: "reports", color: "bg-[hsl(var(--chart-mixed-6))]" },
    { title: t("categories.title"), href: "/categories", icon: "categories", color: "bg-[hsl(var(--primary))]" },
    { title: t("tenants.title"), href: "/account", icon: "tenants", color: "bg-[hsl(var(--chart-cool-4))]" },
  ];

  const onSaved = () => {
    refetchMonthly();
    refetchRecent();
  };

  return (
    <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8 space-y-8">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-foreground">{t("welcome")}</h1>
          <p className="text-muted-foreground mt-1">{t("thisMonth")}</p>
        </div>
        <Button icon="plus" onClick={() => setShowCreate(true)}>
          {t("addTransaction")}
        </Button>
      </div>

      {/* Metrics */}
      {monthlyLoading ? (
        <LoadingSpinner text={tc("loading")} className="py-8" />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <StatCard icon="trending-up" color="success" title={t("income")} value={formatCurrency(totalIncome, currencyCode)} />
          <StatCard icon="trending-down" color="danger" title={t("expenses")} value={formatCurrency(totalExpenses, currencyCode)} />
          <StatCard icon="wallet" color={net >= 0 ? "primary" : "warning"} title={t("net")} value={formatCurrency(net, currencyCode)} />
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Expense breakdown */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("expenseBreakdown")}</CardTitle>
            <CardDescription className="text-sm">{t("thisMonth")}</CardDescription>
          </CardHeader>
          <CardContent>
            {donutData.length > 0 ? (
              <DonutChart
                data={donutData}
                className="flex flex-col items-center"
                valueFormatter={(v) => formatAmountWithSpaces(v)}
              />
            ) : (
              <p className="text-sm text-muted-foreground py-8 text-center">{t("noExpenses")}</p>
            )}
          </CardContent>
        </Card>

        {/* Recent transactions */}
        <Card>
          <div className="flex items-center justify-between p-6">
            <CardTitle className="text-base">{t("recentTransactions")}</CardTitle>
            <Link href="/transactions" className="text-sm text-primary hover:underline">
              {t("viewAll")}
            </Link>
          </div>
          <CardContent>
            {recentLoading ? (
              <LoadingSpinner text={tc("loading")} className="py-6" />
            ) : recentItems.length === 0 ? (
              <p className="text-sm text-muted-foreground py-8 text-center">{t("noRecent")}</p>
            ) : (
              <ul className="divide-y divide-border/60">
                {recentItems.map((tx: any) => {
                  const isExpense = tx?.type === TransactionType.EXPENSE;
                  const amount = Math.abs(Number(tx?.amount?.minorUnits ?? 0) / 100);
                  return (
                    <li key={tx?.id} className="flex items-center justify-between gap-3 py-2.5">
                      <div className="min-w-0 flex items-center gap-2">
                        <span
                          className={`inline-flex items-center justify-center w-7 h-7 rounded-full shrink-0 ${
                            isExpense
                              ? "bg-[hsl(var(--negative)/0.15)] text-[hsl(var(--negative))]"
                              : "bg-[hsl(var(--positive)/0.15)] text-[hsl(var(--positive))]"
                          }`}
                        >
                          <Icon name={isExpense ? "trending-down" : "trending-up"} size={14} />
                        </span>
                        <div className="min-w-0">
                          <p className="text-sm text-foreground truncate">
                            {tx?.comment || tt("noComment")}
                          </p>
                          <p className="text-xs text-muted-foreground">
                            {tx?.occurredAt?.seconds
                              ? new Date(Number(tx.occurredAt.seconds) * 1000).toLocaleDateString(locale)
                              : ""}
                          </p>
                        </div>
                      </div>
                      <span
                        className={`text-sm font-medium shrink-0 ${
                          isExpense ? "text-[hsl(var(--negative))]" : "text-[hsl(var(--positive))]"
                        }`}
                      >
                        {isExpense ? "−" : "+"}
                        {formatCurrency(amount, tx?.amount?.currencyCode || currencyCode)}
                      </span>
                    </li>
                  );
                })}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Sections */}
      <div>
        <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">
          {t("sections")}
        </h2>
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          {sections.map((s) => (
            <Link key={s.href} href={s.href} className="group block">
              <Card hover className="transition-all duration-200 group-hover:shadow-md">
                <CardContent className="flex items-center gap-3 py-4">
                  <div className={`w-10 h-10 rounded-lg ${s.color} flex items-center justify-center text-white`}>
                    <Icon name={s.icon} size={20} />
                  </div>
                  <span className="font-medium text-foreground">{s.title}</span>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      </div>

      <Modal
        open={showCreate}
        title={tt("newTransaction") as string}
        onClose={() => setShowCreate(false)}
        maxWidthClass="max-w-xl"
        footer={
          <>
            <Button variant="outline" onClick={() => setShowCreate(false)}>{tc("cancel")}</Button>
            <Button variant="primary" onClick={() => formRef.current?.submit()}>{tc("save")}</Button>
            <Button variant="secondary" onClick={() => formRef.current?.submitAndAddMore()}>{tt("saveAndAddMore")}</Button>
          </>
        }
      >
        <NewTransactionForm
          ref={formRef}
          onClose={() => setShowCreate(false)}
          onSaved={onSaved}
        />
      </Modal>
    </div>
  );
}

export default function HomePage() {
  return (
    <ClientsProvider>
      <DashboardInner />
    </ClientsProvider>
  );
}
