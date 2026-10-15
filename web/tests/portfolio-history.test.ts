import {describe,it,expect,vi} from "vitest";
import {create} from "@bufbuild/protobuf";
import {Code,ConnectError} from "@connectrpc/connect";
import {GetTrackerHistoryResponseSchema, TrackerHistoryPointSchema, GetInvestmentPerformanceResponseSchema} from "@/proto/budget/v1/investment_nav_pb";
import {loadPortfolioHistory,loadPortfolioPerformanceHistory,historyPointVisible,type HistoryScope} from "@/lib/portfolio-history";
const scope:HistoryScope={ledgerIds:["ledger"],currency:"RUB",fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",fromDate:"2020-01-01",toDate:"2020-01-02"};
function page(date:string,nextDate:string,generation=1n){return create(GetTrackerHistoryResponseSchema,{currency:"RUB",fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",reportingTimezone:"Europe/Moscow",priceConvention:"retrospective_close_next_utc_day_v1",generation,basisKey:"d".repeat(64),nextDate,points:[{date,state:"ready",knownValue:{numerator:"2",denominator:"3"},positions:1,pricedPositions:1,coverageKey:"a".repeat(64)}]});}
describe("tracker historical page coherence",()=>{
 it("assembles exact contiguous pages without converting fractions",async()=>{const api={getTrackerHistory:vi.fn().mockResolvedValueOnce(page("2020-01-01","2020-01-02")).mockResolvedValueOnce(page("2020-01-02",""))};const result=await loadPortfolioHistory(api,scope,new AbortController().signal,vi.fn());expect(result).toHaveLength(2);expect(result[0].knownValue?.denominator).toBe("3");expect(api.getTrackerHistory.mock.calls[1][0].fromDate).toBe("2020-01-02");});
 it.each(["basis","policy","mode","cursor","truncated","missing_point","fraction"])("rejects %s",async kind=>{
  const first=page("2020-01-01","2020-01-02"),second=page("2020-01-02","");
  if(kind==="basis")second.basisKey="e".repeat(64);
  if(kind==="policy")second.fxPolicyVersion=2n;
  if(kind==="mode")second.fxMode="cbr";
  if(kind==="cursor")first.nextDate="2020-01-01";
  if(kind==="truncated")first.nextDate="";
  if(kind==="missing_point")second.points[0].date="2020-01-03";
  if(kind==="fraction")first.points[0].knownValue!.denominator="0";
  const api={getTrackerHistory:vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce(second)};
  await expect(loadPortfolioHistory(api,scope,new AbortController().signal,vi.fn())).rejects.toThrow();
 });
 it("does not plot an entirely unpriced portfolio as zero",()=>{
  const p=create(TrackerHistoryPointSchema,{state:"incomplete",knownValue:{numerator:"0",denominator:"1"},positions:2,pricedPositions:0});expect(historyPointVisible(p)).toBe(false);
  p.pricedPositions=1;expect(historyPointVisible(p)).toBe(true);
  p.positions=p.pricedPositions=0;p.state="ready";expect(historyPointVisible(p)).toBe(true);
 });
});

function performanceFixture() {return create(GetInvestmentPerformanceResponseSchema,{currency:"RUB",fxPolicyKey:"market",fromDate:scope.fromDate,toDate:scope.toDate,historyScope:"recorded_securities_strategy",twrConvention:"securities_daily_end_of_day_flows",reportingTimezone:"Europe/Moscow",profit:{numerator:"-2",denominator:"3"},netContributions:{numerator:"10",denominator:"1"},twr:{numerator:"-1",denominator:"10"},xirr:0.1,xirrAnnualized:true,xirrDays:2});}
function performanceAPI() {
 const first=page("2020-01-01","2020-01-02"),second=page("2020-01-02","");first.performanceSnapshot="signed-first";second.performanceSnapshot="signed-second";
 return {getTrackerHistory:vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce(second),getTrackerPerformance:vi.fn().mockResolvedValue(performanceFixture())};
}
describe("cashless tracker performance",()=>{
 it("uses every chart page and retains the root period across pagination",async()=>{
  const api=performanceAPI();const result=await loadPortfolioPerformanceHistory(api,scope,new AbortController().signal,vi.fn());
  expect(result.performance?.profit?.numerator).toBe("-2");expect(api.getTrackerPerformance.mock.calls[0][0].snapshotTokens).toEqual(["signed-first","signed-second"]);
  expect(api.getTrackerHistory.mock.calls[1][0].performanceFromDate).toBe(scope.fromDate);
 });
 it("keeps the chart without inventing returns from unsigned old pages",async()=>{
  const api=performanceAPI();api.getTrackerHistory.mockReset().mockResolvedValueOnce(page("2020-01-01","2020-01-02")).mockResolvedValueOnce(page("2020-01-02",""));
  const result=await loadPortfolioPerformanceHistory(api,scope,new AbortController().signal,vi.fn());expect(result.points).toHaveLength(2);expect(result.performance).toBeUndefined();expect(api.getTrackerPerformance).not.toHaveBeenCalled();
 });
 it.each(["currency","timezone","fraction","infinity","annualized","missing_profit","missing_return"])("rejects malformed %s",async kind=>{
  const api=performanceAPI(),value=performanceFixture();
  if(kind==="currency")value.currency="USD";
  if(kind==="timezone")value.reportingTimezone="UTC";
  if(kind==="fraction")value.profit!.denominator="0";
  if(kind==="infinity")value.xirr=Infinity;
  if(kind==="annualized")value.xirrAnnualized=false;
  if(kind==="missing_profit")value.profit=undefined;
  if(kind==="missing_return")value.twr=undefined;
  api.getTrackerPerformance.mockResolvedValue(value);
  await expect(loadPortfolioPerformanceHistory(api,scope,new AbortController().signal,vi.fn())).rejects.toThrow("invalid_performance");
 });
 it("preserves explicit unknown returns rather than showing zero",async()=>{
  const api=performanceAPI(),value=performanceFixture();value.twr=undefined;value.xirr=undefined;value.twrReason="gap";value.xirrReason="unknown_flow";api.getTrackerPerformance.mockResolvedValue(value);
  const result=await loadPortfolioPerformanceHistory(api,scope,new AbortController().signal,vi.fn());expect(result.performance?.twr).toBeUndefined();expect(result.performance?.xirr).toBeUndefined();
 });
});

it("asks to refresh after a corrected or expired signed chart snapshot",async()=>{
 const api=performanceAPI();api.getTrackerPerformance.mockRejectedValue(new ConnectError("snapshot changed",Code.FailedPrecondition));
 await expect(loadPortfolioPerformanceHistory(api,scope,new AbortController().signal,vi.fn())).rejects.toThrow("history_changed");
});

it.each(["source", "dates", "currency", "fraction"])("rejects a mismatched benchmark %s", async kind => {
 const api=performanceAPI(),value=performanceFixture();
 value.trackerBenchmarks=[{ $typeName:"budget.v1.TrackerBenchmarkLine",key:"imoex",title:"IMOEX",kind:"price_index",nativeCurrency:"RUB",provider:"moex",sourceUrl:"https://www.moex.com/ru/index/IMOEX",baseDate:"2019-12-31",reason:"",points:["2019-12-31","2020-01-01","2020-01-02"].map(date=>({$typeName:"budget.v1.TrackerBenchmarkPoint",virtualReason:"",date,index:{$typeName:"budget.v1.InvestmentNAVAmount",numerator:"100",denominator:"1",decimal:"100"},reason:""}))}];
 const line=value.trackerBenchmarks[0];
 if(kind==="source")line.sourceUrl="https://example.test";
 if(kind==="dates")line.points.pop();
 if(kind==="currency")line.nativeCurrency="USD";
 if(kind==="fraction")line.points[0].index!.denominator="0";
 api.getTrackerPerformance.mockResolvedValue(value);
 await expect(loadPortfolioPerformanceHistory(api,scope,new AbortController().signal,vi.fn())).rejects.toThrow("invalid_performance");
});

it("loads group value history without requesting whole-account returns",async()=>{
 const api=performanceAPI();const first=page("2020-01-01","2020-01-02"),second=page("2020-01-02","");first.classificationVersion=2n;second.classificationVersion=2n;
 api.getTrackerHistory.mockReset().mockResolvedValueOnce(first).mockResolvedValueOnce(second);
 const result=await loadPortfolioPerformanceHistory(api,{...scope,groupIds:["a"],classificationVersion:2n},new AbortController().signal,vi.fn());
 expect(result.points).toHaveLength(2);expect(result.performance).toBeUndefined();expect(api.getTrackerPerformance).not.toHaveBeenCalled();expect(api.getTrackerHistory.mock.calls[0][0].performanceFromDate).toBeUndefined();
});
it("rejects group reclassification during historical pagination",async()=>{
 const api=performanceAPI(),first=page("2020-01-01","2020-01-02"),second=page("2020-01-02","");first.classificationVersion=2n;second.classificationVersion=3n;
 api.getTrackerHistory.mockReset().mockResolvedValueOnce(first).mockResolvedValueOnce(second);
 await expect(loadPortfolioPerformanceHistory(api,{...scope,groupIds:["a"],classificationVersion:2n},new AbortController().signal,vi.fn())).rejects.toThrow("history_changed");
});
it.each(["date","fraction","convention","missing_profit","period"])("rejects malformed monetary analytics %s",async kind=>{
 const api=performanceAPI(),p=performanceFixture();p.analyticsConvention="retrospective_cashless_strategy_v1";p.analyticsPoints=["2019-12-31","2020-01-01","2020-01-02"].map(date=>create(GetInvestmentPerformanceResponseSchema,{analyticsPoints:[{date,value:{numerator:"2",denominator:"3"},contributions:{numerator:"0",denominator:"1"},profit:{numerator:"0",denominator:"1"}}]}).analyticsPoints[0]);p.monthlyProfit=create(GetInvestmentPerformanceResponseSchema,{monthlyProfit:[{key:"2020-01",fromDate:"2020-01-01",toDate:"2020-01-02",profit:{numerator:"0",denominator:"1"}}]}).monthlyProfit;p.yearlyProfit=create(GetInvestmentPerformanceResponseSchema,{yearlyProfit:[{key:"2020",fromDate:"2020-01-01",toDate:"2020-01-02",profit:{numerator:"0",denominator:"1"}}]}).yearlyProfit;
 if(kind==="date")p.analyticsPoints[1].date="2020-01-03";if(kind==="fraction")p.analyticsPoints[1].profit!.denominator="0";if(kind==="convention")p.analyticsConvention="unknown";if(kind==="missing_profit")p.analyticsPoints[1].profit=undefined;if(kind==="period")p.monthlyProfit[0].toDate="2020-01-01";
 api.getTrackerPerformance.mockResolvedValue(p);await expect(loadPortfolioPerformanceHistory(api,scope,new AbortController().signal,vi.fn())).rejects.toThrow("invalid_analytics");
});
