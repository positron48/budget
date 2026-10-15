import type {GetInvestmentPerformanceResponse} from "@/proto/budget/v1/investment_nav_pb";
import {shiftHistoryDate} from "./portfolio-history";
import {add,negate} from "./portfolio-tracker";
import type {Fraction} from "./investment-positions";
const valid=(v:Fraction|undefined):v is Fraction=>!!v&&v.numerator.length<=256&&v.denominator.length<=256&&/^-?[0-9]+$/.test(v.numerator)&&/^[1-9][0-9]*$/.test(v.denominator);
// Read the signed-snapshot analytics result; never subtract current quotes or
// a partial known subtotal from a historical full value. Flows already include
// purchases/sales and payouts exactly once in the cashless strategy convention.
export function portfolioDayResult(p:GetInvestmentPerformanceResponse|undefined,date:string){
 if(!p||p.fromDate!==date||p.toDate!==date||p.analyticsConvention!=="retrospective_cashless_strategy_v1"||p.reportingTimezone!=="Europe/Moscow"||p.analyticsPoints.length!==2)return;
 const [before,after]=p.analyticsPoints;
 if(before.date!==shiftHistoryDate(date,-1)||after.date!==date||after.flowReason||![before.value,after.value,after.flow,after.dailyProfit].every(valid))return;
 const calculated=add(after.value!,negate(before.value!),negate(after.flow!));
 if(BigInt(calculated.numerator)*BigInt(after.dailyProfit!.denominator)!==BigInt(after.dailyProfit!.numerator)*BigInt(calculated.denominator))return;
 return {profit:after.dailyProfit!,before:before.value!,after:after.value!,flow:after.flow!};
}
