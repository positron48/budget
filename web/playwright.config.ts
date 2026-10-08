import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  workers: process.env.CI ? 2 : 4,
  use: { baseURL: process.env.E2E_BASE_URL || "http://127.0.0.1:3030", trace: "retain-on-failure", screenshot: "only-on-failure" },
  webServer: process.env.E2E_BASE_URL ? undefined : {
    command: "npm run start -- --hostname 127.0.0.1",
    url: "http://127.0.0.1:3030",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: { NEXT_PUBLIC_GRPC_BASE_URL: "http://127.0.0.1:3030/grpc" },
  },
});
