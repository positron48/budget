import { describe, it, expect } from "vitest";
import { assetAmountInput, assetMoney, parseAssetAmount } from "@/lib/assets";
describe("exact savings amounts", () => {
  it("preserves int64 boundaries and local decimal input", () => {
    expect(parseAssetAmount("92 233 720 368 547 758,07")).toBe(
      9223372036854775807n,
    );
    expect(parseAssetAmount("-92233720368547758.08")).toBe(
      -9223372036854775808n,
    );
    expect(assetAmountInput(9223372036854775807n)).toBe("92233720368547758.07");
    expect(assetMoney(-1n, "USD", "en-US")).toBe("-$0.01");
    expect(assetMoney(9007199254740993n, "USD", "en-US")).toBe(
      "$90,071,992,547,409.93",
    );
  });
  it("rounds summary amounts to whole units without losing int64 precision", () => {
    expect(assetMoney(104326982n, "RUB", "en-US", 0)).toBe("RUB 1,043,270");
    expect(assetMoney(149n, "USD", "en-US", 0)).toBe("$1");
    expect(assetMoney(150n, "USD", "en-US", 0)).toBe("$2");
    expect(assetMoney(-150n, "USD", "en-US", 0)).toBe("-$2");
    expect(assetMoney(-1n, "USD", "en-US", 0)).toBe("$0");
    expect(assetMoney(9223372036854775807n, "USD", "en-US", 0)).toBe("$92,233,720,368,547,758");
  });
  it("rejects overflow, extra precision and nonpositive movements", () => {
    for (const value of [
      "92233720368547758.08",
      "-92233720368547758.09",
      "1.001",
      "NaN",
      "",
    ])
      expect(() => parseAssetAmount(value)).toThrow("amount");
    expect(() => parseAssetAmount("0", true)).toThrow("amount");
    expect(() => parseAssetAmount("-1", true)).toThrow("amount");
  });
});

describe("savings conversion", () => {
  it("rounds positive and negative halves away from zero without float precision loss", async () => {
    const { convertAssetMinor } = await import("@/lib/assets");
    expect(convertAssetMinor(1n, "0.5")).toBe(1n);
    expect(convertAssetMinor(-1n, "0.5")).toBe(-1n);
    expect(convertAssetMinor(9007199254740993n, "1.25")).toBe(11258999068426241n);
    expect(() => convertAssetMinor(1n, "0")).toThrow();
  });
  it("does not mark zero foreign balances as missing and rounds before adding", async () => {
    const { valueAssetAccounts } = await import("@/lib/assets");
    const account = (minorUnits: bigint, currencyCode = "USD") => ({ balances: [{ amount: { minorUnits, currencyCode } }] }) as any;
    expect(valueAssetAccounts([account(1n), account(1n), account(0n, "XYZ")], new Map([["USD", "0.5"]]), "EUR")).toEqual({ total: 2n, incomplete: false });
    expect(valueAssetAccounts([account(10n), account(1n, "XYZ")], new Map([["USD", "2"]]), "EUR")).toEqual({ total: 20n, incomplete: true });
  });
});
