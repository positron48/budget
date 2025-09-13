import { memo, useState, useMemo } from "react";
import { useTranslations } from "next-intl";
import { Card, CardContent, CardHeader, CardTitle, CardDescription, Button } from "@/components";
import { formatAmountWithSpaces } from "@/lib/utils";
import { Icon } from "@/components";

interface CategoryData {
  id: string;
  name: string;
  color: string;
  values: number[];
  total: number;
}

interface InteractiveChartProps {
  title: string;
  description?: string;
  categories: CategoryData[];
  months: string[];
  currencyCode: string;
  className?: string;
}

type ChartType = "line" | "bar" | "area";

const InteractiveChart = memo(function InteractiveChart({
  title,
  description,
  categories,
  months,
  currencyCode,
  className,
}: InteractiveChartProps) {
  const t = useTranslations("reports");
  const [visibleCategories, setVisibleCategories] = useState<Set<string>>(
    new Set(categories.map(cat => cat.id))
  );
  const [chartType, setChartType] = useState<ChartType>("area");

  const toggleCategory = (categoryId: string) => {
    const newVisible = new Set(visibleCategories);
    if (newVisible.has(categoryId)) {
      newVisible.delete(categoryId);
    } else {
      newVisible.add(categoryId);
    }
    setVisibleCategories(newVisible);
  };

  const visibleCategoriesData = useMemo(() => 
    categories
      .filter(cat => visibleCategories.has(cat.id))
      .sort((a, b) => b.total - a.total), // Sort by total descending (largest first)
    [categories, visibleCategories]
  );

  const totalByMonth = useMemo(() => {
    return months.map((_, monthIndex) => 
      visibleCategoriesData.reduce((sum, cat) => sum + (cat.values[monthIndex] || 0), 0)
    );
  }, [visibleCategoriesData, months]);

  // Calculate cumulative values for stacked areas
  const cumulativeValues = useMemo(() => {
    const result: number[][] = [];
    for (let monthIndex = 0; monthIndex < months.length; monthIndex++) {
      const monthValues: number[] = [];
      let cumulative = 0;
      
      visibleCategoriesData.forEach(category => {
        const value = category.values[monthIndex] || 0;
        cumulative += value;
        monthValues.push(cumulative);
      });
      
      result.push(monthValues);
    }
    return result;
  }, [visibleCategoriesData, months]);

  const maxValue = useMemo(() => {
    if (chartType === "area") {
      // For stacked area chart, use the maximum cumulative value
      let max = 0;
      cumulativeValues.forEach(monthValues => {
        const monthMax = monthValues[monthValues.length - 1] || 0;
        if (monthMax > max) max = monthMax;
      });
      return Math.max(max, 100);
    } else if (chartType === "bar") {
      // For bar chart, use the maximum total by month
      let max = 0;
      totalByMonth.forEach(value => {
        if (value > max) max = value;
      });
      return Math.max(max, 100);
    } else {
      // For line chart, use individual category values
      let max = 0;
      visibleCategoriesData.forEach(cat => {
        cat.values.forEach(value => {
          if (value > max) max = value;
        });
      });
      return Math.max(max, 100);
    }
  }, [visibleCategoriesData, chartType, cumulativeValues, totalByMonth]);


  const renderLineChart = () => {
    const width = 1200;
    const height = 400;
    const padding = { top: 40, right: 40, bottom: 40, left: 80 };
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

          {/* Lines for each category */}
          {visibleCategoriesData.map((category) => {
            const points = category.values
              .map((value, index) => `${xScale(index)},${yScale(value)}`)
              .join(' ');

            return (
              <g key={category.id}>
                <polyline
                  points={points}
                  fill="none"
                  stroke={category.color}
                  strokeWidth="3"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
                {/* Data points */}
                {category.values.map((value, index) => (
                  <circle
                    key={index}
                    cx={xScale(index)}
                    cy={yScale(value)}
                    r="4"
                    fill={category.color}
                    stroke="white"
                    strokeWidth="2"
                  />
                ))}
              </g>
            );
          })}
        </svg>
      </div>
    );
  };

  const renderBarChart = () => {
    const width = 1200;
    const height = 400;
    const padding = { top: 40, right: 40, bottom: 40, left: 80 };
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;

    const barWidth = chartWidth / months.length * 0.8;
    const barSpacing = chartWidth / months.length * 0.2;

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

          {/* Bars for each month */}
          {months.map((month, monthIndex) => {
            const monthTotal = totalByMonth[monthIndex];
            if (monthTotal === 0) return null;

            let currentY = padding.top + chartHeight;
            const x = xScale(monthIndex);

            return (
              <g key={month}>
                {visibleCategoriesData.map((category) => {
                  const value = category.values[monthIndex] || 0;
                  if (value === 0) return null;

                  const barHeight = (value / maxValue) * chartHeight;
                  currentY -= barHeight;

                  return (
                    <rect
                      key={category.id}
                      x={x}
                      y={currentY}
                      width={barWidth}
                      height={barHeight}
                      fill={category.color}
                      opacity="0.8"
                    />
                  );
                })}
              </g>
            );
          })}
        </svg>
      </div>
    );
  };

  const renderAreaChart = () => {
    const width = 1200;
    const height = 400;
    const padding = { top: 40, right: 40, bottom: 40, left: 80 };
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

          {/* Stacked areas */}
          {visibleCategoriesData.map((category, categoryIndex) => {
            // Create area path for this category
            const topPoints: string[] = [];
            const bottomPoints: string[] = [];
            
            for (let monthIndex = 0; monthIndex < months.length; monthIndex++) {
              const x = xScale(monthIndex);
              const topY = yScale(cumulativeValues[monthIndex][categoryIndex]);
              const bottomY = categoryIndex === 0 
                ? yScale(0) 
                : yScale(cumulativeValues[monthIndex][categoryIndex - 1]);
              
              topPoints.push(`${x},${topY}`);
              bottomPoints.unshift(`${x},${bottomY}`);
            }
            
            const areaPath = `M ${topPoints.join(' L ')} L ${bottomPoints.join(' L ')} Z`;

            return (
              <g key={category.id}>
                <path
                  d={areaPath}
                  fill={category.color}
                  opacity="0.7"
                />
                {/* Top line for this category */}
                <polyline
                  points={topPoints.join(' ')}
                  fill="none"
                  stroke={category.color}
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
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
      case "area":
        return renderAreaChart();
      default:
        return renderLineChart();
    }
  };

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
            <Button
              size="sm"
              variant={chartType === "area" ? "primary" : "outline"}
              onClick={() => setChartType("area")}
              className="text-xs"
            >
              <Icon name="layers" size={14} />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {/* Chart */}
        <div className="mb-6">
          {renderChart()}
        </div>

        {/* Interactive Legend */}
        <div className="pt-4 border-t border-gray-200 dark:border-gray-700">
          <h4 className="text-sm font-medium text-foreground mb-3">{t("categoryTotals")}</h4>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
            {categories
              .sort((a, b) => b.total - a.total) // Sort by total descending (largest first)
              .map((category) => {
                const isVisible = visibleCategories.has(category.id);
                return (
                  <div 
                    key={category.id} 
                    className={`flex items-center justify-between text-sm cursor-pointer p-2 rounded-md transition-colors ${
                      isVisible 
                        ? 'bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700' 
                        : 'opacity-50 hover:opacity-70'
                    }`}
                    onClick={() => toggleCategory(category.id)}
                  >
                    <div className="flex items-center gap-2">
                      <div 
                        className="w-3 h-3 rounded-full" 
                        style={{ backgroundColor: isVisible ? category.color : '#9CA3AF' }}
                      />
                      <span className={`${isVisible ? 'text-foreground' : 'text-gray-500'}`}>
                        {category.name}
                      </span>
                    </div>
                    <span className={`font-medium ${isVisible ? 'text-muted-foreground' : 'text-gray-400'}`}>
                      {formatAmountWithSpaces(category.total)} {currencyCode}
                    </span>
                  </div>
                );
              })}
          </div>
        </div>
      </CardContent>
    </Card>
  );
});

export default InteractiveChart;