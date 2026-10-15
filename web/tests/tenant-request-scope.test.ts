import {beforeEach,afterEach,expect,it,vi} from "vitest";
vi.unmock("@/lib/auth/store");
import {authStore} from "@/lib/auth/store";
import {tenantRequestScope} from "@/lib/tenant-request-scope";
import {allOperations} from "@/lib/portfolio-import";
beforeEach(()=>{const data=new Map<string,string>();vi.stubGlobal("localStorage",{getItem:(k:string)=>data.get(k)??null,setItem:(k:string,v:string)=>data.set(k,v),removeItem:(k:string)=>data.delete(k),clear:()=>data.clear()});});
afterEach(()=>{vi.unstubAllGlobals();vi.restoreAllMocks();});
it("prevents a second RPC after tenant changes while the first RPC is in flight",async()=>{
 authStore.set({tenantId:"first"});
 const task=tenantRequestScope("first");
 let finish!:(value:string)=>void;
 const first=vi.fn(()=>new Promise<string>(resolve=>{finish=resolve;}));
 const next=vi.fn(async()=>"second");
 const chain=async()=>{await task.run(first);return task.run(next);};
 const result=chain();
 authStore.set({tenantId:"second"});finish("ignored response");
 await expect(result).rejects.toMatchObject({name:"AbortError"});
 expect(next).not.toHaveBeenCalled();expect(task.signal.aborted).toBe(true);task.dispose();
});
it("checks the actual selection even if no tenant event was emitted",async()=>{
 authStore.set({tenantId:"first"});const task=tenantRequestScope("first");
 localStorage.setItem("budget/tenant","second");const rpc=vi.fn();
 await expect(task.run(rpc)).rejects.toMatchObject({name:"AbortError"});expect(rpc).not.toHaveBeenCalled();task.dispose();
});
it("does not fetch the next operation page when the transport ignores cancellation",async()=>{
 authStore.set({tenantId:"first"});const task=tenantRequestScope("first");
 const listInvestmentOperations=vi.fn(async()=>{authStore.set({tenantId:"second"});return{operations:Array.from({length:200},()=>({}))};});
 await expect(task.run(options=>allOperations({listInvestmentOperations} as never,"ledger",options))).rejects.toMatchObject({name:"AbortError"});
 expect(listInvestmentOperations).toHaveBeenCalledTimes(1);task.dispose();
});
it("aborts with the parent task and releases completed tasks",async()=>{
 authStore.set({tenantId:"first"});const parent=new AbortController(),task=tenantRequestScope("first",parent.signal);
 await expect(task.run(async()=>42)).resolves.toBe(42);parent.abort();
 await expect(task.run(async()=>43)).rejects.toMatchObject({name:"AbortError"});task.dispose();
});
