"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { authStore } from "@/lib/auth/store";
import { useTranslations } from "next-intl";

function TenantsInner() {
  const { tenant } = useClients();
  const qc = useQueryClient();
  const t = useTranslations("tenants");
  const tc = useTranslations("common");
  const { data, isLoading, error } = useQuery({
    queryKey: ["tenants"],
    queryFn: async () => (await tenant.listMyTenants({} as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const memberships = (data?.memberships ?? []) as any[];
  const [name, setName] = useState("");
  const [currency, setCurrency] = useState("RUB");
  const [slug, setSlug] = useState("");
  const createMut = useMutation({
    mutationFn: async () =>
      await tenant.createTenant({ name, defaultCurrencyCode: currency, slug: slug || undefined } as any),
    onSuccess: () => {
      setName("");
      setSlug("");
      qc.invalidateQueries({ queryKey: ["tenants"] });
    },
  });

  const currentTenantId = authStore.getTenant();
  const makeActive = (id: string) => authStore.set({ tenantId: id });

  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold mb-2">{t("title")}</h1>
      <div className="text-sm text-gray-600 mb-3">{t("activeTenant")}: {currentTenantId ?? "—"}</div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          createMut.mutate();
        }}
        className="flex gap-3 items-end mb-4 flex-wrap"
      >
        <div>
          <label className="block text-xs">{t("name")}</label>
          <input className="border rounded px-2 py-1" value={name} onChange={(e) => setName(e.target.value)} required />
        </div>
        <div>
          <label className="block text-xs">{t("slugOptional")}</label>
          <input className="border rounded px-2 py-1" value={slug} onChange={(e) => setSlug(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">{t("defaultCurrency")}</label>
          <input className="border rounded px-2 py-1 w-24" value={currency} onChange={(e) => setCurrency(e.target.value)} />
        </div>
        <button className="bg-black text-white rounded px-3 py-1" disabled={createMut.isPending}>
          {createMut.isPending ? tc("loading") : t("create")}
        </button>
      </form>

      {isLoading && <div className="text-sm text-gray-500">{tc("loading")}</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}

      <ul className="space-y-2">
        {memberships.map((m) => (
          <li key={m?.tenant?.id} className="flex items-center gap-3 text-sm">
            <span className="font-medium">{m?.tenant?.name}</span>
            <span className="text-gray-500">{m?.tenant?.slug}</span>
            <span className="text-gray-500">{m?.tenant?.defaultCurrencyCode}</span>
            <span className="text-[11px] px-2 py-0.5 rounded border text-gray-600">{String(m?.role)}</span>
            {currentTenantId === m?.tenant?.id ? (
              <span className="ml-auto text-green-600 text-xs">{t("active")}</span>
            ) : (
              <button className="ml-auto text-xs underline" onClick={() => makeActive(m?.tenant?.id)}>
                {t("makeActive")}
              </button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

export default function TenantsPage() {
  return (
    <ClientsProvider>
      <TenantsInner />
    </ClientsProvider>
  );
}


