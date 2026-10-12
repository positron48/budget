import { describe,it,expect } from "vitest";
import { create } from "@bufbuild/protobuf";
import { InvestmentOperationSchema } from "../proto/budget/v1/investment_pb";
import { InvestmentImportSchema } from "../proto/budget/v1/investment_import_pb";
import { queueReviewedRow,reviewedBatch,storedBatchRows,type ReviewedBatchRow } from "../lib/investment-batch";
const row=(record=1):ReviewedBatchRow=>({importId:"source",previewHash:"hash",filename:"source.csv",record,rowVersion:9007199254740993n,reviewReason:"Reviewed",ledgerName:"Ledger",operation:create(InvestmentOperationSchema,{ledgerId:"ledger",kind:"deposit",currency:"RUB",amount:"9007199254740993.123456789012"})});
describe("reviewed import batch",()=>{
 it("preserves exact money and versions and captures source identity",()=>{
  const batch=reviewedBatch([row()],create(InvestmentImportSchema,{id:"source",previewHash:"hash",version:7n}),{ledger:9007199254740995n},"request");
  expect(batch.entries[0].operation?.amount).toBe("9007199254740993.123456789012");
  expect(batch.entries[0].expectedRowVersion).toBe(9007199254740993n);
  expect(batch.expectedLedgerVersions.ledger).toBe(9007199254740995n);
  expect(batch.expectedImportVersion).toBe(7n);
 });
 it("replaces a reviewed row in place without changing tie order",()=>{
  const rows=[row(2),row(1)];
  const replacement={...row(2),reviewReason:"Corrected"};
  const next=queueReviewedRow(rows,replacement);
  expect(next.map(r=>r.record)).toEqual([2,1]);
  expect(next[0].reviewReason).toBe("Corrected");
  expect(rows[0].reviewReason).toBe("Reviewed");
 });
 it("rejects another source or a missing ledger",()=>{
  expect(()=>queueReviewedRow([row()],{...row(2),importId:"other"})).toThrow("batchMixedImport");
  expect(()=>reviewedBatch([row()],create(InvestmentImportSchema,{id:"source",previewHash:"changed"}),{ledger:1n},"key")).toThrow("batchMixedImport");
  expect(()=>reviewedBatch([row()],create(InvestmentImportSchema,{id:"source",previewHash:"hash"}),{},"key")).toThrow("batchLedgerMissing");
 });
});

it("reconstructs financial rows from the durable version rather than retaining older edits",()=>{
 const batch=reviewedBatch([row(9)],create(InvestmentImportSchema,{id:"source",previewHash:"hash",version:2n}),{ledger:3n},"saved");
 batch.entries[0].operation!.amount="123.000000000001";
 const restored=storedBatchRows(batch,"history.csv",{ledger:"Renamed ledger"});
 expect(restored.map(r=>r.record)).toEqual([9]);
 expect(restored[0].operation.amount).toBe("123.000000000001");
 expect(restored[0].ledgerName).toBe("Renamed ledger");
 expect(restored[0].rowVersion).toBe(9007199254740993n);
});

it("retains preparation evidence through saved review and rejects mixed preparations",()=>{
 const source=create(InvestmentImportSchema,{id:"source",previewHash:"hash",version:1n});
 const prepared={...row(),preparationEvidenceJSON:'{"ReviewHash":"first","Mapping":{"timezone":"UTC"}}'};
 const original=reviewedBatch([prepared],source,{ledger:1n},"key");
 const restored=storedBatchRows(original,"source.csv",{ledger:"Ledger"});
 const saved=reviewedBatch(restored,source,{ledger:2n},"key");
 expect(saved.preparationEvidenceJson).toBe(prepared.preparationEvidenceJSON);
 expect(()=>reviewedBatch([prepared,{...row(2),preparationEvidenceJSON:'{"ReviewHash":"other"}'}],source,{ledger:2n},"key")).toThrow("batchMixedReview");
});
