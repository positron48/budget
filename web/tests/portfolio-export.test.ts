import {describe, it, expect, vi, afterEach} from "vitest";
import {webcrypto, createHash} from "node:crypto";
import {create} from "@bufbuild/protobuf";
import {InvestmentExportJobSchema, DownloadInvestmentExportResponseSchema} from "@/proto/budget/v1/investment_pb";
import {downloadPortfolioExport, portfolioExportMaxBytes} from "@/lib/portfolio-export";

afterEach(() => vi.unstubAllGlobals());
function fixture() {
  vi.stubGlobal("crypto", webcrypto);
  const bytes = new TextEncoder().encode('{"format":"synthetic"}');
  const sha256 = createHash("sha256").update(bytes).digest("hex");
  const job = create(InvestmentExportJobSchema, {id: "job", state: "ready", byteSize: BigInt(bytes.length), sha256});
  const chunk = create(DownloadInvestmentExportResponseSchema, {data: bytes, totalBytes: job.byteSize, sha256});
  return {bytes, job, chunk};
}
describe("portfolio export byte validation", () => {
  it("downloads bounded chunks and validates exact file bytes", async () => {
    const {bytes, job, chunk} = fixture();
    const download = vi.fn(async ({offset}: {offset: bigint}) => ({...chunk, offset, data: bytes.slice(Number(offset), Number(offset) + 8)}));
    const progress = vi.fn();
    const blob = await downloadPortfolioExport({downloadInvestmentExport: download}, job, new AbortController().signal, progress);
    expect(blob.size).toBe(bytes.length); expect(blob.type).toBe("application/json");
    expect(download).toHaveBeenCalledTimes(3); expect(progress).toHaveBeenLastCalledWith(bytes.length, bytes.length);
  });
  it.each(["offset", "total", "checksum", "empty", "overrun", "corrupted"])("rejects %s without producing a file", async kind => {
    const {job, chunk} = fixture();
    if (kind === "offset") chunk.offset = 1n;
    if (kind === "total") chunk.totalBytes++;
    if (kind === "checksum") chunk.sha256 = "b".repeat(64);
    if (kind === "empty") chunk.data = new Uint8Array();
    if (kind === "overrun") chunk.data = new Uint8Array(Number(job.byteSize) + 1);
    if (kind === "corrupted") chunk.data[0] ^= 1;
    await expect(downloadPortfolioExport({downloadInvestmentExport: vi.fn(async () => chunk)}, job, new AbortController().signal, vi.fn())).rejects.toThrow();
  });
  it("rejects an oversized or pending job before requesting bytes", async () => {
    const {job} = fixture(), download = vi.fn();
    job.byteSize = BigInt(portfolioExportMaxBytes) + 1n;
    await expect(downloadPortfolioExport({downloadInvestmentExport: download}, job, new AbortController().signal, vi.fn())).rejects.toThrow();
    job.byteSize = 1n; job.state = "pending";
    await expect(downloadPortfolioExport({downloadInvestmentExport: download}, job, new AbortController().signal, vi.fn())).rejects.toThrow();
    expect(download).not.toHaveBeenCalled();
  });
  it("honors cancellation between chunks", async () => {
    const {job, chunk} = fixture(), abort = new AbortController();
    const download = vi.fn(async () => {abort.abort(); return {...chunk, data: chunk.data.slice(0, 2)};});
    await expect(downloadPortfolioExport({downloadInvestmentExport: download}, job, abort.signal, vi.fn())).rejects.toThrow();
    expect(download).toHaveBeenCalledTimes(1);
  });
});
