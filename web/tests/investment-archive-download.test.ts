import {describe,it,expect,vi} from "vitest";
import {webcrypto} from "node:crypto";
import {readInvestmentArchive} from "../lib/investment-archive-download";
const crypto=webcrypto as unknown as Crypto;
const digest=async(bytes:Uint8Array<ArrayBuffer>)=>Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256",bytes)),b=>b.toString(16).padStart(2,"0")).join("");

describe("private investment download",()=>{
 it("assembles exact chunks and verifies SHA256 before returning any file",async()=>{
  vi.stubGlobal("crypto",crypto);
  const bytes=new Uint8Array(300000);bytes[0]=123;bytes[299999]=125;
  const job={id:"job",state:"ready",byteSize:300000n,sha256:await digest(bytes)};
  const read=vi.fn(async(offset:bigint)=>({offset,totalBytes:job.byteSize,sha256:job.sha256,data:bytes.slice(Number(offset),Number(offset)+262144)}));
  const progress=vi.fn();expect(await readInvestmentArchive(job,read,new AbortController().signal,progress)).toEqual(bytes);
  expect(read.mock.calls.map(c=>c[0])).toEqual([0n,262144n]);expect(progress).toHaveBeenLastCalledWith(300000n);
  vi.unstubAllGlobals();
 });
 it.each(["checksum","offset","total","empty","oversize","payload"])("rejects %s corruption",async(kind)=>{
  vi.stubGlobal("crypto",crypto);
  const bytes=new Uint8Array([123,125]);const job={id:"job",state:"ready",byteSize:2n,sha256:await digest(bytes)};
  const chunk={offset:0n,totalBytes:2n,sha256:job.sha256,data:bytes};
  if(kind==="checksum")chunk.sha256="a".repeat(64);if(kind==="offset")chunk.offset=1n;if(kind==="total")chunk.totalBytes=3n;if(kind==="empty")chunk.data=new Uint8Array();if(kind==="oversize")chunk.data=new Uint8Array(262145);if(kind==="payload")chunk.data=new Uint8Array([123,124]);
  await expect(readInvestmentArchive(job,async()=>chunk,new AbortController().signal,vi.fn())).rejects.toThrow();vi.unstubAllGlobals();
 });
 it("aborts a pending response before storing or returning data",async()=>{
  vi.stubGlobal("crypto",crypto);const bytes=new Uint8Array([123,125]);const job={id:"job",state:"ready",byteSize:2n,sha256:await digest(bytes)},abort=new AbortController(),progress=vi.fn();
  const read=async()=>{abort.abort();return{offset:0n,totalBytes:2n,sha256:job.sha256,data:bytes};};
  await expect(readInvestmentArchive(job,read,abort.signal,progress)).rejects.toThrow();expect(progress).not.toHaveBeenCalled();vi.unstubAllGlobals();
 });
 it.each([0n,268435457n,9007199254740993n])("rejects invalid size %s without reading",async(byteSize)=>{
  const read=vi.fn();await expect(readInvestmentArchive({id:"job",state:"ready",byteSize,sha256:"a".repeat(64)},read,new AbortController().signal,vi.fn())).rejects.toThrow();expect(read).not.toHaveBeenCalled();
 });
});
