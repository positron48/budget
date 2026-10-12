import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {gzipSync} from "node:zlib";
import {createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
const raw=Buffer.from('date,nav_numerator,nav_denominator,nav_state,scope\n2024-01-02,519,10,ready,full_registered_accounts\n');
for(const locale of ["ru","en"])test(`${locale} CSV download verifies bytes and refuses corrupt or oversized replies`,async({page,context})=>{
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const series={key:"series",ledgerIds:["ordinary","iis"],currency:"USD",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-02"};
 let mode="ok";const calls:string[]=[];let exports=0;let downloads=0;page.on("download",()=>downloads++);
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService,InvestmentImportService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));
  const bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
  let data:any={},code=0;if(method)calls.push(method.name);
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Export"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:[series]};
  if(method?.name==="GetInvestmentNAVSeries")data={series};
  if(method?.name==="ExportInvestmentSeries"){
   exports++;expect(input).toMatchObject({seriesKey:"series",fromDate:"2024-01-02",toDate:"2024-01-02"});
   data={filename:"investments-USD-2024-01-02-2024-01-02.csv",encoding:"gzip",data:gzipSync(raw),sha256:mode==="corrupt"?"0".repeat(64):createHash("sha256").update(raw).digest("hex"),uncompressedSize:BigInt(raw.length),rowCount:1,generation:17n};
   if(mode==="size")data.uncompressedSize=BigInt(raw.length-1);
   if(mode==="limit")code=8;
  }
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);
  const trailer=Buffer.from(`grpc-status: ${code}\r\n`),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
 });
 const button=page.getByRole("button",{name:locale==="ru"?"Скачать дневной ряд CSV":"Download daily series CSV",exact:true});
 for(const url of ["/investments","/investments/value"]){
  await page.goto(url+"?series=series&from=2024-01-02&to=2024-01-02&group=selected");
  const event=page.waitForEvent("download");await button.click();const file=await event;
  expect(file.suggestedFilename()).toBe("investments-USD-2024-01-02-2024-01-02.csv");expect(await readFile((await file.path())!)).toEqual(raw);
  await expect(page.getByText(locale==="ru"?"CSV проверен и сохранён, дней: 1.":"CSV verified and saved: 1 days.",{exact:true})).toBeVisible();
 }
 for(const bad of ["corrupt","size","limit"]){mode=bad;await button.click();await expect(page.getByRole("alert").filter({hasText:locale==="ru"?(bad==="limit"?"Ряд слишком большой":"Не удалось подготовить"):bad==="limit"?"The series exceeds":"Could not prepare"})).toBeVisible();await expect(button).toBeEnabled();expect(downloads).toBe(2);}
 expect(exports).toBe(5);expect(calls.filter(c=>!/^(Get|List|ExportInvestmentSeries)/.test(c))).toEqual([]);
});
