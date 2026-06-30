import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import Protected from "@/components/Protected";
import HeaderNav from "@/components/HeaderNav";
import { ClientsProvider } from "./providers";
import { ToastProvider } from "@/components/Toast";
import { NextIntlClientProvider } from "next-intl";
import { cookies } from "next/headers";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

const titles: Record<string, string> = {
  en: "Budget Manager — Personal Finance Management",
  ru: "Budget Manager — Управление личными финансами",
};
const descriptions: Record<string, string> = {
  en: "Modern personal finance management application with multi-tenant support, transaction tracking, and detailed reporting.",
  ru: "Современное приложение для управления личными финансами: мультиаккаунты, учёт транзакций и подробные отчёты.",
};

export async function generateMetadata(): Promise<Metadata> {
  const cookieStore = await cookies();
  const loc = cookieStore.get("NEXT_LOCALE")?.value === "ru" ? "ru" : "en";
  return {
    title: titles[loc],
    description: descriptions[loc],
    keywords: ["budget", "finance", "money", "expenses", "income", "tracking"],
    authors: [{ name: "Budget Manager Team" }],
    manifest: "/manifest.json",
  };
}

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const cookieStore = await cookies();
  const loc = cookieStore.get("NEXT_LOCALE")?.value;
  const locale = loc === "ru" ? "ru" : "en";
  const theme = cookieStore.get("budget_theme")?.value === "light" ? "light" : "dark";
  const messages = (await import(`../i18n/${locale}.json`)).default as any;
  return (
    <html lang={locale} className={`h-full${theme === "dark" ? " dark" : ""}`}>
      <body className={`${geistSans.variable} ${geistMono.variable} antialiased min-h-full bg-background text-foreground`}>
        <NextIntlClientProvider messages={messages} locale={locale}>
          <ClientsProvider>
            <ToastProvider>
              <div className="min-h-screen flex flex-col">
                <HeaderNav />
                <main className="flex-1">
                  <Protected>{children}</Protected>
                </main>
              </div>
            </ToastProvider>
          </ClientsProvider>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}

export const dynamic = "force-dynamic";
