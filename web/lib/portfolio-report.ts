import type {InvestmentOperation, InvestmentSnapshot} from "@/proto/budget/v1/investment_pb";
import type {GetInvestmentExchangeRatesResponse} from "@/proto/budget/v1/investment_nav_pb";
import {add, decimal, divide, multiply, negate, payoutKinds, zero, type TrackerPosition, type TrackerQuote} from "./portfolio-tracker";
import type {Fraction} from "./investment-positions";

export function reportingDate(seconds?: bigint): string {
  if (seconds === undefined) return "";
  return new Date(Number(seconds) * 1000).toLocaleDateString("en-CA", {timeZone: "Europe/Moscow"});
}
function reportPrincipal(op: InvestmentOperation) {
  const f = op.tradeFinancials, principal = f?.principal;
  if (!f) return {amount: decimal(op.amount), currency: op.currency, date: op.kind === "opening_position" ? op.acquiredAt ? reportingDate(op.acquiredAt.seconds) : undefined : op.occurredAt ? reportingDate(op.occurredAt.seconds) : undefined};
  if (!principal) return;
  const buy = op.kind === "buy";
  let expected = decimal(principal.amount);
  for (const [m, expense] of [[f.accrued, false], [f.fee, true], [f.tax, true]] as const) {
    if (!m || m.currency !== principal.currency || m.at?.seconds !== principal.at?.seconds || m.at?.nanos !== principal.at?.nanos || expense && !f.actualSettlementIncludesExpenses) continue;
    const amount = decimal(m.amount);
    expected = add(expected, expense && !buy && amount ? negate(amount) : amount);
  }
  const settlement = decimal(f.actualSettlement);
  return {amount: settlement && expected ? add(decimal(principal.amount), settlement, negate(expected)) : undefined,
    currency: principal.currency, date: principal.at ? reportingDate(principal.at.seconds) : undefined};
}
export type ReportPosition = TrackerPosition & {incomeComplete: boolean};
export function portfolioReport(snapshots: InvestmentSnapshot[], operations: InvestmentOperation[], quotes: Map<string, TrackerQuote>, currency: string, fx?: GetInvestmentExchangeRatesResponse) {
  const rates = new Map((fx?.currency === currency ? fx.rates : []).map(r => [`${r.date}\0${r.fromCurrency}`, r.rate]));
  const missingFX = new Set<string>(), positions = new Map<string, ReportPosition>();
  const cash = new Map<string, Fraction>(), economicCost = new Map<string, Fraction>();
  const incompleteCash = new Set<string>(), incompleteCost = new Set<string>(), incompleteEconomicCost = new Set<string>();
  const months = new Map<string, Fraction>();
  let monthsComplete = true;
  const convert = (value: Fraction | undefined, from: string, date: string | undefined): Fraction | undefined => {
    if (!value) return;
    if (!BigInt(value.numerator) || from === currency) return value;
    if (date === undefined) {missingFX.add(`missing-date:${from}/${currency}`); return;}
    const rate = rates.get(`${date}\0${from}`);
    if (!rate || BigInt(rate.numerator) <= 0n || BigInt(rate.denominator) <= 0n) {missingFX.add(`${date || "current"}:${from}/${currency}`); return;}
    return multiply(value, rate);
  };
  const get = (key: string) => {
    let p = positions.get(key);
    if (!p) {p = {key, currency, quantity: zero, cost: zero, income: zero, incomeComplete: true, recoveredAccrued: zero, accounts: []}; positions.set(key, p);}
    return p;
  };
  const now = fx?.readAt?.seconds ?? BigInt(Math.floor(Date.now() / 1000));
  const posted = operations.filter(o => o.status === "posted" && o.occurredAt && o.occurredAt.seconds <= now);
  const byID = new Map(posted.map(o => [o.id, o]));
  for (const s of snapshots) for (const holding of s.positions) {
    const p = get(holding.instrumentKey), quantity = decimal(holding.quantity);
    if (!quantity) {incompleteCost.add(p.key); incompleteCash.add(p.key); continue;}
    p.quantity = add(p.quantity, quantity);
    if (s.ledger?.name && !p.accounts.includes(s.ledger.name)) p.accounts.push(s.ledger.name);
    if (!BigInt(quantity.numerator)) continue;
    if (!holding.lots.length) {incompleteCost.add(p.key); incompleteEconomicCost.add(p.key); continue;}
    for (const lot of holding.lots) {
      const origin = byID.get(lot.acquisitionOperationId || lot.originId);
      const clean = decimal(lot.cleanCost), accrued = decimal(lot.acquiredAccrued);
      // FIFO clean book cost includes acquisition fees. Recover pure principal
      // by its fraction of original capitalized cost, independent of later splits.
      const principal = origin && decimal(origin.amount), fee = origin && decimal(origin.fee);
      const remaining = principal && fee && clean && divide(clean, holding.costBasis === "principal" ? principal : add(principal, fee));
      const source = origin && reportPrincipal(origin);
      const pure = source?.amount && remaining ? convert(multiply(source.amount, remaining), source.currency, lot.acquiredAt ? reportingDate(lot.acquiredAt.seconds) : source.date) : undefined;
      const f = origin?.tradeFinancials;
      const date = origin?.occurredAt ? reportingDate(origin.occurredAt.seconds) : undefined;
      const feeCost = fee && remaining ? convert(multiply(decimal(f?.fee?.amount || origin?.fee || "" ) || zero, remaining), f?.fee?.currency || origin?.feeCurrency || origin?.currency || lot.currency || holding.currency, f?.fee?.at ? reportingDate(f.fee.at.seconds) : date) : undefined;
      const originalAccrued = origin && decimal(origin.accrued);
      const accruedShare = accrued && originalAccrued ? BigInt(originalAccrued.numerator) === 0n ? zero : divide(accrued, originalAccrued) : undefined;
      const interestCost = accruedShare ? convert(multiply(decimal(f?.accrued?.amount || origin?.accrued || "") || zero, accruedShare), f?.accrued?.currency || origin?.currency || lot.currency || holding.currency, f?.accrued?.at ? reportingDate(f.accrued.at.seconds) : date) : undefined;
      const economic = pure && feeCost && interestCost ? add(pure, feeCost, interestCost) : undefined;
      if (pure) p.cost = add(p.cost, pure); else incompleteCost.add(p.key);
      if (economic) economicCost.set(p.key, add(economicCost.get(p.key), economic)); else incompleteEconomicCost.add(p.key);
    }
  }
  const addCash = (key: string, amount: Fraction | undefined, from: string, date: string | undefined, negative = false) => {
    const converted = convert(amount, from, date);
    if (!converted) {incompleteCash.add(key); return;}
    cash.set(key, add(cash.get(key), negative ? negate(converted) : converted));
  };
  for (const op of posted) {
    if (["deposit", "withdrawal", "tax", "tax_refund", "opening_cash", "cash_reconciliation", "fx", "transfer_in", "transfer_out"].includes(op.kind)) continue;
    if (!op.instrumentKey && !payoutKinds.has(op.kind) && op.kind !== "fee") continue;
    const p = get(op.instrumentKey), date = reportingDate(op.occurredAt?.seconds);
    if (payoutKinds.has(op.kind)) {
      const income = convert(decimal(op.amount), op.currency, date);
      if (!income) {p.incomeComplete = false; monthsComplete = false;} else {
        p.income = add(p.income, income);
        const month = date.slice(0, 7); months.set(month, add(months.get(month), income));
      }
      addCash(p.key, decimal(op.amount), op.currency, date);
    } else if (op.kind === "fee") {
      addCash(p.key, decimal(op.amount), op.currency, date, true);
    } else if (["buy", "sell", "redemption", "opening_position"].includes(op.kind)) {
      const buy = op.kind === "buy" || op.kind === "opening_position";
      if (op.kind === "opening_position" && (!op.openingCostKnown || !op.openingAccruedKnown)) {incompleteCash.add(p.key); continue;}
      const f = op.tradeFinancials;
      if (!f) {
        const at = op.kind === "opening_position" ? op.acquiredAt ? reportingDate(op.acquiredAt.seconds) : undefined : date;
        addCash(p.key, decimal(op.amount), op.currency, at, buy);
        addCash(p.key, decimal(op.accrued), op.currency, at, buy);
        addCash(p.key, decimal(op.fee), op.feeCurrency || op.currency, at, true);
      } else {
        // Statement principal adjustment is in the principal's native currency.
        // Expense components have their own currency and effective date.
        const principal = reportPrincipal(op);
        if (!principal) {incompleteCash.add(p.key); continue;}
        addCash(p.key, principal.amount, principal.currency, principal.date, buy);
        for (const [m, negative] of [[f.accrued, buy], [f.fee, true]] as const) {
          if (!m) {incompleteCash.add(p.key); continue;}
          addCash(p.key, decimal(m.amount), m.currency, reportingDate(m.at?.seconds), negative);
        }
      }
    } else if (op.kind !== "split") {
      // Corporate distributions and transfers need an explicit cost-allocation
      // projection. Never silently treat their cash or acquisition basis as zero.
      incompleteCash.add(p.key);
    }
  }
  for (const p of positions.values()) {
    if (incompleteCost.has(p.key)) p.cost = undefined;
    const available = [...quotes].filter(([key]) => key.startsWith(`${p.key}\0`)).sort((a, b) => b[1].at - a[1].at);
    const quote = available[0];
    if (!BigInt(p.quantity.numerator)) p.value = zero;
    else if (quote) {
      const nativeCurrency = quote[0].slice(quote[0].lastIndexOf("\0") + 1);
      p.price = convert(quote[1].price, nativeCurrency, "");
      p.quoteAt = quote[1].at; p.quoteSource = quote[1].source; p.name = quote[1].name; p.ticker = quote[1].ticker;
      if (p.price) p.value = multiply(p.quantity, p.price);
    }
    if (!incompleteCash.has(p.key) && p.value) p.profit = add(p.value, cash.get(p.key));
    if (!incompleteCash.has(p.key) && !incompleteEconomicCost.has(p.key)) p.realized = add(cash.get(p.key), economicCost.get(p.key), negate(p.income));
  }
  return {positions: [...positions.values()], months: [...months].sort(([a], [b]) => a.localeCompare(b)).slice(-12), monthsComplete, missingFX: [...missingFX]};
}
