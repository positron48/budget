import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])test(`${locale} civil dates preserve precision through create and amend`,async({page,context})=>{
 const ru=locale==="ru",readOnly=process.env.E2E_INVESTMENTS_READ_ONLY==="true",writes:any[]=[];let operation:any,checks=0;
 const ledger={id:"ledger",name:"Broker",version:2n,metadataVersion:1n,defaultCurrency:"USD"};
 const stamp=(text:string)=>({seconds:BigInt(Date.parse(text)/1000),nanos:0});
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant")});
 await page.route("**/budget.v1.*/*",async route=>{
 const path=new URL(route.request().url()).pathname,service=[InvestmentService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};let data:any={};
 if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};if(method?.name==="ListInvestmentLedgers")data={ledgers:[ledger]};
 if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger,cash:[{currency:"USD",settled:"100",receivable:"0",payable:"0",economic:"100"}],positions:[]}};
 if(method?.name==="ListInvestmentOperations")data={operations:operation?[operation]:[]};
 if(method?.name==="PreviewInvestmentOperation"){
 checks++;expect(input.operation.timing.timezone).toBe("America/New_York");expect(input.operation.timing.settlementDate).toBe("2024-03-11");expect(input.operation.settlementAt).toBeUndefined();if(!writes.length){expect(input.operation.timing.occurredDate).toBe("2024-03-10");expect(input.operation.occurredAt).toBeUndefined()}else{expect(input.operation.timing.occurredDate).toBe("");expect(input.operation.occurredAt).toBeDefined()}
 data={valid:true,previewHash:"a".repeat(64),checkedAt:stamp("2024-03-12T00:00:00Z"),dirtyFrom:stamp("2024-03-10T05:00:00Z"),comparisons:[],proposed:input.operation};
 }
 if(method?.name==="SaveInvestmentOperation"){
 writes.push(input);ledger.version++;const {$typeName:operationType,...terms}=input.operation;operation={...terms,id:"buy",version:BigInt(writes.length),sequence:2n,status:"posted",occurredAt:input.operation.occurredAt||stamp("2024-03-10T05:00:00Z"),settlementAt:{seconds:BigInt(Date.parse("2024-03-12T03:59:59Z")/1000),nanos:999999000}};data={operation};
 }
 const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/journal?ledger=ledger");const add=page.getByRole("button",{name:ru?"Добавить операцию":"Add operation",exact:true});if(readOnly){await expect(page.getByRole("heading",{level:1})).toBeVisible();await expect(add).toHaveCount(0);expect(checks).toBe(0);return}
 await add.click();let dialog=page.getByRole("dialog");await dialog.getByRole("combobox",{name:ru?"Тип операции":"Operation type",exact:true}).selectOption("buy");
 const occurrenceName=ru?"Известна только дата операции":"Only the operation date is known",settlementName=ru?"Известна только дата расчёта":"Only the settlement date is known";
 await dialog.getByRole("checkbox",{name:occurrenceName,exact:true}).check();await dialog.getByRole("checkbox",{name:settlementName,exact:true}).check();
 await dialog.locator('input[type="date"]').nth(0).fill("2024-03-10");await dialog.locator('input[type="date"]').nth(1).fill("2024-03-11");
 await dialog.getByRole("textbox",{name:ru?"Часовой пояс даты (IANA)":"Date timezone (IANA)",exact:true}).fill("America/New_York");
 await dialog.getByRole("textbox",{name:ru?"Бумага: ISIN или тикер + площадка":"Security: ISIN or ticker + venue",exact:true}).fill("stock");await dialog.getByRole("textbox",{name:ru?"Количество":"Quantity",exact:true}).fill("1");await dialog.getByRole("textbox",{name:ru?"Сумма / чистый номинал сделки":"Amount / clean trade principal",exact:true}).fill("10");
 const check=()=>dialog.getByRole("button",{name:ru?"Проверить изменения":"Check changes",exact:true}),save=()=>dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true});await expect(save()).toBeDisabled();await check().click();await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await dialog.getByText(ru?"Точность даты":"Date precision",{exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:`/private/tmp/budget-date-only-${locale}-mobile.png`,fullPage:true});await save().click();await expect(dialog).toHaveCount(0);
 await expect(page.locator('tbody').last()).toContainText(`2024-03-10 · ${ru?"Только дата":"Date only"} · America/New_York`);
 await page.getByRole("button",{name:ru?"Исправить":"Amend",exact:true}).click();dialog=page.getByRole("dialog");await expect(dialog.getByRole("checkbox",{name:occurrenceName,exact:true})).toBeChecked();await expect(dialog.locator('input[type="date"]').nth(0)).toHaveValue("2024-03-10");await expect(dialog.locator('input[type="date"]').nth(1)).toHaveValue("2024-03-11");
 await dialog.getByRole("checkbox",{name:occurrenceName,exact:true}).uncheck();await dialog.locator('input[type="datetime-local"]').fill("2024-03-10T12:00");await dialog.getByRole("textbox",{name:ru?"Причина исправления":"Reason for correction",exact:true}).fill("Verified broker execution time");await check().click();await save().click();await expect(dialog).toHaveCount(0);expect(writes).toHaveLength(2);expect(writes[1].expectedOperationVersion).toBe(1n);
});
