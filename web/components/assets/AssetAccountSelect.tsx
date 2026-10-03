"use client";
import { useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import type { AssetAccount } from "@/proto/budget/v1/asset_pb";
import { accountCurrencies, useAssetTenant } from "@/lib/assets";

export default function AssetAccountSelect({
  value,
  onChange,
  currency,
  label,
  hint = true,
}: {
  value: string;
  onChange: (value: string) => void;
  currency: string;
  label?: string;
  hint?: boolean;
}) {
  const { asset } = useClients();
  const tenant = useAssetTenant();
  const t = useTranslations("assets");
  const { data } = useQuery<{ accounts: AssetAccount[] }>({
    queryKey: ["assets", "accounts", tenant, true],
    enabled: !!tenant && !!asset,
    queryFn: () => asset.listAccounts({ includeArchived: true }),
  });
  const options = (data?.accounts || []).filter(
    (a) =>
      a.id === value ||
      (!a.archived &&
        a.kind !== "investment" &&
        accountCurrencies(a).includes(currency)),
  );
  return (
    <label className="block space-y-1.5 text-sm">
      <span className="block font-medium">
        {label || t("accountForOperation")}
      </span>
      <select
        className="input"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">{t("withoutAccount")}</option>
        {value && !options.some((a) => a.id === value) && (
          <option value={value}>{t("linkedAccount")}</option>
        )}
        {options.map((a) => (
          <option
            key={a.id}
            value={a.id}
            disabled={
              a.archived ||
              a.kind === "investment" ||
              !accountCurrencies(a).includes(currency)
            }
          >
            {a.name}
            {a.archived ? ` · ${t("archived")}` : ""}
          </option>
        ))}
      </select>
      {hint && (
        <span className="block text-xs text-[hsl(var(--muted-foreground))]">
          {t("accountHint")}
        </span>
      )}
    </label>
  );
}
