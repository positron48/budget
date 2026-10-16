// Actual local browser -> Envoy -> API -> PostgreSQL, with disposable users.
import {test,expect} from "@playwright/test";
import {createClient,Code} from "@connectrpc/connect";
import {createGrpcWebTransport} from "@connectrpc/connect-web";
import {randomUUID} from "node:crypto";
import {AuthService} from "../proto/budget/v1/auth_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {portfolioReport} from "../lib/portfolio-report";
import {positionDecimal} from "../lib/investment-positions";
const enabled=process.env.E2E_FULLSTACK==="1",phase=process.env.E2E_FULLSTACK_PHASE||"write";
test.skip(!enabled,"Run scripts/investments/fullstack_smoke.py against disposable local services");
const password="Disposable-smoke-146!",stamp=(s:string)=>({seconds:BigInt(Date.parse(s)/1000),nanos:0});
for(const locale of ["ru","en"])test(`${locale} simple portfolio ${phase}`,async({page,context})=>{
 const ru=locale==="ru",runId=process.env.E2E_FULLSTACK_RUN!;
 expect(runId).toMatch(/^[a-f0-9]{32}$/);
 const baseURL=test.info().project.use.baseURL as string;
 await context.route("**/*",async route=>{const url=new URL(route.request().url());if(["127.0.0.1","localhost"].includes(url.hostname))await route.continue();else await route.abort("blockedbyclient");});
 const auth=createClient(AuthService,createGrpcWebTransport({baseUrl:baseURL+"/grpc"})),email=`smoke-${runId}-${locale}@example.test`;
 let tokens,tenantId;
 if(phase==="write"){const r=await auth.register({email,password,name:"Disposable smoke",locale,tenantName:`Fullstack ${locale}`});tokens=r.tokens!;tenantId=r.tenant!.id;}
 else{const r=await auth.login({email,password});tokens=r.tokens!;tenantId=r.memberships[0].tenant!.id;}
 const transport=createGrpcWebTransport({baseUrl:baseURL+"/grpc",interceptors:[next=>async req=>{req.header.set("Authorization","Bearer "+tokens.accessToken);req.header.set("x-tenant-id",tenantId);return next(req)}]});
 const api=createClient(InvestmentService,transport),nav=createClient(InvestmentNAVService,transport);
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:baseURL}]);
 await page.addInitScript(({access,refresh,tenant})=>{localStorage.setItem("budget/access",access);localStorage.setItem("budget/refresh",refresh);localStorage.setItem("budget/tenant",tenant)},{access:tokens.accessToken,refresh:tokens.refreshToken,tenant:tenantId});
 if(phase==="disabled"){
  await expect(api.listInvestmentLedgers({})).rejects.toMatchObject({code:Code.Unavailable});
  expect((await page.goto("/investments"))?.status()).toBe(404);return;
 }
 let ledger=(await api.listInvestmentLedgers({})).ledgers.find(l=>l.name===`Portfolio ${locale}`)!;
 if(phase==="write"){
  const outsider=await auth.register({email:`outsider-${runId}-${locale}@example.test`,password,name:"Outsider",locale});
  const denied=createClient(InvestmentService,createGrpcWebTransport({baseUrl:baseURL+"/grpc",interceptors:[next=>async req=>{req.header.set("Authorization","Bearer "+outsider.tokens!.accessToken);req.header.set("x-tenant-id",tenantId);return next(req)}]}));
  await expect(denied.listInvestmentLedgers({})).rejects.toMatchObject({code:Code.PermissionDenied});
  ledger=(await api.createInvestmentLedger({name:`Portfolio ${locale}`,defaultCurrency:"RUB",requestKey:randomUUID()})).ledger!;
  for(const [i,fact] of [{kind:"buy",quantity:"2",amount:"200",fee:"2"},{kind:"sell",quantity:"1",amount:"120",fee:"1"},{kind:"coupon",quantity:"0",amount:"15",taxReviewRequired:true}].entries()){
   const state=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;
   const operation={...fact,ledgerId:ledger.id,instrumentKey:"TRACKER-STOCK",currency:"RUB",occurredAt:stamp(`2024-01-0${i+2}T12:00:00Z`)};
   const request={operation,action:"create",expectedLedgerVersion:state.ledger!.version,requestKey:randomUUID()};
   const preview=await api.previewInvestmentOperation(request);expect(preview.valid).toBe(true);
   const saved={...request,previewHash:preview.previewHash};
   const first=await api.saveInvestmentOperation(saved),retry=await api.saveInvestmentOperation(saved);expect(retry.operation!.id).toBe(first.operation!.id);
  }
  await api.createInvestmentLedger({name:`Secondary ${locale}`,defaultCurrency:"RUB",requestKey:randomUUID()});
 }
 const state=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;
 expect(state.ledger!.portfolioTracking).toBe(true);expect(state.cash).toHaveLength(0);
 expect(state.positions[0].quantity).toBe("1");expect(state.positions[0].costBasis).toBe("principal");expect(state.positions[0].cleanCost).toBe("100");expect(state.positions[0].realizedClean).toBe("20");expect(state.positions[0].couponEarned).toBe("15");
 const operations=(await api.listInvestmentOperations({ledgerId:ledger.id,limit:200})).operations;
 const report=portfolioReport([state],operations,new Map([["TRACKER-STOCK\0RUB",{price:{numerator:"100",denominator:"1"},at:Date.now()}]]),"RUB").positions[0];
 // Principal cost excludes fees, while profit includes both buy/sell fees once.
 expect(positionDecimal(report.cost)).toBe("100");expect(positionDecimal(report.realized)).toBe("18");expect(positionDecimal(report.profit)).toBe("32");
 await page.goto("/investments");await expect(page.getByRole("heading",{name:ru?"Мой портфель":"My portfolio",exact:true})).toBeVisible();
 await page.getByRole("button",{name:ru?"Без категории":"Uncategorized",exact:true}).click();
 await expect(page.getByRole("table")).toContainText("TRACKER-STOCK");
 await expect(page.getByRole("table")).toContainText("100 RUB");
 await page.getByRole("link",{name:ru?"Выплаты":"Payouts",exact:true}).click();await expect(page.getByRole("table")).toContainText("15");
 if(phase==="readonly"){
  await expect(page.getByRole("button",{name:ru?"Добавить операцию":"Add operation",exact:true})).toHaveCount(0);
  await expect(api.createInvestmentLedger({name:"Denied",requestKey:randomUUID()})).rejects.toMatchObject({code:Code.FailedPrecondition});
 }
 await page.getByText(ru?"Выбрать несколько счетов":"Select multiple accounts",{exact:true}).click();
 if(phase==="write"){
  await page.getByRole("group",{name:ru?"Счета в портфеле":"Portfolio accounts",exact:true}).getByLabel(`Secondary ${locale}`,{exact:true}).uncheck();
  await page.getByLabel(ru?"Название набора счетов":"Account set name",{exact:true}).fill(`Primary ${locale}`);
  await page.getByRole("button",{name:ru?"Сохранить набор":"Save account set",exact:true}).click();
 }
 await expect(page.getByRole("button",{name:`Primary ${locale}`,exact:true})).toBeVisible();
 await page.getByRole("button",{name:`Primary ${locale}`,exact:true}).click();
 const sets=(await nav.listInvestmentPortfolios({})).portfolios;
 expect(sets).toHaveLength(1);expect(sets[0].trackerSelection).toBe(true);expect(sets[0].ledgerIds).toEqual([ledger.id]);expect(sets[0].seriesKey).toBe("");
 if(phase==="readonly")await expect(page.getByRole("button",{name:ru?"Сохранить набор":"Save account set",exact:true})).toHaveCount(0);
 await page.goto(`/investments/calendar?account=${ledger.id}&month=2024-01`);
 await expect(page.getByRole("heading",{name:ru?"Календарь выплат":"Payout calendar",exact:true})).toBeVisible();
 await expect(page.getByRole("heading",{name:ru?"План по месяцам":"Monthly plan",exact:true})).toBeVisible();
 const calendar=page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Предстоящие и полученные выплаты":"Planned and received payouts",exact:true})});
 if(phase==="write"){
  await page.getByRole("button",{name:ru?"Добавить событие":"Add event",exact:true}).click();
  const dialog=page.getByRole("dialog");
  await dialog.getByRole("combobox",{name:ru?"Бумага":"Security",exact:true}).fill("TRACKER-STOCK");
  await dialog.getByRole("combobox",{name:ru?"Тип":"Type",exact:true}).selectOption("coupon");
  await dialog.getByLabel(ru?"Сумма на бумагу":"Amount per unit").fill("15");
  await dialog.getByLabel(ru?"Дата выплаты":"Payment date",{exact:true}).fill("2024-01-04");
  await dialog.getByLabel(ru?"Источник или основание прогноза":"Source or projection basis").fill("Synthetic issuer schedule");
  await dialog.getByLabel(ru?"Пояснение":"Explanation").fill("Synthetic historical cutoff");
  await dialog.locator("summary").filter({hasText:ru?"Даты и право":"Dates and entitlement"}).click();
  await dialog.getByLabel(ru?"Правило отсечки":"Cutoff rule").selectOption("trade_before_cutoff");
  await dialog.getByLabel(ru?"Момент отсечки (UTC)":"Cutoff instant (UTC)").fill("2024-01-03T13:00");
  await dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true}).click();await expect(dialog).toHaveCount(0);
  await expect(calendar).toContainText("15 RUB");
  await calendar.getByRole("button",{name:ru?"Сопоставить выплату":"Match receipt",exact:true}).click();
  await dialog.getByRole("combobox",{name:ru?"Полученная выплата":"Received payout",exact:true}).selectOption(operations.find(o=>o.kind==="coupon")!.id);
  await dialog.getByRole("checkbox").check();await dialog.getByRole("button",{name:ru?"Связать":"Link",exact:true}).click();await expect(dialog).toHaveCount(0);
 }
 await expect(calendar).toContainText(ru?"Получено":"Received");await expect(calendar).toContainText("15 RUB");
 if(phase==="readonly")await expect(page.getByRole("button",{name:ru?"Добавить событие":"Add event",exact:true})).toHaveCount(0);
 await page.goto(`/investments/instruments/TRACKER-STOCK?account=${ledger.id}`);
 await page.getByText(ru?"Дивиденды эмитента":"Issuer dividends",{exact:true}).click();
 const issuer=page.getByRole("region",{name:ru?"Дивиденды на акцию":"Dividends per share",exact:true});
 if(phase==="write"){
  await issuer.getByRole("button",{name:ru?"Добавить историю эмитента":"Add issuer history",exact:true}).click();const editor=page.getByRole("dialog");
  await editor.getByLabel(ru?"Сплиты проверены начиная с (UTC)":"Splits reviewed from (UTC)").fill("2023-01-01T00:00");
  await editor.getByLabel(ru?"Источник: ссылка или документ":"Source: link or document").fill("Synthetic issuer statements");
  await editor.getByLabel(ru?"Что проверено по источнику":"Source review notes").fill("Synthetic ordinary/special payouts and complete two-for-one unit history");
  for(const [i,date,amount,kind] of [[0,"2023-07-01","100","regular"],[1,"2024-07-01","75","regular"],[2,"2024-09-01","20","special"]] as const){
   await editor.getByRole("button",{name:ru?"Добавить выплату":"Add payout",exact:true}).click();
   await editor.getByLabel(ru?"Дата выплаты эмитента":"Issuer payment date",{exact:true}).nth(i).fill(date);
   await editor.getByLabel(ru?"Сумма на акцию":"Amount per share",{exact:true}).nth(i).fill(amount);
   await editor.getByLabel(ru?"Тип дивиденда":"Dividend class",{exact:true}).nth(i).selectOption(kind);
   await editor.getByLabel(ru?"Факт выплаты эмитента":"Issuer payment status",{exact:true}).nth(i).selectOption("paid");
   await editor.getByLabel(ru?"Единица этой суммы на (UTC)":"This amount's share unit at (UTC)",{exact:true}).nth(i).fill(date+"T00:00");
  }
  await editor.getByRole("checkbox",{name:/2023/}).check();await editor.getByRole("checkbox",{name:/2024/}).check();
  await editor.locator("summary").filter({hasText:ru?"Сплиты и изменения единицы":"Splits and share-unit changes"}).click();
  await editor.getByRole("button",{name:ru?"Добавить сплит":"Add split",exact:true}).click();
  await editor.getByLabel(ru?"Момент сплита (UTC)":"Split instant (UTC)").fill("2024-01-01T00:00");await editor.getByLabel(ru?"Новых акций":"New units").fill("2");await editor.getByLabel(ru?"Старых акций":"Old units").fill("1");
  await editor.getByRole("checkbox",{name:ru?"Проверены все сплиты и изменения единицы за указанный период":"All splits and share-unit changes in the stated range were reviewed",exact:true}).check();
  await editor.getByRole("button",{name:ru?"Сохранить историю":"Save history",exact:true}).click();await expect(editor).toHaveCount(0);
  await issuer.getByRole("button",{name:ru?"Изменить историю":"Edit history",exact:true}).click();
  await editor.getByLabel(ru?"Причина изменения":"Change reason",{exact:true}).fill("Synthetic source review update");
  await editor.getByRole("checkbox",{name:ru?"Проверены все сплиты и изменения единицы за указанный период":"All splits and share-unit changes in the stated range were reviewed",exact:true}).check();await editor.getByRole("button",{name:ru?"Сохранить историю":"Save history",exact:true}).click();await expect(editor).toHaveCount(0);
 }
 const annual=issuer.getByRole("row").filter({has:page.getByRole("cell",{name:"2024",exact:true})});await expect(annual).toContainText("75 RUB");await expect(annual).toContainText("20 RUB");await expect(annual).toContainText("50%");
 const history=(await api.getInvestmentDividendGrowth({instrumentKey:"TRACKER-STOCK"})).histories[0];expect(history.history!.version).toBe(2n);expect(history.years.find(y=>y.year===2024)!.percentChange!.numerator).toBe("50");
 if(phase==="readonly"){await expect(issuer.getByRole("button",{name:ru?"Добавить историю эмитента":"Add issuer history",exact:true})).toHaveCount(0);await expect(issuer.getByRole("button",{name:ru?"Изменить историю":"Edit history",exact:true})).toHaveCount(0);}
 const unchanged=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;
 expect(unchanged.ledger!.version).toBe(state.ledger!.version);expect(unchanged.positions).toEqual(state.positions);
 await page.goto("/investments");await expect(page.getByRole("heading",{name:ru?"Мой портфель":"My portfolio",exact:true})).toBeVisible();
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
