import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";

for(const locale of ["ru","en"]){
 test(`${locale} private archive job survives reload and downloads verified chunks`,async({page,context})=>{
  await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
  const id="11111111-1111-4111-8111-111111111111",bytes=Buffer.from(JSON.stringify({Format:"budget.investments.postgres.v1",exact:"9007199254740993.123456789012",synthetic:"x".repeat(300000)})),sha=createHash("sha256").update(bytes).digest("hex");
  let state="pending",starts=0,gets=0,corrupt=false;const offsets:bigint[]=[];
  await page.route("**/budget.v1.*/*",async route=>{
   const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));const inputBytes=route.request().postDataBuffer()!,input:any=method?fromBinary(method.input,inputBytes.subarray(5,5+inputBytes.readUInt32BE(1))):{};let data:any={};
   const job=()=>({id,state,sha256:sha,byteSize:BigInt(bytes.length),createdAt:{seconds:1704067200n},expiresAt:{seconds:BigInt(Math.floor(Date.now()/1000)+86400)}});
   if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
   if(method?.name==="ListInvestmentLedgers")data={ledgers:[]};
   if(method?.name==="StartInvestmentExport"){starts++;expect(input.requestKey).toMatch(/^[a-f0-9-]{36}$/);data={job:job()};}
   if(method?.name==="GetInvestmentExport"){gets++;expect(input.jobId).toBe(id);data={job:job()};}
   if(method?.name==="DownloadInvestmentExport"){expect(input.jobId).toBe(id);offsets.push(input.offset);const part=bytes.subarray(Number(input.offset),Number(input.offset)+262144);data={data:corrupt?Buffer.from(part).fill(0):part,offset:input.offset,totalBytes:BigInt(bytes.length),sha256:sha};}
   const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
  });
  await page.goto("/investments/export");await page.getByRole("button",{name:locale==="ru"?"Подготовить новый архив":"Prepare a new archive"}).click();await expect(page).toHaveURL(new RegExp(`job=${id}`));await expect(page.getByRole("status").filter({hasText:locale==="ru"?"Ожидает обработки":"Waiting for processing"})).toBeVisible();
  state="ready";await page.reload();const download=page.getByRole("button",{name:locale==="ru"?"Проверить и скачать":"Verify and download"});await expect(download).toBeVisible();expect(starts).toBe(1);expect(gets).toBeGreaterThan(0);
  const event=page.waitForEvent("download");await download.click();const file=await event;expect(file.suggestedFilename()).toBe(`budget-investments-${id}.json`);const saved=await file.path();expect(saved).not.toBeNull();expect(await readFile(saved!)).toEqual(bytes);expect(offsets).toEqual([0n,262144n]);
  await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  corrupt=true;let unexpected=false;page.on("download",()=>{unexpected=true;});await download.click();await expect(page.locator("main").getByRole("alert")).toContainText(locale==="ru"?"Не удалось":"Could not");expect(unexpected).toBe(false);
  state="expired";await page.reload();await expect(page.getByRole("status").filter({hasText:locale==="ru"?"Срок скачивания истёк":"Download expired"})).toBeVisible();await expect(download).toHaveCount(0);
 });
 test(`${locale} member cannot start or download an archive`,async({page,context})=>{
  await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});let calls=0;
  await page.route("**/budget.v1.*/*",async route=>{
   const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};
   if(method?.name?.includes("Export"))calls++;
   if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Member"},role:3}]};
   const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
  });
  await page.goto("/investments/export?job=11111111-1111-4111-8111-111111111111");await expect(page.getByText(locale==="ru"?"Экспорт доступен владельцу и администратору организации.":"Export is available to organization owners and administrators.")).toBeVisible();await expect(page.getByRole("button",{name:locale==="ru"?"Подготовить новый архив":"Prepare a new archive"})).toHaveCount(0);expect(calls).toBe(0);
 });
}
