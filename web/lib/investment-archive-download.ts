export type ExportJob = { id: string; state: string; byteSize: bigint; sha256: string };
export type ExportChunk = { data: Uint8Array; offset: bigint; totalBytes: bigint; sha256: string };

// Verify the exact private artifact before allowing a browser download. A partial
// or mixed job is never saved. Abort on tenant/auth change, including during hash.
export async function readInvestmentArchive(job: ExportJob, read: (offset: bigint) => Promise<ExportChunk>, signal: AbortSignal, progress: (bytes: bigint) => void) {
  if (job.state !== "ready" || job.byteSize <= 0n || job.byteSize > 268435456n || !/^[a-f0-9]{64}$/.test(job.sha256)) throw new Error("invalid_export");
  const bytes = new Uint8Array(Number(job.byteSize));
  let offset = 0n;
  while (offset < job.byteSize) {
    signal.throwIfAborted();
    const chunk = await read(offset);
    signal.throwIfAborted();
    if (chunk.offset !== offset || chunk.totalBytes !== job.byteSize || chunk.sha256 !== job.sha256 || chunk.data.length < 1 || chunk.data.length > 262144 || offset + BigInt(chunk.data.length) > job.byteSize) throw new Error("invalid_export_chunk");
    bytes.set(chunk.data, Number(offset));
    offset += BigInt(chunk.data.length);
    progress(offset);
  }
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  signal.throwIfAborted();
  const actual = Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, "0")).join("");
  if (actual !== job.sha256) throw new Error("export_checksum_mismatch");
  return bytes;
}
