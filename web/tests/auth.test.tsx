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
          continueWithGoogle: "Continue with Google",
          googleOnlyHint: "Sign in and sign up are available only via Google",
        } 
      } 
    });
    expect(screen.getByRole("button")).toBeTruthy();
  });
});
