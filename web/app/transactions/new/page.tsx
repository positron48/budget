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
  const { register, handleSubmit, reset, watch, setValue } = useForm<FormValues>({
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
    <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8">
      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-bold text-foreground">{t("create")}</h1>
          <p className="text-muted-foreground mt-1">{t("description")}</p>
        </div>
        <Link href="/transactions" className="btn btn-outline">
          ← {tc("back")}
        </Link>
      </div>

      {/* Form */}
      <div className="max-w-2xl">
        <div className="card">
          <div className="card-header">
            <h3 className="card-title">{t("create")}</h3>
          </div>
          <div className="card-content">
            {error && (
              <div className="bg-destructive/10 border border-destructive/20 rounded-md p-3 mb-6">
                <p className="text-sm text-destructive">{error}</p>
              </div>
            )}

            <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">
                    {t("type")}
                  </label>
                  <select 
                    className="input" 
                    {...register("type")}
                  >
                    <option value={TransactionType.EXPENSE}>{t("expense")}</option>
                    <option value={TransactionType.INCOME}>{t("income")}</option>
                  </select>
                </div>

                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">
                    {t("date")}
                  </label>
                  <input 
                    type="datetime-local" 
                    className="input" 
                    {...register("occurredAt")}
                    autoComplete="off"
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                <div className="md:col-span-2 space-y-2">
                  <label className="text-sm font-medium text-foreground">
                    {t("amount")}
                  </label>
                  <input 
                    type="number" 
                    step="0.01" 
                    className="input" 
                    placeholder="0.00"
                    {...register("amount")}
                    autoComplete="off"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">
                    {t("currency")}
                  </label>
                  <input 
                    className="input" 
                    placeholder="RUB"
                    {...register("currencyCode")}
                    autoComplete="off"
                  />
                </div>
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  {t("category")}
                </label>
                <select 
                  className="input" 
                  {...register("categoryId")}
                  disabled={categoriesLoading}
                >
                  <option value="">
                    {categoriesLoading ? t("loading") : t("noCategory")}
                  </option>
                  {(catData?.categories ?? []).map((c: any) => (
                    <option key={c?.id} value={c?.id}>{c?.code}</option>
                  ))}
                </select>
                {categoriesLoading && (
                  <p className="text-xs text-muted-foreground">
                    {t("loadingCategories")}
                  </p>
                )}
                {categoryChanged && !categoriesLoading && (
                  <p className="text-xs text-blue-600 dark:text-blue-400">
                    {t("categoryChanged")}
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  {t("comment")}
                </label>
                <input 
                  className="input" 
                  placeholder={t("comment")}
                  {...register("comment")}
                  autoComplete="off"
                />
              </div>

              <div className="flex gap-3 pt-4">
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="btn btn-primary"
                >
                  {isSubmitting ? (
                    <div className="flex items-center space-x-2">
                      <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></div>
                      <span>{tc("saving")}</span>
                    </div>
                  ) : (
                    <span>{t("create")}</span>
                  )}
                </button>
                <Link href="/transactions" className="btn btn-outline">
                  {tc("cancel")}
                </Link>
              </div>
            </form>
          </div>
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


