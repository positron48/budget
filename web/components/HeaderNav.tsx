"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { authStore } from "@/lib/auth/store";
import { useTranslations } from "next-intl";

export default function HeaderNav() {
  const router = useRouter();
  const t = useTranslations("nav");
  const onLogout = () => {
    authStore.clear();
    router.replace("/login");
  };
  const onLocale = async (loc: string) => {
    try {
      await fetch("/api/locale", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ locale: loc }),
      });
    } catch (_) {}
    window.location.reload();
  };
  return (
    <header className="border-b bg-white/80 backdrop-blur sticky top-0 z-10">
      <div className="max-w-5xl mx-auto px-4 py-2 flex items-center gap-3">
        <Link className="font-semibold" href="/">{t("home")}</Link>
        <nav className="flex items-center gap-3 text-sm">
          <Link href="/categories">{t("categories")}</Link>
          <Link href="/transactions">{t("transactions")}</Link>
          <Link href="/reports/monthly">{t("reports")}</Link>
          <Link href="/fx">{t("fx")}</Link>
          <Link href="/tenants">{t("tenants")}</Link>
          <Link href="/settings/profile">{t("profile")}</Link>
        </nav>
        <div className="ml-auto flex items-center gap-2 text-xs">
          <button className="underline" onClick={() => onLocale("en")}>EN</button>
          <span>/</span>
          <button className="underline" onClick={() => onLocale("ru")}>RU</button>
          <button className="ml-3 px-2 py-1 border rounded" onClick={onLogout}>{t("logout")}</button>
        </div>
      </div>
    </header>
  );
}


