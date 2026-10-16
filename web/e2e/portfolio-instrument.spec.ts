import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])for(const owned of [false,true])test(`${locale} security card ${owned?"shows owned share costs and trades":"keeps unowned bond points separate from money"}`,async({page,context})=>{
 const ru=locale==="ru",uid="55555555-5555-4555-8555-555555555555",key="tinvest:"+uid,ledger="11111111-1111-4111-8111-111111111111",source={provider:"tinvest",accessScope:"personal"};
 let financialWrites=0,historyRequests=0,historyLoaded=!owned;const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
 const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,MarketDataService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
 let data:any={};const at=1704067200n,f=(n:string)=>({numerator:n,denominator:"1"}),instrument={instrumentId:uid,ticker:owned?"SHARE":"BOND",name:owned?"Synthetic share":"Synthetic amortizing bond",kind:owned?"share":"bond",currency:owned?"RUB":"USD",isin:"SYNTHETIC",detailed:true};
 if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:1}]};
 if(method?.name==="ListMarketSources")data={sources:[{source,title:"Synthetic source"}]};
 if(method?.name==="SearchMarketInstruments")data={instruments:[instrument]};
 if(method?.name==="ListInvestmentLedgers")data={ledgers:owned?[{id:ledger,name:"Broker",version:1n,portfolioTracking:true}]:[]};
 if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger:{id:ledger,name:"Broker",portfolioTracking:true},positions:[{instrumentKey:key,currency:"RUB",quantity:"1",cleanCost:"80",costBasis:"principal",lots:[{originId:"buy",quantity:"1",cleanCost:"80",acquiredAccrued:"0",acquiredAt:{seconds:at+86400n}}]}]}};
 if(method?.name==="ListInvestmentOperations")data={operations:[{id:"buy",ledgerId:ledger,kind:"buy",status:"posted",instrumentKey:key,currency:"RUB",quantity:"2",amount:"160",fee:"0",accrued:"0",occurredAt:{seconds:at+86400n}},{id:"sell",ledgerId:ledger,kind:"sell",status:"posted",instrumentKey:key,currency:"RUB",quantity:"1",amount:"90",fee:"0",accrued:"0",occurredAt:{seconds:at+2n*86400n}},{id:"div",ledgerId:ledger,kind:"dividend",status:"posted",instrumentKey:key,currency:"RUB",amount:"5",occurredAt:{seconds:at+2n*86400n}}]};
 if(method?.name==="GetInvestmentQuotes")data={quotes:owned?[{instrumentKey:key,currency:"RUB",state:"fresh",cleanPrice:f("120"),priceAt:{seconds:BigInt(Math.floor(Date.now()/1000))},source:"tinvest/personal"}]:[]};
 if(method?.name==="GetInvestmentExchangeRates")data={currency:input.currency,fxPolicyKey:"market",policyVersion:1n,mode:"market",rates:["",...input.dates].map((date:string)=>({date,fromCurrency:"RUB",rate:f("1")}))};
 if(method?.name==="ListInvestmentValuationProfiles")data={fxProfiles:[{key:"market",mode:"market",version:1n}]};
 if(method?.name==="RequestMarketHistory"){historyRequests++;expect(input.source).toMatchObject(source);expect(input.instrumentId).toBe(uid);expect(input.ledgerId).toBeUndefined();historyLoaded=true;data={jobs:[{id:"history-job",status:"pending"}]};}
 if(method?.name==="GetMarketDataJob")data={job:{id:input.id,status:"ready"}};
 if(method?.name==="GetTrackerInstrumentPrices"){
  const points=[];let day=input.fromDate;while(day<=input.toDate&&points.length<31){points.push({date:day,state:"ready",raw:f("90"),nativeClean:f(owned?"90":"900"),clean:f(owned?"90":"81000"),accrued:owned?undefined:f("3"),nativeDirty:f(owned?"90":"903"),dirty:f(owned?"90":"81270"),nativeCurrency:owned?"RUB":"USD",unit:owned?"currency_per_unit":"percent_of_nominal",source:"tinvest/personal",candleDate:new Date(Date.parse(day)-86400000).toISOString().slice(0,10),observationId:day,observationRevision:1n});day=new Date(Date.parse(day)+86400000).toISOString().slice(0,10);}
  if(!historyLoaded)for(const p of points){Object.assign(p,{state:"unavailable",raw:undefined,nativeClean:undefined,clean:undefined,accrued:undefined,nativeDirty:undefined,dirty:undefined,observationId:"",observationRevision:0n,candleDate:"",issues:["price_missing"]});}
  data={instrumentKey:key,currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",basisKey:(historyLoaded?"a":"b").repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow",nextDate:day<=input.toDate?day:"",points,instrument,metadataAt:{seconds:at},source,metadataInstrumentId:uid,metadataEnabled:false,listingOnly:true,listings:owned?[{id:"c".repeat(64),source,instrumentId:uid,kind:"share",currency:"RUB",unit:"currency_per_unit",enabled:true}]:[],bond:owned?undefined:{asOf:{seconds:at},terms:{nominal:{amount:"1000",currency:"USD"},initialNominal:{amount:"2000",currency:"USD"},currentAccrued:{amount:"99",currency:"USD"},couponFrequency:2,maturity:{seconds:1893456000n}}}};
 }
 if(method&&/^(Create|Update|Commit|Post|Save|Delete|Import)/.test(method.name))financialWrites++;
 const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments/instruments");await page.getByRole("textbox").fill("BOND");await page.getByRole("link",{name:owned?/SHARE.*Synthetic share/:/BOND.*Synthetic amortizing bond/}).click();
 await expect(page.getByRole("heading",{name:owned?"Synthetic share":"Synthetic amortizing bond",exact:true})).toBeVisible();
 await expect(page.getByRole("img",{name:ru?"История цены":"Price history",exact:true})).toBeVisible();if(owned)expect(historyRequests).toBeGreaterThan(0);
 await page.getByLabel(ru?"Выбрать даты":"Choose dates",{exact:true}).click();await page.getByLabel(ru?"История с":"History from",{exact:true}).fill("2024-01-02");await page.getByLabel(ru?"История по":"History through",{exact:true}).fill("2024-01-04");
 const chart=page.getByRole("region",{name:ru?"История цены бумаги":"Security price history"});
 await expect(chart.getByRole("img")).toBeVisible();await expect(chart.locator("svg polyline")).toHaveCount(1);
 if(!owned){await page.getByLabel(ru?"Вид цены":"Price view",{exact:true}).selectOption("nativeClean");await expect(chart.getByRole("img")).toBeVisible();
 await page.getByLabel(ru?"Вид цены":"Price view",{exact:true}).selectOption("nativeDirty");await expect(chart.getByRole("img")).toBeVisible();
 await expect(page.getByText(ru?"В выбранной области нет операций с этой бумагой.":"No operations for this security in the selected scope.")).toBeVisible();
 await page.getByText(ru?"Условия облигации":"Bond terms",{exact:true}).click();await expect(page.getByText(ru?"Погашение":"Maturity",{exact:true})).toBeVisible();}else{
 const positions=page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Позиции по счетам":"Positions by account",exact:true})});
 await expect(positions.locator("tbody td")).toHaveText(["Broker","1","80 RUB","80 RUB","120 RUB"]);
 await expect(chart.locator("circle")).toHaveCount(2);await expect(chart.locator("svg text").filter({hasText:ru?"Средняя в портфеле":"Portfolio average"})).toHaveCount(1);await page.getByText(ru?"Сделки и выплаты":"Trades and payouts",{exact:true}).click();
 await expect(page.getByRole("cell",{name:ru?"Дивиденды":"Dividend",exact:true})).toBeVisible();
 }
 await chart.getByLabel(ru?"Действия с графиком":"Chart actions",{exact:true}).click();const download=page.waitForEvent("download");await chart.getByRole("button",{name:"CSV",exact:true}).click();expect((await download).suggestedFilename()).toBe("instrument-history-2024-01-02-2024-01-04.csv");
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0);});await page.screenshot({path:test.info().outputPath(`security-card-${owned?"share":"bond"}-${locale}.png`),fullPage:true});
 expect(financialWrites).toBe(0);expect(errors).toEqual([]);
});
