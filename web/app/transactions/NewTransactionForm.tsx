"use client";

import { forwardRef, useImperativeHandle, useMemo, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/app/providers";
import { TransactionType, CategoryKind } from "@/proto/budget/v1/common_pb";
import { Icon, CategorySingleInput, Select } from "@/components";
import AssetAccountSelect from "@/components/assets/AssetAccountSelect";
import { parseAssetAmount, localAssetTime } from "@/lib/assets";
import { useTranslations } from "next-intl";

const CURRENCIES = ["RUB", "USD", "EUR", "GBP", "KZT", "CNY", "TRY", "GEL", "AMD", "RSD"] as const;
const CURRENCY_SYMBOLS: Record<string, string> = {
  RUB: "₽", USD: "$", EUR: "€", GBP: "£", KZT: "₸", CNY: "¥", TRY: "₺", GEL: "₾", AMD: "֏", RSD: "дин",
};

const schema = z.object({
  type: z.number().int().min(1).max(2),
  amount: z.number().min(0.01),
  currencyCode: z.string().min(3),
  occurredAt: z.string(),
  categoryId: z.string().min(1),
  comment: z.string().optional(),
  isExtraordinary: z.boolean().optional(),
  assetAccountId: z.string().optional(),
});
type FormValues = z.infer<typeof schema>;

export interface NewTxFormRef {
  submit: () => void;
  submitAndAddMore: () => void;
}

interface Props {
  onClose: () => void;
  onSaved: () => void;
  initialAccountId?: string;
  initialCurrency?: string;
  initialType?: number;
}

const NewTransactionForm = forwardRef<NewTxFormRef, Props>(function NewTransactionForm({ onClose, onSaved, initialAccountId, initialCurrency, initialType }: Props, ref) {
  const { transaction, category } = useClients();
  const t = useTranslations("transactions");
  const ta = useTranslations("assets");
  const requestKey = useRef<string>(crypto.randomUUID());
  const [submitError, setSubmitError] = useState("");
  const qc = useQueryClient();


  const { register, handleSubmit, reset, watch, setValue, getValues, formState: { errors } } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      type: initialType ?? TransactionType.EXPENSE,
      currencyCode: initialCurrency || "RUB",
      assetAccountId: initialAccountId || "",
      occurredAt: localAssetTime(),
      isExtraordinary: false,
    },
  });
  // removed saving state
  const amountRef = useRef<HTMLInputElement>(null);
  const [amountInput, setAmountInput] = useState<string>("");

  const typeWatch = watch("type");
  const typeValue = useMemo(() => Number(typeWatch), [typeWatch]);
  const currencyWatch = watch("currencyCode") || "RUB";
  const currencySymbol = CURRENCY_SYMBOLS[currencyWatch] ?? "";
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
        amount: { currencyCode: v.currencyCode, minorUnits: parseAssetAmount(amountInput || String(v.amount), true) },
        occurredAt: { seconds: Math.floor(new Date(v.occurredAt).getTime() / 1000) },
      };
      if (v.categoryId) payload.categoryId = v.categoryId;
      if (v.comment) payload.comment = v.comment;
      payload.isExtraordinary = Boolean(v.isExtraordinary);
      payload.assetAccountId = v.assetAccountId || "";
      payload.requestKey = requestKey.current;
      await transaction.createTransaction(payload as any);
      qc.invalidateQueries({ queryKey: ["transactions"] });
      qc.invalidateQueries({ queryKey: ["assets"] });
      requestKey.current = crypto.randomUUID();
  }, [transaction, qc, amountInput]);

  const onSubmit = useMemo(() => {
    return async (v: FormValues) => {
      setSubmitError("");
      try { await submitInternal(v); onSaved(); onClose(); } catch (e) { setSubmitError((e as Error).message === "amount" ? ta("errors.amount") : t("updateError")); }
    };
  }, [submitInternal, onSaved, onClose, ta, t]);

  const onSubmitAndAddMore = useMemo(() => {
    return async (v: FormValues) => {
      setSubmitError("");
      try { await submitInternal(v); } catch (e) { setSubmitError((e as Error).message === "amount" ? ta("errors.amount") : t("updateError")); return; }
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
  }, [submitInternal, getValues, reset, ta, t]);

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

  const baseLabelClass = "text-xs font-medium text-muted-foreground";
  const baseInputClass =
    "w-full px-3 py-2 rounded-md border border-border bg-secondary/60 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/40 focus:bg-background transition-colors";

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-2">
      {/* Тип */}
      <div className="space-y-1">
        <label className={baseLabelClass}>{t("type")}</label>
        <div className="grid grid-cols-2 gap-2">
          <label
            className={`cursor-pointer rounded-md border p-2 text-sm transition-colors ${
              typeValue === TransactionType.EXPENSE
                ? "border-[hsl(var(--negative))] bg-[hsl(var(--negative)/0.12)]"
                : "border-border bg-secondary/50 hover:bg-secondary/70"
            }`}
          >
            <input type="radio" className="sr-only" checked={typeValue === TransactionType.EXPENSE} onChange={() => setValue('type', TransactionType.EXPENSE)} />
            <div className="flex items-center gap-2">
              <span className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-[hsl(var(--negative)/0.2)]">
                <Icon name="trending-down" size={14} className="text-[hsl(var(--negative))]" />
              </span>
              <span>{t("expense")}</span>
            </div>
          </label>
          <label
            className={`cursor-pointer rounded-md border p-2 text-sm transition-colors ${
              typeValue === TransactionType.INCOME
                ? "border-[hsl(var(--positive))] bg-[hsl(var(--positive)/0.12)]"
                : "border-border bg-secondary/50 hover:bg-secondary/70"
            }`}
          >
            <input type="radio" className="sr-only" checked={typeValue === TransactionType.INCOME} onChange={() => setValue('type', TransactionType.INCOME)} />
            <div className="flex items-center gap-2">
              <span className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-[hsl(var(--positive)/0.2)]">
                <Icon name="trending-up" size={14} className="text-[hsl(var(--positive))]" />
              </span>
              <span>{t("income")}</span>
            </div>
          </label>
        </div>
      </div>

      {/* Дата */}
      <div className="space-y-1">
        <label className={baseLabelClass}>{t("date")}</label>
        <input type="datetime-local" step="1" className={baseInputClass} {...register('occurredAt')} />
      </div>

      {/* Сумма/валюта */}
      <div className="space-y-1">
        <label className={baseLabelClass}>{t("amount")}</label>
        <div className="grid grid-cols-3 gap-2">
          <div className="col-span-2">
            <div className="relative">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground">{currencySymbol}</span>
              <input
                ref={amountRef}
                type="text"
                inputMode="decimal"
                className={`${baseInputClass} pl-8 font-medium`}
                placeholder="0,00"
                value={amountInput}
                onChange={(e) => handleAmountChange(e.target.value)}
                autoComplete="off"
              />
              {/* hidden input registered with RHF to carry numeric value */}
              <input ref={hiddenAmountRef} type="hidden" />
            </div>
            {errors.amount && <p className="text-xs text-[hsl(var(--negative))] mt-0.5">{errors.amount.message}</p>}
          </div>
          <Select
            value={currencyWatch}
            onChange={(v) => { setValue('currencyCode', String(v), { shouldDirty: true, shouldValidate: true }); setValue('assetAccountId', ""); }}
            options={CURRENCIES.map((c) => ({ value: c, label: c }))}
          />
        </div>
      </div>

      <AssetAccountSelect value={watch("assetAccountId") || ""} onChange={value => setValue("assetAccountId", value)} currency={currencyWatch} />

      {/* Категория */}
      <div className="space-y-1">
        <label className={baseLabelClass}>{t("category")}</label>
        {categoriesLoading ? (
          <div className="w-full px-3 py-2 border border-border rounded-md bg-secondary/60 text-muted-foreground text-sm">{t("loadingCategories")}</div>
        ) : (
          <CategorySingleInput
            categories={catData?.categories || []}
            value={watch('categoryId') || null}
            onChange={(id) => setValue('categoryId', id ?? '', { shouldDirty: true })}
            placeholder={t("categoryPlaceholderSingle") as string}
          />
        )}
      </div>

      {errors.categoryId && <p role="alert" className="text-xs text-[hsl(var(--negative))]">{t("categoryRequired")}</p>}

      {/* Комментарий */}
      <div className="space-y-1">
        <label className={baseLabelClass}>{t("comment")}</label>
        <textarea rows={2} className={baseInputClass} placeholder={t("noComment") as string} {...register('comment')} />
      </div>

      <label className="flex cursor-pointer items-start gap-3 rounded-md border border-border bg-secondary/40 p-2 text-sm text-foreground">
        <input type="checkbox" className="mt-0.5 h-4 w-4 accent-[hsl(var(--primary))]" {...register('isExtraordinary')} />
        <span>
          <span className="block font-medium">{t("extraordinary")}</span>
          <span className="block text-xs text-muted-foreground">{t("extraordinaryHint")}</span>
        </span>
      </label>

      {/* Footer buttons supplied by parent if needed */}

      {submitError && <p role="alert" className="text-sm text-[hsl(var(--negative))]">{submitError}</p>}
      <div className="hidden" />
    </form>
  );
});

export default NewTransactionForm;

