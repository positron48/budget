import {test,expect} from "@playwright/test";

for(const locale of ["ru","en"]){
 test(`${locale} denied investment tenant does not load page data`,async({page,context})=>{
  await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
  await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture-token");localStorage.setItem("budget/tenant","excluded-tenant");});
  const investmentCalls:string[]=[];
  await page.route("**/budget.v1.*/*",async route=>{
   const path=new URL(route.request().url()).pathname;
   const investment=/budget\.v1\.(Investment|MarketData)/.test(path);
   if(investment)investmentCalls.push(path);
   const trailer=Buffer.from(`grpc-status: ${investment?7:0}\r\n`);
   const header=Buffer.alloc(5);header[0]=128;header.writeUInt32BE(trailer.length,1);
   await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([Buffer.alloc(5),header,trailer])});
  });
  for(const target of ["/investments","/investments/groups","/investments/instruments","/investments/instruments/S"]){
  await page.goto(target);
  await expect(page.getByRole("button",{name:locale==="ru"?"Повторить проверку доступа":"Retry access check",exact:true})).toBeVisible();
  await expect(page.locator('a[href="/investments"]')).toHaveCount(0);
  await expect(page.locator('input[type="file"]')).toHaveCount(0);
  }
  expect(investmentCalls.length).toBeGreaterThan(0);
  expect(investmentCalls.every(path=>path.endsWith("/InvestmentService/ListInvestmentLedgers")||path.endsWith(".InvestmentService/ListInvestmentLedgers"))).toBe(true);
 });
}
