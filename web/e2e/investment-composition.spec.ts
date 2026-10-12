import {checkInvestmentChartKeyboard} from "./investment-chart-helpers";
import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
const amount=(value:string)=>({numerator:value,denominator:"1",decimal:value});
for(const locale of ["ru","en"]){test(`${locale} composition groups preserve URL and exclude cash`,async({page,context})=>{
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);
 await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 const group="33333333-3333-4333-8333-333333333333",calls:string[][]=[];let incomplete=false;
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:[{key:"series",ledgerIds:["ledger"],currency:"RUB",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-03"}]};
  if(method?.name==="GetInvestmentGroups")data={classification:{version:1n,groups:[{id:group,name:"Strategy"}]},positions:[{ledgerId:"ledger",ledgerName:"Broker",instrumentKey:"ABC"}]};
  if(method?.name==="GetInvestmentCompositionHistory"){
   const bytes=route.request().postDataBuffer()!,input=fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any;calls.push(input.groupIds);
   const selected=input.groupIds.includes(group);
   data={classificationVersion:1n,currency:"RUB",groups:[{id:group,name:"Strategy"}],complete:!incomplete,points:[
    {date:"2024-01-01",state:"ready",securitiesKnown:amount(selected?"30":"90"),securitiesComplete:true,cashKnown:amount("10"),cashComplete:true},
    {date:"2024-01-02",state:"stale"},
    {date:"2024-01-03",state:incomplete?"incomplete":"ready",securitiesKnown:amount(selected?"30":"90"),securitiesComplete:!incomplete,cashKnown:amount("10"),cashComplete:true}],
    segments:[{kind:"position",groupId:group,categoryKey:group,knownValue:amount("30"),complete:true,...(!incomplete?{weight:{numerator:"3",denominator:"10",decimal:"0.3"}}:{})},{kind:"position",categoryKey:"",knownValue:amount("60"),complete:!incomplete,...(!incomplete?{weight:{numerator:"6",denominator:"10",decimal:"0.6"}}:{})},{kind:"cash",categoryKey:"cash",knownValue:amount("10"),complete:true,...(!incomplete?{weight:{numerator:"1",denominator:"10",decimal:"0.1"}}:{})}]};
   if(input.breakdownBy&&input.breakdownBy!=="group")data.segments=[{kind:"position",categoryKey:input.breakdownBy==="currency"?"USD":input.breakdownBy==="type"?"share":"ledger",knownValue:amount("90"),complete:!incomplete,...(!incomplete?{weight:{numerator:"9",denominator:"10",decimal:"0.9"}}:{})},data.segments.at(-1)];
  }
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),header=Buffer.alloc(5);header.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);
  await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([header,body,th,trailer])});
 });
 await page.goto(`/investments/value?group=${group}`);
 const section=page.getByRole("heading",{name:locale==="ru"?"Группы и состав портфеля":"Groups and portfolio composition",exact:true}).locator("..");
 await expect(section.getByLabel("Strategy",{exact:true})).toBeChecked();await expect(section.getByText("30 RUB",{exact:true}).first()).toBeVisible();await expect.poll(()=>calls.some(v=>v.length===1&&v[0]===group)).toBe(true);
 const pie=section.getByRole("img",{name:locale==="ru"?"Состав полного портфеля по группам и денежным остаткам":"Full portfolio composition by group and cash",exact:true});await expect(pie).toBeVisible();await checkInvestmentChartKeyboard(pie);
 await expect(section.getByText("30.00%",{exact:true})).toBeVisible();
 // A missing day splits the line; no interpolated bridge through a stale value.
 const chart=section.getByRole("img",{name:locale==="ru"?"Стоимость выбранных бумаг":"Selected securities value",exact:true});await expect(chart.locator("path")).toHaveCount(2);await checkInvestmentChartKeyboard(chart);
 await section.getByRole("button",{name:locale==="ru"?"Все бумаги":"All securities",exact:true}).click();await expect(page).toHaveURL(/investments\/value\??$/);await expect(section.getByText("90 RUB",{exact:true}).first()).toBeVisible();
 await page.reload();await expect(section.getByLabel("Strategy",{exact:true})).not.toBeChecked();
 await page.setViewportSize({width:375,height:900});await expect(pie).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);await page.screenshot({path:test.info().outputPath(`budget-investment-composition-${locale}.png`),fullPage:true});
 const breakdown=section.getByLabel(locale==="ru"?"Группировать по":"Group by",{exact:true});
 await breakdown.selectOption("currency");await expect(page).toHaveURL(/composition=currency/);await expect(section.getByRole("cell",{name:"USD",exact:true})).toBeVisible();
 await breakdown.selectOption("account");await expect(section.getByRole("cell",{name:"Broker",exact:true})).toBeVisible();
 await breakdown.selectOption("type");await expect(section.getByRole("cell",{name:locale==="ru"?"Акции":"Shares",exact:true})).toBeVisible();
 incomplete=true;await page.reload();await expect(pie).toHaveCount(0);await expect(section.getByText("30.00%",{exact:true})).toHaveCount(0);
});}
