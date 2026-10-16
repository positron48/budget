export type ChartPoint = {date: string; value?: number; segment?: string};

// Split before sampling: missing days and changes of source/coverage must never
// become a continuous line merely because the chart uses fewer points.
export function chartSegments(points: readonly ChartPoint[], limit = 400): ChartPoint[][] {
  const segments: ChartPoint[][] = [];
  for (const point of points) {
    if (point.value === undefined || !Number.isFinite(point.value)) continue;
    const last = segments.at(-1), previous = last?.at(-1);
    if (previous && Date.parse(point.date) - Date.parse(previous.date) === 86400000 && point.segment === previous.segment) last!.push(point);
    else segments.push([point]);
  }
  const total = segments.reduce((n, s) => n + s.length, 0);
  return segments.map(s => sampleExtrema(s, Math.max(4, Math.floor(limit * s.length / Math.max(1, total)))));
}

// Retain endpoints and each time bucket's extrema, in chronological order.
// These are actual observations, never average balances or summed prices.
export function sampleExtrema<T extends ChartPoint>(points: readonly T[], limit: number): T[] {
  limit = Math.max(4, Math.floor(limit));
  if (points.length <= limit) return [...points];
  const result = [points[0]], buckets = Math.floor((limit - 2) / 2), width = (points.length - 2) / buckets;
  for (let bucket = 0; bucket < buckets; bucket++) {
    const start = 1 + Math.floor(bucket * width), end = Math.min(points.length - 1, 1 + Math.floor((bucket + 1) * width));
    let min = start, max = start;
    for (let i = start + 1; i < end; i++) {
      if (points[i].value! < points[min].value!) min = i;
      if (points[i].value! > points[max].value!) max = i;
    }
    for (const i of [...new Set([min, max])].sort((a, b) => a - b)) result.push(points[i]);
  }
  result.push(points.at(-1)!);
  return result;
}

export function chartDateTicks(from: string, to: string, count = 6): string[] {
  const first = Date.parse(from), days = Math.round((Date.parse(to) - first) / 86400000);
  if (!Number.isFinite(first) || !Number.isFinite(days) || days < 0) return [];
  const length = Math.min(count, days + 1);
  return [...new Set(Array.from({length}, (_, i) => new Date(first + Math.round(days * i / Math.max(1, length - 1)) * 86400000).toISOString().slice(0, 10)))];
}
