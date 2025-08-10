import React from "react";
import { screen } from "@testing-library/react";
import { renderWithIntl } from "./utils";
// avoid Next alias in vitest
import LoginForm from "@/components/LoginForm";
import { describe, it, expect, vi } from "vitest";

vi.mock("@/app/providers", () => ({
  useClients: () => ({ auth: { login: vi.fn().mockResolvedValue({ tokens: {} }) } }),
}));

describe("Login page", () => {
  it("renders form", () => {
    renderWithIntl(<LoginForm /> as any, { locale: "en", messages: { auth: { login: { title: "Login", email: "Email", password: "Password", submit: "Sign in", submitting: "Signing in..." } } } });
    expect(screen.getByText(/login/i)).toBeTruthy();
  });
});


