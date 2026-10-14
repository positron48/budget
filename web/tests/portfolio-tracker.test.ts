import {describe, expect, it, vi} from "vitest";
import {create} from "@bufbuild/protobuf";
import {InvestmentOperationSchema, InvestmentSnapshotSchema, InvestmentImportBatchSchema} from "@/proto/budget/v1/investment_pb";
import {add, decimal, trackerPositions, tradeAmount, trackerError} from "@/lib/portfolio-tracker";
import {positionDecimal} from "@/lib/investment-positions";
import {brokerCatalog, prepareSimpleImport, commitSimpleImport, type SimpleImportPlan} from "@/lib/portfolio-import";
import {InvestmentImportSchema, InvestmentImportRowSchema} from "@/proto/budget/v1/investment_import_pb";
import {InvestmentLedgerSchema} from "@/proto/budget/v1/investment_pb";
import {Code, ConnectError} from "@connectrpc/connect";

describe("portfolio tracker", () => {
  it("combines holdings and payouts without broker cash balances or withholding", () => {
    const snapshot = create(InvestmentSnapshotSchema, {ledger: {id: "account", name: "Broker"}, cash: [{currency: "RUB", economic: "999999"}], positions: [{instrumentKey: "ABC", currency: "RUB", quantity: "2", cleanCost: "200", acquiredAccrued: "4", realizedClean: "10", realizedAccrued: "1"}]});
    const payout = create(InvestmentOperationSchema, {id: "payout", instrumentKey: "ABC", currency: "RUB", kind: "coupon", amount: "15", tax: "3", taxReviewRequired: true, status: "posted"});
    const [position] = trackerPositions([snapshot], [payout], new Map([["ABC\0RUB", {price: decimal("120")!, at: 1}]]));
    expect(positionDecimal(position.cost)).toBe("204");
    expect(positionDecimal(position.value)).toBe("240");
    expect(positionDecimal(position.income)).toBe("15");
    expect(positionDecimal(position.profit)).toBe("62");
  });
  it("keeps unavailable prices and unknown cost distinct from zero", () => {
    const snapshot = create(InvestmentSnapshotSchema, {positions: [{instrumentKey: "ABC", currency: "USD", quantity: "1", cleanCost: "", acquiredAccrued: "0", realizedClean: "0", realizedAccrued: "0"}]});
    const [position] = trackerPositions([snapshot], []);
    expect(position.cost).toBeUndefined(); expect(position.value).toBeUndefined(); expect(position.profit).toBeUndefined();
  });
  it("does not count purchased accrued interest as profit after a legacy coupon recovery", () => {
    const snapshot = create(InvestmentSnapshotSchema, {positions: [{instrumentKey: "BOND", currency: "RUB", quantity: "1", cleanCost: "100", acquiredAccrued: "0", realizedClean: "0", realizedAccrued: "0", couponRecovered: "10"}]});
    const coupon = create(InvestmentOperationSchema, {instrumentKey: "BOND", currency: "RUB", status: "posted", kind: "coupon", amount: "15"});
    const [position] = trackerPositions([snapshot], [coupon], new Map([["BOND\0RUB", {price: decimal("100")!, at: 1}]]));
    expect(positionDecimal(position.income)).toBe("15"); expect(positionDecimal(position.profit)).toBe("5");
  });
  it("does not mix currencies or drop realized results of closed positions", () => {
    const snapshot = create(InvestmentSnapshotSchema, {positions: [
      {instrumentKey: "ABC", currency: "USD", quantity: "0", cleanCost: "0", acquiredAccrued: "0", realizedClean: "12", realizedAccrued: "0"},
      {instrumentKey: "ABC", currency: "RUB", quantity: "1", cleanCost: "20", acquiredAccrued: "0", realizedClean: "0", realizedAccrued: "0"},
    ]});
    const positions = trackerPositions([snapshot], []);
    expect(positions).toHaveLength(2); expect(positionDecimal(positions[0].profit)).toBe("12"); expect(positions[1].profit).toBeUndefined();
  });
  it("multiplies trade decimals exactly and rejects precision loss", () => {
    expect(tradeAmount("3", "0,1")).toBe("0.3");
    expect(() => tradeAmount("0.0000000000001", "1")).toThrow("trade_precision");
    expect(() => tradeAmount("0.0000000000000000000000000001", "0.0000000000000000000000000001")).toThrow("trade_precision");
    expect(positionDecimal(add(decimal("0.1"), decimal("0.2")))).toBe("0.3");
  });
  it("uses clean per-security portfolio money without adding NKD or bond point scaling", () => {
    const data = brokerCatalog([{fetched_at: "2026-10-13T00:00:00Z", portfolio: {positions: [{instrumentUid: "bond", instrumentType: "bond", ticker: "BOND", currentPrice: {currency: "rub", units: "980", nano: 500000000}, currentNkd: {currency: "rub", units: "12", nano: 250000000}}]}}], Date.parse("2026-10-13T01:00:00Z"));
    expect(positionDecimal(data.quotes.get("tinvest:bond\0RUB")?.price)).toBe("980.5"); expect(data.symbols.get("BOND")).toBe("tinvest:bond");
  });
  it("rejects ambiguous tickers, stale snapshots and unknown bond NKD currency", () => {
    const data = brokerCatalog([{fetched_at: "2020-01-01T00:00:00Z", operations: [{ticker: "ABC", instrumentUid: "one"}, {ticker: "ABC", instrumentUid: "two"}], portfolio: {positions: [{ticker: "ABC", instrumentUid: "one", instrumentType: "share", currentPrice: {currency: "rub", units: "10"}}]}}], Date.parse("2026-10-13T00:00:00Z"));
    expect(data.symbols.has("ABC")).toBe(false); expect(data.quotes.size).toBe(0);
  });
  it("isolates a missing purchase to one security and imports other securities", async () => {
    const batch = create(InvestmentImportBatchSchema, {importId: "source", expectedImportVersion: 1n, expectedLedgerVersions: {account: 1n}, entries: [
      {record: 1, operation: {ledgerId: "account", instrumentKey: "BAD", currency: "RUB"}},
      {record: 2, operation: {ledgerId: "account", instrumentKey: "GOOD", currency: "RUB"}},
    ]});
    const preview = vi.fn(async (request: {batch?: typeof batch}) => {if (request.batch?.entries[0].record === 1) throw new ConnectError("short sale is not supported", Code.FailedPrecondition); return {result: {hash: "reviewed"}};});
    const commit = vi.fn(async () => ({result: {committed: true, operations: [{}], snapshots: [{ledger: {id: "account", version: 2n}}]}}));
    const result = await commitSimpleImport({previewInvestmentImportBatch: preview, commitInvestmentImportBatch: commit} as never, {batch, problems: []} as unknown as SimpleImportPlan, true, () => {});
    expect(result.imported).toBe(1); expect(result.problems).toHaveLength(1); expect(result.problems[0].record).toBe(1); expect(commit).toHaveBeenCalledOnce();
  });
  it("stops after an uncertain transport response instead of submitting more trades", async () => {
    const batch = create(InvestmentImportBatchSchema, {expectedLedgerVersions: {account: 1n}, entries: [{record: 1, operation: {ledgerId: "account", instrumentKey: "A"}}, {record: 2, operation: {ledgerId: "account", instrumentKey: "B"}}]});
    const preview = vi.fn(async () => ({result: {hash: "hash"}}));
    const commit = vi.fn(async () => {throw new ConnectError("timeout", Code.Unavailable);});
    const result = await commitSimpleImport({previewInvestmentImportBatch: preview, commitInvestmentImportBatch: commit} as never, {batch, problems: []} as unknown as SimpleImportPlan, true, () => {});
    expect(result.stopped).toBeTruthy(); expect(preview).toHaveBeenCalledOnce(); expect(commit).toHaveBeenCalledOnce();
  });
});

it("imports a split together with the purchases it changes despite its empty currency", async () => {
  const batch = create(InvestmentImportBatchSchema, {expectedLedgerVersions: {account: 1n}, entries: [
    {record: 1, operation: {ledgerId: "account", instrumentKey: "A", currency: "USD", kind: "buy"}},
    {record: 2, operation: {ledgerId: "account", instrumentKey: "A", currency: "", kind: "split", splitNew: "4", splitOld: "1"}},
    {record: 3, operation: {ledgerId: "account", instrumentKey: "A", currency: "USD", kind: "sell"}},
  ]});
  const preview = vi.fn(async (request: {batch?: typeof batch}) => {
    expect(request.batch?.entries.map(e => e.record)).toEqual([1, 2, 3]);
    return {result: {hash: "hash"}};
  });
  const commit = vi.fn(async () => ({result: {committed: true, operations: [{}, {}, {}], snapshots: [{ledger: {id: "account", version: 2n}}]}}));
  const result = await commitSimpleImport({previewInvestmentImportBatch: preview, commitInvestmentImportBatch: commit} as never, {batch, problems: []} as unknown as SimpleImportPlan, true, () => {});
  expect(result.imported).toBe(3); expect(preview).toHaveBeenCalledOnce();
});


it("keeps coincident Moscow and NYSE tickers separate during import", async () => {
  const catalog = brokerCatalog([{operations: [{ticker: "T", instrumentUid: "russian", classCode: "TQBR"}]}]);
  const preview = vi.fn(async (request: {mappingJson: Uint8Array}) => {
    const mapping = JSON.parse(new TextDecoder().decode(request.mappingJson));
    expect(mapping.rules.map((rule: {InstrumentKey: string}) => rule.InstrumentKey)).toEqual(["tinvest:russian", "T@NYSE"]);
    expect(Object.keys(mapping.duplicate_review)).toEqual(["1", "2"]);
    return {rows: []};
  });
  const api = {
    listInvestmentImportRows: async () => ({rows: ["MCX", "NYSE"].map((exchange, i) => create(InvestmentImportRowSchema, {record: i + 1, accountId: "source-account", sourceFields: {Event: "BUY", Symbol: "T", Exchange: exchange, Currency: exchange === "MCX" ? "RUB" : "USD"}}))}),
    previewInvestmentFinancialMapping: preview,
  };
  await prepareSimpleImport(api as never, create(InvestmentImportSchema, {id: "source", rowCount: 2}), [create(InvestmentLedgerSchema, {id: "account"})], {"source-account": "account"}, catalog, true);
  expect(preview).toHaveBeenCalledOnce();
});


it("applies a repeated corporate split once per combined account, including on retry", async () => {
  for (const posted of [false, true]) {
    const rows = [1, 2, 3].map(record => create(InvestmentImportRowSchema, {record, accountId: record === 3 ? "other" : "source-account", operationId: posted && record === 2 ? "posted" : "", ledgerId: posted && record === 2 ? "account" : "", sourceFields: {Event: "SPLIT", Symbol: "ABC", Exchange: "MCX", Currency: "RUB", Date: "2024-01-01 00:00:00", Price: "100"}}));
    const preview = vi.fn(async (request: {mappingJson: Uint8Array}) => {
      const mapping = JSON.parse(new TextDecoder().decode(request.mappingJson));
      expect(mapping.ledger_by_record).toEqual(posted ? {3: "second"} : {1: "account", 3: "second"});
      return {rows: []};
    });
    const result = await prepareSimpleImport({listInvestmentImportRows: async () => ({rows}), previewInvestmentFinancialMapping: preview} as never, create(InvestmentImportSchema, {id: "source", rowCount: 3}), ["account", "second"].map(id => create(InvestmentLedgerSchema, {id})), {"source-account": "account", other: "second"}, brokerCatalog([]), true, false);
    expect(result.ignored).toBe(1); expect(result.alreadyImported).toBe(posted ? 1 : 0); expect(result.problems).toEqual([]);
  }
});


it("explains a change in settlement currency without suggesting a stale-page retry", () => {
  const error = new Error("investment history is inconsistent: instrument cost currency changed; explicit conversion required");
  expect(trackerError(error, true)).toContain("разных валютах");
  expect(trackerError(error, true)).not.toContain("Обновите страницу");
  expect(trackerError(error, false)).toContain("historical exchange rates");
});
