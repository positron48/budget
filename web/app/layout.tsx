import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import Protected from "@/components/Protected";
import HeaderNav from "@/components/HeaderNav";
import { ClientsProvider } from "./providers";
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

export const metadata: Metadata = {
  title: "Budget Manager - Personal Finance Management",
  description: "Modern personal finance management application with multi-tenant support, transaction tracking, and detailed reporting.",
  keywords: ["budget", "finance", "money", "expenses", "income", "tracking"],
  authors: [{ name: "Budget Manager Team" }],
  viewport: "width=device-width, initial-scale=1",
  themeColor: "#020617",
  colorScheme: "dark",
  manifest: "/manifest.json",
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const cookieStore = await cookies();
  const loc = cookieStore.get("NEXT_LOCALE")?.value;
  const locale = loc === "ru" ? "ru" : "en";
  const messages = (await import(`../i18n/${locale}.json`)).default as any;
  return (
    <html lang={locale} className="h-full">
      <body className={`${geistSans.variable} ${geistMono.variable} antialiased min-h-full bg-background text-foreground`}>
        <NextIntlClientProvider messages={messages} locale={locale}>
          <ClientsProvider>
            <div className="min-h-screen flex flex-col">
              <HeaderNav />
              <main className="flex-1">
                <Protected>{children}</Protected>
              </main>
            </div>
          </ClientsProvider>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}

export const dynamic = "force-dynamic";
