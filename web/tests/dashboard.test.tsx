import React from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import HomePage from "@/app/page";

const { listTransactions, getMonthlySummary } = vi.hoisted(() => ({
  listTransactions: vi.fn(),
  getMonthlySummary: vi.fn(),
}));

vi.mock("@/app/providers", () => ({
  ClientsProvider: ({ children }: { children: React.ReactNode }) => children,
  useClients: () => ({ transaction: { listTransactions }, report: { getMonthlySummary } }),
}));
vi.mock("next-intl", () => ({
  useTranslations: () => (key: string) => key,
  useLocale: () => "en",
}));
vi.mock("@/app/transactions/NewTransactionForm", () => ({ default: () => null }));

describe("dashboard recent transactions", () => {
  beforeEach(() => {
    listTransactions.mockReset();
    getMonthlySummary.mockResolvedValue({ items: [] });
  });
  afterEach(cleanup);

  function renderDashboard() {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    render(<QueryClientProvider client={queryClient}><HomePage /></QueryClientProvider>);
  }

  it("renders transactions from the API response with their date and amount", async () => {
    listTransactions.mockResolvedValue({ transactions: [{
      id: "recent-1", type: 2, comment: "Coffee", occurredAt: { seconds: BigInt(1790985600) },
      amount: { minorUnits: BigInt(25000), currencyCode: "RUB" },
    }] });
    renderDashboard();

    expect(await screen.findByText("Coffee")).toBeInTheDocument();
    expect(screen.getByText(/250,00/)).toBeInTheDocument();
    expect(screen.getByText(new Date(1790985600 * 1000).toLocaleDateString("en"))).toBeInTheDocument();
    expect(screen.queryByText("noRecent")).not.toBeInTheDocument();
    expect(listTransactions).toHaveBeenCalledWith({ page: { page: 1, pageSize: 5, sort: "occurred_at desc" } });
  });

  it("shows the empty state when the API returns no transactions", async () => {
    listTransactions.mockResolvedValue({ transactions: [] });
    renderDashboard();
    expect(await screen.findByText("noRecent")).toBeInTheDocument();
  });

  it("shows a load error instead of an empty list and allows retrying", async () => {
    listTransactions.mockRejectedValueOnce(new Error("service unavailable"))
      .mockResolvedValueOnce({ transactions: [] });
    renderDashboard();
    expect(await screen.findByRole("alert")).toHaveTextContent("recentLoadError");
    expect(screen.queryByText("noRecent")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "retry" }));
    expect(await screen.findByText("noRecent")).toBeInTheDocument();
    expect(listTransactions).toHaveBeenCalledTimes(2);
  });
});
