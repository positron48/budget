"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { authStore } from "@/lib/auth/store";
import { useTranslations } from "next-intl";
import { Button, Card, CardContent, CardHeader, CardTitle, CardDescription, Icon, Modal } from "@/components";

function AccountInner() {
  const { tenant } = useClients();
  const qc = useQueryClient();
  const t = useTranslations("tenants");
  const tc = useTranslations("common");

  const { data, isLoading, error } = useQuery({
    queryKey: ["tenants"],
    queryFn: async () => (await tenant.listMyTenants({} as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const memberships = (data?.memberships ?? []) as any[];

  const [name, setName] = useState("");
  const [currency, setCurrency] = useState("RUB");
  const [slug, setSlug] = useState("");

  // Invite modal state (UI only; backend not implemented yet)
  const [inviteOpen, setInviteOpen] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<"owner" | "admin" | "member">("member");

  const createMut = useMutation({
    mutationFn: async () =>
      await tenant.createTenant({ name, defaultCurrencyCode: currency, slug: slug || undefined } as any),
    onSuccess: () => {
      setName("");
      setSlug("");
      qc.invalidateQueries({ queryKey: ["tenants"] });
    },
  });

  const currentTenantId = authStore.getTenant();
  const makeActive = (id: string) => {
    authStore.set({ tenantId: id });
    if (typeof window !== "undefined") {
      window.location.reload();
    }
  };

  const roleLabel = (role: number) => {
    switch (role) {
      case 1:
        return t("roles.owner");
      case 2:
        return t("roles.admin");
      case 3:
        return t("roles.member");
      default:
        return t("roles.unknown");
    }
  };

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
        {/* Header */}
        <div>
          <h1 className="text-xl font-bold text-foreground mb-1">{t("title")}</h1>
          <p className="text-sm text-muted-foreground">{t("description")}</p>
        </div>

        {/* Info Block */}
        <Card className="bg-white dark:bg-gray-900/40 border border-gray-200 dark:border-gray-800">
          <CardHeader>
            <div className="flex items-start gap-3">
              <div className="mt-0.5 w-8 h-8 rounded-lg bg-blue-100 dark:bg-blue-900/20 flex items-center justify-center">
                <Icon name="info" size={16} className="text-blue-600 dark:text-blue-400" />
              </div>
              <div>
                <CardTitle className="text-base">{t("infoTitle")}</CardTitle>
                <CardDescription className="text-sm !mt-1">{t("infoWhat")}</CardDescription>
              </div>
            </div>
          </CardHeader>
          <CardContent className="pt-0">
            <div className="text-sm text-muted-foreground">
              <div className="font-medium text-foreground mb-2">{t("infoCan")}</div>
              <ul className="list-disc pl-5 space-y-1">
                <li>{t("actionCreate")}</li>
                <li>{t("actionSwitch")}</li>
                <li>{t("actionViewRoles")}</li>
              </ul>
              {/* current account id hidden per requirement */}
            </div>
          </CardContent>
        </Card>

        {/* Create Account */}
        <Card className="bg-white dark:bg-gray-900/40 border border-gray-200 dark:border-gray-800">
          <CardHeader>
            <CardTitle className="text-base">{t("create")}</CardTitle>
            <CardDescription className="text-sm">{t("createDescription")}</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                createMut.mutate();
              }}
              className="flex gap-3 items-end flex-wrap"
            >
              <div>
                <label className="block text-xs text-muted-foreground mb-1">{t("name")}</label>
                <input
                  className="border rounded px-2 py-1 bg-background"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                  autoComplete="organization"
                />
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1">{t("slugOptional")}</label>
                <input
                  className="border rounded px-2 py-1 bg-background"
                  value={slug}
                  onChange={(e) => setSlug(e.target.value)}
                  autoComplete="off"
                />
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1">{t("defaultCurrency")}</label>
                <input
                  className="border rounded px-2 py-1 w-24 bg-background"
                  value={currency}
                  onChange={(e) => setCurrency(e.target.value)}
                  autoComplete="off"
                />
              </div>
              <Button className="px-3 py-1" disabled={createMut.isPending} loading={createMut.isPending}>
                {createMut.isPending ? tc("saving") : t("createShort")}
              </Button>
            </form>
            {createMut.error && (
              <div className="mt-2 text-xs text-red-600">{(createMut.error as any).message}</div>
            )}
          </CardContent>
        </Card>

        {/* Accounts list */}
        <Card className="bg-white dark:bg-gray-900/40 border border-gray-200 dark:border-gray-800">
          <CardHeader>
            <div className="flex items-center justify-between gap-3">
              <div>
                <CardTitle className="text-base">{t("listTitle")}</CardTitle>
                <CardDescription className="text-sm">{t("listDescription")}</CardDescription>
              </div>
              <div className="flex items-center gap-2">
                {isLoading && (
                  <span className="text-xs text-muted-foreground">{tc("loading")}</span>
                )}
                <Button variant="outline" size="sm" icon="plus" onClick={() => setInviteOpen(true)}>
                  {t("invite")}
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            {error && (
              <div className="text-sm text-red-600 mb-3">{(error as any).message}</div>
            )}
            <ul className="space-y-2">
              {memberships.map((m) => (
                <li key={m?.tenant?.id} className="flex items-center gap-3 text-sm">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-6 h-6 rounded-md bg-indigo-100 dark:bg-indigo-900/20 text-indigo-600 dark:text-indigo-400 flex items-center justify-center">
                      <Icon name="wallet" size={12} />
                    </div>
                    <span className="font-medium truncate max-w-[200px]" title={m?.tenant?.name}>{m?.tenant?.name}</span>
                  </div>
                  <span className="text-muted-foreground truncate max-w-[160px]" title={m?.tenant?.slug}>{m?.tenant?.slug}</span>
                  <span className="text-muted-foreground">{m?.tenant?.defaultCurrencyCode}</span>
                  <span className="text-[11px] px-2 py-0.5 rounded border text-muted-foreground" title={`role: ${m?.role}`}>
                    {roleLabel(Number(m?.role))}
                  </span>
                  {currentTenantId === m?.tenant?.id ? (
                    <span className="ml-auto text-green-600 text-xs">{t("active")}</span>
                  ) : (
                    <Button
                      variant="outline"
                      size="sm"
                      className="ml-auto text-xs"
                      onClick={() => makeActive(m?.tenant?.id)}
                    >
                      {t("makeActive")}
                    </Button>
                  )}
                </li>
              ))}
              {memberships.length === 0 && !isLoading && (
                <div className="text-sm text-muted-foreground">{t("noTenants")}</div>
              )}
            </ul>
          </CardContent>
        </Card>

        <Modal
          open={inviteOpen}
          onClose={() => setInviteOpen(false)}
          title={t("inviteTitle") as string}
        
        >
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-muted-foreground mb-1">{t("inviteEmail")}</label>
              <input
                className="w-full border rounded px-2 py-1 bg-background"
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                placeholder="user@example.com"
              />
            </div>
            <div>
              <label className="block text-xs text-muted-foreground mb-1">{t("inviteRole")}</label>
              <select
                className="w-full border rounded px-2 py-1 bg-background"
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value as any)}
              >
                <option value="member">{t("roles.member")}</option>
                <option value="admin">{t("roles.admin")}</option>
                <option value="owner">{t("roles.owner")}</option>
              </select>
            </div>
            <div className="text-xs text-muted-foreground">
              {t("inviteNotImplemented")}
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button variant="ghost" onClick={() => setInviteOpen(false)}>{tc("cancel")}</Button>
              <Button onClick={() => setInviteOpen(false)}>{t("invite")}</Button>
            </div>
          </div>
        </Modal>
      </div>
    </div>
  );
}

export default function AccountPage() {
  return (
    <ClientsProvider>
      <AccountInner />
    </ClientsProvider>
  );
}


