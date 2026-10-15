import {describe,it,expect} from "vitest";
import {render,screen,fireEvent} from "@testing-library/react";
import {create} from "@bufbuild/protobuf";
import {GetInvestmentPerformanceResponseSchema} from "@/proto/budget/v1/investment_nav_pb";
import PortfolioReturns from "@/components/PortfolioReturns";
describe("normalized tracker return chart",()=>{
 it("does not bridge TWR gaps or join a restarted segment",()=>{
  const performance=create(GetInvestmentPerformanceResponseSchema,{twrPoints:[{date:"2024-01-01",state:"baseline",segment:1,index:{numerator:"100",denominator:"1"}},{date:"2024-01-02",state:"gap"},{date:"2024-01-03",state:"baseline",segment:2,index:{numerator:"100",denominator:"1"}}],trackerBenchmarks:[{key:"spy",title:"SPY",kind:"etf_price_proxy",nativeCurrency:"USD",sourceUrl:"https://www.ssga.com",points:[{date:"2024-01-01",index:{numerator:"100",denominator:"1"}},{date:"2024-01-02",reason:"missing"},{date:"2024-01-03",index:{numerator:"110",denominator:"1"}}]}]});
  const {container}=render(<PortfolioReturns performance={performance} from="2024-01-02" to="2024-01-03" ru={false}/>);
  expect(container.querySelectorAll("polyline")).toHaveLength(3);
  expect(screen.getByText(/ETF price series, not the index/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("checkbox"));expect(container.querySelectorAll("polyline")).toHaveLength(1);
 });
 it("shows insufficient data instead of a zero return line",()=>{const {container}=render(<PortfolioReturns from="2024-01-02" to="2024-01-03" ru/>);expect(screen.getByRole("status")).toHaveTextContent("Недостаточно истории");expect(container.querySelector("svg")).toBeNull();});
});
