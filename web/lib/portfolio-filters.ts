"use client";
import {useEffect,useMemo,useState} from "react";
import {usePathname,useSearchParams} from "next/navigation";
// Filter changes need no server navigation. Next's native history integration
// keeps Back/Forward and useSearchParams coherent. Local state also updates
// controlled checkboxes immediately; all writers merge the current browser URL.
export function usePortfolioFilters(){
 const source=useSearchParams(),path=usePathname(),query=source.toString();
 const [current,setCurrent]=useState(query);
 useEffect(()=>setCurrent(query),[query]);
 const params=useMemo(()=>new URLSearchParams(current),[current]);
 const update=(patch:Record<string,string|string[]>)=>{
  const next=new URLSearchParams(window.location.search);
  for(const [key,value] of Object.entries(patch)){next.delete(key);for(const v of Array.isArray(value)?value:value?[value]:[])next.append(key,v);}
  setCurrent(next.toString());window.history.pushState(null,"",path+(next.size?"?"+next.toString():""));
 };
 return {params,update};
}
