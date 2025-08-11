"use client";

import React, { useState } from "react";
import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { LoadingSpinner, Icon, Button } from "@/components";

// enums are numeric in TS output; 2 = EXPENSE, 1 = INCOME

function CategoriesInner() {
  const { category } = useClients();
  const qc = useQueryClient();
  const t = useTranslations("categories");
  const tc = useTranslations("common");
  const expenseInputRef = React.useRef<HTMLInputElement | null>(null);
  const incomeInputRef = React.useRef<HTMLInputElement | null>(null);
  
  // Get all categories (both expense and income)
  const { data, error, isLoading } = useQuery({
    queryKey: ["categories", "all"],
    queryFn: async () => {
      const [expenseCategories, incomeCategories] = await Promise.all([
        category.listCategories({ kind: 2, includeInactive: true } as any),
        category.listCategories({ kind: 1, includeInactive: true } as any),
      ]);
      return {
        expenseCategories: expenseCategories?.categories || [],
        incomeCategories: incomeCategories?.categories || [],
      };
    },
    retry: false,
    refetchOnWindowFocus: false,
  });

  // Sort categories: active first, then inactive, then alphabetically
  const sortCategories = (categories: any[]) => {
    return categories.sort((a, b) => {
      // First sort by active status (active first)
      if (a.isActive !== b.isActive) {
        return a.isActive ? -1 : 1;
      }
      // Then sort alphabetically by code
      return a.code.localeCompare(b.code);
    });
  };

  const expenseCategories = sortCategories(data?.expenseCategories || []);
  const incomeCategories = sortCategories(data?.incomeCategories || []);

  // Quick add form states
  const [newExpenseCode, setNewExpenseCode] = useState("");
  const [newIncomeCode, setNewIncomeCode] = useState("");

  // Create mutations
  const createExpenseMut = useMutation({
    mutationFn: async () => {
      // Check for duplicates
      const isDuplicate = expenseCategories.some(cat => cat.code.toLowerCase() === newExpenseCode.toLowerCase());
      if (isDuplicate) {
        throw new Error("Категория с таким названием уже существует");
      }
      return await category.createCategory({ kind: 2, code: newExpenseCode, isActive: true } as any);
    },
    onSuccess: async () => {
      setNewExpenseCode("");
      await qc.invalidateQueries({ queryKey: ["categories"] });
      // Focus expense input after data refetch settles
      setTimeout(() => expenseInputRef.current?.focus(), 0);
    },
  });

  const createIncomeMut = useMutation({
    mutationFn: async () => {
      // Check for duplicates
      const isDuplicate = incomeCategories.some(cat => cat.code.toLowerCase() === newIncomeCode.toLowerCase());
      if (isDuplicate) {
        throw new Error("Категория с таким названием уже существует");
      }
      return await category.createCategory({ kind: 1, code: newIncomeCode, isActive: true } as any);
    },
    onSuccess: async () => {
      setNewIncomeCode("");
      await qc.invalidateQueries({ queryKey: ["categories"] });
      // Focus income input after data refetch settles
      setTimeout(() => incomeInputRef.current?.focus(), 0);
    },
  });

  const deleteMut = useMutation({
    mutationFn: async (id: string) => {
      const response = await category.deleteCategory({ id } as any);
      console.log("Delete response:", response);
      return response;
    },
    onSuccess: () => {
      console.log("Category deleted successfully");
      qc.invalidateQueries({ queryKey: ["categories"] });
    },
    onError: (error: any) => {
      console.error("Delete error:", error);
      // Check if it's a foreign key constraint error
      if (error?.message?.includes("foreign key constraint") || error?.message?.includes("transactions_category_id_fkey")) {
        throw new Error("Нельзя удалить категорию, которая используется в транзакциях. Сначала удалите или измените все связанные транзакции.");
      }
      // Generic error for other cases
      throw new Error("Не удалось удалить категорию. Попробуйте еще раз.");
    },
  });
  
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editCode, setEditCode] = useState<string>("");
  const [editError, setEditError] = useState<string>("");
  
  const updateMut = useMutation({
    mutationFn: async (payload: { id: string; code: string; isActive: boolean }) => {
      // Validate empty name
      if (!payload.code.trim()) {
        throw new Error("Название категории не может быть пустым");
      }
      
      // Check for duplicates (excluding current category)
      const allCategories = [...expenseCategories, ...incomeCategories];
      const isDuplicate = allCategories.some(cat => 
        cat.id !== payload.id && cat.code.toLowerCase() === payload.code.toLowerCase()
      );
      if (isDuplicate) {
        throw new Error("Категория с таким названием уже существует");
      }
      
      return await category.updateCategory(payload as any);
    },
    onSuccess: () => {
      setEditingId(null);
      setEditError("");
      qc.invalidateQueries({ queryKey: ["categories"] });
    },
    onError: (error: any) => {
      setEditError(error.message);
    },
  });

  const toggleActiveMut = useMutation({
    mutationFn: async (payload: { id: string; isActive: boolean }) => {
      const categoryItem = [...expenseCategories, ...incomeCategories].find(c => c.id === payload.id);
      if (!categoryItem) {
        throw new Error("Категория не найдена");
      }
      
      return await category.updateCategory({
        id: payload.id,
        code: categoryItem.code,
        isActive: payload.isActive,
      } as any);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["categories"] });
    },
  });
  
  const startEdit = (c: any) => {
    setEditingId(c?.id);
    setEditCode(c?.code ?? "");
    setEditError("");
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditError("");
  };

  const CategoryCard = ({ c }: { c: any }) => (
    <div key={c?.id} className={`group bg-white dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600 transition-all duration-200 ${
      !c?.isActive ? 'opacity-60' : ''
    }`}>
      <div className="px-3 py-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2 flex-1">
            <div className={`w-6 h-6 rounded-md flex items-center justify-center ${
              c?.kind === 2 
                ? 'bg-red-100 dark:bg-red-900/20 text-red-600 dark:text-red-400' 
                : 'bg-green-100 dark:bg-green-900/20 text-green-600 dark:text-green-400'
            } ${!c?.isActive ? 'opacity-50' : ''}`}>
              <Icon 
                name={c?.kind === 2 ? "trending-down" : "trending-up"} 
                size={12} 
              />
            </div>
            <div className="flex-1 min-w-0">
              {editingId === c?.id ? (
                <div className="space-y-1">
                  <input
                    className={`w-full px-2 py-1 bg-blue-50 dark:bg-blue-900/20 border border-blue-300 dark:border-blue-600 rounded text-gray-900 dark:text-gray-100 font-medium text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500 ${
                      editError ? 'border-red-300 dark:border-red-600 bg-red-50 dark:bg-red-900/20' : ''
                    }`}
                    value={editCode}
                    onChange={(e) => setEditCode(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        updateMut.mutate({
                          id: c.id as string,
                          code: editCode,
                          isActive: c.isActive,
                        });
                      } else if (e.key === 'Escape') {
                        cancelEdit();
                      }
                    }}
                    onBlur={() => {
                      if (editCode.trim() && editCode !== c.code) {
                        updateMut.mutate({
                          id: c.id as string,
                          code: editCode,
                          isActive: c.isActive,
                        });
                      } else {
                        cancelEdit();
                      }
                    }}
                    autoFocus
                  />
                  {editError && (
                    <div className="text-xs text-red-600 dark:text-red-400">{editError}</div>
                  )}
                </div>
              ) : (
                <div 
                  className={`font-medium text-sm cursor-pointer transition-colors px-1 py-0.5 rounded hover:bg-blue-50 dark:hover:bg-blue-900/20 truncate ${
                    c?.isActive 
                      ? 'text-gray-900 dark:text-gray-100 hover:text-blue-600 dark:hover:text-blue-400' 
                      : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
                  }`}
                  onClick={() => startEdit(c)}
                  title={`${c?.code || "Без названия"} (${c?.isActive ? 'активна' : 'неактивна'})`}
                >
                  {c?.code || "Без названия"}
                </div>
              )}
            </div>
          </div>
          
          <div className="flex items-center space-x-0.5 opacity-0 group-hover:opacity-100 transition-opacity duration-200">
            <button
              className="h-5 w-5 p-0 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 flex items-center justify-center"
              onClick={() => startEdit(c)}
              title={tc("edit")}
            >
              <Icon name="edit" size={10} />
            </button>
            <button
              className="h-5 w-5 p-0 text-gray-400 hover:text-red-600 dark:hover:text-red-400 flex items-center justify-center"
              onClick={() => {
                if (confirm("Вы уверены, что хотите удалить эту категорию?")) {
                  deleteMut.mutate(c?.id);
                }
              }}
              title={tc("delete")}
              disabled={deleteMut.isPending}
            >
              {deleteMut.isPending ? (
                <div className="w-2.5 h-2.5 border border-gray-400 border-t-transparent rounded-full animate-spin" />
              ) : (
                <Icon name="trash" size={10} />
              )}
            </button>
            <button
              className="h-5 w-5 p-0 text-gray-400 hover:text-green-600 dark:hover:text-green-400 flex items-center justify-center"
              onClick={() => toggleActiveMut.mutate({ id: c.id, isActive: !c.isActive })}
              disabled={toggleActiveMut.isPending}
              title={c?.isActive ? "Деактивировать" : "Активировать"}
            >
              {toggleActiveMut.isPending ? (
                <div className="w-2.5 h-2.5 border border-gray-400 border-t-transparent rounded-full animate-spin" />
              ) : (
                <Icon name={c?.isActive ? "eye-off" : "eye"} size={10} />
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );

  const QuickAddForm = ({ 
    kind, 
    code, 
    setCode, 
    mutation,
    inputRef,
  }: { 
    kind: number; 
    code: string; 
    setCode: (code: string) => void; 
    mutation: any;
    inputRef: React.RefObject<HTMLInputElement | null>;
  }) => {
    const [localCode, setLocalCode] = useState(code);
    const prevCodeRef = React.useRef<string>(code);
    
    // Sync local state with parent state
    React.useEffect(() => {
      setLocalCode(code);
    }, [code]);

    // No internal focus effect; parent controls focus explicitly via ref

    const handleSubmit = (e: React.FormEvent) => {
      e.preventDefault();
      if (localCode.trim()) {
        setCode(localCode);
        mutation.mutate();
      }
    };

    return (
      <div className="bg-gray-50 dark:bg-gray-800/50 rounded-md border border-dashed border-gray-300 dark:border-gray-600">
        <div className="p-2">
          <form
            className="flex gap-2"
            onSubmit={handleSubmit}
          >
            <input
              ref={inputRef}
              className="flex-1 px-2 py-1.5 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded text-sm text-gray-900 dark:text-gray-100 placeholder-gray-500 dark:placeholder-gray-400 focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
              value={localCode}
              onChange={(e) => setLocalCode(e.target.value)}
              placeholder={kind === 2 ? t("addExpenseCategory") : t("addIncomeCategory")}
              autoComplete="off"
            />
            <Button
              type="submit"
              loading={mutation.isPending}
              className={`px-2 py-1.5 text-xs font-medium ${
                kind === 2 
                  ? 'bg-red-500 hover:bg-red-600' 
                  : 'bg-green-500 hover:bg-green-600'
              } text-white`}
              disabled={!localCode.trim()}
              onMouseDown={(e: any) => e.preventDefault()}
            >
              {mutation.isPending ? tc("saving") : tc("add")}
            </Button>
          </form>
          {mutation.error && (
            <div className="mt-1 text-xs text-red-600 dark:text-red-400">
              {(mutation.error as any).message}
            </div>
          )}
        </div>
      </div>
    );
  };

  const CategorySection = ({ 
    title, 
    icon, 
    iconColor, 
    categories, 
    emptyTitle, 
    emptyDescription, 
    quickAddForm 
  }: {
    title: string;
    icon: "trending-down" | "trending-up";
    iconColor: string;
    categories: any[];
    emptyTitle: string;
    emptyDescription: string;
    quickAddForm: React.ReactNode;
  }) => (
    <div className="space-y-3">
      <div className="flex items-center space-x-2">
        <div className={`w-6 h-6 rounded-md flex items-center justify-center ${iconColor}`}>
          <Icon name={icon} size={14} className="text-white" />
        </div>
        <div>
          <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">{title}</h2>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            {categories.length} {categories.length === 1 ? t("category") : t("categories")}
          </p>
        </div>
      </div>
      
      <div className="space-y-1">
        {categories.length === 0 ? (
          <div className="bg-white dark:bg-gray-800 rounded-md border border-gray-200 dark:border-gray-700 p-4 text-center">
            <div className="w-8 h-8 mx-auto mb-2 rounded-full bg-gray-100 dark:bg-gray-700 flex items-center justify-center">
              <Icon name="categories" size={16} className="text-gray-400" />
            </div>
            <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-1">{emptyTitle}</h3>
            <p className="text-xs text-gray-500 dark:text-gray-400">{emptyDescription}</p>
          </div>
        ) : (
          categories.map((c: any) => (
            <CategoryCard key={c.id} c={c} />
          ))
        )}
        
        {quickAddForm}
      </div>
    </div>
  );

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-4">
        {/* Header */}
        <div className="mb-6">
          <h1 className="text-xl font-bold text-gray-900 dark:text-gray-100 mb-1">
            {t("title")}
          </h1>
          <p className="text-sm text-gray-600 dark:text-gray-400">
            {t("description")}
          </p>
        </div>

        {/* Content */}
        {isLoading && (
          <div className="flex items-center justify-center py-8">
            <LoadingSpinner text={tc("loading")} />
          </div>
        )}

        {error && (
          <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3 mb-4">
            <div className="flex items-center space-x-2">
              <Icon name="alert-circle" size={14} className="text-red-600 dark:text-red-400" />
              <span className="text-red-700 dark:text-red-300 text-sm">{(error as any).message}</span>
            </div>
          </div>
        )}

        {!isLoading && !error && (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
            {/* Expense Categories */}
            <CategorySection
              title={t("expenseCategories")}
              icon="trending-down"
              iconColor="bg-red-500"
              categories={expenseCategories}
              emptyTitle={t("noExpenseCategories")}
              emptyDescription={t("noExpenseCategoriesDescription")}
              quickAddForm={
                <QuickAddForm
                  kind={2}
                  code={newExpenseCode}
                  setCode={setNewExpenseCode}
                  mutation={createExpenseMut}
                  inputRef={expenseInputRef}
                />
              }
            />

            {/* Income Categories */}
            <CategorySection
              title={t("incomeCategories")}
              icon="trending-up"
              iconColor="bg-green-500"
              categories={incomeCategories}
              emptyTitle={t("noIncomeCategories")}
              emptyDescription={t("noIncomeCategoriesDescription")}
              quickAddForm={
                <QuickAddForm
                  kind={1}
                  code={newIncomeCode}
                  setCode={setNewIncomeCode}
                  mutation={createIncomeMut}
                  inputRef={incomeInputRef}
                />
              }
            />
          </div>
        )}

        {/* Global error display for delete mutations */}
        {deleteMut.error && (
          <div className="fixed bottom-4 right-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4 max-w-md shadow-lg">
            <div className="flex items-start space-x-3">
              <Icon name="alert-circle" size={16} className="text-red-600 dark:text-red-400 mt-0.5 flex-shrink-0" />
              <div>
                <h3 className="text-sm font-medium text-red-900 dark:text-red-100 mb-1">Не удалось удалить категорию</h3>
                <p className="text-xs text-red-700 dark:text-red-300">{(deleteMut.error as any).message}</p>
                <button
                  onClick={() => deleteMut.reset()}
                  className="mt-2 text-xs text-red-600 dark:text-red-400 hover:text-red-800 dark:hover:text-red-200"
                >
                  Закрыть
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default function CategoriesPage() {
  return (
    <ClientsProvider>
      <CategoriesInner />
    </ClientsProvider>
  );
}


