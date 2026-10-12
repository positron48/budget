import {test,expect} from "@playwright/test";

test("disabled investment routes expose no investment RPCs",async({page})=>{
 test.skip(process.env.E2E_INVESTMENTS_ENABLED!=="false","Runs with the module disabled");
 const requests:string[]=[];
 page.on("request",request=>{if(/budget\.v1\.(Investment|MarketData)/.test(request.url()))requests.push(request.url());});
 for(const route of ["/investments","/investments/journal","/investments/market","/investments/groups","/investments/payouts","/investments/operations","/investments/accounts","/investments/value","/investments/analytics","/investments/import","/investments/instruments/test-instrument"]){
  const response=await page.goto(route);
  expect(response?.status()).toBe(404);
  await expect(page.locator('a[href="/investments"]')).toHaveCount(0);
 }
 expect(requests).toEqual([]);
});
