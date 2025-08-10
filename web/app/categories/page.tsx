"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslations } from "next-intl";
import { EmptyState, LoadingSpinner, Icon, Button, Badge } from "@/components";

// enums are numeric in TS output; 2 = EXPENSE, 1 = INCOME

function CategoriesInner() {
  const { category } = useClients();
  const qc = useQueryClient();
  const t = useTranslations("categories");
  const tc = useTranslations("common");
  const [kind, setKind] = useState<number>(2);
  const [includeInactive, setIncludeInactive] = useState<boolean>(false);
  const [showCreateForm, setShowCreateForm] = useState(false);
  
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
      setShowCreateForm(false);
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
    <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-8">
        <div>
          <h1 className="text-3xl font-bold text-foreground">{t("title")}</h1>
          <p className="text-muted-foreground mt-1">{t("description")}</p>
        </div>
        <Button
          onClick={() => setShowCreateForm(!showCreateForm)}
          icon="plus"
        >
          {t("create")}
        </Button>
      </div>

      {/* Filters */}
      <div className="card mb-6">
        <div className="card-content">
          <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-end">
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground flex items-center space-x-2">
                <Icon name="filter" size={16} className="text-muted-foreground" />
                <span>{t("view")}</span>
              </label>
              <select
                className="input"
                value={String(kind)}
                onChange={(e) => setKind(Number(e.target.value))}
              >
                <option value={2}>{t("expense")}</option>
                <option value={1}>{t("income")}</option>
              </select>
            </div>
            
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={includeInactive}
                onChange={(e) => setIncludeInactive(e.target.checked)}
                className="rounded border-gray-300"
              />
              {t("includeInactive")}
            </label>
          </div>
        </div>
      </div>

      {/* Create Form */}
      {showCreateForm && (
        <div className="card mb-6">
          <div className="card-header">
            <h3 className="card-title flex items-center space-x-2">
              <Icon name="plus" size={20} />
              <span>{t("create")}</span>
            </h3>
          </div>
          <div className="card-content">
            <form
              className="flex flex-col sm:flex-row gap-4 items-end"
              onSubmit={(e) => {
                e.preventDefault();
                createMut.mutate();
              }}
            >
              <div className="flex-1 space-y-2">
                <label className="text-sm font-medium text-foreground">
                  {t("code")}
                </label>
                <input
                  className="input"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  placeholder={t("code")}
                  required
                  autoComplete="off"
                />
              </div>
              <div className="flex gap-2">
                <Button
                  type="submit"
                  loading={createMut.isPending}
                  icon="check"
                >
                  {createMut.isPending ? tc("saving") : t("save")}
                </Button>
                <Button
                  type="button"
                  onClick={() => setShowCreateForm(false)}
                  variant="outline"
                >
                  {tc("cancel")}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Content */}
      {isLoading && (
        <LoadingSpinner text={tc("loading")} className="py-12" />
      )}

      {error && (
        <div className="card border-destructive/20 bg-destructive/5">
          <div className="card-content">
            <p className="text-destructive flex items-center space-x-2">
              <Icon name="alert-circle" size={16} />
              <span>{(error as any).message}</span>
            </p>
          </div>
        </div>
      )}

      {!isLoading && !error && data?.categories?.length === 0 && (
        <EmptyState
          icon="categories"
          title={t("noCategories")}
          description={t("noCategoriesDescription")}
          action={{
            label: t("create"),
            href: "#",
          }}
        />
      )}

      {!isLoading && !error && data?.categories?.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {data.categories.map((c: any) => (
            <div key={c?.id} className="card hover:shadow-md transition-shadow">
              <div className="card-content">
                <div className="flex items-center justify-between">
                  <div className="flex items-center space-x-3">
                    <div className={`w-10 h-10 rounded-full flex items-center justify-center ${
                      c?.kind === 2 ? 'bg-red-100 text-red-600 dark:bg-red-950/20' : 'bg-green-100 text-green-600 dark:bg-green-950/20'
                    }`}>
                      <Icon 
                        name={c?.kind === 2 ? "trending-down" : "trending-up"} 
                        size={20} 
                      />
                    </div>
                    <div>
                      <div className="font-medium text-foreground">
                        {c?.code}
                      </div>
                      <div className="text-sm text-muted-foreground">
                        {c?.kind === 2 ? t("expense") : t("income")}
                      </div>
                    </div>
                  </div>
                  
                  <div className="flex items-center space-x-2">
                    <Badge variant={c?.isActive ? "default" : "secondary"}>
                      {c?.isActive ? t("active") : t("inactive")}
                    </Badge>
                    
                    {editingId === c?.id ? (
                      <div className="flex items-center space-x-2">
                        <input
                          className="input w-24"
                          value={editCode}
                          onChange={(e) => setEditCode(e.target.value)}
                          placeholder={t("code")}
                          autoComplete="off"
                        />
                        <Button
                          size="sm"
                          loading={updateMut.isPending}
                          icon="check"
                          onClick={() =>
                            updateMut.mutate({
                              id: c.id as string,
                              code: editCode,
                              isActive: editActive,
                            })
                          }
                        >
                          {updateMut.isPending ? tc("saving") : tc("save")}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setEditingId(null)}
                        >
                          {tc("cancel")}
                        </Button>
                      </div>
                    ) : (
                      <div className="flex items-center space-x-2">
                        <Button
                          size="sm"
                          variant="outline"
                          icon="edit"
                          onClick={() => startEdit(c)}
                        >
                          {tc("edit")}
                        </Button>
                        <Button
                          size="sm"
                          variant="destructive"
                          icon="trash"
                          loading={deleteMut.isPending}
                          onClick={() => deleteMut.mutate(c?.id)}
                        >
                          {tc("delete")}
                        </Button>
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
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


