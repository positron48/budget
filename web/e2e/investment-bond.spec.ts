import {test,expect} from "@playwright/test";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {MarketDataService} from "../proto/budget/v1/market_data_pb";
for(const locale of ["ru","en"]){test(`${locale} bond precision gaps pagination and read-only mode`,async({page,context})=>{
 const readonly=process.env.E2E_INVESTMENTS_READ_ONLY==="true",source={provider:"tinvest",accessScope:"personal"},at=BigInt(Math.floor(Date.now()/1000)-86400*2);let loaded=readonly,fail=false;const writes:any[]=[];
 await context.addCookies([{name:"NEXT_LOCALE",value:locale,url:test.info().project.use.baseURL as string}]);await page.addInitScript(()=>{localStorage.setItem("budget/access","fixture");localStorage.setItem("budget/tenant","tenant");});
 await page.route("**/budget.v1.*/*",async route=>{
 const path=new URL(route.request().url()).pathname,svc=[TenantService,InvestmentService,InvestmentNAVService,MarketDataService].find(s=>path.includes(s.typeName+"/")),m=svc?.methods.find(m=>path.endsWith("/"+m.name));let data:any={};const bytes=route.request().postDataBuffer()!,input=m?fromBinary(m.input,bytes.subarray(5,5+bytes.readUInt32BE(1))) as any:{};
 if(m&&!/^(Get|List|Search)/.test(m.name))writes.push(input);
 if(m?.name==="ListMyTenants")data={memberships:[{tenant:{id:"tenant",name:"Owner"},role:1}]};
 if(m?.name==="ListMarketSources")data={sources:[{source,title:"Bond source"}]};
 if(m?.name==="SearchMarketInstruments")data={instruments:[{instrumentId:"listing",name:"Example bond",ticker:"BOND",kind:"bond",currency:"RUB"}]};
 if(m?.name==="GetMarketHistory")data={observations:[{id:"quote",price:"101",observedAt:{seconds:at},currency:"RUB",priceUnit:"percent_nominal",complete:true}]};
 if(m?.name==="RequestMarketRefresh")data={job:{id:"bond-job",kind:"bond_facts",status:"pending"}};
 if(m?.name==="GetMarketDataJob"){loaded=true;data={job:{id:"bond-job",kind:"bond_facts",status:"ready"}};}
 if(m?.name==="GetMarketBondFacts")data=loaded?{snapshotId:"saved-source",payloadHash:"hash",source,instrumentId:"listing",asOf:{seconds:at},from:input.from,to:input.to,coverageState:"observations_only",terms:{nominal:{amount:"9007199254740993.000000001",currency:"RUB"},currentAccrued:{amount:"0",currency:"RUB"},floating:false},coupons:Array.from({length:21},(_,i)=>({number:BigInt(i+1),date:{seconds:at},kind:"COUPON_TYPE_FLOATING",...(i===1?{pay:{amount:"0",currency:"RUB"}}:{})})),events:[{number:1,date:{seconds:at},kind:"EVENT_TYPE_CALL",annualRate:"5.000000001",value:"100"}],accrued:[{date:{seconds:at},value:"0",nominal:"1000"},{date:{seconds:at+86400n}}]}:{source,coverageState:"unavailable"};
 const body=m?Buffer.from(toBinary(m.output,create(m.output,data))):Buffer.alloc(0),h=Buffer.alloc(5);h.writeUInt32BE(body.length,1);const trailer=Buffer.from(`grpc-status: ${m?.name==="GetMarketBondFacts"&&fail?13:0}\r\n`),th=Buffer.alloc(5);th[0]=128;th.writeUInt32BE(trailer.length,1);await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([h,body,th,trailer])});
 });
 await page.goto("/investments/instruments/tinvest%3Alisting?provider=tinvest&scope=personal&listing=listing");const card=page.getByRole("region",{name:locale==="ru"?"Условия и выплаты облигации":"Bond terms and payments",exact:true});await expect(card).toBeVisible();
 const load=card.getByRole("button",{name:locale==="ru"?"Загрузить сведения об облигации":"Load bond facts",exact:true});if(readonly)await expect(load).toBeDisabled();else{await expect(card.getByText(locale==="ru"?"Для всего выбранного периода сохранённого снимка нет. Загрузите сведения.":"No saved snapshot covers the entire selected period. Load bond facts.",{exact:true})).toBeVisible();await load.click();}
 await expect(card.getByText("9007199254740993.000000001 RUB",{exact:true})).toBeVisible();await expect(card.locator("article")).toHaveCount(20);await expect(card.locator("article").nth(0)).toContainText("—");await expect(card.locator("article").nth(1)).toContainText("0 RUB");
 await card.getByRole("button",{name:locale==="ru"?"Далее":"Next",exact:true}).click();await expect(card.locator("article")).toHaveCount(1);await card.getByRole("button",{name:locale==="ru"?"События (1)":"Events (1)",exact:true}).click();await expect(card.locator("article")).toContainText("5.000000001%");
 await card.getByRole("button",{name:locale==="ru"?"История НКД (2)":"Accrued interest history (2)",exact:true}).click();await expect(card.locator("article")).toHaveCount(2);await expect(card.locator("article").nth(1)).toContainText("—");
 await page.setViewportSize({width:375,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await card.screenshot({path:`/private/tmp/budget-bond-${locale}.png`});expect(writes.filter(w=>w.kind==="bond_facts")).toHaveLength(readonly?0:1);if(readonly)expect(writes).toEqual([]);
 fail=true;await page.reload();await expect(card.getByRole("alert")).toBeVisible({timeout:20000});await expect(card.getByText("9007199254740993.000000001 RUB",{exact:true})).toHaveCount(0);
});}
