"use client";

import Link from "next/link";
import { useRouter, usePathname } from "next/navigation";
import { authStore } from "@/lib/auth/store";
import { useTranslations } from "next-intl";
import { useState, useEffect } from "react";
import { Icon, Button } from "@/components";

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
    } catch {
      // Ignore locale change errors
    }
    window.location.reload();
  };

  const isActive = (path: string) => {
    return pathname === path || pathname.startsWith(path + "/");
  };

  const navItems = [
    { href: "/", label: t("home"), icon: "home" },
    { href: "/transactions", label: t("transactions"), icon: "transactions" },
    { href: "/categories", label: t("categories"), icon: "categories" },
    { href: "/reports/monthly", label: t("reports"), icon: "reports" },
    { href: "/tenants", label: t("tenants"), icon: "tenants" },
    { href: "/settings/profile", label: t("profile"), icon: "profile" },
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
              <Icon name="wallet" size={24} className="text-primary" />
              <span>Budget Manager</span>
            </Link>
            
            {/* Desktop Navigation */}
            <nav className="hidden md:flex items-center space-x-1">
              {navItems.slice(1).map((item) => (
                <Link
                  key={item.href}
                  href={item.href}
                  className={`flex items-center space-x-2 px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                    isActive(item.href)
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent"
                  }`}
                >
                  <Icon name={item.icon as any} size={16} />
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
            <Button
              onClick={onLogout}
              variant="outline"
              size="sm"
              icon="logout"
              className="hidden sm:inline-flex"
            >
              {t("logout")}
            </Button>

            {/* Mobile Menu Button */}
            <Button
              onClick={() => setIsMenuOpen(!isMenuOpen)}
              variant="ghost"
              size="sm"
              icon={isMenuOpen ? "close" : "menu"}
              className="md:hidden p-2"
              aria-label="Toggle menu"
            >
              <span className="sr-only">Toggle menu</span>
            </Button>
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
                  <Icon name={item.icon as any} size={18} />
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
                <Icon name="logout" size={18} />
                <span>{t("logout")}</span>
              </button>
            </nav>
          </div>
        )}
      </div>
    </header>
  );
}


