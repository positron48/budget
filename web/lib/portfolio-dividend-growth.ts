import {clone} from "@bufbuild/protobuf";
import {timestampFromDate} from "@bufbuild/protobuf/wkt";
import {InvestmentDividendHistorySchema,type InvestmentDividendHistory} from "@/proto/budget/v1/investment_pb";
const day=(s:string)=>/^\d{4}-\d{2}-\d{2}$/.test(s)&&Number.isFinite(Date.parse(s+"T00:00:00Z"))&&new Date(s+"T00:00:00Z").toISOString().slice(0,10)===s;
export function dividendInputTime(value:string){const d=new Date(value+"Z");if(!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(value)||!Number.isFinite(d.getTime())||d.toISOString().slice(0,value.length)!==value)throw new Error("dividend_date");return timestampFromDate(d);}
export function dividendTimeInput(value?:{seconds:bigint}){return value?new Date(Number(value.seconds)*1000).toISOString().slice(0,19):"";}
export function prepareDividendHistory(input:InvestmentDividendHistory,unitsReviewed:boolean){
 const h=clone(InvestmentDividendHistorySchema,input),source=h.sourceReference.trim(),evidence=h.evidence.trim();
 if(!source||!evidence||!h.reason.trim()||(!h.cancelled&&!unitsReviewed))throw new Error("dividend_review");
 if(!h.basisAt||!h.reviewedFrom||h.reviewedFrom.seconds>h.basisAt.seconds||!h.years.length)throw new Error("dividend_date");
 const fill=(s:string)=>s.trim()||source,explain=(s:string)=>s.trim()||evidence,years=new Set(h.years.map(y=>y.year));
 if(years.size!==h.years.length)throw new Error("dividend_year");
 for(const y of h.years){if(!Number.isInteger(y.year)||y.year<1900||y.year>2200)throw new Error("dividend_year");y.sourceReference=fill(y.sourceReference);y.evidence=explain(y.evidence);}
 for(const d of h.dividends){
  if(!day(d.paymentDate)||!years.has(Number(d.paymentDate.slice(0,4)))||!d.unitBasisAt||d.unitBasisAt.seconds<h.reviewedFrom.seconds||d.unitBasisAt.seconds>h.basisAt.seconds)throw new Error("dividend_date");
  if(d.amount&&!/^\d{1,24}(\.\d{1,12})?$/.test(d.amount)||!["regular","special","unknown"].includes(d.class)||!["paid","announced","unknown","cancelled"].includes(d.status))throw new Error("dividend_amount");
  d.sourceReference=fill(d.sourceReference);d.evidence=explain(d.evidence);
 }
 for(const s of h.splits){if(!s.effectiveAt||s.effectiveAt.seconds<h.reviewedFrom.seconds||s.effectiveAt.seconds>h.basisAt.seconds)throw new Error("dividend_date");if(![s.newUnits,s.oldUnits].every(v=>/^\d{1,24}(\.\d{1,12})?$/.test(v)&&/[1-9]/.test(v)))throw new Error("dividend_split");s.sourceReference=fill(s.sourceReference);s.evidence=explain(s.evidence);}
 return h;
}

// Provider request windows are inclusive and at most 365 days. Leap years need
// two windows; the shared boundary is deduplicated when observations are read.
export function dividendSourceWindows(year:string){
 if(!/^\d{4}$/.test(year)||Number(year)<1900||Number(year)>2199)throw new Error("dividend_year");
 const first=new Date(year+"-01-01T00:00:00Z"),end=new Date(`${Number(year)+1}-01-01T00:00:00Z`),boundary=new Date(Math.min(first.getTime()+365*86400000,end.getTime()));
 return [{from:timestampFromDate(first),to:timestampFromDate(boundary)},...(boundary<end?[{from:timestampFromDate(boundary),to:timestampFromDate(end)}]:[])];
}
