import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { authStore, AUTH_CHANGED_EVENT, TENANT_CHANGED_EVENT } from "@/lib/auth/store";
import type { AssetAccount } from "@/proto/budget/v1/asset_pb";

export const ASSET_CURRENCIES = [
  "RUB",
  "USD",
  "EUR",
  "GBP",
  "KZT",
  "CNY",
  "TRY",
  "GEL",
  "AMD",
  "RSD",
];
export const ASSET_KINDS = ["cash", "bank", "deposit", "investment", "property"] as const;
export type AssetKind = (typeof ASSET_KINDS)[number];
export const ASSET_ICONS = {
  cash: "wallet",
  bank: "credit-card",
  deposit: "piggy-bank",
  investment: "trending-up",
  property: "home",
} as const;

export function useAssetTenant() {
  const [tenant, setTenant] = useState<string>();
  useEffect(() => {
    const update = () => setTenant(authStore.getTenant());
    update();
    window.addEventListener(TENANT_CHANGED_EVENT, update);
    window.addEventListener(AUTH_CHANGED_EVENT, update);
    return () => {
      window.removeEventListener(TENANT_CHANGED_EVENT, update);
      window.removeEventListener(AUTH_CHANGED_EVENT, update);
    };
  }, []);
  return tenant;
}
export function useRefreshAssets() {
  const qc = useQueryClient();
  return async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: ["assets"] }),
      qc.invalidateQueries({ queryKey: ["transactions"] }),
      qc.invalidateQueries({ queryKey: ["currency-exchanges"] }),
      qc.invalidateQueries({ queryKey: ["reports"] }),
    ]);
  };
}
export function parseAssetAmount(input: string, positive = false): bigint {
  const value = input.replace(/[\s\u00a0\u202f]/g, "").replace(",", ".");
  if (!/^-?\d+(\.\d{1,2})?$/.test(value)) throw new Error("amount");
  const negative = value.startsWith("-");
  const [whole, fraction = ""] = value.replace(/^-/, "").split(".");
  const amount =
    (BigInt(whole) * 100n + BigInt(fraction.padEnd(2, "0"))) *
    (negative ? -1n : 1n);
  if (
    amount < -9223372036854775808n ||
    amount > 9223372036854775807n ||
    (positive && amount <= 0n)
  )
    throw new Error("amount");
  return amount;
}
export function assetAmountInput(minor: bigint | string | number = 0n) {
  const value = BigInt(minor);
  const absolute = value < 0n ? -value : value;
  return `${value < 0n ? "-" : ""}${absolute / 100n}.${String(absolute % 100n).padStart(2, "0")}`;
}
export function assetMoney(
  minor: bigint | string | number = 0n,
  currency = "RUB",
  locale = "ru",
  fractionDigits: 0 | 2 = 2,
) {
  let value = BigInt(minor);
  if (fractionDigits === 0) {
    value = value < 0n
      ? -((-value + 50n) / 100n) * 100n
      : ((value + 50n) / 100n) * 100n;
  }
  const absolute = value < 0n ? -value : value;
  const integers = new Intl.NumberFormat(locale, {
    maximumFractionDigits: 0,
  }).formatToParts(absolute / 100n);
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  })
    .formatToParts(value < 0n ? -0 : 0)
    .flatMap((part) =>
      part.type === "integer"
        ? integers
        : part.type === "fraction"
          ? [{ ...part, value: String(absolute % 100n).padStart(2, "0") }]
          : [part],
    )
    .map((p) => p.value)
    .join("");
}
export function localAssetTime(date = new Date()) {
  const pad = (v: number) => String(v).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}
export function assetTimestamp(value: string) {
  const millis = new Date(value).getTime();
  if (!Number.isFinite(millis)) throw new Error("date");
  return { seconds: BigInt(Math.floor(millis / 1000)), nanos: 0 };
}
export function assetDate(
  value?: { seconds: bigint | string | number },
  locale = "ru",
  includeTime = false,
) {
  if (!value) return "—";
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    ...(includeTime ? { timeStyle: "short" as const } : {}),
  }).format(new Date(Number(value.seconds) * 1000));
}
export function accountCurrencies(account?: AssetAccount) {
  return account?.fixedCurrencyCode
    ? [account.fixedCurrencyCode]
    : (account?.balances.map((b) => b.amount!.currencyCode) ?? []);
}
export function hasNegativeBalance(account: AssetAccount) {
  return account.balances.some((b) => (b.amount?.minorUnits ?? 0n) < 0n);
}

// Match the backend: round each balance half away from zero, then sum minor units.
export function convertAssetMinor(amount: bigint, rate: string): bigint {
  if (!/^\d+(\.\d+)?$/.test(rate)) throw new Error("rate");
  const [whole, fraction = ""] = rate.split(".");
  const numerator = BigInt(whole + fraction);
  if (numerator <= 0n) throw new Error("rate");
  const denominator = 10n ** BigInt(fraction.length);
  const signed = amount * numerator;
  const absolute = signed < 0n ? -signed : signed;
  const result = (absolute + denominator / 2n) / denominator;
  return signed < 0n ? -result : result;
}

export function valueAssetAccounts(
  accounts: AssetAccount[],
  rates: ReadonlyMap<string, string>,
  currency: string,
) {
  let total = 0n;
  let incomplete = false;
  for (const account of accounts) {
    for (const { amount } of account.balances) {
      if (!amount || amount.minorUnits === 0n) continue;
      if (amount.currencyCode === currency) total += amount.minorUnits;
      else {
        const rate = rates.get(amount.currencyCode);
        if (!rate) { incomplete = true; continue; }
        try { total += convertAssetMinor(amount.minorUnits, rate); }
        catch { incomplete = true; }
      }
    }
  }
  return { total, incomplete };
}

export function useAssetDisplayCurrency(tenant?: string) {
  const [selection, setSelection] = useState<{ tenant: string; code: string }>();
  useEffect(() => {
    if (!tenant) return;
    let code = "";
    try { code = localStorage.getItem(`assets:display-currency:${tenant}`) || ""; }
    catch { /* Storage may be disabled; selection still works for this visit. */ }
    setSelection({ tenant, code: /^[A-Z]{3}$/.test(code) ? code : "" });
  }, [tenant]);
  const ready = !!tenant && selection?.tenant === tenant;
  const select = (code: string) => {
    if (!tenant) return;
    setSelection({ tenant, code });
    try { localStorage.setItem(`assets:display-currency:${tenant}`, code); }
    catch { /* Keep the in-memory choice. */ }
  };
  return { target: ready ? selection.code : "", setTarget: select, ready };
}

export function useAssetIncludeProperty(tenant?: string) {
  const [selection, setSelection] = useState<{ tenant: string; include: boolean }>();
  useEffect(() => {
    if (!tenant) return;
    let include = true;
    try { include = localStorage.getItem(`assets:include-property:${tenant}`) !== "false"; }
    catch { /* Keep the default when storage is unavailable. */ }
    setSelection({ tenant, include });
  }, [tenant]);
  const ready = !!tenant && selection?.tenant === tenant;
  const setIncludeProperty = (include: boolean) => {
    if (!tenant) return;
    setSelection({ tenant, include });
    try { localStorage.setItem(`assets:include-property:${tenant}`, String(include)); }
    catch { /* Keep the in-memory choice. */ }
  };
  return { includeProperty: ready ? selection.include : true, setIncludeProperty, ready };
}
