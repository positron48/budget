import {it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentGroupsSchema} from "../proto/budget/v1/investment_nav_pb";
import {portfolioAllocation,allocationShare,portfolioHoldingRows,holdingAccounts} from "../lib/portfolio-overview";
import {decimal,zero} from "../lib/portfolio-tracker";
import {positionDecimal} from "../lib/investment-positions";
import type {ReportPosition,ReportHolding} from "../lib/portfolio-report";
const f=(n:string)=>decimal(n)!;
const holdings:ReportHolding[]=[{ledgerId:"one",key:"S",quantity:f("1"),value:f("14000"),cost:f("8000"),priceCurrency:"USD"},{ledgerId:"two",key:"S",quantity:f("2"),value:f("28000"),cost:f("16000"),priceCurrency:"USD"},{ledgerId:"one",key:"MISSING",quantity:f("1")}];
it("uses converted per-account values once and separates unknown portions",()=>{
 const r=portfolioAllocation(holdings,"account","value");expect(positionDecimal(r.total)).toBe("42000");expect(r.missing).toBe(1);expect(r.rows.map(p=>[p.key,positionDecimal(p.known),p.missing])).toEqual([["two","28000",0],["one","14000",1]]);
 expect(positionDecimal(allocationShare(r.rows[0].known,r.total),12)).toBe("0.666666666667");
 const currency=portfolioAllocation(holdings,"currency","value");expect(currency.rows.map(p=>p.key)).toEqual(["USD",""]);expect(currency.rows[1].missing).toBe(1);expect(allocationShare(zero,zero)).toBeUndefined();
});
it("allows one security to be in different groups on different accounts",()=>{
 const groups=create(InvestmentGroupsSchema,{groups:[{id:"a",name:"A"},{id:"b",name:"B"}],memberships:[{ledgerId:"one",instrumentKey:"S",groupId:"a"},{ledgerId:"two",instrumentKey:"S",groupId:"b"}]});
 const r=portfolioAllocation(holdings,"group","cost",groups);expect(r.rows.map(p=>[p.key,positionDecimal(p.known),p.missing])).toEqual([["b","16000",0],["a","8000",0],["","0",1]]);
 const type=portfolioAllocation(holdings,"type","value",groups,new Map([["S","share"]]));expect(type.rows[0].key).toBe("share");expect(type.rows[1].key).toBe("");
});
it("folds account details without hiding an unknown acquisition basis",()=>{
 const r=holdingAccounts([...holdings,{ledgerId:"one",key:"S",quantity:f("1"),value:f("14000")}],"S");expect(positionDecimal(r[0].quantity)).toBe("2");expect(positionDecimal(r[0].value)).toBe("28000");expect(r[0].cost).toBeUndefined();
});
const position=(key:string,amount?:string):ReportPosition=>({key,currency:"RUB",quantity:f("1"),value:amount?f(amount):undefined,income:zero,incomeComplete:true,recoveredAccrued:zero,accounts:["Broker one"]});
it("sorts beyond floating-point precision and keeps missing values last in both directions",()=>{
 const rows=[position("a","9007199254740992"),position("z","9007199254740993"),position("unknown")];
 const options={closed:false,search:"",sort:"value" as const,descending:true,label:(k:string)=>k};
 expect(portfolioHoldingRows(rows,options).map(p=>p.key)).toEqual(["z","a","unknown"]);expect(portfolioHoldingRows(rows,{...options,descending:false}).map(p=>p.key)).toEqual(["a","z","unknown"]);expect(rows.map(p=>p.key)).toEqual(["a","z","unknown"]);
});
it("searches account/name/key locally and includes sold positions only when requested",()=>{
 const rows=[position("ISIN@MCX","1"),{...position("closed","2"),quantity:zero,name:"Retired fund"}];
 const options={closed:false,search:"BROKER",sort:"name" as const,descending:false,label:(k:string)=>k};expect(portfolioHoldingRows(rows,options)).toHaveLength(1);expect(portfolioHoldingRows(rows,{...options,closed:true,search:"retired"})[0].key).toBe("closed");expect(portfolioHoldingRows(rows,{...options,search:"ISIN"})[0].key).toBe("ISIN@MCX");
});
