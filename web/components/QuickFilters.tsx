import { memo } from "react";
import { useTranslations } from "next-intl";
import { Button } from "@/components";
import { formatDateLocal } from "@/lib/utils";

interface QuickFiltersProps {
  from: string;
  to: string;
  onFromChange: (value: string) => void;
  onToChange: (value: string) => void;
}

const QuickFilters = memo(function QuickFilters({
  from,
  to,
  onFromChange,
  onToChange,
}: QuickFiltersProps) {
  const t = useTranslations("transactions");

  const getCurrentMonthRange = () => {
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), now.getMonth(), 1);
    const lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0);
    
    return {
      from: formatDateLocal(firstDay),
      to: formatDateLocal(lastDay)
    };
  };

  const getLastMonthRange = () => {
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), now.getMonth() - 1, 1);
    const lastDay = new Date(now.getFullYear(), now.getMonth(), 0);
    
    return {
      from: formatDateLocal(firstDay),
      to: formatDateLocal(lastDay)
    };
  };

  const getCurrentYearRange = () => {
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), 0, 1);
    const lastDay = new Date(now.getFullYear(), 11, 31);
    
    return {
      from: formatDateLocal(firstDay),
      to: formatDateLocal(lastDay)
    };
  };

  const getLast30DaysRange = () => {
    const now = new Date();
    const thirtyDaysAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
    
    return {
      from: formatDateLocal(thirtyDaysAgo),
      to: formatDateLocal(now)
    };
  };

  const isCurrentMonthActive = () => {
    const currentRange = getCurrentMonthRange();
    return from === currentRange.from && to === currentRange.to;
  };

  const isLastMonthActive = () => {
    const lastRange = getLastMonthRange();
    return from === lastRange.from && to === lastRange.to;
  };

  const isCurrentYearActive = () => {
    const yearRange = getCurrentYearRange();
    return from === yearRange.from && to === yearRange.to;
  };

  const isLast30DaysActive = () => {
    const daysRange = getLast30DaysRange();
    return from === daysRange.from && to === daysRange.to;
  };

  const handleQuickFilter = (filterType: 'currentMonth' | 'lastMonth' | 'currentYear' | 'last30Days') => {
    let range;
    switch (filterType) {
      case 'currentMonth':
        range = getCurrentMonthRange();
        break;
      case 'lastMonth':
        range = getLastMonthRange();
        break;
      case 'currentYear':
        range = getCurrentYearRange();
        break;
      case 'last30Days':
        range = getLast30DaysRange();
        break;
    }
    
    onFromChange(range.from);
    onToChange(range.to);
  };

  return (
    <div className="mb-3">
      <div className="flex flex-wrap items-center gap-2">
        
        <Button
          size="sm"
          variant={isCurrentMonthActive() ? "primary" : "outline"}
          onClick={() => handleQuickFilter('currentMonth')}
          className="text-xs"
        >
          {t("currentMonth")}
        </Button>
        
        <Button
          size="sm"
          variant={isLastMonthActive() ? "primary" : "outline"}
          onClick={() => handleQuickFilter('lastMonth')}
          className="text-xs"
        >
          {t("lastMonth")}
        </Button>
        
        <Button
          size="sm"
          variant={isCurrentYearActive() ? "primary" : "outline"}
          onClick={() => handleQuickFilter('currentYear')}
          className="text-xs"
        >
          {t("currentYear")}
        </Button>
        
        <Button
          size="sm"
          variant={isLast30DaysActive() ? "primary" : "outline"}
          onClick={() => handleQuickFilter('last30Days')}
          className="text-xs"
        >
          {t("last30Days")}
        </Button>
      </div>
    </div>
  );
});

export default QuickFilters;
