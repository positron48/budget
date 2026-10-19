import {chartFallback} from "./fixtures/chart-fallback";
import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService,InvestmentGroupsSchema} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"]){test(`${locale} analytics charts show profit, periods and exact export`,async({page,context})=>{
 test.skip(process.env.E2E_INVESTMENTS_READ_ONLY==="true","Write runtime required");
 const ru=locale==="ru",a="11111111-1111-4111-8111-111111111111",b="22222222-2222-4222-8222-222222222222",one="33333333-3333-4333-8333-333333333333",two="44444444-4444-4444-8444-444444444444",uid="55555555-5555-4555-8555-555555555555",key="tinvest:"+uid;
 const ledgers=[{id:one,name:"Broker one",version:1n,portfolioTracking:true},{id:two,name:"Broker two",version:1n,portfolioTracking:true}];
 let classification=create(InvestmentGroupsSchema,{version:1n,groups:[{id:a,name:"Alpha"},{id:b,name:"Beta"}],memberships:[{ledgerId:one,instrumentKey:key,groupId:a},{ledgerId:two,instrumentKey:key,groupId:b}]});
 let writes=0,historyReads=0,partialHistory=false,missingCost=false,archivedCost=false,coldChart=false,globalHistoryIncludesArchived=false;let releaseHistory=()=>{};let heldHistory:Promise<void>|undefined;const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  const missingPrices=partialHistory&&!(input.instrumentKeys||[]).includes(key);
  let data:any={};const at=1767268800n,qty=input.ledgerId===one?"1":"2";
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers:input.includeArchived?[...ledgers,{id:"66666666-6666-4666-8666-666666666666",name:"Closed source",archived:true,portfolioTracking:true,version:1n}]:ledgers};
  if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger:ledgers.find(l=>l.id===input.ledgerId),positions:[{instrumentKey:key,currency:"RUB",quantity:qty,costBasis:"principal",lots:[{originId:input.ledgerId+"buy",quantity:qty,cleanCost:String(Number(qty)*100),acquiredAccrued:"0",acquiredAt:{seconds:at}}]}]}};
  if(method?.name==="GetInvestmentSnapshot"&&missingCost&&input.ledgerId===one)data.snapshot.positions.push({instrumentKey:"MISSING_COST",currency:"RUB",quantity:"1",costBasis:"principal",lots:[]});
  if(method?.name==="GetInvestmentSnapshot"&&archivedCost&&input.ledgerId===one){data.snapshot.positions[0].costBasis="";data.snapshot.positions[0].lots=[{originId:"archived-buy",acquisitionOperationId:"archived-buy",originLedgerId:"66666666-6666-4666-8666-666666666666",quantity:"1",cleanCost:"75",acquiredAccrued:"0",acquiredAt:{seconds:at}}];}
  if(method?.name==="ListInvestmentOperations")data={operations:[{id:input.ledgerId+"buy",ledgerId:input.ledgerId,kind:"buy",status:"posted",instrumentKey:key,currency:"RUB",amount:String(Number(qty)*100),quantity:qty,fee:"0",accrued:"0",occurredAt:{seconds:at}},{id:input.ledgerId+"div",ledgerId:input.ledgerId,kind:"dividend",status:"posted",instrumentKey:key,currency:"RUB",amount:String(Number(qty)*5),occurredAt:{seconds:at+86400n}}]};
  if(method?.name==="ListInvestmentOperations"&&archivedCost&&input.ledgerId==="66666666-6666-4666-8666-666666666666")data={operations:[{id:"archived-buy",ledgerId:input.ledgerId,kind:"buy",status:"posted",instrumentKey:key,currency:"RUB",amount:"150",quantity:"2",fee:"0",accrued:"0",occurredAt:{seconds:at}}]};
  if(method?.name==="ListInvestmentValuationProfiles")data={fxProfiles:[{key:"market",mode:"market",version:1n}]};
  if(method?.name==="GetInvestmentExchangeRates"){
   const now=BigInt(Math.floor(Date.now()/1000)),sources=input.fromCurrencies.length?input.fromCurrencies:["RUB"];
   data={currency:input.currency,fxPolicyKey:"market",policyVersion:1n,mode:"market",source:"alphavantage",timeConvention:"Retrospective next-day close",readAt:{seconds:now},rates:["",...input.dates].flatMap((date:string)=>sources.map((fromCurrency:string)=>({date,fromCurrency,rate:{numerator:"1",denominator:"1"}}))),currentFxLegs:sources.filter((c:string)=>c!==input.currency).map((c:string)=>({nativeCurrency:c,id:"synthetic-"+c,revision:1n,fromCurrency:c,toCurrency:input.currency,policy:"market@1",observedRate:{numerator:"1",denominator:"1"},asOf:{seconds:now-86400n},availableAt:{seconds:now-3600n},validUntil:{seconds:now+86400n}}))};
   if(input.currency==="RUB") for(const row of data.rates) if(row.date===""&&row.fromCurrency!=="RUB") {row.previousRate={numerator:"4",denominator:"5"};row.previousFxLegs=[{nativeCurrency:row.fromCurrency,id:"previous-"+row.fromCurrency,revision:2n,fromCurrency:row.fromCurrency,toCurrency:"RUB",policy:"market@1",observedRate:row.previousRate,asOf:{seconds:now-172800n},availableAt:{seconds:now-90000n},validUntil:{seconds:now-86400n}}];}
  }
  if(method?.name==="GetInvestmentQuotes")data={quotes:[{instrumentKey:key,currency:"RUB",state:"fresh",previousClose:{numerator:"100",denominator:"1"},previousCloseCurrency:"RUB",previousCloseAt:{seconds:at},cleanPrice:{numerator:"120",denominator:"1"},priceAt:{seconds:BigInt(Math.floor(Date.now()/1000)-60)},source:"tinvest/personal",instrument:{instrumentId:uid,ticker:"ABC",name:"Synthetic security",isin:"SYNTHETIC123",classCode:"SYNTHETIC",kind:"bond",currency:"RUB"},metadataAt:{seconds:at},rawPrice:{numerator:"12",denominator:"1"},rawUnit:"percent_of_nominal",nominal:{numerator:"1000",denominator:"1"},nominalCurrency:"RUB",accrued:{numerator:"0",denominator:"1"},accruedAt:{seconds:at}}]};
  if(method?.name==="ListBrokerSnapshots")data={snapshots:[{id:"snapshot",accountId:"broker",createdAt:{seconds:at}}]};
  if(method?.name==="GetBrokerSnapshot")data={originalJson:new TextEncoder().encode(JSON.stringify({fetched_at:new Date().toISOString(),operations:[{ticker:"ABC",instrumentUid:uid,name:"Synthetic security"}]}))};
  if(method?.name==="GetInvestmentGroups")data={classification,positions:[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:"OLD-CLOSED"}]};
  if(method?.name==="SaveInvestmentGroups"){writes++;expect(input.classification.version).toBe(1n);expect(input.requestKey).toBeTruthy();expect(input.classification.groups.map((g:any)=>g.name).sort()).toEqual(["Core","New"]);expect(input.classification.memberships).toHaveLength(1);expect(input.classification.memberships[0].ledgerId).toBe(one);classification=create(InvestmentGroupsSchema,{...input.classification,version:2n});data={classification};}
  if(await chartFallback(route,method,coldChart?4:12))return;
  if(method?.name==="GetTrackerHistory"&&input.ledgerIds.includes(one)&&input.ledgerIds.includes(two)&&input.ledgerIds.includes("66666666-6666-4666-8666-666666666666"))globalHistoryIncludesArchived=true;
  if(method?.name==="GetTrackerHistory"){historyReads++;
   if(coldChart&&input.performanceFromDate&&input.fromDate>"2019-01-01")await heldHistory;
   const selected=new Set(input.groupIds||[]),group=(id:string)=>classification.memberships.find(m=>m.ledgerId===id&&m.instrumentKey===key)?.groupId||"";
   const amount=(!selected.size||selected.has(group(one))?120:0)+(!selected.size||selected.has(group(two))?240:0);const points:any[]=[];let date=input.fromDate;
   while(date<=input.toDate&&points.length<(input.pageDays||31)){points.push({date,state:missingPrices?"incomplete":"ready",knownValue:{numerator:String(100+((Date.parse(date)-Date.parse(input.performanceFromDate||input.fromDate))/86400000+1)*10),denominator:"1"},positions:amount?(missingPrices?2:1):0,pricedPositions:amount?1:0,coverageKey:"a".repeat(64)});const d=new Date(date+"T00:00:00Z");d.setUTCDate(d.getUTCDate()+1);date=d.toISOString().slice(0,10);}
   if(input.detailDate){for(const p of points)p.details=[{instrumentKey:key,quantity:{numerator:"1",denominator:"1"},unitPrice:{numerator:"100",denominator:"1"},priceSource:"moex/personal",priceAt:{seconds:BigInt(Math.floor(Date.parse(p.date+"T00:00:00Z")/1000))}}];}
   data={detailsIncluded:!!input.detailDate,analyticsInstrumentKeys:partialHistory?(input.instrumentKeys?.length?input.instrumentKeys:[key,"MISSING"]):[],analyticsExcludedKeys:missingPrices?["MISSING"]:[],points,nextDate:date<=input.toDate?date:"",currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",basisKey:"b".repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow",classificationVersion:selected.size?classification.version:0n,performanceSnapshot:selected.size?"":"synthetic-page"};
  }
  if(method?.name==="GetTrackerPerformance"){
   const f=(n:number)=>({numerator:String(n),denominator:"1"}),first=new Date(Date.parse(input.fromDate)-86400000).toISOString().slice(0,10),points:any[]=[];let day=first,i=0;
   while(day<=input.toDate){points.push({date:day,recordedCapital:f(100+i*5),value:missingPrices?undefined:f(100+i*10),reason:missingPrices?"valuation_missing":"",flow:f(i?5:0),contributions:f(i*5),invested:missingPrices?undefined:f(100+i*5),profit:missingPrices?undefined:f(i*5),dailyProfit:missingPrices?undefined:i?f(5):undefined});day=new Date(Date.parse(day)+86400000).toISOString().slice(0,10);i++;}
   const periods=(width:number)=>{const result:any[]=[];for(const d of points.slice(1)){const key=d.date.slice(0,width),last=result.at(-1);if(last?.key===key){last.toDate=d.date;last.profit=f(Number(last.profit.numerator)+5);}else result.push({key,fromDate:d.date,toDate:d.date,profit:f(5)});}return result;};
   data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",historyScope:"recorded_securities_strategy",reportingTimezone:"Europe/Moscow",twrConvention:"securities_daily_end_of_day_flows",profit:f((points.length-1)*5),netContributions:f((points.length-1)*5),twr:{numerator:"1",denominator:"4"},xirr:0.1,xirrAnnualized:true,xirrDays:points.length-1,analyticsConvention:"retrospective_cashless_strategy_v1",analyticsPoints:points,monthlyProfit:periods(7).map(d=>missingPrices?{...d,profit:undefined,reason:"valuation_missing"}:d),yearlyProfit:periods(4).map(d=>missingPrices?{...d,profit:undefined,reason:"valuation_missing"}:d),twrPoints:points.map((p,j)=>({date:p.date,state:j?"calculated":"baseline",segment:1,index:f(100+j*5)}))};
  }

  if(method?.name==="GetTrackerPerformance")data.trackerBenchmarks=[{key:"imoex",title:"IMOEX",provider:"moex",nativeCurrency:"RUB",kind:"price_index",sourceUrl:"https://www.moex.com/ru/index/IMOEX"},{key:"spy",title:"SPY",provider:"twelvedata",nativeCurrency:"USD",kind:"etf_price_proxy",sourceUrl:"https://www.ssga.com/us/en/institutional/etfs/state-street-spdr-sp-500-etf-trust-spy"}].map(b=>({...b,baseDate:data.analyticsPoints[0].date,points:data.analyticsPoints.map((p:any,i:number)=>({date:p.date,index:{numerator:String(100+i*2),denominator:"1"},virtualValue:{numerator:String(100+i*7),denominator:"1"},virtualProfit:{numerator:String(i*2),denominator:"1"}}))}));
  if(method?.name==="GetTrackerContributions"){
   const f=(n:number)=>({numerator:String(n),denominator:"1"});
   const days=(Date.parse(input.toDate)-Date.parse(input.fromDate))/86400000+1;const rows=[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key,groupId:a,groupName:"Alpha",opening:f(60),closing:f(60+days*6),flow:f(days*3),profit:f(days*3),openingQuantity:f(1),closingQuantity:f(1),inScope:true},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key,groupId:b,groupName:"Beta",opening:f(40),closing:f(40+days*4),flow:f(days*2),profit:f(days*2),openingQuantity:f(2),closingQuantity:f(2),inScope:true}].filter(r=>input.ledgerIds.includes(r.ledgerId));
   const total=rows.reduce((n,r)=>n+Number(r.profit.numerator),0);
   data={convention:"recorded_securities_pnl_attribution_v1",currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",fromDate:input.fromDate,toDate:input.toDate,basisKey:"b".repeat(64),classificationVersion:1n,profit:f(total),knownProfit:f(total),rows};
  }
  if(missingPrices&&method?.name==="GetTrackerPerformance")Object.assign(data,{profit:undefined,reason:"valuation_missing",twr:undefined,twrReason:"valuation_missing",xirr:undefined,xirrReason:"valuation_missing",twrPoints:data.twrPoints.map((d:any)=>({...d,state:"gap",index:undefined,reason:"valuation_missing"}))});
  if(method&&/^(Create|Update|Save|Delete|Commit|Post)/.test(method.name))writes++;
  if(["GetTrackerHistory","GetTrackerPerformance","GetTrackerContributions"].includes(method?.name||""))Object.assign(data,{groupIds:input.groupIds||[],instrumentKeys:input.instrumentKeys||[],monetaryOnly:!!input.monetaryOnly});
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments?to=2025-01-03");
 const day=page.getByRole("region",{name:ru?"Дневной результат":"Daily result",exact:true});
 await expect(day).toContainText(ru?"За сегодня":"Today");await expect(day.locator("p.tabular-nums").first()).toContainText("60");await expect(day.locator("p.tabular-nums").last()).toContainText("20");
 const fx=page.getByRole("region",{name:ru?"Валютные курсы портфеля":"Portfolio exchange rates",exact:true});await expect(fx).toContainText("USDRUB");await expect(fx).toContainText("EURRUB");await expect(fx).toContainText("25");
 await expect(page.getByRole("img",{name:ru?"Распределение активов":"Asset allocation"})).toBeVisible();
 await expect(page.getByRole("heading",{name:ru?"Стоимость портфеля":"Portfolio value",exact:true})).toHaveCount(0);
 await page.screenshot({path:test.info().outputPath(`portfolio-overview-${locale}.png`),fullPage:true});
 await page.getByRole("button",{name:/Alpha/}).click();await expect(page).toHaveURL(new RegExp("group="+a));await expect(page.getByRole("button",{name:/ABC/})).toBeVisible();
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`portfolio-category-mobile-${locale}.png`),fullPage:true});await page.setViewportSize({width:1280,height:720});
 archivedCost=true;await page.goto("/investments?to=2025-01-03");
 const archivedCostCard=page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Вложено":"Invested",exact:true})});
 await expect(archivedCostCard.locator("p.tabular-nums")).toContainText("275");await expect(archivedCostCard).not.toContainText(ru?"Неполные данные":"Incomplete data");await expect(page.getByRole("button",{name:"Closed source",exact:true})).toHaveCount(0);archivedCost=false;
 missingCost=true;await page.goto("/investments?to=2025-01-03");
 const invested=page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Вложено":"Invested",exact:true})});
 await expect(invested.locator("p.tabular-nums")).toContainText("300");await expect(invested).toContainText(ru?"Неполные данные":"Incomplete data");missingCost=false;
 partialHistory=true;await page.goto("/investments?to=2025-01-03");await expect(day.locator("p.tabular-nums").first()).toContainText("60");partialHistory=false;
 await page.goto("/investments/analytics?from=2024-12-30&to=2025-01-03");
 await expect(page.getByRole("heading",{name:ru?"Рост":"Growth",exact:true})).toBeVisible();
 const history=page.getByLabel(ru?"Рост портфеля":"Portfolio growth",{exact:true});
 await expect(history.getByRole("img",{name:ru?"Стоимость и вложения":"Value and invested",exact:true})).toBeVisible();
 await expect(history.getByRole("img",{name:ru?"Накопленная прибыль":"Cumulative profit",exact:true})).toBeVisible();
 await expect(history.getByRole("img",{name:ru?"Сравнение роста, %":"Growth comparison, %",exact:true})).toBeVisible();await expect(history.locator("circle")).toHaveCount(0);await expect(history.getByRole("checkbox",{name:"IMOEX",exact:true})).toHaveCount(3);await expect(history.getByRole("img",{name:ru?"Прибыль по бумагам":"Profit by security",exact:true})).toBeVisible();
 await expect(history.getByRole("img",{name:ru?"Прибыль по месяцам":"Monthly profit",exact:true}).locator("rect[rx]")).toHaveCount(2);
 await history.getByRole("button",{name:ru?"По годам":"Yearly",exact:true}).click();await expect(history.getByRole("img",{name:ru?"Прибыль по годам":"Yearly profit",exact:true})).toBeVisible();
 await history.getByLabel(ru?"Действия с графиком":"Chart actions").click();const download=page.waitForEvent("download");await history.getByRole("button",{name:"CSV",exact:true}).first().click();const file=await download;expect(file.suggestedFilename()).toBe("portfolio-analytics-2024-12-30-2025-01-03.csv");
 const fs=await import("node:fs/promises"),text=await fs.readFile((await file.path())!,"utf8");expect(text).toContain("daily_profit_num");expect(text).toContain("retrospective_cashless_strategy_v1");
 await history.getByLabel(ru?"Действия с графиком":"Chart actions").click();await page.screenshot({path:test.info().outputPath(`portfolio-growth-${locale}.png`),fullPage:true});
 historyReads=0;coldChart=true;heldHistory=new Promise<void>(resolve=>{releaseHistory=resolve;});await page.goto("/investments/analytics?from=2019-01-01&to=2025-12-31");
 const preview=history.getByRole("img",{name:ru?"Стоимость портфеля · загрузка":"Portfolio value · loading",exact:true});
 await expect(preview).toBeVisible({timeout:5000});
 await expect(history).toContainText(ru?"Вложения и доходность рассчитываются":"Invested capital and returns are being calculated");
 await expect(history.getByRole("alert")).toHaveCount(0);
 releaseHistory();
 const longValue=history.getByRole("img",{name:ru?"Стоимость и вложения":"Value and invested",exact:true});await expect(longValue).toBeVisible();await expect(history.getByRole("img",{name:ru?"Прибыль по годам":"Yearly profit",exact:true})).toBeVisible();
 expect(historyReads).toBe(29);for(const line of await longValue.locator("polyline").all())expect((await line.getAttribute("points"))!.split(" ").length).toBeLessThanOrEqual(400);
 await expect(longValue.locator("circle")).toHaveCount(0);await expect(longValue.locator("polyline")).toHaveCount(4);
 await history.getByRole("checkbox",{name:"SPY · S&P 500",exact:true}).first().uncheck();await expect(longValue.locator("polyline")).toHaveCount(3);expect(historyReads).toBe(29);
 await page.screenshot({path:test.info().outputPath(`portfolio-seven-years-${locale}.png`),fullPage:true});
 coldChart=false;partialHistory=true;await page.goto("/investments/analytics?from=2024-12-30&to=2025-01-03");
 await expect(history.getByRole("img",{name:ru?"Стоимость и вложения":"Value and invested",exact:true})).toBeVisible();await expect(history.getByRole("img",{name:ru?"Стоимость и вложения":"Value and invested",exact:true}).locator('polyline[stroke-dasharray="5 4"]')).toHaveCount(1);await expect(history.locator("circle")).toHaveCount(0);
 await expect(history.getByRole("img",{name:ru?"Накопленная прибыль":"Cumulative profit",exact:true}).locator('polyline[stroke="#55d0ad"]')).toHaveCount(1);
 await expect(history).toContainText(ru?"Прибыль по доступной истории · бумаг: 1":"Profit with available history · instruments: 1");
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`portfolio-growth-mobile-${locale}.png`),fullPage:true});expect(writes).toBe(0);expect(globalHistoryIncludesArchived).toBe(true);expect(errors).toEqual([]);
 });}
