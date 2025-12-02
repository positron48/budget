import { memo, useState, useMemo } from "react";
import { useTranslations } from "next-intl";
import { Card, CardContent, CardHeader, CardTitle, CardDescription, Button } from "@/components";
import { formatAmountWithSpaces } from "@/lib/utils";

interface CategoryData {
  id: string;
  name: string;
  color: string;
  values: number[];
  total: number;
}

interface CategoryToggleChartProps {
  title: string;
  description?: string;
  categories: CategoryData[];
  months: string[];
  currencyCode: string;
  className?: string;
}

const CategoryToggleChart = memo(function CategoryToggleChart({
  title,
  description,
  categories,
  months,
  currencyCode,
  className,
}: CategoryToggleChartProps) {
  const t = useTranslations("reports");
  const [visibleCategories, setVisibleCategories] = useState<Set<string>>(
    new Set(categories.map(cat => cat.id))
  );

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
    categories.filter(cat => visibleCategories.has(cat.id)),
    [categories, visibleCategories]
  );

  const totalByMonth = useMemo(() => {
    return months.map((_, monthIndex) => 
      visibleCategoriesData.reduce((sum, cat) => sum + (cat.values[monthIndex] || 0), 0)
    );
  }, [visibleCategoriesData, months]);

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
        {description && <CardDescription className="text-sm">{description}</CardDescription>}
      </CardHeader>
      <CardContent>
        {/* Category Toggles */}
        <div className="mb-6">
          <h4 className="text-sm font-medium text-foreground mb-3">{t("toggleCategories")}</h4>
          <div className="flex flex-wrap gap-2">
            {categories.map((category) => {
              const isActive = visibleCategories.has(category.id);
              return (
                <Button
                  key={category.id}
                  size="sm"
                  variant="outline"
                  onClick={() => toggleCategory(category.id)}
                  className={`text-xs flex items-center gap-2 rounded-none border ${
                    isActive ? "!text-foreground" : "!text-muted-foreground !border-border bg-secondary/60"
                  }`}
                  style={
                    isActive
                      ? {
                          backgroundColor: `${category.color}22`,
                          borderColor: `${category.color}66`,
                          color: category.color,
                        }
                      : undefined
                  }
                >
                  <div
                    className="w-3 h-3 rounded-full"
                    style={{ backgroundColor: category.color }}
                  />
                  {category.name}
                </Button>
              );
            })}
          </div>
        </div>

        {/* Chart */}
        <div className="space-y-4">
        {months.map((month, monthIndex) => {
          const monthTotal = totalByMonth[monthIndex];
            
            return (
              <div key={month} className="space-y-2">
                <div className="flex justify-between items-center">
                  <span className="text-sm font-medium text-foreground">{month}</span>
                  <span className="text-sm text-muted-foreground">
                    {formatAmountWithSpaces(monthTotal)} {currencyCode}
                  </span>
                </div>
                
                {/* Stacked Bar */}
                <div className="relative h-6 bg-secondary/40 border border-border rounded-none overflow-hidden">
                  {visibleCategoriesData.map((category, catIndex) => {
                    const categoryValue = category.values[monthIndex] || 0;
                    const categoryPercentage = monthTotal > 0 ? (categoryValue / monthTotal) * 100 : 0;
                    const leftOffset = visibleCategoriesData
                      .slice(0, catIndex)
                      .reduce((sum, cat) => sum + ((cat.values[monthIndex] || 0) / monthTotal) * 100, 0);
                    
                    return (
                      <div
                        key={category.id}
                        className="absolute top-0 h-full transition-all duration-200"
                        style={{
                          left: `${leftOffset}%`,
                          width: `${categoryPercentage}%`,
                          backgroundColor: category.color,
                        }}
                        title={`${category.name}: ${formatAmountWithSpaces(categoryValue)} ${currencyCode}`}
                      />
                    );
                  })}
                </div>
              </div>
            );
          })}
        </div>

        {/* Legend */}
        <div className="mt-6 pt-4 border-t border-border">
          <h4 className="text-sm font-medium text-foreground mb-3">{t("categoryTotals")}</h4>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
            {visibleCategoriesData.map((category) => (
              <div
                key={category.id}
                className={`flex items-center justify-between text-sm px-3 py-2 border border-border/60 rounded-none ${
                  visibleCategories.has(category.id) ? "bg-card/70" : "bg-secondary/50 text-muted-foreground"
                }`}
              >
                <div className="flex items-center gap-2">
                  <div className="w-3 h-3 rounded-full" style={{ backgroundColor: category.color }} />
                  <span className="text-foreground">{category.name}</span>
                </div>
                <span className="text-muted-foreground font-medium">
                  {formatAmountWithSpaces(category.total)} {currencyCode}
                </span>
              </div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  );
});

export default CategoryToggleChart;
