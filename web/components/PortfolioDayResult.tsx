"use client";
import {livePortfolioDayResult} from "@/lib/portfolio-day-result";
import {divide} from "@/lib/portfolio-tracker";
import {reportingDate} from "@/lib/portfolio-report";
import {positionDecimal,type Fraction} from "@/lib/investment-positions";
export default function PortfolioDayResult({currency,ru,live}:{currency:string;ru:boolean;live?:Parameters<typeof livePortfolioDayResult>}){
 const tr=(r:string,e:string)=>ru?r:e,current=live?livePortfolioDayResult(...live):undefined,result=current?.result;
 const displayDate=reportingDate(BigInt(Math.floor(Date.now()/1000)));
 const money=(v?:Fraction)=>v?new Intl.NumberFormat(ru?"ru":"en",{style:"currency",currency,maximumFractionDigits:2}).format(Number(positionDecimal(v))):"—";
 const relative=result&&BigInt(result.before.numerator)>0n?divide(result.profit,result.before):undefined;
 const positive=result&&BigInt(result.profit.numerator)>=0n;
 return <section aria-label={tr("Дневной результат","Daily result")} className="rounded-2xl border border-slate-200 bg-[hsl(var(--card))] p-5 dark:border-slate-700/40" title={tr("Свежие котировки относительно предыдущего закрытия, с учётом сегодняшних операций","Live quotes versus previous close, including today’s operations")}>
 <div className="flex flex-wrap items-center justify-between gap-2 text-slate-500 dark:text-slate-400"><h2 className="text-sm">{tr("За сегодня","Today")}</h2><time className="text-xs" dateTime={displayDate}>{new Intl.DateTimeFormat(ru?"ru":"en",{timeZone:"UTC",day:"numeric",month:"short"}).format(new Date(displayDate+"T00:00:00Z"))}</time></div>
 <p className={"mt-4 text-2xl font-semibold tabular-nums "+(result?positive?"text-emerald-500":"text-rose-500":"")}>{!live?"…":money(result?.profit)}</p>
 <p className={"mt-2 text-sm tabular-nums "+(result?positive?"text-emerald-500":"text-rose-500":"text-slate-400")}>{relative?new Intl.NumberFormat(ru?"ru":"en",{style:"percent",signDisplay:"always",maximumFractionDigits:2}).format(Number(positionDecimal(relative))):"—"}</p>
 {current?.partial&&<span className="text-xs text-amber-600">{tr("По доступным бумагам","Available instruments")}</span>}
 </section>;
}
