import {describe,it,expect} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentPayoutAmountSchema} from "../proto/budget/v1/investment_pb";
import {parseForecastInstant,formatForecastInstant,forecastMaximum,forecastBarPercent} from "../lib/investment-forecast";
const amount=(numerator:string,denominator="1")=>create(InvestmentPayoutAmountSchema,{numerator,denominator,decimal:"unused"});
describe("forecast cutoffs and chart precision",()=>{
 it("preserves explicit offsets and microseconds",()=>{
  const value=parseForecastInstant("2026-10-12T18:00:00.123456+03:00");
  expect(value).toEqual({seconds:BigInt(Date.parse("2026-10-12T15:00:00Z")/1000),nanos:123456000});
  expect(formatForecastInstant(value)).toBe("2026-10-12T15:00:00.123456Z");
  expect(formatForecastInstant(parseForecastInstant("2024-02-29T00:00:00Z"))).toBe("2024-02-29T00:00:00Z");
 });
 it("rejects guessed timezone, normalized invalid dates and excess precision",()=>{
  for(const s of ["2026-10-12T18:00:00","2026-02-30T18:00:00Z","2026-10-12T24:00:00Z","2026-10-12T18:00:00.1234567Z","2026-10-12T18:00:00+25:00"]){expect(()=>parseForecastInstant(s)).toThrow()}
 });
 it("scales exact large fractions without floating point money conversion",()=>{
  const small=amount("9007199254740993123456789012","1000000000000"),large=amount("18014398509481986246913578024","1000000000000");
  expect(forecastMaximum([undefined,small,large])).toBe(large);
  expect(forecastBarPercent(small,large)).toBe(50);
  expect(forecastBarPercent(large,small)).toBe(100);
  expect(forecastBarPercent(amount("0"),large)).toBe(0);
  expect(forecastBarPercent(undefined,large)).toBeUndefined();
  expect(forecastBarPercent(small,amount("0"))).toBeUndefined();
  expect(forecastBarPercent(amount("1","0"),large)).toBeUndefined();
 });
});
