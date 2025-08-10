"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
// enums are numeric in TS output; 2 = EXPENSE, 1 = INCOME

function CategoriesInner() {
  const { category } = useClients();
  const qc = useQueryClient();
  const [kind, setKind] = useState<number>(2);
  const [includeInactive, setIncludeInactive] = useState<boolean>(false);
  const { data, error, isLoading } = useQuery({
    queryKey: ["categories", kind, includeInactive],
    queryFn: async () =>
      (await category.listCategories({ kind, includeInactive } as any)) as any,
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
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editCode, setEditCode] = useState<string>("");
  const [editActive, setEditActive] = useState<boolean>(true);
  const updateMut = useMutation({
    mutationFn: async (payload: { id: string; code: string; isActive: boolean }) =>
      await category.updateCategory(payload as any),
    onSuccess: () => {
      setEditingId(null);
      qc.invalidateQueries({ queryKey: ["categories"] });
    },
  });
  const startEdit = (c: any) => {
    setEditingId(c?.id);
    setEditCode(c?.code ?? "");
    setEditActive(Boolean(c?.isActive));
  };
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
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={includeInactive}
            onChange={(e) => setIncludeInactive(e.target.checked)}
          />
          Include inactive
        </label>
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
        {(data?.categories ?? []).map((c: any) => (
          <li key={c?.id ?? c?.code} className="flex items-center gap-3 py-1">
            {editingId === c?.id ? (
              <>
                <input
                  className="border rounded px-2 py-1 text-sm"
                  value={editCode}
                  onChange={(e) => setEditCode(e.target.value)}
                />
                <label className="flex items-center gap-2 text-xs">
                  <input
                    type="checkbox"
                    checked={editActive}
                    onChange={(e) => setEditActive(e.target.checked)}
                  />
                  Active
                </label>
                <button
                  className="text-xs bg-black text-white rounded px-2 py-1"
                  disabled={updateMut.isPending}
                  onClick={() => updateMut.mutate({ id: editingId!, code: editCode, isActive: editActive })}
                >
                  {updateMut.isPending ? "Saving..." : "Save"}
                </button>
                <button
                  className="text-xs underline"
                  onClick={() => setEditingId(null)}
                >
                  Cancel
                </button>
              </>
            ) : (
              <>
                <span className="min-w-20 text-sm">{c?.code ?? "unknown"}</span>
                <span className="text-xs text-gray-500">
                  {(c?.kind === 2 && "Expense") || (c?.kind === 1 && "Income") || "Unspecified"}
                </span>
                <span className={`text-[10px] px-2 py-0.5 rounded border ${c?.isActive ? "border-green-500 text-green-600" : "border-gray-400 text-gray-500"}`}>
                  {c?.isActive ? "active" : "inactive"}
                </span>
                <button
                  className="text-xs underline"
                  onClick={() => startEdit(c)}
                >
                  Edit
                </button>
                <button
                  className="ml-2 text-xs text-red-600 underline"
                  onClick={() => deleteMut.mutate(c?.id as string)}
                >
                  Delete
                </button>
              </>
            )}
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


