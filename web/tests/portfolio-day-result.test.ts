import {describe,it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {GetInvestmentPerformanceResponseSchema,TrackerValueDetailSchema,InvestmentQuoteSchema,GetInvestmentExchangeRatesResponseSchema} from "@/proto/budget/v1/investment_nav_pb";
import {portfolioDayResult,latestCompletedTradingValuation,livePortfolioDayResult} from "@/lib/portfolio-day-result";
import {InvestmentOperationSchema} from "@/proto/budget/v1/investment_pb";
import {positionDecimal} from "@/lib/investment-positions";
function fixture(){const f=(numerator:string)=>({numerator,denominator:"1"});return create(GetInvestmentPerformanceResponseSchema,{fromDate:"2026-01-02",toDate:"2026-01-02",analyticsConvention:"retrospective_cashless_strategy_v1",reportingTimezone:"Europe/Moscow",analyticsPoints:[{date:"2026-01-01",value:f("100")},{date:"2026-01-02",value:f("210"),flow:f("100"),dailyProfit:f("10")}]});}
describe("completed daily result",()=>{
 it("uses signed daily analytics and excludes new purchase capital",()=>{const p=fixture();expect(portfolioDayResult(p,p.toDate)?.profit.numerator).toBe("10");});
 it("includes payouts once rather than adding them to the published daily result",()=>{const p=fixture(),d=p.analyticsPoints[1];d.value!.numerator="95";d.flow!.numerator="-10";d.dailyProfit!.numerator="5";expect(portfolioDayResult(p,p.toDate)?.profit.numerator).toBe("5");});
 it("preserves unknown boundaries, flow, reset and mismatched calculation instead of showing zero",()=>{for(const mutate of [(p:ReturnType<typeof fixture>)=>{p.analyticsPoints[0].value=undefined;},(p:ReturnType<typeof fixture>)=>{p.analyticsPoints[1].value=undefined;},(p:ReturnType<typeof fixture>)=>{p.analyticsPoints[1].flow=undefined;},(p:ReturnType<typeof fixture>)=>{p.analyticsPoints[1].flowReason="history_reset";},(p:ReturnType<typeof fixture>)=>{p.analyticsPoints[1].dailyProfit!.numerator="110";},(p:ReturnType<typeof fixture>)=>{p.analyticsPoints[1].value!.denominator="0";},(p:ReturnType<typeof fixture>)=>{p.analyticsConvention="other";},(p:ReturnType<typeof fixture>)=>{p.reportingTimezone="UTC";},(p:ReturnType<typeof fixture>)=>{p.analyticsPoints[0].date="2025-12-31";}]){const p=fixture();mutate(p);expect(portfolioDayResult(p,p.toDate)).toBeUndefined();}});
 it("keeps exact known zero",()=>{const p=fixture(),d=p.analyticsPoints[1];d.value!.numerator="200";d.dailyProfit!.numerator="0";expect(portfolioDayResult(p,p.toDate)?.profit.numerator).toBe("0");});
});

it("uses the last actual close instead of a Sunday or newer manual estimate",()=>{
 const detail=(priceSource:string,date:string)=>create(TrackerValueDetailSchema,{priceSource,priceAt:{seconds:BigInt(Date.parse(date+"T00:00:00Z")/1000)},unitPrice:{numerator:"10",denominator:"1"},quantity:{numerator:"1",denominator:"1"}});
 const rows=[detail("moex/personal","2026-10-17"),detail("twelvedata/personal","2026-10-17"),detail("estimate","2026-10-18"),detail("moex/personal","2026-06-24")];
 expect(latestCompletedTradingValuation(rows,"2026-10-18")).toBe("2026-10-17");
 expect(latestCompletedTradingValuation([detail("estimate","2026-10-18")],"2026-10-18")).toBeUndefined();
});

describe('today live change',()=>{
 const now=Date.parse('2026-10-19T17:00:00Z'),f=(n:string)=>({numerator:n,denominator:'1'});
 const position=(quantity='2',price='120')=>({key:'S',currency:'RUB',quantity:f(quantity),price:f(price),value:f(String(Number(quantity)*Number(price))),income:f('0'),recoveredAccrued:f('0'),incomeComplete:true,accounts:[]});
 const quote=()=>create(InvestmentQuoteSchema,{instrumentKey:'S',previousClose:f('100'),previousCloseCurrency:'RUB'});
 const op=(kind:string,amount:string,quantity='0')=>create(InvestmentOperationSchema,{id:kind,ledgerId:'L',instrumentKey:'S',kind,status:'posted',currency:'RUB',amount,quantity,fee:'0',accrued:'0',occurredAt:{seconds:BigInt(now/1000-60)}});
 it('shows current price against previous close independently of graph dates',()=>{
  const r=livePortfolioDayResult([position()],[],[quote()],'RUB',undefined,undefined,now)!;
  expect(positionDecimal(r.result.profit)).toBe('40');expect(positionDecimal(r.result.before)).toBe('200');expect(r.tradingDate).toBe('2026-10-19');expect(r.partial).toBe(false);
 });
 it('removes purchase funding, includes sale proceeds, fees and payouts exactly once',()=>{
  const buy=op('buy','110','1');buy.fee='2';
  const r=livePortfolioDayResult([position('2')],[buy,op('dividend','5')],[quote()],'RUB',undefined,undefined,now)!;
  expect(positionDecimal(r.result.profit)).toBe('33'); // 240 - 100 - 110 - 2 + 5
  const sold=livePortfolioDayResult([position('0')],[op('sell','110','2')],[quote()],'RUB',undefined,undefined,now)!;
  expect(positionDecimal(sold.result.profit)).toBe('-90');expect(positionDecimal(sold.result.before)).toBe('200');
 });
 it('keeps last-known small holdings and excludes unsupported changes without hiding the result',()=>{
  const r=livePortfolioDayResult([position()],[],[],'RUB',undefined,undefined,now)!;
  expect(positionDecimal(r.result.profit)).toBe('0');expect(r.partial).toBe(true);
 });
 it('uses prior-day FX for close and current FX for current valuation',()=>{
  const q=quote();q.previousCloseCurrency='USD';q.previousClose!.numerator='1';
  const fx=create(GetInvestmentExchangeRatesResponseSchema,{currency:'RUB',rates:[{date:'2026-10-18',fromCurrency:'USD',rate:f('90')}]});
  const r=livePortfolioDayResult([position('2','100')],[],[q],'RUB',fx,undefined,now)!;
  expect(positionDecimal(r.result.profit)).toBe('20');
 });
});
