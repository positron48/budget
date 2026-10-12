import {expect,type Locator} from "@playwright/test";

export async function checkInvestmentChartKeyboard(chart:Locator){
 const values=await chart.locator("circle,rect").evaluateAll(nodes=>nodes.filter(n=>n.querySelector("title")?.textContent?.trim()).sort((a,b)=>Number(a.getAttribute("cx")||a.getAttribute("x")||0)-Number(b.getAttribute("cx")||b.getAttribute("x")||0)).map(n=>n.querySelector("title")!.textContent!));expect(values.length).toBeGreaterThan(0);
 const selection=chart.locator("..").getByTestId("investment-chart-selection");
 await chart.focus();await expect(chart).toBeFocused();await expect(selection).toHaveText(values[0]);
 await chart.press("End");await expect(selection).toHaveText(values.at(-1)!);
 await chart.press("Home");await expect(selection).toHaveText(values[0]);
 await chart.press("ArrowRight");await expect(selection).toHaveText(values[Math.min(1,values.length-1)]);
 await chart.press("ArrowLeft");await expect(selection).toHaveText(values[0]);
 await chart.press("Escape");await expect(selection).not.toHaveText(values[0]);
}
