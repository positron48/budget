import {test,expect} from "@playwright/test";
import {create,toBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
for(const locale of ["ru","en"]){test(`${locale} read-only investment views`,async({page,context})=>{
 test.skip(process.env.E2E_INVESTMENTS_READ_ONLY!=="true","Read-only runtime required");
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const writes:string[]=[];
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname;
  const service=[TenantService,InvestmentService,InvestmentImportService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/"));
  const method=service?.methods.find(m=>path.endsWith("/"+m.name));
  let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner tenant"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers:[{id:"ledger",name:"Broker",version:1n}]};
  if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger:{id:"ledger",name:"Broker",version:1n}}};
  if(method?.name==="GetInvestmentGroups")data={classification:{version:1n,groups:[{id:"group",name:"Strategy"}],memberships:[]},positions:[{ledgerId:"ledger",ledgerName:"Broker",instrumentKey:"ABC"}]};
  if(method&&method.name!=="QueryInvestmentOperations"&&!/^(Get|List|Search)/.test(method.name))writes.push(method.name);
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0);
  const header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);
  const trailer=Buffer.from("grpc-status: 0\r\n");const th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);
  await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 for(const path of ["/investments","/investments/journal","/investments/groups","/investments/payouts","/investments/operations","/investments/accounts","/investments/value","/investments/market"]){
  await page.goto(path);
  await expect(page.getByText(locale==="ru"?"Инвестиции работают только для чтения. Создание и изменение данных временно отключены.":"Investments are read-only. Creating and changing data is temporarily disabled.",{exact:true})).toBeVisible();
  await expect(page.locator('input[type="file"]')).toHaveCount(0);
  if(path==="/investments/journal"){await page.getByRole("button",{name:"Broker",exact:true}).click();await expect(page.getByRole("button",{name:locale==="ru"?"Перевод между счетами":"Transfer between accounts",exact:true})).toHaveCount(0);await expect(page.getByRole("button",{name:locale==="ru"?"Связать с накоплениями":"Link to savings",exact:true})).toHaveCount(0);}
  if(path==="/investments/groups"){await expect(page.getByLabel("Broker: ABC")).toBeDisabled();await expect(page.getByRole("button",{name:locale==="ru"?"Сохранить классификацию":"Save classification"})).toHaveCount(0);}
  if(path==="/investments/value")await expect(page.getByRole("button",{name:locale==="ru"?"Создать портфель":"Create portfolio",exact:true})).toBeDisabled();
 }
 await page.goto("/investments/journal?ledger=ledger&cashTransfer=new");
 await expect(page.getByRole("dialog")).toHaveCount(0);
 await expect(page.getByRole("button",{name:locale==="ru"?"Перевод с обычным счётом":"Transfer with ordinary account",exact:true})).toHaveCount(0);
 expect(writes).toEqual([]);
});}
