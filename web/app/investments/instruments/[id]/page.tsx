import PortfolioInstrument from "@/components/PortfolioInstrument";
export default async function Page({params}:{params:Promise<{id:string}>}){const {id}=await params;return <PortfolioInstrument instrumentKey={decodeURIComponent(id)}/>;}
