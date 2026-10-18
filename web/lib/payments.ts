"use client";
import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
export function localPaymentMonth(now = new Date()) {
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}
export function nextPaymentMonth(month: string) {
  const [y, m] = month.split("-").map(Number);
  return localPaymentMonth(new Date(y, m, 1));
}
export function usePaymentMonth() {
  const [month, setMonth] = useState(localPaymentMonth);
  useEffect(() => {
    const update = () => setMonth(localPaymentMonth());
    const id = setInterval(update, 30000);
    window.addEventListener("focus", update);
    document.addEventListener("visibilitychange", update);
    return () => {
      clearInterval(id);
      window.removeEventListener("focus", update);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);
  return month;
}
export function useRefreshPayments() {
  const qc = useQueryClient();
  return async () => {
    await Promise.all(
      [
        "payments",
        "transactions",
        "transactionsTotals",
        "assets",
        "dashboard-monthly",
        "dashboard-recent",
        "monthly",
        "summary",
      ].map((key) => qc.invalidateQueries({ queryKey: [key] })),
    );
  };
}
