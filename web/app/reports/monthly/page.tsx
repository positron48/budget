"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useQuery } from "@tanstack/react-query";

function MonthlyReportInner() {
  const { report } = useClients();
  const now = new Date();
  const month = now.toISOString().slice(0, 7) + "-01";
  const { data, isLoading, error } = useQuery({
    queryKey: ["monthly", month],
    queryFn: async () => (await report.getMonthlySummary({ month: { from: { seconds: Math.floor(new Date(month).getTime() / 1000) } } } as any)) as any,
  });
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold mb-2">Monthly Report</h1>
      {isLoading && <div className="text-sm text-gray-500">Loading...</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}
      <pre className="text-xs bg-gray-100 p-2 rounded">{JSON.stringify(data ?? {}, null, 2)}</pre>
    </div>
  );
}

export default function MonthlyReportPage() {
  return (
    <ClientsProvider>
      <MonthlyReportInner />
    </ClientsProvider>
  );
}

