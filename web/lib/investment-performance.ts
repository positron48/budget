// Exact rational TWR is converted to a percent before display rounding. Money
// magnitude never passes through Number; 0.02 is 2%, not 0.02%.
export function exactReturnPercent(value: {numerator:string;denominator:string}|undefined, locale:string) {
 if(!value||!/^[-]?\d+$/.test(value.numerator)||!/^\d+$/.test(value.denominator)) return "—";
 const n=BigInt(value.numerator), d=BigInt(value.denominator);
 if(d===0n)return "—";
 const absolute=n<0n?-n:n;
 const hundredths=(absolute*10000n*2n+d)/(d*2n);
 const separator=locale.startsWith("ru")?",":".";
 return `${n<0n&&hundredths!==0n?"-":""}${hundredths/100n}${separator}${(hundredths%100n).toString().padStart(2,"0")}%`;
}
export function annualReturnPercent(value:number|undefined,locale:string) {
 if(value===undefined||!Number.isFinite(value)||value<=-1)return "—";
 // Preserve representable extreme annualized returns without overflowing ×100.
 if(Math.abs(value)>1e12){const [mantissa,exponent]=value.toExponential(2).split("e");return `${locale.startsWith("ru")?mantissa.replace(".",","):mantissa}e+${Number(exponent)+2}%`;}
 return new Intl.NumberFormat(locale,{style:"percent",minimumFractionDigits:2,maximumFractionDigits:2}).format(value);
}

export type ReturnFraction = {numerator:string;denominator:string};
export type ReturnPoint = {date:string;state:string;segment:number;segmentReturn?:ReturnFraction;index?:ReturnFraction};
type Rational = {n:bigint;d:bigint};
function fraction(value?:ReturnFraction):Rational|undefined {
 if(!value||! /^-?\d+$/.test(value.numerator)||!/^\d+$/.test(value.denominator))return undefined;
 const d=BigInt(value.denominator);return d>0n?{n:BigInt(value.numerator),d}:undefined;
}
const compare=(a:Rational,b:Rational)=>a.n*b.d-b.n*a.d;
const subtract=(a:Rational,b:Rational):Rational=>({n:a.n*b.d-b.n*a.d,d:a.d*b.d});
function reportingDate(value:string) {
 if(!/^\d{4}-\d{2}-\d{2}$/.test(value))return undefined;
 const ms=Date.parse(`${value}T00:00:00Z`);
 return Number.isFinite(ms)&&new Date(ms).toISOString().slice(0,10)===value?ms:undefined;
}

// Geometry accepts raw return fractions or authoritative base-100 indices.
// Each baseline belongs to its own segment, never stitched to the
// preceding segment's gain. Geometry uses exact fractions until the final,
// bounded pixel coordinate; decimal display rounding never drives the graph.
export function returnGeometry(points:ReturnPoint[],normalized=false) {
 const values=points.map(point=>point.segment>0?(normalized?(["baseline","calculated"].includes(point.state)?fraction(point.index):undefined):(point.state==="baseline"?{n:0n,d:1n}:point.state==="calculated"?fraction(point.segmentReturn):undefined)):undefined);
 const dates=points.map(point=>reportingDate(point.date));
 const usable=values.flatMap((value,index)=>value&&dates[index]!==undefined?[{value,index}]:[]);
 const empty={paths:[] as {segment:number;path:string}[],dots:[] as {x:number;y:number;index:number}[],min:undefined as ReturnFraction|undefined,max:undefined as ReturnFraction|undefined};
 if(!usable.some(p=>points[p.index].state==="calculated"))return empty;
 const min=usable.reduce((a,b)=>compare(a.value,b.value)<0n?a:b).value;
 const max=usable.reduce((a,b)=>compare(a.value,b.value)>0n?a:b).value;
 const span=subtract(max,min);
 const validDates=dates.filter((d):d is number=>d!==undefined);
 const first=Math.min(...validDates),last=Math.max(...validDates);
 const result={...empty,min:{numerator:min.n.toString(),denominator:min.d.toString()},max:{numerator:max.n.toString(),denominator:max.d.toString()}};
 let path="",segment=0,previous:number|undefined;
 const flush=()=>{if(path)result.paths.push({segment,path});path="";};
 points.forEach((point,index)=>{
  const value=values[index],date=dates[index];
  if(!value||date===undefined){flush();previous=undefined;return;}
  if(previous===undefined||date-previous!==86400000||point.segment!==segment||point.state==="baseline")flush();
  // Duplicate/out-of-order dates are not legitimate observations.
  if(previous!==undefined&&date<=previous){previous=undefined;return;}
  segment=point.segment;
  const diff=subtract(value,min);
  const x=last===first?400:20+(date-first)/(last-first)*760;
  const y=span.n===0n?130:240-Number(diff.n*span.d*220000n/(diff.d*span.n))/1000;
  path+=`${path?" L":"M"}${x},${y}`;result.dots.push({x,y,index});previous=date;
 });
 flush();return result;
}
