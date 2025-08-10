"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";

export default function HomePage() {
  const t = useTranslations("home");

  const features = [
    {
      title: t("categories.title"),
      description: t("categories.description"),
      href: "/categories",
      icon: "📂",
      color: "bg-blue-500",
    },
    {
      title: t("transactions.title"),
      description: t("transactions.description"),
      href: "/transactions",
      icon: "💰",
      color: "bg-green-500",
    },
    {
      title: t("reports.title"),
      description: t("reports.description"),
      href: "/reports/monthly",
      icon: "📊",
      color: "bg-purple-500",
    },
    {
      title: t("fx.title"),
      description: t("fx.description"),
      href: "/fx",
      icon: "💱",
      color: "bg-orange-500",
    },
    {
      title: t("tenants.title"),
      description: t("tenants.description"),
      href: "/tenants",
      icon: "🏢",
      color: "bg-indigo-500",
    },
    {
      title: t("profile.title"),
      description: t("profile.description"),
      href: "/settings/profile",
      icon: "👤",
      color: "bg-pink-500",
    },
  ];

  return (
    <div className="container mx-auto px-4 sm:px-6 lg:px-8 py-8">
      {/* Hero Section */}
      <div className="text-center mb-12">
        <h1 className="text-4xl font-bold text-foreground mb-4">
          {t("welcome")}
        </h1>
        <p className="text-xl text-muted-foreground max-w-2xl mx-auto">
          {t("subtitle")}
        </p>
      </div>

      {/* Features Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {features.map((feature) => (
          <Link
            key={feature.href}
            href={feature.href}
            className="group block"
          >
            <div className="card hover:shadow-lg transition-all duration-200 group-hover:scale-105">
              <div className="card-header">
                <div className="flex items-center space-x-3">
                  <div className={`w-12 h-12 rounded-lg ${feature.color} flex items-center justify-center text-white text-2xl`}>
                    {feature.icon}
                  </div>
                  <div>
                    <h3 className="card-title text-lg">{feature.title}</h3>
                    <p className="card-description text-sm">
                      {feature.description}
                    </p>
                  </div>
                </div>
              </div>
            </div>
          </Link>
        ))}
      </div>

      {/* Quick Stats */}
      <div className="mt-12">
        <h2 className="text-2xl font-semibold text-foreground mb-6">
          {t("quickStats")}
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="card">
            <div className="card-content text-center">
              <div className="text-3xl font-bold text-primary mb-2">📈</div>
              <div className="text-sm text-muted-foreground">{t("stats.overview")}</div>
            </div>
          </div>
          <div className="card">
            <div className="card-content text-center">
              <div className="text-3xl font-bold text-green-500 mb-2">💰</div>
              <div className="text-sm text-muted-foreground">{t("stats.balance")}</div>
            </div>
          </div>
          <div className="card">
            <div className="card-content text-center">
              <div className="text-3xl font-bold text-red-500 mb-2">📊</div>
              <div className="text-sm text-muted-foreground">{t("stats.expenses")}</div>
            </div>
          </div>
          <div className="card">
            <div className="card-content text-center">
              <div className="text-3xl font-bold text-blue-500 mb-2">🎯</div>
              <div className="text-sm text-muted-foreground">{t("stats.goals")}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
