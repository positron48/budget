import {describe,it,expect} from "vitest";
import {decimal} from "@/lib/portfolio-tracker";
import {positionDecimal} from "@/lib/investment-positions";
import {create} from "@bufbuild/protobuf";
import {InvestmentGroupsSchema} from "@/proto/budget/v1/investment_nav_pb";
import {groupCostAllocation,groupPredicate,removePortfolioGroup} from "@/lib/portfolio-groups";
const classification=()=>create(InvestmentGroupsSchema,{version:1n,groups:[{id:"a",name:"Core"},{id:"b",name:"Bonds"}],memberships:[{ledgerId:"one",instrumentKey:"S",groupId:"a"},{ledgerId:"two",instrumentKey:"S",groupId:"b"},{ledgerId:"two",instrumentKey:"OLD",groupId:"a"}]});
describe("account-specific security groups",()=>{
 it("unions selections without assigning a ticker globally",()=>{const groups=classification();expect(groupPredicate(groups,["a"])("one","S")).toBe(true);expect(groupPredicate(groups,["a"])("two","S")).toBe(false);expect(groupPredicate(groups,["a","b","a"])("two","S")).toBe(true);expect(groupPredicate(groups,[""])("one","OTHER")).toBe(true);expect(groupPredicate(groups,[])("two","S")).toBe(true);});
 it("deletes membership labels while preserving closed-position history elsewhere",()=>{const original=classification(),next=removePortfolioGroup(original,"a");expect(next.version).toBe(1n);expect(next.memberships).toHaveLength(1);expect(original.memberships).toHaveLength(3);expect(groupPredicate(next,[""])("two","OLD")).toBe(true);});
});

it("allocates existing converted costs by account without replaying trades or hiding unknown cost",()=>{
 const result=groupCostAllocation(classification(),[{ledgerId:"one",key:"S",cost:decimal("100")},{ledgerId:"two",key:"S",cost:decimal("200")},{ledgerId:"one",key:"UNKNOWN"}],"Ungrouped");
 expect(result.map(g=>[g.key,g.cost?positionDecimal(g.cost):undefined])).toEqual([["a","100"],["b","200"],["",undefined]]);
 const mixed=groupCostAllocation(classification(),[{ledgerId:"two",key:"OLD"},{ledgerId:"one",key:"S",cost:decimal("100")},{ledgerId:"two",key:"S",cost:decimal("200")}],"Ungrouped");expect(mixed.find(g=>g.key==="a")?.cost).toBeUndefined();expect(positionDecimal(mixed.find(g=>g.key==="b")!.cost)).toBe("200");
});
