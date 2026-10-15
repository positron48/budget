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
 await expect(page.getByRole("table")).toContainText("TRACKER-STOCK");
 await page.getByRole("button",{name:ru?"Дивиденды и купоны":"Dividends and coupons",exact:true}).click();await expect(page.getByRole("table")).toContainText("15");
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
 const unchanged=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;
 expect(unchanged.ledger!.version).toBe(state.ledger!.version);expect(unchanged.positions).toEqual(state.positions);
 await page.goto("/investments");await expect(page.getByRole("heading",{name:ru?"Мой портфель":"My portfolio",exact:true})).toBeVisible();
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
