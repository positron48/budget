"use client";

import { forwardRef, useImperativeHandle, useMemo, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/app/providers";
import { TransactionType, CategoryKind } from "@/proto/budget/v1/common_pb";
import { Icon, CategorySingleInput } from "@/components";
import { useTranslations } from "next-intl";

const schema = z.object({
  type: z.number().int().min(1).max(2),
  amount: z.number().min(0.01),
  currencyCode: z.string().min(3),
  occurredAt: z.string(),
  categoryId: z.string().optional(),
  comment: z.string().optional(),
});
type FormValues = z.infer<typeof schema>;

export interface NewTxFormRef {
  submit: () => void;
  submitAndAddMore: () => void;
}

interface Props {
  onClose: () => void;
  onSaved: () => void;
}

const NewTransactionForm = forwardRef<NewTxFormRef, Props>(function NewTransactionForm({ onClose, onSaved }: Props, ref) {
  const { transaction, category } = useClients();
  const t = useTranslations("transactions");
  const qc = useQueryClient();
  const toLocalInput = (d: Date) => {
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  };

  const { register, handleSubmit, reset, watch, setValue, getValues, formState: { errors } } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      type: TransactionType.EXPENSE,
      currencyCode: "RUB",
      occurredAt: toLocalInput(new Date()),
    },
  });
  // removed saving state
  const amountRef = useRef<HTMLInputElement>(null);
  const [amountInput, setAmountInput] = useState<string>("");

  const typeWatch = watch("type");
  const typeValue = useMemo(() => Number(typeWatch), [typeWatch]);
  const mappedKind = useMemo(() => (
    typeValue === TransactionType.INCOME ? CategoryKind.INCOME : CategoryKind.EXPENSE
  ), [typeValue]);

  const { data: catData, isLoading: categoriesLoading } = useQuery({
    queryKey: ["tx-new-categories", mappedKind],
    queryFn: async () => category.listCategories({ kind: mappedKind, includeInactive: false } as any),
    staleTime: 0,
  });

  const submitInternal = useMemo(() => async (v: FormValues) => {
      const payload: any = {
        type: Number(v.type),
        amount: { currencyCode: v.currencyCode, minorUnits: Math.round(v.amount * 100) },
        occurredAt: { seconds: Math.floor(new Date(v.occurredAt).getTime() / 1000) },
      };
      if (v.categoryId) payload.categoryId = v.categoryId;
      if (v.comment) payload.comment = v.comment;
      await transaction.createTransaction(payload as any);
      qc.invalidateQueries({ queryKey: ["transactions"] });
  }, [transaction, qc]);

  const onSubmit = useMemo(() => {
    return async (v: FormValues) => {
      await submitInternal(v);
      onSaved();
      onClose();
    };
  }, [submitInternal, onSaved, onClose]);

  const onSubmitAndAddMore = useMemo(() => {
    return async (v: FormValues) => {
      await submitInternal(v);
      const current = getValues();
      reset({
        ...current,
        amount: undefined as any,
        categoryId: "",
        comment: "",
      }, { keepDefaultValues: true });
      setAmountInput("");
      setTimeout(() => amountRef.current?.focus(), 0);
    };
  }, [submitInternal, getValues, reset]);

  // expose submit methods for parent modal footer buttons
  useImperativeHandle(ref, () => ({
    submit: handleSubmit(onSubmit),
    submitAndAddMore: handleSubmit(onSubmitAndAddMore),
  }), [handleSubmit, onSubmit, onSubmitAndAddMore]);

  // hidden registered input to keep RHF value; visible input is controlled
  const { ref: hiddenAmountRef } = register('amount');

  // removed unused formatWithSpaces

  const handleAmountChange = (raw: string) => {
    const noSpaces = raw.replace(/\s+/g, "");
    // allow only digits with optional decimal sep and up to 2 digits
    if (noSpaces === "" || /^[0-9]*[.,]?[0-9]{0,2}$/.test(noSpaces)) {
      // normalize decimal to comma for display
      const display = noSpaces.replace(".", ",");
      const formatted = display.includes(",")
        ? (() => {
            const [i, f] = display.split(",");
            return `${i.replace(/\B(?=(\d{3})+(?!\d))/g, " ")},${f}`;
          })()
        : display.replace(/\B(?=(\d{3})+(?!\d))/g, " ");
      setAmountInput(formatted);
      const asNumber = parseFloat(noSpaces.replace(/\s+/g, "").replace(",", "."));
      if (!isNaN(asNumber)) {
        setValue('amount', asNumber, { shouldDirty: true, shouldValidate: true });
      } else {
        setValue('amount', undefined as any, { shouldDirty: true, shouldValidate: true });
      }
    }
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-2">
      {/* Тип */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("type")}</label>
        <div className="grid grid-cols-2 gap-2">
          <label className={`cursor-pointer rounded-md border p-2 text-sm ${typeValue === TransactionType.EXPENSE ? 'border-red-400 bg-red-50 dark:bg-red-900/20' : 'border-slate-200 dark:border-slate-600'}`}>
            <input type="radio" className="sr-only" checked={typeValue === TransactionType.EXPENSE} onChange={() => setValue('type', TransactionType.EXPENSE)} />
            <div className="flex items-center gap-2">
              <span className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-red-100 dark:bg-red-900/30">
                <Icon name="trending-down" size={14} className="text-red-600" />
              </span>
              <span>{t("expense")}</span>
            </div>
          </label>
          <label className={`cursor-pointer rounded-md border p-2 text-sm ${typeValue === TransactionType.INCOME ? 'border-green-400 bg-green-50 dark:bg-green-900/20' : 'border-slate-200 dark:border-slate-600'}`}>
            <input type="radio" className="sr-only" checked={typeValue === TransactionType.INCOME} onChange={() => setValue('type', TransactionType.INCOME)} />
            <div className="flex items-center gap-2">
              <span className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-green-100 dark:bg-green-900/30">
                <Icon name="trending-up" size={14} className="text-green-600" />
              </span>
              <span>{t("income")}</span>
            </div>
          </label>
        </div>
      </div>

      {/* Дата */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("date")}</label>
        <input type="datetime-local" className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-sm" {...register('occurredAt')} />
      </div>

      {/* Сумма/валюта */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("amount")}</label>
        <div className="grid grid-cols-3 gap-2">
          <div className="col-span-2">
            <div className="relative">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400">₽</span>
              <input
                ref={amountRef}
                type="text"
                inputMode="decimal"
                className="w-full pl-7 pr-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-sm font-medium"
                placeholder="0,00"
                value={amountInput}
                onChange={(e) => handleAmountChange(e.target.value)}
                autoComplete="off"
              />
              {/* hidden input registered with RHF to carry numeric value */}
              <input ref={hiddenAmountRef} type="hidden" />
            </div>
            {errors.amount && <p className="text-xs text-red-600 mt-0.5">{errors.amount.message}</p>}
          </div>
          <input className="px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-sm text-center" placeholder={t("currency") as string} {...register('currencyCode')} />
        </div>
      </div>

      {/* Категория */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("category")}</label>
        {categoriesLoading ? (
          <div className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-slate-500 text-sm">{t("loadingCategories")}</div>
        ) : (
          <CategorySingleInput
            categories={catData?.categories || []}
            value={watch('categoryId') || null}
            onChange={(id) => setValue('categoryId', id ?? '', { shouldDirty: true })}
            placeholder={t("categoryIdsPlaceholder") as string}
          />
        )}
      </div>

      {/* Комментарий */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-slate-700 dark:text-slate-300">{t("comment")}</label>
        <textarea rows={2} className="w-full px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-md bg-white dark:bg-slate-700 text-sm" placeholder={t("noComment") as string} {...register('comment')} />
      </div>

      {/* Footer buttons supplied by parent if needed */}

      <div className="hidden" />
    </form>
  );
});

export default NewTransactionForm;


