import { test, expect } from "@playwright/test";
import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import { RecurringPaymentService } from "../proto/budget/v1/recurring_payment_pb";
import { TenantService } from "../proto/budget/v1/tenant_pb";
import { TransactionService } from "../proto/budget/v1/transaction_pb";
import { CategoryService } from "../proto/budget/v1/category_pb";
import { AssetService } from "../proto/budget/v1/asset_pb";
import { InvestmentService } from "../proto/budget/v1/investment_pb";
import { ReportService } from "../proto/budget/v1/report_pb";
for (const locale of ["ru", "en"])
  test(`recurring payment lifecycle ${locale} at 390px`, async ({
    page,
    context,
  }, info) => {
    const services = [
      InvestmentService,
      RecurringPaymentService,
      TenantService,
      TransactionService,
      CategoryService,
      AssetService,
      ReportService,
    ];
    await context.addCookies([
      { name: "NEXT_LOCALE", value: locale, url: info.project.use.baseURL! },
    ]);
    await page.addInitScript(() => {
      localStorage.setItem("budget/access", "fixture");
      localStorage.setItem("budget/tenant", "tenant-a");
    });
    const now = new Date(),
      month = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`,
      prior = new Date(now.getFullYear(), now.getMonth() - 1, 1),
      priorMonth = `${prior.getFullYear()}-${String(prior.getMonth() + 1).padStart(2, "0")}`;
    const money = { currencyCode: "RUB", minorUnits: 800000n };
    let paid = false,
      version = 1n,
      confirms = 0,
      templates = 0,
      failNext = false;
    const seen: any[] = [];
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const occurrence = () => ({
      id: "utilities",
      templateId: "template",
      scheduledMonth: month,
      periodMonth: priorMonth,
      dueDate: `${month}-10`,
      name: "Utilities",
      categoryId: "housing",
      currencyCode: "RUB",
      timezone: "Europe/Moscow",
      amountMode: "variable",
      expectedAmount: paid ? 845000n : 800000n,
      exactAmount: paid ? 845000n : undefined,
      estimateSource: paid ? "exact" : "initial",
      status: paid ? "paid" : "planned",
      version,
      displayAmount: paid ? { ...money, minorUnits: 845000n } : money,
      transaction: paid
        ? {
            id: "expense",
            type: 2,
            amount: { ...money, minorUnits: 845000n },
            occurredAt: {
              seconds: BigInt(Math.floor(Date.now() / 1000) - 3600),
            },
            comment: "Utilities",
          }
        : undefined,
    });
    await page.route("**/budget.v1.*/*", async (route) => {
      const path = new URL(route.request().url()).pathname,
        service = services.find((s) => path.includes(s.typeName + "/")),
        method = service?.methods.find((m) => path.endsWith("/" + m.name));
      if (!method) throw Error(`Missing fixture ${path}`);
      const wire = route.request().postDataBuffer()!,
        input: any = fromBinary(
          method.input,
          wire.subarray(5, 5 + wire.readUInt32BE(1)),
        );
      seen.push({ method: method.name, input });
      let data: any = {};
      if (method.name === "ListMyTenants")
        data = {
          memberships: [
            {
              tenant: {
                id: "tenant-a",
                name: "Test",
                defaultCurrencyCode: "RUB",
              },
              role: 1,
            },
          ],
        };
      if (method.name === "ListCategories")
        data = {
          categories: [
            {
              id: "housing",
              code: "housing",
              kind: 2,
              translations: [
                { locale, name: locale === "ru" ? "Жильё" : "Housing" },
              ],
            },
          ],
        };
      if (method.name === "ListAccounts") data = { accounts: [] };
      if (method.name === "ListTransactions")
        data = {
          transactions: [
            {
              id: "existing",
              categoryId: "housing",
              type: 2,
              amount: { ...money, minorUnits: 845000n },
              occurredAt: {
                seconds: BigInt(Math.floor(Date.now() / 1000) - 7200),
              },
              comment: "Existing rent",
            },
          ],
        };
      const summary = {
        exact: paid
          ? { ...money, minorUnits: 0n }
          : { ...money, minorUnits: 0n },
        estimated: paid ? { ...money, minorUnits: 0n } : money,
        paid: paid
          ? { ...money, minorUnits: 845000n }
          : { ...money, minorUnits: 0n },
        total: 1,
        outstanding: paid ? 0 : 1,
      };
      if (method.name === "GetPaymentOverview")
        data = {
          summary,
          occurrences: paid ? [] : [occurrence()],
          previousDebts: [
            {
              ...occurrence(),
              id: "debt",
              name: "Old debt",
              dueDate: `${priorMonth}-10`,
              status: "overdue",
            },
          ],
          previousSummary: { total: 1, outstanding: 1 },
        };
      if (method.name === "ListPaymentOccurrences")
        data = { summary, occurrences: [occurrence()], total: 1 };
      if (method.name === "ListRecurringPaymentTemplates")
        data = { templates: [] };
      if (method.name === "CreateRecurringPaymentTemplate") {
        templates++;
        data = {
          template: { ...input.template, id: "new-template", version: 1n },
        };
      }
      if (method.name === "ConfirmPayment") {
        confirms++;
        if (failNext) {
          failNext = false;
          await route.fulfill({
            status: 200,
            contentType: "application/grpc-web+proto",
            body: Buffer.concat([
              Buffer.from([
                128,
                0,
                0,
                0,
                Buffer.byteLength(
                  "grpc-status: 10\r\ngrpc-message: conflict\r\n",
                ),
              ]),
              Buffer.from("grpc-status: 10\r\ngrpc-message: conflict\r\n"),
            ]),
          });
          return;
        }
        expect(input.requestKey).toBeTruthy();
        expect(input.expectedVersion).toBe(version);
        if (input.expense.case === "newExpense") {
          expect(input.expense.value.amount).toBe(845000n);
          expect(input.expense.value.currencyCode).toBe("RUB");
        } else expect(input.expense.value.transactionId).toBe("existing");
        paid = true;
        version++;
        data = { occurrence: occurrence() };
      }
      if (method.name === "UnlinkPayment") {
        paid = false;
        version++;
        data = { occurrence: occurrence() };
      }
      const body = Buffer.from(
          toBinary(method.output, create(method.output, data)),
        ),
        header = Buffer.alloc(5);
      header.writeUInt32BE(body.length, 1);
      const trailer = Buffer.from("grpc-status: 0\r\n"),
        end = Buffer.alloc(5);
      end[0] = 128;
      end.writeUInt32BE(trailer.length, 1);
      await route.fulfill({
        status: 200,
        contentType: "application/grpc-web+proto",
        body: Buffer.concat([header, body, end, trailer]),
      });
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/");
    await expect(page.getByText("Utilities", { exact: true })).toBeVisible();
    await expect(page.getByText("Old debt", { exact: false })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page
      .getByRole("button", {
        name: locale === "ru" ? "Отметить оплату" : "Record payment",
        exact: true,
      })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByLabel(locale === "ru" ? "Точная сумма" : "Exact amount", {
        exact: true,
      })
      .fill("8450");
    await dialog
      .getByRole("button", {
        name: locale === "ru" ? "Подтвердить" : "Confirm",
        exact: true,
      })
      .dblclick();
    await expect(dialog).toHaveCount(0);
    expect(confirms).toBe(1);
    await expect(
      page.getByText(
        locale === "ru"
          ? "В этом месяце всё оплачено"
          : "All payments for this month are settled",
      ),
    ).toBeVisible();
    await page.goto(`/payments?month=${month}`);
    await expect(page.getByText("Utilities", { exact: true })).toBeVisible();
    await page
      .getByRole("button", {
        name: locale === "ru" ? "Снять связь" : "Unlink expense",
        exact: true,
      })
      .click();
    await expect(dialog).toContainText(
      locale === "ru" ? "Расход останется в журнале" : "expense will remain",
    );
    await dialog
      .getByRole("button", {
        name: locale === "ru" ? "Подтвердить" : "Confirm",
        exact: true,
      })
      .click();
    await expect(dialog).toHaveCount(0);
    await page
      .getByRole("button", {
        name: locale === "ru" ? "Отметить оплату" : "Record payment",
        exact: true,
      })
      .click();
    await dialog
      .getByLabel(
        locale === "ru" ? "Уже внесённый расход" : "Existing expense",
        { exact: true },
      )
      .check();
    await dialog.getByText("Existing rent", { exact: false }).click();
    failNext = true;
    await dialog
      .getByRole("button", {
        name: locale === "ru" ? "Подтвердить" : "Confirm",
        exact: true,
      })
      .click();
    await expect(dialog.getByRole("alert")).toBeVisible();
    await dialog
      .getByRole("button", {
        name: locale === "ru" ? "Подтвердить" : "Confirm",
        exact: true,
      })
      .click();
    await expect(dialog).toHaveCount(0);
    expect(confirms).toBe(3);
    await page
      .getByRole("button", {
        name: locale === "ru" ? "Новый шаблон" : "New template",
        exact: true,
      })
      .click();
    await dialog
      .getByLabel(locale === "ru" ? "Название" : "Name", { exact: true })
      .fill("Internet");
    await dialog
      .getByLabel(locale === "ru" ? "Точная сумма" : "Exact amount", {
        exact: true,
      })
      .fill("1200");
    await dialog
      .getByRole("textbox", {
        name: locale === "ru" ? "Категория" : "Category",
        exact: true,
      })
      .click();
    await page
      .getByText(locale === "ru" ? "Жильё" : "Housing", { exact: true })
      .last()
      .click();
    await dialog
      .getByRole("button", {
        name: locale === "ru" ? "Сохранить" : "Save",
        exact: true,
      })
      .click();
    await expect(dialog).toHaveCount(0);
    expect(templates).toBe(1);
    await page.screenshot({
      path: info.outputPath(`payments-${locale}-390.png`),
      fullPage: true,
    });
    expect(errors).toEqual([]);
    expect(
      seen.some(
        (x) => x.method === "GetPaymentOverview" && x.input.month === month,
      ),
    ).toBe(true);
  });
