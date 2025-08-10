"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { authStore } from "@/lib/auth/store";

export default function Protected({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  useEffect(() => {
    const publicRoutes = ["/login", "/register"];
    const normalized = pathname.replace(/^\/(en|ru)(?=\/|$)/, "") || "/";
    if (publicRoutes.includes(normalized)) return;
    const token = authStore.getAccess();
    if (!token) router.replace("/login");
  }, [router, pathname]);
  return <>{children}</>;
}


