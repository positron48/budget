import { memo, useState, useMemo, useRef } from "react";
import type { MouseEvent } from "react";
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
type TooltipState = {
  x: number;
  y: number;
  key: string;
  title: string;
  lines: string[];
} | null;

const formatValue = (value: number, currencyCode: string) =>
  `${formatAmountWithSpaces(value)}${currencyCode ? ` ${currencyCode}` : ""}`;

const niceStep = (roughStep: number) => {
  const exponent = Math.floor(Math.log10(Math.max(roughStep, 1)));
  const fraction = roughStep / 10 ** exponent;
  const niceFraction = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10;
  return niceFraction * 10 ** exponent;
};

const niceMax = (value: number) => {
  const max = Math.max(value, 100);
  const step = niceStep(max / 4);
  return { maxValue: Math.ceil(max / step) * step, step };
};

const InteractiveChart = memo(function InteractiveChart({
  title,
  description,
  categories,
  months,
  currencyCode,
  className,
}: InteractiveChartProps) {
  const t = useTranslations("reports");
  const containerRef = useRef<HTMLDivElement>(null);
  const [visibleCategories, setVisibleCategories] = useState<Set<string>>(
    new Set(categories.map(cat => cat.id))
  );
  const [chartType, setChartType] = useState<ChartType>("bar");
  const [tooltip, setTooltip] = useState<TooltipState>(null);

  const showTooltip = (
    event: MouseEvent<Element>,
    key: string,
    titleText: string,
    lines: string[]
  ) => {
    const rect = containerRef.current?.getBoundingClientRect();
    setTooltip({
      x: event.clientX - (rect?.left || 0),
      y: event.clientY - (rect?.top || 0),
      key,
      title: titleText,
      lines,
    });
  };

  const moveTooltip = (event: MouseEvent<Element>) => {
    if (!tooltip) return;
    const rect = containerRef.current?.getBoundingClientRect();
    setTooltip((current) =>
      current ? { ...current, x: event.clientX - (rect?.left || 0), y: event.clientY - (rect?.top || 0) } : null
    );
  };

  const clearTooltip = () => setTooltip(null);

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
      return niceMax(max);
    } else if (chartType === "bar") {
      // For bar chart, use the maximum total by month
      let max = 0;
      totalByMonth.forEach(value => {
        if (value > max) max = value;
      });
      return niceMax(max);
    } else {
      // For line chart, use individual category values
      let max = 0;
      visibleCategoriesData.forEach(cat => {
        cat.values.forEach(value => {
          if (value > max) max = value;
        });
      });
      return niceMax(max);
    }
  }, [visibleCategoriesData, chartType, cumulativeValues, totalByMonth]);


  const renderLineChart = () => {
    const width = 1200;
    const height = 400;
    const padding = { top: 40, right: 40, bottom: 40, left: 80 };
    const labelOffset = 60; // место для подписей слева
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;
    const svgWidth = width + labelOffset; // увеличиваем ширину SVG для подписей

    const xScale = (index: number) => padding.left + labelOffset + (months.length <= 1 ? chartWidth / 2 : (index / (months.length - 1)) * chartWidth);
    const yScale = (value: number) => padding.top + chartHeight - (value / maxValue.maxValue) * chartHeight;
    return (
      <div className="w-full">
        <svg width="100%" height={height} viewBox={`0 0 ${svgWidth} ${height}`} className="w-full" preserveAspectRatio="xMidYMid meet">
          {/* Grid lines */}
          {Array.from({ length: Math.round(maxValue.maxValue / maxValue.step) + 1 }, (_, index) => index * maxValue.step).map((value) => (
            <g key={value}>
              <line
                x1={padding.left + labelOffset}
                y1={yScale(value)}
                x2={padding.left + labelOffset + chartWidth}
                y2={yScale(value)}
                stroke="currentColor"
                strokeWidth="1"
                opacity="0.1"
              />
              <text
                x={padding.left + labelOffset - 10}
                y={yScale(value) + 4}
                textAnchor="end"
                fontSize="12"
                fill="currentColor"
                opacity="0.6"
                className="select-none"
              >
                {formatAmountWithSpaces(value)}
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
            const seriesActive = tooltip?.key.startsWith(`line-${category.id}-`);
            const points = category.values
              .map((value, index) => `${xScale(index)},${yScale(value)}`)
              .join(' ');

            return (
              <g key={category.id}>
                <polyline
                  points={points}
                  fill="none"
                  stroke={category.color}
                  strokeWidth={seriesActive ? "4" : "3"}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  opacity={tooltip && !seriesActive ? 0.35 : 1}
                />
                {/* Data points */}
                {category.values.map((value, index) => {
                  const itemKey = `line-${category.id}-${index}`;
                  return (
                    <g key={itemKey}>
                      <circle
                        data-testid={itemKey}
                        cx={xScale(index)}
                        cy={yScale(value)}
                        r={tooltip?.key === itemKey ? "6" : "4"}
                        fill={category.color}
                        stroke="white"
                        strokeWidth="2"
                        opacity={tooltip && tooltip.key !== itemKey && !seriesActive ? 0.45 : 1}
                        className="cursor-pointer"
                        onMouseEnter={(event) =>
                          showTooltip(event, itemKey, months[index], [`${category.name}: ${formatValue(value, currencyCode)}`])
                        }
                        onMouseMove={moveTooltip}
                        onMouseLeave={clearTooltip}
                      />
                      <circle
                        cx={xScale(index)}
                        cy={yScale(value)}
                        r="14"
                        fill="transparent"
                        className="cursor-pointer"
                        onMouseEnter={(event) =>
                          showTooltip(event, itemKey, months[index], [`${category.name}: ${formatValue(value, currencyCode)}`])
                        }
                        onMouseMove={moveTooltip}
                        onMouseLeave={clearTooltip}
                      />
                    </g>
                  );
                })}
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
    const labelOffset = 60; // место для подписей слева
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;
    const svgWidth = width + labelOffset; // увеличиваем ширину SVG для подписей

    const barWidth = chartWidth / months.length * 0.8;
    const barSpacing = chartWidth / months.length * 0.2;

    const xScale = (index: number) => padding.left + labelOffset + index * (barWidth + barSpacing);

    return (
      <div className="w-full">
        <svg width="100%" height={height} viewBox={`0 0 ${svgWidth} ${height}`} className="w-full" preserveAspectRatio="xMidYMid meet">
          {/* Grid lines */}
          {Array.from({ length: Math.round(maxValue.maxValue / maxValue.step) + 1 }, (_, index) => index * maxValue.step).map((value) => (
            <g key={value}>
              <line
                x1={padding.left + labelOffset}
                y1={padding.top + chartHeight - (value / maxValue.maxValue) * chartHeight}
                x2={padding.left + labelOffset + chartWidth}
                y2={padding.top + chartHeight - (value / maxValue.maxValue) * chartHeight}
                stroke="currentColor"
                strokeWidth="1"
                opacity="0.1"
              />
              <text
                x={padding.left + labelOffset - 10}
                y={padding.top + chartHeight - (value / maxValue.maxValue) * chartHeight + 4}
                textAnchor="end"
                fontSize="12"
                fill="currentColor"
                opacity="0.6"
                className="select-none"
              >
                {formatAmountWithSpaces(value)}
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

                  const barHeight = (value / maxValue.maxValue) * chartHeight;
                  currentY -= barHeight;
                  const itemKey = `bar-${category.id}-${monthIndex}`;

                  return (
                    <rect
                      key={itemKey}
                      data-testid={itemKey}
                      x={x}
                      y={currentY}
                      width={barWidth}
                      height={barHeight}
                      fill={category.color}
                      opacity={tooltip && tooltip.key !== itemKey ? 0.45 : 0.85}
                      className="cursor-pointer"
                      onMouseEnter={(event) =>
                        showTooltip(event, itemKey, month, [`${category.name}: ${formatValue(value, currencyCode)}`])
                      }
                      onMouseMove={moveTooltip}
                      onMouseLeave={clearTooltip}
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
    const labelOffset = 60; // место для подписей слева
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;
    const svgWidth = width + labelOffset; // увеличиваем ширину SVG для подписей

    const xScale = (index: number) => padding.left + labelOffset + (months.length <= 1 ? chartWidth / 2 : (index / (months.length - 1)) * chartWidth);
    const yScale = (value: number) => padding.top + chartHeight - (value / maxValue.maxValue) * chartHeight;

    return (
      <div className="w-full">
        <svg width="100%" height={height} viewBox={`0 0 ${svgWidth} ${height}`} className="w-full" preserveAspectRatio="xMidYMid meet">
          {/* Grid lines */}
          {Array.from({ length: Math.round(maxValue.maxValue / maxValue.step) + 1 }, (_, index) => index * maxValue.step).map((value) => (
            <g key={value}>
              <line
                x1={padding.left + labelOffset}
                y1={yScale(value)}
                x2={padding.left + labelOffset + chartWidth}
                y2={yScale(value)}
                stroke="currentColor"
                strokeWidth="1"
                opacity="0.1"
              />
              <text
                x={padding.left + labelOffset - 10}
                y={yScale(value) + 4}
                textAnchor="end"
                fontSize="12"
                fill="currentColor"
                opacity="0.6"
                className="select-none"
              >
                {formatAmountWithSpaces(value)}
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
            const itemKey = `area-${category.id}`;
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
                  data-testid={itemKey}
                  d={areaPath}
                  fill={category.color}
                  opacity={tooltip && tooltip.key !== itemKey ? 0.35 : 0.7}
                  className="cursor-pointer"
                  onMouseEnter={(event) =>
                    showTooltip(event, itemKey, category.name, [
                      `${t("total")}: ${formatValue(category.total, currencyCode)}`,
                    ])
                  }
                  onMouseMove={moveTooltip}
                  onMouseLeave={clearTooltip}
                />
                {/* Top line for this category */}
                <polyline
                  points={topPoints.join(' ')}
                  fill="none"
                  stroke={category.color}
                  strokeWidth={tooltip?.key === itemKey ? "3" : "2"}
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
        <div className="relative mb-6" ref={containerRef}>
          {renderChart()}
          {tooltip && (
            <div
              role="tooltip"
              className="pointer-events-none absolute z-10 rounded bg-foreground px-2 py-1 text-xs text-background shadow"
              style={{ left: tooltip.x + 8, top: tooltip.y - 8 }}
            >
              <div className="font-medium">{tooltip.title}</div>
              {tooltip.lines.map((line) => (
                <div key={line}>{line}</div>
              ))}
            </div>
          )}
        </div>

        {/* Interactive Legend */}
        <div className="pt-4 border-t border-border/70">
          <h4 className="text-sm font-medium text-foreground mb-3">{t("categoryTotals")}</h4>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
            {categories
              .sort((a, b) => b.total - a.total) // Sort by total descending (largest first)
              .map((category) => {
                const isVisible = visibleCategories.has(category.id);
                const baseClass = isVisible
                  ? "bg-card/70 border border-border/70"
                  : "bg-secondary/40 border border-border/40 text-muted-foreground opacity-70";
                return (
                  <div 
                    key={category.id} 
                    className={`flex items-center justify-between text-sm cursor-pointer p-2 rounded-none transition-colors ${baseClass}`}
                    onClick={() => toggleCategory(category.id)}
                  >
                    <div className="flex items-center gap-2">
                      <div 
                        className="w-3 h-3 rounded-full" 
                        style={{ backgroundColor: isVisible ? category.color : '#9CA3AF' }}
                      />
                      <span className={isVisible ? "text-foreground" : "text-muted-foreground"}>
                        {category.name}
                      </span>
                    </div>
                    <span className="font-medium text-muted-foreground">
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
