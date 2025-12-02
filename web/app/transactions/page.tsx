"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState, useCallback, useRef } from "react";
import { TransactionType, CategoryKind } from "@/proto/budget/v1/common_pb";
import { useTranslations } from "next-intl";
import { Icon, Button, Card, CardContent, TransactionStats, CategoryBadge, Modal, SortableHeader, ExportButton, QuickFilters } from "@/components";
import { formatDateLocal } from "@/lib/utils";
import ImportWizard from "./ImportWizard";
import NewTransactionForm, { NewTxFormRef } from "./NewTransactionForm";
import FiltersForm from "@/components/FiltersForm";
import { formatCurrency } from "@/lib/utils";

const SURFACE_CARD = "rounded-none border border-border bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60";
const PANEL_CARD = "rounded-none border border-border bg-secondary/40";

function TransactionsInner() {
  const { transaction, category } = useClients();
  const t = useTranslations("transactions");
  const tc = useTranslations("common");
  const qc = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [type, setType] = useState<number>(0);
  // Устанавливаем фильтр по умолчанию на текущий месяц
  const getCurrentMonthRange = () => {
    const now = new Date();
    const firstDay = new Date(now.getFullYear(), now.getMonth(), 1);
    const lastDay = new Date(now.getFullYear(), now.getMonth() + 1, 0);
    
    return {
      from: formatDateLocal(firstDay),
      to: formatDateLocal(lastDay)
    };
  };

  const defaultRange = getCurrentMonthRange();
  const [from, setFrom] = useState<string>(defaultRange.from);
  const [to, setTo] = useState<string>(defaultRange.to);
  const [search, setSearch] = useState<string>("");
  const [selectedCategoryIds, setSelectedCategoryIds] = useState<string[]>([]);
  const [showFilters, setShowFilters] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const [sort, setSort] = useState<string>("occurred_at desc"); // Default sort by date descending
  const formRef = useRef<NewTxFormRef>(null);

  const setTypeCallback = useCallback((value: number) => setType(value), []);
  const setFromCallback = useCallback((value: string) => setFrom(value), []);
  const setToCallback = useCallback((value: string) => setTo(value), []);
  const setSearchCallback = useCallback((value: string) => {
    setSearch(value);
    // Добавляем небольшую задержку для debounce
    setTimeout(() => {
      // Принудительно фокусируем инпут после обновления
      const searchInput = document.querySelector('input[placeholder="Поиск..."]') as HTMLInputElement;
      if (searchInput && document.activeElement === searchInput) {
        searchInput.focus();
      }
    }, 100);
  }, []);
  const setSelectedCategoryIdsCallback = useCallback((value: string[]) => setSelectedCategoryIds(value), []);

  const handleSort = useCallback((field: string, direction: "asc" | "desc" | null) => {
    if (direction === null) {
      setSort("occurred_at desc"); // Reset to default
    } else {
      setSort(`${field} ${direction}`);
    }
    setPage(1); // Reset to first page when sorting changes
  }, []);
  


  const request = useMemo(() => {
    const req: any = { page: { page, pageSize, sort } };
    if (type) req.type = type;
    if (from || to) {
      req.dateRange = {};
      if (from) {
        const f = new Date(`${from}T00:00:00`); // local midnight
        req.dateRange.from = { seconds: Math.floor(f.getTime() / 1000) };
      }
      if (to) {
        const tdate = new Date(`${to}T00:00:00`); // local midnight
        tdate.setDate(tdate.getDate() + 1); // upper bound exclusive: next local midnight
        req.dateRange.to = { seconds: Math.floor(tdate.getTime() / 1000) };
      }
    }
    if (search) req.search = search;
    if (selectedCategoryIds.length) req.categoryIds = selectedCategoryIds;
    return req;
  }, [page, pageSize, sort, type, from, to, search, selectedCategoryIds]);

  const { data, isLoading, error } = useQuery({
    queryKey: ["transactions", request],
    queryFn: async () => (await transaction.listTransactions(request as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 300000, // 5 минут
    refetchInterval: false,
    refetchOnMount: false,
    gcTime: 600000, // 10 минут
    placeholderData: (previousData) => previousData,
  });

  // Totals for full filtered period via RPC (ignores pagination)
  const totalsRequest = useMemo(() => {
    const req: any = {};
    if (type) req.type = type;
    if (from || to) {
      req.dateRange = {};
      if (from) {
        const f = new Date(`${from}T00:00:00`);
        req.dateRange.from = { seconds: Math.floor(f.getTime() / 1000) };
      }
      if (to) {
        const tdate = new Date(`${to}T00:00:00`);
        tdate.setDate(tdate.getDate() + 1);
        req.dateRange.to = { seconds: Math.floor(tdate.getTime() / 1000) };
      }
    }
    if (search) req.search = search;
    if (selectedCategoryIds.length) req.categoryIds = selectedCategoryIds;
    return req;
  }, [type, from, to, search, selectedCategoryIds]);

  const { data: totalsData, isLoading: totalsLoading, error: totalsError } = useQuery({
    queryKey: ["transactionsTotals", totalsRequest],
    queryFn: async () => {
      const resp = await transaction.getTransactionsTotals(totalsRequest as any);
      return {
        totalIncome: Number(resp?.totalIncome?.minorUnits || 0),
        totalExpenses: Number(resp?.totalExpense?.minorUnits || 0),
        currencyCode: resp?.totalIncome?.currencyCode || resp?.totalExpense?.currencyCode || "RUB",
      };
    },
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 300000,
  });

  // Загружаем категории для фильтров и отображения
  const { data: categoriesData, isLoading: categoriesLoading } = useQuery({
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

  const refetchListAndTotals = useCallback(() => {
    qc.invalidateQueries({ queryKey: ["transactions"] });
    qc.invalidateQueries({ queryKey: ["transactionsTotals"] });
  }, [qc]);

  const deleteMut = useMutation({
    mutationFn: async (id: string) => await transaction.deleteTransaction({ id } as any),
    onSuccess: () => refetchListAndTotals(),
  });

  const clearFilters = useCallback(() => {
    setType(0);
    const defaultRange = getCurrentMonthRange();
    setFrom(defaultRange.from);
    setTo(defaultRange.to);
    setSearch("");
    setSelectedCategoryIds([]);
    setSort("occurred_at desc");
    setPage(1);
  }, []);

  const hasActiveFilters = type || from || to || search || selectedCategoryIds.length > 0;

  const filtersProps = useMemo(() => ({
    type,
    from,
    to,
    search,
    selectedCategoryIds,
    categoriesLoading,
    categoriesData,
    onTypeChange: setTypeCallback,
    onFromChange: setFromCallback,
    onToChange: setToCallback,
    onSearchChange: setSearchCallback,
    onCategoryIdsChange: setSelectedCategoryIdsCallback,
  }), [type, from, to, search, selectedCategoryIds, categoriesLoading, categoriesData, setTypeCallback, setFromCallback, setToCallback, setSearchCallback, setSelectedCategoryIdsCallback]);

  return (
    <div className="min-h-screen bg-background">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
        {/* Header */}
        <div>
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
            <div>
              <h1 className="text-3xl font-bold text-foreground mb-1">
                {t("title")}
              </h1>
              <p className="text-muted-foreground">
                {t("description")}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <ExportButton
                type={type}
                from={from}
                to={to}
                search={search}
                selectedCategoryIds={selectedCategoryIds}
                disabled={isLoading || !!error}
              />
              <Button
                variant="secondary"
                icon="upload"
                onClick={() => setShowImport(true)}
                className="shadow-sm"
              >
                {t("import")}
              </Button>
              <Button
                icon="plus"
                onClick={() => setShowCreate(true)}
                className="shadow-sm"
              >
                {t("create")}
              </Button>
            </div>
          </div>
        </div>

        {/* Quick Filters */}
        <QuickFilters
          from={from}
          to={to}
          onFromChange={setFromCallback}
          onToChange={setToCallback}
        />

        {/* Statistics (full filtered period, ignores pagination) */}
        {!isLoading && !error && Number(data?.page?.totalItems || 0) > 0 && !totalsLoading && !totalsError && totalsData && (
          <div className="mb-4">
            <TransactionStats
              totalIncome={totalsData.totalIncome}
              totalExpenses={totalsData.totalExpenses}
              currencyCode={totalsData.currencyCode || "RUB"}
              period={t("periodCurrent") as string}
            />
          </div>
        )}



        {/* Content */}
        {isLoading && (
          <div className="flex items-center justify-center py-12">
            <div className="flex flex-col items-center space-y-3">
              <div className="w-8 h-8 border-4 border-border/60 border-t-primary rounded-full animate-spin"></div>
              <span className="text-muted-foreground font-medium">{tc("loading")}</span>
            </div>
          </div>
        )}

        {error && (
          <Card className="border border-[hsl(var(--negative)/0.4)] bg-[hsl(var(--negative)/0.08)]">
            <CardContent className="pt-4">
              <div className="flex items-center space-x-2 text-[hsl(var(--negative))]">
                <Icon name="alert-circle" size={16} />
                <span className="font-medium">{(error as any).message}</span>
              </div>
            </CardContent>
          </Card>
        )}

        {!isLoading && !error && (
          <>
            <div className="relative">

              
              {/* Compact filters attached to table */}
              {showFilters && (
                <FiltersForm {...filtersProps} />
              )}
              
              <TransactionTable
                items={data?.transactions ?? []}
                categories={categoriesData?.categories ?? []}
                categoriesLoading={categoriesLoading}
                onDelete={(id: string) => deleteMut.mutate(id)}
                isDeleting={deleteMut.isPending}
                onChanged={refetchListAndTotals}
                showFilters={showFilters}
                setShowFilters={setShowFilters}
                hasActiveFilters={hasActiveFilters}
                clearFilters={clearFilters}
                currentSort={sort}
                onSort={handleSort}
              />
              <Modal
                open={showCreate}
                title={t("newTransaction") as string}
                onClose={() => setShowCreate(false)}
                maxWidthClass="max-w-xl"
                footer={(
                  <>
                    <Button variant="outline" onClick={() => setShowCreate(false)}>{tc("cancel")}</Button>
                    <Button variant="primary" id="new-tx-save" onClick={() => formRef.current?.submit()}>{tc("save")}</Button>
                    <Button variant="secondary" id="new-tx-save-more" onClick={() => formRef.current?.submitAndAddMore()}>{t("saveAndAddMore")}</Button>
                  </>
                )}
              >
                {/* We need a form element to bind submit to footer button */}
                <div>
                  <NewTransactionForm
                    ref={formRef}
                    onClose={() => setShowCreate(false)}
                    onSaved={() => setPage(1)}
                  />
                </div>
              </Modal>

              {/* Import modal */}
              <Modal
                open={showImport}
                title={t("importTitle") as string}
                onClose={() => setShowImport(false)}
                maxWidthClass="max-w-3xl"
                footer={(
                  <>
                    <Button variant="outline" onClick={() => setShowImport(false)}>{tc("close")}</Button>
                  </>
                )}
              >
                <ImportWizard
                  onClose={() => setShowImport(false)}
                  onCompleted={(_inserted) => {
                    setShowImport(false);
                    setPage(1);
                    refetchListAndTotals();
                  }}
                />
              </Modal>
            </div>

            {/* Pagination */}
            {data?.page && (
              <div className={`${SURFACE_CARD} mt-4 flex items-center justify-between p-4 rounded-lg`}>
                <div className="text-sm text-muted-foreground">
                  {t("showing")} {((page - 1) * pageSize) + 1} - {Math.min(page * pageSize, Number(data.page.totalItems))} {t("of")} {Number(data.page.totalItems)} {t("transactions")}
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
                    {t("prev")}
                  </Button>
                  <div className="px-3 py-1 text-sm font-medium bg-[hsl(var(--primary)/0.15)] text-[hsl(var(--primary))] rounded">
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
                    {t("next")}
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
  showFilters,
  setShowFilters,
  hasActiveFilters,
  clearFilters,
  currentSort,
  onSort,
}: {
  items: any[];
  categories: any[];
  categoriesLoading: boolean;
  onDelete: (id: string) => void;
  isDeleting: boolean;
  onChanged: () => void;
  showFilters: boolean;
  setShowFilters: (show: boolean) => void;
  hasActiveFilters: boolean | string | number;
  clearFilters: () => void;
  currentSort: string;
  onSort: (field: string, direction: "asc" | "desc" | null) => void;
}) {
  const { transaction } = useClients();
  const t = useTranslations("transactions");
  const tableCardClass = `${SURFACE_CARD} shadow-lg overflow-hidden !rounded-none`;
  const inlineInputClass =
    "w-full px-2 py-1 rounded-md border border-border bg-background/80 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary/40 focus:border-transparent";
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editComment, setEditComment] = useState<string>("");
  const [editAmount, setEditAmount] = useState<string>("");
  const [editCategoryId, setEditCategoryId] = useState<string>("");
  const [editDate, setEditDate] = useState<string>("");
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

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
      <Card className={`${SURFACE_CARD} text-center py-12`}>
        <CardContent>
          <div className="inline-flex items-center justify-center w-16 h-16 bg-secondary/60 rounded-full mb-4">
            <Icon name="receipt" size={32} className="text-muted-foreground" />
          </div>
          <h3 className="text-xl font-bold text-foreground mb-2">{t("noTransactions")}</h3>
          <p className="text-muted-foreground">{t("noTransactionsDescription")}</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className={tableCardClass}>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead className="bg-secondary/40">
            <tr>
              <SortableHeader
                field="type"
                currentSort={currentSort}
                onSort={onSort}
                defaultDirection="asc"
              >
                Тип
              </SortableHeader>
              <SortableHeader
                field="occurred_at"
                currentSort={currentSort}
                onSort={onSort}
                defaultDirection="desc"
              >
                Дата
              </SortableHeader>
              <SortableHeader
                field="comment"
                currentSort={currentSort}
                onSort={onSort}
                defaultDirection="asc"
              >
                Описание
              </SortableHeader>
              <SortableHeader
                field="category_code"
                currentSort={currentSort}
                onSort={onSort}
                defaultDirection="asc"
              >
                Категория
              </SortableHeader>
              <SortableHeader
                field="amount_numeric"
                currentSort={currentSort}
                onSort={onSort}
                className="text-right"
                defaultDirection="asc"
              >
                Сумма
              </SortableHeader>
              <th className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider">
                <div className="flex items-center justify-end space-x-2">
                  {hasActiveFilters && (
                    <button
                      onClick={clearFilters}
                      className="inline-flex items-center p-1 text-xs text-[hsl(var(--negative))] hover:text-[hsl(var(--negative)/0.7)] transition-colors duration-200"
                      title={t("clearFilters")}
                    >
                      <Icon name="trash" size={14} />
                    </button>
                  )}
                  <button
                    onClick={() => setShowFilters(!showFilters)}
                    className="inline-flex items-center p-1 text-xs text-muted-foreground hover:text-[hsl(var(--primary))] transition-colors duration-200"
                    title={t("toggleFilters")}
                  >
                    <Icon name="filter" size={14} />
                    {hasActiveFilters && (
                      <span className="absolute -top-1 -right-1 w-2 h-2 bg-[hsl(var(--primary))] rounded-full"></span>
                    )}
                  </button>
                </div>
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border/60">
            {items.map((tx: any) => (
              <tr key={tx?.id} className="hover:bg-secondary/30 transition-colors">
                <td className="px-4 py-3">
                  <div
                    className={`inline-flex items-center px-2 py-1 text-xs font-medium ${
                      tx?.type === TransactionType.EXPENSE
                        ? "text-[hsl(var(--negative))]"
                        : "text-[hsl(var(--positive))]"
                    }`}
                  >
                    <Icon
                      name={tx?.type === TransactionType.EXPENSE ? "trending-down" : "trending-up"}
                      size={12}
                      className="mr-1"
                    />
                    {tx?.type === TransactionType.EXPENSE ? t("expense") : t("income")}
                  </div>
                </td>
                <td className="px-4 py-3">
                  {editingId === tx?.id ? (
                    <input
                      type="datetime-local"
                      className={inlineInputClass}
                      value={editDate}
                      onChange={(e) => setEditDate(e.target.value)}
                      autoComplete="off"
                    />
                  ) : (
                    <div className="text-sm text-foreground">
                      {tx?.occurredAt?.seconds ? new Date(Number(tx.occurredAt.seconds) * 1000).toLocaleDateString() : ""}
                    </div>
                  )}
                </td>
                <td className="px-4 py-3">
                  {editingId === tx?.id ? (
                    <input
                      className={inlineInputClass}
                      placeholder="Описание"
                      value={editComment}
                      onChange={(e) => setEditComment(e.target.value)}
                      autoComplete="off"
                    />
                  ) : (
                    <div className="text-sm text-foreground">
                      {tx?.comment || t("noComment")}
                    </div>
                  )}
                </td>
                <td className="px-4 py-3">
                  {editingId === tx?.id ? (
                    categoriesLoading ? (
                      <div className={`${inlineInputClass} text-center text-muted-foreground`}>
                        Загрузка категорий...
                      </div>
                    ) : (
                      <select
                        className={inlineInputClass}
                        value={editCategoryId}
                        onChange={(e) => setEditCategoryId(e.target.value)}
                      >
                        <option value="">Без категории</option>
                        {categories
                          .filter((cat: any) => {
                            const isExpense = tx?.type === TransactionType.EXPENSE;
                            const isIncome = tx?.type === TransactionType.INCOME;

                            if (isExpense) {
                              return cat.kind === CategoryKind.EXPENSE;
                            } else if (isIncome) {
                              return cat.kind === CategoryKind.INCOME;
                            }
                            return true;
                          })
                          .map((cat: any) => {
                            const getCategoryName = (cat: any) => {
                              if (cat.translations) {
                                const ruTranslation = cat.translations.find((t: any) => t.locale === "ru");
                                if (ruTranslation) return ruTranslation.name;
                                const enTranslation = cat.translations.find((t: any) => t.locale === "en");
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
                      categoryCode={categories.find((c) => c.id === tx?.categoryId)?.code}
                      categoryTranslations={categories.find((c) => c.id === tx?.categoryId)?.translations}
                      type={tx?.type === TransactionType.EXPENSE ? "expense" : "income"}
                    />
                  )}
                </td>
                <td className="px-4 py-3 text-right">
                  {editingId === tx?.id ? (
                    <input
                      className={`${inlineInputClass} w-24 text-right`}
                      placeholder="0.00"
                      type="text"
                      value={editAmount}
                      onChange={(e) => {
                        const value = e.target.value;
                        if (value === "" || /^[0-9]*[.,]?[0-9]{0,2}$/.test(value)) {
                          setEditAmount(value);
                        }
                      }}
                      autoComplete="off"
                    />
                  ) : (
                    <div
                      className={`text-sm font-semibold ${
                        tx?.type === TransactionType.EXPENSE
                          ? "text-[hsl(var(--negative))]"
                          : "text-[hsl(var(--positive))]"
                      }`}
                    >
                      {tx?.type === TransactionType.EXPENSE ? "-" : "+"}
                      {formatCurrency(Number(tx?.amount?.minorUnits ?? 0), tx?.amount?.currencyCode)}
                    </div>
                  )}
                </td>
                <td className="px-4 py-3 text-center">
                  {editingId === tx?.id ? (
                    <div className="flex items-center justify-center space-x-1">
                      <Button
                        size="sm"
                        variant="primary"
                        loading={updateMut.isPending}
                        icon="check"
                        onClick={() => {
                          console.log('Sending update with values:', {
                            editComment,
                            editAmount,
                            editCategoryId,
                            editDate,
                            categoryId: editCategoryId === "" ? undefined : editCategoryId,
                            occurredAt: editDate ? { seconds: Math.floor(new Date(editDate).getTime() / 1000) } : undefined,
                          });
                                                      updateMut.mutate({
                              id: tx.id as string,
                              comment: editComment,
                              amountMinorUnits: editAmount ? Math.round(parseFloat(editAmount.replace(',', '.')) * 100) : undefined,
                              categoryId: editCategoryId === "" ? undefined : editCategoryId,
                              occurredAt: editDate ? { seconds: Math.floor(new Date(editDate).getTime() / 1000) } : undefined,
                            });
                        }}
                        >
                        {updateMut.isPending ? t("saving") : t("save")}
                      </Button>
                      <Button 
                        size="sm"
                        variant="outline"
                        onClick={() => setEditingId(null)}
                      >
                        {t("cancel")}
                      </Button>
                    </div>
                  ) : (
                    <div className="flex items-center justify-center space-x-1">
                      <button
                        onClick={() => {
                          setEditingId(tx?.id);
                          setEditComment(tx?.comment ?? "");
                          setEditAmount(tx?.amount?.minorUnits ? (Number(tx.amount.minorUnits) / 100).toString() : "");
                          setEditCategoryId(tx?.categoryId ?? "");
                          const toLocalInput = (d: Date) => {
                            const pad = (n: number) => String(n).padStart(2, "0");
                            return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
                          };
                          setEditDate(tx?.occurredAt?.seconds ? toLocalInput(new Date(Number(tx.occurredAt.seconds) * 1000)) : "");
                        }}
                        className="p-1 text-[hsl(var(--primary))] hover:text-[hsl(var(--primary)/0.8)] transition-colors	duration-200"
                        title={t("edit")}
                      >
                        <Icon name="edit" size={16} />
                      </button>
                      <button
                        onClick={() => {
                          if (isDeleting) return;
                          setDeleteTarget(tx?.id || null);
                        }}
                        disabled={isDeleting}
                        className="p-1 text-[hsl(var(--negative))] hover:text-[hsl(var(--negative)/0.7)] transition-colors	duration-200 disabled:opacity-50"
                        title={t("delete")}
                      >
                        {isDeleting ? (
                          <div className="w-4 h-4 border-2 border-[hsl(var(--negative)/0.5)] border-t-[hsl(var(--negative))] rounded-full animate-spin" />
                        ) : (
                          <Icon name="trash" size={16} />
                        )}
                      </button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Modal
        open={Boolean(deleteTarget)}
        title={t("delete")}
        onClose={() => setDeleteTarget(null)}
        maxWidthClass="max-w-md"
        footer={
          <>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>
              {t("cancel")}
            </Button>
            <Button
              variant="destructive"
              icon="trash"
              onClick={() => {
                if (!deleteTarget) return;
                onDelete(deleteTarget);
                setDeleteTarget(null);
              }}
            >
              {t("delete")}
            </Button>
          </>
        }
      >
        <p className="text-sm text-muted-foreground">{t("confirmDelete")}</p>
      </Modal>
    </Card>
  );
}

