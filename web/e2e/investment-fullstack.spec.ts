// Explicit opt-in. Real local services only; external browser traffic is blocked.
// No fulfilled/mocked RPC, injected database rows or existing account credentials.
import {test,expect} from "@playwright/test";
import {createClient,Code} from "@connectrpc/connect";
import {createGrpcWebTransport} from "@connectrpc/connect-web";
import {randomUUID,createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import {AssetService} from "../proto/budget/v1/asset_pb";
import {AuthService} from "../proto/budget/v1/auth_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
const enabled=process.env.E2E_FULLSTACK==="1",phase=process.env.E2E_FULLSTACK_PHASE||"write";
test.skip(!enabled,"Run with scripts/investments/fullstack_smoke.py against disposable local services");
const password="Disposable-smoke-146!",stamp=(s:string)=>({seconds:BigInt(Date.parse(s)/1000),nanos:0});
for(const locale of ["ru","en"]){
 test(`${locale} authenticated journal and forecast ${phase}`,async({page,context})=>{
  const ru=locale==="ru",runId=process.env.E2E_FULLSTACK_RUN!;
  expect(runId).toMatch(/^[a-f0-9]{32}$/);
  const baseURL=test.info().project.use.baseURL as string;
  expect(baseURL).toBe("http://127.0.0.1:3030");
  await context.route("**/*",async route=>{
   const url=new URL(route.request().url());
   if(["127.0.0.1","localhost"].includes(url.hostname))await route.continue();else await route.abort("blockedbyclient");
  });
  const publicTransport=createGrpcWebTransport({baseUrl:baseURL+"/grpc"});
  const auth=createClient(AuthService,publicTransport),email=`smoke-${runId}-${locale}@example.test`;
  let tokens,tenantId;
  if(phase==="write"){
   const registered=await auth.register({email,password,name:"Disposable smoke",locale,tenantName:`Fullstack ${locale}`});
   tokens=registered.tokens!;tenantId=registered.tenant!.id;
  }else{
   const login=await auth.login({email,password});tokens=login.tokens!;tenantId=login.memberships[0].tenant!.id;
  }
  const transport=createGrpcWebTransport({baseUrl:baseURL+"/grpc",interceptors:[next=>async req=>{req.header.set("Authorization","Bearer "+tokens.accessToken);req.header.set("x-tenant-id",tenantId);return next(req)}]});
  const api=createClient(InvestmentService,transport);
  // Obtain a different, actual user's token to prove server-side tenant isolation.
  if(phase==="write"){
   const outsider=await auth.register({email:`outsider-${runId}-${locale}@example.test`,password,name:"Outsider",locale});
   const denied=createClient(InvestmentService,createGrpcWebTransport({baseUrl:baseURL+"/grpc",interceptors:[next=>async req=>{req.header.set("Authorization","Bearer "+outsider.tokens!.accessToken);req.header.set("x-tenant-id",tenantId);return next(req)}]}));
   await expect(denied.listInvestmentLedgers({})).rejects.toMatchObject({code:Code.PermissionDenied});
  }
  await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:baseURL}]);
  await page.addInitScript(({access,refresh,tenant})=>{localStorage.setItem("budget/access",access);localStorage.setItem("budget/refresh",refresh);localStorage.setItem("budget/tenant",tenant)}, {access:tokens.accessToken,refresh:tokens.refreshToken,tenant:tenantId});
  if(phase==="disabled"){
   await expect(api.listInvestmentLedgers({})).rejects.toMatchObject({code:Code.Unavailable});
   const response=await page.goto("/investments/forecast");expect(response?.status()).toBe(404);
   const exportResponse=await page.goto("/investments/export");expect(exportResponse?.status()).toBe(404);
   const accounts=await createClient(AssetService,transport).listAccounts({});expect(accounts.accounts).toHaveLength(0);
   const assets=await page.goto("/assets");expect(assets?.status()).toBe(200);await expect(page.locator("main").last()).toBeVisible();
   await expect(page.getByRole("navigation").getByRole("link",{name:ru?"Инвестиции":"Investments",exact:true})).toHaveCount(0);
   return;
  }
  let ledger=(await api.listInvestmentLedgers({})).ledgers[0];
  if(phase==="write"){
   ledger=(await api.createInvestmentLedger({name:`Fullstack broker ${locale}`,broker:"Synthetic broker",accountType:"regular",defaultCurrency:"RUB",startedAt:stamp("2024-01-01T00:00:00Z"),requestKey:randomUUID()})).ledger!;
   const deposit={ledgerId:ledger.id,kind:"deposit",currency:"RUB",amount:"1000.123456789012",quantity:"0",occurredAt:stamp("2024-01-02T12:00:00Z"),settlementAt:stamp("2024-01-02T12:00:00Z")};
   const snapshot=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;
   const key=randomUUID(),preview=await api.previewInvestmentOperation({action:"create",operation:deposit,expectedLedgerVersion:snapshot.ledger!.version,requestKey:key});
   expect(preview.valid).toBe(true);
   const request={operation:deposit,expectedLedgerVersion:snapshot.ledger!.version,requestKey:key,previewHash:preview.previewHash};
   const first=await api.saveInvestmentOperation(request),retry=await api.saveInvestmentOperation(request);expect(retry.operation!.id).toBe(first.operation!.id);
   const current=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;
   const buy={...deposit,kind:"buy",instrumentKey:"FULLSTACK-STOCK",quantity:"10",amount:"100",occurredAt:stamp("2024-01-03T12:00:00Z"),settlementAt:stamp("2024-01-03T12:00:00Z")};
   const buyKey=randomUUID(),buyPreview=await api.previewInvestmentOperation({action:"create",operation:buy,expectedLedgerVersion:current.ledger!.version,requestKey:buyKey});expect(buyPreview.valid).toBe(true);
   await api.saveInvestmentOperation({operation:buy,expectedLedgerVersion:current.ledger!.version,requestKey:buyKey,previewHash:buyPreview.previewHash});
  }
  const currentMonth=new Date().toISOString().slice(0,7),payment=currentMonth+"-01";
  await page.goto(`/investments/forecast?month=${currentMonth}&currency=RUB&ledger=${ledger.id}`);
  await expect(page.getByRole("heading",{name:ru?"Прогноз выплат":"Payout forecast",exact:true})).toBeVisible();
  if(phase==="write"){
   await page.getByRole("button",{name:ru?"Добавить событие":"Add event",exact:true}).click();const dialog=page.getByRole("dialog");
   for(const [label,value] of [[ru?"Идентификатор бумаги из журнала/карточки":"Instrument identifier from journal/card","FULLSTACK-STOCK"],[ru?"Сумма на одну бумагу":"Amount per unit","2.123456789012"],[ru?"Ссылка или документ источника":"Source link or document","Disposable synthetic issuer"],[ru?"Подтверждение суммы и правила права":"Reviewed amount and entitlement terms","Synthetic entitlement after historical settlement"],[ru?"Причина сохранения / сверки":"Reason for saving / reconciliation","Fullstack reviewed fixture"]])await dialog.getByRole("textbox",{name:label,exact:false}).fill(value);
   await dialog.getByLabel(ru?"Ожидаемая дата выплаты":"Expected payment date").fill(payment);
   await dialog.getByRole("combobox",{name:ru?"Правило права":"Entitlement rule",exact:true}).selectOption("settled_by_cutoff");
   await dialog.getByRole("textbox",{name:ru?"Точный момент отсечки":"Exact entitlement cutoff",exact:false}).fill("2024-01-10T18:00:00.123456+03:00");
   await dialog.getByRole("button",{name:ru?"Сохранить событие":"Save event",exact:true}).click();await expect(dialog).toHaveCount(0);
   const forecast=await api.getInvestmentForecast({fromMonth:currentMonth,currency:"RUB",ledgerIds:[ledger.id]});
   expect(forecast.rows[0].gross?.decimal).toBe("21.23456789012");expect(forecast.rows[0].quantity?.decimal).toBe("10");expect(forecast.events[0].entitlementAt?.nanos).toBe(123456000);
   const snapshot=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;
   const dividend={ledgerId:ledger.id,kind:"dividend",instrumentKey:"FULLSTACK-STOCK",currency:"RUB",quantity:"0",amount:"21.234567890120",tax:"1.234567890120",occurredAt:stamp(payment+"T12:00:00Z"),settlementAt:stamp(payment+"T12:00:00Z")};
   const key=randomUUID(),preview=await api.previewInvestmentOperation({action:"create",operation:dividend,expectedLedgerVersion:snapshot.ledger!.version,requestKey:key});expect(preview.valid).toBe(true);
   const actual=(await api.saveInvestmentOperation({operation:dividend,expectedLedgerVersion:snapshot.ledger!.version,requestKey:key,previewHash:preview.previewHash})).operation!;
   await page.getByRole("button",{name:ru?"Сверить зачисление":"Reconcile credit",exact:true}).click();const link=page.getByRole("dialog");
   await link.getByRole("combobox",{name:ru?"Запись фактической выплаты":"Actual payout entry",exact:true}).selectOption(actual.id);
   await link.getByRole("checkbox",{name:ru?"Подтверждаю полную сверку выплаты по событию":"I confirm full reconciliation of this event payout"}).check();
   await link.getByRole("textbox",{name:ru?"Причина сохранения / сверки":"Reason for saving / reconciliation"}).fill("Disposable fullstack payout reconciled");
   await link.getByRole("button",{name:ru?"Сохранить сверку":"Save reconciliation",exact:true}).click();await expect(link).toHaveCount(0);
  }
  const calendar=page.getByRole("region",{name:ru?"Календарь событий":"Event calendar"});
  await expect(calendar).toContainText(ru?"Получено и сверено":"Received and reconciled");await expect(calendar).toContainText("20 RUB");
  const report=await api.getInvestmentForecast({fromMonth:currentMonth,currency:"RUB",ledgerIds:[ledger.id]});expect(report.rows[0].status).toBe("paid");expect(report.links).toHaveLength(1);
  if(phase==="readonly"){
   await expect(page.getByRole("button",{name:ru?"Добавить событие":"Add event",exact:true})).toHaveCount(0);
   await expect(page.getByRole("button",{name:ru?"Проверить связь":"Review link",exact:true})).toHaveCount(0);
   const event=report.events[0],{ $typeName,...value}=event;
   await expect(api.saveInvestmentForecastEvent({event:{...value,perShare:"9",reason:"Must be denied"},expectedVersion:event.version})).rejects.toMatchObject({code:Code.FailedPrecondition});
   const { $typeName:linkType,...link}=report.links[0];
   await expect(api.saveInvestmentForecastLink({link:{...link,cancelled:true,reason:"Must be denied"},expectedVersion:link.version})).rejects.toMatchObject({code:Code.FailedPrecondition});
   const after=await api.getInvestmentForecast({fromMonth:currentMonth,currency:"RUB",ledgerIds:[ledger.id]});expect(after.events[0].perShare).toBe("2.123456789012");expect(after.links[0].cancelled).toBe(false);
  }
  expect(report.rows[0].actualNet?.decimal).toBe("20");expect(report.events[0].entitlementAt?.nanos).toBe(123456000);
  const operations=(await api.listInvestmentOperations({ledgerId:ledger.id,limit:50})).operations;expect(operations).toHaveLength(3);
  const snapshot=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;expect(snapshot.positions[0].quantity).toBe("10");expect(snapshot.cash[0].settled).toBe("920.123456789012");
  // Persistent async export uses the actual backend worker and browser download.
  // A stable request key retrieves the same artifact after restart/read-only.
  const exportKey=`${runId.slice(0,8)}-${runId.slice(8,12)}-4${runId.slice(13,16)}-a${runId.slice(17,20)}-${runId.slice(20,30)}${ru?"00":"01"}`;
  const exportJob=(await api.startInvestmentExport({requestKey:exportKey})).job!;
  await expect.poll(async()=>(await api.getInvestmentExport({jobId:exportJob.id})).job?.state,{timeout:20000}).toBe("ready");
  await page.goto(`/investments/export?job=${exportJob.id}`);
  const downloadButton=page.getByRole("button",{name:ru?"Проверить и скачать":"Verify and download"});await expect(downloadButton).toBeVisible();
  const downloading=page.waitForEvent("download");await downloadButton.click();const downloaded=await downloading,path=await downloaded.path();expect(path).not.toBeNull();
  const archiveBytes=await readFile(path!),metadata=(await api.getInvestmentExport({jobId:exportJob.id})).job!;
  expect(createHash("sha256").update(archiveBytes).digest("hex")).toBe(metadata.sha256);
  const bundle=JSON.parse(archiveBytes.toString("utf8"));expect(bundle.Format).toBe("budget.investments.postgres.v1");expect(bundle.Data.TenantID).toBe(tenantId);expect(bundle.Data.Tables.find((v:any)=>v.Name==="investment_operations").Rows).toHaveLength(3);expect(bundle.Data.Excluded.investment_export_jobs).toBeTruthy();
  await page.reload();await expect(downloadButton).toBeVisible();
  await page.goto(`/investments/forecast?month=${currentMonth}&currency=RUB&ledger=${ledger.id}`);
  await page.reload();await expect(calendar).toContainText("20 RUB");await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 });
}
