import {it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentOperationTimingSchema} from "../proto/budget/v1/investment_pb";
import {transferTimePayload} from "../lib/investment-transfer-time";
import {localAssetTime} from "../lib/assets";
it("keeps civil date and explicit zone without fabricating an execution timestamp",()=>{
 const got=transferTimePayload("2024-03-31",true,"Europe/Berlin");expect(got.occurredAt).toBeUndefined();expect(got.timing?.occurredDate).toBe("2024-03-31");expect(got.timing?.timezone).toBe("Europe/Berlin");
});
it("preserves microseconds and source precision when an execution timestamp is unchanged",()=>{
 const stamp={seconds:1704153600n,nanos:123456000},timing=create(InvestmentOperationTimingSchema,{timezone:"Europe/Moscow",sourceDate:"2024-01-02"});
 const got=transferTimePayload(localAssetTime(new Date(Number(stamp.seconds)*1000+stamp.nanos/1e6)),false,"Europe/Moscow",stamp,timing);expect(got.occurredAt?.nanos).toBe(123456000);expect(got.timing?.sourceDate).toBe("2024-01-02");
});
