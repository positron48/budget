"use client";
import { Suspense, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { Button, Protected } from "@/components";
import { useDisplayCurrency } from "@/lib/display-currency";
import {
  localPaymentMonth,
  nextPaymentMonth,
  useRefreshPayments,
} from "@/lib/payments";
import type {
  ListPaymentOccurrencesResponse,
  ListRecurringPaymentTemplatesResponse,
  PaymentOccurrence,
  RecurringPaymentTemplate,
} from "@/proto/budget/v1/recurring_payment_pb";
import PaymentSummary from "@/components/payments/PaymentSummary";
import PaymentRows from "@/components/payments/PaymentRows";
import PaymentDialogs from "@/components/payments/PaymentDialogs";
import TemplateDialog from "@/components/payments/TemplateDialog";
function Payments() {
  const params = useSearchParams(),
    t = useTranslations("payments"),
    { recurringPayment: api } = useClients(),
    { tenant, currency } = useDisplayCurrency(),
    refresh = useRefreshPayments();
  const [month, setMonth] = useState(
      params.get("month") || localPaymentMonth(),
    ),
    [status, setStatus] = useState(params.get("status") || ""),
    [debts, setDebts] = useState(params.get("debts") === "1"),
    [search, setSearch] = useState(""),
    [tab, setTab] = useState("occurrences"),
    [page, setPage] = useState(1),
    [action, setAction] = useState<{
      o: PaymentOccurrence;
      action: string;
      tenant: string;
    }>(),
    [editor, setEditor] = useState<{
      template?: RecurringPaymentTemplate;
      tenant: string;
    }>(),
    [archive, setArchive] = useState<RecurringPaymentTemplate>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const q = useQuery<ListPaymentOccurrencesResponse>({
    queryKey: [
      "payments",
      "list",
      tenant,
      month,
      currency,
      status,
      search,
      page,
      debts,
    ],
    enabled: !!tenant && tab === "occurrences",
    queryFn: () =>
      api.listPaymentOccurrences({
        fromDate: debts ? "0001-01-01" : `${month}-01`,
        toDate: debts
          ? `${localPaymentMonth()}-01`
          : `${nextPaymentMonth(month)}-01`,
        statuses: debts ? ["overdue", "needs_review"] : status ? [status] : [],
        search,
        page,
        pageSize: 30,
        targetCurrencyCode: currency,
      }),
  });
  const templates = useQuery<ListRecurringPaymentTemplatesResponse>({
    queryKey: ["payments", "templates", tenant],
    enabled: !!tenant && tab === "templates",
    queryFn: () => api.listRecurringPaymentTemplates({}),
  });
  return (
    <div className="container mx-auto max-w-5xl px-4 py-5 sm:px-8 space-y-4">
      <div className="flex flex-wrap justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="mt-1 hidden sm:block text-sm text-muted-foreground">
            {t("description")}
          </p>
        </div>
        <Button icon="plus" onClick={() => setEditor({ tenant: tenant || "" })}>
          {t("newTemplate")}
        </Button>
      </div>
      <nav
        aria-label={t("title")}
        className="inline-flex max-w-full gap-1 rounded-xl bg-secondary p-1"
      >
        {["occurrences", "templates"].map((v) => (
          <button
            aria-pressed={tab === v}
            className={`rounded-lg px-3 py-1.5 text-sm font-medium transition-colors ${tab === v ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
            key={v}
            onClick={() => setTab(v)}
          >
            {t(v)}
          </button>
        ))}
      </nav>
      {tab === "occurrences" ? (
        <>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-[auto_auto_minmax(120px,1fr)_auto] sm:items-center">
            <input
              aria-label={t("month")}
              className="h-9 min-w-0 rounded-lg border border-border bg-background px-3 text-sm"
              type="month"
              value={month}
              onChange={(e) => {
                setMonth(e.target.value);
                setDebts(false);
                setPage(1);
              }}
            />
            <select
              aria-label={t("status")}
              className="h-9 min-w-0 rounded-lg border border-border bg-background px-3 text-sm"
              value={status}
              onChange={(e) => {
                setStatus(e.target.value);
                setPage(1);
              }}
            >
              {[
                "",
                "planned",
                "paid",
                "overdue",
                "skipped",
                "needs_review",
              ].map((v) => (
                <option value={v} key={v}>
                  {t(v || "all")}
                </option>
              ))}
            </select>
            <input
              className="col-span-2 sm:col-span-1 h-9 min-w-0 rounded-lg border border-border bg-background px-3 text-sm"
              aria-label={t("search")}
              placeholder={t("search")}
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(1);
              }}
            />
            <label className="col-span-2 sm:col-span-1 flex items-center gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                checked={debts}
                onChange={(e) => {
                  setDebts(e.target.checked);
                  setPage(1);
                }}
              />{" "}
              {t("previousDebts")}
            </label>
          </div>
          {q.isError ? (
            <p role="alert">{t("loadError")}</p>
          ) : q.isPending ? (
            <p>{t("loading")}</p>
          ) : (
            <>
              <PaymentSummary
                month={debts ? t("previousDebts") : month}
                summary={q.data.summary}
              />
              <PaymentRows
                items={q.data.occurrences}
                onAction={(o, action) =>
                  setAction({ o, action, tenant: tenant || "" })
                }
              />
              {q.data.total > 30 && (
                <div className="flex items-center justify-center gap-4 border-t border-border pt-4">
                  <button
                    disabled={page === 1}
                    onClick={() => setPage(page - 1)}
                  >
                    {t("previous")}
                  </button>
                  <span>{page}</span>
                  <button
                    disabled={page * 30 >= q.data.total}
                    onClick={() => setPage(page + 1)}
                  >
                    {t("next")}
                  </button>
                </div>
              )}
            </>
          )}
        </>
      ) : templates.isError ? (
        <p role="alert">{t("loadError")}</p>
      ) : (
        <div className="space-y-3">
          {templates.isPending && (
            <p className="py-8 text-center text-muted-foreground">
              {t("loading")}
            </p>
          )}
          {templates.data?.templates.length === 0 && (
            <div className="rounded-xl border border-dashed border-border bg-card px-4 py-6 text-center">
              <h2 className="text-lg font-semibold">{t("emptyTemplates")}</h2>
              <p className="mx-auto mt-2 max-w-md text-sm text-muted-foreground">
                {t("emptyTemplatesHint")}
              </p>
              <Button
                className="mt-3"
                icon="plus"
                onClick={() => setEditor({ tenant: tenant || "" })}
              >
                {t("newTemplate")}
              </Button>
            </div>
          )}
          {templates.data?.templates.map((template) => (
            <article
              key={template.id}
              className="rounded-xl border border-border bg-card p-5 flex flex-wrap justify-between gap-3"
            >
              <div>
                <h2 className="font-semibold">
                  {template.name} {template.archived && `· ${t("archived")}`}
                </h2>
                <p className="mt-1 text-sm text-muted-foreground">
                  {t(`interval${template.intervalMonths}`)} ·{" "}
                  {template.currencyCode} · {template.timezone}
                </p>
              </div>
              {!template.archived && (
                <div className="flex gap-3">
                  <button
                    className="underline"
                    onClick={() =>
                      setEditor({ template, tenant: tenant || "" })
                    }
                  >
                    {t("edit")}
                  </button>
                  <button
                    className="underline"
                    onClick={() => setArchive(template)}
                  >
                    {t("archive")}
                  </button>
                </div>
              )}
            </article>
          ))}
        </div>
      )}
      {error && <p role="alert">{error}</p>}
      {archive && (
        <div className="rounded border border-border p-4 space-y-3">
          <p>{t("archiveExplanation", { name: archive.name })}</p>
          <button
            disabled={busy}
            className="btn btn-primary"
            onClick={async () => {
              setBusy(true);
              try {
                await api.archiveRecurringPaymentTemplate({
                  id: archive.id,
                  expectedVersion: archive.version,
                  requestKey: crypto.randomUUID(),
                });
                await refresh();
                setArchive(undefined);
              } catch {
                setError(t("saveError"));
              } finally {
                setBusy(false);
              }
            }}
          >
            {t("confirm")}
          </button>
          <button className="ml-3" onClick={() => setArchive(undefined)}>
            {t("cancel")}
          </button>
        </div>
      )}
      {action && action.tenant === tenant && (
        <PaymentDialogs
          key={`${tenant}:${action.o.id}:${action.action}`}
          occurrence={action.o}
          action={action.action}
          onClose={() => setAction(undefined)}
        />
      )}
      {editor && editor.tenant === tenant && (
        <TemplateDialog
          key={`${tenant}:${editor.template?.id || "new"}`}
          template={editor.template}
          onClose={() => setEditor(undefined)}
        />
      )}
    </div>
  );
}
export default function Page() {
  return (
    <Protected>
      <Suspense>
        <Payments />
      </Suspense>
    </Protected>
  );
}
