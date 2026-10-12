import {describe,it,expect} from "vitest";
import {investmentPeriodRange,investmentPeriods,validInvestmentDate} from "../lib/investment-period";
describe("investment calendar periods",()=>{
 it("validates real dates without browser timezone normalization",()=>{
  expect(validInvestmentDate("2024-02-29")).toBe(true);
  for(const bad of ["2023-02-29","2024-02-30","2024-13-01","2024-1-01","", "2024-03-01T00:00:00Z"])expect(validInvestmentDate(bad)).toBe(false);
 });
 it("uses inclusive days and exact calendar month/year boundaries",()=>{
  const range=(p:typeof investmentPeriods[number],end="2024-03-31")=>investmentPeriodRange(p,"2019-01-01",end);
  expect(range("7d")).toEqual({from:"2024-03-25",to:"2024-03-31"});
  expect(range("1m")).toEqual({from:"2024-03-01",to:"2024-03-31"});
  expect(range("3m")).toEqual({from:"2024-01-01",to:"2024-03-31"});
  expect(range("6m")).toEqual({from:"2023-10-01",to:"2024-03-31"});
  expect(range("1y","2024-02-29")).toEqual({from:"2023-03-01",to:"2024-02-29"});
  expect(range("5y","2024-02-29")).toEqual({from:"2019-03-01",to:"2024-02-29"});
  expect(range("ytd")).toEqual({from:"2024-01-01",to:"2024-03-31"});
  expect(range("all")).toEqual({from:"2019-01-01",to:"2024-03-31"});
 });
 it("clips to registered history and refuses invalid bounds",()=>{
  for(const p of investmentPeriods)expect(investmentPeriodRange(p,"2024-03-30","2024-03-31")).toEqual({from:"2024-03-30",to:"2024-03-31"});
  expect(investmentPeriodRange("7d","2024-04-01","2024-03-31")).toBeUndefined();
  expect(investmentPeriodRange("1m","2024-02-30","2024-03-31")).toBeUndefined();
 });
});
