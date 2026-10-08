import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import FxPage from "@/app/fx/page";

vi.unmock("@/app/providers");
vi.mock("@/lib/api/transport", () => ({ createTransport: vi.fn() }));
const { batchGetRates } = vi.hoisted(() => ({
  batchGetRates: vi.fn().mockResolvedValue({ rates: [{ fromCurrencyCode: "USD", toCurrencyCode: "RUB", rateDecimal: "80.25", provider: "cbr", asOf: { seconds: 1791417600n } }] }),
}));
vi.mock("@/lib/api/clients", () => ({ createClients: () => ({ fx: { batchGetRates, upsertRate: vi.fn() } }) }));
afterEach(cleanup);

describe("FX page with the real query provider", () => {
  it("renders protobuf timestamps and refetches after changing the date", async () => {
    render(<FxPage />);
    expect(await screen.findByText("80.25")).toBeInTheDocument();
    expect(batchGetRates).toHaveBeenCalledWith(expect.objectContaining({ asOf: { seconds: expect.any(BigInt) } }));
    const date = document.querySelector('input[type="date"]')!;
    fireEvent.change(date, { target: { value: "2026-10-01" } });
    await waitFor(() => expect(batchGetRates).toHaveBeenLastCalledWith(expect.objectContaining({ asOf: { seconds: 1790812800n } })));
  });
});
