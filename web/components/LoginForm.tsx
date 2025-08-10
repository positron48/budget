"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { authStore } from "@/lib/auth/store";

const loginSchema = z.object({
  email: z.string().email("Invalid email address"),
  password: z.string().min(1, "Password is required"),
});

type LoginFormData = z.infer<typeof loginSchema>;

export default function LoginForm() {
  const router = useRouter();
  const { auth } = useClients();
  const t = useTranslations("auth");
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string>("");

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginFormData>({
    resolver: zodResolver(loginSchema),
  });

  const onSubmit = async (data: LoginFormData) => {
    setIsLoading(true);
    setError("");

    try {
      const response = await auth.login({
        email: data.email,
        password: data.password,
      } as any);

      if (response.tokens) {
        authStore.set({
          accessToken: response.tokens.accessToken,
          refreshToken: response.tokens.refreshToken,
        });
        router.push("/");
      }
    } catch (err: any) {
      setError(err.message || t("loginError"));
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
      {error && (
        <div className="bg-destructive/10 border border-destructive/20 rounded-md p-3">
          <p className="text-sm text-destructive">{error}</p>
        </div>
      )}

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
          disabled={isLoading}
        />
        {errors.email && (
          <p className="text-sm text-destructive">{errors.email.message}</p>
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
          disabled={isLoading}
        />
        {errors.password && (
          <p className="text-sm text-destructive">{errors.password.message}</p>
        )}
      </div>

      <button
        type="submit"
        disabled={isLoading}
        className="btn btn-primary w-full"
      >
        {isLoading ? (
          <div className="flex items-center space-x-2">
            <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></div>
            <span>{t("signingIn")}</span>
          </div>
        ) : (
          <span>{t("signIn")}</span>
        )}
      </button>
    </form>
  );
}


