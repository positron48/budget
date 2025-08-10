"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

function CategoriesInner() {
  const { category } = useClients();
  const qc = useQueryClient();
  const { data, error, isLoading } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => (await category.listCategories({} as any)) as any,
  });

  const [code, setCode] = useState("");
  const [kind, setKind] = useState("CATEGORY_KIND_EXPENSE");
  const createMut = useMutation({
    mutationFn: async () =>
      await category.createCategory({ category: { code, kind } } as any),
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
          <label className="block text-xs">Code</label>
          <input
            className="border rounded px-2 py-1"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            required
          />
        </div>
        <div>
          <label className="block text-xs">Kind</label>
          <select
            className="border rounded px-2 py-1"
            value={kind}
            onChange={(e) => setKind(e.target.value)}
          >
            <option value="CATEGORY_KIND_EXPENSE">Expense</option>
            <option value="CATEGORY_KIND_INCOME">Income</option>
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


