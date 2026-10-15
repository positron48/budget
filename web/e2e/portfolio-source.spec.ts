import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {MarketDataService,InvestmentMarketMappingSchema} from "../proto/budget/v1/market_data_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])for(const owner of [true,false])test(`${locale} price source review ${owner?"keeps schedules independent and retries once":"blocks member writes"}`,async({page,context})=>{
 test.skip(process.env.E2E_INVESTMENTS_READ_ONLY==="true","Write runtime required");
 const ru=locale==="ru",id="11111111-1111-4111-8111-111111111111",key="portfolio:S",current={provider:"tinvest",accessScope:"personal"},history={provider:"moex",accessScope:"personal"},stamp=(date:string)=>({seconds:BigInt(Date.parse(date)/1000)}),period=(source:any,date:string)=>({source,instrumentId:id,kind:"share",currency:"RUB",unit:"currency_per_unit",from:stamp(date),evidence:"Synthetic reviewed source"});
 let mapping=create(InvestmentMarketMappingSchema,{instrumentKey:key,version:5n,periods:[period(current,"2020-01-01")],historicalPeriods:[period(history,"2019-01-01")]}),writes=0,attempts=0,metadataLoaded=false,metadataRequests=0,retryKey="",financialWrites=0;
 const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[MarketDataService,InvestmentService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  let data:any={},status=0;
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:owner?1:3}]};
  if(method?.name==="ListMarketSources")data={sources:[{source:current,title:"T-Invest"},{source:history,title:"MOEX"}]};
  if(method?.name==="SearchMarketInstruments")data={instruments:[{instrumentId:id,ticker:"SHARE",name:"Synthetic security",isin:"SYNTHETIC",classCode:"TQBR",kind:"share",currency:"RUB",detailed:input.source.provider==="moex"||metadataLoaded}]};
  if(method?.name==="GetInvestmentMarketMapping")data={mapping};
  if(method?.name==="RequestMarketRefresh"){expect(owner).toBe(true);expect(input.kind).toBe("last_price");expect(input.instrumentId).toBe(id);metadataRequests++;metadataLoaded=true;data={job:{id:"metadata-job",status:"pending"}};}
  if(method?.name==="GetMarketDataJob")data={job:{id:input.id,status:"ready"}};
  if(method?.name==="SaveInvestmentMarketMapping"){
   expect(owner).toBe(true);attempts++;expect(input.instrumentKey).toBe(key);expect(input.requestKey).toBeTruthy();
   if(attempts===1){retryKey=input.requestKey;status=14;}else{
    if(attempts===2){expect(input.requestKey).toBe(retryKey);expect(input.expectedVersion).toBe(5n);expect(input.replaceHistorical).toBe(true);expect(input.periods[0].from.seconds).toBe(stamp("2020-01-01").seconds);expect(input.historicalPeriods).toHaveLength(2);expect(input.historicalPeriods[0].to.seconds).toBe(stamp("2024-01-01").seconds);}else{expect(input.expectedVersion).toBe(6n);expect(input.replaceHistorical).toBe(false);expect(input.historicalPeriods).toHaveLength(0);expect(input.periods).toHaveLength(2);}
    writes++;mapping=create(InvestmentMarketMappingSchema,{instrumentKey:key,version:mapping.version+1n,periods:input.periods,historicalPeriods:input.replaceHistorical?input.historicalPeriods:mapping.historicalPeriods});data={mapping};
   }
  }
  if(method&&/^(Create|Update|Commit|Post|Delete|Import)/.test(method.name))financialWrites++;
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from(`grpc-status: ${status}\r\n`),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments/instruments?connect="+encodeURIComponent(key));await page.getByRole("textbox",{name:ru?"Тикер, название или ISIN":"Ticker, name or ISIN"}).fill("SHARE");await expect(page.getByRole("combobox",{name:ru?"Источник котировок":"Price source"})).toContainText("MOEX");await page.getByRole("button",{name:ru?"Выбрать этот листинг":"Select this listing",exact:true}).click();
 let review=page.getByRole("region",{name:ru?"Подтверждение источника":"Review price source",exact:true});
 if(owner){
  await review.getByLabel(ru?"Применять с даты (UTC)":"Apply from date (UTC)",{exact:true}).fill("2024-01-01");await expect(review.getByRole("button",{name:ru?"Подключить источник":"Connect source",exact:true})).toBeDisabled();await review.getByRole("checkbox").check();await review.getByRole("button",{name:ru?"Подключить источник":"Connect source",exact:true}).click();await expect(review.getByRole("status")).toBeVisible();await review.getByRole("button",{name:ru?"Подключить источник":"Connect source",exact:true}).click();await expect(review.getByRole("status")).toContainText(ru?"Источник сохранён":"Source saved");
  await page.getByRole("button",{name:ru?"Текущие цены":"Current prices",exact:true}).click();await page.getByRole("button",{name:ru?"Выбрать этот листинг":"Select this listing",exact:true}).click();review=page.getByRole("region",{name:ru?"Подтверждение источника":"Review price source",exact:true});
  await expect(review.getByRole("checkbox")).toBeDisabled();
  await review.getByRole("button",{name:ru?"Загрузить реквизиты":"Load metadata",exact:true}).click();await expect(review.getByText(ru?"Нужны полные реквизиты бумаги перед подтверждением.":"Complete instrument metadata is required before confirmation.",{exact:true})).toHaveCount(0);
  await review.getByLabel(ru?"Применять с даты (UTC)":"Apply from date (UTC)",{exact:true}).fill("2025-01-01");await review.getByRole("checkbox").check();await review.getByRole("button",{name:ru?"Подключить источник":"Connect source",exact:true}).click();await expect(review.getByRole("status")).toContainText(ru?"Источник сохранён":"Source saved");expect(writes).toBe(2);expect(metadataRequests).toBe(1);expect(mapping.historicalPeriods[1].from?.seconds).toBe(stamp("2024-01-01").seconds);
 }else{await expect(review.getByRole("checkbox")).toBeDisabled();await expect(review.getByRole("button",{name:ru?"Подключить источник":"Connect source",exact:true})).toBeDisabled();expect(writes).toBe(0);}
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0);});await page.screenshot({path:test.info().outputPath(`source-review-${locale}-${owner?"owner":"member"}.png`),fullPage:true});expect(financialWrites).toBe(0);expect(errors).toEqual([]);
});
