"use client";

import { useClients } from "@/app/providers";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { authStore } from "@/lib/auth/store";
import { useTranslations } from "next-intl";
import Link from "next/link";

const schema = z.object({
  email: z.string().email(),
  password: z.string().min(6),
  name: z.string().min(1),
  locale: z.string().default("ru"),
  tenantName: z.string().min(1),
});
type FormValues = z.infer<typeof schema>;

function RegisterForm() {
  const { auth } = useClients();
  const t = useTranslations("auth.register");
  const tc = useTranslations("auth");
  const { register, handleSubmit, formState } = useForm<FormValues>({ resolver: zodResolver(schema) });
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  
  const onSubmit = async (values: FormValues) => {
    setLoading(true);
    setError(null);
    try {
      const res = await auth.register(values as any);
      const access = res.tokens?.accessToken ?? "";
      const refresh = res.tokens?.refreshToken ?? "";
      authStore.set({ accessToken: access, refreshToken: refresh });
      const tenantId = res.tenant?.id as unknown as string | undefined;
      if (tenantId) authStore.set({ tenantId });
      window.location.href = "/";
    } catch (e: any) {
      setError(e?.message ?? tc("registerError"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-blue-50 to-indigo-100 dark:from-gray-900 dark:to-gray-800 px-4">
      <div className="w-full max-w-md">
        {/* Logo and Title */}
        <div className="text-center mb-8">
          <div className="mx-auto w-16 h-16 bg-primary rounded-full flex items-center justify-center mb-4">
            <span className="text-3xl text-white">💼</span>
          </div>
          <h1 className="text-3xl font-bold text-foreground mb-2">
            {t("title")}
          </h1>
          <p className="text-muted-foreground">
            {t("subtitle")}
          </p>
        </div>

        {/* Register Form */}
        <div className="card shadow-xl">
          <div className="card-content">
            {error && (
              <div className="bg-destructive/10 border border-destructive/20 rounded-md p-3 mb-6">
                <p className="text-sm text-destructive">{error}</p>
              </div>
            )}

            <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
              <div className="space-y-2">
                <label htmlFor="email" className="text-sm font-medium text-foreground">
                  {t("email")}
                </label>
                <input
                  id="email"
                  type="email"
                  {...register("email")}
                  className="input"
                  placeholder={t("emailPlaceholder")}
                />
                {formState.errors.email && (
                  <p className="text-sm text-destructive">{formState.errors.email.message}</p>
                )}
              </div>

              <div className="space-y-2">
                <label htmlFor="password" className="text-sm font-medium text-foreground">
                  {t("password")}
                </label>
                <input
                  id="password"
                  type="password"
                  {...register("password")}
                  className="input"
                  placeholder={t("passwordPlaceholder")}
                />
                {formState.errors.password && (
                  <p className="text-sm text-destructive">{formState.errors.password.message}</p>
                )}
              </div>

              <div className="space-y-2">
                <label htmlFor="name" className="text-sm font-medium text-foreground">
                  {t("name")}
                </label>
                <input
                  id="name"
                  {...register("name")}
                  className="input"
                  placeholder={t("namePlaceholder")}
                />
                {formState.errors.name && (
                  <p className="text-sm text-destructive">{formState.errors.name.message}</p>
                )}
              </div>

              <div className="space-y-2">
                <label htmlFor="locale" className="text-sm font-medium text-foreground">
                  {t("locale")}
                </label>
                <select
                  id="locale"
                  {...register("locale")}
                  className="input"
                  defaultValue="ru"
                >
                  <option value="en">English</option>
                  <option value="ru">Русский</option>
                </select>
              </div>

              <div className="space-y-2">
                <label htmlFor="tenantName" className="text-sm font-medium text-foreground">
                  {t("tenantName")}
                </label>
                <input
                  id="tenantName"
                  {...register("tenantName")}
                  className="input"
                  placeholder={t("tenantNamePlaceholder")}
                />
                {formState.errors.tenantName && (
                  <p className="text-sm text-destructive">{formState.errors.tenantName.message}</p>
                )}
              </div>

              <button
                type="submit"
                disabled={loading}
                className="btn btn-primary w-full"
              >
                {loading ? (
                  <div className="flex items-center space-x-2">
                    <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></div>
                    <span>{t("submitting")}</span>
                  </div>
                ) : (
                  <span>{t("submit")}</span>
                )}
              </button>
            </form>
          </div>
        </div>

        {/* Footer */}
        <div className="text-center mt-6">
          <p className="text-sm text-muted-foreground">
            {tc("alreadyHaveAccount")}{" "}
            <Link href="/login" className="text-primary hover:text-primary/80 font-medium">
              {tc("signInLink")}
            </Link>
          </p>
        </div>
      </div>
    </div>
  );
}

export default function RegisterPage() {
  return <RegisterForm />;
}


