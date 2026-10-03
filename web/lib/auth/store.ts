export type AuthState = {
  accessToken?: string;
  refreshToken?: string;
  tenantId?: string;
};

const ACCESS_KEY = "budget/access";
const REFRESH_KEY = "budget/refresh";
const TENANT_KEY = "budget/tenant";

export const AUTH_CHANGED_EVENT = "budget-auth-changed";
export const TENANT_CHANGED_EVENT = "budget-tenant-changed";

export const authStore = {
  getAccess(): string | undefined {
    if (typeof window === "undefined") return undefined;
    return window.localStorage.getItem(ACCESS_KEY) ?? undefined;
  },
  getAccessToken(): string | undefined {
    if (typeof window === "undefined") return undefined;
    return window.localStorage.getItem(ACCESS_KEY) ?? undefined;
  },
  getRefresh(): string | undefined {
    if (typeof window === "undefined") return undefined;
    return window.localStorage.getItem(REFRESH_KEY) ?? undefined;
  },
  getTenant(): string | undefined {
    if (typeof window === "undefined") return undefined;
    const selected = window.localStorage.getItem(TENANT_KEY);
    if (selected) return selected;
    // Existing sessions can have tokens without an explicit budget selection.
    // Match the server's JWT default; this is a UI hint, not token validation.
    try {
      const token = window.localStorage.getItem(ACCESS_KEY);
      const parts = token?.split(".");
      if (parts?.length !== 3) return undefined;
      const payload = parts[1].replace(/-/g, "+").replace(/_/g, "/");
      const claims = JSON.parse(atob(payload.padEnd(Math.ceil(payload.length / 4) * 4, "=")));
      return typeof claims?.tenant_id === "string" && claims.tenant_id
        ? claims.tenant_id
        : undefined;
    } catch {
      return undefined;
    }
  },
  getLocale(): string | undefined {
    if (typeof window === "undefined") return undefined;
    const cookies = document.cookie.split(';');
    const localeCookie = cookies.find(cookie => cookie.trim().startsWith('NEXT_LOCALE='));
    return localeCookie ? localeCookie.split('=')[1] : undefined;
  },
  set(state: Partial<AuthState>) {
    if (typeof window === "undefined") return;
    if (state.accessToken !== undefined) {
      window.localStorage.setItem(ACCESS_KEY, state.accessToken);
    }
    if (state.refreshToken !== undefined) {
      window.localStorage.setItem(REFRESH_KEY, state.refreshToken);
    }
    if (state.tenantId !== undefined) {
      window.localStorage.setItem(TENANT_KEY, state.tenantId);
      window.dispatchEvent(new Event(TENANT_CHANGED_EVENT));
    }
    window.dispatchEvent(new Event(AUTH_CHANGED_EVENT));
  },
  clear() {
    if (typeof window === "undefined") return;
    window.localStorage.removeItem(ACCESS_KEY);
    window.localStorage.removeItem(REFRESH_KEY);
    window.localStorage.removeItem(TENANT_KEY);
    window.dispatchEvent(new Event(AUTH_CHANGED_EVENT));
    window.dispatchEvent(new Event(TENANT_CHANGED_EVENT));
  },
};

