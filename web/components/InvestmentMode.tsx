"use client";
import {createContext,useContext} from "react";
import {useTranslations} from "next-intl";
const ReadOnlyContext=createContext(false);
export const useInvestmentReadOnly=()=>useContext(ReadOnlyContext);
export default function InvestmentMode({readOnly,children}:{readOnly:boolean;children:React.ReactNode}){
 const t=useTranslations("investmentAccess");
 return <ReadOnlyContext.Provider value={readOnly}>{readOnly&&<p role="status" className="m-4 rounded border border-amber-400 bg-amber-50 p-3 text-amber-950 dark:bg-amber-950 dark:text-amber-100">{t("readOnly")}</p>}{children}<footer className="m-4 text-xs text-gray-500"><a className="underline" href="https://www.cbr.ru/currency_base/" target="_blank" rel="noreferrer">{t("officialFXSource")}</a></footer></ReadOnlyContext.Provider>;
}
