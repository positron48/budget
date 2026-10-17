import {chartFallback} from "./fixtures/chart-fallback";
import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService,InvestmentGroupsSchema} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"]){test(`${locale} groups classify accounts, preserve history and save one atomic draft`,async({page,context})=>{
 test.skip(process.env.E2E_INVESTMENTS_READ_ONLY==="true","Write runtime required");
 const ru=locale==="ru",a="11111111-1111-4111-8111-111111111111",b="22222222-2222-4222-8222-222222222222",one="33333333-3333-4333-8333-333333333333",two="44444444-4444-4444-8444-444444444444",uid="55555555-5555-4555-8555-555555555555",key="tinvest:"+uid;
 const ledgers=[{id:one,name:"Broker one",version:1n,portfolioTracking:true},{id:two,name:"Broker two",version:1n,portfolioTracking:true}];
 let classification=create(InvestmentGroupsSchema,{version:1n,groups:[{id:a,name:"Alpha"},{id:b,name:"Beta"}],memberships:[{ledgerId:one,instrumentKey:key,groupId:a},{ledgerId:two,instrumentKey:key,groupId:b}]});
 let writes=0,summaryCalls=0;const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
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
  if(method?.name==="GetInvestmentGroups")data={classification,positions:[{ledgerId:one,ledgerName:"Broker one",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:key},{ledgerId:two,ledgerName:"Broker two",instrumentKey:"OLD-CLOSED"}]};
  if(method?.name==="SaveInvestmentGroups"){writes++;expect(input.classification.version).toBe(1n);expect(input.requestKey).toBeTruthy();expect(input.classification.groups.map((g:any)=>g.name).sort()).toEqual(["Core","New"]);expect(input.classification.memberships).toHaveLength(1);expect(input.classification.memberships[0].ledgerId).toBe(one);classification=create(InvestmentGroupsSchema,{...input.classification,version:2n});data={classification};}
  if(await chartFallback(route,method))return;
  if(method?.name==="GetTrackerHistory"){
   const selected=new Set(input.groupIds||[]),group=(id:string)=>classification.memberships.find(m=>m.ledgerId===id&&m.instrumentKey===key)?.groupId||"";
   const amount=(!selected.size||selected.has(group(one))?120:0)+(!selected.size||selected.has(group(two))?240:0);const points:any[]=[];let date=input.fromDate;
   while(date<=input.toDate&&points.length<31){points.push({date,state:"ready",knownValue:{numerator:String(amount),denominator:"1"},positions:amount?1:0,pricedPositions:amount?1:0,coverageKey:"a".repeat(64)});const d=new Date(date+"T00:00:00Z");d.setUTCDate(d.getUTCDate()+1);date=d.toISOString().slice(0,10);}
   data={points,nextDate:date<=input.toDate?date:"",currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",basisKey:"b".repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow",classificationVersion:selected.size?classification.version:0n,performanceSnapshot:selected.size?"":"synthetic-page"};
  }
  if(method?.name==="GetTrackerPerformance"){summaryCalls++;data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",historyScope:"recorded_securities_strategy",reportingTimezone:"Europe/Moscow",twrConvention:"securities_daily_end_of_day_flows",reason:"insufficient_history",twrReason:"insufficient_history",xirrReason:"insufficient_history",xirrAnnualized:true};}
  if(["GetTrackerHistory","GetTrackerPerformance","GetTrackerContributions"].includes(method?.name||""))Object.assign(data,{groupIds:input.groupIds||[],instrumentKeys:input.instrumentKeys||[],monetaryOnly:!!input.monetaryOnly});
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),tail=Buffer.alloc(5);tail[0]=0x80;tail.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,tail,trailer])});
 });
 await page.goto("/investments");await expect(page.getByRole("heading",{name:ru?"Мой портфель":"My portfolio",exact:true})).toBeVisible();
 await expect(page.getByRole("button",{name:"Alpha",exact:true})).toBeVisible();await expect(page.getByRole("button",{name:"Beta",exact:true})).toBeVisible();
 await page.getByRole("button",{name:"Alpha",exact:true}).click();await expect(page).toHaveURL(new RegExp("group="+a));await expect(page.locator("tbody tr").first()).toContainText(ru?"1 шт.":"1 units");
 const filteredSummaryCalls=summaryCalls;
 await page.goto("/investments?group="+a+"&group="+b);await expect(page.locator("tbody tr").first()).toContainText(ru?"3 шт.":"3 units");
 await page.goBack();await expect(page.locator("tbody tr").first()).toContainText(ru?"1 шт.":"1 units");expect(summaryCalls).toBe(filteredSummaryCalls);
 await page.locator("header summary").filter({hasText:ru?"Инвестиции":"Investments"}).click();
 await page.getByRole("navigation",{name:ru?"Основная навигация":"Main navigation"}).getByRole("link",{name:ru?"Группы бумаг":"Security groups",exact:true}).click();await expect(page.getByRole("heading",{name:ru?"Группы бумаг":"Security groups",exact:true})).toBeVisible();await expect(page.getByText("OLD-CLOSED",{exact:true})).toBeVisible();
 await page.getByRole("textbox",{name:(ru?"Название группы":"Group name")+": Alpha",exact:true}).fill("Core");
 await page.getByRole("textbox",{name:ru?"Новая группа":"New group",exact:true}).fill("New");await page.getByRole("button",{name:ru?"Добавить группу":"Add group",exact:true}).click();
 await page.getByRole("button",{name:(ru?"Удалить группу":"Delete group")+": Beta",exact:true}).click();
 const newGroup=await page.getByRole("combobox",{name:(ru?"Группа для":"Group for")+" ABC · Broker one",exact:true}).locator("option").filter({hasText:"New"}).getAttribute("value");
 await page.locator("li[draggable=true]").filter({hasText:"Broker one"}).dragTo(page.locator(`section[data-group-id="${newGroup}"]`));await expect(page.getByRole("combobox",{name:(ru?"Группа для":"Group for")+" ABC · Broker one",exact:true})).toHaveValue(newGroup!);
 await page.getByRole("button",{name:ru?"Сохранить группы":"Save groups",exact:true}).click();await expect(page.getByRole("button",{name:ru?"Сохранить группы":"Save groups",exact:true})).toBeDisabled();expect(writes).toBe(1);
 await page.reload();await expect(page.getByText("OLD-CLOSED",{exact:true})).toBeVisible();await expect(page.getByRole("combobox",{name:(ru?"Группа для":"Group for")+" ABC · Broker two",exact:true})).toHaveValue("");
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`portfolio-group-manager-${locale}.png`),fullPage:true});
 await page.getByRole("button",{name:ru?"Основная навигация":"Main navigation"}).click();
 await page.getByRole("navigation",{name:ru?"Основная навигация":"Main navigation"}).getByRole("link",{name:ru?"Портфель":"Portfolio",exact:true}).click();await expect(page).toHaveURL(new RegExp("group="+a));await expect(page.locator("tbody tr")).toHaveCount(0);await expect(page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Прибыль":"Profit",exact:true})}).first()).toContainText("0 RUB");
 await page.getByRole("button",{name:ru?"Все категории":"All categories",exact:true}).click();await page.getByRole("button",{name:"New",exact:true}).click();await expect(page.locator("tbody tr").first()).toContainText(ru?"1 шт.":"1 units");
 await page.getByRole("button",{name:ru?"Все категории":"All categories",exact:true}).click();
 classification=create(InvestmentGroupsSchema,{...classification,version:3n,groups:classification.groups.map(g=>g.id===newGroup?{...g,name:"New updated"}:g)});
 await page.getByRole("button",{name:ru?"Обновить портфель":"Refresh portfolio",exact:true}).click();
 await expect(page.getByRole("button",{name:"New updated",exact:true})).toBeVisible();await expect(page.getByRole("img",{name:ru?"Распределение активов":"Asset allocation",exact:true})).toBeVisible();
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`portfolio-groups-${locale}.png`),fullPage:true});expect(errors).toEqual([]);
 });}
