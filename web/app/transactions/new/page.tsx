"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";

const schema = z.object({
  type: z.enum(["TRANSACTION_TYPE_EXPENSE", "TRANSACTION_TYPE_INCOME"]),
  amount: z.coerce.number().min(0.01),
  currencyCode: z.string().min(3),
  occurredAt: z.string(),
  categoryId: z.string().optional(),
  comment: z.string().optional(),
});
type FormValues = z.infer<typeof schema>;

function TxForm() {
  const { transaction } = useClients();
  const { register, handleSubmit, formState, reset } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      type: "TRANSACTION_TYPE_EXPENSE",
      currencyCode: "RUB",
      occurredAt: new Date().toISOString(),
    },
  });
  const [error, setError] = useState<string | null>(null);
  const [ok, setOk] = useState(false);
  const onSubmit = async (v: FormValues) => {
    setError(null);
    setOk(false);
    try {
      await transaction.createTransaction({
        transaction: {
          type: v.type,
          amount: { currencyCode: v.currencyCode, minorUnits: Math.round(v.amount * 100) },
          occurredAt: { seconds: Math.floor(new Date(v.occurredAt).getTime() / 1000) },
          categoryId: v.categoryId,
          comment: v.comment,
        },
      } as any);
      setOk(true);
      reset();
    } catch (e: any) {
      setError(e?.message ?? "failed");
    }
  };
  return (
    <div className="p-6 max-w-md mx-auto">
      <h1 className="text-2xl font-semibold mb-3">New Transaction</h1>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-3">
        <div>
          <label className="block text-sm">Type</label>
          <select className="border rounded px-2 py-1 w-full" {...register("type")}>
            <option value="TRANSACTION_TYPE_EXPENSE">Expense</option>
            <option value="TRANSACTION_TYPE_INCOME">Income</option>
          </select>
        </div>
        <div className="flex gap-2">
          <div className="flex-1">
            <label className="block text-sm">Amount</label>
            <input type="number" step="0.01" className="border rounded px-2 py-1 w-full" {...register("amount")} />
          </div>
          <div>
            <label className="block text-sm">Currency</label>
            <input className="border rounded px-2 py-1 w-24" {...register("currencyCode")} />
          </div>
        </div>
        <div>
          <label className="block text-sm">Occurred At</label>
          <input className="border rounded px-2 py-1 w-full" {...register("occurredAt")} />
        </div>
        <div>
          <label className="block text-sm">Category ID</label>
          <input className="border rounded px-2 py-1 w-full" {...register("categoryId")} />
        </div>
        <div>
          <label className="block text-sm">Comment</label>
          <input className="border rounded px-2 py-1 w-full" {...register("comment")} />
        </div>
        {formState.errors.root && <div className="text-sm text-red-600">{formState.errors.root.message}</div>}
        {error && <div className="text-sm text-red-600">{error}</div>}
        {ok && <div className="text-sm text-green-700">Saved</div>}
        <button className="bg-black text-white rounded px-3 py-1">Create</button>
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


