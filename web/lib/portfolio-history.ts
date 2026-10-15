import type {GetTrackerHistoryResponse, GetInvestmentPerformanceResponse, TrackerHistoryPoint} from "@/proto/budget/v1/investment_nav_pb";
import {Code, ConnectError} from "@connectrpc/connect";
export type HistoryScope = {fxPolicyVersion: bigint; fxMode: string; ledgerIds: string[]; currency: string; fxPolicyKey: string; fromDate: string; toDate: string};
type HistoryAPI = {getTrackerHistory(input: Omit<HistoryScope, "fxPolicyVersion" | "fxMode"> & {performanceFromDate?: string}, options: {signal: AbortSignal}): Promise<GetTrackerHistoryResponse>};
export function shiftHistoryDate(date: string, days: number): string {
  const at = new Date(date + "T00:00:00Z"); at.setUTCDate(at.getUTCDate() + days); return at.toISOString().slice(0, 10);
}
export function lastHistoryDate(): string {
  const today = new Intl.DateTimeFormat("sv-SE", {timeZone: "Europe/Moscow"}).format(new Date());
  return shiftHistoryDate(today, -1);
}
export async function loadPortfolioHistory(api: HistoryAPI, scope: HistoryScope, signal: AbortSignal, progress: (points: TrackerHistoryPoint[]) => void): Promise<TrackerHistoryPoint[]> {
  return (await historyPages(api, scope, signal, progress, false)).points;
}
type PerformanceAPI = HistoryAPI & {getTrackerPerformance(input: Omit<HistoryScope, "fxPolicyVersion" | "fxMode"> & {snapshotTokens: string[]}, options: {signal: AbortSignal}): Promise<GetInvestmentPerformanceResponse>};
export async function loadPortfolioPerformanceHistory(api: PerformanceAPI, scope: HistoryScope, signal: AbortSignal, progress: (points: TrackerHistoryPoint[]) => void) {
  const result = await historyPages(api, scope, signal, progress, true);
  if (!result.tokens.length) return {points: result.points, performance: undefined};
  const {fxPolicyVersion: _version, fxMode: _mode, ...request} = scope;
  let performance: GetInvestmentPerformanceResponse;
  try {performance = await api.getTrackerPerformance({...request, snapshotTokens: result.tokens}, {signal});}
  catch (error) {if (error instanceof ConnectError && error.code === Code.FailedPrecondition) throw new Error("history_changed"); throw error;}
  signal.throwIfAborted();
  if (performance.currency !== scope.currency || performance.fxPolicyKey !== scope.fxPolicyKey || performance.fromDate !== scope.fromDate || performance.toDate !== scope.toDate || performance.historyScope !== "recorded_securities_strategy" || performance.twrConvention !== "securities_daily_end_of_day_flows" || performance.reportingTimezone !== "Europe/Moscow") throw new Error("invalid_performance");
  for (const amount of [performance.profit, performance.netContributions, performance.twr]) {
    if (amount && (!/^-?[0-9]+$/.test(amount.numerator) || !/^[1-9][0-9]*$/.test(amount.denominator) || !Number.isFinite(Number(amount.numerator) / Number(amount.denominator)))) throw new Error("invalid_performance");
  }
  if ((!performance.reason && (!performance.profit || !performance.netContributions)) || (!performance.twrReason && !performance.twr) || (!performance.xirrReason && performance.xirr === undefined) || (performance.xirr !== undefined && (!Number.isFinite(performance.xirr) || performance.xirr <= -1 || !performance.xirrAnnualized)) || !Number.isInteger(performance.xirrDays) || performance.xirrDays < 0 || performance.xirrDays > 3661) throw new Error("invalid_performance");
  return {points: result.points, performance};
}
async function historyPages(api: HistoryAPI, scope: HistoryScope, signal: AbortSignal, progress: (points: TrackerHistoryPoint[]) => void, performance: boolean): Promise<{points: TrackerHistoryPoint[]; tokens: string[]}> {
  const {fxPolicyVersion, fxMode, ...request} = scope;
  const result: TrackerHistoryPoint[] = [];
  const tokens: string[] = []; let hasSnapshots = true;
  let cursor = scope.fromDate, basis: string | undefined, version: bigint | undefined;
  for (let pages = 0; pages < 119; pages++) {
    signal.throwIfAborted();
    const page = await api.getTrackerHistory({...request, fromDate: cursor, ...(performance ? {performanceFromDate: scope.fromDate} : {})}, {signal});
    signal.throwIfAborted();
    if (page.currency !== scope.currency || page.fxPolicyKey !== scope.fxPolicyKey || page.reportingTimezone !== "Europe/Moscow" || page.priceConvention !== "retrospective_close_next_utc_day_v1" || !["market", "cbr"].includes(page.fxMode) || page.fxPolicyVersion !== fxPolicyVersion || page.fxMode !== fxMode || !/^[a-f0-9]{64}$/.test(page.basisKey) || !page.points.length || page.points.length > 31) throw new Error("invalid_history");
    if (basis !== undefined && (basis !== page.basisKey || version !== page.fxPolicyVersion)) throw new Error("history_changed");
    basis = page.basisKey; version = page.fxPolicyVersion;
    if (performance) {if (!page.performanceSnapshot) hasSnapshots = false; else {if (page.performanceSnapshot.length > 32768) throw new Error("invalid_performance"); tokens.push(page.performanceSnapshot);}}
    for (const point of page.points) {
      if (point.date !== cursor || point.date > scope.toDate || !["ready", "incomplete"].includes(point.state) || !point.knownValue || !/^[a-f0-9]{64}$/.test(point.coverageKey) || !/^[0-9]+$/.test(point.knownValue.numerator) || !/^[1-9][0-9]*$/.test(point.knownValue.denominator) || point.positions < 0 || point.pricedPositions < 0 || point.pricedPositions > point.positions || point.state === "ready" && (point.issues.length > 0 || point.positions !== point.pricedPositions)) throw new Error("invalid_history_point");
      result.push(point); cursor = shiftHistoryDate(cursor, 1);
    }
    progress([...result]);
    if (!page.nextDate) {if (cursor !== shiftHistoryDate(scope.toDate, 1)) throw new Error("truncated_history"); return {points: result, tokens: hasSnapshots ? tokens : []};}
    if (page.nextDate !== cursor || cursor > scope.toDate) throw new Error("invalid_history_cursor");
  }
  throw new Error("history_limit");
}
// Unknown values never become zero-valued chart points. A subtotal remains
// visibly different from a complete securities valuation.
export function historyPointVisible(p: TrackerHistoryPoint): boolean {
  return !!p.knownValue && (p.pricedPositions > 0 || p.positions === 0 && p.state === "ready");
}
