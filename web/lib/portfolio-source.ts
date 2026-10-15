import {create} from "@bufbuild/protobuf";
import {InvestmentMarketPeriodSchema,type InvestmentMarketMapping,type InvestmentMarketPeriod,type MarketInstrument,type MarketSourceKey} from "@/proto/budget/v1/market_data_pb";
const instant=(p:{seconds:bigint;nanos:number})=>p.seconds*1000000000n+BigInt(p.nanos);
// Preserve the timeline before the reviewed UTC date; replace only its tail.
export function replaceSourceTail(periods:InvestmentMarketPeriod[],next:InvestmentMarketPeriod){
 if(!next.from)throw new Error("invalid_source_date");const start=instant(next.from),prior:InvestmentMarketPeriod[]=[];
 for(const p of periods){if(!p.from||instant(p.from)>=start)continue;prior.push(p.to&&instant(p.to)<=start?p:create(InvestmentMarketPeriodSchema,{...p,to:next.from}));}
 if(prior.length>=100)throw new Error("source_period_limit");return [...prior,next];
}
export function reviewedPortfolioSource(key:string,mapping:InvestmentMarketMapping|undefined,source:MarketSourceKey,instrument:MarketInstrument,purpose:"current"|"history",date:string,nominal:string,requestKey:string){
 if(!key||key.length>200||mapping&&mapping.instrumentKey!==key||!source.accessScope||!instrument.instrumentId||!instrument.detailed||!["share","etf","bond"].includes(instrument.kind)||!/^[A-Z]{3}$/.test(instrument.currency)||!/^\d{4}-\d{2}-\d{2}$/.test(date)||!Number.isFinite(Date.parse(date))||new Date(date).toISOString().slice(0,10)!==date||date<"1900-01-01"||date>new Date().toISOString().slice(0,10)||purpose==="current"&&source.provider!=="tinvest"||purpose==="history"&&!["moex","twelvedata"].includes(source.provider))throw new Error("invalid_source_selection");
 if(instrument.kind==="bond"&&(!/^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(nominal)||!/[1-9]/.test(nominal)||nominal.length>50))throw new Error("bond_nominal_required");
 const evidence=`User reviewed ${instrument.ticker} / ${instrument.isin||instrument.instrumentId} / ${instrument.classCode} on ${source.provider}/${source.accessScope} from ${date} UTC`;
 const period=create(InvestmentMarketPeriodSchema,{source,instrumentId:instrument.instrumentId,kind:instrument.kind,currency:instrument.currency,unit:instrument.kind==="bond"?"percent_of_nominal":"currency_per_unit",nominal:instrument.kind==="bond"?nominal:"",nominalCurrency:instrument.kind==="bond"?instrument.currency:"",from:{seconds:BigInt(Date.parse(date)/1000),nanos:0},evidence});
 const old=mapping?.periods||[],history=mapping?.historicalPeriods||[];
 const replaceHistorical=purpose==="history"||!history.length&&old.length>0;
 return {instrumentKey:key,expectedVersion:mapping?.version||0n,requestKey,reason:evidence,periods:purpose==="current"?replaceSourceTail(old,period):old,replaceHistorical,historicalPeriods:purpose==="history"?replaceSourceTail(history.length?history:old,period):replaceHistorical?old:[]};
}
