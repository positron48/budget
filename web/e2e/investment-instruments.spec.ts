import {checkInvestmentChartKeyboard} from "./investment-chart-helpers";
import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
const amount=(v:string)=>({numerator:v,denominator:"1",decimal:v});
const series={key:"series",ledgerIds:["a","b"],currency:"USD",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-03"};
const meta={instrumentId:"listing",ticker:"ABC",isin:"TESTISIN",classCode:"TQBR",name:"Example instrument",kind:"share",currency:"RUB"};
const rows=[{ledgerId:"a",ledgerName:"Ordinary",instrumentKey:"ABC",quantity:"9007199254740993",bookCurrency:"RUB",cleanCost:"101",historicalCleanCost:{numerator:"101",denominator:"10",decimal:"10.1"},historicalCleanComplete:true,historicalAcquiredAccrued:amount("0"),historicalAccruedComplete:true,realizedClean:"3",realizedAccrued:"0",acquiredAccrued:"0",value:amount("12"),unrealizedClean:amount("19"),metadata:meta,payments:[{currency:"RUB",gross:amount("10"),tax:amount("1"),net:amount("9")}]},{ledgerId:"b",ledgerName:"IIS",instrumentKey:"ABC",quantity:"1",bookCurrency:"USD",cleanCost:"10",historicalCostReasons:["acquisition_date_missing"],value:amount("13"),metadata:meta}];
for(const locale of ["ru","en"]){test(`${locale} positions and instrument preserve exact totals, gaps, FX and filters`,async({page,context})=>{
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const calls:{method:string,input:any}[]=[];
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};
  const bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};if(method)calls.push({method:method.name,input});
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:[series]};if(method?.name==="GetInvestmentNAVSeries")data={series};
  if(method?.name==="GetInvestmentPositions")data={date:input.date,currency:"USD",valuationState:"incomplete",positions:[...rows,{ledgerId:"a",instrumentKey:"CLOSED",quantity:"0",bookCurrency:"RUB",cleanCost:"0"}]};
  if(method?.name==="GetInvestmentInstrument")data={instrumentKey:input.instrumentKey,metadata:meta,holdings:{date:input.date,currency:"USD",positions:input.instrumentKey==="ABC"?rows:[]},operations:input.instrumentKey==="ABC"?[{id:input.cursor?"older":"trade",ledgerId:"a",kind:input.cursor?"dividend":"buy",taxReviewRequired:!!input.cursor,timing:{timezone:"America/New_York",occurredDate:"2024-01-01",settlementDate:"2024-01-02"},instrumentKey:"ABC",quantity:"1",amount:"100",currency:"RUB",fee:"1",tax:"0",accrued:"0",status:"posted",occurredAt:{seconds:1704103200n}}]:[],nextCursor:!input.cursor&&input.instrumentKey==="ABC"?"next":""};
  if(method?.name==="GetInvestmentInstrumentPrices")data={currency:"USD",fxPolicyKey:"market",points:[{date:"2024-01-01",state:"ready",nativeCurrency:"RUB",nativeClean:amount("120"),targetClean:amount("12"),priceSource:"test",adjustment:"unadjusted"},{date:"2024-01-02",state:"unavailable",nativeCurrency:"RUB",issues:[{code:"price_unavailable"}]},{date:"2024-01-03",state:"ready",nativeCurrency:"RUB",nativeClean:amount("130"),targetClean:amount("6"),priceSource:"test",adjustment:"unadjusted"}],markers:[{id:"trade",ledgerId:"a",kind:"buy",quantity:"1",amount:"100",currency:"RUB",occurredAt:{seconds:1704103200n}}]};
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/value?group=selected");
 const positions=page.getByRole("heading",{name:locale==="ru"?"Позиции":"Positions",exact:true}).locator("..");
 await expect(positions.getByRole("cell",{name:"9007199254740994",exact:true})).toBeVisible();await expect(positions.getByRole("cell",{name:"25 USD",exact:true})).toBeVisible();
 await expect(positions.getByRole("link",{name:"CLOSED",exact:true})).toHaveCount(0);await positions.getByLabel(locale==="ru"?"Закрытые тоже":"Include closed",{exact:true}).click();await expect(positions.getByRole("link",{name:"CLOSED",exact:true}).first()).toBeVisible();
 await positions.getByRole("link",{name:"Example instrument",exact:true}).first().click();await expect(page).toHaveURL(/instruments\/ABC\?.*group=selected/);
 await expect(page.getByRole("heading",{name:"Example instrument",exact:true})).toBeVisible();await expect(page.getByRole("heading",{name:"Ordinary",exact:true})).toBeVisible();await expect(page.getByRole("heading",{name:"IIS",exact:true})).toBeVisible();
 await expect(page.getByText("10.1 USD",{exact:true})).toBeVisible();
 await expect(page.getByText(locale==="ru"?"Для пересчёта части лотов нужна дата приобретения":"Some lots need an acquisition date for conversion",{exact:true})).toBeVisible();
 const chart=page.getByRole("img",{name:locale==="ru"?"История цены инструмента":"Instrument price history",exact:true});await expect(chart.locator("path")).toHaveCount(2);await expect(chart.locator('[data-testid="instrument-trade-marker"]')).toHaveCount(1);
 const ys=await chart.locator("circle:not([data-testid])").evaluateAll(nodes=>nodes.map(n=>n.getAttribute("cy")));expect(ys).toEqual(["240","20"]);await checkInvestmentChartKeyboard(chart);
 await page.getByLabel(locale==="ru"?"В валюте оценки":"In valuation currency",{exact:true}).click();await expect.poll(()=>chart.locator("circle:not([data-testid])").evaluateAll(nodes=>nodes.map(n=>n.getAttribute("cy")))).toEqual(["20","240"]);
 const operations=page.getByRole("heading",{name:locale==="ru"?"История операций":"Operation history",exact:true}).locator("..");await operations.getByRole("button",{name:locale==="ru"?"Далее":"Next",exact:true}).click();await expect.poll(()=>calls.some(c=>c.method==="GetInvestmentInstrument"&&c.input.cursor==="next"&&c.input.groupIds[0]==="selected")).toBe(true);await expect(operations).toContainText(`2024-01-01 · ${locale==="ru"?"Только дата":"Date only"} · America/New_York`);await expect(operations).not.toContainText("MSK");await expect(operations).toContainText(locale==="ru"?"Сверка налога не завершена: gross и налог неизвестны":"Tax reconciliation pending: gross and tax are unknown");await expect(operations).toContainText(locale==="ru"?"Известное поступление net":"Known net receipt");await expect(operations.getByText(locale==="ru"?"Налог":"Tax",{exact:true}).locator("..")).toContainText("—");
 await expect(chart.locator('[data-testid="instrument-trade-marker"]')).toHaveCount(1);
 await expect(page.getByLabel(locale==="ru"?"В валюте оценки":"In valuation currency",{exact:true})).toBeChecked();
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`budget-investment-instrument-${locale}.png`),fullPage:true});
 await page.goto("/investments/instruments/UNOWNED?series=series&date=2024-01-03");await expect(page.getByText(locale==="ru"?"На выбранную дату и в выбранных группах позиций нет. Рыночную историю можно смотреть независимо от владения.":"No holdings for the selected date and groups. Market history is available independently of ownership.",{exact:true})).toBeVisible();await expect(page.getByRole("img",{name:locale==="ru"?"История цены инструмента":"Instrument price history",exact:true})).toBeVisible();
});}
for(const locale of ["ru","en"]){test(`${locale} unowned listing loads once and respects read-only mode`,async({page,context})=>{
 const readonly=process.env.E2E_INVESTMENTS_READ_ONLY==="true";
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const writes:string[]=[],source={provider:"tinvest",accessScope:"personal"},at=BigInt(Math.floor(Date.now()/1000)-86400*2);let loaded=false;
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};
  if(method&&!/^(Get|List|Search)/.test(method.name))writes.push(method.name);
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="ListMarketSources")data={sources:[{source,title:"Personal source",enabled:true}]};
  if(method?.name==="SearchMarketInstruments")data={instruments:[meta]};
  if(method?.name==="RequestMarketRefresh")data={job:{id:"load",status:"pending",kind:"daily_candle"}};
  if(method?.name==="GetMarketDataJob"){loaded=true;data={job:{id:"load",status:"ready",kind:"daily_candle"}};}
  if(method?.name==="GetMarketHistory")data={observations:loaded?[{id:"native",instrumentId:"listing",kind:"daily_candle",price:"101.5",priceUnit:"currency_per_unit",currency:"RUB",sourceKind:"exchange",adjustment:"unadjusted",complete:true,observedAt:{seconds:at}},{id:"adjusted",instrumentId:"listing",kind:"daily_candle",price:"1000",priceUnit:"currency_per_unit",currency:"RUB",sourceKind:"exchange",adjustment:"split_adjusted",complete:true,observedAt:{seconds:at}}]:[]};
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/instruments/tinvest%3Alisting?provider=tinvest&scope=personal&listing=listing");
 await expect(page.getByRole("heading",{name:"Example instrument",exact:true})).toBeVisible();
 const rawChart=page.getByRole("img",{name:locale==="ru"?"Исходный график провайдера":"Source provider chart",exact:true});
 if(readonly){await expect(page.getByRole("button",{name:locale==="ru"?"Обновить данные":"Refresh data",exact:true})).toBeDisabled();expect(writes).toEqual([]);await expect(rawChart).toHaveCount(0);}
 else {await expect(rawChart).toBeVisible();await expect(rawChart.locator("circle")).toHaveCount(1);await expect(rawChart.locator("circle title")).toContainText("101.5");await checkInvestmentChartKeyboard(rawChart);await page.getByLabel(locale==="ru"?"Ряд источника":"Source series",{exact:true}).selectOption({label:"RUB · currency_per_unit · exchange · split_adjusted"});await expect(rawChart.locator("circle title")).toContainText("1000");expect(writes).toEqual(["RequestMarketRefresh"]);}
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});}
