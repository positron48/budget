import type {InvestmentOperation} from "@/proto/budget/v1/investment_pb";
// Date-only values stay civil dates in their recorded zone. Never format the
// calculation boundary as if it were a verified execution time.
export function operationTime(operation:InvestmentOperation,settlement=false,locale="ru",dayOnlyLabel=locale==="ru"?"Только дата":"Date only") {
 const timing=operation.timing;
 const immediate=!["buy","sell","amortization","redemption","fx"].includes(operation.kind);
 const civil=settlement?(timing?.settlementDate || (operation.settlementAssumed||immediate?timing?.occurredDate:undefined)):timing?.occurredDate;
 if(civil)return `${civil} · ${dayOnlyLabel} · ${timing!.timezone}`;
 const stamp=settlement?operation.settlementAt:operation.occurredAt;
 return stamp?new Date(Number(stamp.seconds)*1000+stamp.nanos/1000000).toLocaleString(locale,{timeZone:"Europe/Moscow"}):"—";
}
