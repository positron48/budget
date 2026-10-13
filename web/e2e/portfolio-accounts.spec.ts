import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])test(`${locale} account settings only require ordinary account details`,async({page,context})=>{
 const ru=locale==="ru",readOnly=process.env.E2E_INVESTMENTS_READ_ONLY==="true";
 let saved=0;const ledger={id:"account",name:"Broker",broker:"Example",accountType:"iis",defaultCurrency:"RUB",version:1n,metadataVersion:1n};
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));
  const bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers:[ledger]};
  if(method?.name==="UpdateInvestmentLedger"){
   expect(input.expectedLedgerVersion).toBe(1n);expect(input.expectedMetadataVersion).toBe(1n);expect(input.action).toBe("edit");
   ledger.name=input.name;ledger.accountType=input.accountType;ledger.version++;ledger.metadataVersion++;saved++;data={ledger};
  }
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),head=Buffer.alloc(5);head.writeUInt32BE(body.length,1);
  const trailer=Buffer.from("grpc-status: 0\r\n"),end=Buffer.alloc(5);end[0]=128;end.writeUInt32BE(trailer.length,1);
  await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,body,end,trailer])});
 });
 await page.goto("/investments/accounts");await expect(page.getByRole("heading",{name:ru?"Мои счета":"My accounts",exact:true})).toBeVisible();
 await expect(page.getByRole("link",{name:ru?"К портфелю":"Back to portfolio",exact:true})).toHaveAttribute("href","/investments");
 const edit=page.getByRole("button",{name:ru?"Изменить":"Edit",exact:true});
 if(readOnly)await expect(edit).toHaveCount(0);
 else{
  await edit.click();const dialog=page.getByRole("dialog");await expect(dialog.locator('input[type="date"]')).toHaveCount(0);
  await dialog.getByRole("textbox",{name:ru?"Название":"Name",exact:true}).fill("Updated account");
  await dialog.getByRole("combobox",{name:ru?"Тип счёта":"Account type",exact:true}).selectOption("regular");
  await dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true}).click();await expect(dialog).toHaveCount(0);await expect(page.getByRole("article")).toContainText("Updated account");expect(saved).toBe(1);
 }
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
