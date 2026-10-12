import {describe,it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentPositionDetailSchema} from "../proto/budget/v1/investment_nav_pb";
import {decimalFraction,positionSum,positionDecimal,comparePositionValues,aggregatePositions,nativePositionSums,positionPayments} from "../lib/investment-positions";
describe("position totals",()=>{
 it("keeps large quantities and tiny differences exact",()=>{
  const sum=positionSum([decimalFraction("9007199254740993.000000000001"),decimalFraction("0.000000000001")]);
  expect(positionDecimal(sum.known)).toBe("9007199254740993.000000000002");
  expect(comparePositionValues(sum.known,decimalFraction("9007199254740993.000000000001"))).toBe(-1);
  expect(positionDecimal(decimalFraction("0.0000000000000000000000000001"),28)).toBe("0.0000000000000000000000000001");
  expect(positionSum([{numerator:"1",denominator:"3"},{numerator:"2",denominator:"3"}]).known).toEqual({numerator:"1",denominator:"1"});
 });
 it("distinguishes an unknown value from a known zero and a subtotal",()=>{
  expect(positionSum([undefined])).toMatchObject({knownCount:0,complete:false});
  expect(positionSum([decimalFraction("0")])).toMatchObject({knownCount:1,complete:true});
  expect(positionSum([decimalFraction("12"),undefined])).toMatchObject({knownCount:1,complete:false,known:{numerator:"12"}});
  expect(positionDecimal(undefined)).toBe("—");expect(decimalFraction("")).toBeUndefined();
 });
 it("combines holdings but keeps book costs and payments in their own currencies",()=>{
  const rows=[create(InvestmentPositionDetailSchema,{instrumentKey:"S",ledgerId:"a",quantity:"2",bookCurrency:"RUB",cleanCost:"100",payments:[{currency:"EUR",net:{numerator:"3",denominator:"2"}}]}),create(InvestmentPositionDetailSchema,{instrumentKey:"S",ledgerId:"b",quantity:"3",bookCurrency:"USD",cleanCost:"10",payments:[{currency:"RUB",net:{numerator:"20",denominator:"1"}}]})];
  expect(aggregatePositions(rows)[0].quantity.known.numerator).toBe("5");
  expect(nativePositionSums(rows,p=>decimalFraction(p.cleanCost)).map(v=>[v.currency,positionDecimal(v.known)])).toEqual([["RUB","100"],["USD","10"]]);
  expect(positionPayments(rows).map(v=>[v.currency,positionDecimal(v.known)])).toEqual([["EUR","1.5"],["RUB","20"]]);
 });
});
