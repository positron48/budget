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
  const surfaceCard = "border border-border bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60";
  
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
    <div
      key={c?.id}
      className={`group rounded-md border border-border bg-card/70 hover:border-primary/40 transition-all duration-200 ${
        !c?.isActive ? "opacity-60" : ""
      }`}
    >
      <div className="px-3 py-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2 flex-1">
            <div
              className={`w-6 h-6 rounded-md flex items-center justify-center ${
                c?.kind === 2
                  ? "bg-[hsl(var(--negative)/0.15)] text-[hsl(var(--negative))]"
                  : "bg-[hsl(var(--positive)/0.15)] text-[hsl(var(--positive))]"
              } ${!c?.isActive ? "opacity-50" : ""}`}
            >
              <Icon 
                name={c?.kind === 2 ? "trending-down" : "trending-up"} 
                size={12} 
              />
            </div>
            <div className="flex-1 min-w-0">
              {editingId === c?.id ? (
                <div className="space-y-1">
                  <input
                    className={`w-full px-2 py-1 rounded text-sm font-medium text-foreground focus:ring-2 focus:ring-[hsl(var(--info))] focus:border-transparent transition-colors ${
                      editError
                        ? "border border-[hsl(var(--negative))] bg-[hsl(var(--negative)/0.08)]"
                        : "border border-[hsl(var(--info))] bg-[hsl(var(--info)/0.08)]"
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
                    <div className="text-xs text-[hsl(var(--negative))]">{editError}</div>
                  )}
                </div>
              ) : (
                <div 
                  className={`font-medium text-sm cursor-pointer transition-colors px-1 py-0.5 rounded truncate ${
                    c?.isActive
                      ? "text-foreground hover:text-[hsl(var(--info))] hover:bg-[hsl(var(--info)/0.08)]"
                      : "text-muted-foreground hover:text-foreground hover:bg-[hsl(var(--info)/0.08)]"
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
              className="h-5 w-5 p-0 text-muted-foreground hover:text-foreground flex items-center justify-center"
              onClick={() => startEdit(c)}
              title={tc("edit")}
            >
              <Icon name="edit" size={10} />
            </button>
            <button
              className="h-5 w-5 p-0 text-muted-foreground hover:text-[hsl(var(--negative))] flex items-center justify-center"
              onClick={() => {
                if (confirm("Вы уверены, что хотите удалить эту категорию?")) {
                  deleteMut.mutate(c?.id);
                }
              }}
              title={tc("delete")}
              disabled={deleteMut.isPending}
            >
              {deleteMut.isPending ? (
                <div className="w-2.5 h-2.5 border border-muted-foreground/60 border-t-transparent rounded-full animate-spin" />
              ) : (
                <Icon name="trash" size={10} />
              )}
            </button>
            <button
              className="h-5 w-5 p-0 text-muted-foreground hover:text-[hsl(var(--positive))] flex items-center justify-center"
              onClick={() => toggleActiveMut.mutate({ id: c.id, isActive: !c.isActive })}
              disabled={toggleActiveMut.isPending}
              title={c?.isActive ? "Деактивировать" : "Активировать"}
            >
              {toggleActiveMut.isPending ? (
                <div className="w-2.5 h-2.5 border border-muted-foreground/60 border-t-transparent rounded-full animate-spin" />
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
    const _prevCodeRef = React.useRef<string>(code);
    
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
      <div className="bg-secondary/40 rounded-md border border-dashed border-border/60">
        <div className="p-2">
          <form
            className="flex gap-2"
            onSubmit={handleSubmit}
          >
            <input
              ref={inputRef}
              className="input flex-1 bg-background/40"
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
                  ? "bg-[hsl(var(--negative))] hover:bg-[hsl(var(--negative)/0.85)] text-[hsl(var(--negative-foreground))]"
                  : "bg-[hsl(var(--positive))] hover:bg-[hsl(var(--positive)/0.85)] text-[hsl(var(--positive-foreground))]"
              }`}
              disabled={!localCode.trim()}
              onMouseDown={(e: any) => e.preventDefault()}
            >
              {mutation.isPending ? tc("saving") : tc("add")}
            </Button>
          </form>
          {mutation.error && (
            <div className="mt-1 text-xs text-[hsl(var(--negative))]">
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
          <h2 className="text-base font-semibold text-foreground">{title}</h2>
          <p className="text-xs text-muted-foreground">
            {categories.length} {categories.length === 1 ? t("category") : t("categories")}
          </p>
        </div>
      </div>
      
      <div className="space-y-1">
        {categories.length === 0 ? (
          <div className={`${surfaceCard} text-center p-4`}>
            <div className="w-8 h-8 mx-auto mb-2 rounded-full bg-[hsl(var(--muted)/0.6)] flex items-center justify-center">
              <Icon name="categories" size={16} className="text-muted-foreground" />
            </div>
            <h3 className="text-sm font-medium text-foreground mb-1">{emptyTitle}</h3>
            <p className="text-xs text-muted-foreground">{emptyDescription}</p>
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
    <div className="min-h-screen bg-background">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-4">
        {/* Header */}
        <div className="mb-6">
          <h1 className="text-xl font-bold text-foreground mb-1">
            {t("title")}
          </h1>
          <p className="text-sm text-muted-foreground">
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
          <div className="bg-[hsl(var(--negative)/0.1)] border border-[hsl(var(--negative)/0.4)] rounded-md p-3 mb-4">
            <div className="flex items-center space-x-2 text-[hsl(var(--negative))]">
              <Icon name="alert-circle" size={14} className="text-[hsl(var(--negative))]" />
              <span className="text-sm">{(error as any).message}</span>
            </div>
          </div>
        )}

        {!isLoading && !error && (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
            {/* Expense Categories */}
            <CategorySection
              title={t("expenseCategories")}
              icon="trending-down"
              iconColor="bg-[hsl(var(--negative))]"
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
              iconColor="bg-[hsl(var(--positive))]"
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
          <div className="fixed bottom-4 right-4 bg-[hsl(var(--negative)/0.12)] border border-[hsl(var(--negative)/0.4)] rounded-lg p-4 max-w-md shadow-lg">
            <div className="flex items-start space-x-3 text-[hsl(var(--negative))]">
              <Icon name="alert-circle" size={16} className="mt-0.5 flex-shrink-0" />
              <div>
                <h3 className="text-sm font-medium mb-1">Не удалось удалить категорию</h3>
                <p className="text-xs opacity-90">{(deleteMut.error as any).message}</p>
                <button
                  onClick={() => deleteMut.reset()}
                  className="mt-2 text-xs underline-offset-2 hover:underline"
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


