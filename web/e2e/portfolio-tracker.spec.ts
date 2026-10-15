import {readFile} from "node:fs/promises";
import {test, expect} from "@playwright/test";
import {create, fromBinary, toBinary} from "@bufbuild/protobuf";
import {InvestmentService} from "../proto/budget/v1/investment_pb";
import {InvestmentImportService} from "../proto/budget/v1/investment_import_pb";
import {InvestmentNAVService} from "../proto/budget/v1/investment_nav_pb";
import {TimestampSchema} from "@bufbuild/protobuf/wkt";
import {TenantService} from "../proto/budget/v1/tenant_pb";

for (const locale of ["ru", "en"]) {
  test(`${locale} simple portfolio shows holdings and payouts and saves a purchase in one step`, async ({page, context}) => {
    const ru = locale === "ru", readOnly = process.env.E2E_INVESTMENTS_READ_ONLY === "true";
    let pendingHistory = false;
    let releaseQuotes!: () => void, releaseRates!: () => void;
    const quotesReady = new Promise<void>(resolve => {releaseQuotes = resolve;});
    const ratesReady = new Promise<void>(resolve => {releaseRates = resolve;});
    const calls: {method: string; input: any}[] = [], errors: string[] = [];
    page.on("pageerror", e => errors.push(e.message));
    await context.addCookies([{name: "NEXT_LOCALE", value: locale, url: test.info().project.use.baseURL as string}]);
    await page.addInitScript(() => {localStorage.setItem("budget/access", "synthetic-session"); localStorage.setItem("budget/tenant", "tenant");});
    const key = "tinvest:11111111-1111-4111-8111-111111111111";
    const ledger = {id: "account", name: "Broker", version: 1n, portfolioTracking: true, startedAt: {seconds: 1704067200n}};
    const position = {costBasis:"principal", instrumentKey: key, currency: "RUB", quantity: "10", cleanCost: "1000", acquiredAccrued: "0", realizedClean: "0", realizedAccrued: "0", lots: [{originId: "buy", quantity: "10", cleanCost: "1000", acquiredAccrued: "0", acquiredAt: {seconds: 1704153600n}}]};
    const operations: any[] = [{id: "buy", ledgerId: ledger.id, status: "posted", instrumentKey: key, currency: "RUB", kind: "buy", quantity: "10", amount: "1000", fee: "0", accrued: "0", occurredAt: {seconds: 1704153600n}}, {id: "payout", ledgerId: ledger.id, status: "posted", instrumentKey: key, currency: "RUB", kind: "dividend", quantity: "0", amount: "100", tax: "20", taxReviewRequired: true, occurredAt: {seconds: 1704240000n}}];
    const foreign = {instrumentKey: "tinvest:foreign", currency: "USD", quantity: "2", cleanCost: "200", acquiredAccrued: "0", lots:[{originId:"foreign-buy", quantity:"2", cleanCost:"200", acquiredAccrued:"0", acquiredAt:{seconds:1704153600n}}]};
    operations.push({id:"foreign-buy", ledgerId:ledger.id, status:"posted", instrumentKey:foreign.instrumentKey, currency:"USD", kind:"buy", quantity:"2", amount:"200", fee:"0", accrued:"0", occurredAt:{seconds:1704153600n}});
    await page.route("**/budget.v1.*/*", async route => {
      const path = new URL(route.request().url()).pathname;
      const service = [InvestmentService, InvestmentImportService, InvestmentNAVService, TenantService].find(s => path.includes(s.typeName + "/"));
      const method = service?.methods.find(m => path.endsWith("/" + m.name));
      const bytes = route.request().postDataBuffer()!;
      const input = method ? fromBinary(method.input, bytes.subarray(5, 5 + bytes.readUInt32BE(1))) as any : {};
      if (method) calls.push({method: method.name, input});
      if (method?.name === "GetInvestmentQuotes") await quotesReady;
      if (method?.name === "GetInvestmentExchangeRates") await ratesReady;
      let data: any = {};
      if (method?.name === "ListMyTenants") data = {memberships: [{tenant: {id: "tenant", name: "Synthetic"}, role: 1}]};
      if (method?.name === "ListInvestmentLedgers") data = {ledgers: [ledger]};
      if (method?.name === "GetInvestmentSnapshot") data = {snapshot: {ledger, issues:pendingHistory ? ["source_trades_pending"] : [], positions: [position, foreign], cash: [{currency: "RUB", economic: "999999"}]}};
      if (method?.name === "ListInvestmentOperations") data = {operations};
      if (method?.name === "ListInvestmentValuationProfiles") data = {fxProfiles:[{key:"market", mode:"market", version:1n},{key:"cbr", mode:"cbr", version:1n}]};
      if (method?.name === "GetTrackerHistory") {
        const points: any[] = []; let date = input.fromDate;
        const shift = (day: string) => {const d = new Date(day + "T00:00:00Z"); d.setUTCDate(d.getUTCDate() + 1); return d.toISOString().slice(0, 10);};
        while (date <= input.toDate && points.length < 31) {
          const i = points.length, partial = i >= 1 && i <= 3, missing = i === 2;
          points.push({date, state: partial ? "incomplete" : "ready", knownValue: {numerator: missing ? "0" : partial ? "75" : "100", denominator: "1"}, positions: 2, pricedPositions: missing ? 0 : partial ? 1 : 2, issues: partial ? [{componentId: "missing", code: "price_missing"}] : [], coverageKey: (missing ? "c" : partial ? "b" : "a").repeat(64)});
          date = shift(date);
        }
        data = {performanceSnapshot: "synthetic-signed-page", points, nextDate: date <= input.toDate ? date : "", currency: input.currency, fxPolicyKey: input.fxPolicyKey, fxMode: input.fxPolicyKey, fxPolicyVersion: 1n, generation: 1n, basisKey: "d".repeat(64), priceConvention: "retrospective_close_next_utc_day_v1", reportingTimezone: "Europe/Moscow"};
      }
      if (method?.name === "GetTrackerPerformance") {
        expect(input.snapshotTokens.length).toBeGreaterThan(0);
        data = {fromDate:input.fromDate,toDate:input.toDate,currency:input.currency,fxPolicyKey:input.fxPolicyKey,historyScope:"recorded_securities_strategy",twrConvention:"securities_daily_end_of_day_flows",reportingTimezone:"Europe/Moscow",profit:{numerator:"12345",denominator:"100"},netContributions:{numerator:"1000",denominator:"1"},twrReason:"discontinuous_history",xirr:0.125,xirrAnnualized:true,xirrDays:30};
        const shift=(day:string,n:number)=>{const d=new Date(day+"T00:00:00Z");d.setUTCDate(d.getUTCDate()+n);return d.toISOString().slice(0,10);};
        const start=shift(input.fromDate,-1),dates:string[]=[];for(let date=start;date<=input.toDate;date=shift(date,1))dates.push(date);
        data.twrPoints=dates.map((date,i)=>({date,state:i===2?"gap":i>2?"baseline":i===0?"baseline":"calculated",segment:i===2?0:i>2?2:1,index:i===2?undefined:{numerator:i===1?"105":"100",denominator:"1"}}));
        data.trackerBenchmarks=[{key:"imoex",title:"IMOEX",kind:"price_index",nativeCurrency:"RUB",provider:"moex",sourceUrl:"https://www.moex.com/ru/index/IMOEX"},{key:"spy",title:"SPY · S&P 500 ETF proxy",kind:"etf_price_proxy",nativeCurrency:"USD",provider:"twelvedata",sourceUrl:"https://www.ssga.com/us/en/institutional/etfs/state-street-spdr-sp-500-etf-trust-spy"}].map(b=>({...b,baseDate:start,points:dates.map((date,i)=>({date,index:{numerator:String(100+i),denominator:"1"}}))}));
      }
      if (method?.name === "GetInvestmentQuotes") data = {quotes:input.instrumentKeys.map((instrumentKey:string) => instrumentKey === foreign.instrumentKey ? {instrumentKey,currency:"USD",state:"fresh",cleanPrice:{numerator:"110",denominator:"1"},priceAt:{seconds:BigInt(Math.floor(Date.now()/1000)-60)},source:"tinvest/personal"} : {instrumentKey,state:"unavailable"})};
      if (method?.name === "GetInvestmentExchangeRates") {
        const current = input.fxPolicyKey === "market" ? 100 : 90, historical = input.fxPolicyKey === "market" ? 80 : 70;
        data = {currency:input.currency, fxPolicyKey:input.fxPolicyKey, policyVersion:1n, mode:input.fxPolicyKey, readAt:{seconds:BigInt(Math.floor(Date.now()/1000))}, rates:["", ...input.dates].flatMap((date:string) => ["RUB","USD"].map(fromCurrency => ({date,fromCurrency,rate:{numerator:String(fromCurrency === input.currency ? 1 : fromCurrency === "USD" ? date ? historical : current : 1), denominator:String(fromCurrency === input.currency || fromCurrency === "USD" ? 1 : date ? historical : current)}})))};
      }
      if (method?.name === "ListBrokerSnapshots") data = {snapshots: [{id: "snapshot", accountId: "broker", createdAt: {seconds: BigInt(Math.floor(Date.now() / 1000))}}]};
      if (method?.name === "GetBrokerSnapshot") data = {originalJson: new TextEncoder().encode(JSON.stringify({fetched_at: new Date().toISOString(), operations: [{ticker: "ABC", instrumentUid: key.slice(8), name: "Example security"}, {ticker:"US",instrumentUid:"foreign",name:"Foreign security",instrumentType:"share",classCode:"SPBXM"}], portfolio: {positions: [{instrumentUid: key.slice(8), ticker: "ABC", instrumentType: "share", currentPrice: {currency: "rub", units: "120", nano: 0}}]}}))};
      if (method?.name === "PreviewInvestmentOperation") data = {valid: true, previewHash: "reviewed"};
      if (method?.name === "SaveInvestmentOperation") {
        expect(input.previewHash).toBe("reviewed"); expect(input.operation.kind).toBe("buy"); expect(input.operation.amount).toBe("100"); expect(input.operation.tax).toBe("0"); expect(input.operation.feeCurrency).toBe("USD"); expect(input.operation.fee).toBe("1");
        ledger.version++; position.quantity = "11"; position.cleanCost = "1100"; position.lots.push({originId:"new-purchase",quantity:"1",cleanCost:"100",acquiredAccrued:"0",acquiredAt:{seconds:1704326400n}});
        operations.push({...input.operation, id: "new-purchase", status: "posted", occurredAt: create(TimestampSchema, {seconds: 1704326400n})});
        data = {operation: operations.at(-1)};
      }
      const body = method ? Buffer.from(toBinary(method.output, create(method.output, data))) : Buffer.alloc(0);
      const header = Buffer.alloc(5); header.writeUInt32BE(body.length, 1);
      const trailer = Buffer.from("grpc-status: 0\r\n"), end = Buffer.alloc(5); end[0] = 128; end.writeUInt32BE(trailer.length, 1);
      await route.fulfill({status: 200, contentType: "application/grpc-web+proto", body: Buffer.concat([header, body, end, trailer])});
    });
    await page.goto("/investments");
    const loading = page.getByRole("status").filter({hasText:ru ? "Загружаем портфель" : "Loading portfolio"});
    await expect(loading).toBeVisible();
    await expect.poll(() => calls.some(c => c.method === "GetInvestmentQuotes")).toBe(true);
    await expect.poll(() => calls.some(c => c.method === "GetInvestmentExchangeRates")).toBe(true);
    const costHeading = page.getByRole("heading", {name:ru ? "Вложено в бумаги" : "Cost of holdings", exact:true});
    await expect(costHeading).toHaveCount(0);
    await page.getByRole("button", {name:ru ? "Сделки" : "Trades", exact:true}).click();
    await expect(page.getByRole("table")).toContainText(ru ? "Покупка" : "Buy");
    await page.getByRole("button", {name:ru ? "Обзор" : "Overview", exact:true}).click();
    releaseQuotes();
    await expect(loading).toBeVisible();
    await expect(costHeading).toHaveCount(0);
    releaseRates();
    await expect(loading).toHaveCount(0);
    await expect(costHeading).toBeVisible();
    await expect(page.getByRole("heading", {name: ru ? "Мой портфель" : "My portfolio", exact: true})).toBeVisible();
    const value = page.locator("section").filter({has: page.getByRole("heading", {name: ru ? "Стоимость портфеля" : "Portfolio value", exact: true})});
    await expect(value).toContainText(/23[\s,.]?200/);
    await expect(page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Вложено в бумаги":"Cost of holdings",exact:true})})).toContainText(/17[\s,.]?000/);
    await page.getByRole("combobox",{name:ru?"Источник валютного курса":"Exchange rate source",exact:true}).selectOption("cbr");
    await expect(value).toContainText(/21[\s,.]?000/);
    await page.getByRole("combobox",{name:ru?"Источник валютного курса":"Exchange rate source",exact:true}).selectOption("market");
    await page.getByRole("combobox",{name:ru?"Валюта сумм":"Amount currency",exact:true}).selectOption("USD");
    await expect(value).toContainText(/232/);
    await expect(page.getByRole("table")).toContainText("ABC");
    await page.getByRole("combobox",{name:ru?"Валюта сумм":"Amount currency",exact:true}).selectOption("RUB");
    await expect(value).toContainText(/23[\s,.]?200/);
    await expect(page.getByRole("table")).toContainText("ABC");
    await expect(page.locator("main")).not.toContainText("999999");
    await expect(page.getByRole("button", {name: ru ? "Дивиденды и купоны" : "Dividends and coupons", exact: true})).toBeVisible();
    const history = page.getByRole("region", {name: ru ? "История портфеля" : "Portfolio history", exact: true});
    await expect(history.getByRole("img")).toBeVisible();
    await expect(history.locator("polyline[stroke-dasharray]")).toHaveCount(2);
    await expect(history.getByRole("status").filter({hasText: ru ? "Оценка неполная" : "Valuation is partial"})).toBeVisible();
    const returns = history.locator('[aria-label="' + (ru ? "Доходность за период" : "Period performance") + '"]');
    await expect(returns).toContainText(/123[,.]45/);
    await expect(returns).toContainText(/12[,.]5/);
    await expect(returns).toContainText("—");
    const pendingCSV = page.waitForEvent("download"); await history.getByRole("button", {name: "CSV", exact: true}).click();
    const file = await pendingCSV, csv = await readFile((await file.path())!, "utf8");
    expect(csv).toContain('"price_convention"'); expect(csv).toContain('"retrospective_close_next_utc_day_v1"'); expect(csv).toContain('"incomplete"');
    await history.getByRole("button",{name:ru?"Доходность":"Returns",exact:true}).click();
    const returnChart=history.locator('[aria-label="'+(ru?"График доходности":"Return chart")+'"]');
    await expect(returnChart.getByRole("img")).toBeVisible();
    await expect(returnChart.getByRole("checkbox")).toHaveCount(2);
    await expect(returnChart.locator("polyline")).toHaveCount(3);
    await returnChart.getByRole("checkbox").last().uncheck();
    await expect(returnChart.locator("polyline")).toHaveCount(2);
    await page.setViewportSize({width: 390, height: 844});
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (process.env.E2E_PORTFOLIO_HISTORY_PREVIEW && ru && !readOnly) await history.screenshot({path: process.env.E2E_PORTFOLIO_HISTORY_PREVIEW});
    await page.setViewportSize({width: 1280, height: 900});
    await page.screenshot({path: test.info().outputPath(`portfolio-tracker-${locale}-desktop.png`), fullPage: true});
    if (!readOnly) {
      await page.getByRole("button", {name: ru ? "Добавить операцию" : "Add operation", exact: true}).click();
      const dialog = page.getByRole("dialog");
      await dialog.getByRole("combobox", {name: ru ? "Бумага" : "Security", exact: true}).fill("ABC");
      await dialog.getByRole("textbox", {name: ru ? "Цена за штуку" : "Price per unit", exact: true}).fill("100");
      await dialog.getByRole("textbox", {name: ru ? "Комиссия" : "Fee", exact: true}).fill("1");
      await dialog.getByRole("combobox", {name: ru ? "Валюта комиссии" : "Fee currency", exact: true}).selectOption("USD");
      await dialog.getByRole("button", {name: ru ? "Сохранить" : "Save", exact: true}).click();
      await expect(dialog).toHaveCount(0); await expect(page.getByRole("table")).toContainText("11");
      await expect(page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Вложено в бумаги":"Cost of holdings",exact:true})})).toContainText(/17[\s,.]?100/);
      await expect(page.locator("section").filter({has:page.getByRole("heading",{name:ru?"Общая прибыль":"Total profit",exact:true})})).toContainText(/6[\s,.]?240/);
      expect(calls.filter(c => c.method === "SaveInvestmentOperation")).toHaveLength(1);
      expect(calls.filter(c => c.method === "SaveInvestmentOperation").every(c => c.input.operation.kind !== "deposit")).toBe(true);
    } else await expect(page.getByRole("button", {name: ru ? "Добавить операцию" : "Add operation", exact: true})).toHaveCount(0);
    await page.getByRole("button", {name: ru ? "Дивиденды и купоны" : "Dividends and coupons", exact: true}).click();
    await expect(page.getByRole("table")).toContainText("100"); await expect(page.getByRole("table")).not.toContainText(ru ? "Налог" : "Tax");
    pendingHistory = true;
    await page.reload();
    await expect(page.getByRole("status").filter({hasText:ru?"непроведённые сделки":"pending trades"})).toBeVisible();
    await expect(page.getByRole("heading",{name:ru?"Известная прибыль":"Known profit",exact:true})).toBeVisible();
    await expect(page.getByRole("heading",{name:ru?"Общая прибыль":"Total profit",exact:true})).toHaveCount(0);
    await page.getByRole("button", {name: ru ? "Дивиденды и купоны" : "Dividends and coupons", exact: true}).click();
    await expect(page.getByRole("table")).toContainText("100");
    await page.setViewportSize({width: 390, height: 844});
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({path: test.info().outputPath(`portfolio-tracker-${locale}-mobile.png`), fullPage: true});
    expect(errors).toEqual([]);
    if (readOnly) expect(calls.filter(c => !/^(Get|List)/.test(c.method))).toEqual([]);
  });

  test(`${locale} simple import keeps one incomplete security from blocking another and deduplicates retry`, async ({page, context}) => {
    test.skip(process.env.E2E_INVESTMENTS_READ_ONLY === "true", "Write runtime required");
    const ru = locale === "ru"; let committed = 0;
    await context.addCookies([{name: "NEXT_LOCALE", value: locale, url: test.info().project.use.baseURL as string}]);
    await page.addInitScript(() => {localStorage.setItem("budget/access", "synthetic-session"); localStorage.setItem("budget/tenant", "tenant");});
    const source = {id: "source", filename: "operations.csv", sourceSha256: "a".repeat(64), rowCount: 2, version: 1n, accounts: [{id: "source-account", name: "Broker", rowCount: 2}]};
    await page.route("**/budget.v1.*/*", async route => {
      const path = new URL(route.request().url()).pathname, service = [InvestmentService, InvestmentImportService, InvestmentNAVService, TenantService].find(s => path.includes(s.typeName + "/")), method = service?.methods.find(m => path.endsWith("/" + m.name));
      const bytes = route.request().postDataBuffer()!, input = method ? fromBinary(method.input, bytes.subarray(5, 5 + bytes.readUInt32BE(1))) as any : {};
      let data: any = {}, code = 0, message = "";
      if (method?.name === "ListMyTenants") data = {memberships: [{tenant: {id: "tenant", name: "Synthetic"}, role: 1}]};
      if (method?.name === "ListInvestmentLedgers") data = {ledgers: [{id: "account", name: "Broker", version: 1n}]};
      if (method?.name === "ListInvestmentImports") data = {imports: [{...source, accounts: []}]};
      if (method?.name === "GetInvestmentImport") data = {import: source};
      if (method?.name === "UploadInvestmentImport") {expect(input.filename).toBe("operations.csv"); expect(input.accounts[0].name).toBe("Broker"); data = {import: source};}
      if (method?.name === "ListInvestmentImportRows") data = {rows: ["BAD", "GOOD"].map((symbol, i) => ({record: i + 1, accountId: "source-account", sourceFields: {Event: "BUY", Symbol: symbol, Exchange: "MCX", Currency: "RUB"}, operationId: committed && i === 1 ? "posted" : ""}))};
      if (method?.name === "PreviewInvestmentFinancialMapping") {
        const mapping = JSON.parse(new TextDecoder().decode(input.mappingJson));
        expect(mapping.ledger_by_record).toEqual(committed ? {1: "account"} : {1: "account", 2: "account"});
        expect(Object.keys(mapping.duplicate_review)).toHaveLength(committed ? 1 : 2);
        expect(mapping.rules.every((rule: {MoneyRounding: string}) => rule.MoneyRounding === "12dp_half_away")).toBe(true);
        const entries = ["BAD", "GOOD"].flatMap((symbol, i) => committed && i === 1 ? [] : [{record: i + 1, expectedRowVersion: 1n, operation: {ledgerId: "account", instrumentKey: `${symbol}@MCX`, currency: "RUB", quantity: "1", amount: "10", kind: "buy"}}]);
        data = {ready: entries.length, total: 2, batch: {importId: source.id, expectedImportVersion: 1n, expectedLedgerVersions: {account: 1n}, entries}, rows: entries.map(e => ({record: e.record, operation: e.operation}))};
      }
      if (method?.name === "PreviewInvestmentImportBatch") {if (input.batch.entries[0].record === 1) {code = 9; message = "investment history is inconsistent: short sale is not supported";} else data = {result: {hash: "reviewed"}};}
      if (method?.name === "CommitInvestmentImportBatch") {expect(input.batch.entries[0].record).toBe(2); committed++; data = {result: {committed: true, operations: [{id: "posted"}], snapshots: [{ledger: {id: "account", version: 2n}}]}};}
      const body = method ? Buffer.from(toBinary(method.output, create(method.output, data))) : Buffer.alloc(0), header = Buffer.alloc(5); header.writeUInt32BE(body.length, 1);
      const trailer = Buffer.from(`grpc-status: ${code}\r\ngrpc-message: ${message}\r\n`), end = Buffer.alloc(5); end[0] = 128; end.writeUInt32BE(trailer.length, 1);
      await route.fulfill({status: 200, contentType: "application/grpc-web+proto", body: Buffer.concat([header, body, end, trailer])});
    });
    await page.goto("/investments/import");
    if (ru) {
      await page.getByRole("button", {name: /operations.csv/}).click();
      await expect(page.getByRole("combobox", {name: /Broker/})).toHaveValue("account");
      await page.getByRole("button", {name: "Предпросмотр", exact: true}).click();
    } else {
      await page.getByLabel("Snowball export").setInputFiles({name:"operations.csv",mimeType:"text/csv",buffer:Buffer.from("synthetic CSV")});
      await page.getByRole("combobox",{name:"Account for operations",exact:true}).selectOption("account");
      await page.getByRole("button",{name:"Preview",exact:true}).click();
    }
    await page.getByRole("button", {name: ru ? "Импортировать (2)" : "Import 2 operations", exact: true}).click();
    await expect(page.getByRole("status")).toContainText(ru ? "Добавлено операций: 1" : "1 operation imported");
    await page.getByText(ru ? "Строк для уточнения: 1" : "Rows needing clarification: 1", {exact: true}).click();
    await expect(page.locator("details")).toContainText("BAD@MCX");
    await expect(page.locator("details")).toContainText(ru ? "Не хватает истории покупок" : "Purchase history is incomplete");
    await page.getByRole("button", {name: ru ? "Предпросмотр" : "Preview", exact: true}).click();
    await expect(page.locator("main")).toContainText(ru ? "Уже добавлено: 1" : "Already imported: 1");
    expect(committed).toBe(1);
    await page.screenshot({path: test.info().outputPath(`portfolio-import-${locale}.png`), fullPage: true});
  });
}
