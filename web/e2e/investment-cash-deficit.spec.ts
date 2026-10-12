import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])test(`${locale} imported cash exception preserves reason and invalidates preview`,async({page,context})=>{
 const ru=locale==="ru",readonly=process.env.E2E_INVESTMENTS_READ_ONLY==="true",writes:any[]=[],checks:any[]=[];
 const clean=(v:any):any=>Array.isArray(v)?v.map(clean):v&&typeof v==="object"?Object.fromEntries(Object.entries(v).filter(([k])=>k!=="$typeName").map(([k,v])=>[k,clean(v)])):v;
 const ledger={id:"ledger",name:"Broker",version:2n,metadataVersion:1n,defaultCurrency:"USD"},stamp={seconds:1704153600n},cash={currency:"USD",settled:"-100",economic:"-100",receivable:"0",payable:"0"};
 let operation:any={id:"imported",ledgerId:"ledger",kind:"withdrawal",currency:"USD",amount:"100",quantity:"0",fee:"0",tax:"0",accrued:"0",occurredAt:stamp,settlementAt:stamp,version:1n,sequence:1n,status:"posted",importedCashDeficitReason:"Original missing funding",source:{importId:"import",record:1,expectedVersion:1n,reviewReason:"Reviewed source"}};
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant")});
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[InvestmentService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};if(method?.name==="ListInvestmentLedgers")data={ledgers:[ledger]};if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger,cash:[cash],issues:["negative_imported_cash","imported_cash_history_incomplete"],positions:[]}};if(method?.name==="ListInvestmentOperations")data={operations:[operation]};
  if(method?.name==="PreviewInvestmentOperation"){
   checks.push(input);const valid=!!input.operation.importedCashDeficitReason;
   data={valid,previewHash:valid?"a".repeat(64):"",proposed:{...operation,...input.operation},checkedAt:stamp,dirtyFrom:stamp,issues:valid?[]:[{code:"negative_cash",cash}],comparisons:valid?[{stage:"current",asOf:stamp,before:{ledger,cash:[cash]},after:{ledger,cash:[cash],issues:["negative_imported_cash","imported_cash_history_incomplete","imported_cash_deficit_reviewed"]}}]:[]};
  }
  if(method?.name==="SaveInvestmentOperation"){writes.push(input);ledger.version++;operation={...operation,...input.operation,id:"imported",status:"posted",sequence:1n,version:2n,source:operation.source};data={operation};}
  const body=method?Buffer.from(toBinary(method.output,create(method.output,clean(data)))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/journal?ledger=ledger");
 const edit=page.getByRole("button",{name:ru?"Исправить":"Amend",exact:true});
 if(readonly){await expect(page.getByRole("heading",{level:1})).toBeVisible();await expect(edit).toHaveCount(0);expect(writes).toHaveLength(0);return}
 await page.getByRole("button",{name:ru?"Добавить операцию":"Add operation",exact:true}).click();await expect(page.getByRole("dialog").getByRole("textbox",{name:ru?"Причина исключения отрицательного cash":"Reason for cash deficit exception",exact:true})).toHaveCount(0);await page.getByRole("dialog").getByRole("button",{name:ru?"Закрыть":"Close",exact:true}).last().click();
 await edit.click();const dialog=page.getByRole("dialog"),reason=dialog.getByRole("textbox",{name:ru?"Причина исключения отрицательного cash":"Reason for cash deficit exception",exact:true}),check=dialog.getByRole("button",{name:ru?"Проверить изменения":"Check changes",exact:true}),save=dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true});
 await expect(reason).toHaveValue("Original missing funding");await dialog.getByRole("textbox",{name:ru?"Причина исправления":"Reason for correction",exact:true}).fill("Review source history");await reason.fill("");await check.click();await expect(save).toBeDisabled();await expect(dialog.getByRole("alert").first()).toBeVisible();
 await reason.fill("Missing historical funding confirmed");await check.click();await expect(save).toBeEnabled();await expect(dialog).toContainText(ru?"Это исключение истории":"This is a history exception");await reason.fill("Corrected missing funding evidence");await expect(save).toBeDisabled();await check.click();
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await save.click();await expect(dialog).toHaveCount(0);expect(writes[0].operation.importedCashDeficitReason).toBe("Corrected missing funding evidence");expect(writes[0].previewHash).toBe("a".repeat(64));expect(checks).toHaveLength(3);
 await edit.click();await expect(page.getByRole("dialog").getByRole("textbox",{name:ru?"Причина исключения отрицательного cash":"Reason for cash deficit exception",exact:true})).toHaveValue("Corrected missing funding evidence");
});
