import type {InvestmentQuote} from "@/proto/budget/v1/investment_nav_pb";
import type {TrackerQuote} from "./portfolio-tracker";

// Exchange timestamps and broker-file download times are different clocks.
// Prefer an eligible normalized exchange quote to the broker-file fallback,
// including when their native price currencies differ.
export function portfolioQuotes(fallback: Map<string, TrackerQuote>, market: InvestmentQuote[], now = Date.now()): Map<string, TrackerQuote> {
  const out = new Map(fallback);
  for (const q of market) {
    if (q.state !== "fresh" || !q.cleanPrice || !q.priceAt || !["RUB", "USD", "EUR"].includes(q.currency)) continue;
    const at = Number(q.priceAt.seconds) * 1000 + q.priceAt.nanos / 1000000;
    if (!Number.isFinite(at) || at > now || now - at > 96 * 3600000 || BigInt(q.cleanPrice.numerator) <= 0n || BigInt(q.cleanPrice.denominator) <= 0n) continue;
    const existing = [...out].find(([key]) => key.startsWith(`${q.instrumentKey}\0`))?.[1];
    for (const key of out.keys()) if (key.startsWith(`${q.instrumentKey}\0`)) out.delete(key);
    out.set(`${q.instrumentKey}\0${q.currency}`, {price: q.cleanPrice, at, name: q.instrument?.name||existing?.name, ticker: q.instrument?.ticker||existing?.ticker, source: "exchange"});
  }
  return out;
}
