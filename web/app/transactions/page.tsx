"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { TransactionType, CategoryKind } from "@/proto/budget/v1/common_pb";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { Icon, Button, Card, CardContent, CardHeader, CardTitle, TransactionStats, CategoryBadge, CategoryTagInput } from "@/components";
import { formatCurrency } from "@/lib/utils";

function TransactionsInner() {
  const { transaction, category } = useClients();
  const t = useTranslations("transactions");
  const tc = useTranslations("common");
  const qc = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [type, setType] = useState<number>(0);
  const [from, setFrom] = useState<string>("");
  const [to, setTo] = useState<string>("");
  const [search, setSearch] = useState<string>("");
  const [selectedCategoryIds, setSelectedCategoryIds] = useState<string[]>([]);
  const [showFilters, setShowFilters] = useState(true);

  const request = useMemo(() => {
    const req: any = { page: { page, pageSize } };
    if (type) req.type = type;
    if (from || to) {
      req.dateRange = {};
      if (from) req.dateRange.from = { seconds: Math.floor(new Date(from).getTime() / 1000) };
      if (to) req.dateRange.to = { seconds: Math.floor(new Date(to).getTime() / 1000) };
    }
    if (search) req.search = search;
    if (selectedCategoryIds.length) req.categoryIds = selectedCategoryIds;
    return req;
  }, [page, pageSize, type, from, to, search, selectedCategoryIds]);

  const { data, isLoading, error } = useQuery({
    queryKey: ["transactions", request],
    queryFn: async () => (await transaction.listTransactions(request as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  // Загружаем категории для фильтров и отображения
  const { data: categoriesData, isLoading: categoriesLoading, error: categoriesError } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => {
      console.log('Fetching categories...');
      try {
        // Загружаем категории доходов
        const incomeResult = await category.listCategories({
          kind: CategoryKind.INCOME,
          includeInactive: false,
          locale: "ru"
        } as any);
        
        // Загружаем категории расходов
        const expenseResult = await category.listCategories({
          kind: CategoryKind.EXPENSE,
          includeInactive: false,
          locale: "ru"
        } as any);
        
        // Объединяем результаты
        const combinedResult = {
          categories: [
            ...(incomeResult.categories || []),
            ...(expenseResult.categories || [])
          ]
        };
        
        console.log('Categories result:', combinedResult);
        return combinedResult;
      } catch (error) {
        console.error('Error fetching categories:', error);
        throw error;
      }
    },
    staleTime: 5 * 60 * 1000, // 5 минут
  });

  const deleteMut = useMutation({
    mutationFn: async (id: string) => await transaction.deleteTransaction({ id } as any),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["transactions"] }),
  });

  const clearFilters = () => {
    setType(0);
    setFrom("");
    setTo("");
    setSearch("");
    setSelectedCategoryIds([]);
    setPage(1);
  };

  const hasActiveFilters = type || from || to || search || selectedCategoryIds.length > 0;

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-50 to-blue-50 dark:from-slate-900 dark:to-slate-800">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-6">
        {/* Header */}
        <div className="mb-6">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
            <div>
              <h1 className="text-3xl font-bold text-slate-900 dark:text-white mb-1">
                {t("title")}
              </h1>
              <p className="text-slate-600 dark:text-slate-300">
                {t("description")}
              </p>
            </div>
            <Link 
              href="/transactions/new" 
              className="inline-flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white font-semibold rounded-lg shadow-lg hover:shadow-xl transition-all duration-200 transform hover:scale-105"
            >
              <Icon name="plus" size={16} className="mr-2" />
              {t("create")}
            </Link>
          </div>
        </div>

        {/* Statistics */}
        {!isLoading && !error && data?.transactions && data.transactions.length > 0 && (
          <div className="mb-4">
            <TransactionStats
              totalIncome={data.transactions
                .filter((tx: any) => tx.type === TransactionType.INCOME)
                .reduce((sum: number, tx: any) => sum + Number(tx.amount?.minorUnits || 0), 0)}
              totalExpenses={data.transactions
                .filter((tx: any) => tx.type === TransactionType.EXPENSE)
                .reduce((sum: number, tx: any) => sum + Number(tx.amount?.minorUnits || 0), 0)}
              currencyCode={data.transactions[0]?.amount?.currencyCode || "RUB"}
              period="за текущий период"
            />
          </div>
        )}

        {/* Filters */}
        <Card className="mb-4 shadow-lg border-0 bg-white/90 dark:bg-slate-800/90 backdrop-blur-sm">
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <CardTitle className="text-lg flex items-center space-x-2">
                <Icon name="filter" size={18} className="text-blue-600 dark:text-blue-400" />
                <span>Фильтры</span>
                {hasActiveFilters && (
                  <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300">
                    Активно
                  </span>
                )}
              </CardTitle>
              <div className="flex items-center space-x-2">
                {hasActiveFilters && (
                  <Button
                    size="sm"
                    variant="outline"
                    icon="trash"
                    onClick={clearFilters}
                    className="text-red-600 border-red-200 hover:bg-red-50 dark:border-red-800 dark:hover:bg-red-900/20"
                  >
                    Очистить
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setShowFilters(!showFilters)}
                  className="flex items-center space-x-1"
                >
                  <Icon name={showFilters ? "chevron-up" : "chevron-down"} size={14} />
                  <span>{showFilters ? "Скрыть" : "Показать"}</span>
                </Button>
              </div>
            </div>
          </CardHeader>
          
          {showFilters && (
            <CardContent className="pt-0">
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-3">
                <div className="space-y-1">
                  <label className="text-xs font-medium text-slate-700 dark:text-slate-300">
                    Тип
                  </label>
                  <select 
                    className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
                    value={String(type)} 
                    onChange={(e) => setType(Number(e.target.value))}
                  >
                    <option value={0}>Все типы</option>
                    <option value={TransactionType.EXPENSE}>Расход</option>
                    <option value={TransactionType.INCOME}>Доход</option>
                  </select>
                </div>

                <div className="space-y-1">
                  <label className="text-xs font-medium text-slate-700 dark:text-slate-300">
                    От
                  </label>
                  <input 
                    className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
                    type="date" 
                    value={from} 
                    onChange={(e) => setFrom(e.target.value)}
                    autoComplete="off"
                  />
                </div>

                <div className="space-y-1">
                  <label className="text-xs font-medium text-slate-700 dark:text-slate-300">
                    До
                  </label>
                  <input 
                    className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
                    type="date" 
                    value={to} 
                    onChange={(e) => setTo(e.target.value)}
                    autoComplete="off"
                  />
                </div>

                <div className="space-y-1">
                  <label className="text-xs font-medium text-slate-700 dark:text-slate-300">
                    Поиск
                  </label>
                  <div className="relative">
                    <Icon 
                      name="search" 
                      size={14} 
                      className="absolute left-2.5 top-1/2 transform -translate-y-1/2 text-slate-400" 
                    />
                    <input 
                      className="w-full pl-8 pr-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-sm" 
                      value={search} 
                      onChange={(e) => setSearch(e.target.value)}
                      placeholder="Поиск..."
                      autoComplete="off"
                    />
                  </div>
                </div>

                <div className="space-y-1">
                  <label className="text-xs font-medium text-slate-700 dark:text-slate-300">
                    Категории
                  </label>
                  {categoriesLoading ? (
                    <div className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-500 dark:text-slate-400 text-sm">
                      Загрузка категорий...
                    </div>
                  ) : (
                    <CategoryTagInput
                      categories={categoriesData?.categories || []}
                      selectedIds={selectedCategoryIds}
                      onSelectionChange={setSelectedCategoryIds}
                      placeholder="Введите код или название категории..."
                    />
                  )}
                </div>
              </div>
            </CardContent>
          )}
        </Card>

        {/* Content */}
        {isLoading && (
          <div className="flex items-center justify-center py-12">
            <div className="flex flex-col items-center space-y-3">
              <div className="w-8 h-8 border-3 border-blue-200 border-t-blue-600 rounded-full animate-spin"></div>
              <span className="text-slate-600 dark:text-slate-300 font-medium">{tc("loading")}</span>
            </div>
          </div>
        )}

        {error && (
          <Card className="border-red-200 bg-red-50 dark:border-red-800 dark:bg-red-900/20">
            <CardContent className="pt-4">
              <div className="flex items-center space-x-2 text-red-700 dark:text-red-300">
                <Icon name="alert-circle" size={16} />
                <span className="font-medium">{(error as any).message}</span>
              </div>
            </CardContent>
          </Card>
        )}

        {!isLoading && !error && (
          <>
            <TransactionTable
              items={data?.transactions ?? []}
              categories={categoriesData?.categories ?? []}
              categoriesLoading={categoriesLoading}
              onDelete={(id: string) => deleteMut.mutate(id)}
              isDeleting={deleteMut.isPending}
              onChanged={() => qc.invalidateQueries({ queryKey: ["transactions"] })}
            />

            {/* Pagination */}
            {data?.page && (
              <div className="flex items-center justify-between mt-4 p-4 bg-white dark:bg-slate-800 rounded-lg shadow-lg">
                <div className="text-sm text-slate-600 dark:text-slate-300">
                  Показано {((page - 1) * pageSize) + 1} - {Math.min(page * pageSize, Number(data.page.totalItems))} из {Number(data.page.totalItems)} транзакций
                </div>
                <div className="flex items-center space-x-2">
                  <Button
                    size="sm"
                    variant="outline"
                    icon="chevron-left"
                    disabled={page <= 1}
                    onClick={() => setPage((p) => Math.max(1, p - 1))}
                    className="px-3 py-1"
                  >
                    Предыдущий
                  </Button>
                  <div className="px-3 py-1 text-sm font-medium bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-300 rounded">
                    {page} / {Number(data.page.totalPages)}
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    icon="chevron-right"
                    iconPosition="right"
                    disabled={page >= Number(data.page.totalPages)}
                    onClick={() => setPage((p) => p + 1)}
                    className="px-3 py-1"
                  >
                    Следующий
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export default function TransactionsPage() {
  return (
    <ClientsProvider>
      <TransactionsInner />
    </ClientsProvider>
  );
}

function TransactionTable({
  items,
  categories,
  categoriesLoading,
  onDelete,
  isDeleting,
  onChanged,
}: {
  items: any[];
  categories: any[];
  categoriesLoading: boolean;
  onDelete: (id: string) => void;
  isDeleting: boolean;
  onChanged: () => void;
}) {
  const { transaction } = useClients();
  const tt = useTranslations("transactions");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editComment, setEditComment] = useState<string>("");
  const [editAmount, setEditAmount] = useState<string>("");
  const [editCategoryId, setEditCategoryId] = useState<string>("");
  const [editDate, setEditDate] = useState<string>("");

  const updateMut = useMutation({
    mutationFn: async (payload: { 
      id: string; 
      comment?: string; 
      amountMinorUnits?: number;
      categoryId?: string;
      occurredAt?: { seconds: number };
    }) => {
      const paths: string[] = [];
      const tx: any = {};
      if (payload.comment !== undefined) {
        paths.push("comment");
        tx.comment = payload.comment;
      }
      if (payload.amountMinorUnits !== undefined) {
        paths.push("amount");
        // Находим оригинальную транзакцию для получения currencyCode
        const originalTx = items.find(t => t.id === payload.id);
        tx.amount = { 
          minorUnits: payload.amountMinorUnits,
          currencyCode: originalTx?.amount?.currencyCode || "RUB"
        };
      }
      if (payload.categoryId !== undefined) {
        paths.push("category_id");
        if (payload.categoryId !== null) {
          // Проверяем, что категория существует в списке
          const categoryExists = categories.some(cat => cat.id === payload.categoryId);
          console.log('Category exists:', categoryExists, 'Category ID:', payload.categoryId);
          console.log('Available categories:', categories.map(c => ({ id: c.id, code: c.code, kind: c.kind })));
          
          if (categoryExists) {
            tx.categoryId = payload.categoryId;
            console.log('Updating categoryId:', payload.categoryId);
          } else {
            console.error('Category not found:', payload.categoryId);
            console.error('This might be because the category is not available for this transaction type');
            throw new Error(`Категория с ID ${payload.categoryId} не найдена или недоступна для данного типа транзакции`);
          }
        } else {
          tx.categoryId = null;
          console.log('Removing categoryId');
        }
      }
      if (payload.occurredAt !== undefined) {
        paths.push("occurred_at");
        tx.occurredAt = payload.occurredAt;
      }
              console.log('Update request:', { 
          id: payload.id, 
          transaction: tx, 
          updateMask: { paths },
          payload: payload
        });
      return transaction.updateTransaction({
        id: payload.id,
        transaction: tx,
        updateMask: { paths },
      } as any);
    },
    onSuccess: () => {
      setEditingId(null);
      onChanged();
    },
    onError: (error) => {
      console.error('Error updating transaction:', error);
      // Сбрасываем состояние редактирования при ошибке
      setEditingId(null);
      // Можно добавить уведомление об ошибке
      alert('Ошибка при обновлении транзакции: ' + (error as any).message);
    },
  });

  // Отладочная информация
  console.log('Categories loaded:', categories.length);
  console.log('Categories loading:', categoriesLoading);
  console.log('Sample category:', categories[0]);
  console.log('Sample transaction:', items[0]);

  if (items.length === 0) {
    return (
      <Card className="text-center py-12 bg-white/90 dark:bg-slate-800/90 backdrop-blur-sm">
        <CardContent>
          <div className="inline-flex items-center justify-center w-16 h-16 bg-slate-100 dark:bg-slate-700 rounded-full mb-4">
            <Icon name="receipt" size={32} className="text-slate-400 dark:text-slate-500" />
          </div>
          <h3 className="text-xl font-bold text-slate-900 dark:text-white mb-2">Нет транзакций</h3>
          <p className="text-slate-600 dark:text-slate-300">Создайте первую транзакцию для начала работы</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="shadow-lg border-0 bg-white/90 dark:bg-slate-800/90 backdrop-blur-sm overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead className="bg-slate-50 dark:bg-slate-700/50">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-600 dark:text-slate-300 uppercase tracking-wider">
                Тип
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-600 dark:text-slate-300 uppercase tracking-wider">
                Дата
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-600 dark:text-slate-300 uppercase tracking-wider">
                Описание
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-slate-600 dark:text-slate-300 uppercase tracking-wider">
                Категория
              </th>
              <th className="px-4 py-3 text-right text-xs font-medium text-slate-600 dark:text-slate-300 uppercase tracking-wider">
                Сумма
              </th>
              <th className="px-4 py-3 text-center text-xs font-medium text-slate-600 dark:text-slate-300 uppercase tracking-wider">
                Действия
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-200 dark:divide-slate-700">
            {items.map((tx: any) => (
              <tr key={tx?.id} className="hover:bg-slate-50 dark:hover:bg-slate-700/30 transition-colors">
                <td className="px-4 py-3">
                  <div className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
                    tx?.type === TransactionType.EXPENSE 
                      ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300' 
                      : 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
                  }`}>
                    <Icon 
                      name={tx?.type === TransactionType.EXPENSE ? "trending-down" : "trending-up"} 
                      size={12} 
                      className="mr-1" 
                    />
                    {tx?.type === TransactionType.EXPENSE ? 'Расход' : 'Доход'}
                  </div>
                </td>
                                 <td className="px-4 py-3">
                   {editingId === tx?.id ? (
                     <input
                       type="datetime-local"
                       className="w-full px-2 py-1 border border-slate-200 dark:border-slate-600 rounded bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent text-sm"
                       value={editDate}
                       onChange={(e) => setEditDate(e.target.value)}
                       autoComplete="off"
                     />
                   ) : (
                     <div className="text-sm text-slate-900 dark:text-white">
                       {tx?.occurredAt?.seconds ? new Date(Number(tx.occurredAt.seconds) * 1000).toLocaleDateString() : ""}
                     </div>
                   )}
                 </td>
                <td className="px-4 py-3">
                  {editingId === tx?.id ? (
                    <input
                      className="w-full px-2 py-1 border border-slate-200 dark:border-slate-600 rounded bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent text-sm"
                      placeholder="Описание"
                      value={editComment}
                      onChange={(e) => setEditComment(e.target.value)}
                      autoComplete="off"
                    />
                  ) : (
                    <div className="text-sm text-slate-900 dark:text-white">
                      {tx?.comment || "Без описания"}
                    </div>
                  )}
                </td>
                                 <td className="px-4 py-3">
                   {editingId === tx?.id ? (
                     categoriesLoading ? (
                       <div className="w-full px-2 py-1 border border-slate-200 dark:border-slate-600 rounded bg-white dark:bg-slate-700 text-slate-500 dark:text-slate-400 text-sm">
                         Загрузка категорий...
                       </div>
                     ) : (
                       <select
                         className="w-full px-2 py-1 border border-slate-200 dark:border-slate-600 rounded bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent text-sm"
                         value={editCategoryId}
                         onChange={(e) => setEditCategoryId(e.target.value)}
                       >
                         <option value="">Без категории</option>
                         {categories
                           .filter((cat: any) => {
                             // Фильтруем категории по типу транзакции
                             const isExpense = tx?.type === TransactionType.EXPENSE;
                             const isIncome = tx?.type === TransactionType.INCOME;
                             
                             if (isExpense) {
                               return cat.kind === CategoryKind.EXPENSE;
                             } else if (isIncome) {
                               return cat.kind === CategoryKind.INCOME;
                             }
                             return true; // Если тип не определен, показываем все
                           })
                           .map((cat: any) => {
                             const getCategoryName = (cat: any) => {
                               if (cat.translations) {
                                 const ruTranslation = cat.translations.find((t: any) => t.locale === 'ru');
                                 if (ruTranslation) return ruTranslation.name;
                                 const enTranslation = cat.translations.find((t: any) => t.locale === 'en');
                                 if (enTranslation) return enTranslation.name;
                                 if (cat.translations.length > 0) return cat.translations[0].name;
                               }
                               return cat.code;
                             };
                             return (
                               <option key={cat.id} value={cat.id}>
                                 {getCategoryName(cat)}
                               </option>
                             );
                           })}
                       </select>
                     )
                   ) : (
                     <CategoryBadge
                       categoryId={tx?.categoryId}
                       categoryCode={categories.find(c => c.id === tx?.categoryId)?.code}
                       categoryTranslations={categories.find(c => c.id === tx?.categoryId)?.translations}
                       type={tx?.type === TransactionType.EXPENSE ? 'expense' : 'income'}
                     />
                   )}
                 </td>
                <td className="px-4 py-3 text-right">
                  {editingId === tx?.id ? (
                    <input
                      className="w-24 px-2 py-1 border border-slate-200 dark:border-slate-600 rounded bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent text-sm text-right"
                      placeholder="Сумма"
                      type="number"
                      step="0.01"
                      value={editAmount}
                                             onChange={(e) => setEditAmount(e.target.value ? parseFloat(e.target.value) * 100 : "")}
                      autoComplete="off"
                    />
                  ) : (
                                         <div className={`text-sm font-semibold ${
                       tx?.type === TransactionType.EXPENSE ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'
                     }`}>
                       {tx?.type === TransactionType.EXPENSE ? '-' : '+'}
                       {formatCurrency(Number(tx?.amount?.minorUnits ?? 0), tx?.amount?.currencyCode)}
                     </div>
                  )}
                </td>
                <td className="px-4 py-3 text-center">
                  {editingId === tx?.id ? (
                    <div className="flex items-center justify-center space-x-1">
                      <Button
                        size="sm"
                        loading={updateMut.isPending}
                        icon="check"
                        onClick={() => {
                          console.log('Sending update with values:', {
                            editComment,
                            editAmount,
                            editCategoryId,
                            editDate,
                            categoryId: editCategoryId === "" ? null : editCategoryId,
                            occurredAt: editDate ? { seconds: Math.floor(new Date(editDate).getTime() / 1000) } : undefined,
                          });
                          updateMut.mutate({
                            id: tx.id as string,
                            comment: editComment,
                            amountMinorUnits: editAmount ? parseFloat(editAmount) * 100 : undefined,
                            categoryId: editCategoryId === "" ? null : editCategoryId,
                            occurredAt: editDate ? { seconds: Math.floor(new Date(editDate).getTime() / 1000) } : undefined,
                          });
                        }}
                        className="bg-green-600 hover:bg-green-700 text-white px-2 py-1"
                      >
                        {updateMut.isPending ? "Сохранение" : "Сохранить"}
                      </Button>
                      <Button 
                        size="sm"
                        variant="outline"
                        onClick={() => setEditingId(null)}
                        className="text-slate-600 dark:text-slate-300 px-2 py-1"
                      >
                        Отмена
                      </Button>
                    </div>
                  ) : (
                    <div className="flex items-center justify-center space-x-1">
                      <Button
                        size="sm"
                        variant="outline"
                        icon="edit"
                        onClick={() => {
                          setEditingId(tx?.id);
                          setEditComment(tx?.comment ?? "");
                          setEditAmount(tx?.amount?.minorUnits ? (Number(tx.amount.minorUnits) / 100).toString() : "");
                          setEditCategoryId(tx?.categoryId ?? "");
                          setEditDate(tx?.occurredAt?.seconds ? new Date(Number(tx.occurredAt.seconds) * 1000).toISOString().slice(0, 16) : "");
                        }}
                        className="text-blue-600 border-blue-200 hover:bg-blue-50 dark:border-blue-800 dark:hover:bg-blue-900/20 px-2 py-1"
                      >
                        Изменить
                      </Button>
                      <Button
                        size="sm"
                        variant="destructive"
                        icon="trash"
                        loading={isDeleting}
                        onClick={() => onDelete(tx?.id)}
                        className="bg-red-600 hover:bg-red-700 text-white px-2 py-1"
                      >
                        Удалить
                      </Button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

