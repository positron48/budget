import {describe,it,expect,vi} from "vitest";
import {create} from "@bufbuild/protobuf";
import {GetTrackerHistoryResponseSchema, TrackerHistoryPointSchema} from "@/proto/budget/v1/investment_nav_pb";
import {loadPortfolioHistory,historyPointVisible,type HistoryScope} from "@/lib/portfolio-history";
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
