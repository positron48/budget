"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useQuery } from "@tanstack/react-query";

function TransactionsInner() {
  const { transaction } = useClients();
  const { data, isLoading, error } = useQuery({
    queryKey: ["transactions"],
    queryFn: async () => (await transaction.listTransactions({ page: { page: 1, pageSize: 20 } } as any)) as any,
  });
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold mb-2">Transactions</h1>
      {isLoading && <div className="text-sm text-gray-500">Loading...</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}
      <ul className="space-y-1">
        {(data?.items ?? []).map((t: any, i: number) => (
          <li key={i} className="text-sm">
            {(t as any).occurredAt?.seconds ? new Date((t as any).occurredAt.seconds * 1000).toISOString().slice(0, 10) : ""}
            {" — "}
            {(t as any).amount?.minorUnits ?? 0}
            {" "}
            {(t as any).amount?.currencyCode ?? ""}
          </li>
        ))}
      </ul>
    </div>
  );
}

export default function TransactionsPage() {
  return (
    <ClientsProvider>
      <TransactionsInner />
    </ClientsProvider>
  );
}

