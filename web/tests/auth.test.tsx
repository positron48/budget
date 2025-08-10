import React from "react";
import { screen } from "@testing-library/react";
import { renderWithIntl } from "./utils";
import LoginForm from "@/components/LoginForm";
import { describe, it, expect } from "vitest";

describe("Login page", () => {
  it("renders form", () => {
    renderWithIntl(<LoginForm />, { 
      locale: "en", 
      messages: { 
        auth: { 
          login: { 
            title: "Login", 
            email: "Email", 
            password: "Password", 
            submit: "Sign in", 
            submitting: "Signing in..." 
          } 
        } 
      } 
    });
    expect(screen.getByText(/signIn/i)).toBeTruthy();
  });
});


