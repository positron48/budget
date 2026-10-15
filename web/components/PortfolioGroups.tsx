"use client";
import {useRef,useState} from "react";
import Link from "next/link";
import {useLocale} from "next-intl";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import {Code,ConnectError} from "@connectrpc/connect";
import {create} from "@bufbuild/protobuf";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import {InvestmentGroupsSchema,InvestmentGroupSchema,type InvestmentGroups,type GetInvestmentGroupsResponse} from "@/proto/budget/v1/investment_nav_pb";
import {usePortfolioFilters} from "@/lib/portfolio-filters";
import {portfolioGroupsKey,removePortfolioGroup} from "@/lib/portfolio-groups";
import {loadBrokerCatalog} from "@/lib/portfolio-import";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";
import {trackerError} from "@/lib/portfolio-tracker";
const input="rounded-lg border border-slate-300 bg-transparent px-3 py-2 text-sm dark:border-slate-700";
const button="rounded-lg border border-slate-300 px-3 py-2 text-sm dark:border-slate-700 disabled:opacity-40";
function Groups({tenantId}:{tenantId:string}){
 const ru=useLocale()==="ru",tr=(r:string,e:string)=>ru?r:e,{investmentNAV:api,investmentImport:imports,tenant}=useClients(),qc=useQueryClient(),readOnly=useInvestmentReadOnly();
 const {params}=usePortfolioFilters();
 const roles=useQuery<{memberships:TenantMembership[]}>({queryKey:["investment",tenantId,"tracker","roles"],queryFn:()=>tenant.listMyTenants({})});
 const canWrite=!readOnly&&roles.data?.memberships.some(m=>m.tenant?.id===tenantId&&[1,2].includes(m.role));
 const list=useQuery<GetInvestmentGroupsResponse>({queryKey:portfolioGroupsKey(tenantId),queryFn:()=>api.getInvestmentGroups({})});
 const catalog=useQuery({queryKey:["investment",tenantId,"tracker","broker-quotes"],queryFn:()=>loadBrokerCatalog(imports),staleTime:60000});
 const [draft,setDraft]=useState<InvestmentGroups>(),[newName,setNewName]=useState(""),[search,setSearch]=useState(""),[account,setAccount]=useState(""),[page,setPage]=useState(0),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const pending=useRef<{signature:string;key:string}|undefined>(undefined);
 const current=draft||list.data?.classification;
 const change=(value:InvestmentGroups)=>{setDraft(value);setError("");};
 const add=()=>{if(!current||!newName.trim())return;const name=newName.trim();if(current.groups.length>=100||current.groups.some(g=>g.name.toLocaleLowerCase()===name.toLocaleLowerCase())){setError(tr("Название группы должно быть уникальным; максимум 100 групп.","Use a unique group name; at most 100 groups are allowed."));return;}change(create(InvestmentGroupsSchema,{...current,groups:[...current.groups,create(InvestmentGroupSchema,{id:crypto.randomUUID(),name})]}));setNewName("");};
 const assign=(ledgerId:string,instrumentKey:string,groupId:string)=>{if(!current)return;const memberships=current.memberships.filter(m=>m.ledgerId!==ledgerId||m.instrumentKey!==instrumentKey);if(groupId)memberships.push({$typeName:"budget.v1.InvestmentGroupMembership",ledgerId,instrumentKey,groupId});change({...current,memberships});};
 const save=async()=>{if(!draft||!canWrite)return;setBusy(true);setError("");try{
  const signature=JSON.stringify([draft.version.toString(),draft.groups,draft.memberships]);if(pending.current?.signature!==signature)pending.current={signature,key:crypto.randomUUID()};
  const result=await api.saveInvestmentGroups({classification:draft,requestKey:pending.current.key,reason:"User updated portfolio groups"});
  if(!result.classification)throw new Error("classification_missing");
  qc.setQueryData(portfolioGroupsKey(tenantId),{...list.data,classification:result.classification});setDraft(undefined);pending.current=undefined;
  await qc.invalidateQueries({queryKey:["investment",tenantId,"tracker-history"]});
  await qc.invalidateQueries({queryKey:["investment",tenantId,"tracker-contributions"]});
 }catch(e){setError(e instanceof ConnectError&&e.code===Code.Aborted?tr("Группы уже изменились. Обновите данные перед повторным сохранением.","Groups changed elsewhere. Reload before saving again."):trackerError(e,ru));}finally{setBusy(false);}};
 const name=(key:string)=>catalog.data?.names.get(key)?.ticker||key.replace(/^tinvest:/,"");
 const positions=(list.data?.positions||[]).filter(p=>(!account||p.ledgerId===account)&&(!search||[name(p.instrumentKey),p.instrumentKey,p.ledgerName].some(s=>s.toLocaleLowerCase().includes(search.toLocaleLowerCase()))));
 const accounts=new Map((list.data?.positions||[]).map(p=>[p.ledgerId,p.ledgerName]));
 const memberships=new Map((current?.memberships||[]).map(m=>[m.ledgerId+"\0"+m.instrumentKey,m.groupId]));
 return <div className="mx-auto max-w-5xl space-y-6 px-4 py-8"><header className="flex flex-wrap items-center justify-between gap-3"><h1 className="text-2xl font-semibold">{tr("Группы бумаг","Security groups")}</h1><Link className={button} href={"/investments"+(params.toString()?"?"+params.toString():"")}>{tr("К портфелю","Back to portfolio")}</Link></header>
 <p className="text-sm text-slate-500">{tr("Одна бумага может входить в разные группы на разных счетах. Текущее распределение применяется ко всей истории. Переименование и удаление групп не меняют сделки; при удалении бумаги переходят в «Без группы».","A security may belong to different groups in different accounts. The current classification applies to all history. Renaming or deleting groups does not change trades; deleted groups leave their securities ungrouped.")}</p>
 {(error||list.error||roles.error)&&<p role="alert" className="text-sm text-rose-600">{error||trackerError(list.error||roles.error,ru)}</p>}
 {list.isPending&&<p role="status">{tr("Загружаем группы…","Loading groups…")}</p>}
 {current&&<><fieldset disabled={!canWrite||busy} className="space-y-3"><div className="grid gap-3 sm:grid-cols-2">{current.groups.map(g=><div className="flex gap-2" key={g.id}><input className={input+" min-w-0 flex-1"} aria-label={tr("Название группы","Group name")+": "+g.name} maxLength={100} value={g.name} onChange={e=>change({...current,groups:current.groups.map(row=>row.id===g.id?{...row,name:e.target.value}:row)})}/><button className={button} onClick={()=>change(removePortfolioGroup(current,g.id))}>{tr("Удалить","Delete")}</button></div>)}</div>
 {canWrite&&<form className="flex flex-wrap gap-2" onSubmit={e=>{e.preventDefault();add();}}><input aria-label={tr("Новая группа","New group")} className={input+" min-w-0 flex-1"} maxLength={100} value={newName} onChange={e=>setNewName(e.target.value)} placeholder={tr("Например: Облигации","For example: Bonds")}/><button className={button} type="submit" disabled={!newName.trim()}>{tr("Добавить группу","Add group")}</button></form>}</fieldset>
 <div className="flex flex-wrap gap-2"><input className={input} aria-label={tr("Найти бумагу","Find security")} placeholder={tr("Поиск бумаги или счёта","Search security or account")} value={search} onChange={e=>{setSearch(e.target.value);setPage(0);}}/><select className={input} aria-label={tr("Счёт для распределения","Assignment account")} value={account} onChange={e=>{setAccount(e.target.value);setPage(0);}}><option value="">{tr("Все счета","All accounts")}</option>{[...accounts].map(([id,title])=><option value={id} key={id}>{title}</option>)}</select></div>
 <p className="text-xs text-slate-500">{tr("В списке сохранены и закрытые позиции.","Closed positions remain in this list.")}</p>
 <div className="overflow-x-auto rounded-2xl border border-slate-200 dark:border-slate-800"><table className="w-full text-left text-sm"><thead><tr>{[tr("Бумага","Security"),tr("Счёт","Account"),tr("Группа","Group")].map(h=><th className="p-3" key={h}>{h}</th>)}</tr></thead><tbody>{positions.slice(page*50,(page+1)*50).map(p=><tr key={p.ledgerId+":"+p.instrumentKey} className="border-t border-slate-100 dark:border-slate-800"><td className="max-w-64 break-all p-3">{name(p.instrumentKey)}</td><td className="p-3">{p.ledgerName}</td><td className="p-3"><select className={input+" max-w-48"} disabled={!canWrite||busy} aria-label={tr("Группа для","Group for")+" "+name(p.instrumentKey)+" · "+p.ledgerName} value={memberships.get(p.ledgerId+"\0"+p.instrumentKey)||""} onChange={e=>assign(p.ledgerId,p.instrumentKey,e.target.value)}><option value="">{tr("Без группы","Ungrouped")}</option>{current.groups.map(g=><option key={g.id} value={g.id}>{g.name}</option>)}</select></td></tr>)}</tbody></table></div>
 <div className="flex flex-wrap items-center gap-3 text-sm"><span>{positions.length} {tr("позиций","positions")}</span><button className={button} disabled={page===0} onClick={()=>setPage(p=>p-1)}>{tr("Назад","Previous")}</button><button className={button} disabled={(page+1)*50>=positions.length} onClick={()=>setPage(p=>p+1)}>{tr("Далее","Next")}</button></div>
 {canWrite&&<div className="flex flex-wrap gap-3"><button className={button+" bg-emerald-600 text-white"} disabled={!draft||busy} onClick={()=>void save()}>{busy?tr("Сохраняем…","Saving…"):tr("Сохранить группы","Save groups")}</button><button className={button} disabled={busy} onClick={()=>{setDraft(undefined);pending.current=undefined;setError("");void list.refetch();}}>{tr("Отменить и обновить","Discard and reload")}</button></div>}
 </>}
 </div>;
}
export default function PortfolioGroups(){const tenant=useAssetTenant();return tenant?<Groups key={tenant} tenantId={tenant}/>:null;}
