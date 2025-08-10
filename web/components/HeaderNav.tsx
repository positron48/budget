"use client";

import Link from "next/link";
import { useRouter, usePathname } from "next/navigation";
import { authStore } from "@/lib/auth/store";
import { useTranslations } from "next-intl";
import { useState, useEffect } from "react";

export default function HeaderNav() {
  const router = useRouter();
  const pathname = usePathname();
  const t = useTranslations("nav");
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const [currentLocale, setCurrentLocale] = useState<string>("en");

  useEffect(() => {
    setCurrentLocale(authStore.getLocale() || "en");
  }, []);

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

  const isActive = (path: string) => {
    return pathname === path || pathname.startsWith(path + "/");
  };

  const navItems = [
    { href: "/", label: t("home"), icon: "🏠" },
    { href: "/categories", label: t("categories"), icon: "📂" },
    { href: "/transactions", label: t("transactions"), icon: "💰" },
    { href: "/reports/monthly", label: t("reports"), icon: "📊" },
    { href: "/fx", label: t("fx"), icon: "💱" },
    { href: "/tenants", label: t("tenants"), icon: "🏢" },
    { href: "/settings/profile", label: t("profile"), icon: "👤" },
  ];

  return (
    <header className="sticky top-0 z-50 w-full border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex h-16 items-center justify-between">
          {/* Logo and Desktop Navigation */}
          <div className="flex items-center space-x-8">
            <Link 
              href="/" 
              className="flex items-center space-x-2 text-xl font-bold text-primary hover:text-primary/80 transition-colors"
            >
              <span className="text-2xl">💼</span>
              <span>Budget Manager</span>
            </Link>
            
            {/* Desktop Navigation */}
            <nav className="hidden md:flex items-center space-x-1">
              {navItems.slice(1).map((item) => (
                <Link
                  key={item.href}
                  href={item.href}
                  className={`flex items-center space-x-1 px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                    isActive(item.href)
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent"
                  }`}
                >
                  <span>{item.icon}</span>
                  <span className="hidden lg:inline">{item.label}</span>
                </Link>
              ))}
            </nav>
          </div>

          {/* Right side - Locale and Logout */}
          <div className="flex items-center space-x-4">
            {/* Locale Switcher */}
            <div className="flex items-center space-x-1 bg-muted rounded-md p-1">
              <button
                onClick={() => onLocale("en")}
                className={`px-2 py-1 text-xs font-medium rounded transition-colors ${
                  currentLocale === "en"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                EN
              </button>
              <button
                onClick={() => onLocale("ru")}
                className={`px-2 py-1 text-xs font-medium rounded transition-colors ${
                  currentLocale === "ru"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                RU
              </button>
            </div>

            {/* Logout Button */}
            <button
              onClick={onLogout}
              className="btn btn-outline btn-sm hidden sm:inline-flex"
            >
              <span className="mr-1">🚪</span>
              {t("logout")}
            </button>

            {/* Mobile Menu Button */}
            <button
              onClick={() => setIsMenuOpen(!isMenuOpen)}
              className="md:hidden btn btn-ghost btn-sm p-2"
              aria-label="Toggle menu"
            >
              <svg
                className="h-5 w-5"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                {isMenuOpen ? (
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M6 18L18 6M6 6l12 12"
                  />
                ) : (
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M4 6h16M4 12h16M4 18h16"
                  />
                )}
              </svg>
            </button>
          </div>
        </div>

        {/* Mobile Navigation */}
        {isMenuOpen && (
          <div className="md:hidden border-t bg-background animate-in">
            <nav className="py-4 space-y-1">
              {navItems.map((item) => (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={() => setIsMenuOpen(false)}
                  className={`flex items-center space-x-3 px-4 py-2 text-sm font-medium rounded-md transition-colors ${
                    isActive(item.href)
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent"
                  }`}
                >
                  <span className="text-lg">{item.icon}</span>
                  <span>{item.label}</span>
                </Link>
              ))}
              <button
                onClick={() => {
                  onLogout();
                  setIsMenuOpen(false);
                }}
                className="flex items-center space-x-3 px-4 py-2 text-sm font-medium text-muted-foreground hover:text-foreground hover:bg-accent rounded-md transition-colors w-full"
              >
                <span className="text-lg">🚪</span>
                <span>{t("logout")}</span>
              </button>
            </nav>
          </div>
        )}
      </div>
    </header>
  );
}


