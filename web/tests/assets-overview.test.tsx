import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AssetsPage from "@/app/assets/page";
import { authStore } from "@/lib/auth/store";
import en from "@/i18n/en.json";

const { getOverview, listAccounts } = vi.hoisted(() => ({ getOverview: vi.fn(), listAccounts: vi.fn() }));
vi.mock("@/app/providers", () => ({
  ClientsProvider: ({ children }: { children: React.ReactNode }) => children,
  useClients: () => ({ asset: { getOverview, listAccounts } }),
}));
vi.mock("@/lib/auth/store", () => ({ authStore: { getTenant: vi.fn() }, TENANT_CHANGED_EVENT: "tenant-changed", AUTH_CHANGED_EVENT: "auth-changed" }));
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
const accounts = [
  { id: "usd", name: "Dollar account", kind: "bank", institution: "", archived: false, balances: [{ amount: { currencyCode: "USD", minorUnits: 10000n } }] },
  { id: "eur", name: "Euro cash", kind: "cash", institution: "", archived: false, balances: [{ amount: { currencyCode: "EUR", minorUnits: 10000n } }] },
];
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return render(<QueryClientProvider client={client}><AssetsPage /></QueryClientProvider>);
}
beforeEach(() => {
  const storage = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => { storage.set(key, value); },
    clear: () => storage.clear(),
  });
  vi.mocked(authStore.getTenant).mockReturnValue("tenant-a");
  listAccounts.mockResolvedValue({ accounts });
  getOverview.mockReset().mockImplementation(async ({ targetCurrencyCode }: any) => targetCurrencyCode === "USD" ? {
    accounts, total: { currencyCode: "USD", minorUnits: 22500n },
    rates: [{ fromCurrencyCode: "EUR", rateDecimal: "1.25", provider: "cbr", asOf: { seconds: 1735430400n } }],
  } : {
    accounts, total: { currencyCode: "RUB", minorUnits: 2250000n },
    rates: [{ fromCurrencyCode: "USD", rateDecimal: "100", provider: "cbr" }, { fromCurrencyCode: "EUR", rateDecimal: "125", provider: "cbr" }],
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("savings display currency", () => {
  it("converts total, types and account cards, retaining original balances and selection", async () => {
    const view = mount();
    expect(await screen.findByText("RUB 22,500.00")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Display in"), { target: { value: "USD" } });
    expect(await screen.findByText("$225.00")).toBeInTheDocument();
    expect(screen.getAllByText("$125.00")).toHaveLength(2); // cash group and EUR account
    expect(screen.getByText("€100.00")).toBeInTheDocument();
    expect(screen.getAllByText("Original balances")).toHaveLength(2);
    expect(localStorage.getItem("assets:display-currency:tenant-a")).toBe("USD");
    expect(getOverview).toHaveBeenLastCalledWith({ targetCurrencyCode: "USD" });
    view.unmount();
    mount();
    await screen.findByText("$225.00");
    expect(screen.getByLabelText("Display in")).toHaveValue("USD");
  });
  it("does not borrow another budget's selection", async () => {
    localStorage.setItem("assets:display-currency:tenant-b", "USD");
    mount();
    await screen.findByText("RUB 22,500.00");
    expect(screen.getByLabelText("Display in")).toHaveValue("RUB");
  });
  it("marks unconvertible cards and the total as partial", async () => {
    getOverview.mockResolvedValue({ accounts, total: { currencyCode: "RUB", minorUnits: 1000000n }, rates: [{ fromCurrencyCode: "USD", rateDecimal: "100", provider: "cbr" }], incomplete: true, missingCurrencies: ["EUR"] });
    mount();
    await waitFor(() => expect(screen.getAllByText(/partial/i).length).toBeGreaterThan(0));
    expect(screen.getByText("€100.00")).toBeInTheDocument();
  });
});
