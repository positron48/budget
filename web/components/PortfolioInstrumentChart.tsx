"use client";
import type {TrackerInstrumentPricePoint} from "@/proto/budget/v1/investment_nav_pb";
import type {InvestmentOperation} from "@/proto/budget/v1/investment_pb";
import {reportingDate} from "@/lib/portfolio-report";
import PortfolioLineChart from "./PortfolioLineChart";
type Mode = "quote" | "nativeClean" | "clean" | "nativeDirty" | "dirty";
export default function PortfolioInstrumentChart({points, operations, mode, from, to, ru, average, currency}: {
  points: TrackerInstrumentPricePoint[]; operations: InvestmentOperation[]; mode: Mode; from: string; to: string; ru: boolean; average?: number; currency?:string;
}) {
  const tr = (r: string, e: string) => ru ? r : e;
  const value = (p: TrackerInstrumentPricePoint) => {const v = mode === "quote" ? p.raw : p[mode]; if (!v) return undefined; const n = Number(v.numerator) / Number(v.denominator); return Number.isFinite(n) ? n : undefined;};
  const units = new Set(points.filter(p => value(p) !== undefined).map(p => mode === "clean" || mode === "dirty" ? "display" : mode === "quote" && p.unit === "percent_of_nominal" ? "%" : p.nativeCurrency + "/" + (mode === "quote" ? p.unit : "money")));
  if (units.size > 1) return <p role="status" className="py-10 text-sm text-amber-600">{tr("Выберите одну валюту или отдельный листинг", "Select one currency or a single listing")}</p>;
  const percentQuote=mode==="quote"&&points.some(p=>p.raw&&p.unit==="percent_of_nominal");
  const chartCurrency=mode==="clean"||mode==="dirty"?currency:percentQuote||mode==="quote"&&points.some(p=>p.raw&&p.unit!=="currency_per_unit")?undefined:points.find(p=>value(p)!==undefined)?.nativeCurrency;
  const markers = operations.filter(o => ["buy", "sell"].includes(o.kind) && o.occurredAt).map(o => ({date: reportingDate(o.occurredAt!.seconds), kind: o.kind as "buy" | "sell", label: `${reportingDate(o.occurredAt!.seconds)} · ${o.kind === "buy" ? tr("Покупка", "Buy") : tr("Продажа", "Sell")} · ${o.quantity} · ${o.amount} ${o.currency}`})).filter(o => o.date >= from && o.date <= to);
  return <PortfolioLineChart precision={6} currency={chartCurrency} unit={percentQuote?"%":undefined} series={[{key: "price", title: tr("Цена", "Price"), color: "#13b5ea", points: points.map(p => ({date: p.date, value: value(p), segment: p.source}))}]} markers={markers} from={from} to={to} locale={ru ? "ru" : "en"} title={tr("История цены", "Price history")} reference={average === undefined ? undefined : {value: average, label: tr("Средняя в портфеле", "Portfolio average")}}/>;
}
