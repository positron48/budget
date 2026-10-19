import {chartFallback} from "./fixtures/chart-fallback";
import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService,InvestmentPortfolioSchema} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])test(`${locale} account subsets are deduplicated, saved and restored without copying trades`,async({page,context})=>{
 const ru=locale==="ru",ids=["11111111-1111-4111-8111-111111111111","22222222-2222-4222-8222-222222222222","33333333-3333-4333-8333-333333333333"],at=1704196800n,ledgers=ids.map((id,i)=>({id,name:`Broker ${i+1}`,version:1n,portfolioTracking:true})),key="STOCK";
 let archivedThird=false,otherActor=false,otherRequests=0;
 let releaseOther!:()=>void;const otherResponse=new Promise<void>(resolve=>{releaseOther=resolve;});
 const f=(n:number)=>({numerator:String(n),denominator:"1"}),errors:string[]=[],historyScopes:string[][]=[],saved:any[]=[],writes:string[]=[];
 page.on("pageerror",e=>errors.push(e.message));await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","header."+btoa(JSON.stringify({sub:"synthetic-owner",tenant_id:"tenant"}))+".signature");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  if(!method)throw new Error("Unknown RPC "+path);let data:any={};const q=ids.indexOf(input.ledgerId)+1;
  if(method.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:1}]};
  if(method.name==="ListInvestmentLedgers")data={ledgers:ledgers.filter(l=>input.includeArchived||!archivedThird||l.id!==ids[2]).map(l=>({...l,archived:archivedThird&&l.id===ids[2]}))};
  if(method.name==="ListInvestmentPortfolios"){if(otherActor){otherRequests++;await otherResponse;}data={portfolios:otherActor?[]:saved};}
  if(method.name==="GetInvestmentSnapshot")data={snapshot:{ledger:ledgers[q-1],positions:[{instrumentKey:key,currency:"RUB",quantity:String(q),costBasis:"principal",lots:[{originId:input.ledgerId+"buy",acquisitionOperationId:q===1?"archived-source-buy":input.ledgerId+"buy",originLedgerId:q===1?"44444444-4444-4444-8444-444444444444":input.ledgerId,quantity:String(q),cleanCost:String(q*100),acquiredAccrued:"0",acquiredAt:{seconds:at}}]}]}};
  if(method.name==="ListInvestmentOperations")data={operations:[{id:input.ledgerId+"buy",ledgerId:input.ledgerId,kind:"buy",status:"posted",instrumentKey:key,currency:"RUB",quantity:String(q),amount:String(q*100),fee:"0",accrued:"0",occurredAt:{seconds:at}},{id:input.ledgerId+"div",ledgerId:input.ledgerId,kind:"dividend",status:"posted",instrumentKey:key,currency:"RUB",amount:String(q*5),occurredAt:{seconds:at+86400n}}]};
  if(method.name==="ListInvestmentValuationProfiles")data={fxProfiles:[{key:"market",mode:"market",version:1n}]};
  if(method.name==="GetInvestmentExchangeRates")data={currency:input.currency,fxPolicyKey:"market",policyVersion:1n,mode:"market",readAt:{seconds:BigInt(Math.floor(Date.now()/1000))},rates:["",...input.dates].flatMap((date:string)=>["RUB","USD","EUR"].map(fromCurrency=>({date,fromCurrency,rate:f(1)})))};
  if(method.name==="GetInvestmentQuotes")data={quotes:[{instrumentKey:key,currency:"RUB",state:"fresh",cleanPrice:f(120),priceAt:{seconds:BigInt(Math.floor(Date.now()/1000)-60)},source:"tinvest/personal"}]};
  if(method.name==="GetInvestmentGroups")data={classification:{version:1n,groups:[],memberships:[]}};
  if(await chartFallback(route,method))return;
  if(method.name==="GetTrackerHistory"){
   expect(new Set(input.ledgerIds).size).toBe(input.ledgerIds.length);historyScopes.push(input.ledgerIds);
   const sum=input.ledgerIds.reduce((n:number,id:string)=>n+ids.indexOf(id)+1,0);
   data={performanceSnapshot:"synthetic-page",points:[{date:input.fromDate,state:"ready",knownValue:f(sum*120),positions:1,pricedPositions:1,coverageKey:"a".repeat(64)}],currency:input.currency,fxPolicyKey:"market",fxPolicyVersion:1n,fxMode:"market",basisKey:"b".repeat(64),priceConvention:"retrospective_close_next_utc_day_v1",reportingTimezone:"Europe/Moscow"};
  }
  if(method.name==="GetTrackerPerformance")data={fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:"market",profit:f(input.ledgerIds.length*10),twrReason:"insufficient_history",xirrReason:"insufficient_history",historyScope:"recorded_securities_strategy",reportingTimezone:"Europe/Moscow",twrConvention:"securities_daily_end_of_day_flows"};
  if(method.name==="SaveInvestmentPortfolio"){
   writes.push(method.name);expect(input.trackerSelection).toBe(true);expect(input.currency).toBe("");expect(input.conventionKey).toBe("");expect(input.ledgerIds).toEqual([ids[0],ids[2]]);
   const value=create(InvestmentPortfolioSchema,{id:input.id||"44444444-4444-4444-8444-444444444444",name:input.name,ledgerIds:input.ledgerIds,trackerSelection:true,archived:input.archived,version:input.expectedVersion+1n});saved.splice(0,saved.length,value);data={portfolio:value};
  }
  if(/^(SaveInvestmentOperation|Commit|CreateInvestmentLedger)/.test(method.name))throw new Error("Unexpected financial write");
  const body=toBinary(method.output,create(method.output,data)),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),end=Buffer.alloc(5);end[0]=128;end.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,end,trailer])});
 });
 await page.goto(`/investments?account=${ids[0]}&account=${ids[2]}&account=${ids[0]}&from=2024-01-02&to=2024-01-02`);
 const cost=page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Вложено":"Invested",exact:true})});await expect(cost).toContainText("400 RUB");await expect(page.getByRole("table")).toContainText("480 RUB");
 await expect.poll(()=>historyScopes.some(s=>JSON.stringify(s)==JSON.stringify([ids[0],ids[2]]))).toBe(true);
 await page.getByText(ru?"Выбрать несколько счетов":"Select multiple accounts",{exact:true}).click();
 const set=page.getByRole("group",{name:ru?"Счета в портфеле":"Portfolio accounts",exact:true});await expect(set.getByLabel("Broker 2",{exact:true})).not.toBeChecked();
 await page.getByLabel(ru?"Название набора счетов":"Account set name",{exact:true}).fill("Selected brokers");await page.getByRole("button",{name:ru?"Сохранить набор":"Save account set",exact:true}).click();await expect(page.getByRole("button",{name:"Selected brokers",exact:true})).toBeVisible();
 await page.getByRole("button",{name:ru?"Все счета":"All accounts",exact:true}).click();await expect(cost).toContainText("600 RUB");await page.getByRole("button",{name:"Selected brokers",exact:true}).click();await expect(cost).toContainText("400 RUB");
 await page.goBack();await expect(cost).toContainText("600 RUB");await page.goForward();await expect(cost).toContainText("400 RUB");
 await page.getByRole("button",{name:ru?"Добавить операцию":"Add operation",exact:true}).click();await expect(page.getByRole("dialog").getByRole("combobox",{name:ru?"Счёт":"Account",exact:true})).toHaveValue("");await page.getByRole("dialog").getByRole("button",{name:"Close",exact:true}).click();
 await set.getByLabel("Broker 1",{exact:true}).uncheck();await set.getByLabel("Broker 3",{exact:true}).uncheck();await expect(page.getByRole("status").filter({hasText:ru?"Выберите хотя бы":"Select at least"})).toBeVisible();await expect(cost).toHaveCount(0);
 await page.getByRole("button",{name:"Selected brokers",exact:true}).click();await expect(cost).toContainText("400 RUB");await page.reload();await expect(cost).toContainText("400 RUB");
 await page.getByText(ru?"Выбрать несколько счетов":"Select multiple accounts",{exact:true}).click();await page.getByRole("button",{name:(ru?"Архивировать набор ":"Archive account set ")+"Selected brokers",exact:true}).click();await expect(page.getByRole("button",{name:(ru?"Восстановить набор ":"Restore account set ")+"Selected brokers",exact:true})).toBeVisible();
 archivedThird=true;await page.reload();await expect(cost).toContainText("400 RUB");
 await page.getByRole("button",{name:ru?"Все счета":"All accounts",exact:true}).click();await expect(cost).toContainText("300 RUB");
 await page.getByRole("button",{name:"Broker 1",exact:true}).click();await expect(cost).toContainText("100 RUB");await expect(page.getByRole("button",{name:"Broker 3",exact:true})).toHaveCount(0);
 await page.getByText(ru?"Выбрать несколько счетов":"Select multiple accounts",{exact:true}).click();await page.getByRole("button",{name:ru?"Selected brokers · архив":"Selected brokers · archived",exact:true}).click();await expect(cost).toContainText("400 RUB");
 // The cost card can render before the previous account's day query settles.
 // Wait for that calculation before proving an invalid selection starts none.
 const daily=page.getByRole("region",{name:ru?"Дневной результат":"Daily result",exact:true});
 await expect(daily).toBeVisible();await expect(daily.locator("p.tabular-nums").first()).not.toHaveText("…");
 const historyCount=historyScopes.length;await page.goto(`/investments?account=${ids[0]}&account=99999999-9999-4999-8999-999999999999`);await expect(page.getByRole("alert").filter({hasText:ru?"Выбранный счёт недоступен":"The selected account is unavailable"})).toBeVisible();await expect(cost).toHaveCount(0);expect(historyScopes.length).toBe(historyCount);await page.goBack();await expect(cost).toContainText("400 RUB");
 await page.setViewportSize({width:375,height:812});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`account-selection-${locale}.png`),fullPage:true});expect(writes).toHaveLength(2);
 otherActor=true;await page.evaluate(()=>{localStorage.setItem("budget/access","header."+btoa(JSON.stringify({sub:"synthetic-other",tenant_id:"tenant"}))+".signature");window.dispatchEvent(new Event("budget-auth-changed"));});
 await expect.poll(()=>otherRequests).toBeGreaterThan(0);
 try{await expect(page.getByRole("button",{name:ru?"Selected brokers · архив":"Selected brokers · archived",exact:true})).toHaveCount(0);}finally{releaseOther();}
 expect(errors).toEqual([]);
});
