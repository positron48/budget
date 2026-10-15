import type {GetInvestmentExchangeRatesResponse,InvestmentCurrentFXLeg} from "@/proto/budget/v1/investment_nav_pb";
import {multiply,add,negate,divide} from "./portfolio-tracker";
import type {Fraction} from "./investment-positions";
const fraction=(v?:Fraction)=>!!v&&v.numerator.length<=256&&v.denominator.length<=256&&/^[1-9][0-9]*$/.test(v.numerator)&&/^[1-9][0-9]*$/.test(v.denominator);
const stamp=(v?:{seconds:bigint;nanos:number})=>v&&v.seconds>=-2208988800n&&v.seconds<=7258118399n&&Number.isInteger(v.nanos)&&v.nanos>=0&&v.nanos<1e9?v.seconds*1000000000n+BigInt(v.nanos):undefined;
export type CurrentFXRow={from:string;rate:Fraction;legs:InvestmentCurrentFXLeg[]};
// Current conversion and its original evidence must agree. Read time is never a fixing/publication time.
export function currentFXRow(p:GetInvestmentExchangeRatesResponse|undefined,from:string,now=Date.now()):CurrentFXRow|undefined{
 if(!p||p.currency!=="RUB"||!p.source||!p.timeConvention||!p.fxPolicyKey||p.policyVersion<1n||!Number.isFinite(now))return;
 const read=stamp(p.readAt);if(read===undefined)return;
 const candidates=p.rates.filter(r=>r.date===""&&r.fromCurrency===from);
 if(candidates.length!==1||!fraction(candidates[0].rate))return;
 const rate=candidates[0].rate!,legs=p.currentFxLegs.filter(l=>l.nativeCurrency===from);
 if(!legs.length||legs.length>2)return;
 if(!validPath(p,from,rate,legs,read,read>BigInt(Math.trunc(now))*1000000n?read:BigInt(Math.trunc(now))*1000000n))return;
 return {from,rate,legs};
}
function validPath(p:GetInvestmentExchangeRatesResponse,from:string,rate:Fraction,legs:InvestmentCurrentFXLeg[],read:bigint,validAt:bigint){
 let currency=from,product:Fraction={numerator:"1",denominator:"1"};
 for(const leg of legs){
  const at=stamp(leg.asOf),available=stamp(leg.availableAt),until=stamp(leg.validUntil);
  if(!leg.id||leg.revision<1n||leg.policy!==p.fxPolicyKey+"@"+p.policyVersion||!fraction(leg.observedRate)||at===undefined||available===undefined||until===undefined||at>available||available>read||until<=validAt||available>validAt||currency!==(leg.inverse?leg.toCurrency:leg.fromCurrency)||leg.fromCurrency===leg.toCurrency||![leg.fromCurrency,leg.toCurrency].every(c=>["RUB","USD","EUR"].includes(c)))return false;
  product=multiply(product,leg.inverse?{numerator:leg.observedRate!.denominator,denominator:leg.observedRate!.numerator}:leg.observedRate!);
  currency=leg.inverse?leg.fromCurrency:leg.toCurrency;
 }
 return currency==="RUB"&&BigInt(product.numerator)*BigInt(rate.denominator)===BigInt(rate.numerator)*BigInt(product.denominator);
}
export function currentFXChange(p:GetInvestmentExchangeRatesResponse|undefined,from:string,now=Date.now()){
 const current=currentFXRow(p,from,now);if(!current||!p)return;
 const point=p.rates.find(r=>r.date===""&&r.fromCurrency===from)!,rate=point.previousRate,legs=point.previousFxLegs;
 if(!fraction(rate)||legs.length!==current.legs.length||!legs.length)return;
 let validAt=0n;
 for(let i=0;i<legs.length;i++){
  const leg=legs[i],live=current.legs[i],at=stamp(leg.asOf),liveAt=stamp(live.asOf),available=stamp(leg.availableAt);
  if(leg.nativeCurrency!==from||leg.fromCurrency!==live.fromCurrency||leg.toCurrency!==live.toCurrency||leg.inverse!==live.inverse||at===undefined||liveAt===undefined||at>=liveAt||available===undefined||stamp(legs[0].asOf)!==at||stamp(current.legs[0].asOf)!==liveAt)return;
  if(available>validAt)validAt=available;
 }
 if(!validPath(p,from,rate!,legs,stamp(p.readAt)!,validAt))return;
 const change=add(current.rate,negate(rate!)),relative=divide(change,rate!);
 return {rate:rate!,legs,change,relative:relative!};
}
