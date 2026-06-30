"use client";

import { useEffect, useState } from "react";
import { useTranslations } from "next-intl";
import Icon from "./Icon";
import { themeStore, THEME_CHANGED_EVENT, type Theme } from "@/lib/theme/store";

export default function ThemeToggle() {
  const t = useTranslations("nav");
  const [theme, setTheme] = useState<Theme>("dark");
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setTheme(themeStore.get());
    setMounted(true);
    const onChange = () => setTheme(themeStore.get());
    window.addEventListener(THEME_CHANGED_EVENT, onChange);
    return () => window.removeEventListener(THEME_CHANGED_EVENT, onChange);
  }, []);

  const onToggle = () => setTheme(themeStore.toggle());

  // Avoid hydration mismatch: render a stable placeholder until mounted.
  const isDark = mounted ? theme === "dark" : true;

  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={t("toggleTheme")}
      title={t("toggleTheme")}
      className="inline-flex items-center justify-center w-9 h-9 rounded-md bg-muted text-muted-foreground hover:text-foreground transition-colors"
    >
      <Icon name={isDark ? "sun" : "moon"} size={18} />
    </button>
  );
}
