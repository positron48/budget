import Icon from "./Icon";
import { formatCurrency } from "@/lib/utils";
import { useTranslations } from "next-intl";

interface TransactionStatsProps {
  totalIncome: number;
  totalExpenses: number;
  currencyCode: string;
  period?: string;
}

export default function TransactionStats({ 
  totalIncome, 
  totalExpenses, 
  currencyCode, 
  period = "за период" 
}: TransactionStatsProps) {
  const t = useTranslations("transactions");
  const netIncome = totalIncome - totalExpenses;
  const isPositive = netIncome >= 0;
  const cardBase =
    "rounded-none border p-4 shadow-sm bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60 transition-colors";
  const accentCard = (token: string) =>
    `${cardBase} border-[hsl(var(--${token})/0.4)] bg-[hsl(var(--${token})/0.08)]`;
  




  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-3 mb-4">
      {/* Total Income */}
      <div className={accentCard("positive")}>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs font-semibold tracking-wide uppercase text-[hsl(var(--positive))]">
              {t("totalIncome")}
            </p>
            <p className="text-2xl font-bold text-foreground">
              {formatCurrency(totalIncome, currencyCode)}
            </p>
            <p className="text-xs text-muted-foreground">{period}</p>
          </div>
          <div className="w-10 h-10 rounded-none bg-[hsl(var(--positive)/0.2)] flex items-center justify-center">
            <Icon name="trending-up" size={20} className="text-[hsl(var(--positive))]" />
          </div>
        </div>
      </div>

      {/* Total Expenses */}
      <div className={accentCard("negative")}>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs font-semibold tracking-wide uppercase text-[hsl(var(--negative))]">
              {t("totalExpenses")}
            </p>
            <p className="text-2xl font-bold text-foreground">
              {formatCurrency(totalExpenses, currencyCode)}
            </p>
            <p className="text-xs text-muted-foreground">{period}</p>
          </div>
          <div className="w-10 h-10 rounded-none bg-[hsl(var(--negative)/0.2)] flex items-center justify-center">
            <Icon name="trending-down" size={20} className="text-[hsl(var(--negative))]" />
          </div>
        </div>
      </div>

      {/* Net Income */}
      <div className={accentCard(isPositive ? "primary" : "warning")}>
        <div className="flex items-center justify-between">
          <div>
            <p
              className={`text-xs font-semibold tracking-wide uppercase ${
                isPositive ? "text-[hsl(var(--primary))]" : "text-[hsl(var(--warning))]"
              }`}
            >
              {t("netIncome")}
            </p>
            <p className="text-2xl font-bold text-foreground">
              {formatCurrency(Math.abs(netIncome), currencyCode)}
            </p>
            <p className="text-xs text-muted-foreground">
              {isPositive ? t("profit") : t("loss")} {period}
            </p>
          </div>
          <div className="flex items-center space-x-2">
            {totalIncome > 0 && (
              <div
                className={`text-2xl font-bold ${
                  isPositive ? "text-[hsl(var(--primary))]" : "text-[hsl(var(--warning))]"
                }`}
              >
                {isPositive ? "+" : "-"}
                {Math.round(Math.abs((netIncome / totalIncome) * 100))}%
              </div>
            )}
            <div
              className={`w-8 h-8 rounded-none flex items-center justify-center ${
                isPositive
                  ? "bg-[hsl(var(--primary)/0.2)]"
                  : "bg-[hsl(var(--warning)/0.2)]"
              }`}
            >
              <Icon
                name={isPositive ? "trending-up" : "trending-down"}
                size={16}
                className={isPositive ? "text-[hsl(var(--primary))]" : "text-[hsl(var(--warning))]"}
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
