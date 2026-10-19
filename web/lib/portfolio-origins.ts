import type {Client,CallOptions} from '@connectrpc/connect';
import {InvestmentService,type InvestmentSnapshot,type InvestmentOperation} from '@/proto/budget/v1/investment_pb';
import {allOperations} from './portfolio-import';
import {decimal} from './portfolio-tracker';

export function acquisitionOriginReferences(snapshots:readonly InvestmentSnapshot[],operations:readonly InvestmentOperation[]){
 const known=new Set(operations.map(o=>o.id)),refs=new Map<string,{ledgerId:string;operationId:string}>();
 for(const snapshot of snapshots)for(const position of snapshot.positions){
  const quantity=decimal(position.quantity);if(!quantity||BigInt(quantity.numerator)<=0n)continue;
  for(const lot of position.lots){
   const operationId=lot.acquisitionOperationId||lot.originId,ledgerId=lot.originLedgerId||snapshot.ledger?.id;
   if(operationId&&ledgerId&&!known.has(operationId))refs.set(ledgerId+'\0'+operationId,{ledgerId,operationId});
  }
 }
 return [...refs.values()].sort((a,b)=>(a.ledgerId+'\0'+a.operationId).localeCompare(b.ledgerId+'\0'+b.operationId));
}
// Read referenced acquisition metadata without adding archived accounts or their
// cash movements to the current portfolio. Failed origins leave cost partial.
export async function loadAcquisitionOrigins(api:Pick<Client<typeof InvestmentService>,'listInvestmentOperations'>,refs:ReturnType<typeof acquisitionOriginReferences>,options?:CallOptions){
 const wanted=new Set(refs.map(r=>r.ledgerId+'\0'+r.operationId)),ids=[...new Set(refs.map(r=>r.ledgerId))];
 const results=await Promise.allSettled(ids.map(id=>allOperations(api,id,options)));
 options?.signal?.throwIfAborted();
 const operations:InvestmentOperation[]=[];let unavailable=0;
 for(const result of results){if(result.status==='fulfilled')operations.push(...result.value.filter(o=>wanted.has(o.ledgerId+'\0'+o.id)));else unavailable++;}
 return {operations,unavailable};
}
