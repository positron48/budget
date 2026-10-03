import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { authInterceptor, refreshAuthInterceptor } from "@/lib/api/interceptors";

const { refreshToken } = vi.hoisted(() => ({ refreshToken: vi.fn() }));

vi.mock("@/lib/api/transport", () => ({ transportBaseUrl: "http://localhost:8081" }));
vi.mock("@connectrpc/connect-web", () => ({ createGrpcWebTransport: vi.fn(() => ({})) }));
vi.mock("@connectrpc/connect", async (importOriginal) => ({
  ...await importOriginal<typeof import("@connectrpc/connect")>(),
  createClient: vi.fn(() => ({ refreshToken })),
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

describe("authentication refresh", () => {
  let access: string;
  let refresh: string;
  let onRefreshFail: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    refreshToken.mockReset();
    access = "expired-access";
    refresh = "old-refresh";
    onRefreshFail = vi.fn();
  });

  function client(next: any) {
    return refreshAuthInterceptor({
      getAccessToken: () => access,
      getRefreshToken: () => refresh,
      setTokens: (newAccess, newRefresh) => { access = newAccess; refresh = newRefresh; },
      onRefreshFail,
    })(authInterceptor(() => access)(next));
  }

  function request() {
    return { url: "http://localhost:8081/budget.v1.TransactionService/ListTransactions", header: new Headers() } as any;
  }

  function expiredNext() {
    return vi.fn(async (req) => {
      if (req.header.get("authorization") !== "Bearer fresh-access") {
        throw new ConnectError("expired access token", Code.Unauthenticated);
      }
      return { message: "success" };
    });
  }

  it("shares one refresh between separate clients and retries every request with the access token", async () => {
    const pending = deferred<any>();
    // Rotation can revoke the old token before the first response reaches the browser.
    refreshToken.mockReturnValueOnce(pending.promise)
      .mockRejectedValue(new ConnectError("refresh token revoked", Code.Unauthenticated));
    const headerNext = expiredNext();
    const dashboardNext = expiredNext();
    const recentNext = expiredNext();
    const dashboard = client(dashboardNext);
    const calls = Promise.allSettled([
      client(headerNext)(request()),
      dashboard(request()),
      dashboard(request()),
      client(recentNext)(request()),
    ]);
    // Allow all independent transports to reach the refresh endpoint.
    await vi.waitFor(() => expect(refreshToken).toHaveBeenCalled());
    const refreshCount = refreshToken.mock.calls.length;
    pending.resolve({ tokens: { accessToken: "fresh-access", refreshToken: "fresh-refresh" } });
    const results = await calls;

    expect(results.every((result) => result.status === "fulfilled")).toBe(true);
    expect(refreshCount).toBe(1);
    expect(refreshToken).toHaveBeenCalledWith({ refreshToken: "old-refresh" });
    for (const next of [headerNext, recentNext]) expect(next).toHaveBeenCalledTimes(2);
    expect(dashboardNext).toHaveBeenCalledTimes(4);
    expect(access).toBe("fresh-access");
    expect(refresh).toBe("fresh-refresh");
    expect(onRefreshFail).not.toHaveBeenCalled();
  });

  it("reuses the new access token when an old request fails after refresh has finished", async () => {
    refreshToken.mockResolvedValue({ tokens: { accessToken: "fresh-access", refreshToken: "fresh-refresh" } });
    const lateResponse = deferred<any>();
    const lateNext = vi.fn()
      .mockImplementationOnce(() => lateResponse.promise)
      .mockImplementation(expiredNext());
    const lateCall = client(lateNext)(request());
    const lateResult = expect(lateCall).resolves.toEqual({ message: "success" });
    await client(expiredNext())(request());
    lateResponse.reject(new ConnectError("expired access token", Code.Unauthenticated));
    await lateResult;

    expect(refreshToken).toHaveBeenCalledTimes(1);
    expect(lateNext).toHaveBeenCalledTimes(2);
    expect(onRefreshFail).not.toHaveBeenCalled();
  });

  it("preserves the session on a temporary refresh outage and allows the next attempt", async () => {
    refreshToken.mockRejectedValueOnce(new ConnectError("service unavailable", Code.Unavailable))
      .mockResolvedValueOnce({ tokens: { accessToken: "fresh-access", refreshToken: "fresh-refresh" } });
    const call = client(expiredNext());
    await expect(call(request())).rejects.toMatchObject({ code: Code.Unauthenticated });
    expect(onRefreshFail).not.toHaveBeenCalled();
    await expect(call(request())).resolves.toEqual({ message: "success" });
    expect(refreshToken).toHaveBeenCalledTimes(2);
  });

  it("reports an invalid refresh token once to all concurrent requests", async () => {
    const pending = deferred<any>();
    refreshToken.mockReturnValue(pending.promise);
    const calls = Promise.allSettled([client(expiredNext())(request()), client(expiredNext())(request())]);
    await vi.waitFor(() => expect(refreshToken).toHaveBeenCalled());
    pending.reject(new ConnectError("invalid refresh token", Code.Unauthenticated));
    const results = await calls;

    expect(results.every((result) => result.status === "rejected")).toBe(true);
    expect(refreshToken).toHaveBeenCalledTimes(1);
    expect(onRefreshFail).toHaveBeenCalledTimes(1);
  });

  it("does not refresh or log out for a non-authentication error", async () => {
    const error = new ConnectError("service unavailable", Code.Unavailable);
    await expect(client(vi.fn().mockRejectedValue(error))(request())).rejects.toBe(error);
    expect(refreshToken).not.toHaveBeenCalled();
    expect(onRefreshFail).not.toHaveBeenCalled();
  });

  it("requires login when no refresh token remains, then allows a new session to refresh", async () => {
    refresh = "";
    await expect(client(expiredNext())(request())).rejects.toMatchObject({ code: Code.Unauthenticated });
    expect(refreshToken).not.toHaveBeenCalled();
    expect(onRefreshFail).toHaveBeenCalledTimes(1);
    refresh = "old-refresh";
    refreshToken.mockResolvedValue({ tokens: { accessToken: "fresh-access", refreshToken: "fresh-refresh" } });
    await expect(client(expiredNext())(request())).resolves.toEqual({ message: "success" });
  });

  it("does not recurse when the refresh endpoint itself rejects authentication", async () => {
    const req = request();
    req.url = "http://localhost:8081/budget.v1.AuthService/RefreshToken";
    await expect(client(expiredNext())(req)).rejects.toMatchObject({ code: Code.Unauthenticated });
    expect(refreshToken).not.toHaveBeenCalled();
  });

  it("does not treat a rejected retried request as refresh failure", async () => {
    refreshToken.mockResolvedValue({ tokens: { accessToken: "fresh-access", refreshToken: "fresh-refresh" } });
    const error = new ConnectError("access denied", Code.PermissionDenied);
    const next = vi.fn()
      .mockRejectedValueOnce(new ConnectError("expired access token", Code.Unauthenticated))
      .mockRejectedValueOnce(error);
    await expect(client(next)(request())).rejects.toBe(error);
    expect(onRefreshFail).not.toHaveBeenCalled();
  });
});
