"use client";
import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import InvestmentMutationPreview from "@/components/InvestmentMutationPreview";
import InvestmentTransferTimingFields from "@/components/InvestmentTransferTimingFields";
import {transferTimePayload} from "@/lib/investment-transfer-time";
import Modal from "@/components/Modal";
import type { InvestmentLedger, InvestmentSnapshot, InvestmentTransfer, PreviewInvestmentTransferResponse } from "@/proto/budget/v1/investment_pb";

const field = "w-full rounded-lg border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-900";
const button = "rounded-lg bg-blue-600 px-4 py-2 text-sm text-white disabled:opacity-50";
const localDate = (d: Date) => new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0,19);

export default function InvestmentTransferDialog({tenantId,fromLedgerId,transferId,onClose,onSaved}:{tenantId:string;fromLedgerId:string;transferId?:string;onClose:()=>void;onSaved:()=>void}) {
 const t=useTranslations("investmentTransfer"),pt=useTranslations("investmentPreview");
 const {investment:api}=useClients();
 const [from,setFrom]=useState(fromLedgerId);
 const [to,setTo]=useState("");
 const [amount,setAmount]=useState("");
 const [security,setSecurity]=useState(false);
 const [instrument,setInstrument]=useState("");
 const [quantity,setQuantity]=useState("");
 const [currency,setCurrency]=useState("RUB");
 const [dateOnly,setDateOnly]=useState(false),[timezone,setTimezone]=useState("Europe/Moscow");
 const [at,setAt]=useState(localDate(new Date()));
 const [note,setNote]=useState("");
 const [reason,setReason]=useState("");
 const [key,setKey]=useState(()=>crypto.randomUUID());
 const [offset,setOffset]=useState(0);
 const [initialized,setInitialized]=useState(false);
 const existing=useQuery<{transfer?:InvestmentTransfer}>({queryKey:["investment",tenantId,"transfer",transferId],enabled:!!transferId,refetchOnWindowFocus:false,refetchOnMount:"always",queryFn:()=>api.getInvestmentTransfer({id:transferId!})});
 useEffect(()=>{const v=existing.data?.transfer;if(!v || initialized || existing.isFetching || existing.error)return;setInitialized(true);setFrom(v.fromLedgerId);setTo(v.toLedgerId);setAmount(v.amount);setSecurity(!!v.instrumentKey);setInstrument(v.instrumentKey);setQuantity(v.quantity);setCurrency(v.currency);setDateOnly(!!v.timing?.occurredDate);setTimezone(v.timing?.timezone||"Europe/Moscow");setAt(v.timing?.occurredDate||localDate(new Date(Number(v.occurredAt!.seconds)*1000+v.occurredAt!.nanos/1000000)));setNote(v.note);},[existing.data,existing.isFetching,existing.error,initialized]);
 const ledgers=useQuery<{ledgers:InvestmentLedger[]}>({queryKey:["investment",tenantId,"transfer-ledgers",offset],queryFn:()=>api.listInvestmentLedgers({limit:200,offset})});
 const source=useQuery<{snapshot?:InvestmentSnapshot}>({queryKey:["investment",tenantId,"transfer-balance",from],enabled:!!from,refetchOnWindowFocus:false,queryFn:()=>api.getInvestmentSnapshot({ledgerId:from})});
 const target=useQuery<{snapshot?:InvestmentSnapshot}>({queryKey:["investment",tenantId,"transfer-balance",to],enabled:!!to,refetchOnWindowFocus:false,queryFn:()=>api.getInvestmentSnapshot({ledgerId:to})});
 const changed=()=>{setKey(crypto.randomUUID());mutation.reset();};
 const signature=JSON.stringify({from,to,amount,security,instrument,quantity,currency,at,dateOnly,timezone,note,reason,key,fromVersion:source.data?.snapshot?.ledger?.version,toVersion:target.data?.snapshot?.ledger?.version,version:existing.data?.transfer?.version},(_k,v)=>typeof v==="bigint"?v.toString():v);
 const payload=(action:string)=>({action,transfer:{id:transferId||"",fromLedgerId:from,toLedgerId:to,currency,amount:security?"0":amount,instrumentKey:security?instrument.trim():"",quantity:security?quantity:"",note,...(action==="void"?{}:transferTimePayload(at,dateOnly,timezone,existing.data?.transfer?.occurredAt,existing.data?.transfer?.timing))},expectedVersion:existing.data?.transfer?.version||0n,expectedFromVersion:source.data?.snapshot?.ledger?.version||0n,expectedToVersion:target.data?.snapshot?.ledger?.version||0n,requestKey:key,reason});
 const preview=useMutation<PreviewInvestmentTransferResponse,Error,{action:string;signature:string}>({mutationFn:({action}:{action:string;signature:string})=>api.previewInvestmentTransfer(payload(action))});
 const reviewed=(action:string)=>!preview.isPending&&!!preview.data?.preview?.valid&&!!preview.data.preview.previewHash&&preview.variables?.signature===signature&&preview.variables?.action===action;
 const mutation=useMutation({mutationFn:(action:string)=>{if(!reviewed(action))throw new Error(pt("required"));return api.mutateInvestmentTransfer({...payload(action),previewHash:preview.data!.preview!.previewHash})},onError:(e)=>{if([9,10].includes((e as {code?:number}).code||0))preview.reset()},onSuccess:()=>{onSaved();onClose();}});
 const error=existing.error||ledgers.error||source.error||target.error||mutation.error||preview.error;
 const ready=!!source.data?.snapshot && !!target.data?.snapshot && !source.isFetching && !target.isFetching && !source.error && !target.error && (!transferId||(initialized&&!existing.isFetching&&!existing.error)) && from!==to;
 const voided=existing.data?.transfer?.status==="voided";
 return <Modal open title={t("title")} onClose={()=>{if(!mutation.isPending)onClose();}} maxWidthClass="max-w-2xl"><form className="space-y-4" onSubmit={e=>{e.preventDefault();mutation.mutate(transferId?"amend":"create");}}>
  <p className="text-sm text-gray-500">{t("help")}</p>
  {error && <p role="alert" className="text-red-600">{String(error)}</p>}
  <p className="text-sm">{t("from")}: {source.data?.snapshot?.ledger?.name||from}</p>
  {transferId?<p className="text-sm">{t("to")}: {target.data?.snapshot?.ledger?.name||to}</p>:<><label className="block text-sm">{t("to")}<select className={field} required disabled={mutation.isPending} value={to} onChange={e=>{setTo(e.target.value);changed();}}><option value="">—</option>{ledgers.data?.ledgers.filter(l=>l.id!==from).map(l=><option key={l.id} value={l.id}>{l.name}</option>)}</select></label>{(offset>0||ledgers.data?.ledgers.length===200)&&<div className="flex gap-3"><button type="button" disabled={!offset||mutation.isPending} onClick={()=>{setOffset(x=>x-200);setTo("");changed();}}>{t("previous")}</button><button type="button" disabled={ledgers.data?.ledgers.length!==200||mutation.isPending} onClick={()=>{setOffset(x=>x+200);setTo("");changed();}}>{t("next")}</button></div>}</>}
  {voided?<p>{t("voided")}</p>:<fieldset disabled={mutation.isPending|| (!!transferId&&!initialized)} className="space-y-3">
   <label className="block text-sm">{t("type")}<select className={field} value={security?"security":"cash"} disabled={!!transferId} onChange={e=>{setSecurity(e.target.value==="security");changed();}}><option value="cash">{t("cash")}</option><option value="security">{t("security")}</option></select></label>
   {security&&<><p className="text-sm text-gray-500">{t("securityHelp")}</p><label className="block text-sm">{t("instrument")}<input className={field} required maxLength={200} list="transfer-instruments" value={instrument} onChange={e=>{setInstrument(e.target.value);changed();}} /></label><datalist id="transfer-instruments">{source.data?.snapshot?.positions.map(p=><option key={`${p.instrumentKey}:${p.currency}`} value={p.instrumentKey}>{p.quantity} {p.currency}</option>)}</datalist><label className="block text-sm">{t("quantity")}<input className={field} required inputMode="decimal" maxLength={128} value={quantity} onChange={e=>{setQuantity(e.target.value);changed();}} /></label></>}
   <label className="block text-sm">{t(security?"bookCurrency":"currency")}<select className={field} value={currency} onChange={e=>{setCurrency(e.target.value);changed();}}>{["RUB","USD","EUR"].map(c=><option key={c}>{c}</option>)}</select></label>
   {!security&&<label className="block text-sm">{t("amount")}<input className={field} required inputMode="decimal" value={amount} onChange={e=>{setAmount(e.target.value);changed();}} /></label>}
   <InvestmentTransferTimingFields at={at} dateOnly={dateOnly} timezone={timezone} onChange={v=>{setAt(v.at);setDateOnly(v.dateOnly);setTimezone(v.timezone);changed()}}/>
   <label className="block text-sm">{t("note")}<textarea className={field} maxLength={5000} value={note} onChange={e=>{setNote(e.target.value);changed();}} /></label>
   {transferId&&<label className="block text-sm">{t("reason")}<textarea className={field} required maxLength={2000} value={reason} onChange={e=>{setReason(e.target.value);changed();}} /></label>}
   <div className="space-y-3"><button type="button" className={button} disabled={!ready||preview.isPending||(!!transferId&&!reason.trim())} onClick={()=>preview.mutate({action:transferId?"amend":"create",signature})}>{pt("check")}</button>{transferId&&<button type="button" className="ml-3 text-red-600" disabled={!ready||preview.isPending||!reason.trim()} onClick={()=>preview.mutate({action:"void",signature})}>{pt("checkVoid")}</button>}{preview.data?.preview&&preview.variables?.signature===signature&&<InvestmentMutationPreview value={preview.data.preview}/>}</div>
   <div className="flex flex-wrap gap-3"><button className={button} disabled={!ready||!reviewed(transferId?"amend":"create")||(!!transferId&&!reason.trim())}>{t("save")}</button>{transferId&&<button type="button" className="text-red-600 disabled:opacity-50" disabled={!ready||!reason.trim()||!reviewed("void")} onClick={()=>mutation.mutate("void")}>{t("void")}</button>}<button type="button" onClick={()=>{source.refetch();if(to)target.refetch();changed();}}>{t("refresh")}</button></div>
  </fieldset>}
 </form></Modal>;
}
