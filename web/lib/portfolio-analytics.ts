import type {GetInvestmentPerformanceResponse,TrackerHistoryPoint} from "@/proto/budget/v1/investment_nav_pb";
import {positionDecimal} from "@/lib/investment-positions";
export type AnalyticsMode="value"|"profit"|"months"|"years"|"indices";
export function analyticsNumber(v?:{numerator:string;denominator:string}){
 if(!v)return undefined;
 const n=Number(positionDecimal(v,12));return Number.isFinite(n)?n:undefined;
}
// Keep export values exact; numbers are only used to position pixels.
export function portfolioAnalyticsCSV(p:GetInvestmentPerformanceResponse,historyPoints?:readonly TrackerHistoryPoint[]){
 const head=["type","date","through","currency","fx_policy","convention","value_num","value_den","flow_num","flow_den","contributions_num","contributions_den","invested_num","invested_den","profit_num","profit_den","daily_profit_num","daily_profit_den","twr_index_num","twr_index_den","reason","flow_reason","benchmark","benchmark_index_num","benchmark_index_den","virtual_value_num","virtual_value_den","virtual_profit_num","virtual_profit_den","virtual_reason"];
 const pair=(v?:{numerator:string;denominator:string})=>[v?.numerator||"",v?.denominator||""];
 const rows=[head],twrByDate=new Map(p.twrPoints.map(t=>[t.date,t])),benchmarkPoints=new Map(p.trackerBenchmarks.map(b=>[b.key,new Map(b.points.map(t=>[t.date,t]))]));
 for(const d of p.analyticsPoints){const twr=twrByDate.get(d.date),base=["day",d.date,d.date,p.currency,p.fxPolicyKey,p.analyticsConvention,...pair(d.value),...pair(d.flow),...pair(d.contributions),...pair(d.invested),...pair(d.profit),...pair(d.dailyProfit),...pair(twr?.index),d.reason,d.flowReason];rows.push([...base,"","","","","","","",""]);for(const b of p.trackerBenchmarks){const q=benchmarkPoints.get(b.key)?.get(d.date);rows.push([...base,b.key,...pair(q?.index),...pair(q?.virtualValue),...pair(q?.virtualProfit),q?.virtualReason||""]);}}
 for(const [kind,periods] of [["month",p.monthlyProfit],["year",p.yearlyProfit]] as const)for(const d of periods)rows.push([kind,d.fromDate,d.toDate,p.currency,p.fxPolicyKey,p.analyticsConvention,"","","","","","","","",...pair(d.profit),"","","","",d.reason,"","","","","","","","",""]);
 rows[0].push("twr_segment","twr_state","twr_reason","daily_return_num","daily_return_den","segment_return_num","segment_return_den","xirr_annual","xirr_reason","xirr_annualized","instrument_keys","group_ids","classification_version","monetary_only");
 for(const row of rows.slice(1)){const twr=row[0]==="day"?twrByDate.get(row[1]):undefined;row.push(twr?.segment.toString()||"",twr?.state||"",twr?.reason||"",...pair(twr?.dailyReturn),...pair(twr?.segmentReturn),p.xirr===undefined?"":String(p.xirr),p.xirrReason,String(p.xirrAnnualized),p.instrumentKeys.join(";"),p.groupIds.join(";"),p.classificationVersion.toString(),String(p.monetaryOnly));}
 if(historyPoints){
  const historyByDate=new Map(historyPoints.map(d=>[d.date,d]));
  rows[0].push("valuation_state","known_value_num","known_value_den","priced_positions","positions","coverage_key","valuation_issues","valuation_issue_count");
  for(const row of rows.slice(1)){
   const d=row[0]==="day"?historyByDate.get(row[1]):undefined;
   row.push(d?.state||"",...pair(d?.knownValue),d?.pricedPositions.toString()||"",d?.positions.toString()||"",d?.coverageKey||"",d?.issues.map(i=>(i.componentId?i.componentId+":":"")+i.code).join(";")||"",d?String(d.issueCount||d.issues.length):"");
  }
 }
 return rows.map(row=>row.map(cell=>'"'+(/^[=+@\-\t\r]/.test(cell)&&!/^-[0-9]+(?:\.[0-9]+)?(?:e[+-]?[0-9]+)?$/i.test(cell)?"'"+cell:cell).replaceAll('"','""')+'"').join(",")).join("\r\n");
}

// All visible growth lines share a real observed baseline. Benchmarks use price
// returns, independently of portfolio trades; portfolio TWR removes flow effects.
export function portfolioGrowthSeries(p:GetInvestmentPerformanceResponse,ids:string[],from:string,portfolio?:GetInvestmentPerformanceResponse){
 const lines=p.trackerBenchmarks.filter(b=>ids.includes(b.key)).map(b=>({key:b.key,title:b.key==="imoex"?"IMOEX":"SPY · S&P 500",color:b.key==="imoex"?"#ffa638":"#ec70ad",points:b.points.map(d=>({date:d.date,value:analyticsNumber(d.index)}))}));
 if(portfolio&&!portfolio.twrReason&&portfolio.twrPoints.some(d=>d.index))lines.unshift({key:"portfolio",title:"Portfolio",color:"#13b5ea",points:portfolio.twrPoints.map(d=>({date:d.date,value:analyticsNumber(d.index)}))});
 const available=lines.filter(s=>s.points.some(d=>d.value!==undefined&&d.date>=from));
 const maps=available.map(s=>new Map(s.points.map(d=>[d.date,d.value])));
 const baseline=available[0]?.points.find(d=>d.date>=from&&maps.every(m=>(m.get(d.date)||0)>0))?.date;
 return {baseline,lines:available.map((s,i)=>({...s,points:s.points.map(d=>({date:d.date,value:baseline&&d.date>=baseline&&d.value!==undefined?(d.value/maps[i].get(baseline)!-1)*100:undefined}))}))};
}
