"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMemo, useState } from "react";
import { TransactionType, CategoryKind } from "@/proto/budget/v1/common_pb";
import { useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";

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
  const router = useRouter();
  const t = useTranslations("transactions");
  const { register, handleSubmit, formState, reset } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      type: TransactionType.EXPENSE as unknown as number,
      currencyCode: "RUB",
      occurredAt: new Date().toISOString(),
    },
  });
  const [error, setError] = useState<string | null>(null);
  const [ok, setOk] = useState(false);
  const typeWatch = (formState as any).values?.type as number | undefined;
  const mappedKind = useMemo(() => (typeWatch === TransactionType.INCOME ? CategoryKind.INCOME : CategoryKind.EXPENSE), [typeWatch]);
  const { data: catData } = useQuery({
    queryKey: ["tx-new-categories", mappedKind],
    queryFn: async () => (await category.listCategories({ kind: mappedKind } as any)) as any,
    staleTime: 30_000,
  });
  const onSubmit = async (v: FormValues) => {
    setError(null);
    setOk(false);
    try {
      const payload: any = {
        type: Number(v.type),
        amount: { currencyCode: v.currencyCode, minorUnits: Math.round(v.amount * 100) },
        occurredAt: { seconds: Math.floor(new Date(v.occurredAt).getTime() / 1000) },
      };
      if (v.categoryId) payload.categoryId = v.categoryId;
      if (v.comment) payload.comment = v.comment;
      await transaction.createTransaction(payload as any);
      setOk(true);
      reset();
      router.push("/transactions");
    } catch (e: any) {
      setError(e?.message ?? "failed");
    }
  };
  return (
    <div className="p-6 max-w-md mx-auto">
      <h1 className="text-2xl font-semibold mb-3">{t("newTitle") ?? "New Transaction"}</h1>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-3">
        <div>
          <label className="block text-sm">{t("type")}</label>
          <select className="border rounded px-2 py-1 w-full" {...register("type")}>
            <option value={TransactionType.EXPENSE}>{t("expense") ?? "Expense"}</option>
            <option value={TransactionType.INCOME}>{t("income") ?? "Income"}</option>
          </select>
        </div>
        <div className="flex gap-2">
          <div className="flex-1">
            <label className="block text-sm">{t("amount")}</label>
            <input type="number" step="0.01" className="border rounded px-2 py-1 w-full" {...register("amount")} />
          </div>
          <div>
            <label className="block text-sm">{t("currency") ?? "Currency"}</label>
            <input className="border rounded px-2 py-1 w-24" {...register("currencyCode")} />
          </div>
        </div>
        <div>
          <label className="block text-sm">{t("occurredAt") ?? "Occurred At"}</label>
          <input className="border rounded px-2 py-1 w-full" {...register("occurredAt")} />
        </div>
        <div>
          <label className="block text-sm">{t("category") ?? "Category"}</label>
          <select className="border rounded px-2 py-1 w-full" {...register("categoryId")}>
            <option value="">—</option>
            {(catData?.categories ?? []).map((c: any) => (
              <option key={c?.id} value={c?.id}>{c?.code}</option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-sm">{t("comment")}</label>
          <input className="border rounded px-2 py-1 w-full" {...register("comment")} />
        </div>
        {formState.errors.root && <div className="text-sm text-red-600">{formState.errors.root.message}</div>}
        {error && <div className="text-sm text-red-600">{error}</div>}
        {ok && <div className="text-sm text-green-700">{t("saved") ?? "Saved"}</div>}
        <button className="bg-black text-white rounded px-3 py-1">{t("create") ?? "Create"}</button>
      </form>
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


