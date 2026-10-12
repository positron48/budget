import React from "react";
import {describe,it,expect,vi} from "vitest";
import {render,screen,fireEvent,cleanup} from "@testing-library/react";
import InvestmentChart from "../components/InvestmentChart";

vi.mock("next-intl",()=>({useTranslations:()=>((key:string)=>key)}));

describe("investment chart interaction",()=>{
 it("uses one tab stop, exact labels, both boundaries and a live value",()=>{
  render(<InvestmentChart role="img" aria-label="NAV"><title>NAV</title><path d="M0 0"/><circle cx="1"><title>2024-01-01: 9007199254740993.123 USD</title></circle><circle cx="3"><title>2024-01-03: 0 USD</title></circle></InvestmentChart>);
  const chart=screen.getByRole("img"),value=screen.getByTestId("investment-chart-selection");
  expect(chart.getAttribute("tabindex")).toBe("0");expect(chart.querySelectorAll("[tabindex]")).toHaveLength(0);
  fireEvent.focus(chart);expect(value.textContent).toContain("9007199254740993.123");
  fireEvent.keyDown(chart,{key:"ArrowRight"});expect(value.textContent).toBe("2024-01-03: 0 USD");
  fireEvent.keyDown(chart,{key:"End"});expect(value.textContent).toBe("2024-01-03: 0 USD");
  fireEvent.keyDown(chart,{key:"Home"});expect(value.textContent).toContain("2024-01-01");
  fireEvent.keyDown(chart,{key:"ArrowLeft"});expect(value.textContent).toContain("2024-01-01");
  expect(value.textContent).not.toContain("2024-01-02");expect(value.getAttribute("aria-live")).toBe("polite");
  fireEvent.keyDown(chart,{key:"Escape"});expect(value.textContent).toBe("noSelection");
  expect(chart.querySelectorAll('[data-investment-chart-active="true"]')).toHaveLength(0);
  cleanup();
 });
 it("resets replaced data, supports hover, and isolates other charts",()=>{
  const draw=(value:string)=><><InvestmentChart role="img" aria-label="Profit"><rect><title>{value}</title></rect></InvestmentChart><InvestmentChart role="img" aria-label="Composition"><circle strokeWidth="24" fill="none"><title>Cash 100%</title></circle></InvestmentChart></>;
  const view=render(draw("Old profit")),profit=screen.getByRole("img",{name:"Profit"}),composition=screen.getByRole("img",{name:"Composition"});
  fireEvent.pointerOver(profit.querySelector("rect")!);expect(screen.getAllByTestId("investment-chart-selection")[0].textContent).toBe("Old profit");
  view.rerender(draw("Old profit"));expect(screen.getAllByTestId("investment-chart-selection")[0].textContent).toBe("Old profit");
  fireEvent.focus(composition);expect(composition.querySelector("circle")!.getAttribute("stroke-width")).toBe("24");
  expect(profit.querySelectorAll('[data-investment-chart-active="true"]')).toHaveLength(1);
  view.rerender(draw("New profit"));expect(screen.getAllByTestId("investment-chart-selection")[0].textContent).toBe("noSelection");
  fireEvent.focus(profit);expect(screen.getAllByTestId("investment-chart-selection")[0].textContent).toBe("New profit");
  cleanup();
 });
 it("traverses time across multiple lines and a current point in visual order",()=>{
  render(<InvestmentChart role="img" aria-label="Comparison"><circle cx="780"><title>Now</title></circle><g><circle cx="20"><title>Day 1 portfolio</title></circle><circle cx="400"><title>Day 2 portfolio</title></circle></g><g><circle cx="20"><title>Day 1 benchmark</title></circle><circle cx="400"><title>Day 2 benchmark</title></circle></g></InvestmentChart>);
  const chart=screen.getByRole("img"),value=screen.getByTestId("investment-chart-selection");
  fireEvent.focus(chart);expect(value.textContent).toBe("Day 1 portfolio");
  fireEvent.keyDown(chart,{key:"ArrowRight"});expect(value.textContent).toBe("Day 1 benchmark");
  fireEvent.keyDown(chart,{key:"ArrowRight"});expect(value.textContent).toBe("Day 2 portfolio");
  fireEvent.keyDown(chart,{key:"End"});expect(value.textContent).toBe("Now");
  cleanup();
 });
});
