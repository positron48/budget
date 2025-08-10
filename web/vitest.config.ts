import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    include: ["tests/**/*.{ts,tsx}", "**/*.test.{ts,tsx}"],
  },
});


