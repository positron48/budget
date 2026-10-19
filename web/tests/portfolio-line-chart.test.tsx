import {expect,it} from "vitest";
import {render} from "@testing-library/react";
import PortfolioLineChart from "@/components/PortfolioLineChart";

it("keeps nonnegative portfolio values above zero without a negative axis",()=>{
 const {container}=render(<PortfolioLineChart title="Value" locale="en" from="2024-01-01" to="2024-01-02" series={[{key:"nav",title:"NAV",color:"blue",points:[{date:"2024-01-01",value:0},{date:"2024-01-02",value:10000000}]}]}/>);
 const labels=[...container.querySelectorAll('svg text')].map(x=>x.textContent);
 expect(labels.some(x=>x?.startsWith("-"))).toBe(false);
});
it("fills mixed-sign profit toward the zero line, retaining real negative values",()=>{
 const {container}=render(<PortfolioLineChart title="Profit" locale="en" from="2024-01-01" to="2024-01-02" series={[{key:"profit",title:"Profit",color:"blue",points:[{date:"2024-01-01",value:-100},{date:"2024-01-02",value:100}]}]}/>);
 expect(container.querySelector('polygon')!.getAttribute('points')!.split(' ')[0]).toBe('76,157');
 expect([...container.querySelectorAll('svg text')].some(x=>x.textContent?.startsWith('-'))).toBe(true);
});
