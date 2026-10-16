import {chartFallback} from "./fixtures/chart-fallback";
import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService,InvestmentGroupsSchema} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"]){test(`${locale} contributions retain closed positions and isolate group flows`,async({page,context})=>{
 test.skip(process.env.E2E_INVESTMENTS_READ_ONLY==="true","Write runtime required");
 const ru=locale==="ru",a="11111111-1111-4111-8111-111111111111",b="22222222-2222-4222-8222-222222222222",one="33333333-3333-4333-8333-333333333333",two="44444444-4444-4444-8444-444444444444",uid="55555555-5555-4555-8555-555555555555",key="tinvest:"+uid;
 const ledgers=[{id:one,name:"Broker one",version:1n,portfolioTracking:true},{id:two,name:"Broker two",version:1n,portfolioTracking:true}];
 let classification=create(InvestmentGroupsSchema,{version:1n,groups:[{id:a,name:"Alpha"},{id:b,name:"Beta"}],memberships:[{ledgerId:one,instrumentKey:key,groupId:a},{ledgerId:two,instrumentKey:key,groupId:b}]});
 let writes=0;const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
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
  if(method?.name==="GetTrackerContributions"){
   const f=(n:number,d=1)=>({numerator:String(n),denominator:String(d)}),all=[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key,groupId:a,groupName:"Alpha",opening:f(0),closing:f(120),flow:f(67),profit:f(53),openingQuantity:f(0),closingQuantity:f(1),inScope:true},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key,groupId:b,groupName:"Beta",opening:f(0),closing:f(120),flow:f(100),profit:f(20),openingQuantity:f(0),closingQuantity:f(1),inScope:true},{ledgerId:one,ledgerName:"Broker one",instrumentKey:"CLOSED",groupId:a,groupName:"Alpha",opening:f(0),closing:f(0),flow:f(-20),profit:f(20),openingQuantity:f(0),closingQuantity:f(0),inScope:true},{ledgerId:one,ledgerName:"Broker one",instrumentKey:"",opening:f(0),closing:f(0),flow:f(3),profit:f(-3),openingQuantity:f(0),closingQuantity:f(0),inScope:!input.groupIds.length}];
   const rows=all.filter(r=>input.ledgerIds.includes(r.ledgerId)&&(!input.groupIds.length||!r.instrumentKey||input.groupIds.includes(r.groupId))),total=rows.filter(r=>r.inScope).reduce((sum,r)=>sum+Number(r.profit.numerator),0);
   data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",classificationVersion:1n,basisKey:"a".repeat(64),convention:"recorded_securities_pnl_attribution_v1",profit:f(total),knownProfit:f(total),rows:rows.map(r=>({...r,profitShare:r.inScope&&total?{numerator:String(Math.round(Number(r.profit.numerator)/total*1e12)),denominator:"1000000000000"}:undefined}))};
  }
  if(method?.name==="GetInvestmentGroups")data={classification,positions:[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:"OLD-CLOSED"}]};
  if(method?.name==="SaveInvestmentGroups"){writes++;expect(input.classification.version).toBe(1n);expect(input.requestKey).toBeTruthy();expect(input.classification.groups.map((g:any)=>g.name).sort()).toEqual(["Core","New"]);expect(input.classification.memberships).toHaveLength(1);expect(input.classification.memberships[0].ledgerId).toBe(one);classification=create(InvestmentGroupsSchema,{...input.classification,version:2n});data={classification};}
  if(await chartFallback(route,method))return;
  if(method?.name==="GetTrackerHistory"){
   const selected=new Set(input.groupIds||[]),group=(id:string)=>classification.memberships.find(m=>m.ledgerId===id&&m.instrumentKey===key)?.groupId||"";
   const amount=(!selected.size||selected.has(group(one))?120:0)+(!selected.size||selected.has(group(two))?240:0);const points:any[]=[];let date=input.fromDate;
   while(date<=input.toDate&&points.length<31){points.push({date,state:"ready",knownValue:{numerator:String(input.groupIds?.length?amount:[360,200,240][Math.min(2,(Date.parse(date)-Date.parse("2024-01-01"))/86400000)]),denominator:"1"},positions:amount?1:0,pricedPositions:amount?1:0,coverageKey:"a".repeat(64)});const d=new Date(date+"T00:00:00Z");d.setUTCDate(d.getUTCDate()+1);date=d.toISOString().slice(0,10);}
   data={points,nextDate:date<=input.toDate?date:"",currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",basisKey:"b".repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow",classificationVersion:selected.size?classification.version:0n,performanceSnapshot:selected.size?"":"synthetic-page"};
  }
  if(method?.name==="GetTrackerPerformance"){
   const f=(n:number)=>({numerator:String(n),denominator:"1"}),dates=["2023-12-31","2024-01-01","2024-01-02","2024-01-03"],values=[0,360,200,240],flows=[0,312,-162,0],invested=[0,312,150,150],profits=[0,48,50,90];
   data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",historyScope:"recorded_securities_strategy",reportingTimezone:"Europe/Moscow",twrConvention:"securities_daily_end_of_day_flows",profit:f(90),netContributions:f(150),twr:f(0),xirrReason:"no_unique_annualized_root",xirrAnnualized:true,xirrDays:3,analyticsConvention:"retrospective_cashless_strategy_v1",analyticsPoints:dates.map((date,i)=>({date,value:f(values[i]),flow:f(flows[i]),contributions:f(invested[i]),invested:f(invested[i]),profit:f(profits[i])})),monthlyProfit:[{key:"2024-01",fromDate:input.fromDate,toDate:input.toDate,profit:f(90)}],yearlyProfit:[{key:"2024",fromDate:input.fromDate,toDate:input.toDate,profit:f(90)}]};
  }
  if(method&&/^(Create|Update|Save|Delete|Commit|Post)/.test(method.name))writes++;

  if(["GetTrackerHistory","GetTrackerPerformance","GetTrackerContributions"].includes(method?.name||""))Object.assign(data,{groupIds:input.groupIds||[],instrumentKeys:input.instrumentKeys||[],monetaryOnly:!!input.monetaryOnly});
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments/analytics?from=2024-01-01&to=2024-01-03");
 const section=page.getByRole("region",{name:ru?"Прибыльность активов":"Asset profitability",exact:true});
 await section.getByText(ru?"Подробнее":"Details",{exact:true}).click();await expect(section.getByRole("table")).toBeVisible();await expect(section.getByRole("link",{name:"ABC",exact:true})).toBeVisible();
 await expect(section.getByRole("table")).toContainText("CLOSED");await expect(section.getByRole("row").filter({hasText:"ABC"})).toContainText("73");
 await expect(section.getByRole("img",{name:ru?"Прибыль по бумагам":"Profit by security",exact:true})).toBeVisible();
 await section.getByLabel(ru?"Действия с прибыльностью":"Profitability actions").click();await section.getByRole("combobox",{name:ru?"Вклад по":"Contributions by",exact:true}).selectOption("group");await expect(section.getByRole("table")).toContainText("Alpha");await expect(section.getByRole("table")).toContainText("Beta");
 await page.getByRole("combobox",{name:ru?"Категория":"Category",exact:true}).selectOption(a);
 await section.getByText(ru?"Подробнее":"Details",{exact:true}).click();await section.getByLabel(ru?"Действия с прибыльностью":"Profitability actions").click();await expect(section.getByRole("row").filter({hasText:"Alpha"})).toContainText("73");
 await expect(section.getByRole("table")).not.toContainText("Beta");await expect(section.getByRole("row").filter({hasText:ru?"Без бумаги":"Unallocated"})).toContainText(/-.*3/);
 const download=page.waitForEvent("download");await section.getByRole("button",{name:"CSV",exact:true}).click();const file=await download;expect(file.suggestedFilename()).toBe("portfolio-contributions-2024-01-01-2024-01-03.csv");const fs=await import("node:fs/promises"),csv=await fs.readFile((await file.path())!,"utf8");expect(csv).toContain("CLOSED");expect(csv).toContain('"false"');expect(csv).toContain('"-3"');
 await section.getByRole("combobox",{name:ru?"Вклад по":"Contributions by",exact:true}).selectOption("instrument");await section.getByRole("textbox",{name:ru?"Поиск вклада":"Search contributions",exact:true}).fill("CLOSED");await expect(section.locator("tbody tr")).toHaveCount(1);await expect(section.getByRole("table")).toContainText("20");
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0);});await page.screenshot({path:test.info().outputPath(`portfolio-contributions-${locale}.png`),fullPage:true});expect(writes).toBe(0);expect(errors).toEqual([]);
 });}
