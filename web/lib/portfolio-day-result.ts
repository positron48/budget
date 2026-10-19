import type {GetInvestmentPerformanceResponse,TrackerValueDetail} from "@/proto/budget/v1/investment_nav_pb";
import {shiftHistoryDate} from "./portfolio-history";
import {portfolioReport,reportingDate} from "./portfolio-report";
import {add,negate,zero,decimal,multiply} from "./portfolio-tracker";
import type {Fraction} from "./investment-positions";
const valid=(v:Fraction|undefined):v is Fraction=>!!v&&v.numerator.length<=256&&v.denominator.length<=256&&/^-?[0-9]+$/.test(v.numerator)&&/^[1-9][0-9]*$/.test(v.denominator);
// Read the signed-snapshot analytics result; never subtract current quotes or
// a partial known subtotal from a historical full value. Flows already include
// purchases/sales and payouts exactly once in the cashless strategy convention.
export function portfolioDayResult(p:GetInvestmentPerformanceResponse|undefined,date:string){
 if(!p||p.fromDate!==date||p.toDate!==date||p.analyticsConvention!=="retrospective_cashless_strategy_v1"||p.reportingTimezone!=="Europe/Moscow"||p.analyticsPoints.length!==2)return;
 const [before,after]=p.analyticsPoints;
 if(before.date!==shiftHistoryDate(date,-1)||after.date!==date||after.flowReason||![before.value,after.value,after.flow,after.dailyProfit].every(valid))return;
 const calculated=add(after.value!,negate(before.value!),negate(after.flow!));
 if(BigInt(calculated.numerator)*BigInt(after.dailyProfit!.denominator)!==BigInt(after.dailyProfit!.numerator)*BigInt(calculated.denominator))return;
 return {profit:after.dailyProfit!,before:before.value!,after:after.value!,flow:after.flow!};
}

// Native historical candle PriceAt is its next-UTC-day availability boundary.
// Manual estimates and stale carried prices cannot declare a trading session.
export function latestCompletedTradingValuation(details:readonly TrackerValueDetail[],through:string){
 const first=shiftHistoryDate(through,-14);
 return details.filter(d=>d.priceAt&&valid(d.unitPrice)&&valid(d.quantity)&&BigInt(d.quantity.numerator)>0n&&/^(moex|twelvedata|alphavantage|tinvest)\//.test(d.priceSource)&&!d.issues.length)
  .map(d=>new Date(Number(d.priceAt!.seconds)*1000).toISOString().slice(0,10))
  .filter(date=>date>=first&&date<=through).sort().at(-1);
}

// Compare the same securities at live prices and the previous close. Today's
// purchases/sales and payouts are cash movements, never market appreciation.
export function livePortfolioDayResult(positions:readonly import('./portfolio-report').ReportPosition[],operations:import('@/proto/budget/v1/investment_pb').InvestmentOperation[],quotes:readonly import('@/proto/budget/v1/investment_nav_pb').InvestmentQuote[],currency:string,fx:import('@/proto/budget/v1/investment_nav_pb').GetInvestmentExchangeRatesResponse|undefined,include:(ledger:string,key:string)=>boolean=()=>true,now=Date.now()){
 const today=reportingDate(BigInt(Math.floor(now/1000))),previous=shiftHistoryDate(today,-1);
 const dayOps=operations.filter(o=>o.status==='posted'&&o.occurredAt&&Number(o.occurredAt.seconds)*1000<=now&&reportingDate(o.occurredAt.seconds)===today&&include(o.ledgerId,o.instrumentKey));
 const cash=portfolioReport([],dayOps.filter(o=>!o.kind.startsWith('security_transfer_')),new Map(),currency,fx,include);
 const cashByKey=new Map(cash.positions.map(p=>[p.key,p.profit]));
 const prices=new Map(quotes.map(q=>[q.instrumentKey,q]));
 const keys=new Set([...positions.filter(p=>BigInt(p.quantity.numerator)!==0n).map(p=>p.key),...dayOps.map(o=>o.instrumentKey).filter(Boolean)]);
 let before=zero,after=zero,profit=zero,count=0,partial=false;
 for(const key of keys){
  const p=positions.find(p=>p.key===key),q=prices.get(key),ops=dayOps.filter(o=>o.instrumentKey===key);
  const currentQuantity=p?.quantity||zero;
  let delta=zero,transfer=zero,unsupported=false;
  for(const o of ops){
   const quantity=decimal(o.quantity);
   if(['buy','sell','redemption','security_transfer_in','security_transfer_out'].includes(o.kind)){
    if(!quantity){unsupported=true;continue;}
    const signed=['sell','redemption','security_transfer_out'].includes(o.kind)?negate(quantity):quantity;
    delta=add(delta,signed);if(o.kind.startsWith('security_transfer_'))transfer=add(transfer,signed);
   }else if(['split','distribution','opening_position','position_reconciliation'].includes(o.kind))unsupported=true;
  }
  const opening=add(currentQuantity,negate(delta));
  const rate=q?.previousCloseCurrency===currency?{numerator:'1',denominator:'1'}:fx?.currency===currency?fx.rates.find(r=>r.date===previous&&r.fromCurrency===q?.previousCloseCurrency)?.rate:undefined;
  const close=q?.previousClose&&rate&&valid(q.previousClose)&&valid(rate)?multiply(q.previousClose,rate):p?.price; // Last known current price freezes an otherwise unpriced small holding.
  const current=p?.value||(BigInt(currentQuantity.numerator)===0n?zero:close?multiply(currentQuantity,close):undefined);
  const movement=cashByKey.has(key)?cashByKey.get(key):zero;
  if(unsupported||!close||!current||!movement||BigInt(opening.numerator)<0n){partial=true;continue;}
  const base=multiply(opening,close);
  before=add(before,base);after=add(after,current);
  profit=add(profit,current,negate(base),movement,negate(multiply(transfer,close)));count++;
  if(!q?.previousClose||!rate)partial=true;
 }
 // Unassigned fees/payouts have no quantity but still affect today's result.
 if(cashByKey.has('')){const amount=cashByKey.get('');if(amount)profit=add(profit,amount);else partial=true;}
 return count?{result:{before,after,profit},tradingDate:today,partial}:undefined;
}
