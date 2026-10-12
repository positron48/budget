"use client";
import {useEffect,useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useClients} from "@/app/providers";
import {useAssetTenant} from "@/lib/assets";
import {authStore,AUTH_CHANGED_EVENT} from "@/lib/auth/store";

// This is a navigation hint, not authorization. Every RPC retains the server's
// feature, rollout, membership and role checks.
export function useInvestmentAccess(enabled=true){
 const tenant=useAssetTenant();const {investment}=useClients();
 const [auth,setAuth]=useState({ready:false,generation:0});
 useEffect(()=>{
  const update=()=>setAuth(previous=>({ready:!!authStore.getAccess(),generation:previous.generation+1}));
  update();window.addEventListener(AUTH_CHANGED_EVENT,update);
  return ()=>window.removeEventListener(AUTH_CHANGED_EVENT,update);
 },[]);
 return useQuery({queryKey:["investment-access",tenant,auth.generation],enabled:enabled&&auth.ready&&!!tenant,retry:false,staleTime:30000,gcTime:0,refetchOnWindowFocus:false,queryFn:async()=>{await investment.listInvestmentLedgers({limit:1,offset:0});return true;}});
}
