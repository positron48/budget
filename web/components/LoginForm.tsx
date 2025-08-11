"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslations } from "next-intl";
import { useClients } from "@/app/providers";
import { authStore } from "@/lib/auth/store";
import Button from "./Button";
import Input from "./Input";
import Icon from "./Icon";

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
  const [showPassword, setShowPassword] = useState(false);

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
        <div className="bg-destructive/10 border border-destructive/20 rounded-md p-4 flex items-start space-x-3 mt-2">
          <Icon name="alert-circle" size={20} className="text-destructive mt-0.5 flex-shrink-0" />
          <p className="text-sm text-destructive">{error}</p>
        </div>
      )}

      <Input
        label={t("email")}
        type="email"
        leftIcon="mail"
        error={errors.email?.message}
        placeholder={t("emailPlaceholder")}
        disabled={isLoading}
        autoComplete="email"
        {...register("email")}
      />

      <Input
        label={t("password")}
        type={showPassword ? "text" : "password"}
        leftIcon="lock"
        rightIcon={showPassword ? "eye-off" : "eye"}
        onRightIconClick={() => setShowPassword(!showPassword)}
        error={errors.password?.message}
        placeholder={t("passwordPlaceholder")}
        disabled={isLoading}
        autoComplete="current-password"
        {...register("password")}
      />

      <Button
        type="submit"
        loading={isLoading}
        icon="log-in"
        className="w-full h-11"
      >
        {isLoading ? t("signingIn") : t("signIn")}
      </Button>
    </form>
  );
}


