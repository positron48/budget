import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
const amount=(n:string,d="1")=>({numerator:n,denominator:d,decimal:"999999999999999999"});
for(const locale of ["ru","en"])test(`${locale} instrument P&L keeps closed positions, exact sorting, scope and gaps`,async({page,context})=>{
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 let partial=false;const calls:string[]=[];
 const series={key:"series",ledgerIds:["a","b"],currency:"USD",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-02"};
 await page.route("**/budget.v1.*/*",async route=>{
  const path=new URL(route.request().url()).pathname,service=[TenantService,InvestmentService,InvestmentNAVService].find(s=>path.includes(s.typeName+"/")),method=service?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};if(method)calls.push(method.name);
  if(method?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Profit"},role:1}]};
  if(method?.name==="ListInvestmentNAVSeries")data={series:[series]};if(method?.name==="GetInvestmentNAVSeries")data={series};
  if(method?.name==="GetInvestmentProfit"){
   const bytes=route.request().postDataBuffer()!,input=fromBinary(method.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any;expect(input).toMatchObject({seriesKey:"series",fromDate:"2024-01-02",toDate:"2024-01-02"});
   data={seriesKey:"series",fromDate:input.fromDate,toDate:input.toDate,currency:"USD",profit:amount("27021597764222979"),instrumentsComplete:!partial,
    instruments:Array.from({length:60},(_,i)=>({instrumentKey:`security-${String(i).padStart(2,"0")}`,ledgerIds:i===0?["a","b"]:["a"],closed:i!==1,complete:!(partial&&i===0),known:amount(i===0?(partial?"-10":"9007199254740993"):i===1?"9007199254740992":"0"),share:!partial?amount(i===0?"1":i===1?"9007199254740992":"0",i===0?"3":"27021597764222979"):undefined,metadata:{instrumentId:`id-${i}`,name:i===0?"Closed winner":i===1?"Open holding":`A zero ${String(i).padStart(2,"0")}`,isin:`TESTISIN${i}`},components:[{kind:"opening_market",known:amount("-10"),complete:true},{kind:"closing_market",known:amount(i===1?"9007199254741002":"0"),complete:true},{kind:"cash_movements",...(partial&&i===0?{reason:"fx_missing"}:{known:amount(i===0?"9007199254741003":i===1?"0":"10"),complete:true})},{kind:"noncash_transfers",known:amount("0"),complete:true}]})),
    instrumentRemainder:{kind:"cash_and_unallocated",...(partial?{reason:"instrument_contribution_incomplete"}:{known:amount("9007199254740994"),complete:true})},instrumentRemainderShare:!partial?amount("9007199254740994","27021597764222979"):undefined};
  }
  const body=method?Buffer.from(toBinary(method.output,create(method.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from("grpc-status: 0\r\n"),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
 });
 await page.goto("/investments/value?series=series&from=2024-01-02&to=2024-01-02&group=selected");
 const section=page.getByTestId("instrument-contributions"),rows=section.getByTestId("contribution-row");await expect(rows).toHaveCount(50);
 await expect(rows.first()).toContainText("Closed winner");await expect(rows.first()).toContainText("9007199254740993 USD");await expect(rows.first()).toContainText(locale==="ru"?"33,33%":"33.33%");await expect(rows.first()).not.toContainText("999999999999999999");
 await section.getByRole("button",{name:locale==="ru"?"Далее":"Next",exact:true}).click();await expect(rows).toHaveCount(10);
 const search=section.getByLabel(locale==="ru"?"Поиск вклада по бумаге":"Search instrument contributions",{exact:true});await search.fill("TESTISIN1");await expect(rows).toHaveCount(11);await expect(page).toHaveURL(/contributionSearch=TESTISIN1/);await page.reload();await expect(search).toHaveValue("TESTISIN1");await search.fill("");await expect(page).not.toHaveURL(/contributionSearch/);
 const closed=section.getByLabel(locale==="ru"?"Включая закрытые позиции":"Include closed positions",{exact:true});await closed.click();await expect(rows).toHaveCount(1);await expect(rows.first()).toContainText("Open holding");await expect(page).toHaveURL(/contributionClosed=false/);await closed.click();await expect(rows).toHaveCount(50);
 const order=section.getByLabel(locale==="ru"?"Сортировка вклада":"Contribution order",{exact:true});await order.selectOption("name");await expect(rows.first()).toContainText("A zero 02");await order.selectOption("profit");await expect(rows.first()).toContainText("Closed winner");
 await rows.first().getByText(locale==="ru"?"Расчёт вклада":"Contribution calculation",{exact:true}).click();await expect(rows.first()).toContainText("-10 USD");await expect(rows.first()).toContainText(locale==="ru"?"Неденежные переносы по подтверждённой рыночной стоимости":"Noncash transfers at reviewed fair value");
 const link=rows.first().getByRole("link");await expect(link).toHaveAttribute("href",/instruments\/security-00\?series=series&from=2024-01-02&to=2024-01-02/);expect(await link.getAttribute("href")).not.toContain("group=");
 await search.fill("Closed winner");await expect(rows).toHaveCount(1);
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await expect(section.getByTestId("contribution-card")).toHaveCount(1);await expect(section.getByTestId("contribution-card")).toContainText("9007199254740993 USD");await section.screenshot({path:`/private/tmp/budget-contributions-${locale}.png`});
 await search.fill("");await expect(page).not.toHaveURL(/contributionSearch/);
 partial=true;await page.reload();await expect(section.getByText(locale==="ru"?"Разложение неполное.":"Incomplete attribution.",{exact:false})).toBeVisible();await expect(section.getByTestId("contribution-card").first()).toContainText(locale==="ru"?"Доля чистой прибыли серии: —":"Share of net series profit: —");await expect(page.getByTestId("contribution-remainder")).not.toContainText("USD");
 await search.fill("Closed winner");await expect(section.getByTestId("contribution-card")).toHaveCount(1);await expect(section.getByTestId("contribution-card")).toContainText("-10 USD");await expect(section.getByTestId("contribution-card")).toContainText(locale==="ru"?"Известная часть компонентов":"Known component subtotal");
 expect(calls.filter(c=>!/^(Get|List)/.test(c))).toEqual([]);
});
