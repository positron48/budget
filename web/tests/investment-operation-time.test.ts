import {describe,it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentOperationSchema} from "../proto/budget/v1/investment_pb";
import {operationTime} from "../lib/investment-operation-time";
describe("operation civil date presentation",()=>{
 it("never shows calculation midnight or end-of-day as factual time",()=>{
  const op=create(InvestmentOperationSchema,{occurredAt:{seconds:1704142800n},settlementAt:{seconds:1704315599n,nanos:999999000},timing:{timezone:"Europe/Moscow",occurredDate:"2024-01-02",settlementDate:"2024-01-03"}});
  expect(operationTime(op,false,"en")).toBe("2024-01-02 · Date only · Europe/Moscow");expect(operationTime(op,true,"ru")).toBe("2024-01-03 · Только дата · Europe/Moscow");
  op.timing!.settlementDate="";op.kind="buy";op.settlementAssumed=true;expect(operationTime(op,true,"en")).toBe("2024-01-02 · Date only · Europe/Moscow");
  op.kind="deposit";op.settlementAssumed=false;expect(operationTime(op,true,"en")).toBe("2024-01-02 · Date only · Europe/Moscow");
 });
 it("keeps restored execution precise while retaining source-only date",()=>{
  const op=create(InvestmentOperationSchema,{occurredAt:{seconds:1704186000n},timing:{timezone:"Europe/Moscow",sourceDate:"2024-01-02"}});
  expect(operationTime(op,false,"en")).not.toContain("Date only");expect(operationTime(op,false,"en")).toContain("12:00:00");
 });
});
