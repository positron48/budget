import { memo } from "react";
import { useTranslations } from "next-intl";
import { TransactionType } from "@/proto/budget/v1/common_pb";
import { Icon, CategoryTagInput } from "@/components";

interface FiltersFormProps {
  type: number;
  from: string;
  to: string;
  search: string;
  selectedCategoryIds: string[];
  categoriesLoading: boolean;
  categoriesData: any;
  onTypeChange: (value: number) => void;
  onFromChange: (value: string) => void;
  onToChange: (value: string) => void;
  onSearchChange: (value: string) => void;
  onCategoryIdsChange: (value: string[]) => void;
}

const FiltersForm = memo(function FiltersForm({
  type,
  from,
  to,
  search,
  selectedCategoryIds,
  categoriesLoading,
  categoriesData,
  onTypeChange,
  onFromChange,
  onToChange,
  onSearchChange,
  onCategoryIdsChange,
}: FiltersFormProps) {
  const t = useTranslations("transactions");
  return (
    <div className="bg-slate-50 dark:bg-slate-700/50 px-4 py-3">
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-3">
        <div className="space-y-1">
          <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("type")}</label>
          <select 
            className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
            value={String(type)} 
            onChange={(e) => onTypeChange(Number(e.target.value))}
          >
            <option value={0}>{t("allTypes")}</option>
            <option value={TransactionType.EXPENSE}>{t("expense")}</option>
            <option value={TransactionType.INCOME}>{t("income")}</option>
          </select>
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("from")}</label>
          <input 
            className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
            type="date" 
            value={from} 
            onChange={(e) => onFromChange(e.target.value)}
            autoComplete="off"
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("to")}</label>
          <input 
            className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
            type="date" 
            value={to} 
            onChange={(e) => onToChange(e.target.value)}
            autoComplete="off"
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("search")}</label>
          <div className="relative">
            <Icon 
              name="search" 
              size={14} 
              className="absolute left-2.5 top-1/2 transform -translate-y-1/2 text-slate-400" 
            />
            <input 
              className="w-full pl-8 pr-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
              value={search} 
              onChange={(e) => onSearchChange(e.target.value)}
              placeholder={t("searchPlaceholder") as string}
              autoComplete="off"
            />
          </div>
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("category")}</label>
          {categoriesLoading ? (
            <div className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-500 dark:text-slate-400 text-sm">{t("loadingCategories")}</div>
          ) : (
            <CategoryTagInput
              categories={categoriesData?.categories || []}
              selectedIds={selectedCategoryIds}
              onSelectionChange={onCategoryIdsChange}
              placeholder={t("categoryIdsPlaceholder") as string}
            />
          )}
        </div>
      </div>
    </div>
  );
});

export default FiltersForm;
