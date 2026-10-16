import type {GetMarketBondFactsResponse} from "@/proto/budget/v1/market_data_pb";

// Indicative annual yields at the current clean price. The maturity estimate
// assumes the next fixed coupon continues at the stated frequency, with bullet
// redemption. Variable, amortizing and perpetual bonds need another model.
export function bondYields(facts: GetMarketBondFactsResponse | undefined, price: {value: number; currency: string; at: number} | undefined) {
  const terms = facts?.terms;
  if (!terms || !price || !Number.isFinite(price.value) || price.value <= 0 || !facts?.asOf || Math.abs(Number(facts.asOf.seconds) - price.at) > 86400) return {};
  const frequency = terms.couponFrequency;
  if (!frequency || frequency < 1 || frequency > 366) return {};
  const coupons = facts!.coupons.filter(c => c.date && Number(c.date.seconds) > price.at && c.pay?.currency === price.currency && Number.isFinite(Number(c.pay.amount)) && Number(c.pay.amount) > 0).sort((a,b) => Number(a.date!.seconds - b.date!.seconds));
  const next = coupons[0];
  if (!next) return {};
  const annualCoupon = Number(next.pay!.amount) * frequency;
  const coupon = annualCoupon / price.value;
  if(!Number.isFinite(coupon))return {};
  const nominal = terms.nominal, accrued = terms.currentAccrued, maturity = Number(terms.maturity?.seconds || 0n);
  if (terms.floating !== false || terms.amortizing !== false || terms.perpetual !== false || nominal?.currency !== price.currency || !Number.isFinite(Number(nominal.amount)) || Number(nominal.amount) <= 0 || accrued?.currency !== price.currency || !Number.isFinite(Number(accrued.amount)) || Number(accrued.amount) < 0 || maturity <= price.at) return {coupon};
  const years = (maturity - price.at) / (365.25 * 86400), interval = 1 / frequency, first = (Number(next.date!.seconds) - price.at) / (365.25 * 86400);
  if (first <= 0 || first > years + 1 / 365.25 || first > interval * 1.5 || years > 100) return {coupon};
  const payments: {t: number; amount: number}[] = [];
  for (let t = first; t <= years + 1 / 365.25; t += interval) {if(payments.length>=5000)return {coupon};payments.push({t: Math.min(t,years),amount: Number(next.pay!.amount)});}
  payments.push({t: years, amount: Number(nominal.amount)});
  const dirty = price.value + Number(accrued.amount);
  const present = (rate: number) => payments.reduce((sum,p)=>sum+p.amount/Math.pow(1+rate,p.t),0)-dirty;
  let low = -.999, high = 1;
  while (present(high)>0 && high<1024) high*=2;
  if (present(low)<0 || present(high)>0) return {coupon};
  for(let i=0;i<100;i++){const mid=(low+high)/2;if(present(mid)>0)low=mid;else high=mid;}
  const maturityYield=(low+high)/2;
  return {coupon, maturity: Number.isFinite(maturityYield)?maturityYield:undefined};
}
