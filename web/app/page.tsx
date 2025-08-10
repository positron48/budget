"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";
import { Icon, Card, CardHeader, CardContent, CardTitle, CardDescription } from "@/components";
import type { IconName } from "@/components/Icon";

export default function HomePage() {
  const t = useTranslations("home");

  const features = [
    {
      title: t("categories.title"),
      description: t("categories.description"),
      href: "/categories",
      icon: "categories" as IconName,
      color: "bg-blue-500",
    },
    {
      title: t("transactions.title"),
      description: t("transactions.description"),
      href: "/transactions",
      icon: "transactions" as IconName,
      color: "bg-green-500",
    },
    {
      title: t("reports.title"),
      description: t("reports.description"),
      href: "/reports/monthly",
      icon: "reports" as IconName,
      color: "bg-purple-500",
    },
    {
      title: t("fx.title"),
      description: t("fx.description"),
      href: "/fx",
      icon: "fx" as IconName,
      color: "bg-orange-500",
    },
    {
      title: t("tenants.title"),
      description: t("tenants.description"),
      href: "/tenants",
      icon: "tenants" as IconName,
      color: "bg-indigo-500",
    },
    {
      title: t("profile.title"),
      description: t("profile.description"),
      href: "/settings/profile",
      icon: "profile" as IconName,
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
            <Card hover className="hover:shadow-lg transition-all duration-200 group-hover:scale-105">
              <CardHeader>
                <div className="flex items-center space-x-3">
                  <div className={`w-12 h-12 rounded-lg ${feature.color} flex items-center justify-center text-white`}>
                    <Icon name={feature.icon} size={24} />
                  </div>
                  <div>
                    <CardTitle className="text-lg">{feature.title}</CardTitle>
                    <CardDescription className="text-sm">
                      {feature.description}
                    </CardDescription>
                  </div>
                </div>
              </CardHeader>
            </Card>
          </Link>
        ))}
      </div>

      {/* Quick Stats */}
      <div className="mt-12">
        <h2 className="text-2xl font-semibold text-foreground mb-6">
          {t("quickStats")}
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <Card>
            <CardContent className="text-center">
              <div className="inline-flex items-center justify-center w-12 h-12 bg-blue-100 dark:bg-blue-950/20 rounded-lg mb-3">
                <Icon name="trending-up" size={24} className="text-blue-600" />
              </div>
              <div className="text-sm text-muted-foreground">{t("stats.overview")}</div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="text-center">
              <div className="inline-flex items-center justify-center w-12 h-12 bg-green-100 dark:bg-green-950/20 rounded-lg mb-3">
                <Icon name="wallet" size={24} className="text-green-600" />
              </div>
              <div className="text-sm text-muted-foreground">{t("stats.balance")}</div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="text-center">
              <div className="inline-flex items-center justify-center w-12 h-12 bg-red-100 dark:bg-red-950/20 rounded-lg mb-3">
                <Icon name="pie-chart" size={24} className="text-red-600" />
              </div>
              <div className="text-sm text-muted-foreground">{t("stats.expenses")}</div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="text-center">
              <div className="inline-flex items-center justify-center w-12 h-12 bg-purple-100 dark:bg-purple-950/20 rounded-lg mb-3">
                <Icon name="target" size={24} className="text-purple-600" />
              </div>
              <div className="text-sm text-muted-foreground">{t("stats.goals")}</div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
