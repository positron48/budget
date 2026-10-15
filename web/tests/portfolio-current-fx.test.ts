import {describe,it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {GetInvestmentExchangeRatesResponseSchema,InvestmentNAVAmountSchema} from "@/proto/budget/v1/investment_nav_pb";
import {currentFXRow} from "@/lib/portfolio-current-fx";
const at=1767225600n,now=Number(at+86400n)*1000;
function fixture(){return create(GetInvestmentExchangeRatesResponseSchema,{currency:"RUB",fxPolicyKey:"alpha",policyVersion:1n,mode:"market",source:"alphavantage",timeConvention:"Retrospective next-day close",readAt:{seconds:at+86400n},rates:[{date:"2025-12-31",fromCurrency:"USD",rate:{numerator:"70",denominator:"1"}},{fromCurrency:"USD",rate:{numerator:"80",denominator:"1"}}],currentFxLegs:[{nativeCurrency:"USD",id:"fixing",revision:2n,fromCurrency:"USD",toCurrency:"RUB",policy:"alpha@1",observedRate:{numerator:"80",denominator:"1"},asOf:{seconds:at},availableAt:{seconds:at+43200n},validUntil:{seconds:at+172800n}}]});}
describe("current reporting FX evidence",()=>{
 it("uses current evidence without replacing fixing time by the API read time",()=>{const p=fixture(),r=currentFXRow(p,"USD",now)!;expect(r.rate.numerator).toBe("80");expect(r.legs[0].asOf!.seconds).toBe(at);expect(r.legs[0].availableAt!.seconds).toBe(at+43200n);});
 it("retains the original quote when the pair is inverted",()=>{const p=fixture();p.currentFxLegs[0].fromCurrency="RUB";p.currentFxLegs[0].toCurrency="USD";p.currentFxLegs[0].inverse=true;p.currentFxLegs[0].observedRate!.numerator="1";p.currentFxLegs[0].observedRate!.denominator="80";expect(currentFXRow(p,"USD",now)?.rate.numerator).toBe("80");});
 it("does not substitute historical, expired, future or incoherent evidence",()=>{
  const changes=[
   (p:ReturnType<typeof fixture>)=>{p.rates=p.rates.slice(0,1);},
   (p:ReturnType<typeof fixture>)=>{p.currentFxLegs=[];},
   (p:ReturnType<typeof fixture>)=>{p.source="";},
   (p:ReturnType<typeof fixture>)=>{p.currentFxLegs[0].policy="cbr@1";},
   (p:ReturnType<typeof fixture>)=>{p.currentFxLegs[0].validUntil!.seconds=at+86400n;},
   (p:ReturnType<typeof fixture>)=>{p.currentFxLegs[0].availableAt!.seconds=at+86401n;},
   (p:ReturnType<typeof fixture>)=>{p.currentFxLegs[0].observedRate!.numerator="79";},
   (p:ReturnType<typeof fixture>)=>{p.currentFxLegs[0].inverse=true;},
   (p:ReturnType<typeof fixture>)=>{p.currentFxLegs[0].asOf!.seconds=999999999999999n;},
   (p:ReturnType<typeof fixture>)=>{p.rates.push(p.rates[1]);},
  ];
  for(const change of changes){const p=fixture();change(p);expect(currentFXRow(p,"USD",now)).toBeUndefined();}
 });
 it("requires both ordered legs for a cross rate",()=>{const p=fixture();p.rates[1].rate!.numerator="120";const original=p.currentFxLegs[0];p.currentFxLegs=[create(GetInvestmentExchangeRatesResponseSchema,{currentFxLegs:[{...original,id:"usd-eur",toCurrency:"EUR",observedRate:create(InvestmentNAVAmountSchema,{numerator:"6",denominator:"5"})}]}).currentFxLegs[0],create(GetInvestmentExchangeRatesResponseSchema,{currentFxLegs:[{...original,id:"eur-rub",fromCurrency:"EUR",observedRate:create(InvestmentNAVAmountSchema,{numerator:"100",denominator:"1"})}]}).currentFxLegs[0]];expect(currentFXRow(p,"USD",now)?.rate.numerator).toBe("120");p.currentFxLegs.reverse();expect(currentFXRow(p,"USD",now)).toBeUndefined();});
});
