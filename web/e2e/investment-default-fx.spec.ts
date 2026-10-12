import {test,expect} from "@playwright/test";
import {create,toBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";

for(const locale of ["ru","en"])test(`${locale} new portfolios default to market FX and keep explicit CBR edits`,async({page,context})=>{
 const ru=locale==="ru",readonly=process.env.E2E_INVESTMENTS_READ_ONLY==="true";let marketAvailable=true;const writes:string[]=[];
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant")});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};
  if(method&&!/^(Get|List)/.test(method.name))writes.push(method.name);
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers:[{id:"a",name:"Broker",version:1n}]};
  if(method?.name==="ListInvestmentPortfolios")data={portfolios:[{id:"saved",name:"Explicit CBR",ledgerIds:["a"],currency:"RUB",fxPolicyKey:"first-policy",conventionKey:"prices",version:1n,fromDate:"2024-01-01",toDate:"2024-01-02"}]};
  if(method?.name==="ListInvestmentValuationProfiles")data={priceProfiles:[{key:"prices",title:"Prices",version:1n}],fxProfiles:[{key:"first-policy",title:"Official",mode:"cbr",version:1n},...(marketAvailable?[{key:"custom-name",title:"Exchange",mode:"market",version:1n}]:[])]};
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
 });
 await page.goto("/investments?newAccount=a");
 const createButton=page.getByRole("button",{name:ru?"Создать портфель":"Create portfolio",exact:true}),fx=page.getByRole("combobox",{name:ru?"Курсы валют":"Exchange rates",exact:true});
 if(readonly){await expect(createButton).toBeDisabled();await expect(fx).toHaveCount(0);expect(writes).toEqual([]);return}
 await expect(fx).toHaveValue("custom-name");await fx.selectOption("first-policy");await expect(fx).toHaveValue("first-policy");
 await page.getByRole("button",{name:ru?"Отмена":"Cancel",exact:true}).click();await page.getByRole("button",{name:ru?"Изменить":"Edit",exact:true}).click();await expect(fx).toHaveValue("first-policy");
 marketAvailable=false;await page.reload();await expect(fx).toHaveValue("");await expect(fx.locator("option[value='first-policy']")).toHaveCount(1);expect(writes).toEqual([]);
});
