import type { Interceptor, UnaryRequest, UnaryResponse } from "@connectrpc/connect";

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


