import {expect,it} from "vitest";
import {create} from "@bufbuild/protobuf";
import {render,screen} from "@testing-library/react";
import {GetInvestmentPerformanceResponseSchema,TrackerHistoryPointSchema} from "@/proto/budget/v1/investment_nav_pb";
import {portfolioAnalyticsCSV} from "@/lib/portfolio-analytics";
import PortfolioAnalytics from "@/components/PortfolioAnalytics";
const f=(n:string,d="1")=>({numerator:n,denominator:d});
function fixture(){return create(GetInvestmentPerformanceResponseSchema,{currency:"RUB",fxPolicyKey:"market",analyticsConvention:"retrospective_cashless_strategy_v1",analyticsPoints:[{date:"2023-12-31",value:f("100"),contributions:f("0"),invested:f("100"),profit:f("0")},{date:"2024-01-01",flow:f("-5"),contributions:f("-5"),invested:f("95"),reason:"valuation_missing"},{date:"2024-01-02",value:f("110"),flow:f("0"),contributions:f("-5"),invested:f("95"),profit:f("15")}],monthlyProfit:[{key:"2024-01",fromDate:"2024-01-01",toDate:"2024-01-02",profit:f("15")}],yearlyProfit:[{key:"2024",fromDate:"2024-01-01",toDate:"2024-01-02",profit:f("15")}],trackerBenchmarks:[{key:"spy",title:"SPY",baseDate:"2023-12-31",points:[{date:"2023-12-31",virtualValue:f("100"),virtualProfit:f("0"),index:f("100")},{date:"2024-01-01",virtualReason:"benchmark_flow_price_missing"},{date:"2024-01-02",virtualReason:"benchmark_flow_price_missing"}]}]});}
it("shows cumulative profit gaps without joining missing days or inventing zero",()=>{const {container}=render(<PortfolioAnalytics performance={fixture()} mode="profit" from="2024-01-01" to="2024-01-02" ru={false} locale="en" benchmarkIds={["spy"]} onBenchmarksChange={()=>{}}/>);expect(container.querySelectorAll("polyline")).toHaveLength(3);expect(screen.getByRole("img")).toHaveAttribute("aria-label","Cumulative profit");expect(container.querySelectorAll("circle")).toHaveLength(0);});
it("retains incomplete monthly rows and displays exact selected boundaries",()=>{const p=fixture();p.monthlyProfit[0].profit=undefined;p.monthlyProfit[0].reason="valuation_missing";const {container}=render(<PortfolioAnalytics performance={p} mode="months" from="2024-01-01" to="2024-01-02" ru locale="ru" benchmarkIds={[]} onBenchmarksChange={()=>{}}/>);expect(screen.queryByRole("table")).toBeNull();expect(screen.getByRole("status")).toHaveTextContent("Не хватает исторической цены или курса");expect(container.querySelector("rect")).toBeNull();});
it("exports exact values and quality with the same number of columns for all row kinds",()=>{const p=fixture();p.analyticsPoints[2].profit=create(GetInvestmentPerformanceResponseSchema,{profit:f("1","3")}).profit;const csv=portfolioAnalyticsCSV(p),rows=csv.split("\r\n").map(s=>s.slice(1,-1).split('\",\"'));expect(new Set(rows.map(r=>r.length))).toEqual(new Set([44]));expect(rows[0]).toContain("virtual_profit_den");expect(rows.find(r=>r[0]==="day"&&r[1]==="2024-01-01")![8]).toBe("-5");const day=rows.find(r=>r[0]==="day"&&r[1]==="2024-01-02"&&r[22]==="")!;expect(day[14]).toBe("1");expect(day[15]).toBe("3");expect(csv).toContain("benchmark_flow_price_missing");expect(rows.filter(r=>["month","year"].includes(r[0]))).toHaveLength(2);});

it("plots the known portion when every full portfolio valuation is incomplete",()=>{
 const p=create(GetInvestmentPerformanceResponseSchema,{currency:"RUB",analyticsPoints:[{date:"2024-01-01",reason:"valuation_missing"},{date:"2024-01-02",reason:"valuation_missing"}]});
 const historyPoints=[create(TrackerHistoryPointSchema,{date:"2024-01-01",state:"incomplete",knownValue:f("100"),pricedPositions:105,positions:110,coverageKey:"a".repeat(64)}),create(TrackerHistoryPointSchema,{date:"2024-01-02",state:"incomplete",knownValue:f("110"),pricedPositions:105,positions:110,coverageKey:"a".repeat(64)})];
 const {container}=render(<PortfolioAnalytics performance={p} historyPoints={historyPoints} mode="value" from="2024-01-01" to="2024-01-02" ru locale="ru" benchmarkIds={[]} onBenchmarksChange={()=>{}}/>);
 expect(screen.getByRole("img",{name:"Стоимость и вложения"})).toBeVisible();
 expect(container.querySelector("polyline")).not.toHaveAttribute("stroke-dasharray");
 expect(container.querySelectorAll("circle")).toHaveLength(0);
 expect(container.querySelector("title")).toHaveTextContent("известная часть");
 expect(p.analyticsPoints.every(d=>!d.value&&!d.profit)).toBe(true);
});

it("exports a partial value separately from unknown full valuation and profit",()=>{
 const p=create(GetInvestmentPerformanceResponseSchema,{currency:"RUB",analyticsPoints:[{date:"2024-01-01",reason:"valuation_missing"}]});
 const historyPoints=[create(TrackerHistoryPointSchema,{date:"2024-01-01",state:"incomplete",knownValue:f("1","3"),pricedPositions:105,positions:110,coverageKey:"a".repeat(64)})];
 const rows=portfolioAnalyticsCSV(p,historyPoints).split("\r\n").map(s=>s.slice(1,-1).split('\",\"')),head=rows[0],row=rows[1];
 expect(row[head.indexOf("value_num")]).toBe("");expect(row[head.indexOf("profit_num")]).toBe("");
 expect(row[head.indexOf("known_value_num")]).toBe("1");expect(row[head.indexOf("known_value_den")]).toBe("3");
 expect(row[head.indexOf("valuation_state")]).toBe("incomplete");expect(row[head.indexOf("priced_positions")]).toBe("105");
 expect(new Set(rows.map(r=>r.length))).toEqual(new Set([52]));
});

it("breaks the known-value line when coverage changes, disappears or becomes complete",()=>{
 const points=[
  create(TrackerHistoryPointSchema,{date:"2024-01-01",state:"ready",knownValue:f("120"),pricedPositions:2,positions:2}),
  create(TrackerHistoryPointSchema,{date:"2024-01-02",state:"incomplete",knownValue:f("100"),pricedPositions:1,positions:2,coverageKey:"a".repeat(64)}),
  create(TrackerHistoryPointSchema,{date:"2024-01-03",state:"incomplete",knownValue:f("110"),pricedPositions:1,positions:2,coverageKey:"a".repeat(64)}),
  create(TrackerHistoryPointSchema,{date:"2024-01-04",state:"incomplete",knownValue:f("115"),pricedPositions:1,positions:2,coverageKey:"b".repeat(64)}),
  create(TrackerHistoryPointSchema,{date:"2024-01-05",state:"incomplete",knownValue:f("0"),pricedPositions:0,positions:2}),
  create(TrackerHistoryPointSchema,{date:"2024-01-06",state:"incomplete",knownValue:f("120"),pricedPositions:1,positions:2,coverageKey:"b".repeat(64)}),
  create(TrackerHistoryPointSchema,{date:"2024-01-07",state:"ready",knownValue:f("130"),pricedPositions:2,positions:2}),
 ];
 const p=create(GetInvestmentPerformanceResponseSchema,{currency:"RUB",analyticsPoints:points.map(d=>({date:d.date,value:d.state==="ready"?d.knownValue:undefined,reason:d.state==="ready"?"":"valuation_missing"}))});
 const {container}=render(<PortfolioAnalytics performance={p} historyPoints={points} mode="value" from="2024-01-01" to="2024-01-07" ru={false} locale="en" benchmarkIds={[]} onBenchmarksChange={()=>{}}/>);
 expect(container.querySelectorAll("polyline")).toHaveLength(5);expect(container.querySelectorAll('polyline[stroke-dasharray="5 4"]')).toHaveLength(0);expect(container.querySelectorAll("circle")).toHaveLength(0);
 expect([...container.querySelectorAll("title")].some(x=>x.textContent?.includes("2024-01-05"))).toBe(false);
});

it("does not turn a known partial value into profit",()=>{
 const p=create(GetInvestmentPerformanceResponseSchema,{currency:"RUB",analyticsPoints:[{date:"2024-01-01",reason:"valuation_missing"}]});
 const points=[create(TrackerHistoryPointSchema,{date:"2024-01-01",state:"incomplete",knownValue:f("100"),pricedPositions:1,positions:2})];
 render(<PortfolioAnalytics performance={p} historyPoints={points} mode="profit" from="2024-01-01" to="2024-01-01" ru={false} locale="en" benchmarkIds={[]} onBenchmarksChange={()=>{}}/>);
 expect(screen.queryByRole("img")).toBeNull();expect(screen.getByText("Historical price or exchange rate missing")).toBeVisible();
});

it("shows independent index growth even when trade-based comparisons are unavailable",()=>{
 const p=fixture();p.trackerBenchmarks[0].points[1].index=create(GetInvestmentPerformanceResponseSchema,{profit:f("110")}).profit;
 p.trackerBenchmarks[0].points[2].index=create(GetInvestmentPerformanceResponseSchema,{profit:f("120")}).profit;
 const {container}=render(<PortfolioAnalytics performance={p} mode="indices" from="2024-01-01" to="2024-01-02" ru locale="ru" benchmarkIds={["spy"]} onBenchmarksChange={()=>{}}/>);
 expect(screen.getByRole("img",{name:"Сравнение роста, %"})).toBeVisible();expect(container.querySelector("polyline title")).toHaveTextContent("20 %");expect(container.querySelector("polyline title")).not.toHaveTextContent("₽");
});

it("uses a common observed baseline for independent growth and continuous portfolio TWR",async()=>{
 const {portfolioGrowthSeries}=await import('@/lib/portfolio-analytics');const p=fixture();
 p.trackerBenchmarks[0].points[0].index=undefined;
 p.trackerBenchmarks[0].points[1].index=create(GetInvestmentPerformanceResponseSchema,{profit:f("110")}).profit;
 p.trackerBenchmarks[0].points[2].index=create(GetInvestmentPerformanceResponseSchema,{profit:f("121")}).profit;
 p.twrPoints=create(GetInvestmentPerformanceResponseSchema,{twrPoints:[{date:"2023-12-31",index:f("100")},{date:"2024-01-01",index:f("120")},{date:"2024-01-02",index:f("126")}]}).twrPoints;
 const growth=portfolioGrowthSeries(p,["spy"],"2023-12-31",p);
 expect(growth.baseline).toBe("2024-01-01");expect(growth.lines).toHaveLength(2);
 expect(growth.lines[0].points[0].value).toBeUndefined();expect(growth.lines[0].points[1].value).toBe(0);
 expect(growth.lines[0].points[2].value).toBeCloseTo(5);expect(growth.lines[1].points[2].value).toBeCloseTo(10);
 p.twrReason="discontinuous_history";expect(portfolioGrowthSeries(p,["spy"],"2023-12-31",p).lines).toHaveLength(1);
});
