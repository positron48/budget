import type { Interceptor } from "@connectrpc/connect";
import { Code, createClient } from "@connectrpc/connect";
import { AuthService } from "@/proto/budget/v1/auth_connect";
import { transportBaseUrl } from "./transport";

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

export function refreshAuthInterceptor(
  opts: {
    getRefreshToken: () => string | undefined;
    setTokens: (accessToken: string, refreshToken: string) => void;
    onRefreshFail?: () => void;
  }
): Interceptor {
  let refreshing = false;
  let refreshPromise: Promise<any> | null = null;

  return (next) => async (req: any) => {
    try {
      return await next(req);
    } catch (e: any) {
      const isAuthRefresh = typeof req?.url === "string" && req.url.includes("AuthService/RefreshToken");
      const code: number | undefined = e?.code;
      const unauth = code === Code.Unauthenticated || /unauth/i.test(String(e?.message ?? ""));
      
      if (!unauth || isAuthRefresh) throw e;

      console.log("🔄 Token refresh needed:", { code, message: e?.message, url: req?.url });

      // If already refreshing, wait for the current refresh to complete
      if (refreshing && refreshPromise) {
        console.log("⏳ Waiting for existing refresh to complete...");
        try {
          await refreshPromise;
          // Retry the original request with the new token
          const token = opts.getRefreshToken();
          if (token) {
            req.header.set("authorization", `Bearer ${token}`);
            return await next(req);
          }
        } catch (refreshError) {
          console.error("❌ Refresh failed while waiting:", refreshError);
          // Refresh failed, throw original error
          throw e;
        }
      }

      // Start refresh process
      console.log("🚀 Starting token refresh...");
      refreshing = true;
      refreshPromise = (async () => {
        try {
          const refreshToken = opts.getRefreshToken();
          if (!refreshToken) {
            throw new Error("No refresh token available");
          }

          console.log("📡 Calling refresh token endpoint...");

          // Create a bare transport without interceptors to avoid recursion
          const { createGrpcWebTransport } = await import("@connectrpc/connect-web");
          const bareTransport = createGrpcWebTransport({ 
            baseUrl: transportBaseUrl,
          });
          
          const authClient = createClient(AuthService, bareTransport);
          const resp: any = await authClient.refreshToken({ refreshToken });
          
          const newAccess = resp?.tokens?.accessToken;
          const newRefresh = resp?.tokens?.refreshToken;
          
          if (!newAccess) {
            throw new Error("No access token in refresh response");
          }

          console.log("✅ Token refresh successful");
          opts.setTokens(newAccess, newRefresh || refreshToken);
          return newAccess;
        } catch (refreshError) {
          console.error("❌ Token refresh failed:", refreshError);
          if (opts.onRefreshFail) {
            opts.onRefreshFail();
          }
          throw refreshError;
        } finally {
          refreshing = false;
          refreshPromise = null;
        }
      })();

      try {
        const newAccessToken = await refreshPromise;
        console.log("🔄 Retrying original request with new token...");
        // Retry the original request with the new access token
        req.header.set("authorization", `Bearer ${newAccessToken}`);
        return await next(req);
      } catch (refreshError) {
        console.error("❌ Failed to retry request after refresh:", refreshError);
        // Refresh failed, throw original error
        throw e;
      }
    }
  };
}


