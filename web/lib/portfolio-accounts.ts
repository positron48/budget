import type {InvestmentLedger} from "@/proto/budget/v1/investment_pb";
export function accountSelection(params:URLSearchParams){
 const ids=[...new Set(params.getAll("account").filter(Boolean))].sort();
 return {ids,explicit:ids.length>0||params.get("scope")==="selected"};
}
export function selectedPortfolioAccounts(params:URLSearchParams,accounts:InvestmentLedger[]){
 const {ids,explicit}=accountSelection(params);
 return {ledgers:explicit?accounts.filter(l=>ids.includes(l.id)):accounts,invalid:ids.some(id=>!accounts.some(l=>l.id===id)),empty:explicit&&!ids.length};
}
export function togglePortfolioAccount(params:URLSearchParams,accounts:InvestmentLedger[],id:string,checked:boolean){
 const scope=accountSelection(params),ids=new Set(scope.explicit?scope.ids:accounts.map(l=>l.id));
 if(checked)ids.add(id);else ids.delete(id);
 return {account:[...ids].sort(),scope:"selected",selection:""};
}
export function accountScopeParams(params:URLSearchParams){const result=new URLSearchParams();for(const key of ["account","scope","currency","fx"])for(const value of params.getAll(key))result.append(key,value);return result;}
