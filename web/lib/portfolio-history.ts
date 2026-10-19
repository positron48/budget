import {chartHistoryPages,chartPageDays} from "./portfolio-history-pages";
import type {GetTrackerHistoryResponse, GetTrackerChartResponse, GetInvestmentPerformanceResponse, TrackerHistoryPoint} from "@/proto/budget/v1/investment_nav_pb";
import {analyticsNumber} from "./portfolio-analytics";
import {Code, ConnectError} from "@connectrpc/connect";
export type HistoryScope = {instrumentKeys?:string[];groupIds?: string[]; classificationVersion?: bigint; fxPolicyVersion: bigint; fxMode: string; ledgerIds: string[]; currency: string; fxPolicyKey: string; fromDate: string; toDate: string};
type HistoryAPI = {getTrackerHistory(input: Omit<HistoryScope, "fxPolicyVersion" | "fxMode" | "classificationVersion"> & {performanceFromDate?: string;monetaryOnly?:boolean;pageDays?:number;compactIssues?:boolean}, options: {signal: AbortSignal}): Promise<GetTrackerHistoryResponse>};
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
type PerformanceAPI = HistoryAPI & {getTrackerChart?(input:Omit<HistoryScope,"fxPolicyVersion"|"fxMode"|"classificationVersion">,options:{signal:AbortSignal;timeoutMs?:number}):Promise<GetTrackerChartResponse>;getTrackerPerformance(input: Omit<HistoryScope, "fxPolicyVersion" | "fxMode" | "classificationVersion"> & {snapshotTokens: string[];monetaryOnly?:boolean}, options: {signal: AbortSignal}): Promise<GetInvestmentPerformanceResponse>};
export type PerformanceHistoryResult = {points:TrackerHistoryPoint[];basisKey:string;performance?:GetInvestmentPerformanceResponse;partialPoints?:TrackerHistoryPoint[];partialBasisKey?:string;partialPerformance?:GetInvestmentPerformanceResponse;growthPerformance?:GetInvestmentPerformanceResponse;excludedKeys?:string[];partialUnavailable?:boolean};
export async function loadPortfolioPerformanceHistory(api: PerformanceAPI, scope: HistoryScope, signal: AbortSignal, progress: (points: TrackerHistoryPoint[], loaded?:number) => void, allowPartial=true):Promise<PerformanceHistoryResult> {
  if(api.getTrackerChart){
    const {fxPolicyVersion:_v,fxMode:_m,classificationVersion:_c,...request}=scope;
    try {
      // A cold multi-year projection must not block the first visible history page.
      const chart=await api.getTrackerChart(request,{signal,timeoutMs:4000});signal.throwIfAborted();
      if(!chart.portfolio?.performance||!chart.portfolio.pages.length)throw new Error("invalid_chart");
      const calculations=[chart.portfolio,...(chart.knownPortfolio?[chart.knownPortfolio]:[]),...(chart.growthPortfolio?[chart.growthPortfolio]:[])].map(c=>({...c,pages:c.pages}));
      // Run the same continuity, exact arithmetic and selection validators as
      // paginated history. Projection pages carry no reusable signed capability.
      const local:PerformanceAPI={
        async getTrackerHistory(input){
          const calculation=calculations.find(c=>historySelectionEqual(c.performance!.instrumentKeys,input.instrumentKeys));
          const page=calculation?.pages.find(p=>p.points[0]?.date===input.fromDate);
          if(page)return {...page,performanceSnapshot:"derived_projection"};
          if(input.fromDate===scope.toDate&&calculation)return {...calculation.pages[calculation.pages.length-1],performanceSnapshot:"derived_projection"};
          throw new Error("invalid_chart_scope");
        },
        async getTrackerPerformance(input){const c=calculations.find(c=>historySelectionEqual(c.performance!.instrumentKeys,input.instrumentKeys));if(!c?.performance)throw new Error("invalid_chart_scope");return c.performance;}
      };
      const validated=await loadPortfolioPerformanceHistory(local,scope,signal,progress,allowPartial);
      if(!historySelectionEqual(chart.excludedKeys,validated.excludedKeys))throw new Error("invalid_chart_scope");
      if(chart.growthPortfolio?.performance){
        const growth=await loadPortfolioPerformanceHistory(local,{...scope,instrumentKeys:chart.growthPortfolio.performance.instrumentKeys},signal,()=>{},false);
        validated.growthPerformance=growth.performance;
      }
      return validated;
    } catch(error){
      signal.throwIfAborted();
      if(!(error instanceof ConnectError&&[Code.Unimplemented,Code.DeadlineExceeded].includes(error.code)))throw error;
      if(error.code===Code.DeadlineExceeded){
        const paginated:PerformanceAPI={getTrackerHistory:(input,options)=>api.getTrackerHistory(input,options),getTrackerPerformance:(input,options)=>api.getTrackerPerformance(input,options)};
        const previewController=new AbortController();
        let recentPoints:TrackerHistoryPoint[]|undefined,loaded=0,finished=false;
        const recentFrom=shiftHistoryDate(scope.toDate,1-chartPageDays);
        if(recentFrom>scope.fromDate){
          // Display one independently validated recent window while older pages
          // load. Never merge snapshots from separate reads into one history.
          void loadPortfolioHistory(paginated,{...scope,fromDate:recentFrom},AbortSignal.any([signal,previewController.signal]),points=>{if(!finished){recentPoints=points;progress(points,Math.max(loaded,points.length));}}).catch(()=>{});
        }
        try {
          const result=await loadPortfolioPerformanceHistory(paginated,scope,signal,points=>{loaded=points.length;progress(recentPoints||points,loaded);},allowPartial);
          finished=true;if(recentPoints)progress(result.points,result.points.length);return result;
        }finally{finished=true;previewController.abort();}
      }
    }
  }
  const result = await historyPages(api, scope, signal, progress, true);
  if (!result.tokens.length) return {points: result.points, basisKey:result.basisKey, performance: undefined};
  const {fxPolicyVersion: _version, fxMode: _mode, classificationVersion: _classification, ...request} = scope;
  let performance: GetInvestmentPerformanceResponse;
  try {performance = await api.getTrackerPerformance({...request, monetaryOnly:!!scope.groupIds?.length,snapshotTokens: result.tokens}, {signal});}
  catch (error) {if (error instanceof ConnectError && error.code === Code.FailedPrecondition) throw new Error("history_changed"); throw error;}
  signal.throwIfAborted();
  if (performance.currency !== scope.currency || performance.fxPolicyKey !== scope.fxPolicyKey || performance.fromDate !== scope.fromDate || performance.toDate !== scope.toDate || performance.historyScope !== (scope.groupIds?.length?"recorded_group_securities_strategy":"recorded_securities_strategy") || performance.twrConvention !== "securities_daily_end_of_day_flows" || performance.reportingTimezone !== "Europe/Moscow") throw new Error("invalid_performance");
  if(!historySelectionEqual(performance.instrumentKeys,scope.instrumentKeys)||!historySelectionEqual(performance.groupIds,scope.groupIds)||performance.monetaryOnly!==!!scope.groupIds?.length)throw new Error("invalid_performance_scope");
  if(scope.groupIds?.length&&(performance.twr||performance.xirr!==undefined||performance.twrPoints.length||performance.twrReason!=="group_returns_unavailable"||performance.xirrReason!=="group_returns_unavailable"||scope.classificationVersion!==undefined&&performance.classificationVersion!==scope.classificationVersion))throw new Error("invalid_group_performance");
  for (const amount of [performance.profit, performance.netContributions, performance.twr]) {
    if (amount && (!/^-?[0-9]+$/.test(amount.numerator) || !/^[1-9][0-9]*$/.test(amount.denominator) || analyticsNumber(amount)===undefined)) throw new Error("invalid_performance");
  }
  if ((!performance.reason && (!performance.profit || !performance.netContributions)) || (!performance.twrReason && !performance.twr) || (!performance.xirrReason && performance.xirr === undefined) || (performance.xirr !== undefined && (!Number.isFinite(performance.xirr) || performance.xirr <= -1 || !performance.xirrAnnualized)) || !Number.isInteger(performance.xirrDays) || performance.xirrDays < 0 || performance.xirrDays > 3661) throw new Error("invalid_performance");
  const rational = (v: {numerator: string; denominator: string} | undefined) => !v || /^-?[0-9]+$/.test(v.numerator) && /^[1-9][0-9]*$/.test(v.denominator) && analyticsNumber(v)!==undefined;
  const first = shiftHistoryDate(scope.fromDate, -1), count = (Date.parse(scope.toDate) - Date.parse(first)) / 86400000 + 1;
  if (performance.twrPoints.length && (performance.twrPoints.length !== count || performance.twrPoints.some((p, i) => p.date !== shiftHistoryDate(first, i) || !["baseline", "calculated", "gap", "unfunded"].includes(p.state) || !Number.isInteger(p.segment) || p.segment < 0 || !rational(p.index)))) throw new Error("invalid_performance");
  if(performance.analyticsPoints.length){
    if(performance.analyticsConvention!=="retrospective_cashless_strategy_v1"||performance.analyticsPoints.length!==count)throw new Error("invalid_analytics");
    for(const [i,d] of performance.analyticsPoints.entries())if(d.date!==shiftHistoryDate(first,i)||![d.value,d.flow,d.contributions,d.invested,d.profit,d.dailyProfit,d.recordedCapital].every(rational)||d.value&&BigInt(d.value.numerator)<0n||!d.reason&&!d.profit||!d.flowReason&&!d.contributions)throw new Error("invalid_analytics");
    for(const [i,d] of performance.analyticsPoints.entries()){if(!i)continue;const chart=result.points[i-1];if(chart.state==="ready"){if(!d.value||BigInt(d.value.numerator)*BigInt(chart.knownValue!.denominator)!==BigInt(chart.knownValue!.numerator)*BigInt(d.value.denominator))throw new Error("invalid_analytics");}else if(d.value)throw new Error("invalid_analytics");}
    for(const [width,periods] of [[7,performance.monthlyProfit],[4,performance.yearlyProfit]] as const){let cursor=scope.fromDate;for(const p of periods){if(p.fromDate!==cursor||p.toDate<p.fromDate||p.toDate>scope.toDate||p.key!==p.fromDate.slice(0,width)||p.key!==p.toDate.slice(0,width)||!rational(p.profit)||!rational(p.knownProfit)||!p.reason&&!p.profit)throw new Error("invalid_analytics");cursor=shiftHistoryDate(p.toDate,1);}if(cursor!==shiftHistoryDate(scope.toDate,1))throw new Error("invalid_analytics");}
  }
  const seen = new Set<string>();
  for (const line of performance.trackerBenchmarks) {
    const expected = line.key === "imoex" ? ["moex", "RUB", "price_index", "https://www.moex.com/ru/index/IMOEX"] : line.key === "spy" ? ["twelvedata", "USD", "etf_price_proxy", "https://www.ssga.com/us/en/institutional/etfs/state-street-spdr-sp-500-etf-trust-spy"] : undefined;
    if (!expected || seen.has(line.key) || [line.provider, line.nativeCurrency, line.kind, line.sourceUrl].some((s, i) => s !== expected[i]) || line.baseDate < first || line.baseDate > scope.toDate || line.points.length !== count || line.points.some((p, i) => p.date !== shiftHistoryDate(first, i) || ![p.index,p.virtualValue,p.virtualProfit].every(rational) || p.virtualValue&&BigInt(p.virtualValue.numerator)<0n || p.virtualValue&&p.virtualReason!=="" || p.index && (p.reason !== "" || Number(p.index.numerator) / Number(p.index.denominator) <= 0))) throw new Error("invalid_performance");
    seen.add(line.key);
  }
  const withGrowth=async(output:PerformanceHistoryResult)=>{
    if(allowPartial&&!scope.groupIds?.length&&result.strictExcludedKeys.some(key=>!result.excludedKeys.includes(key))){
      const strict=new Set(result.strictExcludedKeys),keys=result.instrumentKeys.filter(key=>!strict.has(key));
      if(keys.length){const growth=await loadPortfolioPerformanceHistory(api,{...scope,instrumentKeys:keys},signal,()=>{},false);output.growthPerformance=growth.performance;
        const check=await api.getTrackerHistory({...request,fromDate:scope.toDate,compactIssues:true},{signal});if(check.basisKey!==result.basisKey)throw new Error("history_changed");}
    }
    return output;
  };
  if(allowPartial && result.excludedKeys.length){
    const excluded=new Set(result.excludedKeys),keys=result.instrumentKeys.filter(key=>!excluded.has(key));
    if(!keys.length)return {points:result.points,basisKey:result.basisKey,performance,excludedKeys:result.excludedKeys,partialUnavailable:true};
    try {
      const partial=await loadPortfolioPerformanceHistory(api,{...scope,instrumentKeys:keys},signal,()=>{},false);
      // The subset has its own signed scope. Recheck the original basis after
      // its calculation so edits during the second pass cannot mix journals.
      const check=await api.getTrackerHistory({...request,fromDate:scope.toDate,compactIssues:true},{signal});
      if(check.basisKey!==result.basisKey)throw new Error("history_changed");
      return withGrowth({points:result.points,basisKey:result.basisKey,performance,partialPerformance:partial.performance,partialPoints:partial.points,partialBasisKey:partial.basisKey,excludedKeys:result.excludedKeys,partialUnavailable:!partial.performance});
    } catch(error) {
      signal.throwIfAborted();
      if(error instanceof Error&&error.message==="history_changed")throw error;
      return {points:result.points,basisKey:result.basisKey,performance,excludedKeys:result.excludedKeys,partialUnavailable:true};
    }
  }
  return withGrowth({points: result.points, basisKey:result.basisKey, performance});
}
async function historyPages(api: HistoryAPI, scope: HistoryScope, signal: AbortSignal, progress: (points: TrackerHistoryPoint[]) => void, performance: boolean): Promise<{points: TrackerHistoryPoint[]; tokens: string[];basisKey:string;instrumentKeys:string[];excludedKeys:string[];strictExcludedKeys:string[]}> {
  const {fxPolicyVersion, fxMode, classificationVersion, ...request} = scope;
  const result: TrackerHistoryPoint[] = [];
  const tokens: string[] = []; let hasSnapshots = true;
  let instrumentKeys:string[]|undefined;const excludedKeys=new Set<string>(),strictExcludedKeys=new Set<string>();
  let cursor = scope.fromDate, basis: string | undefined, version: bigint | undefined;
  for await (const page of chartHistoryPages(scope.fromDate,scope.toDate,signal,(date,pageSignal)=>api.getTrackerHistory({...request,fromDate:date,pageDays:chartPageDays,compactIssues:true,...(performance?{performanceFromDate:scope.fromDate,monetaryOnly:!!scope.groupIds?.length}:{})},{signal:pageSignal}))) {
    signal.throwIfAborted();
    if (page.currency !== scope.currency || page.fxPolicyKey !== scope.fxPolicyKey || page.reportingTimezone !== "Europe/Moscow" || page.priceConvention !== "retrospective_close_next_utc_day_v1" || !["market", "cbr"].includes(page.fxMode) || page.fxPolicyVersion !== fxPolicyVersion || page.fxMode !== fxMode || !/^[a-f0-9]{64}$/.test(page.basisKey) || !page.points.length || page.points.length > chartPageDays) throw new Error("invalid_history");
    if(!historySelectionEqual(page.instrumentKeys,scope.instrumentKeys)||!historySelectionEqual(page.groupIds,scope.groupIds)||page.monetaryOnly!==(performance&&!!scope.groupIds?.length))throw new Error("invalid_history_scope");
    if (scope.groupIds?.length && classificationVersion !== undefined && page.classificationVersion !== classificationVersion) throw new Error("history_changed");
    if (basis !== undefined && (basis !== page.basisKey || version !== page.fxPolicyVersion)) throw new Error("history_changed");
    basis = page.basisKey; version = page.fxPolicyVersion;
    const keys=page.analyticsInstrumentKeys||[],excluded=page.monetaryExcludedKeys||[],strict=page.analyticsExcludedKeys||[];
    if(keys.length>500||new Set(keys).size!==keys.length||keys.some(k=>!k)||excluded.length>500||strict.length>500||[...excluded,...strict].some(k=>!keys.includes(k)))throw new Error("invalid_analytics_scope");
    if(instrumentKeys&&!historySelectionEqual(instrumentKeys,keys))throw new Error("history_changed");
    instrumentKeys=keys;for(const key of excluded)excludedKeys.add(key);for(const key of strict)strictExcludedKeys.add(key);

    if (performance) {if (!page.performanceSnapshot) hasSnapshots = false; else {if (page.performanceSnapshot.length > 131072) throw new Error("invalid_performance"); tokens.push(page.performanceSnapshot);}}
    for (const point of page.points) {
      if (point.date !== cursor || point.date > scope.toDate || !["ready", "incomplete"].includes(point.state) || !point.knownValue || !/^[a-f0-9]{64}$/.test(point.coverageKey) || !/^[0-9]+$/.test(point.knownValue.numerator) || !/^[1-9][0-9]*$/.test(point.knownValue.denominator) || point.issueCount < 0 || point.issueCount > 0 && point.issueCount < point.issues.length || point.positions < 0 || point.pricedPositions < 0 || point.pricedPositions > point.positions || point.state === "ready" && (point.issues.length > 0 || point.issueCount > 0 || point.positions !== point.pricedPositions)) throw new Error("invalid_history_point");
      result.push(point); cursor = shiftHistoryDate(cursor, 1);
    }
    progress([...result]);
    if (!page.nextDate) {if (cursor !== shiftHistoryDate(scope.toDate, 1)) throw new Error("truncated_history"); return {points: result, tokens: hasSnapshots ? tokens : [],basisKey:basis,instrumentKeys:instrumentKeys||[],excludedKeys:[...excludedKeys].sort(),strictExcludedKeys:[...strictExcludedKeys].sort()};}
    if (page.nextDate !== cursor || cursor > scope.toDate) throw new Error("invalid_history_cursor");
  }
  throw new Error("history_limit");
}
// Unknown values never become zero-valued chart points. A subtotal remains
// visibly different from a complete securities valuation.
export function historyPointVisible(p: TrackerHistoryPoint): boolean {
  return !!p.knownValue && (p.pricedPositions > 0 || p.positions === 0 && p.state === "ready");
}

export function historySelectionEqual(actual:string[],expected:string[]=[]){const wanted=new Set(expected);return actual.length===wanted.size&&new Set(actual).size===actual.length&&actual.every(key=>wanted.has(key));}
