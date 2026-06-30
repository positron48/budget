export type Theme = "light" | "dark";

export const THEME_COOKIE = "budget_theme";
export const THEME_CHANGED_EVENT = "budget-theme-changed";
export const DEFAULT_THEME: Theme = "dark";

function readCookie(name: string): string | undefined {
  if (typeof document === "undefined") return undefined;
  const match = document.cookie
    .split(";")
    .map((c) => c.trim())
    .find((c) => c.startsWith(`${name}=`));
  return match ? decodeURIComponent(match.split("=")[1]) : undefined;
}

export const themeStore = {
  get(): Theme {
    const v = readCookie(THEME_COOKIE);
    return v === "light" ? "light" : v === "dark" ? "dark" : DEFAULT_THEME;
  },
  apply(theme: Theme) {
    if (typeof document === "undefined") return;
    const root = document.documentElement;
    root.classList.toggle("dark", theme === "dark");
  },
  set(theme: Theme) {
    if (typeof document === "undefined") return;
    document.cookie = `${THEME_COOKIE}=${theme}; path=/; max-age=${60 * 60 * 24 * 365}`;
    this.apply(theme);
    window.dispatchEvent(new Event(THEME_CHANGED_EVENT));
  },
  toggle(): Theme {
    const next: Theme = this.get() === "dark" ? "light" : "dark";
    this.set(next);
    return next;
  },
};
