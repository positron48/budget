import {describe,it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {GetInvestmentPerformanceResponseSchema,TrackerValueDetailSchema} from "@/proto/budget/v1/investment_nav_pb";
import {portfolioDayResult,latestCompletedTradingValuation} from "@/lib/portfolio-day-result";
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
