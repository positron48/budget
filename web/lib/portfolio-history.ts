import type {GetTrackerHistoryResponse, TrackerHistoryPoint} from "@/proto/budget/v1/investment_nav_pb";
export type HistoryScope = {fxPolicyVersion: bigint; fxMode: string; ledgerIds: string[]; currency: string; fxPolicyKey: string; fromDate: string; toDate: string};
type HistoryAPI = {getTrackerHistory(input: Omit<HistoryScope, "fxPolicyVersion" | "fxMode">, options: {signal: AbortSignal}): Promise<GetTrackerHistoryResponse>};
export function shiftHistoryDate(date: string, days: number): string {
  const at = new Date(date + "T00:00:00Z"); at.setUTCDate(at.getUTCDate() + days); return at.toISOString().slice(0, 10);
}
export function lastHistoryDate(): string {
  const today = new Intl.DateTimeFormat("sv-SE", {timeZone: "Europe/Moscow"}).format(new Date());
  return shiftHistoryDate(today, -1);
}
export async function loadPortfolioHistory(api: HistoryAPI, scope: HistoryScope, signal: AbortSignal, progress: (points: TrackerHistoryPoint[]) => void): Promise<TrackerHistoryPoint[]> {
  const {fxPolicyVersion, fxMode, ...request} = scope;
  const result: TrackerHistoryPoint[] = [];
  let cursor = scope.fromDate, basis: string | undefined, version: bigint | undefined;
  for (let pages = 0; pages < 119; pages++) {
    signal.throwIfAborted();
    const page = await api.getTrackerHistory({...request, fromDate: cursor}, {signal});
    signal.throwIfAborted();
    if (page.currency !== scope.currency || page.fxPolicyKey !== scope.fxPolicyKey || page.reportingTimezone !== "Europe/Moscow" || page.priceConvention !== "retrospective_close_next_utc_day_v1" || !["market", "cbr"].includes(page.fxMode) || page.fxPolicyVersion !== fxPolicyVersion || page.fxMode !== fxMode || !/^[a-f0-9]{64}$/.test(page.basisKey) || !page.points.length || page.points.length > 31) throw new Error("invalid_history");
    if (basis !== undefined && (basis !== page.basisKey || version !== page.fxPolicyVersion)) throw new Error("history_changed");
    basis = page.basisKey; version = page.fxPolicyVersion;
    for (const point of page.points) {
      if (point.date !== cursor || point.date > scope.toDate || !["ready", "incomplete"].includes(point.state) || !point.knownValue || !/^[a-f0-9]{64}$/.test(point.coverageKey) || !/^[0-9]+$/.test(point.knownValue.numerator) || !/^[1-9][0-9]*$/.test(point.knownValue.denominator) || point.positions < 0 || point.pricedPositions < 0 || point.pricedPositions > point.positions || point.state === "ready" && (point.issues.length > 0 || point.positions !== point.pricedPositions)) throw new Error("invalid_history_point");
      result.push(point); cursor = shiftHistoryDate(cursor, 1);
    }
    progress([...result]);
    if (!page.nextDate) {if (cursor !== shiftHistoryDate(scope.toDate, 1)) throw new Error("truncated_history"); return result;}
    if (page.nextDate !== cursor || cursor > scope.toDate) throw new Error("invalid_history_cursor");
  }
  throw new Error("history_limit");
}
// Unknown values never become zero-valued chart points. A subtotal remains
// visibly different from a complete securities valuation.
export function historyPointVisible(p: TrackerHistoryPoint): boolean {
  return !!p.knownValue && (p.pricedPositions > 0 || p.positions === 0 && p.state === "ready");
}
