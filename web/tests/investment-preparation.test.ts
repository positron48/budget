import {it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentImportSchema,InvestmentImportRowSchema} from "../proto/budget/v1/investment_import_pb";
import {InvestmentImportBatchSchema} from "../proto/budget/v1/investment_pb";
import {financialGroups,preparationMapping,selectPreparedBatch,parseTradeEvidenceFile,parseIncomeEvidenceFile,parseCashEvidenceFile} from "../lib/investment-preparation";
const row=(record:number,exchange="X")=>create(InvestmentImportRowSchema,{record,accountId:"account",sourceFields:{Event:"BUY",Symbol:"TEST",Exchange:exchange,Currency:"RUB"},decimals:{Price:"9007199254740993.123456789012"}});
it("groups exact source identities without assuming financial meaning",()=>{
 const groups=financialGroups([row(1),row(2),row(3,"Y")]);expect(groups.map(g=>g.count)).toEqual([2,1]);expect(groups.every(g=>!g.enabled&&g.rule.AmountBasis==="")).toBe(true);
});
it("requires complete ordered source and explicit time policy",()=>{
 const source=create(InvestmentImportSchema,{sourceSha256:"hash",rowCount:2});const rows=[row(1),row(2)];
 expect(()=>preparationMapping(source,[row(2),row(1)],{},[],"UTC","assume_trade_time",{},{})).toThrow("incompleteSource");
 expect(()=>preparationMapping(source,rows,{},[],"","",{},{})).toThrow("policiesRequired");
 const groups=financialGroups(rows);groups[0].enabled=true;groups[0].rule.AmountFactor="0.000000000001";
 const mapping=preparationMapping(source,rows,{account:"ledger"},groups,"UTC","assume_trade_time",{},{1:"Separate execution"});
 expect(mapping.ledger_by_record).toEqual({1:"ledger",2:"ledger"});expect(mapping.rules[0].AmountFactor).toBe("0.000000000001");expect(rows[0].decimals.Price).toBe("9007199254740993.123456789012");
});
it("selects only reviewed records and prunes unused journal versions",()=>{
 const batch=create(InvestmentImportBatchSchema,{preparationEvidenceJson:'{"Version":1}',expectedLedgerVersions:{a:9007199254740993n,b:2n},entries:[{record:1,operation:{ledgerId:"a",amount:"9007199254740993.123456789012"}},{record:2,operation:{ledgerId:"b"}}]});
 const selected=selectPreparedBatch(batch,new Set([1,99]));expect(selected.entries).toHaveLength(1);expect(selected.expectedLedgerVersions).toEqual({a:9007199254740993n});expect(selected.preparationEvidenceJson).toBe(batch.preparationEvidenceJson);expect(batch.entries).toHaveLength(2);expect(()=>selectPreparedBatch(batch,new Set())).toThrow("selectRows");
});

it("binds broker evidence to this source and preserves decimal strings",()=>{
 const source=create(InvestmentImportSchema,{sourceSha256:"hash",rowCount:2});
 const value={source_sha256:"hash",trade_evidence:{"2":{source_row_id:"hash:2",payment:"-9007199254740993.12"}}};
 const parsed=parseTradeEvidenceFile(JSON.stringify(value),source);
 expect(parsed["2"]).toEqual(value.trade_evidence["2"]);
 expect(()=>parseTradeEvidenceFile(JSON.stringify({...value,source_sha256:"other"}),source)).toThrow("evidenceSourceMismatch");
 expect(()=>parseTradeEvidenceFile(JSON.stringify({...value,trade_evidence:{"3":{source_row_id:"hash:3"}}}),source)).toThrow("invalidEvidence");
 expect(()=>parseTradeEvidenceFile('{"source_sha256":"hash","trade_evidence":[]}',source)).toThrow("invalidEvidence");
});

it("keeps income evidence separate, source-bound and opt-in",()=>{
 const source=create(InvestmentImportSchema,{sourceSha256:"hash",rowCount:2});
 const value={source_sha256:"hash",income_evidence:{"1":{source_row_id:"hash:1",credit:{payment:"9007199254740993.123456789"},tax:{payment:"-13"}}}};
 const parsed=parseIncomeEvidenceFile(JSON.stringify(value),source);
 expect(parsed).toEqual(value.income_evidence);
 expect(()=>parseTradeEvidenceFile(JSON.stringify(value),source)).toThrow("invalidEvidence");
 expect(()=>parseIncomeEvidenceFile(JSON.stringify({...value,source_sha256:"other"}),source)).toThrow("evidenceSourceMismatch");
 expect(()=>parseIncomeEvidenceFile(JSON.stringify({...value,income_evidence:{"3":{source_row_id:"hash:3"}}}),source)).toThrow("invalidEvidence");
 expect(preparationMapping(source,[row(1),row(2)],{},[],"UTC","assume_trade_time",{},{}).income_evidence).toEqual({});
 expect(preparationMapping(source,[row(1),row(2)],{},[],"UTC","assume_trade_time",{},{},{},parsed).income_evidence).toEqual(value.income_evidence);
});

it("loads cash evidence without enabling it or confusing it with payouts",()=>{
 const source=create(InvestmentImportSchema,{sourceSha256:"hash",rowCount:2});
 const value={source_sha256:"hash",cash_evidence:{"1":{source_row_id:"hash:1",operation:{payment:"9007199254740993.123456789"}}}};
 const parsed=parseCashEvidenceFile(JSON.stringify(value),source);
 expect(parsed).toEqual(value.cash_evidence);
 expect(()=>parseIncomeEvidenceFile(JSON.stringify(value),source)).toThrow("invalidEvidence");
 expect(()=>parseCashEvidenceFile(JSON.stringify({...value,source_sha256:"other"}),source)).toThrow("evidenceSourceMismatch");
 expect(preparationMapping(source,[row(1),row(2)],{},[],"UTC","assume_trade_time",{},{}).cash_evidence).toEqual({});
 expect(preparationMapping(source,[row(1),row(2)],{},[],"UTC","assume_trade_time",{},{},{},{},parsed).cash_evidence).toEqual(value.cash_evidence);
});
