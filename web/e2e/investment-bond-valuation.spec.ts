import {test,expect} from "@playwright/test";
import {create,toBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
const amount=(decimal:string)=>({numerator:decimal.replace(".",""),denominator:decimal.includes(".")?"1"+"0".repeat(decimal.split(".")[1].length):"1",decimal});
for(const locale of ["ru","en"]){test(`${locale} dirty bond prices keep gaps historical FX and reviewed proof`,async({page,context})=>{
 const writes:string[]=[];const series={key:"bond-series",ledgerIds:["a"],currency:"USD",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-03"};
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
 const path=new URL(route.request().url()).pathname,svc=[TenantService,InvestmentService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/")),m=svc?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};
 if(m&&!/^(Get|List|Search)/.test(m.name))writes.push(m.name);
 if(m?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
 if(m?.name==="ListInvestmentNAVSeries")data={series:[series]};if(m?.name==="GetInvestmentNAVSeries")data={series};
 if(m?.name==="GetInvestmentInstrument")data={metadata:{instrumentId:"listing",name:"Reviewed bond",kind:"bond",currency:"RUB"},holdings:{positions:[]},operations:[]};
 if(m?.name==="GetInvestmentInstrumentPrices")data={currency:"USD",fxPolicyKey:"market",priceProfileVersion:3n,points:[{date:"2024-01-01",state:"ready",nativeCurrency:"RUB",percent:amount("120"),nativeClean:amount("1200"),nativeDirty:amount("1205.000000001"),targetClean:amount("120"),targetDirty:amount("120.5000000001"),accruedEvidence:{sourceId:"reviewed-bond:3/source/hash",instrumentId:"listing",currency:"RUB",amountPerUnit:"5.000000001",from:{seconds:1704067200n},to:{seconds:1704153600n}}},{date:"2024-01-02",state:"ready",nativeCurrency:"RUB",percent:amount("120"),nativeClean:amount("600"),targetClean:amount("30"),issues:[{code:"accrued_interest_missing"}]},{date:"2024-01-03",state:"ready",nativeCurrency:"RUB",percent:amount("120"),nativeClean:amount("600"),nativeDirty:amount("600"),targetClean:amount("30"),targetDirty:amount("30"),accruedEvidence:{sourceId:"reviewed-bond:3/zero/hash",instrumentId:"listing",currency:"RUB",amountPerUnit:"0",from:{seconds:1704240000n},to:{seconds:1704326400n}}}]};
 const body=m?Buffer.from(toBinary(m.output,create(m.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
 });
 await page.goto("/investments/instruments/B?series=bond-series&from=2024-01-01&to=2024-01-03&period=custom&priceMode=dirty");
 const chart=page.getByRole("img",{name:locale==="ru"?"История цены инструмента":"Instrument price history",exact:true});await expect(chart).toBeVisible();await expect(chart.locator("circle:not([data-testid])")).toHaveCount(2);await expect(chart.locator('[data-testid="instrument-price-segment"]')).toHaveCount(2);expect(await chart.locator('[data-testid="instrument-price-segment"]').evaluateAll(nodes=>nodes.every(n=>!n.getAttribute("d")?.includes("L")))).toBe(true);
 await expect(chart.locator("circle title").first()).toContainText("1205.000000001");
 await page.getByLabel(locale==="ru"?"В валюте оценки":"In valuation currency",{exact:true}).click();await expect(chart.locator("circle title").first()).toContainText("120.5000000001");
 await page.getByText(locale==="ru"?"Дневные цены и качество":"Daily prices and quality",{exact:true}).click();
 await expect(page.locator("td details")).toHaveCount(2);
 const proof=page.locator("td details").first();await proof.locator("summary").click();await expect(proof).toContainText("5.000000001 RUB");await expect(proof).toContainText("reviewed-bond:3/source/hash");await expect(proof).toContainText("2024-01-02T00:00:00.000Z");
 const zero=page.locator("td details").nth(1);await zero.locator("summary").click();await expect(zero).toContainText("0 RUB");await expect(page.getByText(locale==="ru"?"НКД на дату не подтверждён.":"Accrued interest is not confirmed for this date.",{exact:true})).toBeVisible();expect(writes).toEqual([]);
 await page.setViewportSize({width:375,height:900});await page.getByText(locale==="ru"?"Подтверждённый НКД":"Reviewed accrued interest",{exact:true}).first().click();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`budget-bond-valuation-${locale}.png`),fullPage:true});
});}
