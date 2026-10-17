"use client";
import {useLocale} from "next-intl";
import {useDisplayCurrency, DISPLAY_CURRENCIES} from "@/lib/display-currency";

export default function DisplayCurrencySelect() {
  const {currency, setCurrency} = useDisplayCurrency();
  const locale = useLocale();
  const label = locale.startsWith("ru") ? "Валюта отображения" : "Display currency";
  return <select aria-label={label} title={label} value={currency}
    onChange={e => setCurrency(e.target.value)}
    className="max-w-20 rounded-md border border-border bg-background px-1 py-1.5 text-xs sm:px-2"
  >{DISPLAY_CURRENCIES.map(code => <option key={code}>{code}</option>)}</select>;
}
