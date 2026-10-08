import { test, expect } from "@playwright/test";
import { create, toBinary } from "@bufbuild/protobuf";
import { readdirSync } from "node:fs";
import { AuthService } from "../proto/budget/v1/auth_pb";
import { UserService } from "../proto/budget/v1/user_pb";
import { TenantService } from "../proto/budget/v1/tenant_pb";
import { CategoryService } from "../proto/budget/v1/category_pb";
import { TransactionService } from "../proto/budget/v1/transaction_pb";
import { ReportService } from "../proto/budget/v1/report_pb";
import { FxService } from "../proto/budget/v1/fx_pb";
import { AssetService } from "../proto/budget/v1/asset_pb";
import { CurrencyExchangeService } from "../proto/budget/v1/currency_exchange_pb";

const services = [AuthService, UserService, TenantService, CategoryService, TransactionService, ReportService, FxService, AssetService, CurrencyExchangeService];
const routes = readdirSync("app", { recursive: true }).filter((path) => typeof path === "string" && /(^|\/)page\.tsx$/.test(path)).map((path) => "/" + String(path).replace(/(^|\/)page\.tsx$/, "")).sort();
const account = { id: "test-account", name: "Smoke account", kind: "bank", version: 1n, balances: [{ amount: { currencyCode: "RUB", minorUnits: 112931988n } }] };

for (const locale of ["en", "ru"]) {
  for (const route of routes) {
    test(`${locale} ${route} renders without JavaScript errors`, async ({ page, context }) => {
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
      await context.addCookies([{ name: "NEXT_LOCALE", value: locale, url: test.info().project.use.baseURL as string }]);
      await page.addInitScript(() => {
        localStorage.setItem("budget/access", "smoke-token");
        localStorage.setItem("budget/tenant", "test-tenant");
      });
      // Exercise real transports, protobuf decoding, providers and query caching.
      // No production credentials or writes are needed for these fixtures.
      await page.route("**/budget.v1.*/*", async (request) => {
        const path = new URL(request.request().url()).pathname;
        const service = services.find((s) => path.includes(s.typeName + "/"));
        const method = service?.methods.find((m) => path.endsWith("/" + m.name));
        if (!method) throw new Error(`Missing RPC fixture: ${path}`);
        let data: any = {};
        if (method.name === "BatchGetRates") data = { rates: [{ fromCurrencyCode: "USD", toCurrencyCode: "RUB", rateDecimal: "80.25", provider: "cbr", asOf: { seconds: 1791417600n } }] };
        if (method.name === "GetAccount") data = { account };
        if (method.name === "ListAccounts" || method.name === "GetOverview") data = { accounts: [account], total: { currencyCode: "RUB", minorUnits: 112931988n } };
        const body = Buffer.from(toBinary(method.output, create(method.output, data)));
        const header = Buffer.alloc(5); header.writeUInt32BE(body.length, 1);
        const trailer = Buffer.from("grpc-status: 0\r\n");
        const trailerHeader = Buffer.alloc(5); trailerHeader[0] = 128; trailerHeader.writeUInt32BE(trailer.length, 1);
        await request.fulfill({ status: 200, contentType: "application/grpc-web+proto", body: Buffer.concat([header, body, trailerHeader, trailer]) });
      });
      const url = route.replace("[id]", "test-account");
      await page.goto(url);
      await page.waitForLoadState("networkidle");
      expect(errors).toEqual([]);
      await expect(page.locator("body > main, body main").first()).toBeVisible();
      await expect(page.locator("body > main, body main").first()).not.toBeEmpty();
      if (route === "/fx") {
        await expect(page.getByText("80.25", { exact: true })).toBeVisible();
        const refetch = page.waitForResponse((response) => response.url().endsWith("/BatchGetRates"));
        await page.locator('input[type="date"]').first().fill("2026-10-01");
        await refetch;
        await expect(page.getByText("80.25", { exact: true })).toBeVisible();
      }
      if (route.startsWith("/assets")) await expect(page.getByText("Smoke account", { exact: true })).toBeVisible();
      if (route === "/assets") {
        const summary = page.locator(".asset-summary").nth(1).locator(".tabular-nums");
        const rounded = new Intl.NumberFormat(locale, { style: "currency", currency: "RUB", maximumFractionDigits: 0, minimumFractionDigits: 0 }).format(1129320);
        await expect(summary).toHaveText(rounded);
        await expect(summary).toHaveAttribute("title", new Intl.NumberFormat(locale, { style: "currency", currency: "RUB" }).format(1129319.88));
        expect(await summary.evaluate((element) => getComputedStyle(element).whiteSpace)).toBe("nowrap");
        if (process.env.E2E_SCREENSHOT_DIR) await page.locator(".asset-summary").first().locator("..").screenshot({ path: `${process.env.E2E_SCREENSHOT_DIR}/assets-${locale}.png` });
      }
      if (route === "/tenants") await expect(page).toHaveURL(/\/account$/);
      else await expect(page).toHaveURL(new RegExp(url.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "$"));
      expect(errors).toEqual([]);
    });
  }
}
