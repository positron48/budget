export const investmentPeriods=["7d","1m","3m","6m","ytd","1y","5y","all"] as const;
export type InvestmentPeriod=typeof investmentPeriods[number];
export function validInvestmentDate(value:string){
 if(!/^\d{4}-\d{2}-\d{2}$/.test(value))return false;
 const date=new Date(`${value}T00:00:00Z`);
 return !Number.isNaN(date.valueOf())&&date.toISOString().slice(0,10)===value;
}
// Calendar periods end on the selected inclusive day. The opening baseline is
// the previous day; month/year subtraction clamps leap days and month ends.
export function investmentPeriodRange(period:InvestmentPeriod,start:string,end:string){
 if(!validInvestmentDate(start)||!validInvestmentDate(end)||start>end)return;
 const at=new Date(`${end}T00:00:00Z`);let from=start;
 if(period==="ytd")from=`${end.slice(0,4)}-01-01`;
 else if(period==="7d"){at.setUTCDate(at.getUTCDate()-6);from=at.toISOString().slice(0,10);}
 else if(period!=="all"){
  const months=({"1m":1,"3m":3,"6m":6,"1y":12,"5y":60} as const)[period];
  const wantedDay=at.getUTCDate();at.setUTCDate(1);at.setUTCMonth(at.getUTCMonth()-months);
  const last=new Date(Date.UTC(at.getUTCFullYear(),at.getUTCMonth()+1,0)).getUTCDate();
  at.setUTCDate(Math.min(wantedDay,last)+1);from=at.toISOString().slice(0,10);
 }
 return {from:from<start?start:from,to:end};
}
