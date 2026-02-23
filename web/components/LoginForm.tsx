"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import GoogleAuthButton from "./GoogleAuthButton";
import Icon from "./Icon";

export default function LoginForm() {
  const t = useTranslations("auth");
  const [error, setError] = useState("");

  return (
    <div className="space-y-6">
      {error && (
        <div className="bg-destructive/10 border border-destructive/20 rounded-md p-4 flex items-start space-x-3 mt-2">
          <Icon name="alert-circle" size={20} className="text-destructive mt-0.5 flex-shrink-0" />
          <p className="text-sm text-destructive">{error}</p>
        </div>
      )}

      <GoogleAuthButton onError={setError} />

      <p className="text-xs text-muted-foreground text-center">{t("googleOnlyHint")}</p>
    </div>
  );
}
