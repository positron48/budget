import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
const money=(n:string)=>({numerator:n,denominator:"1",decimal:n});
for(const locale of ["ru","en"])test(`${locale} current NAV is separate, provisional and never a return input`,async({page,context})=>{
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const series={key:"series",ledgerIds:["account"],currency:"USD",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-02"};
 let partial=false,failed=false,wrongScope=false;
 const calls:{method:string,input:any}[]=[];
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentImportService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));
  const bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};let data:any={},code=0;if(method)calls.push({method:method.name,input});
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Current"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:[series]};if(method?.name==="GetInvestmentNAVSeries")data={series};
  if(method?.name==="GetInvestmentNAVHistory")data={points:[{date:"2024-01-01",state:"ready",knownValue:money("100")},{date:"2024-01-02",state:"ready",knownValue:money("110")}].filter(p=>p.date>=input.fromDate&&p.date<=input.toDate)};
  if(method?.name==="GetInvestmentOverview")data={nav:{date:"2024-01-02",state:"ready",knownValue:money("110")}};
  if(method?.name==="GetInvestmentCurrentNAV"){
   code=failed?13:0;
   data={provisional:true,seriesKey:"series",currency:"USD",ledgerIds:[wrongScope?"other":"account"],asOf:{seconds:1704283200n},lastCompletedDate:"2024-01-02",complete:!partial,knownValue:money(partial?"0":"120"),fxPolicy:"market@2",priceProfileKey:"reviewed",priceProfileVersion:3n,issues:partial?[{componentId:"ABC",code:"price_stale"}]:[],quotes:[{instrumentKey:"ABC",state:partial?"stale":"fresh",candidatePrice:money("1200"),currency:"RUB",unit:"currency_per_unit",provider:"tinvest",accessScope:"personal",sourceKind:"exchange",adjustment:"unadjusted",priceAt:{seconds:1704283100n},receivedAt:{seconds:1704283110n},validUntil:{seconds:1704286700n},observationId:"quote",revision:2n,issues:partial?["price_stale"]:[]}],fxLegs:[{observedRate:money("10"),nativeCurrency:"RUB",fromCurrency:"USD",toCurrency:"RUB",policy:"market@2",inverse:true,asOf:{seconds:1704280000n},validUntil:{seconds:1704300000n},id:"fx",revision:1n}]};
  }
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from(`grpc-status: ${code}\r\n`),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
 });
 const current=page.getByTestId("investment-current"),reload=current.getByRole("button",{name:locale==="ru"?"Обновить оценку сейчас":"Reload current valuation",exact:true});
 await page.goto("/investments?series=series&from=2024-01-01&to=2024-01-02");await expect(current.getByTestId("current-known")).toHaveText("120 USD");await expect(page.getByTestId("overview-nav")).toContainText("110 USD");
 await page.goto("/investments/value?series=series&from=2024-01-01&to=2024-01-02");
 await expect(page.getByTestId("nav-current-point")).toHaveCount(1);await page.getByTestId("nav-chart").screenshot({path:`/private/tmp/budget-current-chart-${locale}.png`});await expect(page.getByTestId("nav-current-connector")).toHaveCount(1);await expect(page.getByTestId("nav-chart")).toContainText(locale==="ru"?"Сейчас (предварительно)":"Now (provisional)");
 await expect(page.getByRole("table",{name:locale==="ru"?"Оценка по дням":"Daily valuations",exact:true}).locator("tbody tr")).toHaveCount(2);
 await current.locator("summary").click();await expect(current).toContainText("tinvest/personal");await expect(current).toContainText("quote");await expect(current).toContainText("market@2");
 partial=true;await reload.click();await expect(current.getByTestId("current-known")).toHaveText("0 USD");await expect(current).toContainText(locale==="ru"?"Известная часть":"Known subtotal");await expect(current).toContainText(locale==="ru"?"Не используется в полной оценке":"Excluded from full valuation");await expect(page.getByTestId("nav-current-point")).toHaveCount(0);await expect(page.getByTestId("nav-current-connector")).toHaveCount(0);await expect(page.getByTestId("nav-segment")).toHaveCount(1);
 failed=true;await reload.click();await expect(current.getByRole("alert")).toBeVisible({timeout:15000});await expect(current.getByTestId("current-known")).toHaveCount(0);await expect(page.getByTestId("nav-segment")).toHaveCount(1);
 failed=false;partial=false;wrongScope=true;await reload.click();await expect(current).toContainText(locale==="ru"?"оценка недоступна":"valuation is unavailable");await expect(page.getByTestId("nav-current-point")).toHaveCount(0);
 wrongScope=false;await reload.click();await expect(page.getByTestId("nav-current-point")).toHaveCount(1);
 // Historical range ending before the last completed day has no current marker.
 await page.getByLabel(locale==="ru"?"По дату":"To",{exact:true}).fill("2024-01-01");await expect(page).toHaveURL(/to=2024-01-01/);
 await expect(page.getByTestId("nav-current-point")).toHaveCount(0);
 const finance=calls.filter(c=>["GetInvestmentNAVHistory","GetInvestmentPerformance","GetInvestmentProfit","GetInvestmentOverview","GetInvestmentCompositionHistory"].includes(c.method));for(const c of finance)expect(c.input.toDate).not.toBe("2024-01-03");
 expect(calls.filter(c=>!/^(Get|List)/.test(c.method))).toEqual([]);
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await current.locator("summary").click();await current.screenshot({path:`/private/tmp/budget-current-${locale}.png`});
});
