import { memo, useMemo, useRef } from "react";
import { useTranslations } from "next-intl";
import { TransactionType } from "@/proto/budget/v1/common_pb";
import { Icon, CategoryTagInput, Select, Input } from "@/components";

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
  const inputClass = "input text-sm rounded-none";

  const typeOptions = useMemo(
    () => [
      { value: "0", label: t("allTypes") },
      { value: String(TransactionType.EXPENSE), label: t("expense") },
      { value: String(TransactionType.INCOME), label: t("income") },
    ],
    [t]
  );

  const DateInputField = ({
    label,
    value,
    onChange,
  }: {
    label: string;
    value: string;
    onChange: (val: string) => void;
  }) => {
    const inputRef = useRef<HTMLInputElement>(null);
    return (
      <div className="space-y-1">
        <label className="text-xs font-medium text-muted-foreground">{label}</label>
        <div className="relative">
          <input
            ref={inputRef}
            className={`${inputClass} w-full pr-12 date-input`}
            type="date"
            value={value}
            onChange={(e) => onChange(e.target.value)}
            autoComplete="off"
          />
          <button
            type="button"
            onClick={() => {
              if (!inputRef.current) return;
              if (typeof inputRef.current.showPicker === "function") {
                inputRef.current.showPicker();
              } else {
                inputRef.current.focus();
              }
            }}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
          >
            <Icon name="calendar" size={16} />
          </button>
        </div>
      </div>
    );
  };
  return (
    <div className="rounded-none border border-border bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60 px-4 py-4 space-y-3">
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-3">
        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">{t("type")}</label>
          <Select
            value={String(type)}
            onChange={(value) => onTypeChange(Number(value))}
            options={typeOptions}
            className="w-full"
          />
        </div>

        <DateInputField label={t("from")} value={from} onChange={onFromChange} />

        <DateInputField label={t("to")} value={to} onChange={onToChange} />

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">{t("search")}</label>
          <Input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={t("searchPlaceholder") as string}
            rightIcon="search"
            className="w-full"
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">{t("category")}</label>
          {categoriesLoading ? (
            <div className={`${inputClass} w-full text-muted-foreground`}>{t("loadingCategories")}</div>
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
