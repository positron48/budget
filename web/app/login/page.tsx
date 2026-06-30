"use client";

import { useTranslations } from "next-intl";
import LoginForm from "@/components/LoginForm";
import { Icon } from "@/components";

export default function LoginPage() {
  const t = useTranslations("auth");

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-6 py-12">
      <div className="w-full max-w-md">
        {/* Logo and Title */}
        <div className="text-center mb-8">
          <div className="mx-auto w-16 h-16 rounded-2xl bg-[hsl(var(--primary)/0.15)] flex items-center justify-center mb-4 border border-[hsl(var(--primary)/0.3)]">
            <Icon name="wallet" size={32} className="text-[hsl(var(--primary))]" />
          </div>
          <h1 className="text-3xl font-bold text-foreground mb-2">
            {t("welcomeBack")}
          </h1>
          <p className="text-muted-foreground">
            {t("signInToContinue")}
          </p>
        </div>

        {/* Login Form */}
        <div className="border border-border bg-card/80 backdrop-blur supports-[backdrop-filter]:bg-card/60 shadow-xl rounded-lg p-8">
          <LoginForm />
        </div>

        {/* Footer */}
        <div className="text-center mt-6">
          <p className="text-sm text-muted-foreground">
            {t("dontHaveAccount")}{" "}
            <a href="/register" className="text-primary hover:text-primary/80 font-medium">
              {t("signUp")}
            </a>
          </p>
        </div>
      </div>
    </div>
  );
}


