export type AuthState = {
  accessToken?: string;
  refreshToken?: string;
  tenantId?: string;
};

const ACCESS_KEY = "budget/access";
const REFRESH_KEY = "budget/refresh";
const TENANT_KEY = "budget/tenant";

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
    return window.localStorage.getItem(TENANT_KEY) ?? undefined;
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
    }
  },
  clear() {
    if (typeof window === "undefined") return;
    window.localStorage.removeItem(ACCESS_KEY);
    window.localStorage.removeItem(REFRESH_KEY);
    window.localStorage.removeItem(TENANT_KEY);
  },
};


