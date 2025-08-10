"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { TransactionType } from "@/proto/budget/v1/common_pb";
import Link from "next/link";
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
  const [showFilters, setShowFilters] = useState(false);

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

  const clearFilters = () => {
    setType(0);
    setFrom("");
    setTo("");
    setSearch("");
    setCategoryIds("");
    setPage(1);
  };

  const hasActiveFilters = type || from || to || search || categoryIds;

  return (
    <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-8">
        <div>
          <h1 className="text-3xl font-bold text-foreground">{t("title")}</h1>
          <p className="text-muted-foreground mt-1">{t("description")}</p>
        </div>
        <Link href="/transactions/new" className="btn btn-primary">
          <span className="mr-2">➕</span>
          {t("create")}
        </Link>
      </div>

      {/* Filters */}
      <div className="card mb-6">
        <div className="card-header">
          <div className="flex items-center justify-between">
            <h3 className="card-title text-lg">{t("filters")}</h3>
            <button
              onClick={() => setShowFilters(!showFilters)}
              className="btn btn-ghost btn-sm"
            >
              {showFilters ? "🔽" : "🔼"} {t("toggleFilters")}
            </button>
          </div>
        </div>
        
        {showFilters && (
          <div className="card-content">
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  {t("type")}
                </label>
                <select 
                  className="input" 
                  value={String(type)} 
                  onChange={(e) => setType(Number(e.target.value))}
                >
                  <option value={0}>{t("allTypes")}</option>
                  <option value={TransactionType.EXPENSE}>{t("expense")}</option>
                  <option value={TransactionType.INCOME}>{t("income")}</option>
                </select>
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  {t("from")}
                </label>
                <input 
                  className="input" 
                  type="date" 
                  value={from} 
                  onChange={(e) => setFrom(e.target.value)} 
                />
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  {t("to")}
                </label>
                <input 
                  className="input" 
                  type="date" 
                  value={to} 
                  onChange={(e) => setTo(e.target.value)} 
                />
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  {t("search")}
                </label>
                <input 
                  className="input" 
                  value={search} 
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder={t("searchPlaceholder")}
                />
              </div>
            </div>

            <div className="mt-4 space-y-2">
              <label className="text-sm font-medium text-foreground">
                {t("categoryIds")}
              </label>
              <input 
                className="input" 
                value={categoryIds} 
                onChange={(e) => setCategoryIds(e.target.value)}
                placeholder={t("categoryIdsPlaceholder")}
              />
            </div>

            {hasActiveFilters && (
              <div className="mt-4 flex items-center gap-2">
                <button
                  onClick={clearFilters}
                  className="btn btn-outline btn-sm"
                >
                  🗑️ {t("clearFilters")}
                </button>
                <span className="text-sm text-muted-foreground">
                  {t("activeFilters")}
                </span>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Content */}
      {isLoading && (
        <div className="flex items-center justify-center py-12">
          <div className="flex items-center space-x-2">
            <div className="w-6 h-6 border-2 border-primary/30 border-t-primary rounded-full animate-spin"></div>
            <span className="text-muted-foreground">{tc("loading")}</span>
          </div>
        </div>
      )}

      {error && (
        <div className="card border-destructive/20 bg-destructive/5">
          <div className="card-content">
            <p className="text-destructive">{(error as any).message}</p>
          </div>
        </div>
      )}

      {!isLoading && !error && (
        <>
          <ListWithEdit
            items={data?.transactions ?? []}
            onDelete={(id: string) => deleteMut.mutate(id)}
            isDeleting={deleteMut.isPending}
            onChanged={() => qc.invalidateQueries({ queryKey: ["transactions"] })}
          />

          {/* Pagination */}
          {data?.page && (
            <div className="flex items-center justify-between mt-6">
              <div className="text-sm text-muted-foreground">
                {t("showing")} {((page - 1) * pageSize) + 1} - {Math.min(page * pageSize, Number(data.page.totalItems))} {t("of")} {Number(data.page.totalItems)} {t("transactions")}
              </div>
              <div className="flex items-center space-x-2">
                <button
                  className="btn btn-outline btn-sm"
                  disabled={page <= 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                >
                  ← {t("prev")}
                </button>
                <span className="px-3 py-1 text-sm bg-muted rounded">
                  {page} / {Number(data.page.totalPages)}
                </span>
                <button
                  className="btn btn-outline btn-sm"
                  disabled={page >= Number(data.page.totalPages)}
                  onClick={() => setPage((p) => p + 1)}
                >
                  {t("next")} →
                </button>
              </div>
            </div>
          )}
        </>
      )}
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
  const tt = useTranslations("transactions");
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

  if (items.length === 0) {
    return (
      <div className="text-center py-12">
        <div className="text-6xl mb-4">📭</div>
        <h3 className="text-lg font-medium text-foreground mb-2">{tt("noTransactions")}</h3>
        <p className="text-muted-foreground">{tt("noTransactionsDescription")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {items.map((tx: any) => (
        <div key={tx?.id} className="card hover:shadow-md transition-shadow">
          <div className="card-content">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-4">
                <div className={`w-10 h-10 rounded-full flex items-center justify-center ${
                  tx?.type === TransactionType.EXPENSE ? 'bg-red-100 text-red-600' : 'bg-green-100 text-green-600'
                }`}>
                  {tx?.type === TransactionType.EXPENSE ? '📤' : '📥'}
                </div>
                <div>
                  <div className="font-medium text-foreground">
                    {tx?.occurredAt?.seconds ? new Date(Number(tx.occurredAt.seconds) * 1000).toLocaleDateString() : ""}
                  </div>
                  <div className="text-sm text-muted-foreground">
                    {tx?.comment || tt("noComment")}
                  </div>
                </div>
              </div>
              
              <div className="flex items-center space-x-4">
                <div className="text-right">
                  <div className={`font-semibold ${
                    tx?.type === TransactionType.EXPENSE ? 'text-red-600' : 'text-green-600'
                  }`}>
                    {tx?.type === TransactionType.EXPENSE ? '-' : '+'}
                    {(Number(tx?.amount?.minorUnits ?? 0) / 100).toFixed(2)} {tx?.amount?.currencyCode}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {tx?.categoryId || tt("noCategory")}
                  </div>
                </div>

                {editingId === tx?.id ? (
                  <div className="flex items-center space-x-2">
                    <input
                      className="input w-32"
                      placeholder={tt("comment")}
                      value={editComment}
                      onChange={(e) => setEditComment(e.target.value)}
                    />
                    <input
                      className="input w-24"
                      placeholder={tt("amount")}
                      type="number"
                      step="0.01"
                      value={editAmount}
                      onChange={(e) => setEditAmount(e.target.value ? Number(e.target.value) * 100 : "")}
                    />
                    <button
                      className="btn btn-primary btn-sm"
                      disabled={updateMut.isPending}
                      onClick={() =>
                        updateMut.mutate({
                          id: tx.id as string,
                          comment: editComment,
                          amountMinorUnits: typeof editAmount === "number" ? editAmount : undefined,
                        })
                      }
                    >
                      {updateMut.isPending ? tt("saving") : tt("save")}
                    </button>
                    <button 
                      className="btn btn-ghost btn-sm" 
                      onClick={() => setEditingId(null)}
                    >
                      {tt("cancel")}
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center space-x-2">
                    <button
                      className="btn btn-outline btn-sm"
                      onClick={() => {
                        setEditingId(tx?.id);
                        setEditComment(tx?.comment ?? "");
                        setEditAmount(tx?.amount?.minorUnits ? Number(tx.amount.minorUnits) / 100 : "");
                      }}
                    >
                      ✏️ {tt("edit")}
                    </button>
                    <button
                      className="btn btn-destructive btn-sm"
                      disabled={isDeleting}
                      onClick={() => onDelete(tx?.id)}
                    >
                      🗑️ {tt("delete")}
                    </button>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

