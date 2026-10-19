import {describe, expect, it} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentQuoteSchema} from "@/proto/budget/v1/investment_nav_pb";
import {portfolioQuotes} from "@/lib/portfolio-quotes";
import {decimal} from "@/lib/portfolio-tracker";
import {positionDecimal} from "@/lib/investment-positions";

describe("portfolio market quote selection", () => {
  const now = Date.parse("2026-02-01T12:00:00Z");
  const market = () => create(InvestmentQuoteSchema, {instrumentKey:"PAPER", currency:"USD", cleanPrice:decimal("10"), state:"fresh", priceAt:{seconds:BigInt(now/1000-60)}});
  it("uses normalized exchange prices outside broker holdings", () => {
    const result = portfolioQuotes(new Map(), [market()], now);
    expect(positionDecimal(result.get("PAPER\0USD")?.price)).toBe("10");
    expect(result.get("PAPER\0USD")?.source).toBe("exchange");
  });
  it("uses an explicitly normalized last-known exchange price with its original age", () => {
    const q=market();q.state="estimated";q.source="tinvest/personal";q.issues=["last_known_price"];q.priceAt!.seconds-=365n*86400n;
    const selected=portfolioQuotes(new Map(),[q],now).get("PAPER\0USD");
    expect(positionDecimal(selected?.price)).toBe("10");
    expect(selected?.source).toBe("estimate");
    expect(selected?.at).toBe(Number(q.priceAt!.seconds)*1000);
  });
  it("does not compare exchange time with file download time or retain a second quote currency", () => {
    const result = portfolioQuotes(new Map([["PAPER\0RUB", {price:decimal("999")!,at:now,source:"broker_snapshot"}]]), [market()], now);
    expect(result.has("PAPER\0RUB")).toBe(false);
    expect(result.get("PAPER\0USD")?.at).toBe(now-60000);
  });
  it("keeps the dated broker-file fallback if market evidence is unavailable or ineligible", () => {
    for (const kind of ["stale","future","old","zero"]) {
      const q = market();
      if (kind === "stale") q.state = "stale";
      if (kind === "future") q.priceAt!.seconds += 120n;
      if (kind === "old") q.priceAt!.seconds -= 97n*3600n;
      if (kind === "zero") q.cleanPrice = decimal("0") as never;
      const result = portfolioQuotes(new Map([["PAPER\0RUB", {price:decimal("100")!,at:now,source:"broker_snapshot"}]]), [q], now);
      expect(positionDecimal(result.get("PAPER\0RUB")?.price)).toBe("100");
      expect(result.has("PAPER\0USD")).toBe(false);
    }
  });
});

it("uses reviewed dated estimates, including zero, without making stale exchange quotes fresh", () => {
 const now=Date.parse("2026-10-16T12:00:00Z"), at=Date.parse("2025-08-28T00:00:00Z");
 const q=create(InvestmentQuoteSchema,{instrumentKey:"DELISTED",currency:"RUB",cleanPrice:decimal("0")!,state:"estimated",source:"manual_estimate",priceAt:{seconds:BigInt(at/1000)}});
 const selected=portfolioQuotes(new Map(),[q],now).get("DELISTED\0RUB");
 expect(selected?.source).toBe("estimate");expect(selected?.at).toBe(at);expect(positionDecimal(selected?.price)).toBe("0");
 q.state="stale";expect(portfolioQuotes(new Map(),[q],now).size).toBe(0);
});
