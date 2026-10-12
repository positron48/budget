import {test,expect} from "@playwright/test";
import {create,fromBinary,toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
for(const locale of ["ru","en"])test(`${locale} historical lots require reviewed terms and financial preview`,async({page,context})=>{
 const ru=locale==="ru",readOnly=process.env.E2E_INVESTMENTS_READ_ONLY==="true",resolutions:any[]=[],checks:any[]=[],writes:any[]=[];
 let operation:any,conflict=true,mappingVersion=9007199254740993n;
 const ledger={id:"ledger",name:"Broker",version:2n,metadataVersion:1n,defaultCurrency:"USD"},stamp=(value:string)=>({seconds:BigInt(Date.parse(value)/1000),nanos:0});
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant")});
 await page.route("**/budget.v1.*/*",async route=>{
 const path=new URL(route.request().url()).pathname,service=[InvestmentService,TenantService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name)),bytes=route.request().postDataBuffer()!,input=method?fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};let data:any={},code=0;
 if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};if(method?.name==="ListInvestmentLedgers")data={ledgers:[ledger]};
 if(method?.name==="GetInvestmentSnapshot")data={snapshot:{ledger,cash:[{currency:"USD",settled:"1000",receivable:"0",payable:"0",economic:"1000"}],positions:[]}};
 if(method?.name==="ListInvestmentOperations")data={operations:operation?[operation]:[]};
 if(method?.name==="PreviewInvestmentLotQuantity"){
 resolutions.push(input);expect(input.occurredAt).toBeUndefined();expect(input.timing.timezone).toBe("UTC");
 if(input.instrumentKey==="missing")code=9;
 else {const newer=input.timing.occurredDate==="2024-01-01",size=newer?"100":mappingVersion===9007199254740993n?"10":"5";
 const quantity=input.lots==="9007199254740993.123456789"?"90071992547409931.23456789":size==="100"?"200":size==="10"?"20":"10";
 data={quantity,effectiveAt:stamp(input.timing.occurredDate+"T00:00:00Z"),conversion:{lots:input.lots,lotSize:size,termsId:`stock:${newer?1:0}`,mappingVersion,provider:"tinvest",accessScope:"personal",instrumentId:"11111111-1111-4111-8111-111111111111",evidence:"Reviewed synthetic exchange history",from:stamp(newer?"2024-01-01T00:00:00Z":"2020-01-01T00:00:00Z"),to:newer?undefined:stamp("2024-01-01T00:00:00Z")}};}
 }
 if(method?.name==="PreviewInvestmentOperation"){
 checks.push(input);if(input.operation.lotConversion){expect(input.operation.quantity).toBe("0");expect(input.operation.lotConversion.mappingVersion).toBe(mappingVersion);expect(input.operation.lotConversion.lots).toBe("2")}
 data={valid:true,previewHash:"a".repeat(64),checkedAt:stamp("2024-01-02T00:00:00Z"),dirtyFrom:stamp("2023-12-31T00:00:00Z"),proposed:input.operation,comparisons:[]};
 }
 if(method?.name==="SaveInvestmentOperation"){
 writes.push(input);if(conflict){code=10;conflict=false;mappingVersion++}else{ledger.version++;const {$typeName:unused,...terms}=input.operation;operation={...terms,id:"buy",version:BigInt(writes.length-1),sequence:2n,status:"posted",quantity:input.operation.lotConversion?"10":input.operation.quantity,occurredAt:stamp(input.operation.timing.occurredDate+"T00:00:00Z"),settlementAt:stamp(input.operation.timing.occurredDate+"T00:00:00Z"),settlementAssumed:true};data={operation};}
 }
 const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from(`grpc-status: ${code}\r\n`),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto("/investments/journal?ledger=ledger");const add=page.getByRole("button",{name:ru?"Добавить операцию":"Add operation",exact:true});if(readOnly){await expect(page.getByRole("heading",{level:1})).toBeVisible();await expect(add).toHaveCount(0);expect(resolutions).toHaveLength(0);expect(writes).toHaveLength(0);return}
 await add.click();let dialog=page.getByRole("dialog");
 const check=()=>dialog.getByRole("button",{name:ru?"Проверить изменения":"Check changes",exact:true}),save=()=>dialog.getByRole("button",{name:ru?"Сохранить":"Save",exact:true}),resolve=()=>dialog.getByRole("button",{name:ru?"Получить условия на дату сделки":"Resolve terms at trade time",exact:true});
 const count=()=>dialog.getByRole("textbox",{name:ru?"Количество лотов":"Number of lots",exact:true}),instrument=()=>dialog.getByRole("textbox",{name:ru?"Бумага: ISIN или тикер + площадка":"Security: ISIN or ticker + venue",exact:true}),mode=()=>dialog.getByRole("combobox",{name:ru?"Единица ввода количества":"Quantity input unit",exact:true});
 await dialog.getByRole("combobox",{name:ru?"Тип операции":"Operation type",exact:true}).selectOption("buy");await instrument().fill("stock");await mode().selectOption("lots");
 await dialog.getByRole("checkbox",{name:ru?"Известна только дата операции":"Only the operation date is known",exact:true}).check();await dialog.locator('input[type="date"]').fill("2023-12-31");await dialog.getByRole("textbox",{name:ru?"Часовой пояс даты (IANA)":"Date timezone (IANA)",exact:true}).fill("UTC");await dialog.getByRole("textbox",{name:ru?"Сумма / чистый номинал сделки":"Amount / clean trade principal",exact:true}).fill("20");
 await count().fill("9007199254740993.123456789");await expect(check()).toBeDisabled();await expect(save()).toBeDisabled();await resolve().click();await expect(dialog).toContainText("90071992547409931.23456789");await expect(dialog).toContainText("9007199254740993");
 await count().fill("2");await expect(check()).toBeDisabled();await resolve().click();await expect(check()).toBeEnabled();await check().click();await expect(save()).toBeEnabled();
 // Both unit proof and financial review are invalidated by a trade-date change.
 await dialog.locator('input[type="date"]').fill("2024-01-01");await expect(check()).toBeDisabled();await expect(save()).toBeDisabled();await resolve().click();await expect(dialog).toContainText(ru?"2 лотов × 100":"2 lots × 100");
 await dialog.locator('input[type="date"]').fill("2023-12-31");await resolve().click();await check().click();await save().click();await expect(dialog.getByRole("alert")).toBeVisible();await expect(save()).toBeDisabled();
 // A stale mapping on commit requires new terms and another full review.
 await resolve().click();await expect(dialog).toContainText("9007199254740994");await expect(dialog).toContainText(ru?"2 лотов × 5":"2 lots × 5");await expect(save()).toBeDisabled();await check().click();await expect(save()).toBeEnabled();
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await dialog.getByRole("group",{name:ru?"Конверсия исторического лота":"Historical lot conversion",exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:`/private/tmp/budget-lot-input-${locale}-mobile.png`,fullPage:true});
 await save().click();await expect(dialog).toHaveCount(0);expect(writes).toHaveLength(2);expect(writes[1].operation.lotConversion.lotSize).toBe("5");expect(writes[1].previewHash).toBe("a".repeat(64));
 await page.getByRole("button",{name:ru?"Исправить":"Amend",exact:true}).click();dialog=page.getByRole("dialog");await expect(mode()).toHaveValue("lots");await expect(count()).toHaveValue("2");await expect(dialog).toContainText("9007199254740994");
 await instrument().fill("missing");await expect(check()).toBeDisabled();await resolve().click();await expect(dialog.getByRole("alert")).toBeVisible();await expect(check()).toBeDisabled();await expect(save()).toBeDisabled();
 // Known units remain possible without fabricating historical lot terms.
 await mode().selectOption("units");await dialog.getByRole("textbox",{name:ru?"Количество":"Quantity",exact:true}).fill("3");await dialog.getByRole("textbox",{name:ru?"Причина исправления":"Reason for correction",exact:true}).fill("Independent statement units");await check().click();await save().click();await expect(dialog).toHaveCount(0);expect(writes[2].operation.quantity).toBe("3");expect(writes[2].operation.lotConversion).toBeUndefined();expect(writes[2].expectedOperationVersion).toBe(1n);
});
