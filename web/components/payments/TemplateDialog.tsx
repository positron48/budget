"use client";
import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { Modal, CategorySingleInput } from "@/components";
import AssetAccountSelect from "@/components/assets/AssetAccountSelect";
import {
  ASSET_CURRENCIES,
  assetAmountInput,
  parseAssetAmount,
} from "@/lib/assets";
import { localPaymentMonth, useRefreshPayments } from "@/lib/payments";
import { useDisplayCurrency } from "@/lib/display-currency";
import { CategoryKind } from "@/proto/budget/v1/common_pb";
import type {
  RecurringPaymentTemplate,
  UpdateRecurringPaymentTemplateResponse,
} from "@/proto/budget/v1/recurring_payment_pb";
const input = "w-full rounded-lg border border-border bg-transparent p-2";
export default function TemplateDialog({
  template: old,
  onClose,
}: {
  template?: RecurringPaymentTemplate;
  onClose: () => void;
}) {
  const { recurringPayment: api, category } = useClients(),
    t = useTranslations("payments"),
    { tenant } = useDisplayCurrency(),
    refresh = useRefreshPayments();
  const [name, setName] = useState(old?.name || ""),
    [categoryId, setCategory] = useState(old?.categoryId || ""),
    [currency, setCurrency] = useState(old?.currencyCode || "RUB"),
    [account, setAccount] = useState(old?.accountId || ""),
    [note, setNote] = useState(old?.note || ""),
    [timezone, setTimezone] = useState(
      old?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone,
    ),
    [mode, setMode] = useState(old?.amountMode || "fixed"),
    [amount, setAmount] = useState(
      old?.fixedAmount !== undefined ? assetAmountInput(old.fixedAmount) : "",
    ),
    [estimate, setEstimate] = useState(
      old?.initialEstimate !== undefined
        ? assetAmountInput(old.initialEstimate)
        : "",
    ),
    [first, setFirst] = useState(old?.firstMonth || localPaymentMonth()),
    [last, setLast] = useState(old?.lastMonth || ""),
    [interval, setInterval] = useState(old?.intervalMonths || 1),
    [day, setDay] = useState(old?.day || 10),
    [lastDay, setLastDay] = useState(old?.lastDay || false),
    [offset, setOffset] = useState(old?.periodOffset || 0),
    [preview, setPreview] = useState<UpdateRecurringPaymentTemplateResponse>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const key = useRef(crypto.randomUUID()),
    lock = useRef(false);
  const cats = useQuery({
    queryKey: ["payments", "categories", tenant],
    enabled: !!tenant,
    queryFn: () =>
      category.listCategories({
        kind: CategoryKind.EXPENSE,
        includeInactive: false,
      }),
  });
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError("");
    try {
      const template = {
        id: old?.id || "",
        version: old?.version || 0n,
        name,
        categoryId,
        currencyCode: currency,
        accountId: account,
        note,
        timezone,
        amountMode: mode,
        fixedAmount:
          mode === "fixed" ? parseAssetAmount(amount, true) : undefined,
        initialEstimate:
          mode === "variable" && estimate
            ? parseAssetAmount(estimate, true)
            : undefined,
        firstMonth: first,
        lastMonth: last,
        intervalMonths: interval,
        day,
        lastDay,
        periodOffset: interval === 1 ? offset : 0,
      };
      if (old) {
        if (!preview) {
          setPreview(
            await api.updateRecurringPaymentTemplate({
              template,
              requestKey: crypto.randomUUID(),
              preview: true,
            }),
          );
          return;
        }
        await api.updateRecurringPaymentTemplate({
          template,
          requestKey: key.current,
        });
      } else
        await api.createRecurringPaymentTemplate({
          template,
          requestKey: key.current,
        });
      await refresh();
      onClose();
    } catch {
      setError(t("saveError"));
      await refresh();
    } finally {
      lock.current = false;
      setBusy(false);
    }
  };
  return (
    <Modal
      open
      title={old ? t("editTemplate") : t("newTemplate")}
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <form
        onSubmit={submit}
        onChange={() => setPreview(undefined)}
        className="space-y-4"
      >
        <label className="block">
          {t("name")}
          <input
            className={input}
            required
            maxLength={200}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label className="block">
          {t("category")}
          <CategorySingleInput
            categories={cats.data?.categories ?? []}
            value={categoryId}
            onChange={(v) => {
              setCategory(v || "");
              setPreview(undefined);
            }}
          />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label>
            {t("currency")}
            <select
              className={input}
              value={currency}
              onChange={(e) => {
                setCurrency(e.target.value);
                setAccount("");
              }}
            >
              {ASSET_CURRENCIES.map((c) => (
                <option key={c}>{c}</option>
              ))}
            </select>
          </label>
          <label>
            {t("amountMode")}
            <select
              className={input}
              value={mode}
              onChange={(e) => setMode(e.target.value)}
            >
              <option value="fixed">{t("fixed")}</option>
              <option value="variable">{t("variable")}</option>
            </select>
          </label>
        </div>
        <label className="block">
          {t(mode === "fixed" ? "amount" : "initialEstimate")}
          <input
            className={input}
            inputMode="decimal"
            required={mode === "fixed"}
            value={mode === "fixed" ? amount : estimate}
            onChange={(e) =>
              mode === "fixed"
                ? setAmount(e.target.value)
                : setEstimate(e.target.value)
            }
          />
        </label>
        <label className="block">
          {t("interval")}
          <select
            className={input}
            value={interval}
            onChange={(e) => setInterval(Number(e.target.value))}
          >
            {[1, 3, 12].map((v) => (
              <option value={v} key={v}>
                {t(`interval${v}`)}
              </option>
            ))}
          </select>
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label>
            {t("firstMonth")}
            <input
              type="month"
              className={input}
              required
              value={first}
              onChange={(e) => setFirst(e.target.value)}
            />
          </label>
          <label>
            {t("lastMonth")}
            <input
              type="month"
              className={input}
              value={last}
              onChange={(e) => setLast(e.target.value)}
            />
          </label>
        </div>
        <label className="block">
          {t("day")}
          <input
            type="number"
            min={1}
            max={31}
            required
            disabled={lastDay}
            className={input}
            value={day}
            onChange={(e) => setDay(Number(e.target.value))}
          />
        </label>
        <label className="block">
          <input
            type="checkbox"
            checked={lastDay}
            onChange={(e) => setLastDay(e.target.checked)}
          />{" "}
          {t("lastDay")}
        </label>
        {interval === 1 && (
          <label className="block">
            {t("servicePeriod")}
            <select
              className={input}
              value={offset}
              onChange={(e) => setOffset(Number(e.target.value))}
            >
              <option value={0}>{t("currentMonth")}</option>
              <option value={-1}>{t("previousMonth")}</option>
            </select>
          </label>
        )}
        <label className="block">
          {t("timezone")}
          <input
            className={input}
            required
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
          />
        </label>
        <AssetAccountSelect
          currency={currency}
          value={account}
          onChange={(v) => {
            setAccount(v);
            setPreview(undefined);
          }}
        />
        <label className="block">
          {t("note")}
          <input
            className={input}
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </label>
        {old && <p>{t("editExplanation")}</p>}
        {preview && (
          <div className="rounded border border-border p-3">
            <p>
              {t("previewCount", {
                count: preview.affected.length,
                preserved: preview.preserved.length,
              })}
            </p>
            <ul>
              {preview.proposed.slice(0, 12).map((o) => (
                <li key={o.scheduledMonth}>
                  {o.name} · {o.dueDate}
                </li>
              ))}
            </ul>
            {preview.preserved.map((o) => (
              <p key={o.id}>
                {t("preserved")}: {o.name} · {o.dueDate}
              </p>
            ))}
          </div>
        )}
        {error && (
          <p role="alert" className="text-red-600">
            {error}
          </p>
        )}
        <button className="btn btn-primary" disabled={busy || !categoryId}>
          {busy ? t("saving") : old && !preview ? t("preview") : t("save")}
        </button>
      </form>
    </Modal>
  );
}
