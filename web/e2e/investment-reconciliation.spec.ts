import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])test(`${locale} cash reconciliation preserves observation and preview`,async({page,context})=>{
 const ru=locale==="ru",readOnly=process.env.E2E_INVESTMENTS_READ_ONLY==="true",writes:any[]=[];let operation:any,checks=0;
 const ledger={id:"ledger",name:"Broker",version:2n,metadataVersion:1n,defaultCurrency:"USD"},stamp={seconds:1704067200n},cash=(value:string)=>({currency:"USD",settled:value,economic:value,receivable:"0",payable:"0"});
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant")});
 await page.route("**/budget.v1.*/*",async route=>{
 const path=new URL(route.request().url()).pathname,service=[InvestmentService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};let data:any={};
 if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};if(method?.name==="ListInvestmentLedgers")data={ledgers:[ledger]};
 if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger,cash:[cash(operation?"99":"100.123456789012")],positions:[]}};
 if(method?.name==="ListInvestmentOperations")data={operations:operation?[operation]:[]};
 if(method?.name==="PreviewInvestmentOperation"){
 checks++;expect(input.operation.kind).toBe("cash_reconciliation");expect(input.operation.amount).toBe("0");expect(input.operation.cashReconciliation).toMatchObject({expectedSettled:"100.123456789012",observedSettled:"99"});
 data={valid:true,previewHash:"a".repeat(64),checkedAt:stamp,dirtyFrom:stamp,comparisons:[{stage:"operation",asOf:stamp,before:{ledger,cash:[cash("100.123456789012")]},after:{ledger,cash:[cash("99")],issues:["cash_reconciliation_unclassified"]},cashChange:[cash("-1.123456789012")]}]};
 }
 if(method?.name==="SaveInvestmentOperation"){
 writes.push(input);ledger.version++;operation={...input.operation,id:"reconcile",version:BigInt(writes.length),sequence:2n,amount:"1.123456789012",status:"posted",settlementAt:input.operation.occurredAt};data={operation};
 }
 const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/journal?ledger=ledger");const add=page.getByRole("button",{name:ru?"Добавить операцию":"Add operation",exact:true});
 if(readOnly){await expect(page.getByRole("heading",{level:1})).toBeVisible();await expect(add).toHaveCount(0);expect(writes).toEqual([]);expect(checks).toBe(0);return}
 await add.click();let dialog=page.getByRole("dialog");await dialog.getByRole("combobox",{name:ru?"Тип операции":"Operation type",exact:true}).selectOption("cash_reconciliation");
 const expectedName=ru?"Расчётный зачисленный остаток на дату сверки":"Expected settled cash at reconciliation time",observedName=ru?"Фактический зачисленный остаток по отчёту":"Observed settled cash from statement";
 await dialog.getByRole("textbox",{name:expectedName,exact:true}).fill("100.123456789012");await dialog.getByRole("textbox",{name:observedName,exact:true}).fill("99");await dialog.getByRole("textbox",{name:ru?"Заметка":"Note",exact:true}).fill("Statement USD settled balance");
 await expect(dialog.getByRole("textbox",{name:ru?"Сумма / чистый номинал сделки":"Amount / clean trade principal",exact:true})).toHaveCount(0);
 const check=()=>dialog.getByRole("button",{name:ru?"Проверить изменения":"Check changes",exact:true}),save=()=>dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true});await expect(save()).toBeDisabled();await check().click();await expect(dialog).toContainText("-1.123456789012");await expect(dialog).toContainText(ru?"Разница денежной сверки не классифицирована":"Cash reconciliation difference is unclassified");await save().click();await expect(dialog).toHaveCount(0);
 expect(writes[0].previewHash).toBe("a".repeat(64));expect(writes[0].expectedLedgerVersion).toBe(2n);
 await page.getByRole("button",{name:ru?"Исправить":"Amend",exact:true}).click();dialog=page.getByRole("dialog");await expect(dialog.getByRole("textbox",{name:expectedName,exact:true})).toHaveValue("100.123456789012");await expect(dialog.getByRole("textbox",{name:observedName,exact:true})).toHaveValue("99");await dialog.getByRole("textbox",{name:ru?"Причина исправления":"Reason for correction",exact:true}).fill("Clarify evidence");await expect(save()).toBeDisabled();await check().click();await save().click();await expect(dialog).toHaveCount(0);expect(writes[1].expectedOperationVersion).toBe(1n);expect(writes[1].expectedLedgerVersion).toBe(3n);
});
