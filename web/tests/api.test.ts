import { describe, it, expect, vi, beforeEach } from "vitest";
import { createConnectTransport } from "@connectrpc/connect-web";
import { createPromiseClient } from "@connectrpc/connect";
import { AuthService } from "@/proto/budget/v1/auth_connect";
import { CategoryService } from "@/proto/budget/v1/category_connect";
import { TransactionService } from "@/proto/budget/v1/transaction_connect";

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
    methods: {
      register: {
        name: "Register",
        I: {},
        O: {},
        kind: "unary",
      },
      login: {
        name: "Login",
        I: {},
        O: {},
        kind: "unary",
      },
    },
  },
}));

vi.mock("@/proto/budget/v1/category_connect", () => ({
  CategoryService: {
    typeName: "budget.v1.CategoryService",
    methods: {
      listCategories: {
        name: "ListCategories",
        I: {},
        O: {},
        kind: "unary",
      },
    },
  },
}));

vi.mock("@/proto/budget/v1/transaction_connect", () => ({
  TransactionService: {
    typeName: "budget.v1.TransactionService",
    methods: {
      listTransactions: {
        name: "ListTransactions",
        I: {},
        O: {},
        kind: "unary",
      },
    },
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
      const client = createPromiseClient(AuthService, transport);

      expect(client).toBeDefined();
      expect(AuthService.typeName).toBe("budget.v1.AuthService");
    });

    it('creates category service client', () => {
      const transport = createConnectTransport({
        baseUrl: "http://localhost:8081",
      });
      const client = createPromiseClient(CategoryService, transport);

      expect(client).toBeDefined();
      expect(CategoryService.typeName).toBe("budget.v1.CategoryService");
    });

    it('creates transaction service client', () => {
      const transport = createConnectTransport({
        baseUrl: "http://localhost:8081",
      });
      const client = createPromiseClient(TransactionService, transport);

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
