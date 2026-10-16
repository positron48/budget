import {describe, it, expect} from "vitest";
import {chartSegments, sampleExtrema, chartDateTicks} from "@/lib/portfolio-chart";
const day=(i:number)=>new Date(Date.UTC(2019,0,1+i)).toISOString().slice(0,10);
describe("chart sampling",()=>{
 it("bounds a ten-year line and preserves actual extrema and endpoints",()=>{
  const rows=Array.from({length:3660},(_,i)=>({date:day(i),value:Math.sin(i/10)}));
  rows[1234].value=1000;rows[2001].value=-1000;
  const actual=sampleExtrema(rows,400);
  expect(actual.length).toBeLessThanOrEqual(400);
  expect(actual[0]).toBe(rows[0]);expect(actual.at(-1)).toBe(rows.at(-1));
  expect(actual).toContain(rows[1234]);expect(actual).toContain(rows[2001]);
  expect(actual.every(p=>rows.includes(p))).toBe(true);
  expect(actual.every((p,i)=>!i||p.date>actual[i-1].date)).toBe(true);
 });
 it("does not bridge missing dates or changing source/coverage",()=>{
  const points=[{date:day(0),value:1,segment:"a"},{date:day(1),value:2,segment:"a"},{date:day(2),value:undefined,segment:"a"},{date:day(3),value:4,segment:"a"},{date:day(4),value:5,segment:"b"}];
  expect(chartSegments(points)).toEqual([[points[0],points[1]],[points[3]],[points[4]]]);
 });
 it("provides intermediate date ticks without duplicating single-day labels",()=>{
  const ticks=chartDateTicks(day(0),day(3660));
  expect(ticks).toHaveLength(6);expect(ticks[0]).toBe(day(0));expect(ticks.at(-1)).toBe(day(3660));
  expect(chartDateTicks(day(0),day(0))).toEqual([day(0)]);
 });
});
