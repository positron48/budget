import { describe, expect, it, vi } from "vitest";
import { createQueryClient, hashQueryKey } from "@/lib/query-client";

describe("protobuf query keys", () => {
  it("keeps int64 precision, types and stable object ordering", () => {
    expect(hashQueryKey([9007199254740993n])).not.toBe(hashQueryKey([9007199254740992n]));
    expect(hashQueryKey([1n])).not.toBe(hashQueryKey(["1"]));
    expect(hashQueryKey([1n])).not.toBe(hashQueryKey([1]));
    expect(hashQueryKey([{ b: 2n, a: 1 }])).toBe(hashQueryKey([{ a: 1, b: 2n }]));
  });
  it("reuses the cache and supports prefix invalidation with protobuf timestamps", async () => {
    const client = createQueryClient();
    const queryKey = ["fx", { asOf: { seconds: 1791417600n } }];
    const queryFn = vi.fn().mockResolvedValue({ rate: "80.25" });
    await client.fetchQuery({ queryKey, queryFn, staleTime: Infinity });
    await client.fetchQuery({ queryKey: ["fx", { asOf: { seconds: 1791417600n } }], queryFn, staleTime: Infinity });
    expect(queryFn).toHaveBeenCalledTimes(1);
    await client.invalidateQueries({ queryKey: ["fx"] });
    await client.fetchQuery({ queryKey, queryFn, staleTime: Infinity });
    expect(queryFn).toHaveBeenCalledTimes(2);
    client.clear();
  });
});
