import type {GetInvestmentPerformanceResponse} from "@/proto/budget/v1/investment_nav_pb";
import {positionDecimal} from "@/lib/investment-positions";
export type AnalyticsMode="value"|"profit"|"months"|"years";
export function analyticsNumber(v?:{numerator:string;denominator:string}){
 if(!v)return undefined;
 const n=Number(positionDecimal(v,12));return Number.isFinite(n)?n:undefined;
}
// Keep export values exact; numbers are only used to position pixels.
export function portfolioAnalyticsCSV(p:GetInvestmentPerformanceResponse){
 const head=["type","date","through","currency","fx_policy","convention","value_num","value_den","flow_num","flow_den","contributions_num","contributions_den","invested_num","invested_den","profit_num","profit_den","daily_profit_num","daily_profit_den","twr_index_num","twr_index_den","reason","flow_reason","benchmark","benchmark_index_num","benchmark_index_den","virtual_value_num","virtual_value_den","virtual_profit_num","virtual_profit_den","virtual_reason"];
 const pair=(v?:{numerator:string;denominator:string})=>[v?.numerator||"",v?.denominator||""];
 const rows=[head],twrByDate=new Map(p.twrPoints.map(t=>[t.date,t])),benchmarkPoints=new Map(p.trackerBenchmarks.map(b=>[b.key,new Map(b.points.map(t=>[t.date,t]))]));
 for(const d of p.analyticsPoints){const twr=twrByDate.get(d.date),base=["day",d.date,d.date,p.currency,p.fxPolicyKey,p.analyticsConvention,...pair(d.value),...pair(d.flow),...pair(d.contributions),...pair(d.invested),...pair(d.profit),...pair(d.dailyProfit),...pair(twr?.index),d.reason,d.flowReason];rows.push([...base,"","","","","","","",""]);for(const b of p.trackerBenchmarks){const q=benchmarkPoints.get(b.key)?.get(d.date);rows.push([...base,b.key,...pair(q?.index),...pair(q?.virtualValue),...pair(q?.virtualProfit),q?.virtualReason||""]);}}
 for(const [kind,periods] of [["month",p.monthlyProfit],["year",p.yearlyProfit]] as const)for(const d of periods)rows.push([kind,d.fromDate,d.toDate,p.currency,p.fxPolicyKey,p.analyticsConvention,"","","","","","","","",...pair(d.profit),"","","","",d.reason,"","","","","","","","",""]);
 rows[0].push("twr_segment","twr_state","twr_reason","daily_return_num","daily_return_den","segment_return_num","segment_return_den","xirr_annual","xirr_reason","xirr_annualized");
 for(const row of rows.slice(1)){const twr=row[0]==="day"?twrByDate.get(row[1]):undefined;row.push(twr?.segment.toString()||"",twr?.state||"",twr?.reason||"",...pair(twr?.dailyReturn),...pair(twr?.segmentReturn),p.xirr===undefined?"":String(p.xirr),p.xirrReason,String(p.xirrAnnualized));}
 return rows.map(row=>row.map(cell=>'"'+(/^[=+@\-\t\r]/.test(cell)&&!/^-[0-9]+(?:\.[0-9]+)?(?:e[+-]?[0-9]+)?$/i.test(cell)?"'"+cell:cell).replaceAll('"','""')+'"').join(",")).join("\r\n");
}
