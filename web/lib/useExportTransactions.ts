import { useQuery } from "@tanstack/react-query";
import { useClients } from "@/app/providers";
import { ExportTransaction } from "./exportUtils";

interface UseExportTransactionsProps {
  type: number;
  from: string;
  to: string;
  search: string;
  selectedCategoryIds: string[];
  enabled: boolean;
  locale?: string;
}

export function useExportTransactions({
  type,
  from,
  to,
  search,
  selectedCategoryIds,
  enabled,
  locale = "ru"
}: UseExportTransactionsProps) {
  const { transaction, category } = useClients();

  // Создаем запрос для получения всех транзакций (без пагинации)
  const exportRequest = {
    page: { 
      page: 1, 
      pageSize: 10000, // Большой размер страницы для получения всех данных
      sort: "occurred_at desc" 
    },
    ...(type && { type }),
    ...(from || to) && {
      dateRange: {
        ...(from && { 
          from: { seconds: Math.floor(new Date(`${from}T00:00:00`).getTime() / 1000) } 
        }),
        ...(to && { 
          to: { seconds: Math.floor(new Date(`${to}T00:00:00`).getTime() / 1000 + 86400) } 
        })
      }
    },
    ...(search && { search }),
    ...(selectedCategoryIds.length > 0 && { categoryIds: selectedCategoryIds })
  };

  const { data: transactionsData, isLoading, error } = useQuery({
    queryKey: ["exportTransactions", exportRequest],
    queryFn: async () => {
      const result = await transaction.listTransactions(exportRequest as any);
      return result;
    },
    enabled,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 300000, // 5 минут
  });

  // Загружаем категории для получения названий
  const { data: categoriesData } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => {
      const incomeResult = await category.listCategories({
        kind: 1, // INCOME
        includeInactive: false,
        locale: locale
      } as any);
      
      const expenseResult = await category.listCategories({
        kind: 2, // EXPENSE
        includeInactive: false,
        locale: locale
      } as any);
      
      return {
        categories: [
          ...(incomeResult.categories || []),
          ...(expenseResult.categories || [])
        ]
      };
    },
    enabled,
    staleTime: 5 * 60 * 1000,
  });

  // Преобразуем данные в формат для экспорта
  const exportTransactions: ExportTransaction[] = (transactionsData?.transactions || []).map((tx: any) => {
    const category = categoriesData?.categories?.find((cat: any) => cat.id === tx.categoryId);
    
    const getCategoryName = (cat: any) => {
      if (cat?.translations) {
        const translation = cat.translations.find((t: any) => t.locale === locale);
        if (translation) return translation.name;
        // Fallback to first available translation
        if (cat.translations.length > 0) return cat.translations[0].name;
      }
      return cat?.code || "";
    };

    return {
      id: tx.id,
      type: tx.type,
      occurredAt: tx.occurredAt,
      comment: tx.comment || "",
      amount: tx.amount,
      categoryId: tx.categoryId,
      categoryCode: category?.code,
      categoryName: category ? getCategoryName(category) : ""
    };
  });

  return {
    transactions: exportTransactions,
    isLoading,
    error,
    totalCount: transactionsData?.page?.totalItems || 0
  };
}
