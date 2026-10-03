import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { authStore, TENANT_CHANGED_EVENT } from "@/lib/auth/store";
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
export const ASSET_KINDS = ["cash", "bank", "deposit", "investment"] as const;
export type AssetKind = (typeof ASSET_KINDS)[number];
export const ASSET_ICONS = {
  cash: "wallet",
  bank: "credit-card",
  deposit: "piggy-bank",
  investment: "trending-up",
} as const;

export function useAssetTenant() {
  const [tenant, setTenant] = useState<string>();
  useEffect(() => {
    const update = () => setTenant(authStore.getTenant());
    update();
    window.addEventListener(TENANT_CHANGED_EVENT, update);
    return () => window.removeEventListener(TENANT_CHANGED_EVENT, update);
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
) {
  const value = BigInt(minor);
  const absolute = value < 0n ? -value : value;
  const integers = new Intl.NumberFormat(locale, {
    maximumFractionDigits: 0,
  }).formatToParts(absolute / 100n);
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
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
