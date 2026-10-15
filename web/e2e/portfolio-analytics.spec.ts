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
 let writes=0,partialHistory=false;const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  let data:any={};const at=1767268800n,qty=input.ledgerId===one?"1":"2";
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers};
  if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger:ledgers.find(l=>l.id===input.ledgerId),positions:[{instrumentKey:key,currency:"RUB",quantity:qty,costBasis:"principal",lots:[{originId:input.ledgerId+"buy",quantity:qty,cleanCost:String(Number(qty)*100),acquiredAccrued:"0",acquiredAt:{seconds:at}}]}]}};
  if(method?.name==="ListInvestmentOperations")data={operations:[{id:input.ledgerId+"buy",ledgerId:input.ledgerId,kind:"buy",status:"posted",instrumentKey:key,currency:"RUB",amount:String(Number(qty)*100),quantity:qty,fee:"0",accrued:"0",occurredAt:{seconds:at}},{id:input.ledgerId+"div",ledgerId:input.ledgerId,kind:"dividend",status:"posted",instrumentKey:key,currency:"RUB",amount:String(Number(qty)*5),occurredAt:{seconds:at+86400n}}]};
  if(method?.name==="ListInvestmentValuationProfiles")data={fxProfiles:[{key:"market",mode:"market",version:1n}]};
  if(method?.name==="GetInvestmentExchangeRates"){
   const now=BigInt(Math.floor(Date.now()/1000)),sources=input.fromCurrencies.length?input.fromCurrencies:["RUB"];
   data={currency:input.currency,fxPolicyKey:"market",policyVersion:1n,mode:"market",source:"alphavantage",timeConvention:"Retrospective next-day close",readAt:{seconds:now},rates:["",...input.dates].flatMap((date:string)=>sources.map((fromCurrency:string)=>({date,fromCurrency,rate:{numerator:"1",denominator:"1"}}))),currentFxLegs:sources.filter((c:string)=>c!==input.currency).map((c:string)=>({nativeCurrency:c,id:"synthetic-"+c,revision:1n,fromCurrency:c,toCurrency:input.currency,policy:"market@1",observedRate:{numerator:"1",denominator:"1"},asOf:{seconds:now-86400n},availableAt:{seconds:now-3600n},validUntil:{seconds:now+86400n}}))};
   if(input.currency==="RUB") for(const row of data.rates) if(row.date===""&&row.fromCurrency!=="RUB") {row.previousRate={numerator:"4",denominator:"5"};row.previousFxLegs=[{nativeCurrency:row.fromCurrency,id:"previous-"+row.fromCurrency,revision:2n,fromCurrency:row.fromCurrency,toCurrency:"RUB",policy:"market@1",observedRate:row.previousRate,asOf:{seconds:now-172800n},availableAt:{seconds:now-90000n},validUntil:{seconds:now-86400n}}];}
  }
  if(method?.name==="GetInvestmentQuotes")data={quotes:[{instrumentKey:key,currency:"RUB",state:"fresh",cleanPrice:{numerator:"120",denominator:"1"},priceAt:{seconds:BigInt(Math.floor(Date.now()/1000)-60)},source:"tinvest/personal"}]};
  if(method?.name==="ListBrokerSnapshots")data={snapshots:[{id:"snapshot",accountId:"broker",createdAt:{seconds:at}}]};
  if(method?.name==="GetBrokerSnapshot")data={originalJson:new TextEncoder().encode(JSON.stringify({fetched_at:new Date().toISOString(),operations:[{ticker:"ABC",instrumentUid:uid,name:"Synthetic security"}]}))};
  if(method?.name==="GetInvestmentGroups")data={classification,positions:[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:"OLD-CLOSED"}]};
  if(method?.name==="SaveInvestmentGroups"){writes++;expect(input.classification.version).toBe(1n);expect(input.requestKey).toBeTruthy();expect(input.classification.groups.map((g:any)=>g.name).sort()).toEqual(["Core","New"]);expect(input.classification.memberships).toHaveLength(1);expect(input.classification.memberships[0].ledgerId).toBe(one);classification=create(InvestmentGroupsSchema,{...input.classification,version:2n});data={classification};}
  if(method?.name==="GetTrackerHistory"){
   const selected=new Set(input.groupIds||[]),group=(id:string)=>classification.memberships.find(m=>m.ledgerId===id&&m.instrumentKey===key)?.groupId||"";
   const amount=(!selected.size||selected.has(group(one))?120:0)+(!selected.size||selected.has(group(two))?240:0);const points:any[]=[];let date=input.fromDate;
   while(date<=input.toDate&&points.length<31){points.push({date,state:partialHistory?"incomplete":"ready",knownValue:{numerator:String(100+((Date.parse(date)-Date.parse(input.performanceFromDate||input.fromDate))/86400000+1)*10),denominator:"1"},positions:amount?(partialHistory?2:1):0,pricedPositions:amount?1:0,coverageKey:"a".repeat(64)});const d=new Date(date+"T00:00:00Z");d.setUTCDate(d.getUTCDate()+1);date=d.toISOString().slice(0,10);}
   data={points,nextDate:date<=input.toDate?date:"",currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",basisKey:"b".repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow",classificationVersion:selected.size?classification.version:0n,performanceSnapshot:selected.size?"":"synthetic-page"};
  }
  if(method?.name==="GetTrackerPerformance"){
   const f=(n:number)=>({numerator:String(n),denominator:"1"}),first=new Date(Date.parse(input.fromDate)-86400000).toISOString().slice(0,10),points:any[]=[];let day=first,i=0;
   while(day<=input.toDate){points.push({date:day,value:partialHistory?undefined:f(100+i*10),reason:partialHistory?"valuation_missing":"",flow:f(i?5:0),contributions:f(i*5),invested:partialHistory?undefined:f(100+i*5),profit:partialHistory?undefined:f(i*5),dailyProfit:partialHistory?undefined:i?f(5):undefined});day=new Date(Date.parse(day)+86400000).toISOString().slice(0,10);i++;}
   const periods=(width:number)=>{const result:any[]=[];for(const d of points.slice(1)){const key=d.date.slice(0,width),last=result.at(-1);if(last?.key===key){last.toDate=d.date;last.profit=f(Number(last.profit.numerator)+5);}else result.push({key,fromDate:d.date,toDate:d.date,profit:f(5)});}return result;};
   data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",historyScope:"recorded_securities_strategy",reportingTimezone:"Europe/Moscow",twrConvention:"securities_daily_end_of_day_flows",profit:f((points.length-1)*5),netContributions:f((points.length-1)*5),twr:{numerator:"1",denominator:"4"},xirr:0.1,xirrAnnualized:true,xirrDays:points.length-1,analyticsConvention:"retrospective_cashless_strategy_v1",analyticsPoints:points,monthlyProfit:periods(7).map(d=>partialHistory?{...d,profit:undefined,reason:"valuation_missing"}:d),yearlyProfit:periods(4).map(d=>partialHistory?{...d,profit:undefined,reason:"valuation_missing"}:d),twrPoints:points.map((p,j)=>({date:p.date,state:j?"calculated":"baseline",segment:1,index:f(100+j*5)}))};
  }

  if(partialHistory&&method?.name==="GetTrackerPerformance")Object.assign(data,{profit:undefined,reason:"valuation_missing",twr:undefined,twrReason:"valuation_missing",xirr:undefined,xirrReason:"valuation_missing",twrPoints:data.twrPoints.map((d:any)=>({...d,state:"gap",index:undefined,reason:"valuation_missing"}))});
  if(method&&/^(Create|Update|Save|Delete|Commit|Post)/.test(method.name))writes++;
  if(["GetTrackerHistory","GetTrackerPerformance","GetTrackerContributions"].includes(method?.name||""))Object.assign(data,{groupIds:input.groupIds||[],instrumentKeys:input.instrumentKeys||[],monetaryOnly:!!input.monetaryOnly});
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments/analytics?from=2024-12-30&to=2025-01-03");
 await expect(page.getByRole("heading",{name:ru?"Аналитика портфеля":"Portfolio analytics",exact:true})).toBeVisible();
 const fx=page.getByRole("region",{name:ru?"Валютные курсы портфеля":"Portfolio exchange rates",exact:true});
 await expect(fx).toContainText("USD/RUB: 1");await expect(fx).toContainText("EUR/RUB: 1");await expect(fx).toContainText("Alpha Vantage");await expect(fx).toContainText(ru?"Для оценки с":"Eligible from");await expect(fx).toContainText(ru?"ретроспективная оценка":"retrospective valuation");
 await expect(fx).toContainText(ru?"К предыдущему наблюдению":"Vs previous observation");await expect(fx).toContainText(ru?/\+0,2 RUB \(\+25\s*%/:/\+0\.2 RUB \(\+25\s*%/);await expect(fx).toContainText(ru?"База сравнения":"Comparison basis");
 const history=page.getByRole("region",{name:ru?"История портфеля":"Portfolio history",exact:true});
 await expect(history.getByRole("img",{name:ru?"Стоимость и чистые вложения":"Value and net investment",exact:true})).toBeVisible();
 await history.getByRole("button",{name:ru?"Прибыль":"Profit",exact:true}).click();await expect(history.getByRole("img",{name:ru?"Накопленная прибыль":"Cumulative profit",exact:true})).toBeVisible();
 await expect(history.locator("svg title").last()).toContainText("25");
 await history.getByRole("button",{name:ru?"По месяцам":"Monthly",exact:true}).click();await expect(history.getByRole("table")).toContainText("2024-12");await expect(history.getByRole("table")).toContainText("2025-01");await expect(history.locator("rect")).toHaveCount(2);
 await history.getByRole("button",{name:ru?"По годам":"Yearly",exact:true}).click();await expect(history.getByRole("table")).toContainText("2024-12-30 — 2024-12-31");await expect(history.getByRole("table")).toContainText("2025-01-01 — 2025-01-03");
 const download=page.waitForEvent("download");await history.getByRole("button",{name:"CSV",exact:true}).click();const file=await download;expect(file.suggestedFilename()).toBe("portfolio-analytics-2024-12-30-2025-01-03.csv");
 const fs=await import("node:fs/promises"),text=await fs.readFile((await file.path())!,"utf8");expect(text).toContain("daily_profit_num");expect(text).toContain("retrospective_cashless_strategy_v1");expect(text).toContain('"month"');expect(text).toContain('"year"');
 await page.getByRole("combobox",{name:ru?"Валюта сумм":"Amount currency",exact:true}).selectOption("USD");await expect(history.getByRole("table")).toContainText("USD");
 await history.getByRole("button",{name:ru?"7 дней":"7 days",exact:true}).click();const start=await history.getByLabel(ru?"История с":"History from",{exact:true}).inputValue(),end=await history.getByLabel(ru?"История по":"History through",{exact:true}).inputValue();expect((Date.parse(end)-Date.parse(start))/86400000).toBe(6);
 partialHistory=true;
 await page.goto("/investments/analytics?from=2024-12-30&to=2025-01-03");
 await expect(history.getByRole("img",{name:ru?"Стоимость и чистые вложения":"Value and net investment",exact:true})).toBeVisible();
 await expect(history.locator('polyline[stroke-dasharray="5 4"]')).toHaveCount(1);
 await expect(history.getByText(ru?"Пунктир — известная часть стоимости":"Dashed line — known portion of value",{exact:true})).toBeVisible();
 await expect(history.locator("svg circle")).toHaveCount(5);
 const partialDownload=page.waitForEvent("download");await history.getByRole("button",{name:"CSV",exact:true}).click();const partialFile=await partialDownload;
 const partialText=await fs.readFile((await partialFile.path())!,"utf8");expect(partialText).toContain("known_value_num");expect(partialText).toContain('"incomplete"');
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:test.info().outputPath(`portfolio-known-value-${locale}.png`),fullPage:true});
 await history.getByRole("button",{name:ru?"Прибыль":"Profit",exact:true}).click();await expect(history.getByText(ru?"Нет полной оценки для выбранных показателей.":"Complete valuations are unavailable for these metrics.",{exact:true})).toBeVisible();await expect(history.getByRole("img")).toHaveCount(0);
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`portfolio-analytics-${locale}.png`),fullPage:true});expect(writes).toBe(0);expect(errors).toEqual([]);
 });}
