import { defineConfig } from "vitest/config";
import path from "path";

export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname),
    },
  },
  test: {
    environment: "jsdom",
    include: ["tests/**/*.{test,spec}.{ts,tsx}"],
    exclude: ["**/node_modules/**", "tests/utils.tsx"],
    setupFiles: ["./tests/setup.ts"],
    globals: true,
    coverage: {
      provider: "v8",
      reporter: ["text", "json", "html", "lcov"],
      exclude: [
        "node_modules/",
        "tests/",
        "**/*.d.ts",
        "**/*.config.*",
        "**/coverage/**",
        "**/.next/**",
        "**/dist/**",
        "**/build/**",
        "**/public/**",
        "**/proto/**",
        "**/i18n/**",
        "**/middleware.ts",
        "**/next.config.*",
        "**/postcss.config.*",
        "**/tailwind.config.*",
        "**/vitest.config.*"
      ],
      thresholds: {
        global: {
          branches: 70,
          functions: 70,
          lines: 70,
          statements: 70
        }
      }
    }
  },
});


