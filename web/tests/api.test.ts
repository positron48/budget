import { describe, it, expect, vi } from "vitest";
import { createConnectTransport } from "@connectrpc/connect-web";
import { createClient, Code, ConnectError } from "@connectrpc/connect";
import { AuthService } from "@/proto/budget/v1/auth_pb";
import { CategoryService } from "@/proto/budget/v1/category_pb";
import { TransactionService } from "@/proto/budget/v1/transaction_pb";

describe("API transport and generated clients", () => {
  it("creates callable clients from the current protobuf descriptors", () => {
    const transport = createConnectTransport({ baseUrl: "https://budget.example.test/grpc", useHttpGet: false });
    expect(createClient(AuthService, transport).login).toBeTypeOf("function");
    expect(createClient(CategoryService, transport).listCategories).toBeTypeOf("function");
    expect(createClient(TransactionService, transport).listTransactions).toBeTypeOf("function");
  });

  it.each([[503, Code.Unavailable], [401, Code.Unauthenticated]])("preserves HTTP %s errors through an actual client call", async (status, code) => {
    const fetch = vi.fn(async () => new Response("", { status }));
    const transport = createConnectTransport({ baseUrl: "https://budget.example.test/grpc", useHttpGet: false, fetch });
    const client = createClient(AuthService, transport);
    await expect(client.login({ email: "test@example.test", password: "synthetic" })).rejects.toMatchObject({ code });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch).toHaveBeenCalledWith("https://budget.example.test/grpc/budget.v1.AuthService/Login", expect.objectContaining({ method: "POST" }));
  });

  it("propagates a rejected fetch as a ConnectError", async () => {
    const fetch = vi.fn(async () => { throw new TypeError("synthetic offline"); });
    const client = createClient(AuthService, createConnectTransport({ baseUrl: "https://budget.example.test/grpc", fetch }));
    await expect(client.login({ email: "test@example.test", password: "synthetic" })).rejects.toBeInstanceOf(ConnectError);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
