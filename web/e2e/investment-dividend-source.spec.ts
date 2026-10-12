import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
const id="11111111-1111-4111-8111-111111111111",uid="22222222-2222-4222-8222-222222222222";
async function fixture(page:any,context:any,locale:string,role=1){
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript((id:string)=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant",id)},id);
 const calls:any[]=[];let ready=false;
 const ts=(v:string)=>({seconds:BigInt(Date.parse(v+"T00:00:00Z")/1000),nanos:0});
 await page.route("**/budget.v1.*/*",async(route:any)=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,TenantService,MarketDataService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  calls.push({name:method?.name,input});let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id,name:"Declarations",defaultCurrencyCode:"RUB"},role}]};
  if(method?.name==="GetInvestmentForecast")data={fromMonth:"2026-10",toMonth:"2027-09",currency:"RUB",fxPolicy:"market@3"};
  if(method?.name==="ListMarketSources")data={sources:[{source:{provider:"tinvest",accessScope:"personal"},title:"Personal"}]};
  if(method?.name==="RequestMarketRefresh"){ready=true;data={job:{id,kind:"dividend_facts",status:"pending"}}}
  if(method?.name==="GetMarketDataJob")data={job:{id,kind:"dividend_facts",status:"ready"}};
  if(method?.name==="GetMarketDividendFacts")data=ready?{snapshotId:id,payloadHash:"a".repeat(64),instrument:{instrumentId:uid,name:"Public stock"},coverageState:"observations_only",asOf:ts("2026-10-12"),dividends:[{dividendType:"Regular Cash",regularity:"Annual",dividendNet:{amount:"9007199254740993.000000001",currency:"RUB"},recordDate:ts("2026-11-01"),paymentDate:ts("2027-02-01"),declaredDate:ts("2026-10-01"),lastBuyDate:ts("2026-10-30")},{dividendType:"Cancelled",dividendNet:{amount:"0",currency:"RUB"},recordDate:ts("2026-11-02"),paymentDate:ts("2026-11-20"),declaredDate:ts("2026-10-02")},{dividendType:"Return of Capital",recordDate:ts("2026-11-03")}]}:{coverageState:"unavailable"};
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=0x80;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });return calls;
}
for(const locale of ["ru","en"]){
 test(`${locale} provider declaration refresh and reviewed draft`,async({page,context})=>{
  const ru=locale==="ru",calls=await fixture(page,context,locale);
  await page.goto("/investments/forecast?month=2026-10&currency=RUB");await expect(page.getByRole("heading",{name:ru?"Прогноз выплат":"Payout forecast",exact:true})).toBeVisible();
  expect(calls.some(c=>c.name==="ListMarketSources")).toBe(false);
  await page.getByText(ru?"Объявления дивидендов Т‑Инвест":"T‑Invest dividend declarations",{exact:true}).click();
  const section=page.locator("details");await section.getByRole("combobox",{name:ru?"Источник":"Source",exact:true}).selectOption("0");await section.getByLabel("UID",{exact:false}).fill(uid);
  await section.getByLabel(ru?"Реестр с":"Record date from",{exact:true}).fill("2026-10-01");await section.getByLabel(ru?"Реестр по":"Record date through",{exact:true}).fill("2026-12-31");
  await section.getByRole("button",{name:ru?"Показать сохранённые объявления":"Show saved declarations",exact:true}).click();await expect(section).toContainText(ru?"Данные ещё не загружены":"Data has not been loaded");
  await section.getByRole("button",{name:ru?"Запросить обновление у провайдера":"Request provider refresh",exact:true}).click();await expect(section).toContainText("9007199254740993.000000001");await expect(section).toContainText("2027-02-01");await expect(section).toContainText("0 RUB · Cancelled");await expect(section).toContainText("Return of Capital");
  const refresh=calls.find(c=>c.name==="RequestMarketRefresh").input;expect(refresh).toMatchObject({kind:"dividend_facts",instrumentId:uid,source:{provider:"tinvest",accessScope:"personal"},from:{seconds:1790812800n},to:{seconds:1798761599n}});
  const draft=section.getByRole("button",{name:ru?"Подготовить черновик события":"Prepare event draft",exact:true});await expect(draft).toHaveCount(3);await expect(draft.nth(1)).toBeDisabled();await expect(draft.nth(2)).toBeDisabled();await draft.first().click();
  const dialog=page.getByRole("dialog");await expect(dialog.getByLabel(ru?"Идентификатор бумаги из журнала/карточки":"Instrument identifier from journal/card",{exact:true})).toHaveValue("");await expect(dialog.getByLabel(ru?"Сумма на одну бумагу":"Amount per unit",{exact:false})).toHaveValue("");await expect(dialog.getByRole("combobox",{name:ru?"Правило права":"Entitlement rule",exact:true})).toHaveValue("unknown");await expect(dialog.getByLabel(ru?"Ожидаемая дата выплаты":"Expected payment date")).toHaveValue("2027-02-01");await expect(dialog.getByLabel(ru?"Подтверждение суммы и правила права":"Reviewed amount and entitlement terms")).toContainText("9007199254740993.000000001");
  expect(calls.filter(c=>/^(Save|Mutate|Commit)/.test(c.name||""))).toEqual([]);
  await dialog.getByRole("button",{name:ru?"Закрыть":"Close",exact:true}).last().click();await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`budget145-dividend-source-${locale}.png`),fullPage:true});
 });
 test(`${locale} member reads without requesting provider refresh`,async({page,context})=>{
  const ru=locale==="ru",calls=await fixture(page,context,locale,3);await page.goto("/investments/forecast");await page.getByText(ru?"Объявления дивидендов Т‑Инвест":"T‑Invest dividend declarations",{exact:true}).click();await expect(page.locator("details").getByRole("combobox",{name:ru?"Источник":"Source",exact:true})).toBeVisible();await expect(page.getByRole("button",{name:ru?"Запросить обновление у провайдера":"Request provider refresh",exact:true})).toHaveCount(0);expect(calls.some(c=>c.name==="RequestMarketRefresh")).toBe(false);
 });
}
