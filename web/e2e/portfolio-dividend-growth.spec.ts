import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])for(const member of [false,true])test(`${locale} issuer dividend review ${member?"is read only for members":"keeps evidence separate from personal receipts"}`,async({page,context})=>{
 const ru=locale==="ru",readOnly=process.env.E2E_INVESTMENTS_READ_ONLY==="true",uid="55555555-5555-4555-8555-555555555555",key="tinvest:"+uid,source={provider:"tinvest",accessScope:"personal"},now=BigInt(Math.floor(Date.now()/1000));
 const instrument={instrumentId:uid,ticker:"SHARE",name:"Synthetic issuer",kind:"share",currency:"RUB",isin:"SYNTHETIC",detailed:true};
 let history:any,conflict=false,issuerWrites=0,financialWrites=0,sourceJobs=0;const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,MarketDataService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  let data:any={},status=0;const f=(n:string)=>({numerator:n,denominator:"1"});
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:member?3:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers:[]};
  if(method?.name==="ListBrokerSnapshots")data={snapshots:[]};
  if(method?.name==="ListInvestmentValuationProfiles")data={fxProfiles:[]};
  if(method?.name==="GetTrackerInstrumentPrices"){
   const points:any[]=[];let date=input.fromDate;while(date<=input.toDate&&points.length<31){points.push({date,state:"unavailable",issues:["price_missing"]});date=new Date(Date.parse(date)+86400000).toISOString().slice(0,10);}
   data={instrumentKey:key,currency:input.currency,fxPolicyKey:input.fxPolicyKey,fxMode:"unavailable",basisKey:"a".repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow",nextDate:date<=input.toDate?date:"",points,instrument,metadataAt:{seconds:now},source,metadataInstrumentId:uid,metadataEnabled:true};
  }
  if(method?.name==="GetInvestmentDividendGrowth"){
   expect(input.instrumentKey).toBe(key);
   data={asOf:{seconds:now},histories:history?[{history,years:[{year:2023,regular:history.cancelled?undefined:f("50"),special:history.cancelled?undefined:f("0"),total:history.cancelled?undefined:f("50"),status:history.cancelled?"cancelled":"incomplete_history"},{year:2024,regular:history.cancelled?undefined:f("75"),special:history.cancelled?undefined:f("20"),total:history.cancelled?undefined:f("95"),change:history.cancelled?undefined:f("25"),percentChange:history.cancelled?undefined:f("50"),specialCount:1,status:history.cancelled?"cancelled":"available"}]}]:[]};
  }
  if(method?.name==="SaveInvestmentDividendHistory"){
   issuerWrites++;expect(member||readOnly).toBe(false);expect(input.history.instrumentKey).toBe(key);expect(input.expectedVersion).toBe(history?.version||0n);
   if(conflict){status=10;conflict=false;}else{
    expect(input.history.years.map((y:any)=>y.complete)).toEqual([true,true]);expect(input.history.dividends).toHaveLength(3);expect(input.history.dividends.every((d:any)=>d.sourceReference==="Synthetic issuer statements"&&d.status==="paid"&&d.unitBasisAt)).toBe(true);expect(input.history.splits[0]).toMatchObject({newUnits:"2",oldUnits:"1"});
    history={...input.history,version:input.expectedVersion+1n};data={history};
   }
  }
  if(method?.name==="GetMarketDividendFacts"){
   expect(input.source).toMatchObject(source);expect(input.instrumentId).toBe(uid);expect(input.to.seconds-input.from.seconds<=365n*86400n).toBe(true);
   const boundary=BigInt(Date.parse("2024-12-31T00:00:00Z")/1000),end=BigInt(Date.parse("2025-01-01T00:00:00Z")/1000),d={recordDate:{seconds:boundary},paymentDate:{seconds:end+86400n},dividendNet:{amount:"12",currency:"RUB"},dividendType:"source-type",regularity:"source-regularity"};
   data={source,instrument,coverageState:"observations_only",snapshotId:"snapshot-"+input.from.seconds,asOf:{seconds:now},dividends:[...(boundary>=input.from.seconds&&boundary<=input.to.seconds?[d]:[]),...(end>=input.from.seconds&&end<=input.to.seconds?[{...d,recordDate:{seconds:end},dividendNet:{amount:"777",currency:"RUB"}}]:[])]};
  }
  if(method?.name==="RequestMarketRefresh"){sourceJobs++;expect(input.kind).toBe("dividend_facts");expect(input.source).toMatchObject(source);expect(input.instrumentId).toBe(uid);expect(input.to.seconds-input.from.seconds<=365n*86400n).toBe(true);data={job:{id:"source-job-"+sourceJobs,status:"pending"}};}
  if(method?.name==="GetMarketDataJob")data={job:{id:input.id,status:"ready"}};
  if(method&&method.name!=="SaveInvestmentDividendHistory"&&/^(Create|Update|Commit|Post|Save|Delete|Import)/.test(method.name))financialWrites++;
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from(`grpc-status: ${status}\r\n`),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments/instruments/"+encodeURIComponent(key)+"?from=2024-01-01&to=2024-01-02");
 const issuer=page.getByRole("region",{name:ru?"Дивиденды на акцию":"Dividends per share",exact:true});await expect(issuer).toContainText(ru?"Нет проверенной истории":"No reviewed issuer history");
 await issuer.locator("summary").filter({hasText:ru?"Наблюдения источника":"Provider observations"}).click();await issuer.getByRole("spinbutton",{name:ru?"Год дат записи поставщика (UTC), не дат выплат":"Provider record-date year (UTC), not payment dates"}).fill("2024");
 await expect(issuer.getByText(/dividendNet: 12 RUB/)).toHaveCount(1);await expect(issuer).not.toContainText("777");expect(issuerWrites).toBe(0);
 if(member||readOnly){await expect(issuer.getByRole("button",{name:ru?"Добавить историю эмитента":"Add issuer history",exact:true})).toHaveCount(0);await expect(issuer.getByRole("button",{name:ru?"Запросить наблюдения у поставщика":"Request provider observations",exact:true})).toHaveCount(0);expect(financialWrites+issuerWrites+sourceJobs).toBe(0);return;}
 await issuer.getByRole("button",{name:ru?"Запросить наблюдения у поставщика":"Request provider observations",exact:true}).click();await expect.poll(()=>sourceJobs).toBe(2);await expect(issuer.getByRole("button",{name:ru?"Запросить наблюдения у поставщика":"Request provider observations",exact:true})).toBeEnabled();expect(issuerWrites).toBe(0);
 await issuer.getByRole("button",{name:ru?"Добавить историю эмитента":"Add issuer history",exact:true}).click();const editor=page.getByRole("dialog");
 await editor.getByLabel(ru?"Сплиты проверены начиная с (UTC)":"Splits reviewed from (UTC)").fill("2023-01-01T00:00");await editor.getByLabel(ru?"Источник: ссылка или документ":"Source: link or document").fill("Synthetic issuer statements");await editor.getByLabel(ru?"Что проверено по источнику":"Source review notes").fill("All issuer payouts and share units reviewed");
 for(const [i,date,amount,kind] of [[0,"2023-07-01","100","regular"],[1,"2024-07-01","75","regular"],[2,"2024-09-01","20","special"]] as const){
  await editor.getByRole("button",{name:ru?"Добавить выплату":"Add payout",exact:true}).click();await editor.getByLabel(ru?"Дата выплаты эмитента":"Issuer payment date",{exact:true}).nth(i).fill(date);await editor.getByLabel(ru?"Сумма на акцию":"Amount per share",{exact:true}).nth(i).fill(amount);
  await expect(editor.getByLabel(ru?"Факт выплаты эмитента":"Issuer payment status",{exact:true}).nth(i)).toHaveValue("unknown");await editor.getByLabel(ru?"Тип дивиденда":"Dividend class",{exact:true}).nth(i).selectOption(kind);await editor.getByLabel(ru?"Факт выплаты эмитента":"Issuer payment status",{exact:true}).nth(i).selectOption("paid");await editor.getByLabel(ru?"Единица этой суммы на (UTC)":"This amount's share unit at (UTC)",{exact:true}).nth(i).fill(date+"T00:00");
 }
 await editor.getByRole("checkbox",{name:/2023/}).check();await editor.getByRole("checkbox",{name:/2024/}).check();await editor.locator("summary").filter({hasText:ru?"Сплиты и изменения единицы":"Splits and share-unit changes"}).click();await editor.getByRole("button",{name:ru?"Добавить сплит":"Add split",exact:true}).click();await editor.getByLabel(ru?"Момент сплита (UTC)":"Split instant (UTC)").fill("2024-01-01T00:00");await editor.getByLabel(ru?"Новых акций":"New units").fill("2");await editor.getByLabel(ru?"Старых акций":"Old units").fill("1");
 await editor.getByRole("button",{name:ru?"Сохранить историю":"Save history",exact:true}).click();await expect(editor.getByRole("alert")).toBeVisible();expect(issuerWrites).toBe(0);
 const reviewed=editor.getByRole("checkbox",{name:ru?"Проверены все сплиты и изменения единицы за указанный период":"All splits and share-unit changes in the stated range were reviewed",exact:true});await reviewed.check();await editor.getByRole("button",{name:ru?"Сохранить историю":"Save history",exact:true}).click();await expect(editor).toHaveCount(0);
 await expect(issuer).toContainText("50%");await issuer.getByRole("button",{name:ru?"Изменить историю":"Edit history",exact:true}).click();conflict=true;await reviewed.check();await editor.getByRole("button",{name:ru?"Сохранить историю":"Save history",exact:true}).click();await expect(editor.getByRole("alert")).toBeVisible();await expect(editor.getByLabel(ru?"Сумма на акцию":"Amount per share",{exact:true}).nth(0)).toHaveValue("100");
 await editor.getByRole("button",{name:ru?"Загрузить сохранённое":"Reload saved history",exact:true}).click();await expect(editor).toHaveCount(0);await issuer.getByRole("button",{name:ru?"Изменить историю":"Edit history",exact:true}).click();await editor.getByRole("checkbox",{name:ru?"Исключить эту историю из сравнения":"Exclude this history from comparisons",exact:true}).check();await editor.getByRole("button",{name:ru?"Сохранить историю":"Save history",exact:true}).click();await expect(editor).toHaveCount(0);await expect(issuer).toContainText(ru?"Исключено":"Excluded");
 await issuer.getByRole("button",{name:ru?"Изменить историю":"Edit history",exact:true}).click();await editor.getByRole("checkbox",{name:ru?"Исключить эту историю из сравнения":"Exclude this history from comparisons",exact:true}).uncheck();await reviewed.check();await editor.getByRole("button",{name:ru?"Сохранить историю":"Save history",exact:true}).click();await expect(editor).toHaveCount(0);await expect(issuer).toContainText("50%");expect(history.version).toBe(3n);
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`issuer-dividends-${locale}.png`),fullPage:true});await issuer.getByRole("button",{name:ru?"Изменить историю":"Edit history",exact:true}).click();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await expect(editor).toHaveCSS("opacity","1");await expect(editor).not.toHaveCSS("background-color","rgba(0, 0, 0, 0)");await page.screenshot({path:test.info().outputPath(`issuer-editor-${locale}.png`),fullPage:false});await editor.getByRole("button",{name:"Close",exact:true}).click();await expect(editor).toHaveCount(0);expect(financialWrites).toBe(0);expect(errors).toEqual([]);
});
