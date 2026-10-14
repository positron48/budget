import {test, expect} from "@playwright/test";
import {create, fromBinary, toBinary} from "@bufbuild/protobuf";
import {createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {TenantService} from "../proto/budget/v1/tenant_pb";
import {AssetService} from "../proto/budget/v1/asset_pb";

for (const locale of ["ru", "en"] as const) for (const scenario of ["success", "corrupt", "expired"] as const) test(`${locale} portfolio export ${scenario}`, async ({page, context}) => {
  const ru = locale === "ru", corrupted = scenario === "corrupt", expired = scenario === "expired";
  const bytes = Buffer.from(JSON.stringify({format: "synthetic", data: "a".repeat(300000)}));
  const sha256 = createHash("sha256").update(bytes).digest("hex");
  let starts = 0, chunks = 0, downloads = 0;
  page.on("download", () => downloads++);
  await context.addCookies([{name: "NEXT_LOCALE", value: locale, url: test.info().project.use.baseURL as string}]);
  await page.addInitScript(() => {localStorage.setItem("budget/access", "fixture"); localStorage.setItem("budget/tenant", "tenant");});
  await page.route("**/budget.v1.*/*", async route => {
    const path = new URL(route.request().url()).pathname;
    const service = [InvestmentService, TenantService, AssetService].find(s => path.includes(s.typeName + "/"));
    const method = service?.methods.find(m => path.endsWith("/" + m.name));
    const raw = route.request().postDataBuffer()!;
    const input = method ? fromBinary(method.input, raw.subarray(5, 5 + raw.readUInt32BE(1))) as any : {};
    let data: any = {}, grpcStatus = 0;
    if (method?.name === "ListMyTenants") data = {memberships: [{tenant: {id: "tenant"}, role: 1}]};
    if (method?.name === "StartInvestmentExport") {starts++; expect(input.requestKey).not.toBe(""); data = {job: {id: "archive", state: "pending"}};}
    if (method?.name === "GetInvestmentExport") data = {job: {id: "archive", state: expired && chunks ? "expired" : "ready", byteSize: BigInt(bytes.length), sha256}};
    if (method?.name === "DownloadInvestmentExport") {
      chunks++; if (expired) grpcStatus = 9; const offset = Number(input.offset);
      const block = Buffer.from(bytes.subarray(offset, offset + 256 * 1024));
      if (corrupted && offset === 0) block[0] ^= 1;
      data = {data: block, offset: input.offset, totalBytes: BigInt(bytes.length), sha256};
    }
    const body = method ? Buffer.from(toBinary(method.output, create(method.output, data))) : Buffer.alloc(0), head = Buffer.alloc(5); head.writeUInt32BE(body.length, 1);
    const trailer = Buffer.from(`grpc-status: ${grpcStatus}\r\n`), end = Buffer.alloc(5); end[0] = 128; end.writeUInt32BE(trailer.length, 1);
    await route.fulfill({status: 200, contentType: "application/grpc-web+proto", body: Buffer.concat([head, body, end, trailer])});
  });
  await page.goto("/investments/accounts");
  await page.getByRole("button", {name: ru ? "Скачать архив" : "Export archive", exact: true}).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText(ru ? "всех инвестиционных счетов" : "all investment accounts");
  await dialog.getByRole("button", {name: ru ? "Подготовить архив" : "Prepare archive", exact: true}).click();
  const button = dialog.getByRole("button", {name: ru ? "Скачать JSON" : "Download JSON", exact: true});
  await expect(button).toBeVisible();
  if (scenario === "success") {
    await page.reload();
    await page.getByRole("button", {name: ru ? "Скачать архив" : "Export archive", exact: true}).click();
    await expect(button).toBeVisible();
    expect(starts).toBe(1);
  }
  await page.setViewportSize({width: 390, height: 844});
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  if (process.env.E2E_PORTFOLIO_EXPORT_PREVIEW && ru && scenario === "success") await page.screenshot({path: process.env.E2E_PORTFOLIO_EXPORT_PREVIEW});
  if (expired) {
    await button.click();
    await expect(dialog.getByRole("button", {name: ru ? "Подготовить архив" : "Prepare archive", exact: true})).toBeVisible();
    expect(downloads).toBe(0);
  } else if (corrupted) {
    await button.click(); await expect(dialog.getByRole("alert")).toBeVisible(); expect(downloads).toBe(0);
  } else {
    const pending = page.waitForEvent("download"); await button.click(); const file = await pending;
    expect(file.suggestedFilename()).toBe("budget-investments-archive.json");
    expect(await readFile((await file.path())!)).toEqual(bytes);
  }
  expect(starts).toBe(1); expect(chunks).toBe(expired ? 1 : 2);
});
