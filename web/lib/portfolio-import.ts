import {Code, ConnectError, type Client} from "@connectrpc/connect";
import {InvestmentService, type InvestmentLedger, type InvestmentImportBatch, type InvestmentOperation} from "@/proto/budget/v1/investment_pb";
import {InvestmentImportService, type InvestmentImport, type InvestmentImportRow} from "@/proto/budget/v1/investment_import_pb";
import {add, trackerError, type TrackerQuote} from "./portfolio-tracker";
import type {Fraction} from "./investment-positions";

type ImportAPI = Client<typeof InvestmentImportService>;
type InvestmentAPI = Client<typeof InvestmentService>;
type Money = {units?: string | number; nano?: number; currency?: string};
type BrokerPosition = {instrumentUid?: string; instrumentType?: string; classCode?: string; ticker?: string; currentPrice?: Money; currentNkd?: Money};
type BrokerOperation = {instrumentUid?: string; classCode?: string; ticker?: string; name?: string; instrumentType?: string};
type BrokerSnapshot = {fetched_at?: string; operations?: BrokerOperation[]; portfolio?: {positions?: BrokerPosition[]}};
export type BrokerCatalog = {quotes: Map<string, TrackerQuote>; symbols: Map<string, string>; names: Map<string, {ticker: string; name: string; instrumentType?: string; classCode?: string}>};

function money(value?: Money): Fraction | undefined {
  if (!value || !/^-?\d{1,19}$/.test(String(value.units ?? 0)) || !Number.isInteger(value.nano ?? 0) || Math.abs(value.nano ?? 0) > 999999999) return;
  return {numerator: (BigInt(value.units ?? 0) * 1000000000n + BigInt(value.nano ?? 0)).toString(), denominator: "1000000000"};
}
// GetPortfolio.currentPrice is MoneyValue per security, unlike last-price/candle
// quotations in bond points. Never apply the candle nominal conversion here.
export function brokerCatalog(snapshots: BrokerSnapshot[], now = Date.now()): BrokerCatalog {
  const quotes = new Map<string, TrackerQuote>(), names = new Map<string, {ticker: string; name: string; instrumentType?: string; classCode?: string}>();
  const candidates = new Map<string, Set<string>>();
  const identify = (ticker?: string, uid?: string, name?: string, instrumentType?: string, classCode?: string) => {
    if (!ticker || !uid) return;
    const key = `tinvest:${uid}`;
    const set = candidates.get(ticker) || new Set<string>(); set.add(key); candidates.set(ticker, set);
    if (!names.has(key)) names.set(key, {ticker, name: name || ticker, instrumentType, classCode});
    else names.set(key, {...names.get(key)!, ...(instrumentType ? {instrumentType} : {}), ...(classCode ? {classCode} : {})});
  };
  for (const snapshot of snapshots) {
    const at = Date.parse(snapshot.fetched_at || "");
    for (const op of snapshot.operations || []) identify(op.ticker, op.instrumentUid, op.name, op.instrumentType, op.classCode);
    for (const p of snapshot.portfolio?.positions || []) {
      identify(p.ticker, p.instrumentUid, undefined, p.instrumentType, p.classCode);
      if (!p.instrumentUid || !["share", "bond", "etf"].includes(p.instrumentType || "")) continue;
      const currency = p.currentPrice?.currency?.toUpperCase(), price = money(p.currentPrice);
      if (!currency || !price || BigInt(price.numerator) <= 0n || !Number.isFinite(at) || at > now || now - at > 7 * 86400000) continue;
      if (p.instrumentType === "bond" && p.currentNkd?.currency?.toUpperCase() !== currency) continue;
      const dirty = p.instrumentType === "bond" ? add(price, money(p.currentNkd)) : price;
      const key = `tinvest:${p.instrumentUid}\0${currency}`;
      if ((quotes.get(key)?.at || 0) < at) quotes.set(key, {price: dirty, at, ticker: p.ticker, name: names.get(`tinvest:${p.instrumentUid}`)?.name});
    }
  }
  const symbols = new Map<string, string>();
  for (const [ticker, keys] of candidates) if (keys.size === 1) symbols.set(ticker, [...keys][0]);
  return {quotes, symbols, names};
}

export async function loadBrokerCatalog(api: ImportAPI): Promise<BrokerCatalog> {
  const latest = new Map<string, {id: string; at: bigint}>();
  for (let offset = 0; ; offset += 200) {
    const page = await api.listBrokerSnapshots({limit: 200, offset});
    for (const item of page.snapshots) if (!latest.has(item.accountId) || (latest.get(item.accountId)?.at || 0n) < (item.createdAt?.seconds || 0n)) latest.set(item.accountId, {id: item.id, at: item.createdAt?.seconds || 0n});
    if (page.snapshots.length < 200) break;
  }
  const snapshots: BrokerSnapshot[] = [];
  // Keep request concurrency bounded even for accounts with large source files.
  for (const item of latest.values()) {
    const response = await api.getBrokerSnapshot({id: item.id});
    snapshots.push(JSON.parse(new TextDecoder().decode(response.originalJson)) as BrokerSnapshot);
  }
  return brokerCatalog(snapshots);
}

export async function importRows(api: ImportAPI, source: InvestmentImport): Promise<InvestmentImportRow[]> {
  const rows: InvestmentImportRow[] = [];
  while (rows.length < source.rowCount) {
    const page = await api.listInvestmentImportRows({importId: source.id, limit: 200, offset: rows.length});
    if (!page.rows.length) throw new Error("incomplete_import");
    for (const row of page.rows) { if (row.record !== rows.length + 1) throw new Error("incomplete_import"); rows.push(row); }
  }
  return rows;
}

export type ImportProblem = {record: number; symbol: string; message: string};
export type SimpleImportPlan = {
  source: InvestmentImport; batch?: InvestmentImportBatch; ready: number; alreadyImported: number;
  ignored: number; problems: ImportProblem[]; bindings: Record<string, string>;
};
export async function prepareSimpleImport(api: ImportAPI, source: InvestmentImport, ledgers: InvestmentLedger[], bindings: Record<string, string>, catalog: BrokerCatalog, ru: boolean, preserveIdenticalRows = true): Promise<SimpleImportPlan> {
  const rows = await importRows(api, source), ledgerByRecord: Record<string, string> = {}, rules = new Map<string, object>();
  let alreadyImported = 0, ignored = 0;
  const problems: ImportProblem[] = [];
  const supported = new Set(["BUY", "SELL", "DIVIDEND", "SPLIT", "CASH_GAIN"]);
  for (const row of rows) {
    if (row.operationId) { alreadyImported++; continue; }
    const f = row.sourceFields;
    if (["CASH_IN", "CASH_OUT", "TAX", "FEE"].includes(f.Event)) { ignored++; continue; }
    if (!supported.has(f.Event)) { problems.push({record: row.record, symbol: f.Symbol || "", message: f.Event === "REPAYMENT" ? ru ? "Погашение облигации: уточните, списывается ли количество бумаг." : "Bond repayment: clarify whether security units were redeemed." : ru ? "Этот тип операции требует уточнения." : "This operation type needs clarification."}); continue; }
    const ledger = row.ledgerId || bindings[row.accountId];
    if (!ledgers.some(item => item.id === ledger && !item.archived)) { problems.push({record: row.record, symbol: f.Symbol || "", message: ru ? "Выберите счёт." : "Select an account."}); continue; }
    ledgerByRecord[row.record] = ledger;
    const brokerKey = catalog.symbols.get(f.Symbol);
    // A Moscow ticker can coincide with a different NYSE/NASDAQ security.
    // Broker identity is not portable across those exchanges based on ticker alone.
    const moscow = /^TQ/.test(catalog.names.get(brokerKey || "")?.classCode || "");
    const instrumentKey = brokerKey && (!moscow || f.Exchange === "MCX") ? brokerKey : `${f.Symbol}@${f.Exchange || "UNKNOWN"}`;
    const income = f.Event === "DIVIDEND" || f.Event === "CASH_GAIN";
    const key = JSON.stringify([f.Event, f.Symbol, f.Exchange, f.Currency]);
    rules.set(key, {
      Event: f.Event, Symbol: f.Symbol, Exchange: f.Exchange, Currency: f.Currency, InstrumentKey: instrumentKey,
      Kind: f.Event === "CASH_GAIN" ? "repo_income" : income ? catalog.names.get(instrumentKey)?.instrumentType === "bond" ? "coupon" : "dividend" : f.Event.toLowerCase(),
      AmountBasis: income ? "quantity" : "quantity_price", QuantityFactor: "1", AmountFactor: "1",
      MoneyRounding: "12dp_half_away", AccruedBasis: "total", FeePolicy: "same_currency", IncomeBasis: "received",
      Reason: "Snowball import: security units, per-security price, total accrued interest; payouts as supplied; monetary totals rounded half away from zero to 12 decimal places.",
    });
  }
  const duplicateReview = preserveIdenticalRows ? Object.fromEntries(rows.filter(row => ledgerByRecord[row.record]).map(row => [row.record, "Preserve each CSV row as supplied; identical values alone do not establish duplicate transactions. This is a source import policy, not independent execution verification."])) : {};
  const mapping = {duplicate_review: duplicateReview, source_sha256: source.sourceSha256, timezone: "Europe/Moscow", settlement_policy: "assume_trade_time", ledger_by_record: ledgerByRecord, rules: [...rules.values()]};
  let batch: InvestmentImportBatch | undefined;
  const reported = new Set(problems.map(p => p.record));
  // The API pages candidates at 5000 rows; never silently truncate larger CSVs.
  for (let offset = 0; offset < source.rowCount; offset += 5000) {
    const response = await api.previewInvestmentFinancialMapping({importId: source.id, expectedImportVersion: source.version, mappingJson: new TextEncoder().encode(JSON.stringify(mapping)), requestKey: `tracker-${source.id}`, offset, limit: 5000});
    if (response.batch) {
      if (!batch) batch = {...response.batch, entries: [], expectedLedgerVersions: {...response.batch.expectedLedgerVersions}};
      Object.assign(batch.expectedLedgerVersions, response.batch.expectedLedgerVersions);
      batch.entries.push(...response.batch.entries);
    }
    for (const candidate of response.rows) {
      const row = rows[candidate.record - 1];
      if (!row || row.operationId || !supported.has(row.sourceFields.Event) || reported.has(row.record) || candidate.operation) continue;
      reported.add(row.record);
      problems.push({record: row.record, symbol: row.sourceFields.Symbol || "", message: candidate.issues.includes("identical_rows_require_review") ? ru ? "Повторяющаяся строка: проверьте, не дубликат ли это." : "Repeated row: check whether this is a duplicate." : ru ? "Проверьте количество, цену, валюту и комиссию этой строки." : "Check this row's quantity, price, currency and fee."});
    }
  }
  return {source, batch, ready: batch?.entries.length || 0, alreadyImported, ignored, problems, bindings};
}

export type ImportResult = {imported: number; problems: ImportProblem[]; stopped?: string};
export async function commitSimpleImport(api: InvestmentAPI, plan: SimpleImportPlan, ru: boolean, onProgress: (result: ImportResult) => void): Promise<ImportResult> {
  const result: ImportResult = {imported: 0, problems: [...plan.problems]};
  if (!plan.batch) return result;
  const groups = new Map<string, typeof plan.batch.entries>();
  for (const entry of plan.batch.entries) {
    if (!entry.operation) continue;
    const key = JSON.stringify([entry.operation.ledgerId, entry.operation.instrumentKey]);
    const entries = groups.get(key) || []; entries.push(entry); groups.set(key, entries);
  }
  const versions = {...plan.batch.expectedLedgerVersions};
  for (const entries of groups.values()) {
    const operation = entries[0].operation!;
    if (entries.length > 5000) { result.problems.push(...entries.map(e => ({record: e.record, symbol: operation.instrumentKey, message: ru ? "Слишком много операций одной бумаги в файле." : "Too many operations for one security in this file."}))); continue; }
    const batch: InvestmentImportBatch = {...plan.batch, entries, expectedLedgerVersions: {[operation.ledgerId]: versions[operation.ledgerId]}, requestKey: crypto.randomUUID()};
    try {
      const preview = await api.previewInvestmentImportBatch({batch});
      const committed = await api.commitInvestmentImportBatch({batch, expectedHash: preview.result?.hash});
      if (!committed.result?.committed) throw new Error("commit_not_confirmed");
      result.imported += committed.result.operations.length;
      for (const snapshot of committed.result.snapshots) if (snapshot.ledger) versions[snapshot.ledger.id] = snapshot.ledger.version;
    } catch (error) {
      const code = ConnectError.from(error).code;
      if (code !== Code.FailedPrecondition && code !== Code.InvalidArgument) {
        result.stopped = trackerError(error, ru); onProgress({...result}); return result;
      }
      result.problems.push(...entries.map(entry => ({record: entry.record, symbol: operation.instrumentKey, message: trackerError(error, ru)})));
    }
    onProgress({...result});
  }
  return result;
}

export async function allOperations(api: InvestmentAPI, ledgerId: string): Promise<InvestmentOperation[]> {
  const operations: InvestmentOperation[] = [];
  for (let offset = 0; ; offset += 200) {
    const response = await api.listInvestmentOperations({ledgerId, limit: 200, offset});
    operations.push(...response.operations);
    if (response.operations.length < 200) return operations;
  }
}
