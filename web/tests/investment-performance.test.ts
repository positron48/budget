import {describe,it,expect} from "vitest";
import {exactReturnPercent,annualReturnPercent} from "../lib/investment-performance";
describe("performance formatting",()=>{
 it("preserves rational precision, signs and percentage units",()=>{
  expect(exactReturnPercent({numerator:"1",denominator:"50"},"ru")).toBe("2,00%");
  expect(exactReturnPercent({numerator:"-1",denominator:"3"},"en")).toBe("-33.33%");
  expect(exactReturnPercent({numerator:"9007199254740993",denominator:"100"},"en")).toBe("9007199254740993.00%");
  expect(exactReturnPercent({numerator:"-1",denominator:"1000000"},"en")).toBe("0.00%");
 });
 it("distinguishes undefined from zero and handles extreme annualization",()=>{
  expect(annualReturnPercent(undefined,"en")).toBe("—");
  expect(annualReturnPercent(0,"en")).toBe("0.00%");
  expect(annualReturnPercent(Infinity,"en")).toBe("—");
  expect(annualReturnPercent(1e308,"en")).toBe("1.00e+310%");
 });
});

import {returnGeometry} from "../lib/investment-performance";
describe("daily return segments",()=>{
 const fraction=(n:string,d="1")=>({numerator:n,denominator:d});
 it("never joins a gap, calendar hole or different segment",()=>{
  const graph=returnGeometry([
   {date:"2026-01-01",state:"baseline",segment:1},
   {date:"2026-01-02",state:"calculated",segment:1,segmentReturn:fraction("1","50")},
   {date:"2026-01-03",state:"gap",segment:1,segmentReturn:fraction("999")},
   {date:"2026-01-04",state:"baseline",segment:2},
   {date:"2026-01-05",state:"calculated",segment:2,segmentReturn:fraction("-1","10")},
   {date:"2026-01-06",state:"calculated",segment:3,segmentReturn:fraction("1","10")},
   {date:"2026-01-08",state:"calculated",segment:3,segmentReturn:fraction("1","5")},
  ]);
  expect(graph.paths.map(p=>p.segment)).toEqual([1,2,3,3]);
  expect(graph.dots.map(p=>p.index)).toEqual([0,1,3,4,5,6]);
  expect(graph.min).toEqual(fraction("-1","10"));expect(graph.max).toEqual(fraction("1","5"));
 });
 it("uses exact fractions rather than rounded decimal strings",()=>{
  const graph=returnGeometry([
   {date:"2026-01-01",state:"calculated",segment:1,segmentReturn:fraction("9007199254740993000001","1000000000000")},
   {date:"2026-01-02",state:"calculated",segment:1,segmentReturn:fraction("9007199254740993000002","1000000000000")},
  ]);
  expect(graph.dots.map(p=>p.y)).toEqual([240,20]);
 });
 it("does not invent returns from a lone baseline or invalid evidence",()=>{
  expect(returnGeometry([{date:"2026-01-01",state:"baseline",segment:1}]).dots).toHaveLength(0);
  const graph=returnGeometry([
   {date:"2026-02-30",state:"calculated",segment:1,segmentReturn:fraction("1")},
   {date:"2026-03-01",state:"calculated",segment:1,segmentReturn:fraction("1","0")},
   {date:"2026-03-02",state:"calculated",segment:1,segmentReturn:fraction("0")},
  ]);
  expect(graph.dots).toEqual([{index:2,x:780,y:130}]);
 });
});

it("uses only authoritative base-100 indices and preserves segment gaps",()=>{
 const fraction=(n:string,d="1")=>({numerator:n,denominator:d});
 const graph=returnGeometry([
  {date:"2024-01-01",state:"baseline",segment:1,index:fraction("100")},
  {date:"2024-01-02",state:"calculated",segment:1,index:fraction("400","3"),segmentReturn:fraction("999")},
  {date:"2024-01-03",state:"gap",segment:1,index:fraction("99999")},
  {date:"2024-01-04",state:"baseline",segment:2,index:fraction("100")},
  {date:"2024-01-05",state:"calculated",segment:2,index:fraction("110")},
  {date:"2024-01-06",state:"calculated",segment:2,segmentReturn:fraction("10")},
 ],true);
 expect(graph.paths.map(p=>p.segment)).toEqual([1,2]);
 expect(graph.dots.map(p=>p.index)).toEqual([0,1,3,4]);
 expect(graph.min).toEqual(fraction("100"));expect(graph.max).toEqual(fraction("400","3"));
});
