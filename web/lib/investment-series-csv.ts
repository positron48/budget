export type SeriesCSVDownload={filename:string;data:Uint8Array;encoding:string;sha256:string;uncompressedSize:bigint;rowCount:number};
export async function decodeInvestmentSeriesCSV(value:SeriesCSVDownload){
 if(value.encoding!=="gzip"||value.data.length===0||value.data.length>3*1024*1024||value.uncompressedSize<=0n||value.uncompressedSize>32n*1024n*1024n||!/^investments-[A-Z]{3}-\d{4}-\d{2}-\d{2}-\d{4}-\d{2}-\d{2}\.csv$/.test(value.filename)||!/^[a-f0-9]{64}$/.test(value.sha256)||value.rowCount<1||value.rowCount>3660)throw new Error("Invalid export envelope");
 const packed=Uint8Array.from(value.data),size=Number(value.uncompressedSize),raw=new Uint8Array(size);
 const reader=new Blob([packed.buffer]).stream().pipeThrough(new DecompressionStream("gzip")).getReader();
 let offset=0;
 try{while(true){const next=await reader.read();if(next.done)break;if(offset+next.value.length>size)throw new Error("Export size mismatch");raw.set(next.value,offset);offset+=next.value.length;}}
 finally{await reader.cancel();}
 if(offset!==size)throw new Error("Export size mismatch");
 const hash=Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256",raw))).map(v=>v.toString(16).padStart(2,"0")).join("");
 if(hash!==value.sha256)throw new Error("Export checksum mismatch");
 return raw;
}
