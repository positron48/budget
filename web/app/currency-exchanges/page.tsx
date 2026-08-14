"use client";

import { FormEvent, useMemo, useState } from "react";
import { useLocale, useTranslations } from "next-intl";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClientsProvider, useClients } from "@/app/providers";
import { Button, Card, CardContent, ConfirmDialog, Icon, Modal } from "@/components";
import { formatCurrency } from "@/lib/utils";

const CURRENCIES = ["RUB", "USD", "EUR", "GBP", "KZT", "CNY", "TRY", "GEL", "AMD", "RSD"];
const QUICK_CURRENCIES = ["RUB", "USD", "EUR"];

interface CurrencyPickerProps {
  value: string;
  onChange: (currency: string) => void;
  quickLabel: string;
  allLabel: string;
}

function CurrencyPicker({ value, onChange, quickLabel, allLabel }: CurrencyPickerProps) {
  return (
    <div className="space-y-2">
      <span className="block text-xs font-medium text-muted-foreground">{quickLabel}</span>
      <div className="grid grid-cols-3 gap-1.5">
        {QUICK_CURRENCIES.map((currency) => (
          <button
            key={currency}
            type="button"
            aria-pressed={value === currency}
            onClick={() => onChange(currency)}
            className={`h-9 rounded-md border px-2 text-xs font-semibold transition-colors ${
              value === currency
                ? "border-[hsl(var(--primary))] bg-[hsl(var(--primary))] text-[hsl(var(--primary-foreground))]"
                : "border-border bg-[hsl(var(--secondary))] text-foreground hover:border-[hsl(var(--primary))] hover:bg-[hsl(var(--accent))]"
            }`}
          >
            {currency}
          </button>
        ))}
      </div>
      <label className="block text-xs font-medium text-muted-foreground">{allLabel}</label>
      <select className="input w-full" value={value} onChange={(event) => onChange(event.target.value)}>
        {CURRENCIES.map((currency) => <option key={currency} value={currency}>{currency}</option>)}
      </select>
    </div>
  );
}

function localDateTimeValue(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function formatRate(value: string, locale: string) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return value;
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 8 }).format(numeric);
}

function CurrencyExchangesInner() {
  const { currencyExchange } = useClients();
  const t = useTranslations("currencyExchanges");
  const tc = useTranslations("common");
  const locale = useLocale();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [showCreate, setShowCreate] = useState(false);
  const [deleteID, setDeleteID] = useState<string | null>(null);
  const [fromAmount, setFromAmount] = useState("");
  const [fromCurrency, setFromCurrency] = useState("RUB");
  const [toAmount, setToAmount] = useState("");
  const [toCurrency, setToCurrency] = useState("EUR");
  const [occurredAt, setOccurredAt] = useState(() => localDateTimeValue(new Date()));
  const [note, setNote] = useState("");
  const [formError, setFormError] = useState("");
  const pageSize = 20;

  const request = useMemo(() => ({ page: { page, pageSize } }), [page]);
  const { data, isLoading, error } = useQuery({
    queryKey: ["currency-exchanges", request],
    queryFn: async () => await currencyExchange.listCurrencyExchanges(request as any),
    retry: false,
    refetchOnWindowFocus: false,
  });

  const resetForm = () => {
    setFromAmount("");
    setFromCurrency("RUB");
    setToAmount("");
    setToCurrency("EUR");
    setNote("");
    setOccurredAt(localDateTimeValue(new Date()));
    setFormError("");
  };

  const createMutation = useMutation({
    mutationFn: async () => {
      const fromValue = Number(fromAmount.replace(",", "."));
      const toValue = Number(toAmount.replace(",", "."));
      const fromMinorUnits = Math.round(fromValue * 100);
      const toMinorUnits = Math.round(toValue * 100);
      if (fromCurrency === toCurrency) throw new Error(t("sameCurrency"));
      if (!Number.isFinite(fromValue) || !Number.isFinite(toValue) || fromMinorUnits <= 0 || toMinorUnits <= 0) {
        throw new Error(t("positiveAmount"));
      }
      return currencyExchange.createCurrencyExchange({
        fromAmount: { currencyCode: fromCurrency, minorUnits: fromMinorUnits },
        toAmount: { currencyCode: toCurrency, minorUnits: toMinorUnits },
        occurredAt: { seconds: Math.floor(new Date(occurredAt).getTime() / 1000) },
        note: note.trim(),
      } as any);
    },
    onSuccess: async () => {
      setPage(1);
      await queryClient.invalidateQueries({ queryKey: ["currency-exchanges"] });
      resetForm();
      setShowCreate(false);
    },
    onError: (mutationError) => setFormError((mutationError as Error).message),
  });

  const deleteMutation = useMutation({
    mutationFn: async (id: string) => currencyExchange.deleteCurrencyExchange({ id } as any),
    onSuccess: async () => {
      setDeleteID(null);
      await queryClient.invalidateQueries({ queryKey: ["currency-exchanges"] });
    },
  });

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    setFormError("");
    createMutation.mutate();
  };

  const exchanges = data?.exchanges ?? [];
  const totalItems = Number(data?.page?.totalItems ?? 0);
  const totalPages = Math.max(1, Number(data?.page?.totalPages ?? 1));
  const inputClass = "input w-full";

  return (
    <div className="min-h-screen bg-background">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h1 className="text-3xl font-bold text-foreground">{t("title")}</h1>
            <p className="mt-1 text-muted-foreground">{t("description")}</p>
          </div>
          <Button icon="plus" onClick={() => setShowCreate(true)}>{t("create")}</Button>
        </div>

        {isLoading && (
          <div className="flex justify-center py-16">
            <Icon name="loader-2" className="animate-spin text-primary" size={28} />
          </div>
        )}

        {error && (
          <Card className="border-destructive/40 bg-destructive/5">
            <CardContent className="pt-4 text-destructive">{(error as Error).message}</CardContent>
          </Card>
        )}

        {!isLoading && !error && exchanges.length === 0 && (
          <Card>
            <CardContent className="py-14 text-center">
              <Icon name="fx" size={32} className="mx-auto mb-3 text-muted-foreground" />
              <h2 className="text-lg font-semibold">{t("empty")}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{t("emptyDescription")}</p>
            </CardContent>
          </Card>
        )}

        {!isLoading && !error && exchanges.length > 0 && (
          <div className="space-y-3">
            {exchanges.map((exchange: any) => {
              const date = exchange.occurredAt?.seconds
                ? new Date(Number(exchange.occurredAt.seconds) * 1000)
                : null;
              return (
                <Card key={exchange.id}>
                  <CardContent className="py-4">
                    <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                      <div className="min-w-0 space-y-2">
                        <div className="flex flex-wrap items-center gap-2 text-lg font-semibold">
                          <span>{formatCurrency(Number(exchange.fromAmount?.minorUnits ?? 0), exchange.fromAmount?.currencyCode)}</span>
                          <Icon name="arrow-right" size={18} className="text-muted-foreground" />
                          <span>{formatCurrency(Number(exchange.toAmount?.minorUnits ?? 0), exchange.toAmount?.currencyCode)}</span>
                        </div>
                        <div className="inline-flex rounded-md bg-primary/10 px-2.5 py-1 text-sm font-medium text-primary">
                          {t("rateFormat", {
                            from: exchange.fromAmount?.currencyCode,
                            rate: formatRate(exchange.rateDecimal, locale),
                            to: exchange.toAmount?.currencyCode,
                          })}
                        </div>
                        {exchange.note && <p className="text-sm text-foreground">{exchange.note}</p>}
                        {date && (
                          <p className="text-xs text-muted-foreground">
                            {new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date)}
                          </p>
                        )}
                      </div>
                      <Button variant="ghost" size="sm" icon="trash" onClick={() => setDeleteID(exchange.id)}>
                        {tc("delete")}
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              );
            })}

            <div className="flex items-center justify-between pt-2">
              <span className="text-sm text-muted-foreground">{t("showing", { count: exchanges.length, total: totalItems })}</span>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>{tc("prev")}</Button>
                <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage((value) => value + 1)}>{tc("next")}</Button>
              </div>
            </div>
          </div>
        )}
      </div>

      <Modal
        open={showCreate}
        title={t("newExchange")}
        onClose={() => { setShowCreate(false); setFormError(""); }}
        maxWidthClass="max-w-xl"
        footer={(
          <>
            <Button variant="outline" onClick={() => setShowCreate(false)} disabled={createMutation.isPending}>{tc("cancel")}</Button>
            <Button type="submit" form="currency-exchange-form" loading={createMutation.isPending}>{tc("save")}</Button>
          </>
        )}
      >
        <form id="currency-exchange-form" onSubmit={onSubmit} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <fieldset className="space-y-2 rounded-lg border border-border p-3">
              <legend className="px-1 text-sm font-semibold">{t("from")}</legend>
              <label className="block text-xs font-medium text-muted-foreground">{t("amount")}</label>
              <input className={inputClass} inputMode="decimal" required value={fromAmount} onChange={(event) => setFromAmount(event.target.value)} placeholder="100.00" />
              <CurrencyPicker
                value={fromCurrency}
                onChange={setFromCurrency}
                quickLabel={t("quickCurrencies")}
                allLabel={t("allCurrencies")}
              />
            </fieldset>
            <fieldset className="space-y-2 rounded-lg border border-border p-3">
              <legend className="px-1 text-sm font-semibold">{t("to")}</legend>
              <label className="block text-xs font-medium text-muted-foreground">{t("amount")}</label>
              <input className={inputClass} inputMode="decimal" required value={toAmount} onChange={(event) => setToAmount(event.target.value)} placeholder="9500.00" />
              <CurrencyPicker
                value={toCurrency}
                onChange={setToCurrency}
                quickLabel={t("quickCurrencies")}
                allLabel={t("allCurrencies")}
              />
            </fieldset>
          </div>
          <div className="space-y-2">
            <label className="block text-sm font-medium">{t("date")}</label>
            <input className={inputClass} type="datetime-local" required value={occurredAt} onChange={(event) => setOccurredAt(event.target.value)} />
          </div>
          <div className="space-y-2">
            <label className="block text-sm font-medium">{t("note")}</label>
            <textarea className={`${inputClass} min-h-20 resize-y`} value={note} onChange={(event) => setNote(event.target.value)} placeholder={t("notePlaceholder")} />
          </div>
          {formError && <p className="text-sm text-destructive">{formError}</p>}
        </form>
      </Modal>

      <ConfirmDialog
        open={deleteID !== null}
        message={t("confirmDelete")}
        loading={deleteMutation.isPending}
        onCancel={() => setDeleteID(null)}
        onConfirm={() => deleteID && deleteMutation.mutate(deleteID)}
      />
    </div>
  );
}

export default function CurrencyExchangesPage() {
  return <ClientsProvider><CurrencyExchangesInner /></ClientsProvider>;
}
