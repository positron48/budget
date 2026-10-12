"use client";
import {useState} from "react";
import {useMutation,useInfiniteQuery,useQuery,useQueryClient} from "@tanstack/react-query";
import {useLocale,useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import Modal from "@/components/Modal";
import {assetMoney,assetTimestamp} from "@/lib/assets";
import type {AssetAccount} from "@/proto/budget/v1/asset_pb";
import type {InvestmentAssetActivationPreview} from "@/proto/budget/v1/investment_pb";
const field="w-full rounded-lg border border-gray-300 bg-transparent p-2 text-sm dark:border-gray-700";
const button="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50";
export default function InvestmentAssetActivation({tenantId,ledgerId}:{tenantId:string;ledgerId:string}){
 const t=useTranslations("investmentActivation"),locale=useLocale(),{investment:api,investmentNAV,asset}=useClients(),qc=useQueryClient();
 const [open,setOpen]=useState(false),[accountId,setAccount]=useState(""),[conventionKey,setConvention]=useState(""),[date,setDate]=useState(()=>new Date(new Date().getTime()-new Date().getTimezoneOffset()*60000).toISOString().slice(0,19)),[evidence,setEvidence]=useState(""),[confirmed,setConfirmed]=useState(false),[key,setKey]=useState(()=>crypto.randomUUID());
 const accounts=useQuery<{accounts:AssetAccount[]}>({queryKey:["assets",tenantId,"activation"],enabled:open,queryFn:()=>asset.listAccounts({includeArchived:false})});
 const profiles=useInfiniteQuery({queryKey:["investmentNAV",tenantId,"activation-profiles"],enabled:open,initialPageParam:"",queryFn:({pageParam})=>investmentNAV.listInvestmentValuationProfiles({afterPriceKey:pageParam}),getNextPageParam:last=>last.nextPriceKey||undefined});
 const preview=useMutation<{preview?:InvestmentAssetActivationPreview}>({mutationFn:()=>api.prepareInvestmentAssetActivation({activation:{accountId,ledgerId,conventionKey,valuationFrom:assetTimestamp(date)}}),onSuccess:()=>{setConfirmed(false);setKey(crypto.randomUUID());}});
 const p=preview.data?.preview;
 const save=useMutation({mutationFn:()=>api.activateInvestmentAsset({activation:{accountId,ledgerId,conventionKey,valuationFrom:p!.valuationFrom,previewHash:p!.previewHash,expectedAccountVersion:p!.account!.version,expectedLedgerVersion:p!.ledger!.version,evidence,requestKey:key}}),onSuccess:async()=>{await qc.invalidateQueries({queryKey:["assets"]});setOpen(false);}});
 const changed=()=>{preview.reset();save.reset();setConfirmed(false);setKey(crypto.randomUUID());};
 const busy=preview.isPending||save.isPending,error=accounts.error||profiles.error||preview.error||save.error;
 return <><button type="button" className={button} onClick={()=>{changed();setOpen(true);}}>{t("open")}</button>{open&&<Modal open title={t("title")} onClose={()=>{if(!busy)setOpen(false);}} maxWidthClass="max-w-2xl"><form className="space-y-4" onSubmit={e=>{e.preventDefault();preview.mutate();}}>
 <p className="text-sm text-gray-500">{t("help")}</p>{error&&<p role="alert" className="break-words text-sm text-red-600">{String(error)}</p>}
 <fieldset disabled={busy} className="space-y-3">
 <label className="block text-sm">{t("account")}<select required className={field} value={accountId} onChange={e=>{setAccount(e.target.value);changed();}}><option value="">—</option>{accounts.data?.accounts.filter(a=>a.kind==="investment"&&a.investmentMode!=="ledger"&&!a.archived).map(a=><option key={a.id} value={a.id}>{a.name}</option>)}</select></label>
 <label className="block text-sm">{t("profile")}<select className={field} value={conventionKey} onChange={e=>{setConvention(e.target.value);changed();}}><option value="">{t("cashOnly")}</option>{Array.from(new Map(profiles.data?.pages.flatMap(p=>p.priceProfiles).map(p=>[p.key,p])).values()).map(p=><option key={p.key} value={p.key}>{p.title} · {p.key}</option>)}</select></label>
 {profiles.hasNextPage&&<button type="button" onClick={()=>profiles.fetchNextPage()} disabled={profiles.isFetchingNextPage}>{t("more")}</button>}
 <label className="block text-sm">{t("date")}<input className={field} type="datetime-local" step="1" required value={date} onChange={e=>{setDate(e.target.value);changed();}} /></label>
 <button className={button} disabled={!accountId||accounts.isFetching||!!accounts.error||profiles.isFetching||!!profiles.error}>{t("compare")}</button>
 </fieldset>
 {p&&<section aria-label={t("comparison")} className="space-y-3 rounded-lg border border-gray-300 p-3 dark:border-gray-700"><dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-2 sm:gap-3"><dt>{t("manual")}</dt><dd className="break-words tabular-nums">{assetMoney(p.manualValue?.minorUnits,p.manualValue?.currencyCode,locale)}</dd><dt>{t("ledger")}</dt><dd className="break-words tabular-nums">{assetMoney(p.ledgerKnownValue?.minorUnits,p.ledgerKnownValue?.currencyCode,locale)}</dd><dt>{t("inventory")}</dt><dd>{p.inventoryCount.toString()}</dd></dl>
 <p role="status" className="text-sm">{t(!p.complete?"incomplete":p.matches?"matched":"mismatch")}</p>{p.issues.length>0&&<ul className="list-inside list-disc break-words text-sm">{p.issues.map((v,i)=><li key={`${v}:${i}`}>{t.has(`issues.${v}`)?t(`issues.${v}`):v}</li>)}</ul>}
 {p.complete&&p.matches&&<fieldset disabled={busy} className="space-y-3"><label className="block text-sm">{t("evidence")}<textarea className={field} maxLength={2000} value={evidence} onChange={e=>{setEvidence(e.target.value);setKey(crypto.randomUUID());save.reset();}} /></label><label className="flex items-start gap-2 text-sm"><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)} />{t("confirm")}</label><button type="button" className={button} disabled={!confirmed||!evidence.trim()} onClick={()=>save.mutate()}>{t("activate")}</button></fieldset>}
 </section>}
 </form></Modal>}</>;
}
