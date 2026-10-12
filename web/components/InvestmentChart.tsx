"use client";

import {useEffect,useId,useRef,useState,type SVGProps} from "react";
import {useTranslations} from "next-intl";

// One tab stop per chart, including large daily series. The same exact labels
// serve pointer users, keyboard users and screen readers; gaps have no shapes
// and therefore cannot become fabricated zero-valued selectable points.
export default function InvestmentChart({children,...props}:SVGProps<SVGSVGElement>){
 const t=useTranslations("investmentChart"),id=useId(),svg=useRef<SVGSVGElement>(null),signature=useRef("");
 const [selection,setSelection]=useState<{index:number;text:string}>({index:-1,text:""});
 const shapes=()=>Array.from(svg.current?.querySelectorAll<SVGElement>("circle,rect")||[])
  .filter(node=>node.querySelector("title")?.textContent?.trim())
  .sort((a,b)=>Number(a.getAttribute("cx")||a.getAttribute("x")||0)-Number(b.getAttribute("cx")||b.getAttribute("x")||0));
 const select=(index:number)=>{
  const nodes=shapes(),node=nodes[index];
  for(const item of nodes)item.removeAttribute("data-investment-chart-active");
  if(node)node.setAttribute("data-investment-chart-active","true");
  setSelection({index:node?index:-1,text:node?.querySelector("title")?.textContent||""});
 };
 useEffect(()=>{
  const nodes=shapes(),next=JSON.stringify(nodes.map(node=>[node.tagName,node.querySelector("title")?.textContent,...["cx","cy","x","y","width","height"].map(key=>node.getAttribute(key))]));
  if(next===signature.current)return;
  signature.current=next;
  // A new series/mode must not leave the preceding point's value on screen.
  for(const node of nodes)node.removeAttribute("data-investment-chart-active");
  setSelection({index:-1,text:""});
 },[children]);
 return <div className="min-w-0 space-y-2" data-testid="investment-chart">
  <svg {...props} ref={svg} tabIndex={0} aria-describedby={id} className={`${props.className||""} rounded focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500`} onFocus={()=>select(0)} onKeyDown={event=>{
   const count=shapes().length;if(!count)return;
   let next=selection.index;
   if(event.key==="ArrowRight"||event.key==="ArrowDown")next=Math.min(count-1,next+1);
   else if(event.key==="ArrowLeft"||event.key==="ArrowUp")next=Math.max(0,next-1);
   else if(event.key==="Home")next=0;
   else if(event.key==="End")next=count-1;
   else if(event.key==="Escape")next=-1;
   else return;
   event.preventDefault();select(next);
  }} onPointerOver={event=>{
   const node=(event.target as Element).closest?.("circle,rect");if(!node)return;
   const index=shapes().indexOf(node as SVGElement);if(index>=0)select(index);
  }}>{children}</svg>
  <p id={id} className="text-xs text-gray-500">{t("help")}</p>
  <p aria-live="polite" aria-atomic="true" className="min-h-5 break-words text-sm tabular-nums" data-testid="investment-chart-selection">{selection.text||t("noSelection")}</p>
  <style>{`[data-investment-chart-active="true"]{stroke:#f59e0b;filter:drop-shadow(0 0 2px #f59e0b)}`}</style>
 </div>;
}
