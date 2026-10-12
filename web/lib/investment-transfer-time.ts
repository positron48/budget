import type {InvestmentOperationTiming} from "@/proto/budget/v1/investment_pb";
import {assetTimestamp,localAssetTime} from "@/lib/assets";
type Stamp={seconds:bigint;nanos:number};
export function transferTimePayload(at:string,dateOnly:boolean,timezone:string,original?:Stamp,timing?:InvestmentOperationTiming){
 if(dateOnly)return {occurredAt:undefined,timing:{timezone,occurredDate:at,settlementDate:"",sourceDate:timing?.sourceDate===at?at:""}};
 const unchanged=original&&at===localAssetTime(new Date(Number(original.seconds)*1000+original.nanos/1e6));
 return {occurredAt:unchanged?original:assetTimestamp(at),timing:unchanged&&timing?{...timing,occurredDate:"",settlementDate:""}:undefined};
}
