"use client";

import Link from "next/link";
import { useRouter, usePathname } from "next/navigation";
import { authStore, AUTH_CHANGED_EVENT, TENANT_CHANGED_EVENT } from "@/lib/auth/store";
import { useTranslations } from "next-intl";
import { useState, useEffect, useMemo, useRef } from "react";
import { Icon, Button } from "@/components";
import { useClients } from "@/app/providers";
import { useQuery } from "@tanstack/react-query";

export default function HeaderNav() {
  const router = useRouter();
  const pathname = usePathname();
  const t = useTranslations("nav");
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const [currentLocale, setCurrentLocale] = useState<string>("en");
  const [isAccountOpen, setIsAccountOpen] = useState<boolean>(false);
  const [mounted, setMounted] = useState<boolean>(false);
  const accountRef = useRef<HTMLDivElement | null>(null);
  const { tenant } = useClients();
  const [isAuthorized, setIsAuthorized] = useState<boolean>(Boolean(authStore.getAccess()));
  const [currentTenantId, setCurrentTenantId] = useState<string | undefined>(authStore.getTenant());

  useEffect(() => {
    setCurrentLocale(authStore.getLocale() || "en");
    const onAuth = () => setIsAuthorized(Boolean(authStore.getAccess()));
    const onTenant = () => setCurrentTenantId(authStore.getTenant());
    if (typeof window !== "undefined") {
      window.addEventListener(AUTH_CHANGED_EVENT, onAuth);
      window.addEventListener(TENANT_CHANGED_EVENT, onTenant);
    }
    setMounted(true);
    return () => {
      if (typeof window !== "undefined") {
        window.removeEventListener(AUTH_CHANGED_EVENT, onAuth);
        window.removeEventListener(TENANT_CHANGED_EVENT, onTenant);
      }
    };
  }, []);

  // Close account menu on outside click or Escape
  useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (!isAccountOpen) return;
      const node = accountRef.current;
      if (node && e.target instanceof Node && !node.contains(e.target)) {
        setIsAccountOpen(false);
      }
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setIsAccountOpen(false);
    };
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [isAccountOpen]);

  // Close menu on route changes
  useEffect(() => {
    setIsAccountOpen(false);
  }, [pathname]);

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
    { href: "/account", label: t("tenants"), icon: "tenants" },
    { href: "/settings/profile", label: t("profile"), icon: "profile" },
  ];

  // Load user memberships to show active account name and allow switching
  const { data: tenantsData } = useQuery({
    queryKey: ["tenants", isAuthorized],
    enabled: isAuthorized,
    queryFn: async () => (await tenant.listMyTenants({} as any)) as any,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const memberships = useMemo(() => (tenantsData?.memberships ?? []) as any[], [tenantsData?.memberships]);
  const activeMembership = useMemo(
    () => memberships.find((m) => m?.tenant?.id === currentTenantId) || memberships[0],
    [memberships, currentTenantId]
  );
  const activeTenantName = activeMembership?.tenant?.name as string | undefined;

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

          {/* Right side - Locale and Account */}
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

            {/* Account selector with logout (only when authorized). Mount-gated to avoid hydration mismatch */}
            {mounted && isAuthorized && (
              <div className="relative" ref={accountRef}>
                <Button
                  onClick={() => setIsAccountOpen(!isAccountOpen)}
                  variant="outline"
                  size="sm"
                  icon="wallet"
                >
                  {activeTenantName || "Account"}
                </Button>
                {isAccountOpen && (
                  <div className="absolute right-0 mt-2 w-64 bg-background border rounded-md shadow-md z-50">
                    <div className="py-1 max-h-80 overflow-auto">
                      {memberships.map((m) => (
                        <button
                          key={m?.tenant?.id}
                          onClick={() => {
                            if (!m?.tenant?.id) return;
                            setIsAccountOpen(false);
                            authStore.set({ tenantId: m.tenant.id });
                            window.location.reload();
                          }}
                          className={`w-full text-left px-3 py-2 text-sm hover:bg-accent flex items-center justify-between ${
                            currentTenantId === m?.tenant?.id ? "font-medium" : ""
                          }`}
                        >
                          <span className="truncate pr-2">{m?.tenant?.name}</span>
                          {currentTenantId === m?.tenant?.id && <Icon name="check" size={16} className="text-green-600" />}
                        </button>
                      ))}
                      <div className="my-1 border-t" />
                      <button
                        onClick={() => { setIsAccountOpen(false); onLogout(); }}
                        className="w-full text-left px-3 py-2 text-sm hover:bg-accent flex items-center gap-2 text-red-600"
                      >
                        <Icon name="logout" size={16} />
                        <span>{t("logout")}</span>
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )}

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
              {/* No explicit logout in mobile list; use account selector above */}
            </nav>
          </div>
        )}
      </div>
    </header>
  );
}


