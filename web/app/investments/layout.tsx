import InvestmentNavigationScope from "@/components/InvestmentNavigationScope";
import InvestmentMode from "@/components/InvestmentMode";
import InvestmentAccessGate from "@/components/InvestmentAccessGate";
import {notFound} from "next/navigation";

export default function InvestmentsLayout({children}:{children:React.ReactNode}) {
 if(process.env.INVESTMENTS_ENABLED!=="true")notFound();
 return <InvestmentNavigationScope><InvestmentAccessGate><InvestmentMode readOnly={process.env.INVESTMENTS_READ_ONLY === "true"}>{children}</InvestmentMode></InvestmentAccessGate></InvestmentNavigationScope>;
}
export const dynamic="force-dynamic";
