import {describe,it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentLedgerSchema} from "../proto/budget/v1/investment_pb";
import {accountSelection,selectedPortfolioAccounts,togglePortfolioAccount,accountScopeParams,visiblePortfolioAccounts,historicalPortfolioAccounts} from "../lib/portfolio-accounts";
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
 it("hides archived history from all and unrelated explicit account navigation",()=>{
  const all=[...accounts,create(InvestmentLedgerSchema,{id:"old",archived:true})];
  expect(selectedPortfolioAccounts(new URLSearchParams(),all).ledgers.map(l=>l.id)).toEqual(["a","b","c"]);
  expect(visiblePortfolioAccounts(new URLSearchParams("account=a"),all).map(l=>l.id)).toEqual(["a","b","c"]);
  expect(selectedPortfolioAccounts(new URLSearchParams("account=old"),all).ledgers.map(l=>l.id)).toEqual(["old"]);
  expect(visiblePortfolioAccounts(new URLSearchParams("account=old"),all).map(l=>l.id)).toContain("old");
  expect(togglePortfolioAccount(new URLSearchParams(),all,"b",false).account).toEqual(["a","c"]);
 });
 it("toggles from all without reintroducing unselected accounts or changing FX",()=>{
  const p=new URLSearchParams("currency=EUR&fx=cbr&group=g");const next=togglePortfolioAccount(p,accounts,"b",false);
  expect(next).toEqual({account:["a","c"],scope:"selected",selection:""});
  expect(accountScopeParams(p).toString()).toBe("currency=EUR&fx=cbr");
 });
});

it("retains archived tracker history only in the global chart without restoring navigation",()=>{
 const source=create(InvestmentLedgerSchema,{id:"closed-source",archived:true,portfolioTracking:true}),legacy=create(InvestmentLedgerSchema,{id:"legacy-cash",archived:true});
 const all=[...accounts,source,legacy];
 expect(historicalPortfolioAccounts(new URLSearchParams(),all).map(l=>l.id)).toEqual(["a","b","c","closed-source"]);
 expect(visiblePortfolioAccounts(new URLSearchParams(),all).map(l=>l.id)).toEqual(["a","b","c"]);
 expect(historicalPortfolioAccounts(new URLSearchParams("account=a"),all).map(l=>l.id)).toEqual(["a"]);
});
