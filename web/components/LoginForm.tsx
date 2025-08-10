"use client";

import React, { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useClients } from "@/app/providers";
import { authStore } from "@/lib/auth/store";
import { useTranslations } from "next-intl";

const schema = z.object({
  email: z.string().email(),
  password: z.string().min(4),
});

type FormValues = z.infer<typeof schema>;

export default function LoginForm() {
  const { auth } = useClients();
  const t = useTranslations("auth.login");
  const { register, handleSubmit, formState } = useForm<FormValues>({ resolver: zodResolver(schema) });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onSubmit = async (values: FormValues) => {
    setLoading(true);
    setError(null);
    try {
      const res = await auth.login(values as any);
      const access = res.tokens?.accessToken ?? "";
      const refresh = res.tokens?.refreshToken ?? "";
      authStore.set({ accessToken: access, refreshToken: refresh });
      const tenantId = res.memberships?.[0]?.tenantId as unknown as string | undefined;
      if (tenantId) authStore.set({ tenantId });
      window.location.href = "/";
    } catch (e: any) {
      setError(e?.message ?? "Login failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="p-6 max-w-sm mx-auto">
      <h1 className="text-2xl font-semibold mb-4">{t("title")}</h1>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-3">
        <div>
          <label className="block text-sm">{t("email")}</label>
          <input className="border rounded px-3 py-2 w-full" {...register("email")} />
          {formState.errors.email && <div className="text-xs text-red-600">{formState.errors.email.message}</div>}
        </div>
        <div>
          <label className="block text-sm">{t("password")}</label>
          <input type="password" className="border rounded px-3 py-2 w-full" {...register("password")} />
          {formState.errors.password && <div className="text-xs text-red-600">{formState.errors.password.message}</div>}
        </div>
        {error && <div className="text-red-600 text-sm">{error}</div>}
        <button className="bg-black text-white rounded px-4 py-2" disabled={loading}>
          {loading ? t("submitting") : t("submit")}
        </button>
      </form>
    </div>
  );
}


