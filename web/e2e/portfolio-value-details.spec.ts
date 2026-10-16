import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService,InvestmentGroupsSchema} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"]){test(`${locale} valuation breakdown shows native amounts and exact dated FX`,async({page,context})=>{
 test.skip(process.env.E2E_INVESTMENTS_READ_ONLY==="true","Write runtime required");
 const ru=locale==="ru",a="11111111-1111-4111-8111-111111111111",b="22222222-2222-4222-8222-222222222222",one="33333333-3333-4333-8333-333333333333",two="44444444-4444-4444-8444-444444444444",uid="55555555-5555-4555-8555-555555555555",key="tinvest:"+uid;
 const ledgers=[{id:one,name:"Broker one",version:1n,portfolioTracking:true},{id:two,name:"Broker two",version:1n,portfolioTracking:true}];
 let classification=create(InvestmentGroupsSchema,{version:1n,groups:[{id:a,name:"Alpha"},{id:b,name:"Beta"}],memberships:[{ledgerId:one,instrumentKey:key,groupId:a},{ledgerId:two,instrumentKey:key,groupId:b}]});
 let changed=false,detailReads=0,writes=0;const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  let data:any={};const at=1767268800n,qty=input.ledgerId===one?"1":"2";
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers};
  if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger:ledgers.find(l=>l.id===input.ledgerId),positions:[{instrumentKey:key,currency:"RUB",quantity:qty,costBasis:"principal",lots:[{originId:input.ledgerId+"buy",quantity:qty,cleanCost:String(Number(qty)*100),acquiredAccrued:"0",acquiredAt:{seconds:at}}]}]}};
  if(method?.name==="ListInvestmentOperations")data={operations:[{id:input.ledgerId+"buy",ledgerId:input.ledgerId,kind:"buy",status:"posted",instrumentKey:key,currency:"RUB",amount:String(Number(qty)*100),quantity:qty,fee:"0",accrued:"0",occurredAt:{seconds:at}},{id:input.ledgerId+"div",ledgerId:input.ledgerId,kind:"dividend",status:"posted",instrumentKey:key,currency:"RUB",amount:String(Number(qty)*5),occurredAt:{seconds:at+86400n}}]};
  if(method?.name==="ListInvestmentValuationProfiles")data={fxProfiles:[{key:"market",mode:"market",version:1n}]};
  if(method?.name==="GetInvestmentExchangeRates")data={currency:input.currency,fxPolicyKey:"market",policyVersion:1n,mode:"market",readAt:{seconds:BigInt(Math.floor(Date.now()/1000))},rates:["",...input.dates].map((date:string)=>({date,fromCurrency:"RUB",rate:{numerator:"1",denominator:"1"}}))};
  if(method?.name==="GetInvestmentQuotes")data={quotes:[{instrumentKey:key,currency:"RUB",state:"fresh",cleanPrice:{numerator:"120",denominator:"1"},priceAt:{seconds:BigInt(Math.floor(Date.now()/1000)-60)},source:"tinvest/personal"}]};
  if(method?.name==="ListBrokerSnapshots")data={snapshots:[{id:"snapshot",accountId:"broker",createdAt:{seconds:at}}]};
  if(method?.name==="GetBrokerSnapshot")data={originalJson:new TextEncoder().encode(JSON.stringify({fetched_at:new Date().toISOString(),operations:[{ticker:"ABC",instrumentUid:uid,name:"Synthetic security"}]}))};
  if(method?.name==="GetTrackerContributions"){const f=(n:number)=>({numerator:String(n),denominator:"1"});data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",classificationVersion:1n,basisKey:"b".repeat(64),convention:"recorded_securities_pnl_attribution_v1",profit:f(25),knownProfit:f(25),rows:[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key,groupId:a,groupName:"Alpha",opening:f(100),closing:f(150),flow:f(25),profit:f(25),openingQuantity:f(1),closingQuantity:f(1),inScope:true}]};}
  if(method?.name==="GetInvestmentGroups")data={classification,positions:[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:"OLD-CLOSED"}]};
  if(method?.name==="SaveInvestmentGroups"){writes++;expect(input.classification.version).toBe(1n);expect(input.requestKey).toBeTruthy();expect(input.classification.groups.map((g:any)=>g.name).sort()).toEqual(["Core","New"]);expect(input.classification.memberships).toHaveLength(1);expect(input.classification.memberships[0].ledgerId).toBe(one);classification=create(InvestmentGroupsSchema,{...input.classification,version:2n});data={classification};}
  if(method?.name==="GetTrackerHistory"){
   const selected=new Set(input.groupIds||[]),group=(id:string)=>classification.memberships.find(m=>m.ledgerId===id&&m.instrumentKey===key)?.groupId||"";
   const amount=(!selected.size||selected.has(group(one))?120:0)+(!selected.size||selected.has(group(two))?240:0);const points:any[]=[];let date=input.fromDate;
   while(date<=input.toDate&&points.length<31){points.push({date,state:"ready",knownValue:{numerator:String(100+((Date.parse(date)-Date.parse(input.performanceFromDate||input.fromDate))/86400000+1)*10),denominator:"1"},positions:amount?1:0,pricedPositions:amount?1:0,coverageKey:"a".repeat(64)});const d=new Date(date+"T00:00:00Z");d.setUTCDate(d.getUTCDate()+1);date=d.toISOString().slice(0,10);}
   data={points,nextDate:date<=input.toDate?date:"",currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",basisKey:"b".repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow",classificationVersion:selected.size?classification.version:0n,performanceSnapshot:selected.size?"":"synthetic-page"};
  }
  if(method?.name==="GetTrackerPerformance"){
   const f=(n:number)=>({numerator:String(n),denominator:"1"}),first=new Date(Date.parse(input.fromDate)-86400000).toISOString().slice(0,10),points:any[]=[];let day=first,i=0;
   while(day<=input.toDate){points.push({date:day,value:f(100+i*10),flow:f(i?5:0),contributions:f(i*5),invested:f(100+i*5),profit:f(i*5),dailyProfit:i?f(5):undefined});day=new Date(Date.parse(day)+86400000).toISOString().slice(0,10);i++;}
   const periods=(width:number)=>{const result:any[]=[];for(const d of points.slice(1)){const key=d.date.slice(0,width),last=result.at(-1);if(last?.key===key){last.toDate=d.date;last.profit=f(Number(last.profit.numerator)+5);}else result.push({key,fromDate:d.date,toDate:d.date,profit:f(5)});}return result;};
   data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",historyScope:"recorded_securities_strategy",reportingTimezone:"Europe/Moscow",twrConvention:"securities_daily_end_of_day_flows",profit:f((points.length-1)*5),netContributions:f((points.length-1)*5),twr:{numerator:"1",denominator:"4"},xirr:0.1,xirrAnnualized:true,xirrDays:points.length-1,analyticsConvention:"retrospective_cashless_strategy_v1",analyticsPoints:points,monthlyProfit:periods(7),yearlyProfit:periods(4),twrPoints:points.map((p,j)=>({date:p.date,state:j?"calculated":"baseline",segment:1,index:f(100+j*5)}))};
  }

  if(method?.name==="GetTrackerHistory"&&input.detailDate){
   detailReads++;expect(input.fromDate).toBe("2024-12-30");expect(input.toDate).toBe("2025-01-03");expect(input.performanceFromDate).toBe("");
   const n=100+((Date.parse(input.detailDate)-Date.parse(input.fromDate))/86400000+1)*10,f=(n:number,d=1)=>({numerator:String(n),denominator:String(d)}),seconds=BigInt(Date.parse(input.detailDate)/1000);
   data={...data,basisKey:(changed?"c":"b").repeat(64),detailsIncluded:true,nextDate:"",points:[{date:input.detailDate,state:"ready",positions:1,pricedPositions:1,knownValue:f(n),details:[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key,nativeCurrency:"USD",quantity:f(1),unitPrice:f(n,90),nativeValue:f(n,90),value:f(n),fxRate:f(90),priceAt:{seconds},priceSource:"moex/personal",mappingVersion:1n}],fxLegs:[{nativeCurrency:"USD",id:"synthetic-fx",revision:2n,policy:"market@1",fromCurrency:"USD",toCurrency:"RUB",observedRate:f(90),asOf:{seconds},availableAt:{seconds},validUntil:{seconds:seconds+86400n}}]}]};
  }
  if(method&&/^(Create|Update|Save|Delete|Commit|Post)/.test(method.name))writes++;
  if(["GetTrackerHistory","GetTrackerPerformance","GetTrackerContributions"].includes(method?.name||""))Object.assign(data,{groupIds:input.groupIds||[],instrumentKeys:input.instrumentKeys||[],monetaryOnly:!!input.monetaryOnly});
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments/analytics?from=2024-12-30&to=2025-01-03");
 await expect(page.getByRole("heading",{name:ru?"Рост":"Growth",exact:true})).toBeVisible();
 const history=page.getByLabel(ru?"Рост портфеля":"Portfolio growth",{exact:true});
 await expect(history.getByRole("img",{name:ru?"Стоимость и вложения":"Value and invested",exact:true})).toBeVisible();
 const chart=history.getByRole("img",{name:ru?"Стоимость и вложения":"Value and invested",exact:true});await chart.focus();await chart.press("Home");await chart.press("ArrowRight");await chart.press("ArrowRight");await chart.press("Enter");
 const details=history.locator('[aria-label="'+(ru?"Расшифровка стоимости":"Valuation breakdown")+'"]');
 await expect(details).toContainText("1.333333333333 USD");await expect(details).toContainText("120 RUB");await expect(details).toContainText("moex/personal");
 await details.getByText(ru?"Источники валютного курса":"FX evidence",{exact:true}).click();await expect(details).toContainText("USD/RUB: 90");await expect(details).toContainText("synthetic-fx · rev 2");
 await details.getByRole("combobox",{name:ru?"Дата оценки":"Valuation date",exact:true}).selectOption("2025-01-03");await expect(details).toContainText("1.666666666667 USD");await expect(details).toContainText("150 RUB");
 await details.getByText(ru?"Источники валютного курса":"FX evidence",{exact:true}).click();
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0);});await page.screenshot({path:test.info().outputPath(`valuation-breakdown-${locale}.png`),fullPage:true});
 changed=true;await details.getByRole("combobox",{name:ru?"Дата оценки":"Valuation date",exact:true}).selectOption("2025-01-02");await expect(details.getByRole("alert")).toContainText(ru?"История изменилась":"History changed");await expect(details).not.toContainText("150 RUB");expect(detailReads).toBe(3);expect(writes).toBe(0);expect(errors).toEqual([]);
 });}
