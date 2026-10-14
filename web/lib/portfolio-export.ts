import type {InvestmentExportJob, DownloadInvestmentExportResponse} from "@/proto/budget/v1/investment_pb";

export const portfolioExportMaxBytes = 256 * 1024 * 1024;
type ExportAPI = {
  downloadInvestmentExport(input: {jobId: string; offset: bigint}, options?: {signal: AbortSignal}): Promise<DownloadInvestmentExportResponse>;
};

// Validate every chunk against the ready job, then hash the exact downloaded
// bytes. Never trigger a browser download for truncated or mismatched content.
export async function downloadPortfolioExport(api: ExportAPI, job: InvestmentExportJob, signal: AbortSignal, progress: (received: number, total: number) => void): Promise<Blob> {
  if (job.state !== "ready" || !job.id || !/^[a-f0-9]{64}$/.test(job.sha256) || job.byteSize <= 0n || job.byteSize > BigInt(portfolioExportMaxBytes)) throw new Error("invalid_export");
  const size = Number(job.byteSize), data = new Uint8Array(size);
  let offset = 0;
  while (offset < size) {
    signal.throwIfAborted();
    const chunk = await api.downloadInvestmentExport({jobId: job.id, offset: BigInt(offset)}, {signal});
    if (chunk.offset !== BigInt(offset) || chunk.totalBytes !== job.byteSize || chunk.sha256 !== job.sha256 || !chunk.data.length || chunk.data.length > 256 * 1024 || chunk.data.length > size - offset) throw new Error("invalid_export_chunk");
    data.set(chunk.data, offset); offset += chunk.data.length;
    progress(offset, size);
  }
  signal.throwIfAborted();
  const digest = await crypto.subtle.digest("SHA-256", data);
  const sha = Array.from(new Uint8Array(digest), value => value.toString(16).padStart(2, "0")).join("");
  if (sha !== job.sha256) throw new Error("export_checksum_mismatch");
  signal.throwIfAborted();
  return new Blob([data.buffer], {type: "application/json"});
}
