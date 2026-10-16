import {create} from "@bufbuild/protobuf";
import {expect,it} from "vitest";
import {GetMarketBondFactsResponseSchema} from "@/proto/budget/v1/market_data_pb";
import {bondYields} from "@/lib/portfolio-bond-yields";
const at=1800000000, year=365.25*86400;
const stamp=(seconds:number)=>({seconds:BigInt(Math.round(seconds)),nanos:0});
function facts(){return create(GetMarketBondFactsResponseSchema,{asOf:stamp(at),terms:{nominal:{amount:"1000",currency:"RUB"},currentAccrued:{amount:"0",currency:"RUB"},maturity:stamp(at+year),floating:false,amortizing:false,perpetual:false,couponFrequency:2},coupons:[{date:stamp(at+year/2),pay:{amount:"50",currency:"RUB"}}]});}
const price={value:1000,currency:"RUB",at};
it("annualizes the current fixed coupon and solves effective maturity yield at par",()=>{const r=bondYields(facts(),price);expect(r.coupon).toBe(.1);expect(r.maturity).toBeCloseTo(.1025,6);expect(bondYields(facts(),{...price,value:900}).maturity!).toBeGreaterThan(r.maturity!);});
it("includes accrued interest in the maturity purchase price",()=>{const f=facts();f.terms!.currentAccrued!.amount="25";expect(bondYields(f,price).maturity!).toBeLessThan(bondYields(facts(),price).maturity!);});
it("does not project floating, amortizing, perpetual or unknown terms",()=>{for(const key of ["floating","amortizing","perpetual"] as const){for(const v of [true,undefined]){const f=facts();f.terms![key]=v;const r=bondYields(f,price);expect(r.coupon).toBe(.1);expect(r.maturity).toBeUndefined();}}});
it("refuses mismatched currencies, stale facts and absent future coupons",()=>{expect(bondYields(facts(),{...price,currency:"USD"})).toEqual({});expect(bondYields(facts(),{...price,at:at+86401})).toEqual({});const f=facts();f.coupons=[];expect(bondYields(f,price)).toEqual({});f.coupons=facts().coupons;f.terms!.currentAccrued!.currency="USD";expect(bondYields(f,price).maturity).toBeUndefined();});
