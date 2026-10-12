"use client";
import {useTranslations} from "next-intl";
import {useInvestmentAccess} from "@/lib/investment-access";

export default function InvestmentAccessGate({children}:{children:React.ReactNode}){
 const t=useTranslations("investmentAccess");const access=useInvestmentAccess();
 if(access.isError)return <section className="m-6 space-y-3"><h1 className="text-xl font-semibold">{t("title")}</h1><p role="alert">{t("unavailable")}</p><button className="rounded bg-blue-600 px-4 py-2 text-white" onClick={()=>void access.refetch()}>{t("retry")}</button></section>;
 if(!access.isSuccess)return <p role="status" className="m-6">{t("loading")}</p>;
 return children;
}
