"use client";

import { useState } from "react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import GoogleAuthButton from "@/components/GoogleAuthButton";
import Input from "@/components/Input";
import Icon from "@/components/Icon";

export default function RegisterPage() {
  const t = useTranslations("auth.register");
  const tc = useTranslations("auth");
  const [tenantName, setTenantName] = useState("");
  const [locale, setLocale] = useState("ru");
  const [error, setError] = useState("");

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-blue-50 to-indigo-100 dark:from-gray-900 dark:to-gray-800 px-4">
      <div className="w-full max-w-md">
        <div className="text-center mb-8">
          <div className="mx-auto w-16 h-16 bg-primary rounded-full flex items-center justify-center mb-4">
            <span className="text-3xl text-white">💼</span>
          </div>
          <h1 className="text-3xl font-bold text-foreground mb-2">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>

        <div className="card shadow-xl">
          <div className="card-content pt-8 space-y-6">
            {error && (
              <div className="bg-destructive/10 border border-destructive/20 rounded-md p-4 flex items-start space-x-3">
                <Icon name="alert-circle" size={20} className="text-destructive mt-0.5 flex-shrink-0" />
                <p className="text-sm text-destructive">{error}</p>
              </div>
            )}

            <div className="space-y-2">
              <label htmlFor="locale" className="text-sm font-medium text-foreground">
                {t("locale")}
              </label>
              <select id="locale" className="input" value={locale} onChange={(e) => setLocale(e.target.value)}>
                <option value="en">English</option>
                <option value="ru">Русский</option>
              </select>
            </div>

            <Input
              label={t("tenantName")}
              placeholder={t("tenantNamePlaceholder")}
              autoComplete="organization"
              value={tenantName}
              onChange={(e) => setTenantName(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">{t("tenantNameOptional")}</p>

            <GoogleAuthButton
              locale={locale}
              tenantName={tenantName}
              onError={setError}
            />
          </div>
        </div>

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
