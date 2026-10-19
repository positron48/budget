import {it,expect,vi} from 'vitest';
import {create} from '@bufbuild/protobuf';
import {InvestmentSnapshotSchema,InvestmentOperationSchema} from '@/proto/budget/v1/investment_pb';
import {acquisitionOriginReferences,loadAcquisitionOrigins} from '@/lib/portfolio-origins';
it('reads only referenced acquisition metadata and deduplicates repeated lots',async()=>{
 const s=create(InvestmentSnapshotSchema,{ledger:{id:'current'},positions:[{instrumentKey:'S',quantity:'2',lots:[{acquisitionOperationId:'buy',originLedgerId:'archived'},{acquisitionOperationId:'buy',originLedgerId:'archived'}]},{instrumentKey:'CLOSED',quantity:'0',lots:[{acquisitionOperationId:'unused',originLedgerId:'other'}]}]});
 const refs=acquisitionOriginReferences([s],[]);expect(refs).toEqual([{ledgerId:'archived',operationId:'buy'}]);
 const listInvestmentOperations=vi.fn().mockResolvedValue({operations:[create(InvestmentOperationSchema,{id:'buy',ledgerId:'archived'}),create(InvestmentOperationSchema,{id:'unrelated',ledgerId:'archived'})]});
 const result=await loadAcquisitionOrigins({listInvestmentOperations},refs);
 expect(result.operations.map(o=>o.id)).toEqual(['buy']);expect(result.unavailable).toBe(0);
 expect(listInvestmentOperations).toHaveBeenCalledWith({ledgerId:'archived',limit:200,offset:0},undefined);
 expect(acquisitionOriginReferences([s],result.operations)).toEqual([]);
});
it('keeps resolved origins when another source is unavailable',async()=>{
 const listInvestmentOperations=vi.fn().mockImplementation(async({ledgerId})=>{if(ledgerId==='missing')throw Error('unavailable');return {operations:[create(InvestmentOperationSchema,{id:'buy',ledgerId})]};});
 const result=await loadAcquisitionOrigins({listInvestmentOperations},[{ledgerId:'known',operationId:'buy'},{ledgerId:'missing',operationId:'old'}]);
 expect(result.operations).toHaveLength(1);expect(result.unavailable).toBe(1);
});
