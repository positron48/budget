import { vi } from 'vitest';
import React from 'react';

// Make React available globally
global.React = React;

// Mock Next.js navigation
vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    forward: vi.fn(),
    refresh: vi.fn(),
    prefetch: vi.fn(),
  }),
}));

// Mock Next.js internationalization
vi.mock("next-intl", () => ({
  useTranslations: () => (key: string) => key,
  NextIntlClientProvider: ({ children }: { children: any }) => children,
}));

// Mock providers
vi.mock("@/app/providers", () => ({
  useClients: () => ({ 
    auth: { 
      login: vi.fn().mockResolvedValue({ tokens: {} }),
      register: vi.fn().mockResolvedValue({ tokens: {} }),
      refreshToken: vi.fn().mockResolvedValue({ tokens: {} }),
    } 
  }),
}));

// Mock auth store
vi.mock("@/lib/auth/store", () => ({
  authStore: {
    getAccess: vi.fn(),
    getRefresh: vi.fn(),
    getTenant: vi.fn(),
    set: vi.fn(),
    clear: vi.fn(),
  },
}));

// Mock react-hook-form
vi.mock("react-hook-form", () => ({
  useForm: () => ({
    register: vi.fn(),
    handleSubmit: (fn: any) => fn,
    formState: { errors: {} },
  }),
}));

// Mock @hookform/resolvers
vi.mock("@hookform/resolvers", () => ({
  zodResolver: vi.fn(),
}));
