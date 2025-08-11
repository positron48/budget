"use client";

import { useTranslations } from "next-intl";
import LoginForm from "@/components/LoginForm";

export default function LoginPage() {
  const t = useTranslations("auth");

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-blue-50 to-indigo-100 dark:from-gray-900 dark:to-gray-800 px-4">
      <div className="w-full max-w-md">
        {/* Logo and Title */}
        <div className="text-center mb-8">
          <div className="mx-auto w-16 h-16 bg-primary rounded-full flex items-center justify-center mb-4">
            <span className="text-3xl text-white">💼</span>
          </div>
          <h1 className="text-3xl font-bold text-foreground mb-2">
            {t("welcomeBack")}
          </h1>
          <p className="text-muted-foreground">
            {t("signInToContinue")}
          </p>
        </div>

        {/* Login Form */}
        <div className="card shadow-xl">
          <div className="card-content pt-8">
            <LoginForm />
          </div>
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


