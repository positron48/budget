import type { Interceptor } from "@connectrpc/connect";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
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
  return (next) => async (req: any) => {
    try {
      return await next(req);
    } catch (e: any) {
      const alreadyRetried = req?.header?.get?.("x-retry-refreshed") === "1";
      const isAuthRefresh = typeof req?.url === "string" && req.url.includes("AuthService/RefreshToken");
      const code: number | undefined = e?.code;
      const unauth = code === Code.Unauthenticated || /unauth/i.test(String(e?.message ?? ""));
      if (!unauth || alreadyRetried || isAuthRefresh) throw e;

      const refreshToken = opts.getRefreshToken();
      if (!refreshToken) throw e;

      try {
        // create a bare transport without auth/refresh interceptors to avoid recursion
        const { createGrpcWebTransport } = require("@connectrpc/connect-web");
        const bareTransport = createGrpcWebTransport({ baseUrl: transportBaseUrl });
        const authClient = createClient(AuthService as any, bareTransport);
        const resp: any = await authClient.refreshToken({ refreshToken } as any);
        const newAccess = resp?.tokens?.accessToken;
        const newRefresh = resp?.tokens?.refreshToken ?? refreshToken;
        if (!newAccess) throw e;

        opts.setTokens(newAccess, newRefresh);
        // retry original request with new access token
        req.header.set("authorization", `Bearer ${newAccess}`);
        req.header.set("x-retry-refreshed", "1");
        return await next(req);
      } catch (e2) {
        if (opts.onRefreshFail) opts.onRefreshFail();
        throw e;
      }
    }
  };
}


