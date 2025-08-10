"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslations } from "next-intl";

function ProfileSettingsInner() {
  const { user } = useClients();
  const qc = useQueryClient();
  const t = useTranslations("profile");
  const tc = useTranslations("common");
  const { data, isLoading, error } = useQuery({
    queryKey: ["me"],
    queryFn: async () => (await user.getMe({} as any)) as any,
    retry: false,
  });
  const me = (data?.user ?? {}) as any;
  const [name, setName] = useState<string>("");
  const [locale, setLocale] = useState<string>("ru");
  const updateMut = useMutation({
    mutationFn: async () => await user.updateProfile({ name, locale } as any),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["me"] }),
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
  });

  return (
    <div className="p-6 max-w-xl">
      <h1 className="text-2xl font-semibold mb-2">{t("title")}</h1>
      {isLoading && <div className="text-sm text-gray-500">{tc("loading")}</div>}
      {error && <div className="text-sm text-red-600">{(error as any).message}</div>}
      <div className="space-y-1 text-sm mb-4">
        <div>{t("email")}: {me.email}</div>
        <div>
          {t("createdAt")}: {me.createdAt?.seconds ? new Date(Number(me.createdAt.seconds) * 1000).toISOString() : ""}
        </div>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          updateMut.mutate();
        }}
        className="space-y-3 border p-3 rounded mb-6"
      >
        <div className="text-lg font-medium">{t("updateTitle")}</div>
        <div>
          <label className="block text-xs">{t("name")}</label>
          <input className="border rounded px-2 py-1 w-full" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">{t("locale")}</label>
          <input className="border rounded px-2 py-1 w-40" value={locale} onChange={(e) => setLocale(e.target.value)} />
        </div>
        <button className="bg-black text-white rounded px-3 py-1" disabled={updateMut.isPending}>
          {updateMut.isPending ? t("saving") : t("save")}
        </button>
      </form>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          setPwOk(false);
          pwMut.mutate();
        }}
        className="space-y-3 border p-3 rounded"
      >
        <div className="text-lg font-medium">{t("passwordTitle")}</div>
        <div>
          <label className="block text-xs">{t("currentPassword")}</label>
          <input type="password" className="border rounded px-2 py-1 w-full" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} />
        </div>
        <div>
          <label className="block text-xs">{t("newPassword")}</label>
          <input type="password" className="border rounded px-2 py-1 w-full" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} />
        </div>
        {pwOk && <div className="text-green-700 text-sm">{t("passwordChanged")}</div>}
        <button className="bg-black text-white rounded px-3 py-1" disabled={pwMut.isPending}>
          {pwMut.isPending ? t("saving") : t("save")}
        </button>
      </form>
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

