"use client";
import Icon from "./Icon";
import {useLocale} from "next-intl";
import {useDisplayCurrency, DISPLAY_CURRENCIES} from "@/lib/display-currency";

export default function DisplayCurrencySelect() {
  const {currency, setCurrency} = useDisplayCurrency();
  const locale = useLocale();
  const label = locale.startsWith("ru") ? "Валюта отображения" : "Display currency";
  return (
    <div className="relative shrink-0">
      <select aria-label={label} title={label} value={currency}
        onChange={e => setCurrency(e.target.value)}
        className="h-8 w-[4.5rem] appearance-none rounded-md border border-border bg-background pl-2 pr-7 text-xs leading-normal"
      >{DISPLAY_CURRENCIES.map(code => <option key={code}>{code}</option>)}</select>
      <Icon name="chevron-down" size={16} className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
    </div>
  );
}
