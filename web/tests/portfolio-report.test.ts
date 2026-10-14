import {describe, expect, it} from "vitest";
import {create} from "@bufbuild/protobuf";
import {TimestampSchema} from "@bufbuild/protobuf/wkt";
import {InvestmentOperationSchema, InvestmentSnapshotSchema} from "@/proto/budget/v1/investment_pb";
import {GetInvestmentExchangeRatesResponseSchema} from "@/proto/budget/v1/investment_nav_pb";
import {portfolioReport} from "@/lib/portfolio-report";
import {decimal} from "@/lib/portfolio-tracker";
import {positionDecimal} from "@/lib/investment-positions";
const at = (date: string) => create(TimestampSchema, {seconds: BigInt(Date.parse(date + "T10:00:00Z") / 1000), nanos: 0});
const fx = () => create(GetInvestmentExchangeRatesResponseSchema, {currency:"RUB", mode:"market", readAt:at("2026-03-01"), rates:[
  {date:"2026-01-01", fromCurrency:"USD", rate:decimal("80")},
  {date:"2026-02-01", fromCurrency:"USD", rate:decimal("90")},
  {date:"", fromCurrency:"USD", rate:decimal("100")},
]});
const buy = () => create(InvestmentOperationSchema, {id:"buy", ledgerId:"one", status:"posted", kind:"buy", instrumentKey:"US", currency:"USD", amount:"200", quantity:"2", fee:"2", accrued:"4", occurredAt:at("2026-01-01")});
const snapshot = () => create(InvestmentSnapshotSchema, {ledger:{id:"one", name:"Broker"}, positions:[{instrumentKey:"US", currency:"USD", quantity:"1", cleanCost:"101", acquiredAccrued:"2", lots:[{originId:"buy", acquiredAt:at("2026-01-01"), quantity:"1", cleanCost:"101", acquiredAccrued:"2"}]}]});
const quote = () => new Map([["US\0USD", {price:decimal("140")!, at:Date.parse("2026-03-01")}]]);

describe("portfolio reporting currency", () => {
  it("translates remaining principal at acquisition FX, cash flows at their own dates and market value at current FX", () => {
    const sell = create(InvestmentOperationSchema, {id:"sell", ledgerId:"one", status:"posted", kind:"sell", instrumentKey:"US", currency:"USD", amount:"120", quantity:"1", fee:"1", accrued:"3", occurredAt:at("2026-02-01")});
    const payout = create(InvestmentOperationSchema, {id:"payout", status:"posted", kind:"coupon", instrumentKey:"US", currency:"USD", amount:"10", tax:"2", occurredAt:at("2026-02-01")});
    const report = portfolioReport([snapshot()], [buy(), sell, payout], quote(), "RUB", fx());
    const p = report.positions[0];
    expect(positionDecimal(p.quantity)).toBe("1");
    expect(positionDecimal(p.cost)).toBe("8000"); // pure 100 USD * acquisition 80, not current 100
    expect(positionDecimal(p.value)).toBe("14000");
    expect(positionDecimal(p.income)).toBe("900"); // supplied payout, no tax recalculation
    expect(positionDecimal(p.profit)).toBe("9400"); // 14000 -206*80 +122*90 +10*90
    expect(positionDecimal(p.realized)).toBe("2740");
    expect(report.months.map(([month, amount]) => [month, positionDecimal(amount)])).toEqual([["2026-02", "900"]]);
    expect(report.missingFX).toEqual([]);
  });
  it("merges account holdings of one security and includes other native currencies", () => {
    const b = buy(); b.quantity = "1"; b.amount = "100"; b.fee = "1"; b.accrued = "2";
    const other = create(InvestmentSnapshotSchema, {ledger:{id:"two", name:"Other"}, positions:[{instrumentKey:"RU", currency:"RUB", quantity:"2", lots:[{originId:"rub-buy", quantity:"2", cleanCost:"102", acquiredAccrued:"0", acquiredAt:at("2026-01-01")}]}]});
    const rub = create(InvestmentOperationSchema, {id:"rub-buy", ledgerId:"two", status:"posted", kind:"buy", instrumentKey:"RU", currency:"RUB", amount:"100", fee:"2", accrued:"0", quantity:"2", occurredAt:at("2026-01-01")});
    const quotes = quote(); quotes.set("RU\0RUB", {price:decimal("60")!, at:1});
    const report = portfolioReport([snapshot(), other], [b, rub], quotes, "RUB", fx());
    expect(report.positions.map(p => [p.key, positionDecimal(p.value), positionDecimal(p.cost)])).toEqual([["US", "14000", "8000"], ["RU", "120", "100"]]);
  });
  it("does not use today's FX for a missing historical fixing or a missing acquisition date", () => {
    const rates = fx(); rates.rates = rates.rates.filter(r => !r.date);
    let report = portfolioReport([snapshot()], [buy()], quote(), "RUB", rates);
    expect(report.positions[0].value).toBeDefined(); expect(report.positions[0].cost).toBeUndefined(); expect(report.positions[0].profit).toBeUndefined();
    expect(report.missingFX).toContain("2026-01-01:USD/RUB");
    const s = snapshot(); s.positions[0].lots[0].acquiredAt = undefined;
    const b = buy(); b.kind = "opening_position"; b.openingCostKnown = true; b.openingAccruedKnown = true; b.acquiredAt = undefined;
    report = portfolioReport([s], [b], quote(), "RUB", rates);
    expect(report.positions[0].cost).toBeUndefined(); expect(report.missingFX).toContain("missing-date:USD/RUB");
  });
  it("uses capitalized-cost proportions rather than pre-split units when stripping remaining acquisition fees", () => {
    const b = buy(); b.accrued = "0";
    const s = snapshot(); s.positions[0].quantity = "5"; s.positions[0].lots[0].quantity = "5"; s.positions[0].lots[0].cleanCost = "50.5"; s.positions[0].lots[0].acquiredAccrued = "0";
    const p = portfolioReport([s], [b], quote(), "RUB", fx()).positions[0];
    expect(positionDecimal(p.cost)).toBe("4000"); // quarter of original principal, despite a later split
  });
  it("translates statement principal and a separately charged fee on their respective native dates", () => {
    const b = buy(); b.currency = "RUB"; b.amount = "16000"; b.fee = "180"; b.accrued = "0";
    b.tradeFinancials = create(InvestmentOperationSchema, {tradeFinancials:{actualSettlement:"200", actualSettlementIncludesExpenses:false, principal:{amount:"200", currency:"USD", at:at("2026-01-01")}, accrued:{amount:"0", currency:"USD", at:at("2026-01-01")}, fee:{amount:"2", currency:"USD", at:at("2026-02-01")}, tax:{amount:"0", currency:"USD", at:at("2026-01-01")}}}).tradeFinancials;
    const s = snapshot(); s.positions[0].currency = "RUB"; s.positions[0].quantity = "2"; s.positions[0].lots[0].cleanCost = "16180"; s.positions[0].lots[0].acquiredAccrued = "0";
    const p = portfolioReport([s], [b], quote(), "RUB", fx()).positions[0];
    expect(positionDecimal(p.cost)).toBe("16000");
    expect(positionDecimal(p.profit)).toBe("11820"); // current 28000 -native principal 16000 -later fee 180
  });
  it("reports native FIFO principal lots and a fee paid in another currency without capitalizing it twice", () => {
    const rub = create(InvestmentOperationSchema, {id:"rub", status:"posted", kind:"buy", instrumentKey:"BOND", currency:"RUB", quantity:"2", amount:"200", fee:"2", feeCurrency:"USD", accrued:"0", occurredAt:at("2026-01-01")});
    const usd = create(InvestmentOperationSchema, {id:"usd", status:"posted", kind:"buy", instrumentKey:"BOND", currency:"USD", quantity:"3", amount:"30", fee:"3", feeCurrency:"RUB", accrued:"0", occurredAt:at("2026-01-01")});
    const s = create(InvestmentSnapshotSchema, {positions:[{instrumentKey:"BOND", currency:"RUB", costBasis:"principal", quantity:"5", lots:[
      {originId:"rub", currency:"RUB", quantity:"2", cleanCost:"200", acquiredAccrued:"0", acquiredAt:at("2026-01-01")},
      {originId:"usd", currency:"USD", quantity:"3", cleanCost:"30", acquiredAccrued:"0", acquiredAt:at("2026-01-01")},
    ]}]});
    const p = portfolioReport([s], [rub, usd], new Map([["BOND\0RUB", {price:decimal("100")!, at:1}]]), "RUB", fx()).positions[0];
    expect(positionDecimal(p.cost)).toBe("2600");
    expect(positionDecimal(p.profit)).toBe("-2263");
    expect(positionDecimal(p.realized)).toBe("0");
  });
  it("includes portfolio payouts and fees without requiring a security or broker cash book", () => {
    const payout = create(InvestmentOperationSchema, {id:"interest", status:"posted", kind:"cash_interest", currency:"RUB", amount:"15", occurredAt:at("2026-02-01")});
    const fee = create(InvestmentOperationSchema, {...payout, id:"fee", kind:"fee", amount:"2"});
    const tax = create(InvestmentOperationSchema, {...payout, id:"tax", kind:"tax", amount:"3"});
    const p = portfolioReport([], [payout, fee, tax], new Map(), "RUB", fx()).positions[0];
    expect(positionDecimal(p.income)).toBe("15"); expect(positionDecimal(p.profit)).toBe("13");
  });
  it("retains payouts for closed securities and excludes voided and future operations", () => {
    const payout = create(InvestmentOperationSchema, {id:"payout", status:"posted", kind:"dividend", instrumentKey:"CLOSED", currency:"USD", amount:"10", occurredAt:at("2026-02-01")});
    const voided = create(InvestmentOperationSchema, {...payout, id:"void", status:"voided", amount:"1000"});
    const future = create(InvestmentOperationSchema, {...payout, id:"future", occurredAt:at("2027-01-01")});
    const p = portfolioReport([], [payout, voided, future], new Map(), "RUB", fx()).positions[0];
    expect(positionDecimal(p.quantity)).toBe("0"); expect(positionDecimal(p.profit)).toBe("900");
  });
});
