import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService,InvestmentForecastEventSchema,type InvestmentForecastEvent} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])test(`${locale} payout calendar edits evidence, matches existing receipts and shows comparable growth`,async({page,context})=>{
 const ru=locale==="ru",readOnly=process.env.E2E_INVESTMENTS_READ_ONLY==="true",ledger="11111111-1111-4111-8111-111111111111",key="tinvest:22222222-2222-4222-8222-222222222222",id="33333333-3333-4333-8333-333333333333",operation="44444444-4444-4444-8444-444444444444";
 const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 const calls:any[]=[],f=(n:string)=>({numerator:n,denominator:"1"});
 let event=create(InvestmentForecastEventSchema,{id,version:1n,instrumentKey:key,kind:"dividend",nature:"announced",currency:"USD",perShare:"2",paymentDate:"2026-11-10",timezone:"Europe/Moscow",entitlementRule:"settled_by_cutoff",entitlementAt:{seconds:1791190805n,nanos:123000},sourceReference:"Synthetic issuer",evidence:"Historical entitlement before sale",reason:"Synthetic fixture"});
 const events:InvestmentForecastEvent[]=[event];let link:any;
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string},{name:"budget_theme",value:ru?"dark":"light",url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","synthetic-session");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,InvestmentNAVService,InvestmentImportService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  if(method)calls.push({method:method.name,input});let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers:[{id:ledger,name:"Broker",portfolioTracking:true,version:5n}]};
  if(method?.name==="ListInvestmentValuationProfiles")data={fxProfiles:[{key:"market",mode:"market",version:1n},{key:"cbr",mode:"cbr",version:1n}]};
  if(method?.name==="GetInvestmentForecast"){
   expect(input.taxRate).toBe("");expect(input.ledgerIds).toEqual([ledger]);
   const paid=link&&!link.cancelled&&link.complete;
   data={fromMonth:input.fromMonth,toMonth:"2027-09",currency:input.currency,fxPolicy:input.fxPolicyKey+"@1",events,links:link?[link]:[],announced:paid?0:1,projected:1,unknown:1,paid:paid?1:0,
    months:Array.from({length:12},(_,i)=>({month:new Date(Date.UTC(2026,9+i,1)).toISOString().slice(0,7),gross:i===1?undefined:f("0"),knownGross:i===1?f("3600"):f("0"),unknown:i===1?1:0,announced:i===1?1:0,projected:i===1?1:0})),
    rows:event.cancelled?[]:[{eventId:event.id,eventVersion:event.version,ledgerId:ledger,instrumentKey:key,kind:"dividend",nature:"announced",paymentDate:event.paymentDate,currency:"USD",quantity:f("20"),gross:paid?undefined:f("40"),convertedGross:paid?undefined:f("3600"),actualNet:paid?f("100"):undefined,entitlement:"historical_right",status:paid?"paid":"upcoming",issues:[]}]
   };
  }
  if(method?.name==="GetInvestmentPayouts")data={growth:[{month:"2026-09",previousMonth:"2025-09",currency:"USD",incomeNet:f("200"),previousIncomeNet:f("100"),change:f("100"),percentChange:f("100"),status:"available"},{month:"2026-10",previousMonth:"2025-10",currency:"USD",incomeNet:f("20"),previousIncomeNet:f("0"),status:"partial_interval"}]};
  if(method?.name==="ListBrokerSnapshots")data={snapshots:[{id:"snapshot",accountId:"account",createdAt:{seconds:1n}}]};
  if(method?.name==="GetBrokerSnapshot")data={originalJson:new TextEncoder().encode(JSON.stringify({fetched_at:new Date().toISOString(),operations:[{ticker:"ABC",instrumentUid:key.slice(8),name:"Synthetic share"}]}))};
  if(method?.name==="ListInvestmentOperations")data={operations:[{id:operation,ledgerId:ledger,instrumentKey:key,kind:"dividend",currency:"USD",amount:"100",tax:"13",status:"posted",version:5n,occurredAt:{seconds:1791190805n}}]};
  if(method?.name==="SaveInvestmentForecastEvent"){
   expect(readOnly).toBe(false);if(input.event.id===id){expect(input.expectedVersion).toBe(event.version);expect(input.event.entitlementAt).toEqual(event.entitlementAt);event=create(InvestmentForecastEventSchema,{...input.event,version:event.version+1n});events[0]=event;data={event};}
   else{expect(input.expectedVersion).toBe(0n);expect(input.event.instrumentKey).toBe(key);const created=create(InvestmentForecastEventSchema,{...input.event,version:1n});events.push(created);data={event:created};}
  }
  if(method?.name==="SaveInvestmentForecastLink"){expect(readOnly).toBe(false);expect(input.link.operationId).toBe(operation);expect(input.link.operationVersion).toBe(5n);expect(input.link.eventVersion).toBe(event.version);link={...input.link,version:(link?.version||0n)+1n};data={link};}
  if(!method)throw new Error("Unknown RPC "+path);
  const body=toBinary(method.output,create(method.output,data)),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),end=Buffer.alloc(5);end[0]=128;end.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,end,trailer])});
 });
 await page.goto(`/investments/calendar?month=2026-10&account=${ledger}`);
 await expect(page.getByRole("heading",{name:ru?"Календарь выплат":"Payout calendar",exact:true})).toBeVisible();
 const monthly=page.locator("section").filter({has:page.getByRole("heading",{name:ru?"План по месяцам":"Monthly plan",exact:true})});await expect(monthly).toContainText("2027-09");await expect(monthly).toContainText("—");await expect(monthly).toContainText(/3[\s,.]?600 RUB/);
 const rows=page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Предстоящие и полученные выплаты":"Planned and received payouts",exact:true})});await expect(rows).toContainText("ABC");await expect(rows).toContainText(ru?"Историческое право":"Historical entitlement");
 const growth=page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Рост полученных выплат":"Growth in received payouts",exact:true})});await expect(growth).toContainText("100 USD · 100 %");await expect(growth).toContainText(ru?"Неполный интервал":"Incomplete interval");
 if(!readOnly){
  await page.locator("summary").filter({hasText:ru?"События и источники":"Events and sources"}).click();await page.getByRole("button",{name:ru?"Изменить":"Edit",exact:true}).click();
  const dialog=page.getByRole("dialog");await dialog.getByLabel(ru?"Сумма на бумагу":"Amount per unit",{exact:true}).fill("3.125");await dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true}).click();await expect(dialog).toHaveCount(0);expect(event.perShare).toBe("3.125");
  await page.getByRole("button",{name:ru?"Изменить":"Edit",exact:true}).click();await dialog.getByRole("checkbox",{name:ru?"Событие отменено":"Event cancelled",exact:true}).check();await dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true}).click();await expect(dialog).toHaveCount(0);await expect(rows.locator("tbody tr")).toHaveCount(0);
  await page.getByRole("button",{name:ru?"Изменить":"Edit",exact:true}).click();await dialog.getByRole("checkbox",{name:ru?"Событие отменено":"Event cancelled",exact:true}).uncheck();await dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true}).click();await expect(dialog).toHaveCount(0);await expect(rows.locator("tbody tr")).toHaveCount(1);
  await rows.getByRole("button",{name:ru?"Сопоставить выплату":"Match receipt",exact:true}).click();await dialog.getByRole("combobox",{name:ru?"Полученная выплата":"Received payout",exact:true}).selectOption(operation);await dialog.getByRole("checkbox").check();await dialog.getByRole("button",{name:ru?"Связать":"Link",exact:true}).click();await expect(rows).toContainText("100 USD");await expect(rows).toContainText(ru?"Получено":"Received");
  await rows.getByRole("button",{name:ru?"Сопоставить выплату":"Match receipt",exact:true}).click();await dialog.getByRole("combobox").selectOption(operation);await dialog.getByRole("button",{name:ru?"Снять связь":"Unlink receipt",exact:true}).click();await expect(rows).toContainText(ru?"Ожидается":"Upcoming");
  await page.getByRole("button",{name:ru?"Добавить событие":"Add event",exact:true}).click();await dialog.getByRole("combobox",{name:ru?"Бумага":"Security",exact:true}).fill("ABC");await dialog.getByLabel(ru?"Источник или основание прогноза":"Source or projection basis").fill("Synthetic projection");await dialog.getByLabel(ru?"Пояснение":"Explanation").fill("Constant holdings scenario");await dialog.getByLabel(ru?"Природа события":"Event nature").selectOption("forecast");await dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true}).click();await expect(dialog).toHaveCount(0);expect(events).toHaveLength(2);
 }else{await expect(page.getByRole("button",{name:ru?"Добавить событие":"Add event",exact:true})).toHaveCount(0);await expect(rows.getByRole("button")).toHaveCount(0);}
 await page.setViewportSize({width:375,height:812});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`calendar-${locale}.png`),fullPage:true});
 expect(calls.filter(c=>/^(SaveInvestmentOperation|Commit|Preview)/.test(c.method))).toEqual([]);expect(errors).toEqual([]);
});
