"use client";
import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslations, useLocale } from "next-intl";
import { useClients } from "@/app/providers";
import { Modal, CategorySingleInput } from "@/components";
import AssetAccountSelect from "@/components/assets/AssetAccountSelect";
import {
  assetAmountInput,
  assetMoney,
  localAssetTime,
  parseAssetAmount,
} from "@/lib/assets";
import { useDisplayCurrency } from "@/lib/display-currency";
import { useRefreshPayments } from "@/lib/payments";
import { CategoryKind, TransactionType } from "@/proto/budget/v1/common_pb";
import type { ListCategoriesResponse } from "@/proto/budget/v1/category_pb";
import type { PaymentOccurrence } from "@/proto/budget/v1/recurring_payment_pb";
import type {
  Transaction,
  ListTransactionsResponse,
} from "@/proto/budget/v1/transaction_pb";
const input = "w-full rounded-lg border border-border bg-transparent p-2";
export default function PaymentDialogs({
  occurrence: o,
  action,
  onClose,
}: {
  occurrence: PaymentOccurrence;
  action: string;
  onClose: () => void;
}) {
  const { recurringPayment: api, transaction, category } = useClients(),
    t = useTranslations("payments"),
    locale = useLocale(),
    { tenant } = useDisplayCurrency(),
    refresh = useRefreshPayments();
  const [amount, setAmount] = useState(
      (action === "edit" ? o.exactAmount : o.expectedAmount) !== undefined
        ? assetAmountInput(
            (action === "edit" ? o.exactAmount : o.expectedAmount)!,
          )
        : "",
    ),
    [estimate, setEstimate] = useState(
      o.estimateOverride !== undefined
        ? assetAmountInput(o.estimateOverride)
        : "",
    ),
    [due, setDue] = useState(o.dueDate),
    [note, setNote] = useState(o.note),
    [date, setDate] = useState(localAssetTime()),
    [account, setAccount] = useState(o.accountId),
    [cat, setCat] = useState(o.categoryId),
    [mode, setMode] = useState(
      o.status === "needs_review" ? "existing" : "new",
    ),
    [selected, setSelected] = useState<Transaction | undefined>(o.transaction),
    [search, setSearch] = useState(""),
    [wide, setWide] = useState(false),
    [page, setPage] = useState(1),
    [accept, setAccept] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const key = useRef(crypto.randomUUID()),
    inFlight = useRef(false);
  const cats = useQuery<ListCategoriesResponse>({
    queryKey: ["payments", "categories", tenant],
    queryFn: () =>
      category.listCategories({
        kind: CategoryKind.EXPENSE,
        includeInactive: false,
      }),
    enabled: !!tenant,
  });
  const dueAt = new Date(o.dueDate + "T12:00:00");
  const from = new Date(dueAt);
  from.setDate(from.getDate() - 31);
  const to = new Date(dueAt);
  to.setDate(to.getDate() + 32);
  const candidates = useQuery<ListTransactionsResponse>({
    queryKey: ["payments", "candidates", tenant, o.id, search, wide, page],
    enabled: !!tenant && mode === "existing" && action === "pay",
    queryFn: () =>
      transaction.listTransactions({
        type: TransactionType.EXPENSE,
        currencyCode: o.currencyCode,
        categoryIds: wide ? [] : [o.categoryId],
        search,
        dateRange: wide
          ? undefined
          : {
              from: { seconds: BigInt(Math.floor(from.getTime() / 1000)) },
              to: { seconds: BigInt(Math.floor(to.getTime() / 1000)) },
            },
        page: { page, pageSize: 20, sort: "occurred_at desc" },
      }),
  });
  const actual =
    mode === "existing"
      ? selected?.amount?.minorUnits
      : amount
        ? (() => {
            try {
              return parseAssetAmount(amount, true);
            } catch {
              return undefined;
            }
          })()
        : undefined;
  const mismatch =
    o.exactAmount !== undefined &&
    actual !== undefined &&
    o.exactAmount !== actual;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const base = {
        id: o.id,
        expectedVersion: o.version,
        requestKey: key.current,
      };
      if (action === "unlink") await api.unlinkPayment(base);
      else if (action === "skip")
        await api.setPaymentSkipped({
          ...base,
          skipped: o.status !== "skipped",
          reason: note,
        });
      else if (action === "edit")
        await api.updatePaymentOccurrence({
          ...base,
          dueDate: due,
          note,
          exactAmount: amount ? parseAssetAmount(amount, true) : undefined,
          estimateOverride: estimate
            ? parseAssetAmount(estimate, true)
            : undefined,
        });
      else {
        let expense;
        if (mode === "existing") {
          if (!selected?.amount || !selected.occurredAt)
            throw Error("selection");
          expense = {
            case: "existingTransaction" as const,
            value: {
              transactionId: selected.id,
              expectedAmount: selected.amount.minorUnits,
              expectedCurrencyCode: selected.amount.currencyCode,
              expectedOccurredAt: selected.occurredAt,
            },
          };
        } else {
          expense = {
            case: "newExpense" as const,
            value: {
              amount: parseAssetAmount(amount, true),
              currencyCode: o.currencyCode,
              occurredAt: {
                seconds: BigInt(Math.floor(new Date(date).getTime() / 1000)),
              },
              accountId: account,
              categoryId: cat,
              comment: note,
            },
          };
        }
        await api.confirmPayment({
          occurrenceId: o.id,
          expectedVersion: o.version,
          requestKey: key.current,
          acceptAmountChange: accept,
          expense,
        });
      }
      // A replay may contain an old response: always refresh the current server state.
      await refresh();
      onClose();
    } catch {
      setError(t("saveError"));
      await refresh();
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };
  if (action === "view")
    return (
      <Modal open title={t("viewExpense")} onClose={onClose}>
        <dl className="space-y-3">
          <dt>{t("amount")}</dt>
          <dd>
            {assetMoney(
              o.transaction?.amount?.minorUnits,
              o.transaction?.amount?.currencyCode,
              locale,
            )}
          </dd>
          <dt>{t("paidAt")}</dt>
          <dd>
            {o.transaction?.occurredAt
              ? new Date(
                  Number(o.transaction.occurredAt.seconds) * 1000,
                ).toLocaleString(locale)
              : ""}
          </dd>
          <dt>{t("category")}</dt>
          <dd>
            {cats.data?.categories
              .find((c) => c.id === o.transaction?.categoryId)
              ?.translations.find((tr) => tr.locale === locale)?.name ||
              o.transaction?.categoryId}
          </dd>
          <dt>{t("note")}</dt>
          <dd>{o.transaction?.comment}</dd>
        </dl>
        <p className="mt-4">{t("expenseEditHint")}</p>
      </Modal>
    );
  return (
    <Modal
      open
      title={t(action)}
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <form onSubmit={submit} className="space-y-4">
        <p>
          {o.name} · {o.periodMonth} · {o.currencyCode}
        </p>
        {action === "unlink" ? (
          <p>{t("unlinkExplanation")}</p>
        ) : action === "skip" ? (
          <label>
            {t("reason")}
            <input
              className={input}
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
          </label>
        ) : (
          <>
            {action === "pay" && (
              <div className="flex gap-3">
                <label>
                  <input
                    type="radio"
                    checked={mode === "new"}
                    disabled={o.status === "needs_review"}
                    onChange={() => {
                      setMode("new");
                      setAccept(false);
                    }}
                  />{" "}
                  {t("newExpense")}
                </label>
                <label>
                  <input
                    type="radio"
                    checked={mode === "existing"}
                    onChange={() => {
                      setMode("existing");
                      setAccept(false);
                    }}
                  />{" "}
                  {t("existingExpense")}
                </label>
              </div>
            )}
            {mode === "existing" && action === "pay" ? (
              <div className="space-y-2">
                <input
                  aria-label={t("searchExpense")}
                  placeholder={t("searchExpense")}
                  className={input}
                  value={search}
                  onChange={(e) => {
                    setSearch(e.target.value);
                    setPage(1);
                  }}
                />
                <label className="block">
                  <input
                    type="checkbox"
                    checked={wide}
                    onChange={(e) => {
                      setWide(e.target.checked);
                      setPage(1);
                    }}
                  />{" "}
                  {t("expandSearch")}
                </label>
                {selected && (
                  <p>
                    {t("selected")}:{" "}
                    {assetMoney(
                      selected.amount?.minorUnits,
                      selected.amount?.currencyCode,
                      locale,
                    )}{" "}
                    · {selected.comment}
                  </p>
                )}
                {candidates.isError && <p role="alert">{t("loadError")}</p>}
                {candidates.data?.transactions
                  .filter(
                    (tx) =>
                      !tx.paymentOccurrenceId ||
                      tx.paymentOccurrenceId === o.id,
                  )
                  .map((tx) => (
                    <label
                      key={tx.id}
                      className="flex gap-2 rounded border border-border p-2"
                    >
                      <input
                        type="radio"
                        name="existing"
                        checked={selected?.id === tx.id}
                        onChange={() => {
                          setSelected(tx);
                          setAccept(false);
                        }}
                      />
                      <span>
                        {assetMoney(
                          tx.amount?.minorUnits,
                          tx.amount?.currencyCode,
                          locale,
                        )}{" "}
                        ·{" "}
                        {tx.occurredAt
                          ? new Date(
                              Number(tx.occurredAt.seconds) * 1000,
                            ).toLocaleDateString(locale)
                          : ""}{" "}
                        · {tx.comment}
                      </span>
                    </label>
                  ))}
                <div className="flex gap-3">
                  <button
                    type="button"
                    disabled={page <= 1}
                    onClick={() => setPage(page - 1)}
                  >
                    {t("previous")}
                  </button>
                  <span>{page}</span>
                  <button
                    type="button"
                    disabled={(candidates.data?.transactions.length ?? 0) < 20}
                    onClick={() => setPage(page + 1)}
                  >
                    {t("next")}
                  </button>
                </div>
              </div>
            ) : (
              <>
                <label className="block">
                  {t("amount")}
                  <input
                    className={input}
                    inputMode="decimal"
                    value={amount}
                    onChange={(e) => {
                      setAmount(e.target.value);
                      setAccept(false);
                    }}
                    required={action === "pay"}
                  />
                </label>
                {action === "edit" ? (
                  <>
                    <label className="block">
                      {t("estimate")}
                      <input
                        className={input}
                        inputMode="decimal"
                        value={estimate}
                        onChange={(e) => setEstimate(e.target.value)}
                      />
                    </label>
                    <label className="block">
                      {t("dueDate")}
                      <input
                        type="date"
                        required
                        className={input}
                        value={due}
                        onChange={(e) => setDue(e.target.value)}
                      />
                    </label>
                  </>
                ) : (
                  <>
                    <label className="block">
                      {t("paidAt")}
                      <input
                        className={input}
                        type="datetime-local"
                        required
                        value={date}
                        onChange={(e) => setDate(e.target.value)}
                      />
                    </label>
                    <label className="block">
                      {t("category")}
                      <CategorySingleInput
                        categories={cats.data?.categories ?? []}
                        value={cat}
                        onChange={(v) => setCat(v || "")}
                      />
                    </label>
                    <AssetAccountSelect
                      currency={o.currencyCode}
                      value={account}
                      onChange={setAccount}
                    />
                  </>
                )}
                <label className="block">
                  {t("note")}
                  <input
                    className={input}
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                  />
                </label>
              </>
            )}
            {action === "pay" && mismatch && (
              <label className="block rounded bg-amber-50 p-3 text-amber-900">
                <input
                  type="checkbox"
                  checked={accept}
                  onChange={(e) => setAccept(e.target.checked)}
                />{" "}
                {t("acceptAmountChange", {
                  amount: assetMoney(o.exactAmount, o.currencyCode, locale),
                })}
              </label>
            )}
          </>
        )}
        {error && (
          <p role="alert" className="text-red-600">
            {error}
          </p>
        )}
        <button
          className="btn btn-primary"
          disabled={
            busy || (action === "pay" && ((mismatch && !accept) || !actual))
          }
        >
          {busy ? t("saving") : t("confirm")}
        </button>
      </form>
    </Modal>
  );
}
