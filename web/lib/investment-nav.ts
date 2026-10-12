export type ChartPoint = { date: string; state: string; knownValue?: { decimal: string } };

const scaled = (value: string) => {
  if (!/^-?\d+(\.\d{1,12})?$/.test(value)) return undefined;
  const negative = value.startsWith("-");
  const [whole, fraction = ""] = value.replace(/^-/, "").split(".");
  const number = BigInt(whole) * 1000000000000n + BigInt(fraction.padEnd(12, "0"));
  return negative ? -number : number;
};

// Convert only bounded relative coordinates to Number. Large money values and
// tiny differences never pass through floating-point arithmetic before scaling.
export function navGeometry(points: ChartPoint[], range?:{min:string;max:string}) {
  const values = points.map(p => p.state === "ready" && p.knownValue ? scaled(p.knownValue.decimal) : undefined);
  const valid = values.filter((v): v is bigint => v !== undefined);
  if (!valid.length) return { paths: [] as string[], dots: [] as { x: number; y: number; index: number }[], min: "", max: "" };
  const nativeMin = valid.reduce((a, b) => a < b ? a : b), nativeMax = valid.reduce((a, b) => a > b ? a : b);
  const min=range?scaled(range.min)??nativeMin:nativeMin, max=range?scaled(range.max)??nativeMax:nativeMax;
  const first = Date.parse(points[0].date + "T00:00:00Z"), last = Date.parse(points.at(-1)!.date + "T00:00:00Z");
  const paths: string[] = [], dots: { x: number; y: number; index: number }[] = [];
  let path = "", previous = NaN;
  values.forEach((v, index) => {
    const date = Date.parse(points[index].date + "T00:00:00Z");
    if (v === undefined || !Number.isFinite(date)) { if (path) paths.push(path); path = ""; previous = NaN; return; }
    if (date - previous !== 86400000 && path) { paths.push(path); path = ""; }
    const x = last === first ? 400 : 20 + (date - first) / (last - first) * 760;
    const y = max === min ? 130 : 240 - Number((v - min) * 220000n / (max - min)) / 1000;
    path += `${path ? " L" : "M"}${x},${y}`;
    dots.push({ x, y, index }); previous = date;
  });
  if (path) paths.push(path);
  return { paths, dots, min: range?.min??points[values.indexOf(nativeMin)].knownValue!.decimal, max: range?.max??points[values.indexOf(nativeMax)].knownValue!.decimal };
}
export const navDecimal = (s: string) => s.replace(/(\.\d*?[1-9])0+$|\.0+$/, "$1");

// Both lines use the same money/index scale; independently autoscaling them
// would visually imply identical returns for economically different paths.
export function comparisonGeometry(left:ChartPoint[],right:ChartPoint[]) {
 const all=navGeometry([...left,...right]);
 const range=all.min!==""?{min:all.min,max:all.max}:undefined;
 return {left:navGeometry(left,range),right:navGeometry(right,range),min:all.min,max:all.max};
}

// A current snapshot is a separate marker. Closed paths never consume it; a
// dashed connector is possible only from the adjacent complete closed day.
export function currentNAVGeometry(points:ChartPoint[],current?:{provisional:boolean;complete:boolean;lastCompletedDate:string;knownValue?:{decimal:string}}){
 const ordinary=navGeometry(points);
 if(!current?.provisional||!current.complete||!current.knownValue||points.at(-1)?.date!==current.lastCompletedDate)return {...ordinary,current:undefined,connector:undefined};
 const day=Date.parse(current.lastCompletedDate+"T00:00:00Z");
 if(!Number.isFinite(day))return {...ordinary,current:undefined,connector:undefined};
 const point={date:new Date(day+86400000).toISOString().slice(0,10),state:current.complete?"ready":"incomplete",knownValue:current.knownValue};
 const all=navGeometry([...points,point]);
 const range=all.min!==""?{min:all.min,max:all.max}:undefined;
 const history=navGeometry([...points,{date:point.date,state:"unavailable"}],range);
 const dot=all.dots.find(p=>p.index===points.length);
 const previous=history.dots.find(p=>p.index===points.length-1);
 return {...history,min:all.min,max:all.max,current:dot,connector:dot&&previous?`M${previous.x},${previous.y} L${dot.x},${dot.y}`:undefined};
}
