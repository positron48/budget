"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMemo, useState, useEffect, useRef } from "react";
import { TransactionType, CategoryKind } from "@/proto/budget/v1/common_pb";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Icon, Button, Card, CardContent, CardHeader, CardTitle } from "@/components";

const schema = z.object({
  // 2 = EXPENSE, 1 = INCOME
  type: z.coerce.number().int().min(1).max(2),
  amount: z.coerce.number().min(0.01),
  currencyCode: z.string().min(3),
  occurredAt: z.string(),
  categoryId: z.string().optional(),
  comment: z.string().optional(),
});
type FormValues = z.infer<typeof schema>;

function TxForm() {
  const { transaction, category } = useClients();
  const queryClient = useQueryClient();
  const router = useRouter();
  const t = useTranslations("transactions");
  const tc = useTranslations("common");
  const { register, handleSubmit, reset, watch, setValue, formState: { errors } } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      type: TransactionType.EXPENSE as unknown as number,
      currencyCode: "RUB",
      occurredAt: new Date().toISOString().slice(0, 16),
    },
  });
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [categoryChanged, setCategoryChanged] = useState(false);
  
  const typeWatch = watch("type");
  const mappedKind = useMemo(() => {
    const typeWatchNumber = Number(typeWatch);
    return typeWatchNumber === TransactionType.INCOME ? CategoryKind.INCOME : CategoryKind.EXPENSE;
  }, [typeWatch]);
  const prevMappedKindRef = useRef(mappedKind);
  
  const { data: catData, isLoading: categoriesLoading } = useQuery({
    queryKey: ["tx-new-categories", mappedKind],
    queryFn: async () => {
      return await category.listCategories({ 
        kind: mappedKind,
        includeInactive: false 
      } as any);
    },
    staleTime: 0, // Disable caching to ensure fresh data
  });

  // Reset category selection when transaction type changes
  useEffect(() => {
    if (prevMappedKindRef.current !== mappedKind) {
      const currentCategoryId = watch("categoryId");
      if (currentCategoryId) {
        // Small delay to show loading state
        const timer = setTimeout(() => {
          setValue("categoryId", "");
          setCategoryChanged(true);
          // Hide the notification after 2 seconds
          setTimeout(() => setCategoryChanged(false), 2000);
        }, 100);
        return () => clearTimeout(timer);
      }
      // Invalidate and refetch categories
      queryClient.invalidateQueries({ queryKey: ["tx-new-categories"] });
      prevMappedKindRef.current = mappedKind;
    }
  }, [mappedKind, setValue, watch, queryClient]);
  
  const onSubmit = async (v: FormValues) => {
    setError(null);
    setIsSubmitting(true);
    try {
      const payload: any = {
        type: Number(v.type),
        amount: { currencyCode: v.currencyCode, minorUnits: Math.round(v.amount * 100) },
        occurredAt: { seconds: Math.floor(new Date(v.occurredAt).getTime() / 1000) },
      };
      if (v.categoryId) payload.categoryId = v.categoryId;
      if (v.comment) payload.comment = v.comment;
      await transaction.createTransaction(payload as any);
      reset();
      router.push("/transactions");
    } catch (e: any) {
      setError(e?.message ?? "Failed to create transaction");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-50 to-blue-50 dark:from-slate-900 dark:to-slate-800">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {/* Header */}
        <div className="mb-8">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-4xl font-bold text-slate-900 dark:text-white mb-2">
                {t("create")}
              </h1>
              <p className="text-slate-600 dark:text-slate-300 text-lg">
                Создайте новую финансовую транзакцию
              </p>
            </div>
            <Link 
              href="/transactions" 
              className="inline-flex items-center px-4 py-2 text-slate-600 dark:text-slate-300 hover:text-slate-900 dark:hover:text-white font-medium rounded-lg border border-slate-200 dark:border-slate-600 hover:bg-white dark:hover:bg-slate-800 transition-all duration-200"
            >
              <Icon name="arrow-left" size={16} className="mr-2" />
              {tc("back")}
            </Link>
          </div>
        </div>

        {/* Form */}
        <div className="max-w-2xl mx-auto">
          <Card className="shadow-xl border-0 bg-white/90 dark:bg-slate-800/90 backdrop-blur-sm">
            <CardHeader className="pb-6">
              <CardTitle className="text-2xl flex items-center space-x-3">
                <div className={`p-2 rounded-lg ${
                  typeWatch === TransactionType.EXPENSE 
                    ? 'bg-red-100 dark:bg-red-900/30' 
                    : 'bg-green-100 dark:bg-green-900/30'
                }`}>
                  <Icon 
                    name={typeWatch === TransactionType.EXPENSE ? "trending-down" : "trending-up"} 
                    size={20} 
                    className={typeWatch === TransactionType.EXPENSE ? "text-red-600 dark:text-red-400" : "text-green-600 dark:text-green-400"}
                  />
                </div>
                <span>Новая транзакция</span>
              </CardTitle>
            </CardHeader>
            <CardContent>
              {error && (
                <div className="mb-6 p-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                  <div className="flex items-center space-x-2 text-red-700 dark:text-red-300">
                    <Icon name="alert-circle" size={16} />
                    <span className="font-medium">{error}</span>
                  </div>
                </div>
              )}

              <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
                {/* Transaction Type */}
                <div className="space-y-3">
                  <label className="text-sm font-semibold text-slate-700 dark:text-slate-300">
                    Тип транзакции
                  </label>
                  <div className="grid grid-cols-2 gap-3">
                    <label className={`relative cursor-pointer rounded-lg border-2 p-4 transition-all duration-200 ${
                      typeWatch === TransactionType.EXPENSE
                        ? 'border-red-500 bg-red-50 dark:bg-red-900/20'
                        : 'border-slate-200 dark:border-slate-600 hover:border-red-300 dark:hover:border-red-700'
                    }`}>
                      <input
                        type="radio"
                        value={TransactionType.EXPENSE}
                        {...register("type")}
                        className="sr-only"
                      />
                      <div className="flex items-center space-x-3">
                        <div className="w-8 h-8 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
                          <Icon name="trending-down" size={16} className="text-red-600 dark:text-red-400" />
                        </div>
                        <div>
                          <div className="font-semibold text-slate-900 dark:text-white">Расход</div>
                          <div className="text-sm text-slate-500 dark:text-slate-400">Потраченные деньги</div>
                        </div>
                      </div>
                    </label>
                    
                    <label className={`relative cursor-pointer rounded-lg border-2 p-4 transition-all duration-200 ${
                      typeWatch === TransactionType.INCOME
                        ? 'border-green-500 bg-green-50 dark:bg-green-900/20'
                        : 'border-slate-200 dark:border-slate-600 hover:border-green-300 dark:hover:border-green-700'
                    }`}>
                      <input
                        type="radio"
                        value={TransactionType.INCOME}
                        {...register("type")}
                        className="sr-only"
                      />
                      <div className="flex items-center space-x-3">
                        <div className="w-8 h-8 rounded-full bg-green-100 dark:bg-green-900/30 flex items-center justify-center">
                          <Icon name="trending-up" size={16} className="text-green-600 dark:text-green-400" />
                        </div>
                        <div>
                          <div className="font-semibold text-slate-900 dark:text-white">Доход</div>
                          <div className="text-sm text-slate-500 dark:text-slate-400">Полученные деньги</div>
                        </div>
                      </div>
                    </label>
                  </div>
                </div>

                {/* Amount and Currency */}
                <div className="space-y-3">
                  <label className="text-sm font-semibold text-slate-700 dark:text-slate-300">
                    Сумма
                  </label>
                  <div className="grid grid-cols-3 gap-3">
                    <div className="col-span-2">
                      <div className="relative">
                        <span className="absolute left-3 top-1/2 transform -translate-y-1/2 text-slate-400 text-lg font-medium">
                          ₽
                        </span>
                        <input 
                          type="number" 
                          step="0.01" 
                          className="w-full pl-8 pr-4 py-3 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-lg font-semibold" 
                          placeholder="0.00"
                          {...register("amount")}
                          autoComplete="off"
                        />
                      </div>
                      {errors.amount && (
                        <p className="text-sm text-red-600 dark:text-red-400 mt-1">{errors.amount.message}</p>
                      )}
                    </div>
                    <div>
                      <input 
                        className="w-full px-4 py-3 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 text-center font-medium" 
                        placeholder="RUB"
                        {...register("currencyCode")}
                        autoComplete="off"
                      />
                    </div>
                  </div>
                </div>

                {/* Date and Time */}
                <div className="space-y-3">
                  <label className="text-sm font-semibold text-slate-700 dark:text-slate-300">
                    Дата и время
                  </label>
                  <input 
                    type="datetime-local" 
                    className="w-full px-4 py-3 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200" 
                    {...register("occurredAt")}
                    autoComplete="off"
                  />
                </div>

                {/* Category */}
                <div className="space-y-3">
                  <label className="text-sm font-semibold text-slate-700 dark:text-slate-300">
                    Категория
                  </label>
                  <select 
                    className="w-full px-4 py-3 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200" 
                    {...register("categoryId")}
                    disabled={categoriesLoading}
                  >
                    <option value="">
                      {categoriesLoading ? "Загрузка категорий..." : "Выберите категорию"}
                    </option>
                    {(catData?.categories ?? []).map((c: any) => (
                      <option key={c?.id} value={c?.id}>{c?.code} - {c?.name}</option>
                    ))}
                  </select>
                  {categoriesLoading && (
                    <div className="text-sm text-slate-500 dark:text-slate-400 flex items-center space-x-1">
                      <div className="w-4 h-4 border-2 border-slate-300 border-t-slate-600 rounded-full animate-spin"></div>
                      <span>Загрузка категорий...</span>
                    </div>
                  )}
                  {categoryChanged && !categoriesLoading && (
                    <p className="text-sm text-blue-600 dark:text-blue-400 flex items-center space-x-1">
                      <Icon name="info" size={14} />
                      <span>Категории обновлены для выбранного типа</span>
                    </p>
                  )}
                </div>

                {/* Comment */}
                <div className="space-y-3">
                  <label className="text-sm font-semibold text-slate-700 dark:text-slate-300">
                    Комментарий
                  </label>
                  <textarea 
                    className="w-full px-4 py-3 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all duration-200 resize-none" 
                    rows={3}
                    placeholder="Добавьте описание транзакции..."
                    {...register("comment")}
                    autoComplete="off"
                  />
                </div>

                {/* Submit Buttons */}
                <div className="flex gap-3 pt-6 border-t border-slate-200 dark:border-slate-600">
                  <Button
                    type="submit"
                    disabled={isSubmitting}
                    className="flex-1 bg-blue-600 hover:bg-blue-700 text-white font-semibold py-3 px-6 rounded-lg shadow-lg hover:shadow-xl transition-all duration-200 transform hover:scale-105"
                  >
                    {isSubmitting ? (
                      <div className="flex items-center space-x-2">
                        <div className="w-5 h-5 border-2 border-white/30 border-t-white rounded-full animate-spin"></div>
                        <span>Создание...</span>
                      </div>
                    ) : (
                      <div className="flex items-center space-x-2">
                        <Icon name="check" size={18} />
                        <span>Создать транзакцию</span>
                      </div>
                    )}
                  </Button>
                  <Link 
                    href="/transactions" 
                    className="px-6 py-3 text-slate-600 dark:text-slate-300 hover:text-slate-900 dark:hover:text-white font-medium rounded-lg border border-slate-200 dark:border-slate-600 hover:bg-white dark:hover:bg-slate-800 transition-all duration-200"
                  >
                    {tc("cancel")}
                  </Link>
                </div>
              </form>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

export default function NewTxPage() {
  return (
    <ClientsProvider>
      <TxForm />
    </ClientsProvider>
  );
}


