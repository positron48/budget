import Link from "next/link";
import { getTranslations } from "next-intl/server";

export default async function Home() {
  const t = await getTranslations("home");
  const nav = await getTranslations("nav");
  return (
    <main className="p-6 space-y-3">
      <h1 className="text-2xl font-semibold">{t("title")}</h1>
      <div className="space-x-3">
        <Link href="/login" className="underline">{nav("login")}</Link>
        <Link href="/categories" className="underline">{nav("categories")}</Link>
        <Link href="/transactions" className="underline">{nav("transactions")}</Link>
        <Link href="/reports/monthly" className="underline">{nav("reports")}</Link>
        <Link href="/fx" className="underline">{nav("fx")}</Link>
        <Link href="/settings/profile" className="underline">{nav("profile")}</Link>
      </div>
    </main>
  );
}
