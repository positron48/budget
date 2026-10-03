import React from "react";
import { cleanup, render, screen, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AssetsPage from "@/app/assets/page";
import { authStore } from "@/lib/auth/store";
import en from "@/i18n/en.json";

vi.unmock("@/lib/auth/store");
const { getOverview } = vi.hoisted(() => ({ getOverview: vi.fn() }));
vi.mock("@/app/providers", () => ({
  ClientsProvider: ({ children }: { children: React.ReactNode }) => children,
  useClients: () => ({ asset: { getOverview } }),
}));
vi.mock("next-intl", () => ({
  useTranslations: () => (key: string) => key.split(".").reduce((v: any, k) => v?.[k], en.assets) || key,
  useLocale: () => "en-US",
}));
vi.mock("@/components/assets/AssetDialogs", () => ({ default: () => null }));
vi.mock("@/components", () => ({
  Protected: ({ children }: { children: React.ReactNode }) => children,
  Icon: () => null,
  Button: ({ children, onClick }: any) => <button onClick={onClick}>{children}</button>,
}));
const token = (tenant: string) => `header.${btoa(JSON.stringify({ sub: "test-user", tenant_id: tenant })).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "")}.signature`;
const account = (id: string, kind: string, institution = "") => ({
  id, name: id, kind, institution, archived: false,
  balances: [{ amount: { currencyCode: "RUB", minorUnits: 10000n } }],
});
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return render(<QueryClientProvider client={client}><AssetsPage /></QueryClientProvider>);
}
const storageDescriptor = Object.getOwnPropertyDescriptor(window, "localStorage");
beforeEach(() => {
  const data = new Map<string, string>();
  const storage = {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => { data.set(key, value); },
    removeItem: (key: string) => { data.delete(key); },
  };
  vi.stubGlobal("localStorage", storage);
  Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  getOverview.mockReset().mockResolvedValue({
    accounts: [account("Cash one", "cash"), account("Cash two", "cash"), account("Bank account", "bank", "Bank")],
    total: { currencyCode: "RUB", minorUnits: 30000n }, rates: [],
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  if (storageDescriptor) Object.defineProperty(window, "localStorage", storageDescriptor);
});

describe("savings with an existing authenticated session", () => {
  it.each([undefined, ""])("loads two cash accounts without location and a bank when saved tenant is %s", async (saved) => {
    authStore.set({ accessToken: token("default-tenant"), ...(saved === undefined ? {} : { tenantId: saved }) });
    mount();
    expect(await screen.findByText("Cash one")).toBeInTheDocument();
    expect(screen.getByText("Cash two")).toBeInTheDocument();
    expect(screen.getByText("Bank account")).toBeInTheDocument();
    expect(screen.getByText("RUB 300.00")).toBeInTheDocument();
    expect(getOverview).toHaveBeenCalledTimes(1);
  });
  it("reacts when a refreshed token supplies the tenant after mounting", async () => {
    mount();
    act(() => authStore.set({ accessToken: token("default-tenant") }));
    await waitFor(() => expect(getOverview).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("Cash one")).toBeInTheDocument();
  });
  it("keeps an explicit budget selection ahead of the token default", () => {
    authStore.set({ accessToken: token("default-tenant"), tenantId: "selected-tenant" });
    expect(authStore.getTenant()).toBe("selected-tenant");
    authStore.set({ accessToken: token("new-default") });
    expect(authStore.getTenant()).toBe("selected-tenant");
  });
  it("handles missing or malformed tokens and clears the derived tenant on logout", () => {
    expect(authStore.getTenant()).toBeUndefined();
    authStore.set({ accessToken: "not-a-token" });
    expect(authStore.getTenant()).toBeUndefined();
    authStore.set({ accessToken: token("default-tenant") });
    expect(authStore.getTenant()).toBe("default-tenant");
    authStore.clear();
    expect(authStore.getTenant()).toBeUndefined();
  });
});
