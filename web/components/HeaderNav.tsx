"use client";

import {useInvestmentAccess} from "@/lib/investment-access";
import DisplayCurrencySelect from "@/components/DisplayCurrencySelect";
import Link from "next/link";
import { useRouter, usePathname, useSearchParams } from "next/navigation";
import { authStore, AUTH_CHANGED_EVENT, TENANT_CHANGED_EVENT } from "@/lib/auth/store";
import { useTranslations } from "next-intl";
import { useState, useEffect, useMemo, useRef } from "react";
import { Icon, Button, ThemeToggle } from "@/components";
import { useClients } from "@/app/providers";
import { useQuery, useQueryClient } from "@tanstack/react-query";

export default function HeaderNav({investmentsEnabled=false}:{investmentsEnabled?:boolean}) {
  const investmentAccess=useInvestmentAccess(investmentsEnabled);
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const t = useTranslations("nav");
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const [currentLocale, setCurrentLocale] = useState<string>("en");
  const [isAccountOpen, setIsAccountOpen] = useState<boolean>(false);
  const [mounted, setMounted] = useState<boolean>(false);
  const navigationRef = useRef<HTMLElement | null>(null);
  const accountRef = useRef<HTMLDivElement | null>(null);
  const { tenant } = useClients();
  const qc = useQueryClient();
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
      navigationRef.current?.querySelectorAll("details[open]").forEach(menu => {
        if (e.target instanceof Node && !menu.contains(e.target)) menu.removeAttribute("open");
      });
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
    setIsMenuOpen(false);
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
    setCurrentLocale(loc);
    // Re-render server components (layout reloads messages) without a full reload.
    router.refresh();
  };

  const isActive = (path: string) => {
    return pathname === path || pathname.startsWith(path + "/");
  };

  const investmentLinks = [
    ["/investments", "overview", "portfolio", "pie-chart"],
    ["/investments/analytics", "", "growth", "trending-up"],
    ["/investments", "trades", "trades", "fx"],
    ["/investments", "payments", "payments", "coins"],
    ["/investments/calendar", "", "calendar", "calendar"],
    ["/investments/instruments", "", "findSecurity", "search"],
    ["/investments/groups", "", "securityGroups", "layers"],
    ["/investments/accounts", "", "investmentAccounts", "wallet"],
    ["/investments/import", "", "import", "upload"],
  ].map(([route, view, label, icon]) => {
    const params = new URLSearchParams(pathname.startsWith("/investments") ? searchParams.toString() : "");
    params.delete("fx");
    params.delete("page");
    params.delete("view");
    if (view && view !== "overview") params.set("view", view);
    return {
      href: route + (params.size ? "?" + params.toString() : ""),
      label: t(label),
      icon,
      active: route === "/investments"
        ? pathname === route && (searchParams.get("view") || "overview") === view
        : isActive(route),
    };
  });
  const navGroups: {label: string; items: {href: string; label: string; icon: string; active?: boolean}[]}[] = [
    {label: t("budget"), items: [
      {href: "/", label: t("home"), icon: "home"},
      {href: "/transactions", label: t("transactions"), icon: "transactions"},
      {href: "/reports", label: t("reports"), icon: "reports"},
      {href: "/categories", label: t("categories"), icon: "categories"},
    ]},
    ...(investmentsEnabled && investmentAccess.isSuccess ? [{label: t("investments"), items: investmentLinks}] : []),
    {label: t("assets"), items: [
      {href: "/assets", label: t("assets"), icon: "wallet"},
      {href: "/currency-exchanges", label: t("currencyExchanges"), icon: "fx"},
      {href: "/fx", label: t("fx"), icon: "fx"},
    ]},
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
          <div className="flex items-center gap-3 xl:gap-8">
            <Link 
              href="/" 
              className="flex items-center space-x-2 text-xl font-bold text-primary hover:text-primary/80 transition-colors"
            >
              <Icon name="wallet" size={24} className="text-primary" />
              <span className="hidden min-[420px]:inline">Budget<span className="hidden 2xl:inline"> Manager</span></span>
            </Link>
            
            {/* Desktop Navigation */}
            <nav ref={navigationRef} aria-label={t("navigation")} className="hidden xl:flex items-center gap-1">
              {navGroups.map(group => <details name="primary-navigation" key={group.label} className="relative group" onKeyDown={e => {if(e.key === "Escape") e.currentTarget.open = false;}}>
                <summary className={`flex cursor-pointer list-none items-center gap-2 rounded-md px-3 py-2 text-sm font-medium ${group.items.some(item => (item.active ?? isActive(item.href))) ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent hover:text-foreground"}`}>
                  {group.label}<span aria-hidden="true" className="text-xs group-open:rotate-180">⌄</span>
                </summary>
                <div className="absolute left-0 top-full z-50 mt-2 max-h-[calc(100dvh-5rem)] w-56 overflow-y-auto rounded-lg border border-border bg-[hsl(var(--card))] p-1 shadow-lg">
                  {group.items.map(item => <Link key={item.href} href={item.href} aria-current={(item.active ?? isActive(item.href)) ? "page" : undefined} onClick={e => {const menu=e.currentTarget.closest("details");if(menu)menu.open=false;}} className={`flex items-center gap-3 rounded-md px-3 py-2 text-sm hover:bg-accent ${(item.active ?? isActive(item.href)) ? "bg-accent font-medium text-foreground" : "text-muted-foreground"}`}>
                    <Icon name={item.icon as any} size={16}/>{item.label}
                  </Link>)}
                </div>
              </details>)}
            </nav>
          </div>

          {/* Right side - Locale and Account */}
          <div className="flex items-center gap-1.5 sm:gap-2">
            {mounted && isAuthorized && <DisplayCurrencySelect />}
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

            {/* Theme Switcher */}
            <ThemeToggle />

            {/* Account selector with logout (only when authorized). Mount-gated to avoid hydration mismatch */}
            {mounted && isAuthorized && (
              <div className="relative" ref={accountRef}>
                <Button
                  onClick={() => setIsAccountOpen(!isAccountOpen)}
                  variant="outline"
                  size="sm"
                  icon="wallet"
                  aria-label={activeTenantName || "Account"}
                >
                  <span className="hidden max-w-28 truncate md:inline">{activeTenantName || "Account"}</span>
                </Button>
                {isAccountOpen && (
                  <div
                    className="absolute right-0 top-full mt-2 w-64 border border-border rounded-lg shadow-lg z-[200] backdrop-blur supports-[backdrop-filter]:bg-card/70"
                    style={{ backgroundColor: "hsl(var(--card) / 0.95)" }}
                  >
                    <div className="py-1 max-h-80 overflow-auto">
                      {memberships.map((m) => (
                        <button
                          key={m?.tenant?.id}
                          onClick={() => {
                            if (!m?.tenant?.id) return;
                            setIsAccountOpen(false);
                            authStore.set({ tenantId: m.tenant.id });
                            setCurrentTenantId(m.tenant.id);
                            // Refetch all data for the newly selected tenant without a full reload.
                            qc.invalidateQueries();
                            router.refresh();
                          }}
                          className={`w-full text-left px-3 py-2 text-sm hover:bg-secondary/40 flex items-center justify-between ${
                            currentTenantId === m?.tenant?.id ? "font-medium" : ""
                          }`}
                        >
                          <span className="truncate pr-2">{m?.tenant?.name}</span>
                          {currentTenantId === m?.tenant?.id && <Icon name="check" size={16} className="text-green-600" />}
                        </button>
                      ))}
                      <div className="my-1 border-t border-border/60" />
                      {[{href:"/account",label:t("tenants")},{href:"/settings/profile",label:t("profile")}].map(item => <Link key={item.href} href={item.href} onClick={()=>setIsAccountOpen(false)} className="block px-3 py-2 text-sm hover:bg-secondary/40">{item.label}</Link>)}
                      <button
                        onClick={() => { setIsAccountOpen(false); onLogout(); }}
                        className="w-full text-left px-3 py-2 text-sm hover:bg-secondary/40 flex items-center gap-2 text-[hsl(var(--negative))]"
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
            <div className="xl:hidden">
              <Button
                onClick={() => setIsMenuOpen(!isMenuOpen)}
                variant="ghost"
                size="sm"
                icon={isMenuOpen ? "close" : "menu"}
                className="p-2"
                aria-label={t("navigation")} aria-expanded={isMenuOpen}
              >
                <span className="sr-only">{t("navigation")}</span>
              </Button>
            </div>
          </div>
        </div>

        {/* Mobile Navigation */}
        {isMenuOpen && (
          <div className="xl:hidden border-t bg-background animate-in">
            <nav aria-label={t("navigation")} className="max-h-[calc(100dvh-4rem)] overflow-y-auto py-3">
              {navGroups.map(group => <section key={group.label} className="pb-3"><h2 className="px-4 py-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{group.label}</h2>{group.items.map((item) => (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={() => setIsMenuOpen(false)}
                  aria-current={(item.active ?? isActive(item.href)) ? "page" : undefined}
                  className={`flex items-center space-x-3 px-4 py-2 text-sm font-medium rounded-md transition-colors ${
                    (item.active ?? isActive(item.href))
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent"
                  }`}
                >
                  <Icon name={item.icon as any} size={18} />
                  <span>{item.label}</span>
                </Link>
              ))}</section>)}
              {mounted && isAuthorized && (
                <>
                  <div className="my-2 border-t border-border/60" />
                  <button
                    onClick={() => { setIsMenuOpen(false); onLogout(); }}
                    className="w-full flex items-center space-x-3 px-4 py-2 text-sm font-medium rounded-md text-[hsl(var(--negative))] hover:bg-accent"
                  >
                    <Icon name="logout" size={18} />
                    <span>{t("logout")}</span>
                  </button>
                </>
              )}
            </nav>
          </div>
        )}
      </div>
    </header>
  );
}

