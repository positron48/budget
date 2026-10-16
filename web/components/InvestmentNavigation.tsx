"use client";
import Link from "next/link";
import {useLocale} from "next-intl";
import {usePathname} from "next/navigation";
import {usePortfolioFilters} from "@/lib/portfolio-filters";

export default function InvestmentNavigation(){
 const ru=useLocale()==="ru",path=usePathname(),{params}=usePortfolioFilters();
 const links=[
  ["/investments","overview","Портфель","Portfolio"],
  ["/investments/analytics","","Рост","Growth"],
  ["/investments","trades","Сделки","Trades"],
  ["/investments","payments","Выплаты","Payouts"],
  ["/investments/calendar","","Календарь","Calendar"],
  ["/investments/instruments","","Найти бумагу","Find security"],
  ["/investments/groups","","Группы бумаг","Security groups"],
  ["/investments/accounts","","Счета","Accounts"],
  ["/investments/import","","Импорт","Import"],
 ];
 return <nav aria-label={ru?"Навигация инвестиций":"Investment navigation"} className="border-b border-slate-200 bg-white dark:border-slate-700 dark:bg-[#272832]">
  <div className="mx-auto flex max-w-[1480px] flex-wrap gap-x-5 px-4 lg:px-7">{links.map(([route,view,r,e])=>{
   const q=new URLSearchParams(params);q.delete("fx");q.delete("page");q.delete("view");if(view&&view!=="overview")q.set("view",view);
   const active=path===route&&(route!=="/investments"||(params.get("view")||"overview")===view);
   return <Link key={route+view} aria-current={active?"page":undefined} className={"border-b-2 py-4 text-sm font-medium "+(active?"border-emerald-600 text-emerald-700 dark:text-emerald-400":"border-transparent text-slate-500 hover:text-slate-900 dark:hover:text-slate-100")} href={route+(q.size?"?"+q.toString():"")}>{ru?r:e}</Link>;
  })}</div>
 </nav>;
}
