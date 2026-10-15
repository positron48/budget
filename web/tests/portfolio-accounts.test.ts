import {describe,it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentLedgerSchema} from "../proto/budget/v1/investment_pb";
import {accountSelection,selectedPortfolioAccounts,togglePortfolioAccount,accountScopeParams} from "../lib/portfolio-accounts";
const accounts=["a","b","c"].map(id=>create(InvestmentLedgerSchema,{id}));
describe("account scope",()=>{
 it("normalizes duplicates and keeps explicit empty different from all",()=>{
  expect(accountSelection(new URLSearchParams("account=c&account=a&account=c"))).toEqual({ids:["a","c"],explicit:true});
  expect(selectedPortfolioAccounts(new URLSearchParams(),accounts).ledgers).toHaveLength(3);
  expect(selectedPortfolioAccounts(new URLSearchParams("scope=selected"),accounts)).toEqual({ledgers:[],empty:true,invalid:false});
 });
 it("does not silently accept an unavailable account in an otherwise valid subset",()=>{
  expect(selectedPortfolioAccounts(new URLSearchParams("account=a&account=foreign"),accounts).invalid).toBe(true);
 });
 it("toggles from all without reintroducing unselected accounts or changing FX",()=>{
  const p=new URLSearchParams("currency=EUR&fx=cbr&group=g");const next=togglePortfolioAccount(p,accounts,"b",false);
  expect(next).toEqual({account:["a","c"],scope:"selected",selection:""});
  expect(accountScopeParams(p).toString()).toBe("currency=EUR&fx=cbr");
 });
});
