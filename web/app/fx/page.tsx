"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

function FxInner() {
  const { fx } = useClients();
  const qc = useQueryClient();
  const today = new Date().toISOString().slice(0, 10);
  const [fromCodes, setFromCodes] = useState("USD,EUR");
  const [toCode, setToCode] = useState("RUB");
  const [asOf, setAsOf] = useState(today);
  const req = useMemo(() => {
    const codes = fromCodes
      .split(",")
      .map((s) => s.trim().toUpperCase())
      .filter(Boolean);
    return {
      fromCurrencyCodes: codes,
      toCurrencyCode: toCode.toUpperCase(),
      asOf: { seconds: Math.floor(new Date(asOf).getTime() / 1000) },
    } as any;
  }, [fromCodes, toCode, asOf]);
  const { data, isLoading, error } = useQuery({
    queryKey: ["fx", req],
    queryFn: async () => (await fx.batchGetRates(req)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const [uFrom, setUFrom] = useState("USD");
  const [uTo, setUTo] = useState("RUB");
  const [uRate, setURate] = useState("0");
  const [uDate, setUDate] = useState(today);
  const upsertMut = useMutation({
    mutationFn: async () =>
      await fx.upsertRate({
        rate: {
          fromCurrencyCode: uFrom.toUpperCase(),
          toCurrencyCode: uTo.toUpperCase(),
          rateDecimal: uRate,
          asOf: { seconds: Math.floor(new Date(uDate).getTime() / 1000) },
          provider: "manual",
        },
      } as any),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["fx"] }),
  });

  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold mb-2">FX Rates</h1>
      <div className="flex gap-3 items-end mb-4 flex-wrap">
        <div>
          <label className="block text-xs">From Currencies (comma)</label>
          <input className="border rounded px-2 py-1 w-64" value={fromCodes} onChange={(e) => setFromCodes(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">To</label>
          <input className="border rounded px-2 py-1 w-24" value={toCode} onChange={(e) => setToCode(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">As Of</label>
          <input className="border rounded px-2 py-1" type="date" value={asOf} onChange={(e) => setAsOf(e.target.value)} />
        </div>
      </div>
      {isLoading && <div className="text-sm text-gray-500">Loading...</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}
      <table className="text-sm w-full border-collapse">
        <thead>
          <tr className="border-b">
            <th className="text-left py-1">Pair</th>
            <th className="text-right py-1">Rate</th>
            <th className="text-left py-1">Provider</th>
            <th className="text-left py-1">As Of</th>
          </tr>
        </thead>
        <tbody>
          {(data?.rates ?? []).map((r: any, i: number) => (
            <tr key={i} className="border-b">
              <td className="py-1">{r?.fromCurrencyCode}/{r?.toCurrencyCode}</td>
              <td className="py-1 text-right">{r?.rateDecimal}</td>
              <td className="py-1">{r?.provider}</td>
              <td className="py-1">{r?.asOf?.seconds ? new Date(r.asOf.seconds * 1000).toISOString().slice(0, 10) : ""}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <h2 className="text-xl font-semibold mt-6 mb-2">Upsert Rate</h2>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          upsertMut.mutate();
        }}
        className="flex gap-3 items-end flex-wrap"
      >
        <div>
          <label className="block text-xs">From</label>
          <input className="border rounded px-2 py-1 w-24" value={uFrom} onChange={(e) => setUFrom(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">To</label>
          <input className="border rounded px-2 py-1 w-24" value={uTo} onChange={(e) => setUTo(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">Rate (decimal)</label>
          <input className="border rounded px-2 py-1 w-32" value={uRate} onChange={(e) => setURate(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">As Of</label>
          <input className="border rounded px-2 py-1" type="date" value={uDate} onChange={(e) => setUDate(e.target.value)} />
        </div>
        <button className="bg-black text-white rounded px-3 py-1" disabled={upsertMut.isPending}>
          {upsertMut.isPending ? "Saving..." : "Save"}
        </button>
      </form>
    </div>
  );
}

export default function FxPage() {
  return (
    <ClientsProvider>
      <FxInner />
    </ClientsProvider>
  );
}


