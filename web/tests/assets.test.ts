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
