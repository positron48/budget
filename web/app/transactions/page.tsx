"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { TransactionType } from "@/proto/budget/v1/common_pb";
import { useTranslations } from "next-intl";

function TransactionsInner() {
  const { transaction } = useClients();
  const t = useTranslations("transactions");
  const tc = useTranslations("common");
  const qc = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [type, setType] = useState<number>(0);
  const [from, setFrom] = useState<string>("");
  const [to, setTo] = useState<string>("");
  const [search, setSearch] = useState<string>("");
  const [categoryIds, setCategoryIds] = useState<string>("");
  const request = useMemo(() => {
    const req: any = { page: { page, pageSize } };
    if (type) req.type = type;
    if (from || to) {
      req.dateRange = {};
      if (from) req.dateRange.from = { seconds: Math.floor(new Date(from).getTime() / 1000) };
      if (to) req.dateRange.to = { seconds: Math.floor(new Date(to).getTime() / 1000) };
    }
    if (search) req.search = search;
    const ids = categoryIds
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    if (ids.length) req.categoryIds = ids;
    return req;
  }, [page, pageSize, type, from, to, search, categoryIds]);
  const { data, isLoading, error } = useQuery({
    queryKey: ["transactions", request],
    queryFn: async () => (await transaction.listTransactions(request as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const deleteMut = useMutation({
    mutationFn: async (id: string) => await transaction.deleteTransaction({ id } as any),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["transactions"] }),
  });
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold mb-2">{t("title")}</h1>
      <div className="flex gap-3 items-end mb-3 flex-wrap">
        <div>
          <label className="block text-xs">{t("type")}</label>
          <select className="border rounded px-2 py-1" value={String(type)} onChange={(e) => setType(Number(e.target.value))}>
            <option value={0}>All</option>
            <option value={TransactionType.EXPENSE}>{t("expense") ?? "Expense"}</option>
            <option value={TransactionType.INCOME}>{t("income") ?? "Income"}</option>
          </select>
        </div>
        <div>
          <label className="block text-xs">{t("from")}</label>
          <input className="border rounded px-2 py-1" type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">{t("to")}</label>
          <input className="border rounded px-2 py-1" type="date" value={to} onChange={(e) => setTo(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">{t("categoryIds")}</label>
          <input className="border rounded px-2 py-1 w-60" value={categoryIds} onChange={(e) => setCategoryIds(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">{t("search")}</label>
          <input className="border rounded px-2 py-1 w-60" value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
        <div className="ml-auto flex items-center gap-2 text-sm">
          <button
            className="px-2 py-1 border rounded"
            disabled={page <= 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            {t("prev")}
          </button>
          <span>Page {page}</span>
          <button
            className="px-2 py-1 border rounded"
            disabled={Boolean(data?.page) && page >= (data?.page?.totalPages ?? page)}
            onClick={() => setPage((p) => p + 1)}
          >
            {t("next")}
          </button>
        </div>
      </div>
      {isLoading && <div className="text-sm text-gray-500">{tc("loading")}</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}
      <ListWithEdit
        items={data?.transactions ?? []}
        onDelete={(id: string) => deleteMut.mutate(id)}
        isDeleting={deleteMut.isPending}
        onChanged={() => qc.invalidateQueries({ queryKey: ["transactions"] })}
      />
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

function ListWithEdit({
  items,
  onDelete,
  isDeleting,
  onChanged,
}: {
  items: any[];
  onDelete: (id: string) => void;
  isDeleting: boolean;
  onChanged: () => void;
}) {
  const { transaction } = useClients();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editComment, setEditComment] = useState<string>("");
  const [editAmount, setEditAmount] = useState<number | "">("");
  const updateMut = useMutation({
    mutationFn: async (payload: { id: string; comment?: string; amountMinorUnits?: number }) => {
      const paths: string[] = [];
      const tx: any = {};
      if (payload.comment !== undefined) {
        paths.push("comment");
        tx.comment = payload.comment;
      }
      if (payload.amountMinorUnits !== undefined) {
        paths.push("amount");
        tx.amount = { minorUnits: payload.amountMinorUnits };
      }
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
  });
  return (
    <ul className="space-y-1">
      {items.map((t: any) => (
        <li key={t?.id} className="text-sm flex items-center gap-3">
          {t?.occurredAt?.seconds ? new Date(t.occurredAt.seconds * 1000).toISOString().slice(0, 10) : ""}
          {" — "}
          {t?.amount?.minorUnits ?? 0}
          {" "}
          {t?.amount?.currencyCode ?? ""}
          {editingId === t?.id ? (
            <>
              <input
                className="border rounded px-2 py-0.5 text-xs"
                 placeholder={t("comment")}
                value={editComment}
                onChange={(e) => setEditComment(e.target.value)}
              />
              <input
                className="border rounded px-2 py-0.5 text-xs w-24"
                 placeholder={t("amount")}
                type="number"
                step="1"
                value={editAmount}
                onChange={(e) => setEditAmount(e.target.value ? Number(e.target.value) : "")}
              />
              <button
                className="text-xs bg-black text-white rounded px-2 py-1"
                disabled={updateMut.isPending}
                onClick={() =>
                  updateMut.mutate({
                    id: t.id as string,
                    comment: editComment,
                    amountMinorUnits: typeof editAmount === "number" ? editAmount : undefined,
                  })
                }
              >
                {updateMut.isPending ? t("saving") : t("edit")}
              </button>
              <button className="text-xs underline" onClick={() => setEditingId(null)}>
                Cancel
              </button>
            </>
          ) : (
            <>
              <span className="text-xs text-gray-500">{t?.comment ?? ""}</span>
              <button
                className="text-xs underline"
                onClick={() => {
                  setEditingId(t?.id);
                  setEditComment(t?.comment ?? "");
                  setEditAmount(t?.amount?.minorUnits ?? "");
                }}
              >
                {t("edit")}
              </button>
            </>
          )}
          <button
            className="ml-auto text-xs text-red-600 underline"
            disabled={isDeleting}
            onClick={() => onDelete(t?.id)}
          >
            {t("delete")}
          </button>
        </li>
      ))}
    </ul>
  );
}

