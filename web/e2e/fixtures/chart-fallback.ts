import type {Route} from "@playwright/test";

// Paginated fixtures intentionally represent the older server contract. A new
// RPC must return Unimplemented, rather than a successful empty projection.
export async function chartFallback(route:Route,method?:{name:string}){
 if(method?.name!=="GetTrackerChart")return false;
 const trailer=Buffer.from("grpc-status: 12\r\ngrpc-message: unsupported by fixture\r\n"),head=Buffer.alloc(5);
 head[0]=0x80;head.writeUInt32BE(trailer.length,1);
 await route.fulfill({status:200,contentType:"application/grpc-web+proto",body:Buffer.concat([head,trailer])});
 return true;
}
