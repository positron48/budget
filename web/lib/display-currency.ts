"use client";

import {useCallback, useEffect, useState} from "react";
import {authStore, AUTH_CHANGED_EVENT, TENANT_CHANGED_EVENT} from "@/lib/auth/store";

export const DISPLAY_CURRENCIES = ["RUB", "USD", "EUR"] as const;
const EVENT = "budget-display-currency-changed";
const choices = new Map<string, string>();
const valid = (code: string | null): code is string => DISPLAY_CURRENCIES.includes(code as typeof DISPLAY_CURRENCIES[number]);

// A shared browser preference per budget, independent of operation input currency.
export function useDisplayCurrency(explicitTenant?: string) {
  const [selection, setSelection] = useState({tenant: "", code: "RUB"});
  useEffect(() => {
    const sync = () => {
      const tenant = explicitTenant || authStore.getTenant() || "";
      let code = choices.get(tenant) || "RUB";
      try {
        const saved = localStorage.getItem(`budget:display-currency:${tenant}`);
        const legacy = localStorage.getItem(`assets:display-currency:${tenant}`);
        code = valid(saved) ? saved : valid(legacy) ? legacy : "RUB";
      } catch { /* The in-memory preference works with storage disabled. */ }
      setSelection({tenant, code});
    };
    sync();
    window.addEventListener(EVENT, sync);
    window.addEventListener("storage", sync);
    window.addEventListener(AUTH_CHANGED_EVENT, sync);
    window.addEventListener(TENANT_CHANGED_EVENT, sync);
    return () => {
      window.removeEventListener(EVENT, sync);
      window.removeEventListener("storage", sync);
      window.removeEventListener(AUTH_CHANGED_EVENT, sync);
      window.removeEventListener(TENANT_CHANGED_EVENT, sync);
    };
  }, [explicitTenant]);
  const setCurrency = useCallback((code: string) => {
    const tenant = explicitTenant || authStore.getTenant();
    if (!tenant || !valid(code)) return;
    choices.set(tenant, code);
    try { localStorage.setItem(`budget:display-currency:${tenant}`, code); } catch { /* Keep in memory. */ }
    window.dispatchEvent(new Event(EVENT));
  }, [explicitTenant]);
  const current = explicitTenant || (typeof window === "undefined" ? "" : authStore.getTenant());
  return {currency: selection.tenant === current ? selection.code : "RUB", setCurrency, ready: !!current && selection.tenant === current, tenant: current};
}
