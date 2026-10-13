// Actual local browser -> Envoy -> API -> PostgreSQL, with disposable users.
import {test,expect} from "@playwright/test";
import {createClient,Code} from "@connectrpc/connect";
import {createGrpcWebTransport} from "@connectrpc/connect-web";
import {randomUUID} from "node:crypto";
import {AuthService} from "../proto/budget/v1/auth_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
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
 const api=createClient(InvestmentService,transport);
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:baseURL}]);
 await page.addInitScript(({access,refresh,tenant})=>{localStorage.setItem("budget/access",access);localStorage.setItem("budget/refresh",refresh);localStorage.setItem("budget/tenant",tenant)},{access:tokens.accessToken,refresh:tokens.refreshToken,tenant:tenantId});
 if(phase==="disabled"){
  await expect(api.listInvestmentLedgers({})).rejects.toMatchObject({code:Code.Unavailable});
  expect((await page.goto("/investments"))?.status()).toBe(404);return;
 }
 let ledger=(await api.listInvestmentLedgers({})).ledgers[0];
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
 }
 const state=(await api.getInvestmentSnapshot({ledgerId:ledger.id})).snapshot!;
 expect(state.ledger!.portfolioTracking).toBe(true);expect(state.cash).toHaveLength(0);
 expect(state.positions[0].quantity).toBe("1");expect(state.positions[0].cleanCost).toBe("101");expect(state.positions[0].realizedClean).toBe("18");expect(state.positions[0].couponEarned).toBe("15");
 await page.goto("/investments");await expect(page.getByRole("heading",{name:ru?"Мой портфель":"My portfolio",exact:true})).toBeVisible();
 await expect(page.getByRole("table")).toContainText("TRACKER-STOCK");
 await page.getByRole("button",{name:ru?"Дивиденды и купоны":"Dividends and coupons",exact:true}).click();await expect(page.getByRole("table")).toContainText("15");
 if(phase==="readonly"){
  await expect(page.getByRole("button",{name:ru?"Добавить операцию":"Add operation",exact:true})).toHaveCount(0);
  await expect(api.createInvestmentLedger({name:"Denied",requestKey:randomUUID()})).rejects.toMatchObject({code:Code.FailedPrecondition});
 }
 await page.reload();await expect(page.getByRole("heading",{name:ru?"Мой портфель":"My portfolio",exact:true})).toBeVisible();
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
