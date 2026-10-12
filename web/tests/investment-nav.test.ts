import { describe, expect, it } from "vitest";
import { navGeometry, comparisonGeometry, currentNAVGeometry } from "../lib/investment-nav";
const point = (date: string, state: string, decimal = "100") => ({date, state, knownValue: {decimal}});
describe("NAV chart", () => {
 it("preserves gaps and does not plot a known subtotal or leaked stale amount", () => {
  const graph = navGeometry([point("2024-01-01","ready"),point("2024-01-02","incomplete"),point("2024-01-03","ready"),point("2024-01-04","stale"),point("2024-01-05","unavailable"),point("2024-01-06","ready")]);
  expect(graph.paths).toHaveLength(3);expect(graph.dots.map(p=>p.index)).toEqual([0,2,5]);
 });
 it("keeps tiny differences between values above safe Number precision", () => {
  const graph = navGeometry([point("2024-01-01","ready","9007199254740993.000000000001"),point("2024-01-02","ready","9007199254740993.000000000002")]);
  expect(graph.dots.map(p=>p.y)).toEqual([240,20]);expect(graph.paths).toHaveLength(1);
 });
 it("plots zero, splits omitted dates, and handles a single negative point",()=>{
  const graph=navGeometry([point("2024-01-01","ready","0"),point("2024-01-03","ready","-100")]);
  expect(graph.paths).toHaveLength(2);expect(graph.dots.map(p=>p.y)).toEqual([20,240]);
  expect(navGeometry([point("2024-01-01","ready","-100")]).dots).toEqual([{x:400,y:130,index:0}]);
 });
});

it("uses one scale for both comparison lines",()=>{
 const graph=comparisonGeometry([point("2024-01-01","ready","1000"),point("2024-01-02","ready","1200")],[point("2024-01-01","ready","1000"),point("2024-01-02","ready","1700")]);
 expect(graph.min).toBe("1000");expect(graph.max).toBe("1700");
 expect(graph.left.dots[1].y).toBeCloseTo(177.143,3);expect(graph.right.dots[1].y).toBe(20);
});

it("keeps a provisional marker separate from closed paths and gaps",()=>{
 const points=[point("2024-01-01","ready","100"),point("2024-01-02","ready","110")];
 const now={provisional:true,complete:true,lastCompletedDate:"2024-01-02",knownValue:{decimal:"120"}};
 const graph=currentNAVGeometry(points,now);
 expect(graph.dots.map(p=>p.index)).toEqual([0,1]);expect(graph.current).toEqual({x:780,y:20,index:2});expect(graph.connector).toContain("L780,20");expect(graph.paths[0]).not.toContain("780,20");
 expect(currentNAVGeometry(points,{...now,complete:false}).paths).toEqual(navGeometry(points).paths);
 expect(currentNAVGeometry(points,{...now,lastCompletedDate:"2024-01-03"}).current).toBeUndefined();
 const gap=currentNAVGeometry([points[0],point("2024-01-02","incomplete")],now);expect(gap.current).toBeDefined();expect(gap.connector).toBeUndefined();
 const onlyCurrent=currentNAVGeometry([point("2024-01-02","incomplete")],now);expect(onlyCurrent.min).toBe("120");expect(onlyCurrent.max).toBe("120");expect(onlyCurrent.dots).toHaveLength(0);expect(onlyCurrent.current).toBeDefined();
 const zero=currentNAVGeometry(points,{...now,knownValue:{decimal:"0"}});expect(zero.current).toBeDefined();
});
