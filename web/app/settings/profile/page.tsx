"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useTranslations } from "next-intl";
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Icon, Input } from "@/components";
import { normalizeApiErrorMessage } from "@/lib/api/errors";
import { authStore } from "@/lib/auth/store";

function ProfileSettingsInner() {
  const { user } = useClients();
  const qc = useQueryClient();
  const t = useTranslations("profile");
  const tc = useTranslations("common");

  const { data, isLoading, error } = useQuery({
    queryKey: ["me"],
    queryFn: async () => (await user.getMe({} as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const me = useMemo(() => (data?.user ?? {}) as any, [data]);

  const [name, setName] = useState<string>("");
  const [locale, setLocale] = useState<string>("ru");
  const [profileOk, setProfileOk] = useState(false);

  useEffect(() => {
    if (me?.name) setName(me.name);
    // prefer user profile locale, fallback to cookie
    const currentLocale = me?.locale || authStore.getLocale() || "en";
    setLocale(currentLocale === "ru" ? "ru" : "en");
  }, [me?.name, me?.locale]);

  const updateMut = useMutation({
    mutationFn: async () => await user.updateProfile({ name, locale } as any),
    onSuccess: async () => {
      setProfileOk(true);
      await qc.invalidateQueries({ queryKey: ["me"] });
      // Update NEXT_LOCALE cookie if changed
      if (me?.locale !== locale) {
        try {
          await fetch("/api/locale", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ locale }),
          });
          // reload to re-hydrate translations
          window.location.reload();
        } catch (e) {
          // ignore
        }
      }
    },
    onError: (e: any) => {
      alert(normalizeApiErrorMessage(e, "Не удалось обновить профиль"));
    },
  });

  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [pwOk, setPwOk] = useState(false);
  const pwMut = useMutation({
    mutationFn: async () => await user.changePassword({ currentPassword, newPassword } as any),
    onSuccess: () => {
      setPwOk(true);
      setCurrentPassword("");
      setNewPassword("");
    },
    onError: (e: any) => {
      alert(normalizeApiErrorMessage(e, "Не удалось изменить пароль"));
    },
  });

  const createdAtStr = useMemo(() => {
    const sec = me?.createdAt?.seconds ? Number(me.createdAt.seconds) : undefined;
    if (!sec) return "";
    try {
      return new Date(sec * 1000).toLocaleString(locale === "ru" ? "ru-RU" : "en-US");
    } catch {
      return new Date(sec * 1000).toISOString();
    }
  }, [me?.createdAt?.seconds, locale]);

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
        <div>
          <h1 className="text-xl font-bold text-foreground mb-1">{t("title")}</h1>
          <p className="text-sm text-muted-foreground">{t("description")}</p>
        </div>

        <Card className="bg-white dark:bg-gray-900/40 border border-gray-200 dark:border-gray-800">
          <CardHeader>
            <div className="flex items-start gap-3">
              <div className="mt-0.5 w-8 h-8 rounded-lg bg-indigo-100 dark:bg-indigo-900/20 flex items-center justify-center">
                <Icon name="user" size={16} className="text-indigo-600 dark:text-indigo-400" />
              </div>
              <div>
                <CardTitle className="text-base">{me?.email || ""}</CardTitle>
                <CardDescription className="text-sm !mt-1">
                  {t("createdAt")}: {createdAtStr}
                </CardDescription>
              </div>
            </div>
          </CardHeader>
        </Card>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Update profile */}
          <Card className="bg-white dark:bg-gray-900/40 border border-gray-200 dark:border-gray-800">
            <CardHeader>
              <CardTitle className="text-base">{t("updateTitle")}</CardTitle>
            </CardHeader>
            <CardContent>
              {isLoading && <div className="text-sm text-muted-foreground mb-2">{tc("loading")}</div>}
              {error && (
                <div className="text-sm text-red-600 mb-2">{(error as any).message}</div>
              )}
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  setProfileOk(false);
                  updateMut.mutate();
                }}
                className="space-y-4"
              >
                <Input
                  label={t("name") as string}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  autoComplete="name"
                  placeholder="John Doe"
                />
                <div>
                  <label className="text-sm font-medium text-foreground">{t("locale")}</label>
                  <select
                    className="input mt-2 w-40"
                    value={locale}
                    onChange={(e) => setLocale(e.target.value)}
                  >
                    <option value="en">English</option>
                    <option value="ru">Русский</option>
                  </select>
                </div>
                {profileOk && (
                  <div className="text-xs text-green-700">{t("profileUpdated")}</div>
                )}
                <Button type="submit" loading={updateMut.isPending} disabled={updateMut.isPending}>
                  {updateMut.isPending ? tc("saving") : tc("save")}
                </Button>
                {updateMut.error && (
                  <div className="text-xs text-red-600">{(updateMut.error as any).message}</div>
                )}
              </form>
            </CardContent>
          </Card>

          {/* Change password */}
          <Card className="bg-white dark:bg-gray-900/40 border border-gray-200 dark:border-gray-800">
            <CardHeader>
              <CardTitle className="text-base">{t("passwordTitle")}</CardTitle>
            </CardHeader>
            <CardContent>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  setPwOk(false);
                  pwMut.mutate();
                }}
                className="space-y-4"
              >
                <Input
                  label={t("currentPassword") as string}
                  type="password"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  autoComplete="current-password"
                />
                <Input
                  label={t("newPassword") as string}
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  autoComplete="new-password"
                />
                {pwOk && <div className="text-xs text-green-700">{t("passwordChanged")}</div>}
                <Button type="submit" loading={pwMut.isPending} disabled={pwMut.isPending}>
                  {pwMut.isPending ? tc("saving") : tc("save")}
                </Button>
                {pwMut.error && (
                  <div className="text-xs text-red-600">{(pwMut.error as any).message}</div>
                )}
              </form>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

export default function ProfileSettingsPage() {
  return (
    <ClientsProvider>
      <ProfileSettingsInner />
    </ClientsProvider>
  );
}

