import {checkInvestmentChartKeyboard} from "./investment-chart-helpers";
import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
const amount=(value:string)=>({numerator:value,denominator:"1",decimal:value});
for(const locale of ["ru","en"]){test(`${locale} profit curves, partial months and attribution`,async({page,context})=>{
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});let partial=false;const writes:string[]=[];
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};if(method&&!/^(Get|List|Search)/.test(method.name))writes.push(method.name);
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:[{key:"series",ledgerIds:["ledger"],currency:"USD",fxPolicyKey:"market",fromDate:"2024-01-31",toDate:"2024-02-02"}]};
  if(method?.name==="GetInvestmentProfit"){
   const bytes=route.request().postDataBuffer()!,input=fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any;expect(input.fromDate).toBe("2024-01-31");expect(input.toDate).toBe("2024-02-02");
   data={currency:"USD",fxPolicyKey:"market",profit:amount("0"),attributionComplete:!partial,
    points:[{date:"2024-01-30",cumulative:amount("0")},{date:"2024-01-31",daily:amount("2"),cumulative:amount("2")},{date:"2024-02-01",dailyReason:"terminal_nav_unknown",cumulativeReason:"terminal_nav_unknown"},{date:"2024-02-02",dailyReason:"initial_nav_unknown",cumulative:amount("0")}],
    months:[{period:"2024-01",fromDate:"2024-01-31",toDate:"2024-01-31",profit:amount("2"),netContributions:amount("50")},{period:"2024-02",fromDate:"2024-02-01",toDate:"2024-02-02",profit:amount("-2"),netContributions:amount("10")}],years:[{period:"2024",fromDate:"2024-01-31",toDate:"2024-02-02",profit:amount("0"),netContributions:amount("60")}],
    components:[{kind:"earned_income",known:amount("3"),complete:true},{kind:"taxes",...(!partial?{known:amount("-1"),complete:true}:{reason:"tax_review_required"})},{kind:"trade_fees_in_cost",known:amount("7"),complete:true,informational:true},{kind:"fx_residual",...(!partial?{known:amount("0"),complete:true}:{reason:"attribution_incomplete"})}]};
  }
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/value?group=selected");const section=page.getByTestId("investment-profit");await expect(section).toBeVisible();
 const chart=section.getByRole("img",{name:locale==="ru"?"Накопленная прибыль периода":"Cumulative period profit",exact:true});await expect(chart.getByTestId("profit-segment")).toHaveCount(2);await expect(chart.locator("circle")).toHaveCount(3);await section.getByLabel(locale==="ru"?"График прибыли":"Profit chart",{exact:true}).focus();await page.keyboard.press("Tab");await expect(chart).toBeFocused();await checkInvestmentChartKeyboard(chart);
 await expect(section.getByRole("cell",{name:"2024-01 2024-01-31—2024-01-31",exact:true})).toBeVisible();await expect(section.getByRole("cell",{name:"-2 USD",exact:true})).toBeVisible();await expect(section.getByTestId("profit-bar")).toHaveCount(2);
 await section.getByLabel(locale==="ru"?"График прибыли":"Profit chart",{exact:true}).selectOption("daily");const daily=section.getByRole("img",{name:locale==="ru"?"Прибыль за каждый день":"Daily profit",exact:true});await expect(daily.locator("circle")).toHaveCount(1);
 await section.getByLabel(locale==="ru"?"Периодичность":"Frequency",{exact:true}).selectOption("years");await expect(section.getByTestId("profit-bar")).toHaveCount(1);await expect(section.getByRole("cell",{name:"60 USD",exact:true})).toBeVisible();
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await section.screenshot({path:test.info().outputPath(`budget-investment-profit-${locale}.png`)});
 partial=true;await page.reload();const residual=section.getByText(locale==="ru"?"FX / прочие эффекты атрибуции":"FX / other attribution effects",{exact:true}).locator("../..");await expect(residual.getByText("—",{exact:true})).toBeVisible();await expect(section.getByText(locale==="ru"?"Справочно: уже включено в FIFO, повторно не вычитается.":"Informational: already included in FIFO, not deducted again.",{exact:true})).toBeVisible();expect(writes).toEqual([]);
});}
