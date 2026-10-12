import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";

for(const locale of ["ru","en"])test(`${locale} distribution market value requires review and survives editing`,async({page,context})=>{
 const ru=locale==="ru",readonly=process.env.E2E_INVESTMENTS_READ_ONLY==="true",writes:any[]=[],checks:any[]=[];
 const clean=(v:any):any=>Array.isArray(v)?v.map(clean):v&&typeof v==="object"?Object.fromEntries(Object.entries(v).filter(([k])=>k!=="$typeName").map(([k,v])=>[k,clean(v)])):v;
 const stamp={seconds:1704153600n},ledger={id:"ledger",name:"Broker",version:2n,metadataVersion:1n,defaultCurrency:"USD",startedAt:{seconds:1704067200n}};
 let operation:any={id:"event",ledgerId:"ledger",kind:"distribution",currency:"USD",instrumentKey:"parent",amount:"0",quantity:"0",fee:"0",tax:"0",accrued:"0",occurredAt:stamp,settlementAt:stamp,version:1n,sequence:1n,status:"posted",note:"Reviewed corporate terms",distribution:{eventKey:"event",targetInstrumentKey:"child",newUnits:"1",oldUnits:"1",costNumerator:"1",costDenominator:"2",marketValue:"40",marketValueEvidence:"Original fair value evidence"}};
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant")});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));
  const raw=route.request().postDataBuffer(),input:any=method&&raw?fromBinary(method.input,raw.subarray(5,5+raw.readUInt32BE(1))):{};let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="ListInvestmentLedgers")data={ledgers:[ledger]};
  if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger,cash:[],positions:[]}};
  if(method?.name==="ListInvestmentOperations")data={operations:[operation]};
  if(method?.name==="PreviewInvestmentOperation"){
   checks.push(input);const terms=input.operation.distribution,valid=!!terms.marketValue===!!terms.marketValueEvidence;
   data={valid,previewHash:valid?"b".repeat(64):"",proposed:{...operation,...input.operation},checkedAt:stamp,dirtyFrom:stamp,issues:valid?[]:[{code:"invalid_operation"}]};
  }
  if(method?.name==="SaveInvestmentOperation"){writes.push(input);ledger.version++;operation={...operation,...input.operation,version:2n,status:"posted",sequence:1n};data={operation};}
  const body=method?Buffer.from(toBinary(method.output,create(method.output,clean(data)))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);
  await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
 });
 await page.goto("/investments/journal?ledger=ledger");const edit=page.getByRole("button",{name:ru?"Исправить":"Amend",exact:true});
 if(readonly){await expect(page.getByRole("heading",{level:1})).toBeVisible();await expect(edit).toHaveCount(0);expect(writes).toHaveLength(0);return}
 await edit.click();const dialog=page.getByRole("dialog"),value=dialog.getByLabel(ru?"Рыночная стоимость переноса (всего)":"Total market value transferred",{exact:true}),evidence=dialog.getByLabel(ru?"Основание рыночной оценки":"Market valuation evidence",{exact:true}),check=dialog.getByRole("button",{name:ru?"Проверить изменения":"Check changes",exact:true}),save=dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true});
 await expect(value).toHaveValue("40");await expect(evidence).toHaveValue("Original fair value evidence");
 await dialog.getByLabel(ru?"Причина исправления":"Reason for correction",{exact:true}).fill("Review fair value");
 await evidence.fill("");await check.click();await expect(save).toBeDisabled();
 await evidence.fill("Reviewed total at event time");await value.fill("9007199254740993.123456789012");await check.click();await expect(save).toBeEnabled();
 await value.fill("50");await expect(save).toBeDisabled();await check.click();await expect(save).toBeEnabled();
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await dialog.screenshot({path:`/private/tmp/budget127-distribution-${locale}.png`});await save.click();await expect(dialog).toHaveCount(0);
 expect(checks[1].operation.distribution.marketValue).toBe("9007199254740993.123456789012");expect(writes[0].operation.distribution).toMatchObject({marketValue:"50",marketValueEvidence:"Reviewed total at event time"});expect(writes[0].previewHash).toBe("b".repeat(64));
 await edit.click();await expect(value).toHaveValue("50");await expect(evidence).toHaveValue("Reviewed total at event time");
});
