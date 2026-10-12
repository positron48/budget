import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";

for(const locale of ["ru","en"]){test(`${locale} unowned multi-year history hydrates partial cache and exposes every window`,async({page,context})=>{
 const readonly=process.env.E2E_INVESTMENTS_READ_ONLY==="true";
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const source={provider:"tinvest",accessScope:"personal"},calls:any[]=[],jobs:any[]=[];
 const yesterday=new Date(Date.now()-86400000).toISOString().slice(0,10),at=BigInt(Math.floor(Date.now()/1000)-2*86400);
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));
  const bytes=route.request().postDataBuffer()!,input:any=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))):{};let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="ListMarketSources")data={sources:[{source,title:"Personal source"}]};
  if(method?.name==="SearchMarketInstruments")data={instruments:[{instrumentId:"listing",ticker:"ABC",name:"Unowned history",classCode:"TQBR",kind:"share",currency:"RUB"}]};
  if(method?.name==="GetMarketHistory")data={observations:[{id:"partial",instrumentId:"listing",kind:"daily_candle",price:"101.5",priceUnit:"currency_per_unit",currency:"RUB",sourceKind:"exchange",adjustment:"unadjusted",complete:true,observedAt:{seconds:at}}]};
  if(method?.name==="RequestMarketHistory"){
   calls.push(input);jobs.length=0;
   for(let cursor=input.from.seconds;cursor<input.to.seconds;){const next=cursor+365n*86400n<input.to.seconds?cursor+365n*86400n:input.to.seconds;jobs.push({id:`window-${jobs.length}`,instrumentId:"listing",source,kind:"daily_candle",status:"pending",from:{seconds:cursor},to:{seconds:next}});cursor=next;}
   data={jobs,coverageState:"requests_only"};
  }
  if(method?.name==="RequestMarketRefresh")data={job:{...jobs[0],id:"retry-0",status:"pending"}};
  if(method?.name==="GetMarketDataJob"){const retry=input.id==="retry-0",index=retry?0:Number(input.id.replace("window-",""));data={job:{...jobs[index],id:input.id,status:index===0&&!retry?"error":"ready",errorCode:index===0&&!retry?"provider_rejected":"",coverageState:index===1?"empty_unexplained":index===0&&!retry?"":"observed"}};}
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);
  await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/instruments/tinvest%3Alisting?provider=tinvest&scope=personal&listing=listing");
 await expect(page.getByRole("heading",{name:"Unowned history",exact:true})).toBeVisible();
 await page.getByLabel(locale==="ru"?"По дату (UTC)":"Through date (UTC)",{exact:true}).fill(yesterday);
 await page.getByLabel(locale==="ru"?"С даты (UTC)":"From date (UTC)",{exact:true}).fill("2019-10-01");
 if(readonly){await expect(page.getByRole("button",{name:locale==="ru"?"Обновить данные":"Refresh data",exact:true})).toBeDisabled();expect(calls).toEqual([]);}
 else{
  await expect.poll(()=>calls.length).toBe(1);
  expect(calls[0].source.provider).toBe("tinvest");expect(calls[0].source.accessScope).toBe("personal");expect(calls[0].instrumentId).toBe("listing");
  expect(calls[0].from.seconds).toBe(BigInt(Date.parse("2019-10-01T00:00:00Z")/1000));
  const count=jobs.length;expect(count).toBeGreaterThan(1);
  await expect(page.getByText(locale==="ru"?`Окна истории: завершено ${count-1} из ${count} запросов; ошибок: 1.`:`History windows: ${count-1} of ${count} requests finished; errors: 1.`,{exact:true})).toBeVisible();
  await expect(page.getByText(locale==="ru"?"Завершённые запросы не подтверждают полноту истории. Пустые окна и ошибки остаются видимыми.":"Finished requests do not prove full history coverage. Empty windows and errors remain visible.",{exact:true})).toBeVisible();
  const windows=page.getByRole("status").filter({hasText:locale==="ru"?"Окна истории":"History windows"}).getByRole("listitem");await expect(windows).toHaveCount(count);
  await expect(windows.nth(0)).toContainText(locale==="ru"?"Ошибка":"Failed");
  await expect(windows.nth(1)).toContainText(locale==="ru"?"В выбранном периоде наблюдений нет":"No observations in this period");
  await windows.nth(0).getByRole("button",{name:locale==="ru"?"Повторить":"Retry",exact:true}).click();
  await expect(page.getByText(locale==="ru"?`Окна истории: завершено ${count} из ${count} запросов; ошибок: 0.`:`History windows: ${count} of ${count} requests finished; errors: 0.`,{exact:true})).toBeVisible();
 }
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.screenshot({path:`/private/tmp/budget134-history-${locale}-${readonly?"readonly":"write"}.png`,fullPage:true});
});}
