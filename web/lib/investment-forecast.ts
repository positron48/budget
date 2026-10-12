import type {InvestmentPayoutAmount} from "@/proto/budget/v1/investment_pb";

// An explicit offset avoids interpreting a market cutoff in the browser zone.
// Preserve microseconds instead of passing a fractional instant through Date.
export function parseForecastInstant(value:string){
 const m=/^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})(?:\.(\d{1,6}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
 if(!m)throw new Error("invalid_cutoff");
 const day=new Date(`${m[1]}T00:00:00Z`);
 if(!Number.isFinite(day.getTime())||day.toISOString().slice(0,10)!==m[1])throw new Error("invalid_cutoff");
 const ms=Date.parse(`${m[1]}T${m[2]}${m[4]}`);
 if(!Number.isFinite(ms)||m[2].startsWith("24:")||day.getUTCFullYear()<1900||day.getUTCFullYear()>2200)throw new Error("invalid_cutoff");
 return {seconds:BigInt(ms/1000),nanos:Number((m[3]||"").padEnd(9,"0"))};
}
export function formatForecastInstant(value?:{seconds:bigint;nanos:number}){
 if(!value)return "";
 const base=new Date(Number(value.seconds)*1000).toISOString().replace(/\.000Z$/,""),fraction=String(value.nanos).padStart(9,"0").replace(/0+$/,"");
 return `${base}${fraction?"."+fraction:""}Z`;
}
export function forecastBarPercent(value?:InvestmentPayoutAmount,max?:InvestmentPayoutAmount){
 if(!value||!max)return undefined;
 try {
  const n=BigInt(value.numerator),d=BigInt(value.denominator),mn=BigInt(max.numerator),md=BigInt(max.denominator);
  if(d<=0n||md<=0n||n<0n||mn<=0n)return undefined;
  const ratio=n*md*10000n/(d*mn);
  return Number(ratio>10000n?10000n:ratio)/100;
 }catch{return undefined}
}
export function forecastMaximum(values:(InvestmentPayoutAmount|undefined)[]){
 let maximum:InvestmentPayoutAmount|undefined;
 for(const value of values){
  if(!value)continue;
  try{const n=BigInt(value.numerator),d=BigInt(value.denominator);if(n<0n||d<=0n)continue;
   if(!maximum||n*BigInt(maximum.denominator)>BigInt(maximum.numerator)*d)maximum=value;
  }catch{/* Invalid evidence must not become a zero-height confirmed amount. */}
 }
 return maximum;
}
