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

  // Базовый фильтр для экспорта (страницы добавим в запросе)
  const baseExportFilter = {
    ...(type && { type }),
    ...((from || to) && {
      dateRange: {
        ...(from && {
          from: { seconds: Math.floor(new Date(`${from}T00:00:00`).getTime() / 1000) }
        }),
        ...(to && {
          to: { seconds: Math.floor(new Date(`${to}T00:00:00`).getTime() / 1000 + 86400) }
        })
      }
    }),
    ...(search && { search }),
    ...(selectedCategoryIds.length > 0 && { categoryIds: selectedCategoryIds })
  } as any;

  const { data: transactionsData, isLoading, error } = useQuery({
    queryKey: ["exportTransactions", baseExportFilter],
    queryFn: async () => {
      // Сервер ограничивает размер страницы до 500, поэтому пагинируем
      const pageSize = 500;
      let currentPage = 1;

      // Первый запрос, чтобы узнать общее количество и страницы
      const firstRequest = {
        ...baseExportFilter,
        page: { page: currentPage, pageSize, sort: "occurred_at desc" }
      };
      const firstResponse = await transaction.listTransactions(firstRequest);

      const allTransactions: any[] = [...(firstResponse.transactions || [])];
      const totalPages: number = firstResponse.page?.totalPages ?? 1;

      // Догружаем остальные страницы, если есть
      for (currentPage = 2; currentPage <= totalPages; currentPage++) {
        const req = {
          ...baseExportFilter,
          page: { page: currentPage, pageSize, sort: "occurred_at desc" }
        } as any;
        const resp = await transaction.listTransactions(req);
        if (!resp.transactions || resp.transactions.length === 0) {
          break;
        }
        allTransactions.push(...resp.transactions);
      }

      return { transactions: allTransactions, page: firstResponse.page } as any;
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
