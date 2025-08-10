import Link from "next/link";

export default function Home() {
  return (
    <main className="p-6 space-y-3">
      <h1 className="text-2xl font-semibold">Budget</h1>
      <div className="space-x-3">
        <Link href="/login" className="underline">Login</Link>
        <Link href="/categories" className="underline">Categories</Link>
        <Link href="/transactions" className="underline">Transactions</Link>
        <Link href="/reports/monthly" className="underline">Monthly</Link>
        <Link href="/fx" className="underline">FX</Link>
        <Link href="/settings/profile" className="underline">Profile</Link>
      </div>
    </main>
  );
}
