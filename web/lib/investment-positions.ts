import type {InvestmentPositionDetail} from "@/proto/budget/v1/investment_nav_pb";
export type Fraction={numerator:string;denominator:string};
export type PositionSum={known:Fraction;complete:boolean;knownCount:number};
function parse(v?:Fraction){if(!v||! /^-?\d{1,2048}$/.test(v.numerator)||!/^\d{1,2048}$/.test(v.denominator))return;const d=BigInt(v.denominator);if(d<=0n)return;return {n:BigInt(v.numerator),d};}
function gcd(a:bigint,b:bigint):bigint{a=a<0n?-a:a;while(b){const r=a%b;a=b;b=r;}return a||1n;}
export function decimalFraction(value:string):Fraction|undefined{
 if(! /^-?\d{1,100}(\.\d{1,28})?$/.test(value))return;
 const negative=value.startsWith("-"),[whole,part=""]=value.replace(/^-/ ,"").split(".");const d=10n**BigInt(part.length),n=(BigInt(whole)*d+BigInt(part||"0"))*(negative?-1n:1n);return {numerator:n.toString(),denominator:d.toString()};
}
export function positionSum(values:(Fraction|undefined)[]):PositionSum{
 let n=0n,d=1n,complete=true,knownCount=0;
 for(const value of values){const v=parse(value);if(!v){complete=false;continue}knownCount++;n=n*v.d+v.n*d;d*=v.d;const factor=gcd(n,d);n/=factor;d/=factor;}
 return {known:{numerator:n.toString(),denominator:d.toString()},complete,knownCount};
}
export function positionDecimal(value?:Fraction,precision:12|28=12):string{
 const v=parse(value);if(!v)return "—";
 const absolute=v.n<0n?-v.n:v.n,scale=10n**BigInt(precision),rounded=(absolute*scale*2n+v.d)/(v.d*2n);
 const part=(rounded%scale).toString().padStart(precision,"0").replace(/0+$/,"");return `${v.n<0n&&rounded!==0n?"-":""}${rounded/scale}${part?"."+part:""}`;
}
export function comparePositionValues(a?:Fraction,b?:Fraction){const x=parse(a),y=parse(b);if(!x)return y?1:0;if(!y)return -1;const diff=x.n*y.d-y.n*x.d;return diff===0n?0:diff>0n?-1:1;}
export function aggregatePositions(rows:InvestmentPositionDetail[]){
 const groups=new Map<string,InvestmentPositionDetail[]>();for(const row of rows){const group=groups.get(row.instrumentKey)||[];group.push(row);groups.set(row.instrumentKey,group)}
 return Array.from(groups,([instrumentKey,rows])=>({instrumentKey,rows,metadata:rows.find(r=>r.metadata)?.metadata,quantity:positionSum(rows.map(r=>decimalFraction(r.quantity))),value:positionSum(rows.map(r=>r.value)),weight:positionSum(rows.map(r=>r.weight))}));
}
export function nativePositionSums(rows:InvestmentPositionDetail[],read:(p:InvestmentPositionDetail)=>Fraction|undefined){
 const grouped=new Map<string,(Fraction|undefined)[]>();for(const row of rows){const values=grouped.get(row.bookCurrency)||[];values.push(read(row));grouped.set(row.bookCurrency,values)}
 return Array.from(grouped,([currency,values])=>({currency,...positionSum(values)})).sort((a,b)=>a.currency.localeCompare(b.currency));
}
export function positionPayments(rows:InvestmentPositionDetail[]){
 const grouped=new Map<string,(Fraction|undefined)[]>();for(const row of rows)for(const payment of row.payments){const values=grouped.get(payment.currency)||[];values.push(payment.net);grouped.set(payment.currency,values)}
 return Array.from(grouped,([currency,values])=>({currency,...positionSum(values)})).sort((a,b)=>a.currency.localeCompare(b.currency));
}
