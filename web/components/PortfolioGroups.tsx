"use client";
import {useRef,useState} from "react";
import {useLocale} from "next-intl";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import {Code,ConnectError} from "@connectrpc/connect";
import {create} from "@bufbuild/protobuf";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {useInvestmentReadOnly} from "@/components/InvestmentMode";
import {InvestmentGroupsSchema,InvestmentGroupSchema,type InvestmentGroups,type GetInvestmentGroupsResponse} from "@/proto/budget/v1/investment_nav_pb";
import {portfolioGroupsKey,removePortfolioGroup} from "@/lib/portfolio-groups";
import {loadBrokerCatalog} from "@/lib/portfolio-import";
import type {TenantMembership} from "@/proto/budget/v1/tenant_pb";
import {trackerError} from "@/lib/portfolio-tracker";
const input="rounded-lg border border-slate-300 bg-transparent px-3 py-2 text-sm dark:border-slate-700";
const button="rounded-lg border border-slate-300 px-3 py-2 text-sm dark:border-slate-700 disabled:opacity-40";
function Groups({tenantId}:{tenantId:string}){
 const ru=useLocale()==="ru",tr=(r:string,e:string)=>ru?r:e,{investmentNAV:api,investmentImport:imports,tenant}=useClients(),qc=useQueryClient(),readOnly=useInvestmentReadOnly();
 const roles=useQuery<{memberships:TenantMembership[]}>({queryKey:["investment",tenantId,"tracker","roles"],queryFn:()=>tenant.listMyTenants({})});
 const canWrite=!readOnly&&roles.data?.memberships.some(m=>m.tenant?.id===tenantId&&[1,2].includes(m.role));
 const list=useQuery<GetInvestmentGroupsResponse>({queryKey:portfolioGroupsKey(tenantId),queryFn:()=>api.getInvestmentGroups({})});
 const catalog=useQuery({queryKey:["investment",tenantId,"tracker","broker-quotes"],queryFn:()=>loadBrokerCatalog(imports),staleTime:60000});
 const [draft,setDraft]=useState<InvestmentGroups>(),[newName,setNewName]=useState(""),[search,setSearch]=useState(""),[account,setAccount]=useState(""),[dragged,setDragged]=useState<string>(),[dropTarget,setDropTarget]=useState<string>(),[busy,setBusy]=useState(false),[error,setError]=useState("");
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
 return <div className="mx-auto max-w-[1480px] space-y-5 px-4 py-8"><header className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="text-2xl font-semibold">{tr("Группы бумаг","Security groups")}</h1><p className="mt-2 text-sm text-slate-500">{tr("Перетащите бумагу в нужную группу.","Drag a security into a group.")}</p></div>{canWrite&&<div className="flex gap-2"><button className={button+" bg-emerald-600 text-white"} disabled={!draft||busy} onClick={()=>void save()}>{busy?tr("Сохраняем…","Saving…"):tr("Сохранить группы","Save groups")}</button><button className={button} disabled={!draft||busy} onClick={()=>{setDraft(undefined);pending.current=undefined;setError("");void list.refetch();}}>{tr("Отменить","Discard")}</button></div>}</header>
 {(error||list.error||roles.error)&&<p role="alert" className="text-sm text-rose-600">{error||trackerError(list.error||roles.error,ru)}</p>}
 {list.isPending&&<p role="status">{tr("Загружаем группы…","Loading groups…")}</p>}
 {current&&<><div className="flex flex-wrap items-center gap-3"><input className={input+" min-w-0 flex-1"} aria-label={tr("Найти бумагу","Find security")} placeholder={tr("Поиск бумаги или счёта","Search security or account")} value={search} onChange={e=>setSearch(e.target.value)}/><select className={input} aria-label={tr("Счёт для распределения","Assignment account")} value={account} onChange={e=>setAccount(e.target.value)}><option value="">{tr("Все счета","All accounts")}</option>{[...accounts].map(([id,title])=><option value={id} key={id}>{title}</option>)}</select>
 {canWrite&&<form className="flex min-w-0 w-full gap-2 sm:w-auto" onSubmit={e=>{e.preventDefault();add();}}><input disabled={busy} aria-label={tr("Новая группа","New group")} className={input+" min-w-0 flex-1"} maxLength={100} value={newName} onChange={e=>setNewName(e.target.value)} placeholder={tr("Название группы","Group name")}/><button className={button} type="submit" disabled={busy||!newName.trim()}>{tr("Добавить группу","Add group")}</button></form>}</div>
 <div className="flex gap-4 overflow-x-auto pb-4" aria-label={tr("Распределение по группам","Group assignment")}>
 {[{id:"",name:tr("Без группы","Ungrouped")},...current.groups].map(g=>{const items=positions.filter(p=>(memberships.get(p.ledgerId+"\0"+p.instrumentKey)||"")===g.id);return <section key={g.id} aria-label={g.name} data-group-id={g.id} className={"w-56 shrink-0 rounded-xl border p-3 "+(dropTarget===g.id?"border-sky-400 bg-sky-50 dark:bg-sky-950":"border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-[#30313d]")} onDragOver={e=>{if(canWrite&&!busy&&dragged){e.preventDefault();e.dataTransfer.dropEffect="move";setDropTarget(g.id);}}} onDragLeave={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node|null))setDropTarget(undefined);}} onDrop={e=>{e.preventDefault();if(canWrite&&!busy&&dragged){const p=(list.data?.positions||[]).find(p=>p.ledgerId+"\0"+p.instrumentKey===dragged);if(p)assign(p.ledgerId,p.instrumentKey,g.id);}setDragged(undefined);setDropTarget(undefined);}}>
 <header className="mb-3 flex items-center gap-2">{g.id?<><input disabled={!canWrite||busy} className="min-w-0 flex-1 bg-transparent font-medium outline-none focus:underline" aria-label={tr("Название группы","Group name")+": "+g.name} value={g.name} maxLength={100} onChange={e=>change({...current,groups:current.groups.map(row=>row.id===g.id?{...row,name:e.target.value}:row)})}/>{canWrite&&<button disabled={busy} className="px-1 text-slate-400 hover:text-rose-500" aria-label={tr("Удалить группу","Delete group")+": "+g.name} onClick={()=>change(removePortfolioGroup(current,g.id))}>×</button>}</>:<span className="flex-1 font-medium">{g.name}</span>}<span className="text-xs text-slate-500">{items.length}</span></header>
 <ul className="max-h-[65vh] min-h-40 space-y-2 overflow-y-auto" aria-label={tr("Бумаги в группе","Securities in group")+": "+g.name}>{items.map(p=>{const key=p.ledgerId+"\0"+p.instrumentKey;return <li key={key} draggable={!!canWrite&&!busy} onDragStart={e=>{setDragged(key);e.dataTransfer.effectAllowed="move";e.dataTransfer.setData("text/plain",key);}} onDragEnd={()=>{setDragged(undefined);setDropTarget(undefined);}} className={"rounded-lg border border-slate-100 bg-white p-3 shadow-sm dark:border-slate-700 dark:bg-slate-800 "+(dragged===key?"opacity-40":"")}><div className="flex justify-between gap-2"><span className="font-medium">{name(p.instrumentKey)}</span>{canWrite&&<span aria-hidden="true" className="cursor-grab text-slate-400">⠿</span>}</div><p className="mt-1 truncate text-xs text-slate-500">{p.ledgerName}</p><select className="mt-3 w-full rounded border border-slate-200 bg-transparent px-2 py-1 text-xs dark:border-slate-700" disabled={!canWrite||busy} aria-label={tr("Группа для","Group for")+" "+name(p.instrumentKey)+" · "+p.ledgerName} value={memberships.get(key)||""} onChange={e=>assign(p.ledgerId,p.instrumentKey,e.target.value)}><option value="">{tr("Без группы","Ungrouped")}</option>{current.groups.map(g=><option key={g.id} value={g.id}>{g.name}</option>)}</select></li>;})}</ul>
 </section>;})}
 </div><p className="text-xs text-slate-500">{tr("Распределение сохраняется отдельно для каждого счёта. Закрытые позиции остаются в истории.","Groups are assigned per account. Closed positions remain in the history.")}</p>
 </>}
 </div>;
}
export default function PortfolioGroups(){const tenant=useAssetTenant();return tenant?<Groups key={tenant} tenantId={tenant}/>:null;}
