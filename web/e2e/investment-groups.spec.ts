import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
for(const locale of ["ru","en"]){test(`${locale} account-specific instrument groups`,async({page,context})=>{
 test.skip(process.env.E2E_INVESTMENTS_READ_ONLY==="true"||process.env.E2E_INVESTMENTS_ENABLED==="false","Writable runtime required");
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const a="11111111-1111-4111-8111-111111111111",b="22222222-2222-4222-8222-222222222222",g="33333333-3333-4333-8333-333333333333";
 let classification:any={version:9007199254740993n,groups:[{id:g,name:"Strategy"}],memberships:[{ledgerId:a,instrumentKey:"ABC",groupId:g}]};const saves:any[]=[];
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="GetInvestmentGroups")data={classification,positions:[{ledgerId:a,ledgerName:"Ordinary",instrumentKey:"ABC"},{ledgerId:b,ledgerName:"IIS",instrumentKey:"ABC"}]};
  if(method?.name==="SaveInvestmentGroups"){const bytes=route.request().postDataBuffer()!;const input=fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any;saves.push(input);classification={...input.classification,version:input.classification.version+1n};data={classification};}
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);
  await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/groups");
 await expect(page.getByRole("heading",{name:locale==="ru"?"Группы инструментов":"Instrument groups",exact:true})).toBeVisible();
 await expect(page.getByLabel("Ordinary: ABC")).toHaveValue(g);await expect(page.getByLabel("IIS: ABC")).toHaveValue("");await page.getByLabel("IIS: ABC").selectOption(g);
 const reason=page.getByLabel(locale==="ru"?"Причина изменения":"Reason for change"),save=page.getByRole("button",{name:locale==="ru"?"Сохранить классификацию":"Save classification"});
 await reason.fill("Reviewed classification");await save.click();await expect.poll(()=>saves.length).toBe(1);expect(saves[0].classification.version).toBe(9007199254740993n);expect(saves[0].classification.memberships).toHaveLength(2);
 await expect(reason).toHaveValue("");await expect(page.getByLabel("IIS: ABC")).toHaveValue(g);
 await page.getByRole("button",{name:locale==="ru"?"Удалить группу":"Remove group"}).click();await expect(page.getByLabel("Ordinary: ABC")).toHaveValue("");await expect(page.getByLabel("IIS: ABC")).toHaveValue("");
 await reason.fill("Remove group, preserve history");await save.click();await expect.poll(()=>saves.length).toBe(2);expect(saves[1].classification.groups).toHaveLength(0);expect(saves[1].classification.memberships).toHaveLength(0);
 await expect(page.getByLabel("Ordinary: ABC")).toBeVisible();await expect(page.getByLabel("IIS: ABC")).toBeVisible();
});}
