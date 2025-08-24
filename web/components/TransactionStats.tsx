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
  




  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-3 mb-4">
      {/* Total Income */}
      <div className="bg-gradient-to-br from-green-50 to-emerald-50 dark:from-green-900/20 dark:to-emerald-900/20 rounded-lg p-4 border border-green-200 dark:border-green-800">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs font-medium text-green-600 dark:text-green-400">{t("totalIncome")}</p>
            <p className="text-lg font-bold text-green-900 dark:text-green-100">
              {formatCurrency(totalIncome, currencyCode)}
            </p>
            <p className="text-xs text-green-600 dark:text-green-400">{period}</p>
          </div>
          <div className="w-10 h-10 bg-green-100 dark:bg-green-900/30 rounded-lg flex items-center justify-center">
            <Icon name="trending-up" size={20} className="text-green-600 dark:text-green-400" />
          </div>
        </div>
      </div>

      {/* Total Expenses */}
      <div className="bg-gradient-to-br from-red-50 to-rose-50 dark:from-red-900/20 dark:to-rose-900/20 rounded-lg p-4 border border-red-200 dark:border-red-800">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs font-medium text-red-600 dark:text-red-400">{t("totalExpenses")}</p>
            <p className="text-lg font-bold text-red-900 dark:text-red-100">
              {formatCurrency(totalExpenses, currencyCode)}
            </p>
            <p className="text-xs text-red-600 dark:text-red-400">{period}</p>
          </div>
          <div className="w-10 h-10 bg-red-100 dark:bg-red-900/30 rounded-lg flex items-center justify-center">
            <Icon name="trending-down" size={20} className="text-red-600 dark:text-red-400" />
          </div>
        </div>
      </div>

      {/* Net Income */}
      <div className={`bg-gradient-to-br rounded-lg p-4 border ${
        isPositive 
          ? 'from-blue-50 to-cyan-50 dark:from-blue-900/20 dark:to-cyan-900/20 border-blue-200 dark:border-blue-800' 
          : 'from-orange-50 to-amber-50 dark:from-orange-900/20 dark:to-amber-900/20 border-orange-200 dark:border-orange-800'
      }`}>
        <div className="flex items-center justify-between">
          <div>
            <p className={`text-xs font-medium ${
              isPositive ? 'text-blue-600 dark:text-blue-400' : 'text-orange-600 dark:text-orange-400'
            }`}>
              {t("netIncome")}
            </p>
            <p className={`text-lg font-bold ${
              isPositive ? 'text-blue-900 dark:text-blue-100' : 'text-orange-900 dark:text-orange-100'
            }`}>
              {formatCurrency(Math.abs(netIncome), currencyCode)}
            </p>
            <p className={`text-xs ${
              isPositive ? 'text-blue-600 dark:text-blue-400' : 'text-orange-600 dark:text-orange-400'
            }`}>
              {isPositive ? t("profit") : t("loss")} {period}
            </p>
          </div>
          <div className="flex items-center space-x-2">
            {/* Крупный процент слева от иконки */}
            {totalIncome > 0 && (
              <div className={`text-2xl font-bold ${
                isPositive ? 'text-blue-600 dark:text-blue-400' : 'text-orange-600 dark:text-orange-400'
              }`}>
                {isPositive ? '+' : '-'}{Math.round(Math.abs((netIncome / totalIncome) * 100))}%
              </div>
            )}
            {/* Иконка по центру */}
            <div className={`w-8 h-8 rounded-lg flex items-center justify-center ${
              isPositive 
                ? 'bg-blue-100 dark:bg-blue-900/30' 
                : 'bg-orange-100 dark:bg-orange-900/30'
            }`}>
              <Icon 
                name={isPositive ? "trending-up" : "trending-down"} 
                size={16} 
                className={isPositive ? "text-blue-600 dark:text-blue-400" : "text-orange-600 dark:text-orange-400"} 
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
