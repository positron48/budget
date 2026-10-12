import type {InvestmentImportRow,InvestmentImport} from "@/proto/budget/v1/investment_import_pb";
import type {InvestmentImportBatch} from "@/proto/budget/v1/investment_pb";
export type FinancialRule={Event:string;Symbol:string;Exchange:string;Currency:string;InstrumentKey:string;Kind:string;AmountBasis:string;QuantityFactor:string;AmountFactor:string;AccruedBasis:string;FeePolicy:string;IncomeBasis:string;Reason:string};
export type RuleGroup={key:string;count:number;enabled:boolean;rule:FinancialRule};
export function financialGroups(rows:InvestmentImportRow[]):RuleGroup[]{
 const groups=new Map<string,RuleGroup>();
 for(const row of rows){const f=row.sourceFields;const key=JSON.stringify([f.Event||"",f.Symbol||"",f.Exchange||"",f.Currency||""]);const old=groups.get(key);if(old){old.count++;continue;}
 groups.set(key,{key,count:1,enabled:false,rule:{Event:f.Event||"",Symbol:f.Symbol||"",Exchange:f.Exchange||"",Currency:f.Currency||"",InstrumentKey:"",Kind:"",AmountBasis:"",QuantityFactor:"",AmountFactor:"",AccruedBasis:"",FeePolicy:"",IncomeBasis:"",Reason:""}});}
 return [...groups.values()];
}
export function preparationMapping(source:InvestmentImport,rows:InvestmentImportRow[],accounts:Record<string,string>,groups:RuleGroup[],timezone:string,settlementPolicy:string,settlements:Record<number,string>,duplicates:Record<number,string>,tradeEvidence:Record<string,unknown>={},incomeEvidence:Record<string,unknown>={},cashEvidence:Record<string,unknown>={}){
 if(rows.length!==source.rowCount||rows.some((row,index)=>row.record!==index+1))throw new Error("incompleteSource");
 if(!timezone.trim()||!settlementPolicy)throw new Error("policiesRequired");
 const ledger_by_record:Record<number,string>={};
 for(const row of rows){if(accounts[row.accountId])ledger_by_record[row.record]=accounts[row.accountId];}
 return {cash_evidence:cashEvidence,income_evidence:incomeEvidence,trade_evidence:tradeEvidence,source_sha256:source.sourceSha256,timezone:timezone.trim(),settlement_policy:settlementPolicy,ledger_by_record,settlement_by_record:settlementPolicy==="explicit"?settlements:{},duplicate_review:duplicates,rules:groups.filter(group=>group.enabled).map(group=>group.rule)};
}
export function selectPreparedBatch(batch:InvestmentImportBatch,selected:Set<number>):InvestmentImportBatch{
 const entries=batch.entries.filter(entry=>selected.has(entry.record));if(!entries.length)throw new Error("selectRows");
 const ids=new Set(entries.map(entry=>entry.operation?.ledgerId));
 return {...batch,entries,expectedLedgerVersions:Object.fromEntries(Object.entries(batch.expectedLedgerVersions).filter(([id])=>ids.has(id)))};
}

export function parseTradeEvidenceFile(text:string,source:InvestmentImport):Record<string,unknown>{return parseEvidenceFile(text,source,"trade_evidence");}
export function parseIncomeEvidenceFile(text:string,source:InvestmentImport):Record<string,unknown>{return parseEvidenceFile(text,source,"income_evidence");}
export function parseCashEvidenceFile(text:string,source:InvestmentImport):Record<string,unknown>{return parseEvidenceFile(text,source,"cash_evidence");}
function parseEvidenceFile(text:string,source:InvestmentImport,kind:"trade_evidence"|"income_evidence"|"cash_evidence"):Record<string,unknown>{
 let parsed:unknown;try{parsed=JSON.parse(text);}catch{throw new Error("invalidEvidence");}
 if(!parsed||typeof parsed!=="object"||Array.isArray(parsed))throw new Error("invalidEvidence");
 const value=parsed as Record<string,unknown>;
 if(value.source_sha256!==source.sourceSha256)throw new Error("evidenceSourceMismatch");
 if(!value[kind]||typeof value[kind]!=="object"||Array.isArray(value[kind])||Object.keys(value).some(key=>!["source_sha256",kind].includes(key)))throw new Error("invalidEvidence");
 const evidence=value[kind] as Record<string,unknown>;
 if(!Object.keys(evidence).length)throw new Error("invalidEvidence");
 for(const [record,item] of Object.entries(evidence)){
  if(!/^[1-9][0-9]*$/.test(record)||Number(record)>source.rowCount||!item||typeof item!=="object"||Array.isArray(item)||(item as Record<string,unknown>).source_row_id!==`${source.sourceSha256}:${record}`)throw new Error("invalidEvidence");
 }
 return evidence;
}
