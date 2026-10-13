import InvestmentNavigationScope from "@/components/InvestmentNavigationScope";
import Protected from "@/components/Protected";
import InvestmentMode from "@/components/InvestmentMode";
import InvestmentAccessGate from "@/components/InvestmentAccessGate";
import {notFound} from "next/navigation";

export default function InvestmentsLayout({children}:{children:React.ReactNode}) {
 if(process.env.INVESTMENTS_ENABLED!=="true")notFound();
 return <Protected><InvestmentNavigationScope><InvestmentAccessGate><InvestmentMode readOnly={process.env.INVESTMENTS_READ_ONLY === "true"}>{children}</InvestmentMode></InvestmentAccessGate></InvestmentNavigationScope></Protected>;
}
export const dynamic="force-dynamic";
