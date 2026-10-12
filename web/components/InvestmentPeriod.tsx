"use client";
import {useSearchParams,useRouter,usePathname} from "next/navigation";
import {useTranslations} from "next-intl";
import {investmentPeriods,investmentPeriodRange,validInvestmentDate,type InvestmentPeriod} from "@/lib/investment-period";

export function useInvestmentRange(seriesKey:string,start:string,end:string){
 const params=useSearchParams(),router=useRouter(),pathname=usePathname();
 const from=params.get("from")??start,to=params.get("to")??(pathname.includes("/instruments/")?params.get("date"):null)??end;
 const valid=validInvestmentDate(from)&&validInvestmentDate(to)&&from>=start&&to<=end&&from<=to;
 const update=(nextFrom:string,nextTo:string,period="custom")=>{
  const q=new URLSearchParams(params.toString());q.set("series",seriesKey);q.set("from",nextFrom);q.set("to",nextTo);q.set("period",period);
  if(pathname.includes("/instruments/"))q.set("date",nextTo);
  router.replace(`${pathname}?${q}`,{scroll:false});
 };
 const select=(period:InvestmentPeriod)=>{const range=investmentPeriodRange(period,start,valid?to:end);if(range)update(range.from,range.to,period);};
 const candidate=params.get("period") as InvestmentPeriod|null;
 const expected=candidate&&investmentPeriods.includes(candidate)?investmentPeriodRange(candidate,start,to):undefined;
 const period=expected&&expected.from===from&&expected.to===to?candidate:"custom";
 return {from,to,valid,period,update,select};
}
export default function InvestmentPeriod({period,select}:{period:string|null;select:(value:InvestmentPeriod)=>void}){
 const t=useTranslations("investmentPeriod");
 return <div className="space-y-1"><div role="group" aria-label={t("title")} className="flex flex-wrap gap-2">{investmentPeriods.map(value=><button key={value} type="button" aria-pressed={period===value} onClick={()=>select(value)} className="rounded-lg border border-gray-300 px-3 py-2 text-sm aria-pressed:bg-blue-100 aria-pressed:text-blue-900 dark:border-gray-700 dark:aria-pressed:bg-blue-950 dark:aria-pressed:text-blue-100">{t(value)}</button>)}</div><p className="text-xs text-gray-500">{t("method")}</p></div>;
}
