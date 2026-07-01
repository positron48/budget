"use client";

import { memo, useMemo, useRef, useState } from "react";
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

interface CombinedChartProps {
  title: string;
  description?: string;
  expenses: CategoryData[];
  incomes: CategoryData[];
  months: string[];
  currencyCode: string;
  className?: string;
}

type ChartType = "line" | "bar";
type TooltipState = {
  x: number;
  y: number;
  key: string;
  title: string;
  lines: string[];
} | null;

const width = 1200;
const height = 300;
const padding = { top: 24, right: 40, bottom: 40, left: 80 };
const labelOffset = 60;
const chartWidth = width - padding.left - padding.right;
const chartHeight = height - padding.top - padding.bottom;
const svgWidth = width + labelOffset;
const chartLeft = padding.left + labelOffset;
const chartRight = chartLeft + chartWidth;

const formatValue = (value: number, currencyCode: string) =>
  `${formatAmountWithSpaces(value)}${currencyCode ? ` ${currencyCode}` : ""}`;

const niceStep = (roughStep: number) => {
  const exponent = Math.floor(Math.log10(Math.max(roughStep, 1)));
  const fraction = roughStep / 10 ** exponent;
  const niceFraction = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10;
  return niceFraction * 10 ** exponent;
};

const getRange = (values: number[]) => {
  const finiteValues = values.filter(Number.isFinite);
  let minValue = Math.min(0, ...finiteValues);
  let maxValue = Math.max(0, ...finiteValues);

  if (minValue === maxValue) {
    minValue -= 100;
    maxValue += 100;
  }
  const step = niceStep((maxValue - minValue) / 4);
  return {
    minValue: Math.floor(minValue / step) * step,
    maxValue: Math.ceil(maxValue / step) * step,
    step,
  };
};

const getX = (index: number, count: number) => {
  if (count <= 1) return chartLeft + chartWidth / 2;
  return chartLeft + (index / (count - 1)) * chartWidth;
};

const getBarX = (index: number, count: number, barWidth: number, barSpacing: number) =>
  chartLeft + index * (barWidth + barSpacing);

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
  const containerRef = useRef<HTMLDivElement>(null);
  // Expenses and incomes are always shown; the "net" series is derived from both.
  const showExpenses = true;
  const showIncomes = true;
  const [chartType, setChartType] = useState<ChartType>("line");
  const [tooltip, setTooltip] = useState<TooltipState>(null);

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

  const cumulativeByMonth = useMemo(() => {
    let running = 0;
    return totalByMonth.map((month) => {
      running += month.net;
      return { monthlyNet: month.net, cumulativeNet: running };
    });
  }, [totalByMonth]);

  const mainRange = useMemo(
    () =>
      getRange(
        totalByMonth.flatMap((month) => [
          showExpenses ? month.expenses : 0,
          showIncomes ? month.incomes : 0,
          month.net,
        ])
      ),
    [showExpenses, showIncomes, totalByMonth]
  );

  const cumulativeRange = useMemo(
    () => getRange(cumulativeByMonth.flatMap((month) => [month.monthlyNet, month.cumulativeNet])),
    [cumulativeByMonth]
  );

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

  const yScale = (value: number, range: { minValue: number; maxValue: number }) => {
    const span = range.maxValue - range.minValue || 1;
    return padding.top + chartHeight - ((value - range.minValue) / span) * chartHeight;
  };

  const renderGrid = (range: { minValue: number; maxValue: number; step: number }) => (
    <>
      {Array.from({ length: Math.round((range.maxValue - range.minValue) / range.step) + 1 }, (_, index) => range.minValue + index * range.step).map((value) => {
        const y = yScale(value, range);
        return (
          <g key={value}>
            <line x1={chartLeft} y1={y} x2={chartRight} y2={y} stroke="currentColor" strokeWidth="1" opacity="0.1" />
            <text
              x={chartLeft - 10}
              y={y + 4}
              textAnchor="end"
              fontSize="12"
              fill="currentColor"
              opacity="0.6"
              className="select-none"
            >
              {formatAmountWithSpaces(Math.round(value))}
            </text>
          </g>
        );
      })}
      <line
        data-testid="combined-zero-line"
        x1={chartLeft}
        y1={yScale(0, range)}
        x2={chartRight}
        y2={yScale(0, range)}
        stroke="currentColor"
        strokeWidth="2"
        opacity="0.35"
      />
    </>
  );

  const renderMonthLabels = (barWidth?: number, barSpacing?: number) =>
    months.map((month, index) => (
      <text
        key={month}
        x={barWidth && barSpacing ? getBarX(index, months.length, barWidth, barSpacing) + barWidth / 2 : getX(index, months.length)}
        y={height - 10}
        textAnchor="middle"
        fontSize="12"
        fill="currentColor"
        opacity="0.6"
      >
        {month.split(" ")[0]}
      </text>
    ));

  const renderLineSeries = (label: string, keyPrefix: string, color: string, values: number[], range = mainRange, dashed = false) => {
    const active = tooltip?.key.startsWith(keyPrefix);
    const points = values.map((value, index) => `${getX(index, months.length)},${yScale(value, range)}`).join(" ");

    return (
      <g>
        <polyline
          data-testid={`${keyPrefix}-line`}
          points={points}
          fill="none"
          stroke={color}
          strokeWidth={active ? 4 : 3}
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeDasharray={dashed ? "5,5" : undefined}
          opacity={tooltip && !active ? 0.35 : 1}
        />
        {values.map((value, index) => {
          const itemKey = `${keyPrefix}-${index}`;
          return (
            <g key={itemKey}>
              <circle
                data-testid={itemKey}
                cx={getX(index, months.length)}
                cy={yScale(value, range)}
                r={tooltip?.key === itemKey ? 6 : 4}
                fill={color}
                stroke="hsl(var(--card))"
                strokeWidth="2"
                opacity={tooltip && tooltip.key !== itemKey && !active ? 0.45 : 1}
                className="cursor-pointer"
                onMouseEnter={(event) =>
                  showTooltip(event, itemKey, months[index], [`${label}: ${formatValue(value, currencyCode)}`])
                }
                onMouseMove={moveTooltip}
                onMouseLeave={clearTooltip}
              />
              <circle
                cx={getX(index, months.length)}
                cy={yScale(value, range)}
                r="14"
                fill="transparent"
                className="cursor-pointer"
                onMouseEnter={(event) =>
                  showTooltip(event, itemKey, months[index], [`${label}: ${formatValue(value, currencyCode)}`])
                }
                onMouseMove={moveTooltip}
                onMouseLeave={clearTooltip}
              />
            </g>
          );
        })}
      </g>
    );
  };

  const renderLineChart = () => (
    <svg width="100%" height={height} viewBox={`0 0 ${svgWidth} ${height}`} className="w-full min-w-[560px] sm:min-w-0" preserveAspectRatio="xMidYMid meet">
      {renderGrid(mainRange)}
      {renderMonthLabels()}
      {showExpenses && renderLineSeries(t("expense"), "expense", "#ef4444", totalByMonth.map((month) => month.expenses))}
      {showIncomes && renderLineSeries(t("income"), "income", "#22c55e", totalByMonth.map((month) => month.incomes))}
      {renderLineSeries(t("netIncome"), "net", "#3b82f6", totalByMonth.map((month) => month.net), mainRange, true)}
    </svg>
  );

  const renderBarChart = () => {
    const slotWidth = chartWidth / Math.max(months.length, 1);
    const barWidth = slotWidth * 0.6;
    const barSpacing = slotWidth * 0.4;
    const zeroY = yScale(0, mainRange);

    return (
      <svg width="100%" height={height} viewBox={`0 0 ${svgWidth} ${height}`} className="w-full min-w-[560px] sm:min-w-0" preserveAspectRatio="xMidYMid meet">
        {renderGrid(mainRange)}
        {renderMonthLabels(barWidth, barSpacing)}
        {months.map((month, monthIndex) => {
          const monthData = totalByMonth[monthIndex];
          const x = getBarX(monthIndex, months.length, barWidth, barSpacing);
          const bars = [
            showExpenses ? { key: "expense", label: t("expense"), value: monthData.expenses, color: "#ef4444", x } : null,
            showIncomes
              ? { key: "income", label: t("income"), value: monthData.incomes, color: "#22c55e", x: x + barWidth / 2 }
              : null,
          ].filter(Boolean) as Array<{ key: string; label: string; value: number; color: string; x: number }>;

          return (
            <g key={month}>
              {bars.map((bar) => {
                if (bar.value === 0) return null;
                const itemKey = `${bar.key}-bar-${monthIndex}`;
                const valueY = yScale(bar.value, mainRange);
                return (
                  <rect
                    key={itemKey}
                    data-testid={itemKey}
                    x={bar.x}
                    y={Math.min(valueY, zeroY)}
                    width={barWidth / 2}
                    height={Math.abs(zeroY - valueY)}
                    fill={bar.color}
                    opacity={tooltip && tooltip.key !== itemKey ? 0.45 : 0.85}
                    className="cursor-pointer"
                    onMouseEnter={(event) =>
                      showTooltip(event, itemKey, month, [`${bar.label}: ${formatValue(bar.value, currencyCode)}`])
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
    );
  };

  const renderCumulativeChart = () => (
    <svg width="100%" height={height} viewBox={`0 0 ${svgWidth} ${height}`} className="w-full min-w-[560px] sm:min-w-0" preserveAspectRatio="xMidYMid meet">
      {renderGrid(cumulativeRange)}
      {renderMonthLabels()}
      {cumulativeByMonth.map((month, index) => {
        if (month.monthlyNet === 0) return null;
        const slotWidth = chartWidth / Math.max(months.length, 1);
        const barWidth = Math.max(14, slotWidth * 0.34);
        const x = getX(index, months.length) - barWidth / 2;
        const zeroY = yScale(0, cumulativeRange);
        const valueY = yScale(month.monthlyNet, cumulativeRange);
        const itemKey = `cumulative-monthly-${index}`;
        return (
          <rect
            key={itemKey}
            x={x}
            y={Math.min(zeroY, valueY)}
            width={barWidth}
            height={Math.abs(zeroY - valueY)}
            rx="3"
            fill={month.monthlyNet >= 0 ? "#22c55e" : "#ef4444"}
            opacity={tooltip && tooltip.key !== itemKey ? 0.25 : 0.28}
            className="cursor-pointer"
            onMouseEnter={(event) =>
              showTooltip(event, itemKey, months[index], [
                `${t("netIncome")}: ${formatValue(month.monthlyNet, currencyCode)}`,
                `${t("cumulativeNetIncome")}: ${formatValue(month.cumulativeNet, currencyCode)}`,
              ])
            }
            onMouseMove={moveTooltip}
            onMouseLeave={clearTooltip}
          />
        );
      })}
      <polyline
        data-testid="cumulative-net-line"
        points={cumulativeByMonth
          .map((month, index) => `${getX(index, months.length)},${yScale(month.cumulativeNet, cumulativeRange)}`)
          .join(" ")}
        fill="none"
        stroke="#8b5cf6"
        strokeWidth="3"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      {cumulativeByMonth.map((month, index) => {
        const itemKey = `cumulative-${index}`;
        const color = month.cumulativeNet >= 0 ? "#22c55e" : "#ef4444";
        return (
          <g key={itemKey}>
            <circle
              data-testid={itemKey}
              cx={getX(index, months.length)}
              cy={yScale(month.cumulativeNet, cumulativeRange)}
              r={tooltip?.key === itemKey ? 6 : 4}
              fill={color}
              stroke="hsl(var(--card))"
              strokeWidth="2"
              className="cursor-pointer"
              onMouseEnter={(event) =>
                showTooltip(event, itemKey, months[index], [
                  `${t("netIncome")}: ${formatValue(month.monthlyNet, currencyCode)}`,
                  `${t("cumulativeNetIncome")}: ${formatValue(month.cumulativeNet, currencyCode)}`,
                ])
              }
              onMouseMove={moveTooltip}
              onMouseLeave={clearTooltip}
            />
            <circle
              cx={getX(index, months.length)}
              cy={yScale(month.cumulativeNet, cumulativeRange)}
              r="14"
              fill="transparent"
              className="cursor-pointer"
              onMouseEnter={(event) =>
                showTooltip(event, itemKey, months[index], [
                  `${t("netIncome")}: ${formatValue(month.monthlyNet, currencyCode)}`,
                  `${t("cumulativeNetIncome")}: ${formatValue(month.cumulativeNet, currencyCode)}`,
                ])
              }
              onMouseMove={moveTooltip}
              onMouseLeave={clearTooltip}
            />
          </g>
        );
      })}
    </svg>
  );

  const totalExpenses = expenses.reduce((sum, cat) => sum + cat.total, 0);
  const totalIncomes = incomes.reduce((sum, cat) => sum + cat.total, 0);
  const netIncome = totalIncomes - totalExpenses;

  return (
    <Card className={className}>
      <CardHeader>
        <div className="flex items-center justify-between gap-4">
          <div>
            <CardTitle className="text-base">{title}</CardTitle>
            {description && <CardDescription className="text-sm">{description}</CardDescription>}
          </div>
          <div className="flex gap-1">
            <Button size="sm" variant={chartType === "line" ? "primary" : "outline"} onClick={() => setChartType("line")} className="text-xs">
              <Icon name="trending-up" size={14} />
            </Button>
            <Button size="sm" variant={chartType === "bar" ? "primary" : "outline"} onClick={() => setChartType("bar")} className="text-xs">
              <Icon name="bar-chart" size={14} />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="relative mb-6" ref={containerRef}>
          <div className="overflow-x-auto">
            {chartType === "line" ? renderLineChart() : renderBarChart()}
          </div>
          <div className="mt-6 pt-6 border-t border-border/70">
            <div className="mb-3">
              <h4 className="text-sm font-medium text-foreground">{t("cumulativeNetIncome")}</h4>
              <p className="text-xs text-muted-foreground">{t("cumulativeNetIncomeDescription")}</p>
            </div>
            <div className="overflow-x-auto">
              {renderCumulativeChart()}
            </div>
          </div>
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

        <div className="pt-4 border-t border-border/70">
          <h4 className="text-sm font-medium text-foreground mb-3">{t("categoryTotals")}</h4>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div className="text-center">
              <div className="text-lg font-bold text-red-600">
                {formatValue(totalExpenses, currencyCode)}
              </div>
              <div className="text-xs text-muted-foreground">{t("totalExpenses")}</div>
            </div>
            <div className="text-center">
              <div className="text-lg font-bold text-green-600">
                {formatValue(totalIncomes, currencyCode)}
              </div>
              <div className="text-xs text-muted-foreground">{t("totalIncome")}</div>
            </div>
            <div className="text-center">
              <div className={`text-lg font-bold ${netIncome >= 0 ? "text-green-600" : "text-red-600"}`}>
                {formatValue(netIncome, currencyCode)}
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