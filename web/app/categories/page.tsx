"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
// enums are numeric in TS output; 2 = EXPENSE, 1 = INCOME

function CategoriesInner() {
  const { category } = useClients();
  const qc = useQueryClient();
  const [kind, setKind] = useState<number>(2);
  const { data, error, isLoading } = useQuery({
    queryKey: ["categories", kind],
    queryFn: async () => (await category.listCategories({ kind } as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const [code, setCode] = useState("");
  const createMut = useMutation({
    mutationFn: async () =>
      await category.createCategory({ kind, code, isActive: true } as any),
    onSuccess: () => {
      setCode("");
      qc.invalidateQueries({ queryKey: ["categories"] });
    },
  });

  const deleteMut = useMutation({
    mutationFn: async (id: string) =>
      await category.deleteCategory({ id } as any),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["categories"] }),
  });
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold mb-2">Categories</h1>
      {isLoading && <div className="text-sm text-gray-500">Loading...</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}

      <form
        className="flex gap-2 items-end mb-4"
        onSubmit={(e) => {
          e.preventDefault();
          createMut.mutate();
        }}
      >
        <div>
          <label className="block text-xs">View</label>
          <select
            className="border rounded px-2 py-1"
            value={String(kind)}
            onChange={(e) => setKind(Number(e.target.value))}
          >
            <option value={2}>Expense</option>
            <option value={1}>Income</option>
          </select>
        </div>
        <div>
          <label className="block text-xs">Code</label>
          <input
            className="border rounded px-2 py-1"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            required
          />
        </div>
        <div className="hidden">{/* keep kind hidden for create */}
          <label className="block text-xs">Kind</label>
          <select className="border rounded px-2 py-1" value={String(kind)} onChange={(e) => setKind(Number(e.target.value))}>
            <option value={2}>Expense</option>
            <option value={1}>Income</option>
          </select>
        </div>
        <button className="bg-black text-white rounded px-3 py-1" disabled={createMut.isPending}>
          {createMut.isPending ? "Adding..." : "Add"}
        </button>
      </form>
      <ul className="list-disc pl-5">
        {(data?.items ?? []).map((c: any, i: number) => (
          <li key={i} className="flex items-center gap-2">
            <span className="min-w-20 text-sm">{c?.code ?? "unknown"}</span>
            <span className="text-xs text-gray-500">{c?.kind}</span>
            <button
              className="ml-2 text-xs text-red-600 underline"
              onClick={() => deleteMut.mutate(c?.id as string)}
            >
              Delete
            </button>
          </li>
        ))}
      </ul>
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


