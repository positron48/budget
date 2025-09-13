import { memo, useState, useMemo } from "react";
import { useTranslations } from "next-intl";
import { Card, CardContent, CardHeader, CardTitle, CardDescription, Button } from "@/components";
import { formatAmountWithSpaces } from "@/lib/utils";
import { getCategoryColor } from "@/lib/categoryColors";
import { Icon } from "@/components";

interface CategoryData {
  id: string;
  name: string;
  color: string;
  values: number[];
  total: number;
}

interface CombinedChartProps {
  title: string;
  description?: string;
  expenses: CategoryData[];
  incomes: CategoryData[];
  months: string[];
  currencyCode: string;
  className?: string;
}

const CombinedChart = memo(function CombinedChart({
  title,
  description,
  expenses,
  incomes,
  months,
  currencyCode,
  className,
}: CombinedChartProps) {
  const t = useTranslations("reports");
  const [showExpenses, setShowExpenses] = useState(true);
  const [showIncomes, setShowIncomes] = useState(true);
  const [chartType, setChartType] = useState<"line" | "bar">("line");

  const totalByMonth = useMemo(() => {
    return months.map((_, monthIndex) => {
      const expenseTotal = showExpenses 
        ? expenses.reduce((sum, cat) => sum + (cat.values[monthIndex] || 0), 0)
        : 0;
      const incomeTotal = showIncomes 
        ? incomes.reduce((sum, cat) => sum + (cat.values[monthIndex] || 0), 0)
        : 0;
      return { expenses: expenseTotal, incomes: incomeTotal, net: incomeTotal - expenseTotal };
    });
  }, [expenses, incomes, months, showExpenses, showIncomes]);

  const maxValue = useMemo(() => {
    let max = 0;
    totalByMonth.forEach(month => {
      if (month.expenses > max) max = month.expenses;
      if (month.incomes > max) max = month.incomes;
    });
    return max;
  }, [totalByMonth]);

  const renderLineChart = () => {
    const width = 1200;
    const height = 300;
    const padding = { top: 20, right: 40, bottom: 40, left: 80 };
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;

    const xScale = (index: number) => padding.left + (index / (months.length - 1)) * chartWidth;
    const yScale = (value: number) => padding.top + chartHeight - (value / maxValue) * chartHeight;

    return (
      <div className="w-full">
        <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} className="w-full">
          {/* Grid lines */}
          {[0, 0.25, 0.5, 0.75, 1].map((ratio) => (
            <g key={ratio}>
              <line
                x1={padding.left}
                y1={padding.top + (1 - ratio) * chartHeight}
                x2={padding.left + chartWidth}
                y2={padding.top + (1 - ratio) * chartHeight}
                stroke="currentColor"
                strokeWidth="1"
                opacity="0.1"
              />
              <text
                x={padding.left - 10}
                y={padding.top + (1 - ratio) * chartHeight + 4}
                textAnchor="end"
                fontSize="12"
                fill="currentColor"
                opacity="0.6"
              >
                {formatAmountWithSpaces(maxValue * ratio)}
              </text>
            </g>
          ))}

          {/* Month labels */}
          {months.map((month, index) => (
            <text
              key={month}
              x={xScale(index)}
              y={height - 10}
              textAnchor="middle"
              fontSize="12"
              fill="currentColor"
              opacity="0.6"
            >
              {month.split(' ')[0]}
            </text>
          ))}

          {/* Zero line */}
          <line
            x1={padding.left}
            y1={padding.top + chartHeight}
            x2={padding.left + chartWidth}
            y2={padding.top + chartHeight}
            stroke="currentColor"
            strokeWidth="2"
            opacity="0.3"
          />

          {/* Expenses line */}
          {showExpenses && (
            <g>
              <polyline
                points={totalByMonth
                  .map((month, index) => `${xScale(index)},${yScale(month.expenses)}`)
                  .join(' ')}
                fill="none"
                stroke={getCategoryColor("Expenses", "expense")}
                strokeWidth="3"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
              {totalByMonth.map((month, index) => (
                <circle
                  key={index}
                  cx={xScale(index)}
                  cy={yScale(month.expenses)}
                  r="4"
                  fill={getCategoryColor("Expenses", "expense")}
                  stroke="white"
                  strokeWidth="2"
                />
              ))}
            </g>
          )}

          {/* Incomes line */}
          {showIncomes && (
            <g>
              <polyline
                points={totalByMonth
                  .map((month, index) => `${xScale(index)},${yScale(month.incomes)}`)
                  .join(' ')}
                fill="none"
                stroke={getCategoryColor("Income", "income")}
                strokeWidth="3"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
              {totalByMonth.map((month, index) => (
                <circle
                  key={index}
                  cx={xScale(index)}
                  cy={yScale(month.incomes)}
                  r="4"
                  fill={getCategoryColor("Income", "income")}
                  stroke="white"
                  strokeWidth="2"
                />
              ))}
            </g>
          )}

          {/* Net line */}
          <g>
            <polyline
              points={totalByMonth
                .map((month, index) => `${xScale(index)},${yScale(Math.abs(month.net))}`)
                .join(' ')}
              fill="none"
              stroke="#6366f1"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeDasharray="5,5"
            />
          </g>
        </svg>
      </div>
    );
  };

  const renderBarChart = () => {
    const width = 1200;
    const height = 300;
    const padding = { top: 20, right: 40, bottom: 40, left: 80 };
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;

    const barWidth = chartWidth / months.length * 0.6;
    const barSpacing = chartWidth / months.length * 0.4;

    const xScale = (index: number) => padding.left + index * (barWidth + barSpacing);
    const yScale = (value: number) => padding.top + chartHeight - (value / maxValue) * chartHeight;

    return (
      <div className="w-full">
        <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} className="w-full">
          {/* Grid lines */}
          {[0, 0.25, 0.5, 0.75, 1].map((ratio) => (
            <g key={ratio}>
              <line
                x1={padding.left}
                y1={padding.top + (1 - ratio) * chartHeight}
                x2={padding.left + chartWidth}
                y2={padding.top + (1 - ratio) * chartHeight}
                stroke="currentColor"
                strokeWidth="1"
                opacity="0.1"
              />
              <text
                x={padding.left - 10}
                y={padding.top + (1 - ratio) * chartHeight + 4}
                textAnchor="end"
                fontSize="12"
                fill="currentColor"
                opacity="0.6"
              >
                {formatAmountWithSpaces(maxValue * ratio)}
              </text>
            </g>
          ))}

          {/* Month labels */}
          {months.map((month, index) => (
            <text
              key={month}
              x={xScale(index) + barWidth / 2}
              y={height - 10}
              textAnchor="middle"
              fontSize="12"
              fill="currentColor"
              opacity="0.6"
            >
              {month.split(' ')[0]}
            </text>
          ))}

          {/* Zero line */}
          <line
            x1={padding.left}
            y1={padding.top + chartHeight}
            x2={padding.left + chartWidth}
            y2={padding.top + chartHeight}
            stroke="currentColor"
            strokeWidth="2"
            opacity="0.3"
          />

          {/* Bars for each month */}
          {months.map((month, monthIndex) => {
            const monthData = totalByMonth[monthIndex];
            const x = xScale(monthIndex);

            return (
              <g key={month}>
                {/* Expenses bar */}
                {showExpenses && monthData.expenses > 0 && (
                  <rect
                    x={x}
                    y={yScale(monthData.expenses)}
                    width={barWidth / 2}
                    height={padding.top + chartHeight - yScale(monthData.expenses)}
                    fill={getCategoryColor("Expenses", "expense")}
                    opacity="0.8"
                  />
                )}

                {/* Incomes bar */}
                {showIncomes && monthData.incomes > 0 && (
                  <rect
                    x={x + barWidth / 2}
                    y={yScale(monthData.incomes)}
                    width={barWidth / 2}
                    height={padding.top + chartHeight - yScale(monthData.incomes)}
                    fill={getCategoryColor("Income", "income")}
                    opacity="0.8"
                  />
                )}
              </g>
            );
          })}
        </svg>
      </div>
    );
  };

  const renderChart = () => {
    switch (chartType) {
      case "line":
        return renderLineChart();
      case "bar":
        return renderBarChart();
      default:
        return renderLineChart();
    }
  };

  const totalExpenses = expenses.reduce((sum, cat) => sum + cat.total, 0);
  const totalIncomes = incomes.reduce((sum, cat) => sum + cat.total, 0);
  const netIncome = totalIncomes - totalExpenses;

  return (
    <Card className={className}>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-base">{title}</CardTitle>
            {description && <CardDescription className="text-sm">{description}</CardDescription>}
          </div>
          <div className="flex gap-1">
            <Button
              size="sm"
              variant={chartType === "line" ? "primary" : "outline"}
              onClick={() => setChartType("line")}
              className="text-xs"
            >
              <Icon name="trending-up" size={14} />
            </Button>
            <Button
              size="sm"
              variant={chartType === "bar" ? "primary" : "outline"}
              onClick={() => setChartType("bar")}
              className="text-xs"
            >
              <Icon name="bar-chart" size={14} />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {/* Toggle buttons */}
        <div className="mb-6">
          <h4 className="text-sm font-medium text-foreground mb-3">{t("toggleCategories")}</h4>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              variant={showExpenses ? "primary" : "outline"}
              onClick={() => setShowExpenses(!showExpenses)}
              className="text-xs flex items-center gap-2"
            >
              <div 
                className="w-3 h-3 rounded-full" 
                style={{ backgroundColor: getCategoryColor("Expenses", "expense") }}
              />
              {t("expense")}
            </Button>
            <Button
              size="sm"
              variant={showIncomes ? "primary" : "outline"}
              onClick={() => setShowIncomes(!showIncomes)}
              className="text-xs flex items-center gap-2"
            >
              <div 
                className="w-3 h-3 rounded-full" 
                style={{ backgroundColor: getCategoryColor("Income", "income") }}
              />
              {t("income")}
            </Button>
          </div>
        </div>

        {/* Chart */}
        <div className="mb-6">
          {renderChart()}
        </div>

        {/* Summary */}
        <div className="pt-4 border-t border-gray-200 dark:border-gray-700">
          <h4 className="text-sm font-medium text-foreground mb-3">{t("categoryTotals")}</h4>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div className="text-center">
              <div className="text-lg font-bold text-red-600">
                {formatAmountWithSpaces(totalExpenses)} {currencyCode}
              </div>
              <div className="text-xs text-muted-foreground">{t("totalExpenses")}</div>
            </div>
            <div className="text-center">
              <div className="text-lg font-bold text-green-600">
                {formatAmountWithSpaces(totalIncomes)} {currencyCode}
              </div>
              <div className="text-xs text-muted-foreground">{t("totalIncome")}</div>
            </div>
            <div className="text-center">
              <div className={`text-lg font-bold ${netIncome >= 0 ? 'text-green-600' : 'text-red-600'}`}>
                {formatAmountWithSpaces(netIncome)} {currencyCode}
              </div>
              <div className="text-xs text-muted-foreground">{t("netIncome")}</div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
});

export default CombinedChart;