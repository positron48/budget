"use client";

import { FormEvent, useMemo, useState, useRef } from "react";
import { useLocale, useTranslations } from "next-intl";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClientsProvider, useClients } from "@/app/providers";
import { Button, Card, CardContent, ConfirmDialog, Icon, Modal } from "@/components";
import AssetAccountSelect from "@/components/assets/AssetAccountSelect";
import { assetAmountInput, parseAssetAmount, assetMoney, localAssetTime } from "@/lib/assets";

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


function formatRate(value: string, locale: string) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return value;
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 8 }).format(numeric);
}

function displayRate(exchange: any) {
  const fromAmount = Math.abs(Number(exchange.fromAmount?.minorUnits ?? 0));
  const toAmount = Math.abs(Number(exchange.toAmount?.minorUnits ?? 0));
  const fromCurrency = exchange.fromAmount?.currencyCode ?? "";
  const toCurrency = exchange.toAmount?.currencyCode ?? "";

  if (fromAmount <= 0 || toAmount <= 0) {
    return { from: fromCurrency, to: toCurrency, rate: exchange.rateDecimal };
  }

  if (fromAmount >= toAmount) {
    return { from: toCurrency, to: fromCurrency, rate: String(fromAmount / toAmount) };
  }

  return { from: fromCurrency, to: toCurrency, rate: String(toAmount / fromAmount) };
}

function CurrencyExchangesInner() {
  const { currencyExchange } = useClients();
  const t = useTranslations("currencyExchanges");
  const tc = useTranslations("common");
  const locale = useLocale();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [showCreate, setShowCreate] = useState(false);
  const [editingID, setEditingID] = useState<string | null>(null);
  const [deleteID, setDeleteID] = useState<string | null>(null);
  const [fromAmount, setFromAmount] = useState("");
  const [fromCurrency, setFromCurrency] = useState("RUB");
  const [toAmount, setToAmount] = useState("");
  const [toCurrency, setToCurrency] = useState("EUR");
  const [occurredAt, setOccurredAt] = useState(() => localAssetTime(new Date()));
  const [note, setNote] = useState("");
  const [formError, setFormError] = useState("");
  const [fromAccount, setFromAccount] = useState("");
  const [toAccount, setToAccount] = useState("");
  const requestKey = useRef(crypto.randomUUID());
  const ta = useTranslations("assets");
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
    setFromAccount(""); setToAccount(""); requestKey.current = crypto.randomUUID();
    setFromCurrency("RUB");
    setToAmount("");
    setToCurrency("EUR");
    setNote("");
    setOccurredAt(localAssetTime(new Date()));
    setFormError("");
  };

  const closeForm = () => {
    setShowCreate(false);
    setEditingID(null);
    setFormError("");
  };

  const openCreate = () => {
    resetForm();
    setEditingID(null);
    setShowCreate(true);
  };

  const openEdit = (exchange: any) => {
    setFromAmount(assetAmountInput(exchange.fromAmount?.minorUnits ?? 0n));
    setFromCurrency(exchange.fromAmount?.currencyCode ?? "RUB");
    setToAmount(assetAmountInput(exchange.toAmount?.minorUnits ?? 0n));
    setToCurrency(exchange.toAmount?.currencyCode ?? "EUR");
    setOccurredAt(exchange.occurredAt?.seconds
      ? localAssetTime(new Date(Number(exchange.occurredAt.seconds) * 1000))
      : localAssetTime(new Date()));
    setNote(exchange.note ?? "");
    setFromAccount(exchange.fromAssetAccountId || ""); setToAccount(exchange.toAssetAccountId || "");
    setFormError("");
    setEditingID(exchange.id);
    setShowCreate(true);
  };

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (fromCurrency === toCurrency) throw new Error(t("sameCurrency"));
      if (!!fromAccount !== !!toAccount) throw new Error(ta("errors.exchangeAccounts"));
      let fromMinorUnits: bigint, toMinorUnits: bigint;
      try { fromMinorUnits = parseAssetAmount(fromAmount, true); toMinorUnits = parseAssetAmount(toAmount, true); } catch { throw new Error(t("positiveAmount")); }
      const payload = {
        fromAmount: { currencyCode: fromCurrency, minorUnits: fromMinorUnits },
        toAmount: { currencyCode: toCurrency, minorUnits: toMinorUnits },
        occurredAt: { seconds: Math.floor(new Date(occurredAt).getTime() / 1000) },
        note: note.trim(),
        fromAssetAccountId: fromAccount, toAssetAccountId: toAccount, requestKey: requestKey.current,
      };
      if (editingID) {
        return currencyExchange.updateCurrencyExchange({ id: editingID, ...payload } as any);
      }
      return currencyExchange.createCurrencyExchange(payload as any);
    },
    onSuccess: async () => {
      setPage(1);
      await queryClient.invalidateQueries({ queryKey: ["currency-exchanges"] });
      await queryClient.invalidateQueries({ queryKey: ["assets"] });
      resetForm();
      closeForm();
    },
    onError: (mutationError) => setFormError((mutationError as Error).message),
  });

  const deleteMutation = useMutation({
    mutationFn: async (id: string) => currencyExchange.deleteCurrencyExchange({ id } as any),
    onSuccess: async () => {
      setDeleteID(null);
      await queryClient.invalidateQueries({ queryKey: ["currency-exchanges"] });
      await queryClient.invalidateQueries({ queryKey: ["assets"] });
    },
  });

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    setFormError("");
    saveMutation.mutate();
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
          <Button icon="plus" onClick={openCreate}>{t("create")}</Button>
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
            <Card>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[760px]">
                  <thead className="bg-secondary/40">
                    <tr className="text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      <th className="px-4 py-3">{t("date")}</th>
                      <th className="px-4 py-3 text-right">{t("from")}</th>
                      <th className="w-8 py-3" aria-label={t("to")} />
                      <th className="px-4 py-3 text-right">{t("to")}</th>
                      <th className="px-4 py-3">{t("rate")}</th>
                      <th className="px-4 py-3">{t("note")}</th>
                      <th className="w-20 px-2 py-3" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {exchanges.map((exchange: any) => {
                      const date = exchange.occurredAt?.seconds
                        ? new Date(Number(exchange.occurredAt.seconds) * 1000)
                        : null;
                      const rate = displayRate(exchange);
                      return (
                        <tr key={exchange.id} className="transition-colors hover:bg-secondary/20">
                          <td className="whitespace-nowrap px-4 py-3 text-sm text-muted-foreground">
                            {date && new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date)}
                          </td>
                          <td className="whitespace-nowrap px-4 py-3 text-right text-sm font-semibold">
                            {assetMoney(exchange.fromAmount?.minorUnits ?? 0n, exchange.fromAmount?.currencyCode, locale)}
                          </td>
                          <td className="py-3 text-center">
                            <Icon name="arrow-right" size={16} className="text-muted-foreground" />
                          </td>
                          <td className="whitespace-nowrap px-4 py-3 text-right text-sm font-semibold">
                            {assetMoney(exchange.toAmount?.minorUnits ?? 0n, exchange.toAmount?.currencyCode, locale)}
                          </td>
                          <td className="whitespace-nowrap px-4 py-3 text-sm font-medium text-primary">
                            {t("rateFormat", {
                              from: rate.from,
                              rate: formatRate(rate.rate, locale),
                              to: rate.to,
                            })}
                          </td>
                          <td className="max-w-xs truncate px-4 py-3 text-sm" title={exchange.note || undefined}>
                            {exchange.note || "—"}
                          </td>
                          <td className="px-2 py-2">
                            <div className="flex justify-end gap-1">
                              <Button
                                variant="ghost"
                                size="sm"
                                icon="edit"
                                aria-label={tc("edit")}
                                onClick={() => openEdit(exchange)}
                              >
                                <span className="sr-only">{tc("edit")}</span>
                              </Button>
                              <Button
                                variant="ghost"
                                size="sm"
                                icon="trash"
                                aria-label={tc("delete")}
                                onClick={() => setDeleteID(exchange.id)}
                              >
                                <span className="sr-only">{tc("delete")}</span>
                              </Button>
                            </div>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </Card>

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
        title={editingID ? t("editExchange") : t("newExchange")}
        onClose={closeForm}
        maxWidthClass="max-w-xl"
        footer={(
          <>
            <Button variant="outline" onClick={closeForm} disabled={saveMutation.isPending}>{tc("cancel")}</Button>
            <Button type="submit" form="currency-exchange-form" loading={saveMutation.isPending}>{tc("save")}</Button>
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
                onChange={value => { setFromCurrency(value); setFromAccount(""); }}
                quickLabel={t("quickCurrencies")}
                allLabel={t("allCurrencies")}
              />
              <AssetAccountSelect value={fromAccount} onChange={setFromAccount} currency={fromCurrency} hint={false} />
            </fieldset>
            <fieldset className="space-y-2 rounded-lg border border-border p-3">
              <legend className="px-1 text-sm font-semibold">{t("to")}</legend>
              <label className="block text-xs font-medium text-muted-foreground">{t("amount")}</label>
              <input className={inputClass} inputMode="decimal" required value={toAmount} onChange={(event) => setToAmount(event.target.value)} placeholder="9500.00" />
              <CurrencyPicker
                value={toCurrency}
                onChange={value => { setToCurrency(value); setToAccount(""); }}
                quickLabel={t("quickCurrencies")}
                allLabel={t("allCurrencies")}
              />
              <AssetAccountSelect value={toAccount} onChange={setToAccount} currency={toCurrency} hint={false} />
            </fieldset>
          </div>
          <div className="space-y-2">
            <label className="block text-sm font-medium">{t("date")}</label>
            <input className={inputClass} type="datetime-local" step="1" required value={occurredAt} onChange={(event) => setOccurredAt(event.target.value)} />
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
