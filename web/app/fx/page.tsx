"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import LoadingSpinner from "@/components/LoadingSpinner";

function FxInner() {
  const { fx } = useClients();
  const qc = useQueryClient();
  const t = useTranslations("fx");
  const tc = useTranslations("common");
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
    <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8">
      {/* Header */}
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-foreground">{t("title")}</h1>
        <p className="text-muted-foreground mt-1">{t("description")}</p>
      </div>

      {/* Filters */}
      <div className="card mb-6">
        <div className="card-header">
          <h3 className="card-title">{t("title")}</h3>
        </div>
        <div className="card-content">
          <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-end">
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">{t("fromCurrencies")}</label>
              <input 
                className="input w-64" 
                value={fromCodes} 
                onChange={(e) => setFromCodes(e.target.value)}
                placeholder="USD,EUR"
                autoComplete="off"
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">{t("to")}</label>
              <input 
                className="input w-24" 
                value={toCode} 
                onChange={(e) => setToCode(e.target.value)}
                placeholder="RUB"
                autoComplete="off"
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">{t("asOf")}</label>
              <input 
                className="input" 
                type="date" 
                value={asOf} 
                onChange={(e) => setAsOf(e.target.value)}
                autoComplete="off"
              />
            </div>
          </div>
        </div>
      </div>

      {/* Content */}
      {isLoading && (
        <LoadingSpinner text={tc("loading")} className="py-12" />
      )}

      {error && (
        <div className="card border-destructive/20 bg-destructive/5">
          <div className="card-content">
            <p className="text-destructive">{(error as any).message}</p>
          </div>
        </div>
      )}

      {!isLoading && !error && data && (
        <div className="space-y-6">
          {/* Rates Table */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">{t("title")}</h3>
            </div>
            <div className="card-content">
              <table className="w-full">
                <thead>
                  <tr className="border-b">
                    <th className="text-left py-2 font-medium">{t("pair")}</th>
                    <th className="text-right py-2 font-medium">{t("rate")}</th>
                    <th className="text-left py-2 font-medium">{t("provider")}</th>
                    <th className="text-left py-2 font-medium">{t("asOf")}</th>
                  </tr>
                </thead>
                <tbody>
                  {(data?.rates ?? []).map((r: any, i: number) => (
                    <tr key={i} className="border-b">
                      <td className="py-2">{r?.fromCurrencyCode}/{r?.toCurrencyCode}</td>
                      <td className="py-2 text-right">{r?.rateDecimal}</td>
                      <td className="py-2">{r?.provider}</td>
                      <td className="py-2">
                        {r?.asOf?.seconds ? new Date(r.asOf.seconds * 1000).toISOString().slice(0, 10) : ""}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          {/* Add/Update Rate Form */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">{t("upsertTitle")}</h3>
            </div>
            <div className="card-content">
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  upsertMut.mutate();
                }}
                className="flex flex-col sm:flex-row gap-4 items-start sm:items-end"
              >
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">{t("from")}</label>
                  <input 
                    className="input w-24" 
                    value={uFrom} 
                    onChange={(e) => setUFrom(e.target.value)}
                    placeholder="USD"
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">{t("to")}</label>
                  <input 
                    className="input w-24" 
                    value={uTo} 
                    onChange={(e) => setUTo(e.target.value)}
                    placeholder="RUB"
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">{t("rateDecimal")}</label>
                  <input 
                    className="input w-32" 
                    value={uRate} 
                    onChange={(e) => setURate(e.target.value)}
                    placeholder="75.50"
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">{t("asOf")}</label>
                  <input 
                    className="input" 
                    type="date" 
                    value={uDate} 
                    onChange={(e) => setUDate(e.target.value)}
                    autoComplete="off"
                  />
                </div>
                <button 
                  className="btn btn-primary" 
                  disabled={upsertMut.isPending}
                >
                  {upsertMut.isPending ? (
                    <div className="flex items-center space-x-2">
                      <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></div>
                      <span>{tc("loading")}</span>
                    </div>
                  ) : (
                    <span>{t("save")}</span>
                  )}
                </button>
              </form>
            </div>
          </div>
        </div>
      )}
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


