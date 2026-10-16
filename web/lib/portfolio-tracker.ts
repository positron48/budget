import type {InvestmentOperation, InvestmentSnapshot} from "@/proto/budget/v1/investment_pb";
import {decimalFraction, positionDecimal, positionSum, type Fraction} from "./investment-positions";

export const trackerKinds = new Set(["buy", "sell", "dividend", "coupon"]);
export const payoutKinds = new Set(["dividend", "coupon", "repo_income", "cash_interest"]);
export const zero: Fraction = {numerator: "0", denominator: "1"};
export const add = (...values: (Fraction | undefined)[]) => positionSum(values).known;
export const decimal = (value: string) => decimalFraction(value);
export const negate = (value: Fraction): Fraction => ({...value, numerator: (-BigInt(value.numerator)).toString()});
export function multiply(a: Fraction, b: Fraction): Fraction {
  return {numerator: (BigInt(a.numerator) * BigInt(b.numerator)).toString(), denominator: (BigInt(a.denominator) * BigInt(b.denominator)).toString()};
}
export function divide(a: Fraction, b: Fraction): Fraction | undefined {
  const n = BigInt(b.numerator);
  if (!n) return;
  const sign = n < 0n ? -1n : 1n;
  return {numerator: (BigInt(a.numerator) * BigInt(b.denominator) * sign).toString(), denominator: (BigInt(a.denominator) * n * sign).toString()};
}
export function tradeAmount(quantity: string, price: string): string {
  const q = decimal(quantity.replace(",", ".")), p = decimal(price.replace(",", "."));
  if (!q || !p || BigInt(q.numerator) <= 0n || BigInt(p.numerator) <= 0n) throw new Error("positive_trade_values");
  const value = multiply(q, p);
  // Storage accepts 12 decimal places. Reject excess precision, never silently round a trade.
  const amount = positionDecimal(value, 28), [, fraction = ""] = amount.split(".");
  const parsed = decimal(amount);
  if (fraction.length > 12 || !parsed || BigInt(parsed.numerator) * BigInt(value.denominator) !== BigInt(value.numerator) * BigInt(parsed.denominator)) throw new Error("trade_precision");
  return amount;
}

export type TrackerQuote = {price: Fraction; at: number; name?: string; ticker?: string; source?: "exchange" | "broker_snapshot" | "estimate"};
export type TrackerPosition = {
  key: string; currency: string; quantity: Fraction; cost?: Fraction; realized?: Fraction;
  income: Fraction; recoveredAccrued: Fraction; value?: Fraction; price?: Fraction; profit?: Fraction;
  accounts: string[]; name?: string; ticker?: string; quoteAt?: number; quoteSource?: "exchange" | "broker_snapshot" | "estimate";
};
export function trackerPositions(snapshots: InvestmentSnapshot[], operations: InvestmentOperation[], quotes = new Map<string, TrackerQuote>()): TrackerPosition[] {
  const positions = new Map<string, TrackerPosition>();
  for (const snapshot of snapshots) for (const p of snapshot.positions) {
    const key = `${p.instrumentKey}\0${p.currency}`;
    const quantity = decimal(p.quantity);
    if (!quantity) continue;
    const cost = p.cleanCost !== "" && p.acquiredAccrued !== "" ? add(decimal(p.cleanCost), decimal(p.acquiredAccrued)) : undefined;
    const realized = p.realizedClean !== "" && p.realizedAccrued !== "" ? add(decimal(p.realizedClean), decimal(p.realizedAccrued)) : undefined;
    const previous = positions.get(key);
    positions.set(key, {
      key: p.instrumentKey, currency: p.currency, quantity: add(previous?.quantity, quantity),
      cost: previous ? previous.cost && cost ? add(previous.cost, cost) : undefined : cost,
      realized: previous ? previous.realized && realized ? add(previous.realized, realized) : undefined : realized,
      income: previous?.income || zero, recoveredAccrued: add(previous?.recoveredAccrued, decimal(p.couponRecovered || "0")), accounts: [...(previous?.accounts || []), snapshot.ledger?.name || ""],
    });
  }
  for (const op of operations) {
    if (op.status !== "posted" || !payoutKinds.has(op.kind)) continue;
    const key = `${op.instrumentKey}\0${op.currency}`;
    let position = positions.get(key);
    if (!position) {
      position = {key: op.instrumentKey, currency: op.currency, quantity: zero, cost: zero, realized: zero, income: zero, recoveredAccrued: zero, accounts: []};
      positions.set(key, position);
    }
    // Payouts are amounts supplied by the user/import, independent of a cash or tax book.
    position.income = add(position.income, decimal(op.amount));
  }
  for (const p of positions.values()) {
    const quote = quotes.get(`${p.key}\0${p.currency}`);
    if (quote) {
      p.price = quote.price; p.name = quote.name; p.ticker = quote.ticker; p.quoteAt = quote.at; p.quoteSource = quote.source;
      p.value = multiply(p.quantity, quote.price);
      if (p.cost && p.realized) p.profit = add(p.value, negate(p.cost), p.realized, p.income, negate(p.recoveredAccrued));
    } else if (BigInt(p.quantity.numerator) === 0n && p.realized) {
      p.value = zero; p.profit = add(p.realized, p.income, negate(p.recoveredAccrued));
    }
  }
  return [...positions.values()];
}

export function payoutMonths(operations: InvestmentOperation[], currency: string) {
  const months = new Map<string, Fraction>();
  for (const op of operations) if (op.status === "posted" && op.currency === currency && payoutKinds.has(op.kind) && op.occurredAt) {
    const month = new Date(Number(op.occurredAt.seconds) * 1000).toLocaleDateString("en-CA", {year: "numeric", month: "2-digit", timeZone: "Europe/Moscow"});
    months.set(month, add(months.get(month), decimal(op.amount)));
  }
  return [...months].sort(([a], [b]) => a.localeCompare(b)).slice(-12);
}

export function trackerError(error: unknown, ru: boolean): string {
  const message = error instanceof Error ? error.message : String(error);
  if (/short sale|existing position|entire position/.test(message)) return ru ? "Не хватает истории покупок этой бумаги. Добавьте недостающую покупку или начальную позицию." : "Purchase history is incomplete for this security. Add the missing purchase or opening position.";
  if (/precedes ledger opening/.test(message)) return ru ? "Операция раньше начала истории счёта. Проверьте дату." : "The operation predates the account history. Check its date.";
  if (/pending_write_changed/.test(message)) return ru ? "Результат предыдущего сохранения ещё не подтверждён. Обновите портфель перед изменением операции." : "The previous save is not confirmed. Refresh the portfolio before changing the operation.";
  if (/positive_trade_values|invalid decimal/.test(message)) return ru ? "Укажите положительные количество и цену." : "Enter a positive quantity and price.";
  if (/trade_precision|precision exceeded/.test(message)) return ru ? "Слишком много знаков после запятой. Проверьте сумму." : "Too many decimal places. Check the amount.";
  if (/instrument cost currency changed/.test(message)) return ru ? "У бумаги операции в разных валютах. Нужен пересчёт стоимости по курсам на даты сделок." : "This security has trades in different currencies. Translate acquisition costs using historical exchange rates.";
  if (/changed|conflict|aborted/i.test(message)) return ru ? "Данные изменились. Обновите страницу и повторите." : "Data changed. Refresh and retry.";
  return ru ? "Не удалось сохранить изменения. Попробуйте ещё раз; сохранённая история останется на месте." : "Could not save changes. Retry; existing history is preserved.";
}
