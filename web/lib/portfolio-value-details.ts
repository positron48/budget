import type {GetTrackerHistoryResponse,TrackerHistoryPoint} from "@/proto/budget/v1/investment_nav_pb";
import type {Timestamp} from "@bufbuild/protobuf/wkt";
import {historySelectionEqual,type HistoryScope} from "./portfolio-history";
import {multiply} from "./portfolio-tracker";
import {positionSum,type Fraction} from "./investment-positions";
type API={getTrackerHistory(input:{ledgerIds:string[];groupIds?:string[];instrumentKeys?:string[];currency:string;fxPolicyKey:string;fromDate:string;toDate:string;detailDate:string},options:{signal:AbortSignal}):Promise<GetTrackerHistoryResponse>};
const valid=(v?:Fraction)=>!v||/^[0-9]+$/.test(v.numerator)&&/^[1-9][0-9]*$/.test(v.denominator);
const equal=(a:Fraction,b:Fraction)=>BigInt(a.numerator)*BigInt(b.denominator)===BigInt(b.numerator)*BigInt(a.denominator);
const ns=(t?:Timestamp)=>t? t.seconds*1000000000n+BigInt(t.nanos):undefined;
export async function loadPortfolioValueDetails(api:API,scope:HistoryScope,basisKey:string,point:TrackerHistoryPoint,signal:AbortSignal){
 signal.throwIfAborted();
 const {fxPolicyVersion,fxMode,classificationVersion,...q}=scope;
 const p=await api.getTrackerHistory({...q,detailDate:point.date},{signal});signal.throwIfAborted();
 if(!p.detailsIncluded||p.basisKey!==basisKey||p.currency!==q.currency||p.fxPolicyKey!==q.fxPolicyKey||p.fxPolicyVersion!==fxPolicyVersion||p.fxMode!==fxMode||p.priceConvention!=="retrospective_close_next_utc_day_v1"||p.reportingTimezone!=="Europe/Moscow"||p.nextDate||p.points.length!==1||p.monetaryOnly||!historySelectionEqual(p.instrumentKeys,q.instrumentKeys)||!historySelectionEqual(p.groupIds,q.groupIds)||q.groupIds?.length&&p.classificationVersion!==classificationVersion)throw new Error("history_changed");
 const d=p.points[0],cutoff=BigInt(Date.parse(d.date+"T21:00:00Z"))*1000000n-1n;
 if(d.date!==point.date||d.state!==point.state||d.positions!==point.positions||d.pricedPositions!==point.pricedPositions||!d.knownValue||!valid(d.knownValue)||!point.knownValue||!equal(d.knownValue,point.knownValue)||d.details.length>10000)throw new Error("invalid_valuation_details");
 const seen=new Set<string>(),currencies=new Set<string>();
 for(const r of d.details){
  const id=r.ledgerId+"\0"+r.instrumentKey;
  if(seen.has(id)||!q.ledgerIds.includes(r.ledgerId)||!r.instrumentKey||q.instrumentKeys?.length&&!q.instrumentKeys.includes(r.instrumentKey)||![r.quantity,r.unitPrice,r.nativeValue,r.value,r.fxRate].every(valid)||!r.quantity||BigInt(r.quantity.numerator)===0n||!r.issues.length&&(!r.nativeValue||!r.value)||r.unitPrice&&(!r.priceAt||ns(r.priceAt)!>cutoff||!r.priceSource||r.mappingVersion<1n)||r.nativeValue&&(!r.unitPrice||!equal(r.nativeValue,multiply(r.quantity,r.unitPrice))))throw new Error("invalid_valuation_component");
  seen.add(id);currencies.add(r.nativeCurrency);
  if(r.value&&r.nativeValue){
   if(BigInt(r.nativeValue.numerator)===0n){if(BigInt(r.value.numerator)!==0n)throw new Error("invalid_valuation_fx");}
   else {
    if(!r.fxRate||BigInt(r.fxRate.numerator)===0n||!equal(r.value,multiply(r.nativeValue,r.fxRate)))throw new Error("invalid_valuation_fx");
    const legs=d.fxLegs.filter(l=>l.nativeCurrency===r.nativeCurrency);let current=r.nativeCurrency,rate:Fraction={numerator:"1",denominator:"1"};
    if(legs.length>2)throw new Error("invalid_valuation_fx");
    for(const leg of legs){
     if(!leg.observedRate||!valid(leg.observedRate)||BigInt(leg.observedRate.numerator)===0n||!leg.id||leg.revision<1n||leg.policy!==q.fxPolicyKey+"@"+fxPolicyVersion||!leg.asOf||!leg.availableAt||!leg.validUntil||ns(leg.asOf)!>ns(leg.availableAt)!||ns(leg.availableAt)!>cutoff||ns(leg.validUntil)!<=cutoff||current!==(leg.inverse?leg.toCurrency:leg.fromCurrency))throw new Error("invalid_valuation_fx");
     rate=multiply(rate,leg.inverse?{numerator:leg.observedRate.denominator,denominator:leg.observedRate.numerator}:leg.observedRate);current=leg.inverse?leg.fromCurrency:leg.toCurrency;
    }
    if(current!==q.currency||!equal(rate,r.fxRate))throw new Error("invalid_valuation_fx");
   }
  }
 }
 if(d.fxLegs.some(l=>!currencies.has(l.nativeCurrency))||new Set(d.details.map(r=>r.instrumentKey)).size!==d.positions||!equal(positionSum(d.details.map(r=>r.value)).known,d.knownValue))throw new Error("invalid_valuation_total");
 return d;
}
