import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
const amount=(n:string,d="1")=>({numerator:n,denominator:d});
for(const locale of ["ru","en"]){test(`${locale} FIFO lots preserve precision, lineage, FX and independent pagination`,async({page,context})=>{
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const calls:{method:string,input:any}[]=[];let conflict=false;
 const series={key:"series",ledgerIds:["a"],currency:"USD",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-03"};
 const original={originId:"buy",acquiredAt:{seconds:1704070800n},quantity:amount("3"),cleanCost:amount("1"),acquiredAccrued:amount("0")};
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};
  const bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};if(method)calls.push({method:method.name,input});
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:[series]};if(method?.name==="GetInvestmentNAVSeries")data={series};
  if(method?.name==="GetInvestmentInstrument")data={instrumentKey:"S",metadata:{name:"Example",kind:"share"},holdings:{date:input.date,currency:"USD"}};
  if(method?.name==="GetInvestmentLots")data={date:input.date,currency:"USD",fxPolicyKey:"market",total:51,nextCursor:input.cursor?"":"next",lots:input.cursor?[{ledgerId:"b",ledgerName:"IIS",bookCurrency:"RUB",lot:{originId:"unknown",quantity:amount("1"),acquiredAccrued:amount("0")},historicalAcquiredAccrued:amount("0"),historicalAccruedComplete:true,reasons:["book_value_unknown"]}]:[{ledgerId:"a",ledgerName:"Ordinary",bookCurrency:"RUB",lot:{...original,quantity:amount("9007199254740993"),cleanCost:amount("2","3")},origin:{id:"buy",kind:"buy",instrumentKey:"OLD",quantity:"3",amount:"1",currency:"RUB",fee:"0"},historicalCleanCost:amount("1","15"),historicalAcquiredAccrued:amount("0"),historicalCleanComplete:true,historicalAccruedComplete:true,acquisitionFxRate:amount("1","10"),acquisitionFxCutoff:{seconds:1704142799n},fxLegs:[{id:"fixing",revision:2n,fromCurrency:"USD",toCurrency:"RUB",policy:"market:1",inverse:true,asOf:{seconds:1704067200n}}],changes:[{operationId:"buy",kind:"buy",instrumentKey:"OLD",at:{seconds:1704070800n},after:original},{operationId:"sale",kind:"sell",instrumentKey:"S",at:{seconds:1704074400n},before:original,after:{...original,quantity:amount("2"),cleanCost:amount("2","3")}}]}]};
  const error=conflict&&method?.name==="GetInvestmentLots"&&input.cursor;
  const body=method&&!error?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from(`grpc-status: ${error?10:0}\r\n`),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/instruments/S?series=series&date=2024-01-03&group=chosen");
 const lots=page.getByRole("region",{name:locale==="ru"?"Остатки лотов FIFO":"Remaining FIFO lots",exact:true});
 await expect(lots.getByText("9007199254740993",{exact:true})).toBeVisible();await expect(lots.getByText("0.066666666667 USD",{exact:true})).toBeVisible();
 await lots.locator("summary").filter({hasText:locale==="ru"?"Курс приобретения":"Acquisition FX rate"}).click();await expect(lots.getByText(/USD\/RUB.*fixing v2/)).toBeVisible();
 await lots.locator("summary").filter({hasText:locale==="ru"?"История приобретения":"Acquisition history"}).click();await expect(lots.getByText("sale",{exact:true})).toBeVisible();await expect(lots.locator('[title="2/3"]')).toHaveCount(2);
 await lots.getByRole("button",{name:locale==="ru"?"Далее":"Next",exact:true}).click();await expect(lots.getByRole("heading",{name:"IIS"})).toBeVisible();await expect(lots.getByText(locale==="ru"?"Расчёт неполный: Себестоимость неизвестна":"Incomplete calculation: Unknown book value",{exact:true})).toBeVisible();
 expect(calls.filter(c=>c.method==="GetInvestmentLots").every(c=>c.input.groupIds[0]==="chosen"&&c.input.date==="2024-01-03")).toBe(true);
 await lots.getByRole("button",{name:locale==="ru"?"Назад":"Previous",exact:true}).click();await expect(lots.getByText("9007199254740993",{exact:true})).toBeVisible();
 conflict=true;await lots.getByRole("button",{name:locale==="ru"?"Далее":"Next",exact:true}).click();await expect(lots.getByRole("alert")).toBeVisible();await lots.getByRole("button",{name:locale==="ru"?"Обновить":"Reload",exact:true}).click();await expect(lots.getByText("9007199254740993",{exact:true})).toBeVisible();
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:`/private/tmp/budget-investment-lots-${locale}.png`,fullPage:true});
 expect(calls.filter(c=>!/^(Get|List|Search)/.test(c.method))).toEqual([]);
});}
