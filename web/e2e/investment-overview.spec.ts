import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
const amount=(n:string,d="1")=>({numerator:n,denominator:d,decimal:String(Number(n)/Number(d))});
for(const locale of ["ru","en"])test(`${locale} overview keeps full totals, partial cash and stale gaps distinct`,async({page,context})=>{
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const base={ledgerIds:["ordinary","iis"],currency:"USD",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-02"};
 const calls:{method:string,input:any}[]=[];const now=BigInt(Math.floor(Date.now()/1000));
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService,InvestmentImportService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));
  const bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  if(method)calls.push({method:method.name,input});let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Overview"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:["ready","partial","stale"].map(key=>({...base,key}))};
  if(method?.name==="GetInvestmentNAVSeries")data={series:{...base,key:input.seriesKey}};
  if(method?.name==="GetInvestmentOverview"){
   const ready=input.seriesKey==="ready",partial=input.seriesKey==="partial";
   data={seriesKey:input.seriesKey,scope:"full_registered_accounts",readAt:{seconds:now},nav:{date:input.toDate,state:ready?"ready":partial?"incomplete":"stale",knownValue:ready?amount("519","10"):partial?amount("227","5"):amount("999"),computedAt:{seconds:now-10n}},performance:{currency:"USD",profit:ready?amount("-509","10"):undefined,reason:ready?"":"terminal_nav_unknown",twr:ready?amount("-1","2"):undefined,twrReason:ready?"":"discontinuous_history",xirr:ready?0:undefined,xirrReason:ready?"":"terminal_nav_unknown"},securitiesKnown:ready?amount("13","2"):partial?amount("0"):undefined,securitiesComplete:ready,cashKnown:ready||partial?amount("227","5"):undefined,cashComplete:ready||partial,dailyProfit:ready?amount("-509","10"):undefined,dailyReason:ready?"":"terminal_nav_unknown",fx:[{fromCurrency:"USD",toCurrency:"RUB",rate:amount("90"),change:amount("1","100"),state:"fresh",mode:"market",source:"Verified provider",asOf:{seconds:now-3600n}},{fromCurrency:"EUR",toCurrency:"RUB",rate:amount("100"),state:"expired",mode:"market",source:"Verified provider",asOf:{seconds:now-864000n}}]};
  }
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);
  const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments?series=ready&from=2024-01-02&to=2024-01-02&group=selected");
 await expect(page.getByTestId("overview-nav")).toContainText("51.9 USD");await expect(page.getByTestId("overview-cash")).toContainText("45.4 USD");await expect(page.getByTestId("overview-securities")).toContainText("6.5 USD");
 await expect(page.getByTestId("overview-profit")).toContainText("-50.9 USD");await expect(page.getByTestId("overview-daily")).toContainText("-50.9 USD");
 await expect(page.getByTestId("overview-twr")).toContainText(locale==="ru"?"-50,00%":"-50.00%");await expect(page.getByTestId("overview-xirr")).toContainText(locale==="ru"?/0,00\s*%/:"0.00%");
 await expect(page.getByText(locale==="ru"?"Срок действия истёк":"Fixing expired",{exact:true})).toBeVisible();
 await expect.poll(()=>calls.some(c=>c.method==="GetInvestmentOverview"&&c.input.fromDate==="2024-01-02"&&c.input.toDate==="2024-01-02")).toBe(true);
 const selector=page.getByLabel(locale==="ru"?"Область счетов, валюта и FX":"Account scope, currency and FX",{exact:true});
 await selector.selectOption("partial");await expect(page.getByTestId("overview-nav")).toContainText("45.4 USD");await expect(page.getByTestId("overview-nav")).toContainText(locale==="ru"?"Известная часть":"Known subtotal");await expect(page.getByTestId("overview-cash")).toContainText("45.4 USD");await expect(page.getByTestId("overview-profit")).not.toContainText("USD");
 await selector.selectOption("stale");await expect(page.getByTestId("overview-nav")).not.toContainText("USD");await expect(page.getByTestId("overview-nav")).not.toContainText("999");await expect(page.getByTestId("overview-cash")).not.toContainText("USD");
 await selector.selectOption("ready");await expect(page.getByTestId("overview-nav")).toContainText("51.9 USD");
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:`/private/tmp/budget-investment-overview-${locale}.png`,fullPage:true});
 const details=page.getByRole("link",{name:locale==="ru"?"Позиции, графики и состав прибыли":"Positions, charts and profit composition",exact:true});
 await expect(details).toHaveAttribute("href",/investments\/value.*group=selected/);
 await details.click();await expect(page).toHaveURL(/investments\/value/);
 await page.getByRole("link",{name:locale==="ru"?"К обзору портфеля":"Back to portfolio overview",exact:true}).click();
 await expect(page).toHaveURL(/investments\?series=ready.*group=selected/);await expect(page.getByTestId("overview-nav")).toContainText("51.9 USD");
 if(process.env.E2E_INVESTMENTS_READ_ONLY==="true")expect(calls.filter(c=>!/^(Get|List)/.test(c.method))).toEqual([]);
});
