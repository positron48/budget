"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useQuery } from "@tanstack/react-query";

function CategoriesInner() {
  const { category } = useClients();
  const { data, error, isLoading } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => (await category.listCategories({} as any)) as any,
  });
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold mb-2">Categories</h1>
      {isLoading && <div className="text-sm text-gray-500">Loading...</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}
      <ul className="list-disc pl-5">
        {(data?.items ?? []).map((c: any, i: number) => (
          <li key={i}>{(c as any).code ?? "unknown"}</li>
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


