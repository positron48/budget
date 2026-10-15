import type {GetTrackerInstrumentPricesResponse,TrackerInstrumentPricePoint} from "@/proto/budget/v1/investment_nav_pb";
import {shiftHistoryDate} from "./portfolio-history";
export type InstrumentScope={instrumentKey:string;currency:string;fxPolicyKey:string;fromDate:string;toDate:string;listingId?:string;provider?:string;accessScope?:string;instrumentId?:string};
type API={getTrackerInstrumentPrices(q:InstrumentScope,options:{signal:AbortSignal}):Promise<GetTrackerInstrumentPricesResponse>};
export async function loadPortfolioInstrument(api:API,scope:InstrumentScope,signal:AbortSignal,progress:(data:GetTrackerInstrumentPricesResponse)=>void){
 const points:TrackerInstrumentPricePoint[]=[];let cursor=scope.fromDate,first:GetTrackerInstrumentPricesResponse|undefined;
 const rational=(v:{numerator:string;denominator:string}|undefined)=>!v||/^[0-9]+$/.test(v.numerator)&&/^[1-9][0-9]*$/.test(v.denominator)&&Number.isFinite(Number(v.numerator)/Number(v.denominator));
 for(let n=0;n<119;n++){
  signal.throwIfAborted();const page=await api.getTrackerInstrumentPrices({...scope,fromDate:cursor},{signal});signal.throwIfAborted();
  if(page.instrumentKey!==scope.instrumentKey||page.currency!==scope.currency||page.fxPolicyKey!==scope.fxPolicyKey||page.reportingTimezone!=="Europe/Moscow"||page.priceConvention!=="retrospective_close_next_utc_day_v1"||!["market","cbr","unavailable"].includes(page.fxMode)||!/^[a-f0-9]{64}$/.test(page.basisKey)||page.points.length<1||page.points.length>31)throw new Error("invalid_instrument_history");
  if(first&&(first.basisKey!==page.basisKey||first.fxPolicyVersion!==page.fxPolicyVersion||first.fxMode!==page.fxMode))throw new Error("history_changed");first??=page;
  for(const p of page.points){if(p.date!==cursor||p.date>scope.toDate||!["ready","incomplete","unavailable"].includes(p.state)||![p.raw,p.nativeClean,p.clean,p.accrued,p.nativeDirty,p.dirty].every(rational)||p.raw&&(p.candleDate>p.date||p.observationRevision<1n||!p.observationId||BigInt(p.raw.numerator)<=0n)||p.state==="unavailable"&&!!p.raw||!p.raw&&!!(p.clean||p.nativeClean||p.dirty||p.nativeDirty||p.accrued))throw new Error("invalid_instrument_point");points.push(p);cursor=shiftHistoryDate(cursor,1);}
  const result={...first,points:[...points],nextDate:page.nextDate};progress(result);
  if(!page.nextDate){if(cursor!==shiftHistoryDate(scope.toDate,1))throw new Error("truncated_instrument_history");return result;}
  if(page.nextDate!==cursor||cursor>scope.toDate)throw new Error("invalid_instrument_cursor");
 }
 throw new Error("history_limit");
}
export function instrumentHref(key:string,query=""){return "/investments/instruments/"+encodeURIComponent(key)+(query?"?"+query:"");}
