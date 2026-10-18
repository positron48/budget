import React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, renderHook, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { create } from "@bufbuild/protobuf";
import { PaymentSummarySchema } from "@/proto/budget/v1/recurring_payment_pb";
import PaymentSummary from "@/components/payments/PaymentSummary";
import { usePaymentMonth } from "@/lib/payments";
import messages from "@/i18n/en.json";
vi.mock("next-intl", () => ({
  useLocale: () => "en",
  useTranslations:
    () =>
    (key: string, values: Record<string, unknown> = {}) => {
      let text = (messages.payments as Record<string, string>)[key] || key;
      for (const [k, v] of Object.entries(values))
        text = text.replace(`{${k}}`, String(v));
      return text;
    },
  NextIntlClientProvider: ({ children }: { children: React.ReactNode }) =>
    children,
}));
afterEach(() => vi.useRealTimers());
describe("recurring-payments", () => {
  it("refreshes the calendar month while the app stays open", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 9, 31, 23, 59, 50));
    const { result } = renderHook(() => usePaymentMonth());
    expect(result.current).toBe("2026-10");
    act(() => {
      vi.advanceTimersByTime(30000);
    });
    expect(result.current).toBe("2026-11");
  });
  it("refreshes on focus after a suspended tab crosses months", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 9, 31));
    const { result } = renderHook(() => usePaymentMonth());
    vi.setSystemTime(new Date(2026, 10, 1));
    act(() => window.dispatchEvent(new Event("focus")));
    expect(result.current).toBe("2026-11");
  });
  it("keeps unknown amounts and review counts visible", () => {
    const summary = create(PaymentSummarySchema, {
      exact: { currencyCode: "RUB", minorUnits: 10000n },
      estimated: { currencyCode: "RUB", minorUnits: 20000n },
      paid: { currencyCode: "RUB", minorUnits: 0n },
      total: 4,
      outstanding: 3,
      unknown: 1,
      needsReview: 1,
      missingFx: 1,
    });
    render(
      <NextIntlClientProvider locale="en" messages={messages}>
        <PaymentSummary summary={summary} month="2026-10" />
      </NextIntlClientProvider>,
    );
    expect(
      screen.getByText("1 more payments without an amount"),
    ).toBeInTheDocument();
    expect(screen.getByText("Need review: 1")).toBeInTheDocument();
    expect(screen.getByText(/Total incomplete/)).toBeInTheDocument();
    expect(
      screen.queryByText(/All payments for this month are settled/),
    ).not.toBeInTheDocument();
  });
});
