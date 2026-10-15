import {add,divide,zero} from "./portfolio-tracker";
import type {Fraction} from "./investment-positions";
import type {ReportHolding,ReportPosition} from "./portfolio-report";
import type {InvestmentGroups} from "@/proto/budget/v1/investment_nav_pb";
export type AllocationMode="security"|"group"|"account"|"currency"|"type";
export type AllocationBasis="value"|"cost";
export type AllocationBucket={key:string;known:Fraction;missing:number};
export function compareAmounts(a:Fraction,b:Fraction){const difference=BigInt(a.numerator)*BigInt(b.denominator)-BigInt(b.numerator)*BigInt(a.denominator);return difference<0n?-1:difference>0n?1:0;}
// All inputs are already in the report's display currency. Keep missing amounts
// separate; the denominator is explicitly the known portion when incomplete.
export function portfolioAllocation(holdings:ReportHolding[],mode:AllocationMode,basis:AllocationBasis,groups?:InvestmentGroups,types=new Map<string,string>()){
 const membership=new Map((groups?.memberships||[]).map(m=>[m.ledgerId+"\0"+m.instrumentKey,m.groupId]));
 const buckets=new Map<string,AllocationBucket>();let missing=0;
 for(const h of holdings){
  const key=mode==="security"?h.key:mode==="account"?h.ledgerId:mode==="currency"?h.priceCurrency||"":mode==="type"?types.get(h.key)||"":membership.get(h.ledgerId+"\0"+h.key)||"";
  const bucket=buckets.get(key)||{key,known:zero,missing:0},amount=h[basis];
  if(amount&&BigInt(amount.denominator)>0n&&BigInt(amount.numerator)>=0n)bucket.known=add(bucket.known,amount);else{bucket.missing++;missing++;}
  buckets.set(key,bucket);
 }
 const rows=[...buckets.values()].sort((a,b)=>compareAmounts(b.known,a.known)||a.key.localeCompare(b.key));
 const total=add(...rows.map(r=>r.known));return {rows,total,missing};
}
export function allocationShare(amount:Fraction|undefined,total:Fraction){return amount&&BigInt(amount.numerator)>=0n?divide(amount,total):undefined;}
export type HoldingSort="name"|"quantity"|"price"|"value"|"cost"|"profit"|"income"|"unrealized";
export function portfolioHoldingRows(positions:ReportPosition[],options:{closed:boolean;search:string;sort:HoldingSort;descending:boolean;label:(key:string)=>string;extra?:(key:string)=>string}){
 const query=options.search.trim().toLocaleLowerCase();
 return positions.filter(p=>(options.closed||BigInt(p.quantity.numerator)!==0n)&&(!query||[p.key,p.name,p.ticker,options.label(p.key),options.extra?.(p.key),...p.accounts].some(v=>v?.toLocaleLowerCase().includes(query)))).sort((a,b)=>{
  let comparison=0;
  if(options.sort==="name")comparison=options.label(a.key).localeCompare(options.label(b.key));
  else{const av=options.sort==="income"&&!a.incomeComplete?undefined:a[options.sort],bv=options.sort==="income"&&!b.incomeComplete?undefined:b[options.sort];if(!av||!bv)return av?-1:bv?1:a.key.localeCompare(b.key);comparison=compareAmounts(av,bv);}
  return (options.descending?-comparison:comparison)||a.key.localeCompare(b.key);
 });
}
export function holdingAccounts(holdings:ReportHolding[],key:string){
 const rows=new Map<string,{ledgerId:string;quantity:Fraction;value?:Fraction;cost?:Fraction}>();
 for(const h of holdings){if(h.key!==key)continue;const row=rows.get(h.ledgerId)||{ledgerId:h.ledgerId,quantity:zero,value:zero,cost:zero};row.quantity=add(row.quantity,h.quantity);row.value=row.value!==undefined&&h.value!==undefined?add(row.value,h.value):undefined;row.cost=row.cost!==undefined&&h.cost!==undefined?add(row.cost,h.cost):undefined;rows.set(h.ledgerId,row);}
 return [...rows.values()].sort((a,b)=>a.ledgerId.localeCompare(b.ledgerId));
}
