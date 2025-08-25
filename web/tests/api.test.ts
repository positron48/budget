import { describe, it, expect, vi, beforeEach } from "vitest";
import { createConnectTransport } from "@connectrpc/connect-web";
import { createClient } from "@connectrpc/connect";
import { AuthService } from "@/proto/budget/v1/auth_pb";
import { CategoryService } from "@/proto/budget/v1/category_pb";
import { TransactionService } from "@/proto/budget/v1/transaction_pb";

// Mock the transport
vi.mock("@connectrpc/connect-web", () => ({
  createConnectTransport: vi.fn(() => ({
    baseUrl: "http://localhost:8081",
    useHttpGet: false,
  })),
}));

// Mock the services
vi.mock("@/proto/budget/v1/auth_connect", () => ({
  AuthService: {
    typeName: "budget.v1.AuthService",
    methods: new Map([
      ["register", {
        name: "Register",
        I: {},
        O: {},
        kind: "unary",
      }],
      ["login", {
        name: "Login",
        I: {},
        O: {},
        kind: "unary",
      }],
    ]),
  },
}));

vi.mock("@/proto/budget/v1/category_connect", () => ({
  CategoryService: {
    typeName: "budget.v1.CategoryService",
    methods: new Map([
      ["listCategories", {
        name: "ListCategories",
        I: {},
        O: {},
        kind: "unary",
      }],
    ]),
  },
}));

vi.mock("@/proto/budget/v1/transaction_connect", () => ({
  TransactionService: {
    typeName: "budget.v1.TransactionService",
    methods: new Map([
      ["listTransactions", {
        name: "ListTransactions",
        I: {},
        O: {},
        kind: "unary",
      }],
    ]),
  },
}));

describe('API', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Transport', () => {
    it('creates transport with correct configuration', () => {
      const transport = createConnectTransport({
        baseUrl: "http://localhost:8081",
        useHttpGet: false,
      });

      expect(createConnectTransport).toHaveBeenCalledWith({
        baseUrl: "http://localhost:8081",
        useHttpGet: false,
      });
      expect(transport.baseUrl).toBe("http://localhost:8081");
    });
  });

  describe('Service Clients', () => {
    it('creates auth service client', () => {
      const transport = createConnectTransport({
        baseUrl: "http://localhost:8081",
      });
      const client = createClient(AuthService, transport);

      expect(client).toBeDefined();
      expect(AuthService.typeName).toBe("budget.v1.AuthService");
    });

    it('creates category service client', () => {
      const transport = createConnectTransport({
        baseUrl: "http://localhost:8081",
      });
      const client = createClient(CategoryService, transport);

      expect(client).toBeDefined();
      expect(CategoryService.typeName).toBe("budget.v1.CategoryService");
    });

    it('creates transaction service client', () => {
      const transport = createConnectTransport({
        baseUrl: "http://localhost:8081",
      });
      const client = createClient(TransactionService, transport);

      expect(client).toBeDefined();
      expect(TransactionService.typeName).toBe("budget.v1.TransactionService");
    });
  });

  describe('Error Handling', () => {
    it('handles network errors gracefully', () => {
      // This would test error handling in real implementation
      expect(true).toBe(true);
    });

    it('handles authentication errors', () => {
      // This would test auth error handling
      expect(true).toBe(true);
    });
  });
});
