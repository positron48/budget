"use client";

import { useClients } from "@/app/providers";
import { authStore } from "@/lib/auth/store";
import { normalizeApiErrorMessage } from "@/lib/api/errors";
import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import Button from "./Button";

declare global {
  interface Window {
    google?: any;
  }
}

type Props = {
  locale?: string;
  tenantName?: string;
  onError?: (message: string) => void;
};

export default function GoogleAuthButton({ locale, tenantName, onError }: Props) {
  const t = useTranslations("auth");
  const { auth } = useClients();
  const router = useRouter();
  const [loading, setLoading] = useState(false);
  const readyRef = useRef(false);

  useEffect(() => {
    if (window.google) {
      readyRef.current = true;
      return;
    }
    const script = document.createElement("script");
    script.src = "https://accounts.google.com/gsi/client";
    script.async = true;
    script.defer = true;
    script.onload = () => {
      readyRef.current = true;
    };
    document.head.appendChild(script);
    return () => {
      script.remove();
    };
  }, []);

  const handleCredential = async (credential: string) => {
    setLoading(true);
    onError?.("");
    try {
      const response = await auth.googleAuth({
        idToken: credential,
        locale: locale ?? "",
        tenantName: tenantName ?? "",
      } as any);
      const tokens = response.tokens;
      if (!tokens?.accessToken || !tokens?.refreshToken) {
        throw new Error(t("loginError"));
      }
      const defaultMembership = response.memberships?.find((m: any) => m.isDefault) ?? response.memberships?.[0];
      authStore.set({
        accessToken: tokens.accessToken,
        refreshToken: tokens.refreshToken,
        tenantId: defaultMembership?.tenant?.id ?? response.tenant?.id ?? "",
      });
      router.push("/");
    } catch (e: any) {
      onError?.(normalizeApiErrorMessage(e, t("loginError")));
    } finally {
      setLoading(false);
    }
  };

  const openGoogleAuth = () => {
    const clientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID;
    if (!clientId) {
      onError?.(t("googleConfigError"));
      return;
    }
    if (!readyRef.current || !window.google?.accounts?.id) {
      onError?.(t("loadingGoogle"));
      return;
    }
    window.google.accounts.id.initialize({
      client_id: clientId,
      callback: (response: { credential?: string }) => {
        if (!response?.credential) {
          onError?.(t("loginError"));
          return;
        }
        handleCredential(response.credential);
      },
    });
    window.google.accounts.id.prompt();
  };

  return (
    <Button
      type="button"
      className="w-full h-11 bg-white text-black hover:bg-white/90 border border-black/10"
      onClick={openGoogleAuth}
      loading={loading}
    >
      <span className="inline-flex items-center gap-2">
        <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-white text-[#4285F4] font-bold">
          G
        </span>
        {t("continueWithGoogle")}
      </span>
    </Button>
  );
}
