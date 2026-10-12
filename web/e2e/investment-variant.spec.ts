import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
const amount={numerator:"100",denominator:"1",decimal:"100"};
for(const locale of ["ru","en"])test(`${locale} currency and FX retain scope and dates on overview, analytics and instrument`,async({page,context})=>{
 const readonly=process.env.E2E_INVESTMENTS_READ_ONLY==="true";
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const series=(key:string)=>({key,ledgerIds:["ordinary","iis"],currency:key.split("-")[0].toUpperCase(),fxPolicyKey:key.endsWith("cbr")?"cbr":"market",fromDate:"2024-01-01",toDate:"2024-01-03"});
 const registered=new Set(["usd-market","rub-market","rub-cbr"]);let failure=0,badScope=false;
 const calls:{method:string,input:any}[]=[];
 let holdVariant=false;let releaseVariant:()=>void=()=>{};let variantSeen:()=>void=()=>{};
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentImportService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));
  const bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};let data:any={},code=0;if(method)calls.push({method:method.name,input});
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Variants"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:[series("usd-market")]};
  if(method?.name==="GetInvestmentNAVSeries")data={series:series(input.seriesKey)};
  if(method?.name==="ListInvestmentValuationProfiles")data=input.afterFxKey?{fxProfiles:[{key:"cbr",mode:"cbr",version:1n}]}:{fxProfiles:[{key:"market",mode:"market",version:2n}],nextFxKey:"market"};
  if(method?.name==="GetInvestmentNAVVariant"||method?.name==="PrepareInvestmentNAVVariant"){
   if(holdVariant&&method.name==="GetInvestmentNAVVariant"){variantSeen();await new Promise<void>(resolve=>{releaseVariant=resolve});}
   const key=input.currency.toLowerCase()+"-"+input.fxPolicyKey;
   if(method.name==="PrepareInvestmentNAVVariant"){expect(readonly).toBe(false);registered.add(key)}
   data={series:{...series(key),ledgerIds:badScope?["other"]:["iis","ordinary"]},registered:badScope||registered.has(key),queuedDays:method.name.startsWith("Prepare")?3:0};code=failure;
  }
  if(method?.name==="GetInvestmentNAVHistory")data={points:["2024-01-02","2024-01-03"].map(date=>({date,state:input.seriesKey==="eur-market"?"incomplete":"ready",knownValue:amount}))};
  if(method?.name==="GetInvestmentOverview")data={seriesKey:input.seriesKey,performance:{currency:series(input.seriesKey).currency},nav:{date:input.toDate,state:"ready",knownValue:amount}};
  if(method?.name==="GetInvestmentInstrument")data={instrumentKey:"ABC",metadata:{instrumentId:"ABC",name:"Variant instrument",currency:"RUB",kind:"share"},holdings:{currency:series(input.seriesKey).currency}};
  if(method?.name==="GetInvestmentInstrumentPrices")data={currency:series(input.seriesKey).currency,points:["2024-01-02","2024-01-03"].map(date=>({date,state:"ready",nativeCurrency:"RUB",nativeClean:amount,targetClean:amount}))};
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from(`grpc-status: ${code}\r\n`),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
 });
 const section=page.getByTestId("investment-variant"),currency=section.getByLabel(locale==="ru"?"Валюта оценки":"Valuation currency",{exact:true}),fx=section.getByLabel(locale==="ru"?"Политика исторических курсов":"Historical FX policy",{exact:true}),apply=section.getByRole("button",{name:locale==="ru"?"Применить валюту и FX":"Apply currency and FX",exact:true});
 for(const path of ["/investments","/investments/value","/investments/instruments/ABC"]){
  await page.goto(path+"?series=usd-market&from=2024-01-02&to=2024-01-03&period=custom&group=selected&priceCurrency=reporting");await expect(currency).toHaveValue("USD");await expect(page.getByRole("link",{name:locale==="ru"?"Официальные курсы валют: Банк России":"Official exchange rates: Bank of Russia",exact:true})).toHaveAttribute("href","https://www.cbr.ru/currency_base/");
  await currency.selectOption("EUR");await apply.click();
  if(readonly){await expect(section.getByRole("alert")).toContainText(locale==="ru"?"ещё не подготовлен":"not prepared yet");await expect(page).toHaveURL(/series=usd-market/)}else{await expect(page).toHaveURL(/series=eur-market/);await expect(currency).toHaveValue("EUR")}
  await currency.selectOption("RUB");await apply.click();await expect(page).toHaveURL(/series=rub-market/);await expect(currency).toHaveValue("RUB");
  expect(new URL(page.url()).searchParams.get("from")).toBe("2024-01-02");expect(new URL(page.url()).searchParams.get("to")).toBe("2024-01-03");expect(new URL(page.url()).searchParams.get("group")).toBe("selected");
  const more=section.getByRole("button",{name:locale==="ru"?"Ещё политики курсов":"More FX policies",exact:true});if(await more.count())await more.click();
  await expect(fx.getByRole("option",{name:/ЦБ|CBR/})).toHaveCount(1);await fx.selectOption("cbr");await apply.click();await expect(page).toHaveURL(/series=rub-cbr/);await expect(fx).toHaveValue("cbr");await page.reload();await expect(currency).toHaveValue("RUB");await expect(fx).toHaveValue("cbr");
 }
 // Failed policy and a mismatched account scope retain the actual old view.
 await currency.selectOption("USD");failure=9;await apply.click();await expect(section.getByRole("alert")).toContainText(locale==="ru"?"недоступны":"unavailable");await expect(page).toHaveURL(/series=rub-cbr/);
 failure=0;badScope=true;await apply.click();await expect(section.getByRole("alert")).toContainText(locale==="ru"?"Не удалось переключить":"Could not switch");await expect(page).toHaveURL(/series=rub-cbr/);
 const financial=calls.filter(c=>["GetInvestmentNAVHistory","GetInvestmentPerformance","GetInvestmentProfit","GetInvestmentOverview","GetInvestmentCompositionHistory","GetInvestmentInstrumentPrices"].includes(c.method));expect(financial.length).toBeGreaterThan(0);for(const c of financial)expect(c.input).toMatchObject({fromDate:"2024-01-02",toDate:"2024-01-03"});
 expect(calls.filter(c=>c.method==="PrepareInvestmentNAVVariant").length).toBe(readonly?0:1);
 expect(calls.filter(c=>!/^(Get|List|PrepareInvestmentNAVVariant)/.test(c.method))).toEqual([]);
 // A completed response for an old range must not enqueue or navigate.
 failure=0;badScope=false;await page.goto("/investments/value?series=usd-market&from=2024-01-02&to=2024-01-03");await expect(currency).toHaveValue("USD");
 holdVariant=true;const seen=new Promise<void>(resolve=>{variantSeen=resolve});await currency.selectOption("EUR");await apply.click();await seen;
 await page.getByLabel(locale==="ru"?"С даты":"From",{exact:true}).fill("2024-01-01");await expect(page).toHaveURL(/from=2024-01-01/);releaseVariant();await expect(apply).toBeEnabled();await expect(page).toHaveURL(/series=usd-market/);
 expect(calls.filter(c=>c.method==="PrepareInvestmentNAVVariant").length).toBe(readonly?0:1);
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await section.screenshot({path:test.info().outputPath(`budget-variant-${locale}.png`)});
});
