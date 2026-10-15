import {add,zero} from "./portfolio-tracker";
import type {Fraction} from "./investment-positions";
import type {InvestmentGroups} from "@/proto/budget/v1/investment_nav_pb";
export const portfolioGroupsKey = (tenant: string) => ["investment", tenant, "tracker", "groups"];
export function groupPredicate(groups: InvestmentGroups | undefined, selected: string[]) {
 const membership = new Map((groups?.memberships || []).map(m => [m.ledgerId+"\0"+m.instrumentKey,m.groupId]));
 const wanted = new Set(selected);
 return (ledger: string, instrument: string) => !wanted.size || wanted.has(membership.get(ledger+"\0"+instrument) || "");
}
export function removePortfolioGroup(groups: InvestmentGroups, id: string): InvestmentGroups {
 return {...groups, groups: groups.groups.filter(g => g.id !== id), memberships: groups.memberships.filter(m => m.groupId !== id)};
}

// Consume the already converted acquisition costs once, rather than replaying
// the whole operation journal once per group.
export function groupCostAllocation(groups:InvestmentGroups|undefined,holdings:{ledgerId:string;key:string;cost?:Fraction}[],ungrouped:string){
 const membership=new Map((groups?.memberships||[]).map(m=>[m.ledgerId+"\0"+m.instrumentKey,m.groupId]));
 const names=new Map((groups?.groups||[]).map(g=>[g.id,g.name]));
 const result=new Map<string,{key:string;name:string;cost?:Fraction}>();
 for(const h of holdings){const id=membership.get(h.ledgerId+"\0"+h.key)||"",value=result.get(id)||{key:id,name:names.get(id)||ungrouped,cost:zero};value.cost=value.cost!==undefined&&h.cost?add(value.cost,h.cost):undefined;result.set(id,value);}
 return [...result.values()];
}
