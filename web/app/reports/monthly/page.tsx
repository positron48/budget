"use client";

import { ClientsProvider } from "@/app/providers";

function MonthlyReportInner() {
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold">Monthly Report</h1>
      <p className="text-sm text-gray-500">Choose month/year and target currency.</p>
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

