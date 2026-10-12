"use client";
import InvestmentTradeFinancialsDetails from "@/components/InvestmentTradeFinancialsDetails";
import {useTranslations} from "next-intl";
import type {PreviewInvestmentOperationResponse} from "@/proto/budget/v1/investment_pb";
const date=(s?:{seconds:bigint;nanos:number})=>s?new Date(Number(s.seconds)*1000+s.nanos/1000000).toLocaleString("en-GB",{timeZone:"Europe/Moscow"}):"—";
export default function InvestmentMutationPreview({value}:{value:PreviewInvestmentOperationResponse}){
 const t=useTranslations("investmentPreview"),tt=useTranslations("investmentTiming");
 const warnings=[...new Set(value.comparisons.flatMap(c=>c.after?.issues||[]))];
 return <section className="space-y-3 rounded-xl border border-blue-300 p-3 dark:border-blue-800" aria-label={t("title")}><h3 className="font-semibold">{t("title")}</h3><p role={value.valid?"status":"alert"}>{t(value.valid?"valid":"invalid")}</p><p className="break-words text-xs">{t("checked")} {date(value.checkedAt)} · {t("dirty")} {date(value.dirtyFrom)}</p><p className="text-xs">{t("method")}</p>
 {value.proposed?.timing?.occurredDate&&<p className="break-words text-xs text-amber-700">{tt("occurredDateOnly")}: {value.proposed.timing.occurredDate} · {value.proposed.timing.timezone}. {tt("policy")}</p>}
 {value.proposed?.importedCashDeficitReason&&<p className="break-words text-sm text-amber-700">{t("cashDeficitReason")}: {value.proposed.importedCashDeficitReason}</p>}
 {value.proposed?.tradeFinancials&&<InvestmentTradeFinancialsDetails operation={value.proposed}/>}
 {value.issues.map((v,i)=><div key={i} role="alert" className="space-y-1 break-all text-sm text-red-600"><p>{t.has(`issues.${v.code}`)?t(`issues.${v.code}`):v.code}</p>{v.operationId&&<p>{t("operation")} {v.operationId}</p>}{v.instrumentKey&&<p>{v.instrumentKey}</p>}{v.at&&<p>{date(v.at)}</p>}{v.cash&&<p>{v.cash.currency}: {t("settled")} {v.cash.settled}; {t("receivable")} {v.cash.receivable}; {t("payable")} {v.cash.payable}; {t("economic")} {v.cash.economic}</p>}</div>)}
 {warnings.map(w=><p key={w} className="break-words text-xs text-amber-700 dark:text-amber-400">{t.has(`warnings.${w}`)?t(`warnings.${w}`):w}</p>)}
 {value.valid&&value.comparisons.map((c,i)=><details key={i} className="space-y-2 rounded border border-gray-300 p-2 dark:border-gray-700" open={c.stage==="operation"}><summary className="cursor-pointer break-words font-medium">{t(`stages.${c.stage}`)} · {c.after?.ledger?.name} · {date(c.asOf)}</summary><p className="text-xs">{t("columns")}</p>
 {c.cashChange.map(v=>{const b=c.before?.cash.find(a=>a.currency===v.currency),a=c.after?.cash.find(a=>a.currency===v.currency);return <div key={v.currency} className="space-y-1 border-t pt-2 text-xs"><p className="font-semibold">{v.currency}</p>{(["settled","receivable","payable","economic"] as const).map(k=><p className="break-all font-mono" key={k}>{t(k)}: {b?.[k]||"0"} → {a?.[k]||"0"} ({v[k]})</p>)}</div>})}
 {c.positionChange.map(v=>{const b=c.before?.positions.find(a=>a.instrumentKey===v.instrumentKey),a=c.after?.positions.find(a=>a.instrumentKey===v.instrumentKey);return <div key={v.instrumentKey} className="space-y-1 border-t pt-2 text-xs"><p className="break-all font-semibold">{v.instrumentKey}</p><p className="break-all font-mono">{t("quantity")}: {b?.quantity||"0"} → {a?.quantity||"0"} ({v.quantity})</p><p className="break-all font-mono">{t("cost")}: {b?(b.cleanCost||"—"):"0"} {b?.currency||v.currency} → {a?(a.cleanCost||"—"):"0"} {a?.currency||v.currency} ({v.cleanCost||"—"} {v.currency})</p></div>})}
 </details>)}
 </section>
}
