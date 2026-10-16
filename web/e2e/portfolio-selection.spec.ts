import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService,InvestmentGroupsSchema} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"]){test(`${locale} selected securities and groups show their own daily and period profit`,async({page,context})=>{
 test.skip(process.env.E2E_INVESTMENTS_READ_ONLY==="true","Write runtime required");
 const ru=locale==="ru",a="11111111-1111-4111-8111-111111111111",b="22222222-2222-4222-8222-222222222222",one="33333333-3333-4333-8333-333333333333",two="44444444-4444-4444-8444-444444444444",uid="55555555-5555-4555-8555-555555555555",key="tinvest:"+uid;
 const ledgers=[{id:one,name:"Broker one",version:1n,portfolioTracking:true},{id:two,name:"Broker two",version:1n,portfolioTracking:true}];
 let classification=create(InvestmentGroupsSchema,{version:1n,groups:[{id:a,name:"Alpha"},{id:b,name:"Beta"}],memberships:[{ledgerId:one,instrumentKey:key,groupId:a},{ledgerId:two,instrumentKey:key,groupId:b},{ledgerId:one,instrumentKey:"CLOSED",groupId:a}]});
 let writes=0;const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  const choose=(group:string,k:string)=>(!input.groupIds?.length||input.groupIds.includes(group))&&(!input.instrumentKeys?.length||input.instrumentKeys.includes(k));
  const selectedA=choose(a,key),selectedB=choose(b,key),selectedC=choose(a,"CLOSED"),unallocated=!input.groupIds?.length&&!input.instrumentKeys?.length;
  const vals=[0,Number(selectedA)*200+Number(selectedB)*100+Number(selectedC)*60,Number(selectedA)*100+Number(selectedB)*100,Number(selectedA)*120+Number(selectedB)*120];
  const flows=[0,Number(selectedA)*162+Number(selectedB)*100+Number(selectedC)*50,Number(selectedA)*-95+Number(selectedC)*-70+Number(unallocated)*3,0];
  let data:any={};const at=1767268800n,qty="1";
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers};
  if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger:ledgers.find(l=>l.id===input.ledgerId),positions:[{instrumentKey:key,currency:"RUB",quantity:qty,costBasis:"principal",lots:[{originId:input.ledgerId+"buy",quantity:qty,cleanCost:String(Number(qty)*100),acquiredAccrued:"0",acquiredAt:{seconds:at}}]}]}};
  if(method?.name==="ListInvestmentOperations")data={operations:[{id:input.ledgerId+"buy",ledgerId:input.ledgerId,kind:"buy",status:"posted",instrumentKey:key,currency:"RUB",amount:String(Number(qty)*100),quantity:qty,fee:"0",accrued:"0",occurredAt:{seconds:at}},{id:input.ledgerId+"div",ledgerId:input.ledgerId,kind:"dividend",status:"posted",instrumentKey:key,currency:"RUB",amount:String(Number(qty)*5),occurredAt:{seconds:at+86400n}}]};
  if(method?.name==="ListInvestmentValuationProfiles")data={fxProfiles:[{key:"market",mode:"market",version:1n}]};
  if(method?.name==="GetInvestmentExchangeRates")data={currency:input.currency,fxPolicyKey:"market",policyVersion:1n,mode:"market",readAt:{seconds:BigInt(Math.floor(Date.now()/1000))},rates:["",...input.dates].map((date:string)=>({date,fromCurrency:"RUB",rate:{numerator:"1",denominator:"1"}}))};
  if(method?.name==="GetInvestmentQuotes")data={quotes:[{instrumentKey:key,currency:"RUB",state:"fresh",cleanPrice:{numerator:"120",denominator:"1"},priceAt:{seconds:BigInt(Math.floor(Date.now()/1000)-60)},source:"tinvest/personal"}]};
  if(method?.name==="ListBrokerSnapshots")data={snapshots:[{id:"snapshot",accountId:"broker",createdAt:{seconds:at}}]};
  if(method?.name==="GetBrokerSnapshot")data={originalJson:new TextEncoder().encode(JSON.stringify({fetched_at:new Date().toISOString(),operations:[{ticker:"ABC",instrumentUid:uid,name:"Synthetic security"}]}))};
  if(method?.name==="ListInvestmentOperations"&&input.ledgerId===one)data.operations.push({id:"closed-buy",ledgerId:one,kind:"buy",status:"posted",instrumentKey:"CLOSED",currency:"RUB",amount:"50",quantity:"1",occurredAt:{seconds:1704067200n}},{id:"closed-sell",ledgerId:one,kind:"sell",status:"posted",instrumentKey:"CLOSED",currency:"RUB",amount:"70",quantity:"1",occurredAt:{seconds:1704153600n}});
  if(method?.name==="GetTrackerContributions"){
   const f=(n:number,d=1)=>({numerator:String(n),denominator:String(d)}),all=[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key,groupId:a,groupName:"Alpha",opening:f(0),closing:f(120),flow:f(67),profit:f(53),openingQuantity:f(0),closingQuantity:f(1),inScope:true},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key,groupId:b,groupName:"Beta",opening:f(0),closing:f(120),flow:f(100),profit:f(20),openingQuantity:f(0),closingQuantity:f(1),inScope:true},{ledgerId:one,ledgerName:"Broker one",instrumentKey:"CLOSED",groupId:a,groupName:"Alpha",opening:f(0),closing:f(0),flow:f(-20),profit:f(20),openingQuantity:f(0),closingQuantity:f(0),inScope:true},{ledgerId:one,ledgerName:"Broker one",instrumentKey:"",opening:f(0),closing:f(0),flow:f(3),profit:f(-3),openingQuantity:f(0),closingQuantity:f(0),inScope:!input.groupIds.length&&!input.instrumentKeys.length}];
   const rows=all.filter(r=>input.ledgerIds.includes(r.ledgerId)&&(!r.instrumentKey||choose(r.groupId||"",r.instrumentKey))),total=rows.filter(r=>r.inScope).reduce((sum,r)=>sum+Number(r.profit.numerator),0);
   data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",classificationVersion:1n,basisKey:"a".repeat(64),convention:"recorded_securities_pnl_attribution_v1",profit:f(total),knownProfit:f(total),rows:rows.map(r=>({...r,profitShare:r.inScope&&total?{numerator:String(Math.round(Number(r.profit.numerator)/total*1e12)),denominator:"1000000000000"}:undefined}))};
  }
  if(method?.name==="GetInvestmentGroups")data={classification,positions:[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:"OLD-CLOSED"}]};
  if(method?.name==="SaveInvestmentGroups"){writes++;expect(input.classification.version).toBe(1n);expect(input.requestKey).toBeTruthy();expect(input.classification.groups.map((g:any)=>g.name).sort()).toEqual(["Core","New"]);expect(input.classification.memberships).toHaveLength(1);expect(input.classification.memberships[0].ledgerId).toBe(one);classification=create(InvestmentGroupsSchema,{...input.classification,version:2n});data={classification};}
  if(method?.name==="GetTrackerHistory"){
   const points:any[]=[];let date=input.fromDate;
   while(date<=input.toDate&&points.length<31){const i=(Date.parse(date)-Date.parse("2023-12-31"))/86400000;points.push({date,state:"ready",knownValue:{numerator:String(vals[i]),denominator:"1"},positions:1,pricedPositions:1,coverageKey:"a".repeat(64)});date=new Date(Date.parse(date)+86400000).toISOString().slice(0,10);}
   data={points,nextDate:date<=input.toDate?date:"",currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",basisKey:"b".repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow",classificationVersion:input.groupIds.length?1n:0n,performanceSnapshot:"synthetic-page"};
  }
  if(method?.name==="GetTrackerPerformance"){
   const f=(n:number,d=1)=>({numerator:String(n),denominator:String(d)}),dates=["2023-12-31","2024-01-01","2024-01-02","2024-01-03"];let total=0;
   const points=dates.map((date,i)=>{total+=flows[i];return{date,value:f(vals[i]),flow:f(flows[i]),contributions:f(total),invested:f(total),profit:f(vals[i]-total)}}),profit=vals[3]-total;
   data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",historyScope:input.monetaryOnly?"recorded_group_securities_strategy":"recorded_securities_strategy",reportingTimezone:"Europe/Moscow",classificationVersion:input.groupIds.length?1n:0n,twrConvention:"securities_daily_end_of_day_flows",profit:f(profit),netContributions:f(total),twr:input.monetaryOnly?undefined:f(18,100),twrReason:input.monetaryOnly?"group_returns_unavailable":"",xirrReason:input.monetaryOnly?"group_returns_unavailable":"no_unique_annualized_root",xirrAnnualized:true,xirrDays:3,analyticsConvention:"retrospective_cashless_strategy_v1",analyticsPoints:points,monthlyProfit:[{key:"2024-01",fromDate:input.fromDate,toDate:input.toDate,profit:f(profit)}],yearlyProfit:[{key:"2024",fromDate:input.fromDate,toDate:input.toDate,profit:f(profit)}],twrPoints:input.monetaryOnly?[]:dates.map((date,i)=>({date,state:i===0?"unfunded":i===1?"baseline":"calculated",segment:i?1:0,index:i?f(i===3?118:100):undefined}))};
  }
  if(method&&/^(Create|Update|Save|Delete|Commit|Post)/.test(method.name))writes++;

  if(["GetTrackerHistory","GetTrackerPerformance","GetTrackerContributions"].includes(method?.name||""))Object.assign(data,{groupIds:input.groupIds||[],instrumentKeys:input.instrumentKeys||[],monetaryOnly:!!input.monetaryOnly});
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments/analytics?from=2024-01-01&to=2024-01-03");
 const history=page.getByLabel(ru?"Рост портфеля":"Portfolio growth",{exact:true}),groups=page.getByRole("combobox",{name:ru?"Категория":"Category",exact:true});
 await groups.selectOption(a);
 const profit=history.getByRole("img",{name:ru?"Накопленная прибыль":"Cumulative profit",exact:true});await expect(profit).toBeVisible();await expect(profit.locator("title").first()).toContainText("73");
 await expect(history.getByRole("button",{name:ru?"Доходность":"Returns",exact:true})).toHaveCount(0);
 await history.getByRole("button",{name:ru?"По годам":"Yearly",exact:true}).click();await expect(history.getByRole("img",{name:ru?"Прибыль по годам":"Yearly profit",exact:true}).locator("title").first()).toContainText("73");
 await page.getByRole("combobox",{name:ru?"Фильтр бумаги":"Security filter",exact:true}).selectOption(key);await expect(profit.locator("title").first()).toContainText("53");
 await history.getByLabel(ru?"Действия с графиком":"Chart actions").click();const download=page.waitForEvent("download");await history.getByRole("button",{name:"CSV",exact:true}).first().click();const file=await download,fs=await import("node:fs/promises"),csv=await fs.readFile((await file.path())!,"utf8");expect(csv).toContain("instrument_keys");expect(csv).toContain(key);expect(csv).toContain("group_returns_unavailable");
 await groups.selectOption("__all");await expect(profit).toBeVisible();await expect(profit.locator("title").first()).toContainText("73");
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0);});await page.screenshot({path:test.info().outputPath(`selected-analytics-${locale}.png`),fullPage:true});
 await page.goto("/investments/analytics?from=2024-01-01&to=2024-01-03&instrument=not-owned");await expect(page.locator("p[role=alert]")).toContainText(ru?"Выбранная бумага недоступна":"The selected security is unavailable");await expect(page.getByLabel(ru?"Рост портфеля":"Portfolio growth",{exact:true})).toHaveCount(0);
 expect(writes).toBe(0);expect(errors).toEqual([]);
 });}
