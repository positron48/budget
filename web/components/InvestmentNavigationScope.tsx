"use client";
import {useEffect,useState} from "react";
import {usePathname,useSearchParams,useRouter} from "next/navigation";
import {useQueryClient} from "@tanstack/react-query";
import {useTranslations} from "next-intl";
import {authStore,TENANT_CHANGED_EVENT} from "@/lib/auth/store";

// This wrapper stays mounted outside the access gate. Switching organization
// removes the previous investment selection before mounting its next view.
export default function InvestmentNavigationScope({children}:{children:React.ReactNode}){
 const router=useRouter(),pathname=usePathname(),params=useSearchParams(),qc=useQueryClient(),t=useTranslations("investmentAccess");
 const [switching,setSwitching]=useState(false);
 useEffect(()=>{
  let previous=authStore.getTenant();
  const change=()=>{
   const next=authStore.getTenant();if(next===previous)return;previous=next;
   setSwitching(true);
   const filters={predicate:(query:{queryKey:readonly unknown[]})=>/^(investment|market|instrumentPrices|broker-)/.test(String(query.queryKey[0]))};
   void qc.cancelQueries(filters).then(()=>qc.removeQueries(filters));
   router.replace("/investments/value",{scroll:false});
  };
  window.addEventListener(TENANT_CHANGED_EVENT,change);
  return ()=>window.removeEventListener(TENANT_CHANGED_EVENT,change);
 },[router,qc]);
 useEffect(()=>{if(switching&&pathname==="/investments/value"&&!params.toString())setSwitching(false);},[switching,pathname,params]);
 return switching?<p role="status" className="m-6">{t("loading")}</p>:children;
}
