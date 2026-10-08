import { hashKey, QueryClient } from "@tanstack/react-query";

// Protobuf int64 fields (timestamps and money) are bigint. Keep their full
// precision and distinguish them from strings when hashing query keys.
export function hashQueryKey(queryKey: readonly unknown[]): string {
  return hashKey(JSON.parse(JSON.stringify(queryKey, (_key, value) =>
    typeof value === "bigint" ? { $bigint: value.toString() } : value,
  )));
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { queryKeyHashFn: hashQueryKey } },
  });
}
