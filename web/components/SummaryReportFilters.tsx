import { memo, useEffect, useState } from "react";
import { useTranslations } from "next-intl";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components";
import { formatDateLocal } from "@/lib/utils";
import { useClients } from "@/app/providers";

interface SummaryReportFiltersProps {
  from: string;
  to: string;
  onFromChange: (value: string) => void;
  onToChange: (value: string) => void;
}

const SummaryReportFilters = memo(function SummaryReportFilters({
  from,
  to,
  onFromChange,
  onToChange,
}: SummaryReportFiltersProps) {
  const t = useTranslations("reports");
  const { report } = useClients();
  
  // Get date range from backend
  const { data: dateRangeData, error: dateRangeError, isLoading: dateRangeLoading } = useQuery({
    queryKey: ["dateRange"],
    queryFn: async () => (await report.getDateRange({ 
      locale: "ru", 
      timezoneOffsetMinutes: new Date().getTimezoneOffset() 
    } as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });
  
  const [earliestDate, setEarliestDate] = useState<string>("");
  const [latestDate, setLatestDate] = useState<string>("");
  
  useEffect(() => {
    if (dateRangeData) {
      setEarliestDate(dateRangeData.earliestDate || "");
      setLatestDate(dateRangeData.latestDate || "");
    }
  }, [dateRangeData]);

  const getCurrentYearRange = () => {
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), 0, 1);
    const lastDay = new Date(now.getFullYear(), 11, 31);
    
    return {
      from: formatDateLocal(firstDay),
      to: formatDateLocal(lastDay)
    };
  };

  const getLastYearRange = () => {
    const now = new Date();
    const firstDay = new Date(now.getFullYear() - 1, 0, 1);
    const lastDay = new Date(now.getFullYear() - 1, 11, 31);
    
    return {
      from: formatDateLocal(firstDay),
      to: formatDateLocal(lastDay)
    };
  };

  const getAllTimeRange = () => {
    // Use actual date range from backend, rounded to full months
    if (earliestDate && latestDate) {
      // Round earliest date to beginning of month
      const earliest = new Date(earliestDate);
      const firstDayOfMonth = new Date(earliest.getFullYear(), earliest.getMonth(), 1);
      
      // Round latest date to end of month
      const latest = new Date(latestDate);
      const lastDayOfMonth = new Date(latest.getFullYear(), latest.getMonth() + 1, 0);
      
      return {
        from: formatDateLocal(firstDayOfMonth),
        to: formatDateLocal(lastDayOfMonth)
      };
    }
    
    // Fallback to current year if no data yet
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), 0, 1);
    const lastDay = new Date(now.getFullYear(), 11, 31);
    
    return {
      from: formatDateLocal(firstDay),
      to: formatDateLocal(lastDay)
    };
  };

  const isCurrentYearActive = () => {
    const yearRange = getCurrentYearRange();
    return from === yearRange.from && to === yearRange.to;
  };

  const isLastYearActive = () => {
    const yearRange = getLastYearRange();
    return from === yearRange.from && to === yearRange.to;
  };

  const isAllTimeActive = () => {
    const allTimeRange = getAllTimeRange();
    return from === allTimeRange.from && to === allTimeRange.to && earliestDate && latestDate;
  };

  const handleQuickFilter = (filterType: 'currentYear' | 'lastYear' | 'allTime') => {
    let range;
    switch (filterType) {
      case 'currentYear':
        range = getCurrentYearRange();
        break;
      case 'lastYear':
        range = getLastYearRange();
        break;
      case 'allTime':
        range = getAllTimeRange();
        break;
    }
    
    onFromChange(range.from);
    onToChange(range.to);
  };

  return (
    <div className="mb-6">
      <div className="flex flex-wrap items-center gap-2 mb-4">
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
          variant={isLastYearActive() ? "primary" : "outline"}
          onClick={() => handleQuickFilter('lastYear')}
          className="text-xs"
        >
          {t("lastYear")}
        </Button>
        
        <Button
          size="sm"
          variant={isAllTimeActive() ? "primary" : "outline"}
          onClick={() => handleQuickFilter('allTime')}
          className="text-xs"
        >
          {t("allTime")}
        </Button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="space-y-1">
          <label className="text-sm font-medium text-foreground">{t("from")}</label>
          <input 
            className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
            type="date" 
            value={from} 
            onChange={(e) => onFromChange(e.target.value)}
            autoComplete="off"
          />
        </div>

        <div className="space-y-1">
          <label className="text-sm font-medium text-foreground">{t("to")}</label>
          <input 
            className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
            type="date" 
            value={to} 
            onChange={(e) => onToChange(e.target.value)}
            autoComplete="off"
          />
        </div>
      </div>
    </div>
  );
});

export default SummaryReportFilters;
