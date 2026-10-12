import { create } from "@bufbuild/protobuf";
import { InvestmentImportBatchSchema, type InvestmentOperation } from "@/proto/budget/v1/investment_pb";
import type { InvestmentImport } from "@/proto/budget/v1/investment_import_pb";

export type ReviewedBatchRow = {
  importId: string; previewHash: string; filename: string;
  preparationEvidenceJSON?: string;
  record: number; rowVersion: bigint; reviewReason: string;
  ledgerName: string; operation: InvestmentOperation;
};
export function queueReviewedRow(rows: ReviewedBatchRow[], entry: ReviewedBatchRow) {
  if (rows.some(row => row.importId !== entry.importId || row.previewHash !== entry.previewHash)) throw new Error("batchMixedImport");
  const index = rows.findIndex(row => row.record === entry.record);
  const next = [...rows];
  if (index < 0) next.push(entry); else next[index] = entry;
  if (next.length > 5000 || new Set(next.map(row => row.operation.ledgerId)).size > 20) throw new Error("batchLimit");
  return next;
}
export function reviewedBatch(rows: ReviewedBatchRow[], source: InvestmentImport, versions: Record<string, bigint>, requestKey: string) {
  if (!rows.length || rows.some(row => row.importId !== source.id || row.previewHash !== source.previewHash)) throw new Error("batchMixedImport");
  if (rows.some(row => !versions[row.operation.ledgerId])) throw new Error("batchLedgerMissing");
  const evidence=[...new Set(rows.map(row=>row.preparationEvidenceJSON).filter(Boolean))];
  if(evidence.length>1)throw new Error("batchMixedReview");
  return create(InvestmentImportBatchSchema, {
    preparationEvidenceJson:evidence[0]||"",
    importId: source.id, previewHash: source.previewHash, expectedImportVersion: source.version,
    requestKey, expectedLedgerVersions: versions,
    entries: rows.map(row => ({ record: row.record, expectedRowVersion: row.rowVersion, reviewReason: row.reviewReason, operation: row.operation })),
  });
}

export function storedBatchRows(batch: import("@/proto/budget/v1/investment_pb").InvestmentImportBatch, filename: string, ledgerNames: Record<string,string>): ReviewedBatchRow[] {
  return batch.entries.map(entry => {
    if (!entry.operation) throw new Error("batchNoPreview");
    return {
      importId:batch.importId, previewHash:batch.previewHash, filename, preparationEvidenceJSON:batch.preparationEvidenceJson,
      record:entry.record, rowVersion:entry.expectedRowVersion, reviewReason:entry.reviewReason,
      ledgerName:ledgerNames[entry.operation.ledgerId] || entry.operation.ledgerId,
      operation:entry.operation,
    };
  });
}
