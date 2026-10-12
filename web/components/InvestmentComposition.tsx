"use client";
import InvestmentChart from "@/components/InvestmentChart";
import {useState} from "react";
import {useSearchParams,useRouter,usePathname} from "next/navigation";
import {useQuery} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {useClients} from "@/app/providers";
import {navDecimal,navGeometry} from "@/lib/investment-nav";
import type {GetInvestmentCompositionHistoryResponse,GetInvestmentGroupsResponse} from "@/proto/budget/v1/investment_nav_pb";
const button="rounded-lg border border-gray-300 px-3 py-2 text-sm disabled:opacity-50 dark:border-gray-700";
const colors=["#2563eb","#059669","#d97706","#9333ea","#e11d48","#0891b2","#64748b"];
export default function InvestmentComposition({tenantId,seriesKey,from,to}:{tenantId:string;seriesKey:string;from:string;to:string}){
 const quality=useTranslations("investmentNAV");
 const t=useTranslations("investmentComposition"),{investmentNAV:api}=useClients(),params=useSearchParams(),router=useRouter(),pathname=usePathname();
 const dimensionParam=params.get("composition")||"group";
 const dimension=["group","account","currency","type"].includes(dimensionParam)?dimensionParam:"group";
 const selected=Array.from(new Set(params.getAll("group"))).sort(),[offset,setOffset]=useState(0);
 const change=(values:string[])=>{const q=new URLSearchParams(params.toString());q.delete("group");values.forEach(id=>q.append("group",id));setOffset(0);router.replace(`${pathname}?${q}`,{scroll:false});};
 const groups=useQuery<GetInvestmentGroupsResponse>({queryKey:["investmentGroups",tenantId],queryFn:()=>api.getInvestmentGroups({}),refetchInterval:15000});
 const history=useQuery<GetInvestmentCompositionHistoryResponse>({queryKey:["investmentComposition",tenantId,seriesKey,from,to,selected,dimension,groups.data?.classification?.version],enabled:!!groups.data?.classification,queryFn:()=>api.getInvestmentCompositionHistory({seriesKey,fromDate:from,toDate:to,groupIds:selected,breakdownBy:dimension}),refetchInterval:15000});
 const points=history.data?.points||[],latest=points.at(-1),segments=history.data?.segments||[],currency=history.data?.currency||"";
 const geometry=navGeometry(points.map(p=>({date:p.date,state:p.securitiesComplete?"ready":p.state==="ready"?"incomplete":p.state,knownValue:p.securitiesKnown})));
 const label=(id:string,kind:string)=>{
 if(kind==="cash")return t("cash");
 if(dimension==="account")return groups.data?.positions.find(p=>p.ledgerId===id)?.ledgerName||id;
 if(dimension==="currency")return id==="unknown"?t("unknown"):id;
 if(dimension==="type")return t.has(`types.${id}`)?t(`types.${id}`):t("unknown");
 return id?(history.data?.groups.find(g=>g.id===id)?.name||id):t("ungrouped");
 };
 const weights=history.data?.complete&&segments.length>0&&segments.every(s=>s.weight&&Number(s.weight.decimal)>=0&&Number(s.weight.decimal)<=1);
 let start=0;
 return <section className="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-gray-800">
  <h2 className="text-xl font-semibold">{t("title")}</h2><p className="text-sm text-gray-500">{t("policy")}</p>
  <fieldset className="flex flex-wrap gap-3"><legend className="mb-2 font-medium">{t("filter")}</legend>{[{id:"",name:t("ungrouped")},...(groups.data?.classification?.groups||[])].map(g=><label key={g.id} className="flex max-w-full min-w-0 items-center gap-2"><input type="checkbox" checked={selected.includes(g.id)} onChange={e=>change(e.target.checked?[...selected,g.id]:selected.filter(id=>id!==g.id))}/><span className="min-w-0 break-all">{g.name}</span></label>)}</fieldset>
  {selected.length>0&&<button className={button} onClick={()=>change([])}>{t("all")}</button>}
  <p className="text-sm">{t("scope")}</p>
  {(groups.error||history.error)&&<p role="alert">{t("error")} <button className={button} onClick={()=>{change([]);void groups.refetch();void history.refetch();}}>{t("reload")}</button></p>}
  {(groups.isPending||history.isFetching)&&<p role="status">{t("loading")}</p>}
  {history.data&&<>
   <h3 className="font-semibold">{t("securities")}</h3><p className="break-all font-mono tabular-nums">{latest?.securitiesKnown?`${navDecimal(latest.securitiesKnown.decimal)} ${currency}`:"—"}{latest?.securitiesKnown&&!latest.securitiesComplete&&<span className="ml-2 font-sans text-sm">{t("subtotal")}</span>}</p>
   <p className="text-sm">{t("cashSeparate")}: {latest?.cashKnown?`${navDecimal(latest.cashKnown.decimal)} ${currency}`:"—"}{latest?.cashKnown&&!latest.cashComplete&&` · ${t("subtotal")}`}</p>
   {geometry.dots.length>0?<figure><InvestmentChart role="img" aria-label={t("securities")} viewBox="0 0 800 270" className="w-full"><title>{t("securities")}</title>{geometry.paths.map((d,i)=><path key={i} d={d} fill="none" stroke="#2563eb" strokeWidth="2"/>)}{geometry.dots.map(p=><circle key={p.index} cx={p.x} cy={p.y} r="3" fill="#2563eb"><title>{points[p.index].date}: {navDecimal(points[p.index].securitiesKnown!.decimal)} {currency}</title></circle>)}</InvestmentChart><figcaption className="flex justify-between text-xs"><span>{from}</span><span>{to}</span></figcaption></figure>:<p>{t("noComplete")}</p>}
   {latest?.issues.some(i=>i.code==="component_breakdown_missing")&&<p role="status">{t("refreshRequired")}</p>}
   <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="font-semibold">{t("composition",{date:latest?.date||to})}</h3><label>{t("breakdown")} <select aria-label={t("breakdown")} className={button} value={dimension} onChange={e=>{const q=new URLSearchParams(params.toString());q.set("composition",e.target.value);router.replace(`${pathname}?${q}`,{scroll:false});}}>{["group","account","currency","type"].map(v=><option key={v} value={v}>{t(`dimensions.${v}`)}</option>)}</select></label></div>
   {!weights&&<p className="text-sm">{t("noWeights")}</p>}
   {weights&&<InvestmentChart role="img" aria-label={t("pie")} viewBox="0 0 140 140" className="mx-auto h-48 w-48"><title>{t("pie")}</title>{segments.map((s,i)=>{const weight=Number(s.weight!.decimal)*100,offset=start;start+=weight;return <circle key={`${s.kind}:${s.categoryKey}`} cx="70" cy="70" r="50" fill="none" stroke={colors[i%colors.length]} strokeWidth="24" pathLength="100" strokeDasharray={`${weight} ${100-weight}`} strokeDashoffset={-offset} transform="rotate(-90 70 70)"><title>{label(s.categoryKey||s.groupId,s.kind)}: {(weight).toFixed(2)}%</title></circle>;})}</InvestmentChart>}
   <div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr><th className="p-2">{t(`dimensions.${dimension}`)}</th><th className="p-2">{t("value")}</th><th className="p-2">{t("weight")}</th></tr></thead><tbody>{segments.map((s,i)=><tr key={`${s.kind}:${s.categoryKey}`}><td className="p-2"><span aria-hidden="true" className="mr-2 inline-block h-3 w-3 rounded" style={{backgroundColor:colors[i%colors.length]}}/>{label(s.categoryKey||s.groupId,s.kind)}</td><td className="p-2 font-mono">{s.knownValue?`${navDecimal(s.knownValue.decimal)} ${currency}`:"—"}{!s.complete&&<span className="block font-sans text-xs">{t("subtotal")}</span>}</td><td className="p-2">{s.weight?`${(Number(s.weight.decimal)*100).toFixed(2)}%`:"—"}</td></tr>)}</tbody></table></div>
   <details><summary className="cursor-pointer">{t("daily")}</summary><div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr><th className="p-2">{t("date")}</th><th className="p-2">{t("securities")}</th><th className="p-2">{t("cash")}</th><th className="p-2">{t("issues")}</th></tr></thead><tbody>{points.slice(offset,offset+50).map(p=><tr key={p.date}><td className="p-2 whitespace-nowrap">{p.date}</td><td className="p-2 font-mono">{p.securitiesKnown?navDecimal(p.securitiesKnown.decimal):"—"}{p.securitiesKnown&&!p.securitiesComplete&&<span className="block font-sans text-xs">{t("subtotal")}</span>}</td><td className="p-2 font-mono">{p.cashKnown?navDecimal(p.cashKnown.decimal):"—"}{p.cashKnown&&!p.cashComplete&&<span className="block font-sans text-xs">{t("subtotal")}</span>}</td><td className="p-2">{p.issues.map((issue,i)=><p key={i} className="break-all">{quality.has(`issues.${issue.code}`)?quality(`issues.${issue.code}`):quality("issue",{code:issue.code})}</p>)}</td></tr>)}</tbody></table></div><div className="mt-3 flex gap-3"><button className={button} disabled={offset===0} onClick={()=>setOffset(v=>v-50)}>{t("previous")}</button><button className={button} disabled={offset+50>=points.length} onClick={()=>setOffset(v=>v+50)}>{t("next")}</button></div></details>
  </>}
 </section>;
}
