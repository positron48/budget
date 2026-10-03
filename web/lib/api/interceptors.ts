import type { Interceptor } from "@connectrpc/connect";
import { Code, createClient } from "@connectrpc/connect";
import { AuthService } from "@/proto/budget/v1/auth_pb";
import { transportBaseUrl } from "./transport";
import { normalizeApiErrorMessage } from "./errors";

export function authInterceptor(getAccessToken: () => string | undefined): Interceptor {
  return (next) => async (req) => {
    const token = getAccessToken();
    if (token) {
      req.header.set("authorization", `Bearer ${token}`);
    }
    return next(req);
  };
}

export function tenantInterceptor(getTenantId: () => string | undefined): Interceptor {
  return (next) => async (req) => {
    const tenantId = getTenantId();
    if (tenantId) {
      req.header.set("x-tenant-id", tenantId);
    }
    return next(req);
  };
}

export function loggingInterceptor(): Interceptor {
  return (next) => async (req: any) => next(req);
}

// All client providers use the same browser session and rotating refresh token.
// Keep the in-flight refresh shared across transports, including during navigation.
let refreshPromise: Promise<string> | null = null;

export function refreshAuthInterceptor(
  opts: {
    getAccessToken: () => string | undefined;
    getRefreshToken: () => string | undefined;
    setTokens: (accessToken: string, refreshToken: string) => void;
    onRefreshFail?: () => void;
  }
): Interceptor {
  return (next) => async (req: any) => {
    const originalAccess = opts.getAccessToken();
    try {
      return await next(req);
    } catch (e: any) {
      const isAuthRefresh = typeof req?.url === "string" && req.url.includes("AuthService/RefreshToken");
      const code: number | undefined = e?.code;
      const unauth = code === Code.Unauthenticated || /unauth/i.test(String(e?.message ?? ""));
      
      if (!unauth || isAuthRefresh) {
        // Clean up noisy bracketed prefixes before bubbling up
        e.message = normalizeApiErrorMessage(e, e?.message || "");
        throw e;
      }

      // An older request can fail after another request has already refreshed.
      const currentAccess = opts.getAccessToken();
      if (currentAccess && currentAccess !== originalAccess) {
        req.header.set("authorization", `Bearer ${currentAccess}`);
        return next(req);
      }

      if (!refreshPromise) {
        refreshPromise = (async () => {
          const refreshToken = opts.getRefreshToken();
          if (!refreshToken) {
            opts.onRefreshFail?.();
            throw new Error("No refresh token available");
          }
          try {
            // Use a bare transport so refreshing cannot recurse.
            const { createGrpcWebTransport } = await import("@connectrpc/connect-web");
            if (!transportBaseUrl) {
              throw new Error("NEXT_PUBLIC_GRPC_BASE_URL is not configured");
            }
            const bareTransport = createGrpcWebTransport({ baseUrl: transportBaseUrl });
            const authClient = createClient(AuthService, bareTransport);
            const resp = await authClient.refreshToken({ refreshToken });
            const newAccess = resp.tokens?.accessToken;
            if (!newAccess) {
              throw new Error("No access token in refresh response");
            }
            opts.setTokens(newAccess, resp.tokens?.refreshToken || refreshToken);
            return newAccess;
          } catch (refreshError: any) {
            // A network/server outage does not invalidate the saved session.
            if (refreshError?.code === Code.Unauthenticated) {
              opts.onRefreshFail?.();
            }
            throw refreshError;
          }
        })().finally(() => {
          refreshPromise = null;
        });
      }

      let newAccessToken: string;
      try {
        newAccessToken = await refreshPromise;
      } catch {
        throw e;
      }
      req.header.set("authorization", `Bearer ${newAccessToken}`);
      return next(req);
    }
  };
}

