"use client";

import { FormEvent, useRef, useState } from "react";
import { useLocale, useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { Button, Icon, Modal } from "@/components";
import type {
  AssetAccount,
  AssetSnapshot,
  AssetTransfer,
} from "@/proto/budget/v1/asset_pb";
import {
  ASSET_CURRENCIES,
  ASSET_KINDS,
  accountCurrencies,
  assetAmountInput,
  assetMoney,
  assetTimestamp,
  localAssetTime,
  parseAssetAmount,
  useRefreshAssets,
} from "@/lib/assets";
import { Code } from "@connectrpc/connect";
import NewTransactionForm, {
  NewTxFormRef,
} from "@/app/transactions/NewTransactionForm";

export type AssetAction = {
  mode: "account" | "snapshot" | "transfer" | "exchange" | "transaction";
  account?: AssetAccount;
  snapshot?: AssetSnapshot;
  transfer?: AssetTransfer;
  direction?: "in" | "out";
  txType?: number;
};
const fieldClass = "space-y-1.5 text-sm font-medium";
function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className={fieldClass}>
      <span className="block">{label}</span>
      {children}
    </label>
  );
}
function Currency({
  value,
  onChange,
  choices = ASSET_CURRENCIES,
  disabled = false,
}: {
  value: string;
  onChange: (v: string) => void;
  choices?: string[];
  disabled?: boolean;
}) {
  return (
    <select
      className="input"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
    >
      {Array.from(new Set([value, ...choices]))
        .filter(Boolean)
        .map((code) => (
          <option key={code}>{code}</option>
        ))}
    </select>
  );
}
export function assetError(
  error: unknown,
  t: ReturnType<typeof useTranslations>,
) {
  const e = error as { message?: string; code?: number };
  if (e.message === "amount") return t("errors.amount");
  if (e.message === "date") return t("errors.date");
  if (e.code === Code.Aborted) return t("errors.conflict");
  if (e.code === Code.FailedPrecondition) return t("errors.precondition");
  if (e.code === Code.InvalidArgument) return t("errors.invalid");
  if (e.code === Code.NotFound) return t("errors.notFound");
  return t("errors.save");
}
function FormShell({
  children,
  onSubmit,
  onClose,
  saveLabel,
}: {
  children: React.ReactNode;
  onSubmit: () => Promise<unknown>;
  onClose: () => void;
  saveLabel?: string;
}) {
  const t = useTranslations("assets");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (saving) return;
    setError("");
    setSaving(true);
    try {
      await onSubmit();
    } catch (e) {
      setError(assetError(e, t));
    } finally {
      setSaving(false);
    }
  };
  return (
    <form className="space-y-5" onSubmit={submit}>
      {children}
      {error && (
        <p
          role="alert"
          className="rounded-lg bg-[hsl(var(--negative)/0.08)] px-3 py-2 text-sm text-[hsl(var(--negative))]"
        >
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2 border-t border-[hsl(var(--border))] pt-4">
        <Button
          type="button"
          variant="ghost"
          onClick={onClose}
          disabled={saving}
        >
          {t("cancel")}
        </Button>
        <Button type="submit" loading={saving}>
          {saveLabel ?? t("save")}
        </Button>
      </div>
    </form>
  );
}

function AccountForm({
  existing,
  onSaved,
  onClose,
}: {
  existing?: AssetAccount;
  onSaved: () => Promise<void>;
  onClose: () => void;
}) {
  const { asset } = useClients();
  const t = useTranslations("assets");
  const [name, setName] = useState(existing?.name ?? "");
  const [kind, setKind] = useState(existing?.kind ?? "bank");
  const [institution, setInstitution] = useState(existing?.institution ?? "");
  const [note, setNote] = useState(existing?.note ?? "");
  const [currency, setCurrency] = useState(
    existing?.fixedCurrencyCode || "RUB",
  );
  const [rate, setRate] = useState(existing?.depositRateDecimal ?? "");
  const [opened, setOpened] = useState(existing?.depositOpenedOn ?? "");
  const [matures, setMatures] = useState(existing?.depositMaturesOn ?? "");
  const [asOf, setAsOf] = useState(localAssetTime);
  const [balances, setBalances] = useState([{ currency: "RUB", amount: "" }]);
  const key = useRef(crypto.randomUUID());
  const fixed = kind === "deposit" || kind === "investment";
  const save = async () => {
    const account = {
      id: existing?.id,
      version: existing?.version,
      name,
      kind,
      institution,
      note,
      fixedCurrencyCode: fixed ? currency : "",
      depositRateDecimal: kind === "deposit" ? rate.replace(",", ".") : "",
      depositOpenedOn: kind === "deposit" ? opened : "",
      depositMaturesOn: kind === "deposit" ? matures : "",
    };
    if (existing) await asset.updateAccount({ account });
    else
      await asset.createAccount({
        account,
        requestKey: key.current,
        openingBalances: (fixed
          ? [{ currency, amount: balances[0].amount }]
          : balances
        ).map((b) => ({
          amount: {
            currencyCode: b.currency,
            minorUnits: parseAssetAmount(b.amount),
          },
          asOf: assetTimestamp(asOf),
          kind: "opening",
        })),
      });
    await onSaved();
  };
  return (
    <FormShell
      onSubmit={save}
      onClose={onClose}
      saveLabel={existing ? t("save") : t("createAccount")}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("name")}>
          <input
            className="input"
            required
            maxLength={200}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("namePlaceholder")}
            autoFocus
          />
        </Field>
        <Field label={t("kind")}>
          <select
            className="input"
            value={kind}
            disabled={!!existing}
            onChange={(e) => setKind(e.target.value)}
          >
            {ASSET_KINDS.map((k) => (
              <option key={k} value={k}>
                {t(`kinds.${k}`)}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <Field label={kind === "cash" ? t("location") : t("institution")}>
        <input
          className="input"
          maxLength={200}
          value={institution}
          onChange={(e) => setInstitution(e.target.value)}
          placeholder={
            kind === "cash"
              ? t("locationPlaceholder")
              : t("institutionPlaceholder")
          }
        />
      </Field>
      {fixed && (
        <Field
          label={kind === "investment" ? t("valuationCurrency") : t("currency")}
        >
          <Currency
            value={currency}
            onChange={setCurrency}
            disabled={!!existing}
          />
        </Field>
      )}
      {kind === "investment" && (
        <p className="rounded-lg bg-[hsl(var(--primary)/0.07)] p-3 text-sm text-[hsl(var(--muted-foreground))]">
          {t("investmentHint")}
        </p>
      )}
      {kind === "deposit" && (
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label={t("rate")}>
            <input
              className="input"
              inputMode="decimal"
              value={rate}
              onChange={(e) => setRate(e.target.value)}
              placeholder="16.5"
            />
          </Field>
          <Field label={t("openedOn")}>
            <input
              className="input"
              type="date"
              value={opened}
              onChange={(e) => setOpened(e.target.value)}
            />
          </Field>
          <Field label={t("maturesOn")}>
            <input
              className="input"
              type="date"
              value={matures}
              onChange={(e) => setMatures(e.target.value)}
            />
          </Field>
        </div>
      )}
      {!existing && (
        <div className="space-y-3 rounded-xl bg-[hsl(var(--secondary))] p-4">
          <p className="font-medium">
            {kind === "investment"
              ? t("initialValuation")
              : t("openingBalances")}
          </p>
          {(fixed ? balances.slice(0, 1) : balances).map((b, index) => (
            <div className="flex items-end gap-2" key={index}>
              <div className="min-w-0 flex-1">
                <Field label={t("amount")}>
                  <input
                    className="input"
                    required
                    inputMode="decimal"
                    value={b.amount}
                    onChange={(e) =>
                      setBalances((old) =>
                        old.map((v, i) =>
                          i === index ? { ...v, amount: e.target.value } : v,
                        ),
                      )
                    }
                    placeholder="0.00"
                  />
                </Field>
              </div>
              {!fixed && (
                <div className="w-28">
                  <Field label={t("currency")}>
                    <Currency
                      value={b.currency}
                      onChange={(v) =>
                        setBalances((old) =>
                          old.map((b, i) =>
                            i === index ? { ...b, currency: v } : b,
                          ),
                        )
                      }
                    />
                  </Field>
                </div>
              )}
              {!fixed && balances.length > 1 && (
                <button
                  type="button"
                  className="p-2 text-[hsl(var(--muted-foreground))]"
                  aria-label={t("removeCurrency")}
                  onClick={() =>
                    setBalances((old) => old.filter((_, i) => i !== index))
                  }
                >
                  <Icon name="close" size={18} />
                </button>
              )}
            </div>
          ))}
          {!fixed && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              icon="plus"
              disabled={balances.length >= ASSET_CURRENCIES.length}
              onClick={() =>
                setBalances((old) => [
                  ...old,
                  {
                    currency:
                      ASSET_CURRENCIES.find(
                        (c) => !old.some((b) => b.currency === c),
                      ) || "USD",
                    amount: "",
                  },
                ])
              }
            >
              {t("addCurrency")}
            </Button>
          )}
          <Field label={t("asOf")}>
            <input
              className="input"
              type="datetime-local"
              step="1"
              required
              value={asOf}
              onChange={(e) => setAsOf(e.target.value)}
            />
          </Field>
          <p className="text-xs text-[hsl(var(--muted-foreground))]">
            {t("openingHint")}
          </p>
        </div>
      )}
      <Field label={t("note")}>
        <textarea
          className="input h-auto min-h-20"
          maxLength={5000}
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
      </Field>
    </FormShell>
  );
}

function SnapshotForm({
  account,
  snapshot,
  onSaved,
  onClose,
}: {
  account: AssetAccount;
  snapshot?: AssetSnapshot;
  onSaved: () => Promise<void>;
  onClose: () => void;
}) {
  const { asset } = useClients();
  const t = useTranslations("assets");
  const locale = useLocale();
  const [currency, setCurrency] = useState(
    snapshot?.amount?.currencyCode || accountCurrencies(account)[0] || "RUB",
  );
  const [amount, setAmount] = useState(
    snapshot ? assetAmountInput(snapshot.amount?.minorUnits) : "",
  );
  const [asOf, setAsOf] = useState(
    snapshot?.asOf
      ? localAssetTime(new Date(Number(snapshot.asOf.seconds) * 1000))
      : localAssetTime(),
  );
  const [note, setNote] = useState(snapshot?.note || "");
  const key = useRef(crypto.randomUUID());
  const current = account.balances.find(
    (b) => b.amount?.currencyCode === currency,
  );
  const save = async () => {
    const value = {
      id: snapshot?.id,
      version: snapshot?.version,
      accountId: account.id,
      amount: { currencyCode: currency, minorUnits: parseAssetAmount(amount) },
      asOf: assetTimestamp(asOf),
      kind:
        snapshot?.kind ||
        (account.kind === "investment" ? "valuation" : "reconciliation"),
      note,
    };
    if (snapshot) await asset.updateSnapshot({ snapshot: value });
    else
      await asset.createSnapshot({ snapshot: value, requestKey: key.current });
    await onSaved();
  };
  return (
    <FormShell onSubmit={save} onClose={onClose}>
      <div className="rounded-xl bg-[hsl(var(--secondary))] p-4">
        <p className="text-xs text-[hsl(var(--muted-foreground))]">
          {t("currentBalance")}
        </p>
        <p className="mt-1 text-xl font-semibold tabular-nums">
          {current
            ? assetMoney(current.amount?.minorUnits, currency, locale)
            : t("unknownBalance")}
        </p>
      </div>
      <div className="grid gap-4 sm:grid-cols-[1fr_110px]">
        <Field
          label={
            account.kind === "investment"
              ? t("wholeValuation")
              : t("actualBalance")
          }
        >
          <input
            className="input"
            inputMode="decimal"
            required
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            autoFocus
            placeholder="0.00"
          />
        </Field>
        <Field label={t("currency")}>
          <Currency
            value={currency}
            onChange={setCurrency}
            choices={
              account.fixedCurrencyCode
                ? [account.fixedCurrencyCode]
                : ASSET_CURRENCIES
            }
            disabled={!!snapshot || !!account.fixedCurrencyCode}
          />
        </Field>
      </div>
      <Field label={t("asOf")}>
        <input
          className="input"
          type="datetime-local"
          step="1"
          required
          value={asOf}
          onChange={(e) => setAsOf(e.target.value)}
        />
      </Field>
      <p className="text-sm text-[hsl(var(--muted-foreground))]">
        {account.kind === "investment"
          ? t("investmentHint")
          : t("snapshotHint")}
      </p>
      <Field label={t("note")}>
        <textarea
          className="input h-auto min-h-20"
          maxLength={5000}
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
      </Field>
    </FormShell>
  );
}

function MovementForm({
  accounts,
  action,
  onSaved,
  onClose,
}: {
  accounts: AssetAccount[];
  action: AssetAction;
  onSaved: () => Promise<void>;
  onClose: () => void;
}) {
  const { asset, currencyExchange } = useClients();
  const t = useTranslations("assets");
  const existing = action.transfer;
  const exchanging = action.mode === "exchange";
  const [from, setFrom] = useState(
    existing?.fromAccountId ??
      (action.direction === "in"
        ? ""
        : action.account?.id || accounts[0]?.id || ""),
  );
  const [to, setTo] = useState(
    existing?.toAccountId ??
      (action.direction === "in" ? action.account?.id || "" : ""),
  );
  const [fromCurrency, setFromCurrency] = useState(
    existing?.fromAmount?.currencyCode ||
      accountCurrencies(accounts.find((a) => a.id === from))[0] ||
      "RUB",
  );
  const [toCurrency, setToCurrency] = useState(
    existing?.toAmount?.currencyCode ||
      (exchanging
        ? "USD"
        : accountCurrencies(accounts.find((a) => a.id === to))[0] ||
          fromCurrency),
  );
  const [fromAmount, setFromAmount] = useState(
    existing ? assetAmountInput(existing.fromAmount?.minorUnits) : "",
  );
  const [toAmount, setToAmount] = useState(
    existing ? assetAmountInput(existing.toAmount?.minorUnits) : "",
  );
  const [at, setAt] = useState(
    existing?.occurredAt
      ? localAssetTime(new Date(Number(existing.occurredAt.seconds) * 1000))
      : localAssetTime(),
  );
  const [note, setNote] = useState(existing?.note || "");
  const key = useRef(crypto.randomUUID());
  const fromAccount = accounts.find((a) => a.id === from);
  const toAccount = accounts.find((a) => a.id === to);
  const investment =
    fromAccount?.kind === "investment" || toAccount?.kind === "investment";
  const effectiveFromCurrency = from ? fromCurrency : toCurrency;
  const effectiveToCurrency = to ? toCurrency : effectiveFromCurrency;
  const monetaryExchange =
    !investment &&
    !!from &&
    !!to &&
    effectiveFromCurrency !== effectiveToCurrency;
  const sameAmount =
    !exchanging && effectiveFromCurrency === effectiveToCurrency;
  const choose = (id: string, direction: "from" | "to") => {
    const acc = accounts.find((a) => a.id === id);
    const codes = accountCurrencies(acc);
    if (direction === "from") {
      setFrom(id);
      if (codes.length && !codes.includes(fromCurrency))
        setFromCurrency(codes[0]);
    } else {
      setTo(id);
      if (codes.length && !codes.includes(toCurrency)) setToCurrency(codes[0]);
    }
  };
  const save = async () => {
    if (exchanging && (!from || !to || fromCurrency === toCurrency))
      throw { code: Code.InvalidArgument };
    const sourceCurrency = !from && !exchanging ? toCurrency : fromCurrency;
    const destinationCurrency =
      !to && !exchanging ? sourceCurrency : toCurrency;
    const source = {
      currencyCode: sourceCurrency,
      minorUnits: parseAssetAmount(fromAmount, true),
    };
    const destination = {
      currencyCode: destinationCurrency,
      minorUnits: sameAmount
        ? source.minorUnits
        : parseAssetAmount(toAmount, true),
    };
    if ((exchanging || monetaryExchange) && !existing)
      await currencyExchange.createCurrencyExchange({
        fromAmount: source,
        toAmount: destination,
        fromAssetAccountId: from,
        toAssetAccountId: to,
        occurredAt: assetTimestamp(at),
        note,
        requestKey: key.current,
      });
    else {
      const transfer = {
        id: existing?.id,
        version: existing?.version,
        fromAccountId: from,
        toAccountId: to,
        fromAmount: source,
        toAmount: destination,
        occurredAt: assetTimestamp(at),
        note,
      };
      if (existing) await asset.updateTransfer({ transfer });
      else await asset.createTransfer({ transfer, requestKey: key.current });
    }
    await onSaved();
  };
  const choices = accounts.filter(
    (a) => !a.archived && (!exchanging || a.kind !== "investment"),
  );
  return (
    <FormShell onSubmit={save} onClose={onClose}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("fromAccount")}>
          <select
            className="input"
            value={from}
            onChange={(e) => choose(e.target.value, "from")}
            required={exchanging}
          >
            <option value="">
              {exchanging ? t("chooseAccount") : t("outside")}
            </option>
            {choices.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label={t("toAccount")}>
          <select
            className="input"
            value={to}
            onChange={(e) => choose(e.target.value, "to")}
            required={exchanging}
          >
            <option value="">
              {exchanging ? t("chooseAccount") : t("outside")}
            </option>
            {choices.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-3">
          <Field label={sameAmount ? t("amount") : t("debited")}>
            <input
              className="input"
              required
              inputMode="decimal"
              value={fromAmount}
              onChange={(e) => setFromAmount(e.target.value)}
              placeholder="0.00"
            />
          </Field>
          <Field label={t("currency")}>
            <Currency
              value={effectiveFromCurrency}
              onChange={setFromCurrency}
              choices={
                fromAccount
                  ? accountCurrencies(fromAccount)
                  : accountCurrencies(toAccount)
              }
              disabled={!from}
            />
          </Field>
        </div>
        <div className="space-y-3">
          {!sameAmount && (
            <Field label={investment ? t("valuationCredit") : t("credited")}>
              <input
                className="input"
                required
                inputMode="decimal"
                value={toAmount}
                onChange={(e) => setToAmount(e.target.value)}
                placeholder="0.00"
              />
            </Field>
          )}
          <Field label={t("receivingCurrency")}>
            <Currency
              value={toCurrency}
              onChange={setToCurrency}
              choices={
                toAccount
                  ? accountCurrencies(toAccount)
                  : accountCurrencies(fromAccount)
              }
              disabled={!to}
            />
          </Field>
        </div>
      </div>
      <p className="text-sm text-[hsl(var(--muted-foreground))]">
        {exchanging || monetaryExchange
          ? t("exchangeHint")
          : investment
            ? t("investmentTransferHint")
            : t("transferHint")}
      </p>
      <Field label={t("occurredAt")}>
        <input
          className="input"
          type="datetime-local"
          step="1"
          required
          value={at}
          onChange={(e) => setAt(e.target.value)}
        />
      </Field>
      <Field label={t("note")}>
        <textarea
          className="input h-auto min-h-20"
          maxLength={5000}
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
      </Field>
    </FormShell>
  );
}

export default function AssetDialogs({
  action,
  accounts,
  onClose,
}: {
  action: AssetAction | null;
  accounts: AssetAccount[];
  onClose: () => void;
}) {
  const t = useTranslations("assets");
  const refresh = useRefreshAssets();
  const txRef = useRef<NewTxFormRef>(null);
  const saved = async () => {
    await refresh();
    onClose();
  };
  if (!action) return null;
  const title =
    action.mode === "account"
      ? action.account
        ? t("editAccount")
        : t("createAccount")
      : action.mode === "snapshot"
        ? action.snapshot
          ? t("editSnapshot")
          : action.account?.kind === "investment"
            ? t("updateValuation")
            : t("reconcile")
        : action.mode === "exchange"
          ? t("exchange")
          : action.mode === "transaction"
            ? action.txType === 1
              ? t("income")
              : t("expense")
            : action.transfer
              ? t("editTransfer")
              : t("transfer");
  return (
    <Modal
      open
      title={title}
      onClose={onClose}
      maxWidthClass="max-w-xl"
      footer={
        action.mode === "transaction" ? (
          <Button onClick={() => txRef.current?.submit()}>{t("save")}</Button>
        ) : undefined
      }
    >
      {action.mode === "account" && (
        <AccountForm
          existing={action.account}
          onSaved={saved}
          onClose={onClose}
        />
      )}
      {action.mode === "snapshot" && action.account && (
        <SnapshotForm
          account={action.account}
          snapshot={action.snapshot}
          onSaved={saved}
          onClose={onClose}
        />
      )}
      {(action.mode === "transfer" || action.mode === "exchange") && (
        <MovementForm
          accounts={accounts}
          action={action}
          onSaved={saved}
          onClose={onClose}
        />
      )}
      {action.mode === "transaction" && (
        <NewTransactionForm
          ref={txRef}
          initialAccountId={action.account?.id}
          initialCurrency={accountCurrencies(action.account)[0]}
          initialType={action.txType}
          onClose={onClose}
          onSaved={() => void refresh()}
        />
      )}
    </Modal>
  );
}
