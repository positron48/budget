"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { authStore } from "@/lib/auth/store";

export default function Protected({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  useEffect(() => {
    const token = authStore.getAccess();
    if (!token) router.replace("/login");
  }, [router]);
  return <>{children}</>;
}


