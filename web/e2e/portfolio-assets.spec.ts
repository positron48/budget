import {test, expect} from "@playwright/test";
import {create, fromBinary, toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {AssetService} from "../proto/budget/v1/asset_pb";

for (const [locale, conflict] of [["ru", false], ["en", false], ["ru", true]] as const) test(`${locale} partial tracker replacement${conflict ? " after a stale preview" : ""}`, async ({page, context}) => {
  const ru = locale === "ru";
  let commits = 0, prepares = 0, attempts = 0, linked = false;
  const ledger = {id: "tracker", name: "Broker", portfolioTracking: true, version: 4n, metadataVersion: 1n};
  const account = {id: "manual", name: "Existing investment", kind: "investment", fixedCurrencyCode: "RUB", investmentMode: "manual", version: 3n};
  await context.addCookies([{name: "NEXT_LOCALE", value: locale, url: test.info().project.use.baseURL as string}]);
  await page.addInitScript(() => {localStorage.setItem("budget/access", "fixture"); localStorage.setItem("budget/tenant", "tenant");});
  await page.route("**/budget.v1.*/*", async route => {
    const path = new URL(route.request().url()).pathname;
    const service = [InvestmentService, TenantService, AssetService].find(s => path.includes(s.typeName + "/"));
    const method = service?.methods.find(m => path.endsWith("/" + m.name));
    const bytes = route.request().postDataBuffer()!;
    const input = method ? fromBinary(method.input, bytes.subarray(5, 5 + bytes.readUInt32BE(1))) as any : {};
    let data: any = {}, status = 0;
    if (method?.name === "ListMyTenants") data = {memberships: [{tenant: {id: "tenant"}, role: 1}]};
    if (method?.name === "ListInvestmentLedgers") data = {ledgers: [ledger]};
    if (method?.name === "ListAccounts") data = {accounts: [linked ? {...account, investmentMode: "ledger", investmentLedgerId: ledger.id} : account, {id: "bank", name: "Bank", kind: "bank"}]};
    if (method?.name === "PrepareInvestmentAssetActivation") {
      prepares++; expect(input.activation.accountId).toBe(account.id); expect(input.activation.ledgerId).toBe(ledger.id);
      expect(input.activation.conventionKey).toBe(""); expect(input.activation.valuationFrom).toBeDefined();
      data = {preview: {account, ledger, manualValue: {currencyCode: "RUB", minorUnits: 999999n}, ledgerKnownValue: {currencyCode: "RUB", minorUnits: 360000n}, complete: false, matches: false, issues: ["price_missing"], previewHash: "a".repeat(64)}};
    }
    if (method?.name === "ActivateInvestmentAsset") {
      expect(input.activation.expectedAccountVersion).toBe(3n); expect(input.activation.expectedLedgerVersion).toBe(4n);
      expect(input.activation.previewHash).toBe("a".repeat(64)); expect(input.activation.evidence).not.toBe("");
      attempts++;
      if (conflict && attempts === 1) status = 10;
      else {linked = true; commits++; data = {account: {...account, investmentMode: "ledger", investmentLedgerId: ledger.id}};}
    }
    const body = method ? Buffer.from(toBinary(method.output, create(method.output, data))) : Buffer.alloc(0), head = Buffer.alloc(5); head.writeUInt32BE(body.length, 1);
    const trailer = Buffer.from(`grpc-status: ${status}\r\n`), end = Buffer.alloc(5); end[0] = 128; end.writeUInt32BE(trailer.length, 1);
    await route.fulfill({status: 200, contentType: "application/grpc-web+proto", body: Buffer.concat([head, body, end, trailer])});
  });
  await page.goto("/investments/accounts");
  const link = page.getByRole("button", {name: ru ? "Включить в накопления" : "Include in savings", exact: true});
  if (process.env.E2E_INVESTMENTS_READ_ONLY === "true") {await expect(link).toHaveCount(0); return;}
  await link.click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox").selectOption("manual");
  await expect(dialog.getByRole("option", {name: "Bank"})).toHaveCount(0);
  await dialog.getByRole("button", {name: ru ? "Посмотреть оценку" : "Preview value", exact: true}).click();
  await expect(dialog.getByRole("status")).toContainText(ru ? "Оценка неполная" : "Valuation is partial");
  await expect(dialog).toContainText(ru ? "Стоимость известной части" : "Known securities value");
  expect(commits).toBe(0); expect(prepares).toBe(1);
  if (locale === "ru" && !conflict && process.env.E2E_PORTFOLIO_ASSET_PREVIEW) await page.screenshot({path: process.env.E2E_PORTFOLIO_ASSET_PREVIEW});
  await page.setViewportSize({width: 390, height: 844});
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await dialog.getByRole("button", {name: ru ? "Заменить ручную оценку" : "Replace manual estimate", exact: true}).click();
  if (conflict) {
    await expect(dialog.getByRole("alert")).toBeVisible();
    expect(commits).toBe(0); expect(attempts).toBe(1);
    await dialog.getByRole("button", {name: ru ? "Посмотреть оценку" : "Preview value", exact: true}).click();
    await dialog.getByRole("button", {name: ru ? "Заменить ручную оценку" : "Replace manual estimate", exact: true}).click();
    expect(prepares).toBe(2);
  }
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("link", {name: ru ? "В накоплениях" : "View in savings", exact: true})).toHaveAttribute("href", "/assets/manual");
  expect(commits).toBe(1);
});
