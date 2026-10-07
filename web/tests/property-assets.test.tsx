import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AssetDialogs from "@/components/assets/AssetDialogs";
import AssetAccountSelect from "@/components/assets/AssetAccountSelect";
import AssetAccountPage from "@/app/assets/[id]/page";
import type { AssetAccount } from "@/proto/budget/v1/asset_pb";
import en from "@/i18n/en.json";

const { asset } = vi.hoisted(() => ({
  asset: {
    createAccount: vi.fn(),
    createSnapshot: vi.fn(),
    getAccount: vi.fn(),
    listAccounts: vi.fn(),
    listAccountHistory: vi.fn(),
  },
}));
vi.mock("@/app/providers", () => ({
  ClientsProvider: ({ children }: { children: React.ReactNode }) => children,
  useClients: () => ({ asset }),
}));
vi.mock("@/lib/auth/store", () => ({
  authStore: { getTenant: () => "tenant-a" },
  TENANT_CHANGED_EVENT: "tenant-changed",
  AUTH_CHANGED_EVENT: "auth-changed",
}));
vi.mock("next-intl", () => ({
  useTranslations: () => (key: string) =>
    key.split(".").reduce((v: any, k) => v?.[k], en.assets) || key,
  useLocale: () => "en-US",
}));
vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "property" }),
  useRouter: () => ({ push: vi.fn() }),
}));
vi.mock("@/app/transactions/NewTransactionForm", () => ({
  default: () => null,
}));
vi.mock("@/components", () => ({
  Protected: ({ children }: { children: React.ReactNode }) => children,
  Icon: () => null,
  ConfirmDialog: () => null,
  Modal: ({ children, title }: any) => (
    <div role="dialog" aria-label={title}>
      {children}
    </div>
  ),
  Button: ({ children, onClick, type, disabled, loading }: any) => (
    <button type={type} disabled={disabled || loading} onClick={onClick}>
      {children}
    </button>
  ),
}));
const property = {
  id: "property",
  name: "Apartment",
  kind: "property",
  fixedCurrencyCode: "RUB",
  version: 1n,
  institution: "Moscow",
  balances: [{ amount: { currencyCode: "RUB", minorUnits: 1500000000n } }],
} as AssetAccount;
const bank = {
  ...property,
  id: "bank",
  name: "Bank",
  kind: "bank",
  fixedCurrencyCode: "",
};
const broker = {
  ...property,
  id: "broker",
  name: "Broker",
  kind: "investment",
};
function mount(ui: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  asset.createAccount.mockResolvedValue({ account: property });
  asset.createSnapshot.mockResolvedValue({});
  asset.getAccount.mockResolvedValue({ account: property });
  asset.listAccounts.mockResolvedValue({ accounts: [property, bank, broker] });
  asset.listAccountHistory.mockResolvedValue({
    items: [],
    page: { totalPages: 0, totalItems: 0n },
  });
});
afterEach(cleanup);

describe("property assets", () => {
  it("creates property with one valuation currency and rejects a negative estimate", async () => {
    mount(
      <AssetDialogs
        action={{ mode: "account" }}
        accounts={[]}
        onClose={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByLabelText("Asset type"), {
      target: { value: "property" },
    });
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Apartment" },
    });
    expect(
      screen.getByLabelText("Location or description"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Add currency")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Valuation currency"), {
      target: { value: "USD" },
    });
    fireEvent.change(screen.getByLabelText("Amount"), {
      target: { value: "-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add asset" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      en.assets.errors.propertyValue,
    );
    expect(asset.createAccount).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Amount"), {
      target: { value: "150 000,50" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add asset" }));
    await waitFor(() =>
      expect(asset.createAccount).toHaveBeenCalledWith(
        expect.objectContaining({
          account: expect.objectContaining({
            name: "Apartment",
            kind: "property",
            fixedCurrencyCode: "USD",
          }),
          openingBalances: [
            expect.objectContaining({
              amount: { currencyCode: "USD", minorUnits: 15000050n },
              kind: "opening",
            }),
          ],
        }),
      ),
    );
  });
  it("uses manual valuations in the detail page without monetary actions", async () => {
    mount(<AssetAccountPage />);
    await screen.findByRole("heading", { name: "Apartment" });
    for (const name of [
      "Top up",
      "Withdraw",
      "Expense",
      "Income",
      "Currency exchange",
    ]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
    fireEvent.click(screen.getByRole("button", { name: "Update valuation" }));
    expect(screen.getByLabelText("Currency")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Estimated value"), {
      target: { value: "16000000" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(asset.createSnapshot).toHaveBeenCalledWith(
        expect.objectContaining({
          snapshot: expect.objectContaining({
            accountId: "property",
            kind: "valuation",
            amount: { currencyCode: "RUB", minorUnits: 1600000000n },
          }),
        }),
      ),
    );
  });
  it("excludes property from transfer and transaction account selectors", async () => {
    const view = mount(
      <AssetDialogs
        action={{ mode: "transfer" }}
        accounts={[property, bank, broker]}
        onClose={vi.fn()}
      />,
    );
    expect(
      screen.queryByRole("option", { name: "Apartment" }),
    ).not.toBeInTheDocument();
    expect(screen.getAllByRole("option", { name: "Broker" })).toHaveLength(2);
    view.unmount();
    mount(<AssetAccountSelect value="" onChange={vi.fn()} currency="RUB" />);
    await screen.findByRole("option", { name: "Bank" });
    expect(
      screen.queryByRole("option", { name: "Apartment" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "Broker" }),
    ).not.toBeInTheDocument();
  });
});
