"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { authStore } from "@/lib/auth/store";
import { useTranslations } from "next-intl";
import { Button, Card, CardContent, CardHeader, CardTitle, CardDescription, Icon, Modal } from "@/components";
import { normalizeApiErrorMessage } from "@/lib/api/errors";

function AccountInner() {
  const { tenant, user } = useClients();
  const qc = useQueryClient();
  const t = useTranslations("tenants");
  const tc = useTranslations("common");

  const { data, isLoading, error } = useQuery({
    queryKey: ["tenants"],
    queryFn: async () => (await tenant.listMyTenants({} as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const memberships = useMemo(() => (data?.memberships ?? []) as any[], [data?.memberships]);

  // current user (for "you" marks in members list)
  const { data: me } = useQuery({
    queryKey: ["me"],
    queryFn: async () => (await (user as any).getMe({})) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const [name, setName] = useState("");
  const [currency, setCurrency] = useState("RUB");
  const [slug, setSlug] = useState("");

  // Invite modal state
  const [inviteOpen, setInviteOpen] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<"owner" | "admin" | "member">("member");

  // Edit account modal
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState("");
  const [editSlug, setEditSlug] = useState("");
  const [editCurrency, setEditCurrency] = useState("");

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

  const activeMembership = useMemo(() => memberships.find(m => m?.tenant?.id === currentTenantId) || memberships[0], [memberships, currentTenantId]);
  const activeTenantId = activeMembership?.tenant?.id as string | undefined;

  // Helpers to detect presence of new RPCs
  const tenantAny = tenant as any;
  const hasUpdate = typeof tenantAny.updateTenant === "function";
  const hasMembers = typeof tenantAny.listMembers === "function";
  const hasAddMember = typeof tenantAny.addMember === "function";
  const hasUpdateMemberRole = typeof tenantAny.updateMemberRole === "function";
  const hasRemoveMember = typeof tenantAny.removeMember === "function";

  // Members list
  const { data: membersResp, refetch: refetchMembers, isLoading: membersLoading, error: membersError } = useQuery({
    queryKey: ["tenantMembers", activeTenantId],
    enabled: Boolean(activeTenantId) && hasMembers,
    queryFn: async () => (await tenantAny.listMembers({ tenantId: activeTenantId })) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const members = (membersResp?.members ?? []) as any[];

  // Mutations
  const updateTenantMut = useMutation({
    mutationFn: async (payload: { id: string; name: string; slug?: string; defaultCurrencyCode?: string }) =>
      await tenantAny.updateTenant(payload),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["tenants"] }),
        refetchMembers(),
      ]);
      setEditOpen(false);
    },
    onError: (e: any) => {
      alert(normalizeApiErrorMessage(e, "Не удалось обновить аккаунт"));
    },
  });

  const addMemberMut = useMutation({
    mutationFn: async (payload: { tenantId: string; email: string; role: number }) =>
      await tenantAny.addMember(payload),
    onSuccess: async () => {
      setInviteEmail("");
      setInviteRole("member");
      setInviteOpen(false);
      await refetchMembers();
    },
    onError: (e: any) => {
      alert(normalizeApiErrorMessage(e, "Не удалось добавить участника"));
    },
  });

  const updateMemberRoleMut = useMutation({
    mutationFn: async (payload: { tenantId: string; userId: string; role: number }) =>
      await tenantAny.updateMemberRole(payload),
    onSuccess: async () => {
      await refetchMembers();
    },
    onError: (e: any) => {
      alert(normalizeApiErrorMessage(e, "Не удалось обновить роль"));
    },
  });

  const removeMemberMut = useMutation({
    mutationFn: async (payload: { tenantId: string; userId: string }) => await tenantAny.removeMember(payload),
    onSuccess: async () => {
      await refetchMembers();
    },
    onError: (e: any) => {
      alert(normalizeApiErrorMessage(e, "Не удалось удалить участника"));
    },
  });

  const enumToNum = (r: "owner" | "admin" | "member") => (r === "owner" ? 1 : r === "admin" ? 2 : 3);
  const surfaceCard = "border border-border bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60";
  const inputClass = "input bg-background/40";

  return (
    <div className="min-h-screen bg-background">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
        {/* Header */}
        <div>
          <h1 className="text-xl font-bold text-foreground mb-1">{t("title")}</h1>
          <p className="text-sm text-muted-foreground">{t("description")}</p>
        </div>

        {/* Info Block */}
        <Card className={surfaceCard}>
          <CardHeader>
            <div className="flex items-start gap-3">
              <div className="mt-0.5 w-8 h-8 rounded-lg bg-[hsl(var(--info)/0.2)] flex items-center justify-center">
                <Icon name="info" size={16} className="text-[hsl(var(--info))]" />
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
        <Card className={surfaceCard}>
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
                  className={`${inputClass} w-64`}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                  autoComplete="organization"
                />
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1">{t("slugOptional")}</label>
                <input
                  className={`${inputClass} w-48`}
                  value={slug}
                  onChange={(e) => setSlug(e.target.value)}
                  autoComplete="off"
                />
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1">{t("defaultCurrency")}</label>
                <input
                  className={`${inputClass} w-24 text-center`}
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
              <div className="mt-2 text-xs text-[hsl(var(--negative))]">{(createMut.error as any).message}</div>
            )}
          </CardContent>
        </Card>

        {/* Accounts list */}
        <Card className={surfaceCard}>
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
                {hasAddMember && activeTenantId && (
                  <Button
                    variant="outline"
                    size="sm"
                    icon="plus"
                    onClick={() => setInviteOpen(true)}
                  >
                    {t("invite")}
                  </Button>
                )}
              </div>
            </div>
          </CardHeader>
          <CardContent>
            {error && (
              <div className="text-sm text-[hsl(var(--negative))] mb-3">{(error as any).message}</div>
            )}
            <ul className="space-y-2">
              {memberships.map((m) => (
                <li key={m?.tenant?.id} className="flex items-center gap-3 text-sm">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-6 h-6 rounded-md bg-[hsl(var(--info)/0.2)] text-[hsl(var(--info))] flex items-center justify-center">
                      <Icon name="wallet" size={12} />
                    </div>
                    <span className="font-medium truncate max-w-[200px]" title={m?.tenant?.name}>{m?.tenant?.name}</span>
                  </div>
                  <span className="text-muted-foreground truncate max-w-[160px]" title={m?.tenant?.slug}>{m?.tenant?.slug}</span>
                  <span className="text-muted-foreground">{m?.tenant?.defaultCurrencyCode}</span>
                  <span className="text-[11px] px-2 py-0.5 rounded border border-border/70 text-muted-foreground" title={`role: ${m?.role}`}>
                    {roleLabel(Number(m?.role))}
                  </span>
                  {currentTenantId === m?.tenant?.id ? (
                    <span className="ml-auto text-[hsl(var(--positive))] text-xs">{t("active")}</span>
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
                  {hasUpdate && (Number(m?.role) === 1 || Number(m?.role) === 2) && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-xs"
                      onClick={() => {
                        setEditName(m?.tenant?.name ?? "");
                        setEditSlug(m?.tenant?.slug ?? "");
                        setEditCurrency(m?.tenant?.defaultCurrencyCode ?? "");
                        setEditOpen(true);
                      }}
                    >
                      {t("editAccount")}
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

        {/* Invite user */}
        <Modal
          open={inviteOpen}
          onClose={() => setInviteOpen(false)}
          title={t("inviteTitle") as string}
        
        >
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-muted-foreground mb-1">{t("inviteEmail")}</label>
              <input
                className={`${inputClass} w-full`}
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                placeholder="user@example.com"
              />
            </div>
            <div>
              <label className="block text-xs text-muted-foreground mb-1">{t("inviteRole")}</label>
              <select
                className={`${inputClass} w-full text-sm`}
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value as any)}
              >
                <option value="member">{t("roles.member")}</option>
                <option value="admin">{t("roles.admin")}</option>
                <option value="owner">{t("roles.owner")}</option>
              </select>
              <div className="text-[11px] text-muted-foreground mt-1">
                {inviteRole === "owner" ? t("roleDescriptions.owner") : inviteRole === "admin" ? t("roleDescriptions.admin") : t("roleDescriptions.member")}
              </div>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button variant="ghost" onClick={() => setInviteOpen(false)}>{tc("cancel")}</Button>
              <Button
                onClick={() => {
                  if (!activeTenantId || !hasAddMember) return setInviteOpen(false);
                  addMemberMut.mutate({ tenantId: activeTenantId, email: inviteEmail, role: enumToNum(inviteRole) });
                }}
                disabled={!hasAddMember || addMemberMut.isPending || !inviteEmail}
                loading={addMemberMut.isPending}
              >
                {t("invite")}
              </Button>
            </div>
            {addMemberMut.error && (
              <div className="text-xs text-[hsl(var(--negative))]">{(addMemberMut.error as any).message}</div>
            )}
          </div>
        </Modal>

        {/* Edit account */}
        <Modal
          open={editOpen}
          onClose={() => setEditOpen(false)}
          title={t("editAccount") as string}
        >
          <div className="space-y-3">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div>
                <label className="block text-xs text-muted-foreground mb-1">{t("name")}</label>
                <input className={`${inputClass} w-full`} value={editName} onChange={(e) => setEditName(e.target.value)} />
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1">{t("slug")}</label>
                <input className={`${inputClass} w-full`} value={editSlug} onChange={(e) => setEditSlug(e.target.value)} />
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1">{t("defaultCurrency")}</label>
                <input className={`${inputClass} w-full text-center`} value={editCurrency} onChange={(e) => setEditCurrency(e.target.value)} />
              </div>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button variant="ghost" onClick={() => setEditOpen(false)}>{tc("cancel")}</Button>
              <Button
                onClick={() => {
                  if (!activeTenantId || !hasUpdate) return setEditOpen(false);
                  updateTenantMut.mutate({ id: activeTenantId, name: editName, slug: editSlug, defaultCurrencyCode: editCurrency || undefined });
                }}
                disabled={!hasUpdate || updateTenantMut.isPending || !editName}
                loading={updateTenantMut.isPending}
              >
                {t("updateShort")}
              </Button>
            </div>
            {updateTenantMut.error && (
              <div className="text-xs text-[hsl(var(--negative))]">{(updateTenantMut.error as any).message}</div>
            )}
          </div>
        </Modal>

        {/* Members list for active tenant */}
        {activeTenantId && hasMembers && (
          <Card className={surfaceCard}>
            <CardHeader>
              <CardTitle className="text-base">{t("membersTitle")}</CardTitle>
              <CardDescription className="text-sm">{t("membersDescription")}</CardDescription>
            </CardHeader>
            <CardContent>
              {membersLoading && <div className="text-sm text-muted-foreground">{tc("loading")}</div>}
              {membersError && <div className="text-sm text-[hsl(var(--negative))]">{(membersError as any).message}</div>}
              {!membersLoading && !membersError && (
                <ul className="space-y-2">
                  {members.map((m: any) => (
                    <li key={m?.user?.id} className="flex items-center gap-3 text-sm">
                      <div className="flex items-center gap-2 min-w-0">
                        <div className="w-6 h-6 rounded-md bg-[hsl(var(--secondary)/0.5)] text-[hsl(var(--foreground))] flex items-center justify-center">
                          <Icon name="user" size={12} />
                        </div>
                        <div className="min-w-0">
                          <div className="truncate max-w-[220px]" title={m?.user?.email}>{m?.user?.email}</div>
                          <div className="text-[11px] text-muted-foreground truncate max-w-[220px]">
                            {m?.user?.name}{me?.user?.email && me?.user?.email === m?.user?.email ? ` · ${t("you")}` : ""}
                          </div>
                        </div>
                      </div>
                      {/* Role selector */}
                      {hasUpdateMemberRole && (
                        me?.user?.id === m?.user?.id ? (
                          <span className="ml-auto text-xs text-muted-foreground">{roleLabel(Number(m?.role))}</span>
                        ) : (
                          <select
                            className="ml-auto input w-32 bg-background/40 text-xs"
                            value={Number(m?.role) || 0}
                            onChange={(e) => {
                              const newRole = Number(e.target.value);
                              if (!activeTenantId || !m?.user?.id) return;
                              updateMemberRoleMut.mutate({ tenantId: activeTenantId, userId: m.user.id, role: newRole });
                            }}
                          >
                            <option value={1}>{t("roles.owner")}</option>
                            <option value={2}>{t("roles.admin")}</option>
                            <option value={3}>{t("roles.member")}</option>
                          </select>
                        )
                      )}
                      {hasRemoveMember && me?.user?.id !== m?.user?.id && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-xs"
                          onClick={() => {
                            if (!activeTenantId || !m?.user?.id) return;
                            removeMemberMut.mutate({ tenantId: activeTenantId, userId: m.user.id });
                          }}
                          disabled={removeMemberMut.isPending}
                        >
                          {t("remove")}
                        </Button>
                      )}
                    </li>
                  ))}
                  {members.length === 0 && (
                    <div className="text-sm text-muted-foreground">{t("noMembers")}</div>
                  )}
                </ul>
              )}
            </CardContent>
          </Card>
        )}
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


