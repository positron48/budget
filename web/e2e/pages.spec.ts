import { test, expect } from "@playwright/test";
import { create, toBinary, fromBinary } from "@bufbuild/protobuf";
import { readdirSync } from "node:fs";
import { RecurringPaymentService } from "../proto/budget/v1/recurring_payment_pb";
import { AuthService } from "../proto/budget/v1/auth_pb";
import { UserService } from "../proto/budget/v1/user_pb";
import { TenantService } from "../proto/budget/v1/tenant_pb";
import { CategoryService } from "../proto/budget/v1/category_pb";
import { TransactionService } from "../proto/budget/v1/transaction_pb";
import { ReportService } from "../proto/budget/v1/report_pb";
import { FxService } from "../proto/budget/v1/fx_pb";
import { InvestmentNAVService } from "../proto/budget/v1/investment_nav_pb";
import { MarketDataService } from "../proto/budget/v1/market_data_pb";
import { InvestmentService, InvestmentOperationSourceSchema } from "../proto/budget/v1/investment_pb";
import { InvestmentImportService } from "../proto/budget/v1/investment_import_pb";
import { AssetService } from "../proto/budget/v1/asset_pb";
import { CurrencyExchangeService } from "../proto/budget/v1/currency_exchange_pb";

const services = [RecurringPaymentService, InvestmentNAVService, MarketDataService, AuthService, UserService, TenantService, CategoryService, TransactionService, ReportService, FxService, AssetService, InvestmentImportService, InvestmentService, CurrencyExchangeService];
const routes = readdirSync("app", { recursive: true }).filter((path) => typeof path === "string" && /(^|\/)page\.tsx$/.test(path)).map((path) => "/" + String(path).replace(/(^|\/)page\.tsx$/, "")).filter(path=>!path.startsWith("/investments")).sort();
const account = { id: "test-account", name: "Smoke account", kind: "bank", version: 1n, balances: [{ amount: { currencyCode: "RUB", minorUnits: 112931988n } }] };

for (const locale of ["en", "ru"]) {
  for (const route of routes) {
    test(`${locale} ${route} renders without JavaScript errors`, async ({ page, context }) => {
      await page.route("https://accounts.google.com/gsi/client", route => route.fulfill({contentType:"application/javascript",body:""}));
      const errors: string[] = [];
      let importAccountID = "proposal-a";
      let journalCash = "1000";
      let journalSaves = 0;
      let batchFixture:any, batchAttempts=0, batchSaveAttempts=0, batchPosts=0, savedBatch:any, cancellationPosts=0;
      let transferFixture:any;
      let marketJob:any;
      let marketAttempts=0;
      let marketQuota=false;
      let marketHasSources=true;
      let navEmpty=false, navRefreshes=0;
      let portfolioFixture:any; let portfolioSaves=0; let portfolioRequestKey="";
      let mappingFixture:any;
      let mappingSaves=0;
      let mappingRequestKey="";
      let mappingConflict=false;
      const marketID="11111111-1111-4111-8111-111111111111";
      const marketInstrument={instrumentId:marketID,ticker:"SBER",classCode:"TQBR",name:"Sber fixture",isin:"RU0009029540",kind:"share",revision:1n};
      let brokerLinks=0;
      let brokerPosts=0;let brokerPostKey="";
      const importRow = () => ({record:1,sourceRowId:"source:1",accountId:importAccountID,version:importAccountID === "proposal-a" ? 1n : 2n,sourceFields:{Date:"2026-01-01 12:00:00",Event:"BUY",Symbol:"SBER",Currency:"RUB"},decimals:{Quantity:"10",Price:"250.123456789"},issues:["instrument_mapping_required"]});
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
        let grpcStatus=0;
        if(method.name==="PostBrokerTrade") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.brokerOperationId).toBe("new-broker-op");expect(input.expectedLedgerVersion).toBe(9007199254740993n);expect(input.reconciliationConfirmed).toBe(true);expect(input.reason).toBe("Reviewed new trade");
          if(!brokerPostKey)brokerPostKey=input.requestKey;expect(input.requestKey).toBe(brokerPostKey);
          brokerPosts++;if(brokerPosts===1)grpcStatus=14;
          data={operationId:"posted-broker-operation",linkId:"posted-broker-link"};
        }
        if(method.name==="LinkBrokerTrade") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.operationId).toBe("exact-journal-operation");
          expect(input.expectedOperationVersion).toBe(7n);
          expect(input.reason).toBe("Broker statement checked");
          expect(input.brokerOperationId).toBe("broker-op");
          brokerLinks++;
          if(brokerLinks===1)grpcStatus=14;
          data={linkId:"saved-broker-link"};
        }
        if(method.name==="PreviewBrokerTrade") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.snapshotId).toBe("11111111-1111-4111-8111-111111111111");
          if(input.ledgerId==="ledger-2")grpcStatus=9;
          data={operation:{kind:"buy",instrumentKey:"tinvest:fixture",quantity:"9007199254740993",amount:"123.45",fee:"0.3",accrued:"0",currency:"RUB"},sourceIdentities:["tinvest:broker-account:broker-op"],journalMatches:[{operationId:"exact-journal-operation",ledgerId:"ledger-1",instrumentKey:"tinvest:fixture",sameInstrument:true,sameLedger:true,exactTime:true,operationVersion:7n},{operationId:"possible-snowball-duplicate",ledgerId:"ledger-2",instrumentKey:"old-ticker",snowballSource:true}],postingAllowed:false,replayLedgerVersion:4n,replayValid:false,replayIssue:"fixture history check failed"};
          if(input.operationId==="new-broker-op")data={...data,journalMatches:[],postingAllowed:true,replayValid:true,replayIssue:"",replayLedgerVersion:9007199254740993n};
        }
        if(method.name==="ListBrokerSnapshotRows")data={rows:[{ordinal:1,observationId:"observation",operationId:"broker-op",originalJson:Buffer.from('{"quantityDone":"9007199254740993"}'),hasOtherVersions:true,...(brokerLinks>1?{linkId:"saved-broker-link",linkedLedgerId:"ledger-1",linkedOperationId:"exact-journal-operation",linkedStatus:"voided",linkedVersion:7n,currentVersion:8n}:{} )},{ordinal:2,observationId:"new-observation",operationId:"new-broker-op",originalJson:Buffer.from("{}")}]};
        if(method.name==="ListBrokerSnapshots")data={snapshots:[{id:"11111111-1111-4111-8111-111111111111",accountId:"broker-account",rowCount:12}]};
        if(method.name==="UploadBrokerSnapshot"){
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(Buffer.from(input.originalJson).toString("utf8")).toBe('{"fixture":"брокер","amount":"9007199254740993.123"}');
          data={id:"11111111-1111-4111-8111-111111111111"};
        }
        if(method.name==="GetBrokerSnapshot"){
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.id).toBe("11111111-1111-4111-8111-111111111111");
          data={originalJson:Buffer.from('{"fixture":"брокер","amount":"9007199254740993.123"}')};
        }
        if(method.name==="ListInvestmentPortfolios")data={portfolios:portfolioFixture?[portfolioFixture]:[]};
        if(method.name==="ListInvestmentValuationProfiles")data={priceProfiles:[{key:"reviewed",title:"Reviewed rules",version:1n}],fxProfiles:[{key:"market",title:"Market FX",mode:"market",version:1n}]};
        if(method.name==="SaveInvestmentPortfolio"){
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          portfolioSaves++;expect(input.name).toBe(portfolioSaves<=2?"My portfolio":"Renamed portfolio");expect(input.ledgerIds).toEqual(["ledger-1"]);expect(input.conventionKey).toBe("reviewed");expect(input.fxPolicyKey).toBe("market");expect(input.autoExtend).toBe(true);
          if(portfolioSaves===1){portfolioRequestKey=input.requestKey;grpcStatus=14;}else{if(portfolioSaves===2)expect(input.requestKey).toBe(portfolioRequestKey);else{expect(input.requestKey).not.toBe(portfolioRequestKey);expect(input.id).toBe("portfolio-1");expect(input.expectedVersion).toBe(BigInt(portfolioSaves-2));}if(portfolioSaves===4)expect(input.archived).toBe(true);if(portfolioSaves===5)expect(input.archived).toBe(false);portfolioFixture={...input,id:"portfolio-1",seriesKey:"nav-series",version:BigInt(portfolioSaves-1)};data={portfolio:portfolioFixture};}
        }
        if(method.name==="GetInvestmentNAVSeries")data={series:{key:"nav-series",ledgerIds:["ledger-1"],currency:"RUB",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:"2024-01-06"}};
        if (method.name === "ListInvestmentNAVSeries") data={series:navEmpty?[]:[{key:"nav-series",ledgerIds:["ledger-1"],currency:"RUB",fxPolicyKey:"market",fromDate:"2024-01-01",toDate:portfolioFixture?.autoExtend?"2024-01-06":"2024-01-05"}]};
        if(method.name==="ListInvestmentBenchmarks")data={benchmarks:[{key:"test-index",title:"Fixture index",kind:"price_index",nativeCurrency:"RUB",version:1n}]};
        if(method.name==="GetInvestmentBenchmarkComparison") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.seriesKey).toBe("nav-series");expect(input.benchmarkKey).toBe("test-index");
          const amount=(value:string)=>({numerator:value,denominator:"1",decimal:value+".000000000000"});
          data={benchmark:{key:"test-index",title:"Fixture index",kind:"price_index",nativeCurrency:"RUB"},currency:"RUB",fxPolicyKey:"market",points:[
            {date:"2024-01-01",state:"ready",portfolioValue:amount("1000"),value:amount("1000"),index:amount("100"),portfolioIndex:amount("100")},
            {date:"2024-01-02",state:"gap",reason:"benchmark_price_unavailable",portfolioValue:amount("1100"),value:amount("999999")},
            {date:"2024-01-03",state:"ready",portfolioValue:amount("1200"),value:amount("1500"),index:amount("150"),portfolioIndex:amount("120")},
            {date:"2024-01-04",state:"pending",reason:"benchmark_withdrawal_pending",portfolioValue:amount("1250"),value:amount("888888"),index:amount("160"),portfolioIndex:amount("125")},
            {date:"2024-01-05",state:"ready",portfolioValue:amount("1300"),value:amount("1700"),index:amount("170"),portfolioIndex:amount("130")},
          ].filter(p=>p.date>=input.fromDate&&p.date<=input.toDate)};
        }
        if(method.name==="GetInvestmentPerformance") {
          const amount=(value:string)=>({numerator:value,denominator:"1",decimal:value+".000000000000"});
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.seriesKey).toBe("nav-series");
          data=input.fromDate==="2024-01-03"?{fromDate:input.fromDate,toDate:input.toDate,currency:"RUB",profit:{numerator:"20",denominator:"1",decimal:"20.000000000000"},netContributions:{numerator:"500",denominator:"1",decimal:"500.000000000000"},twr:{numerator:"1",denominator:"50",decimal:"0.020000000000"},xirr:0,xirrAnnualized:true}:{reason:"initial_nav_unknown",twrReason:"discontinuous_history",xirrReason:"initial_nav_unknown",xirrAnnualized:true};
          data.twrPoints=[
           {date:"2023-12-31",state:"baseline",segment:1,index:amount("100")},
           {date:"2024-01-01",state:"calculated",segment:1,index:amount("102"),dailyReturn:{numerator:"1",denominator:"50"},segmentReturn:{numerator:"1",denominator:"50"}},
           {date:"2024-01-02",state:"gap",reason:"nav_incomplete",segmentReturn:{numerator:"999",denominator:"1"}},
           {date:"2024-01-03",state:"baseline",segment:2,index:amount("100")},
           {date:"2024-01-04",state:"calculated",segment:2,index:amount("90"),dailyReturn:{numerator:"-1",denominator:"10"},segmentReturn:{numerator:"-1",denominator:"10"}},
           {date:"2024-01-05",state:"calculated",segment:2,index:amount("100"),dailyReturn:{numerator:"1",denominator:"9"},segmentReturn:{numerator:"0",denominator:"1"}},
          ].filter(p=>p.date>=new Date(Date.parse(input.fromDate+"T00:00:00Z")-86400000).toISOString().slice(0,10)&&p.date<=input.toDate);
        }
        if (method.name === "GetInvestmentNAVHistory") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.seriesKey).toBe("nav-series");
          data={reportingTimezone:"Europe/Moscow",points:[
            {date:"2024-01-01",state:"ready",knownValue:{numerator:"100",denominator:"1",decimal:"100.000000000000"}},
            {date:"2024-01-02",state:"incomplete",knownValue:{numerator:"50",denominator:"1",decimal:"50.000000000000"},issues:[{componentId:"bond",code:"accrued_missing"}]},
            {date:"2024-01-03",state:"ready",knownValue:{numerator:"120",denominator:"1",decimal:"120.000000000000"}},
            {date:"2024-01-04",state:"unavailable"},
            {date:"2024-01-05",state:"stale",knownValue:{numerator:"999999",denominator:"1",decimal:"999999.000000000000"}},
            {date:"2024-01-06",state:"ready",knownValue:{numerator:"130",denominator:"1",decimal:"130.000000000000"}},
          ].filter(p=>p.date>=input.fromDate&&p.date<=input.toDate)};
        }
        if (method.name === "RequestInvestmentNAVRefresh") {navRefreshes++;data={state:"requested"};if(navRefreshes>1)grpcStatus=8;}
        if(method.name === "GetInvestmentMarketMapping") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          if(!mappingFixture) grpcStatus=5;
          data={mapping:mappingFixture};
        }
        if(method.name === "SaveInvestmentMarketMapping") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          mappingSaves++;
          if(mappingConflict) grpcStatus=10;
          expect(input.instrumentKey).toBe("SBER@MOEX");
          expect(input.reason).toBeTruthy();
          if(mappingSaves<=2) {
            expect(input.expectedVersion).toBe(0n);
            expect(input.periods[0].instrumentId).toBe(marketID);
            expect(input.periods[0].from.nanos).toBe(123456000);
            expect(input.periods[0].currency).toBe("RUB");
            if(mappingSaves===1) {mappingRequestKey=input.requestKey;grpcStatus=14;}
            else expect(input.requestKey).toBe(mappingRequestKey);
          } else {expect(input.expectedVersion).toBe(1n);if(!mappingConflict) expect(input.periods).toHaveLength(0);}
          if(!grpcStatus) mappingFixture={instrumentKey:input.instrumentKey,version:input.expectedVersion+1n,periods:input.periods,reason:input.reason};
          data={mapping:mappingFixture};
        }
        if (method.name === "ListMarketSources") data={sources:marketHasSources?[{source:{provider:"tinvest",accessScope:"personal"},title:"T-Invest fixture"},{source:{provider:"tinvest",accessScope:"other"},title:"Other source"}]:[]};
        if(method.name === "SearchMarketInstruments") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          data={catalogState:"cached_only",instruments:input.source.accessScope==="personal"?[marketInstrument,{...marketInstrument,instrumentId:"22222222-2222-4222-8222-222222222222",classCode:"SPEQ"}]:[]};
        }
        if(method.name === "GetMarketHistory") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.instrumentId).toBe(marketID);
          expect(input.to.seconds).toBeGreaterThan(BigInt(Math.floor(Date.now()/1000)));
          data={coverageState:"observations_only",observations:[{id:"quote-1",instrumentId:marketID,kind:input.kind,price:"9007199254740993.000000001",currency:"RUB",priceUnit:"provider_unspecified",adjustment:"provider_unspecified",complete:false,revision:2n,sourceKind:"exchange",observedAt:{seconds:BigInt(Math.floor(Date.now()/1000)),nanos:0}}]};
        }
        if(method.name === "RequestMarketRefresh") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          marketAttempts++;
          if(input.kind==="catalog_search") {expect(input.searchQuery).toBe("SBER");expect(input.instrumentKind).toBe("share");expect(input.instrumentId).toBe("");}
          else {expect(input.kind).toBe("daily_candle");expect(input.instrumentId).toBe(marketID);expect(input.from).toBeDefined();}
          if(marketQuota) grpcStatus=8;
          marketJob={id:`market-job-${marketAttempts}`,source:input.source,kind:input.kind,instrumentId:input.instrumentId,searchQuery:input.searchQuery,status:"pending",attempts:0};
          data={job:marketJob};
        }
        if(method.name === "GetMarketDataJob") data={job:{...marketJob,status:"ready",coverageState:marketJob.kind==="catalog_search"?"catalog_truncated":"observed",instrumentCount:2,observationCount:1}};
        if (method.name === "ListInvestmentLedgers") data = {ledgers:[{id:"ledger-1",name:"Journal fixture",version:4n,startedAt:{seconds:1704067200n}},{id:"ledger-2",name:"Destination fixture",version:7n,startedAt:{seconds:1704067200n}}]};
        if (method.name === "GetInvestmentSnapshot") data = {snapshot:{ledger:{id:"ledger-1",name:"Journal fixture",version:4n,startedAt:{seconds:1704067200n}},cash:[{currency:"USD",settled:journalCash,receivable:"0",payable:"0",economic:journalCash}],positions:[{instrumentKey:"TEST@NYSE",currency:"USD",quantity:"2",cleanCost:"202",acquiredAccrued:"0",realizedClean:"0",realizedAccrued:"0"}],valuationStatus:"market_data_required"}};
        if (method.name === "GetInvestmentSnapshot") {
          const wire=request.request().postDataBuffer()!;
          const input:any=fromBinary(method.input,wire.subarray(5,5+wire.readUInt32BE(1)));
          data.snapshot.positions.push({instrumentKey:"UNKNOWN",currency:"USD",quantity:"1",cleanCost:"",acquiredAccrued:"",realizedClean:"",realizedAccrued:""});
          data.snapshot.issues=["history_before_opening_unknown","unknown_acquisition_cost"];
          if(input.ledgerId==="ledger-2") data={snapshot:{ledger:{id:"ledger-2",name:"Destination fixture",version:7n,startedAt:{seconds:1704067200n}},cash:[],positions:[],issues:[]}};
        }
        if (method.name === "ListInvestmentOperations") data = {operations:transferFixture?[{id:"transfer-out",ledgerId:"ledger-1",transferId:transferFixture.id,counterpartyLedgerId:"ledger-2",kind:"transfer_out",currency:"USD",amount:transferFixture.amount,quantity:"0",status:transferFixture.status,version:transferFixture.version,occurredAt:{seconds:1704070800n}}]:[]};
        if (method.name === "PreviewInvestmentTransfer" || method.name === "PreviewInvestmentOperation") {
          const wire=request.request().postDataBuffer()!;
          const input:any=fromBinary(method.input,wire.subarray(5,5+wire.readUInt32BE(1)));
          const preview={valid:true,previewHash:"a".repeat(64),checkedAt:{seconds:1704240000n},dirtyFrom:{seconds:1704153600n},comparisons:[]};
          // JSON response fixtures must exclude protobuf runtime type markers recursively.
          const plain=JSON.parse(JSON.stringify(input, (key,value)=>key==="$typeName"?undefined:typeof value==="bigint"?value.toString():value));
          data=method.name === "PreviewInvestmentTransfer"?{transfer:plain.transfer,preview}:{...preview,proposed:plain.operation};
        }
        if(method.name === "GetInvestmentTransfer") data={transfer:transferFixture};
        if(method.name === "MutateInvestmentTransfer") {
          const wire=request.request().postDataBuffer()!;
          const input:any=fromBinary(method.input,wire.subarray(5,5+wire.readUInt32BE(1)));
          expect(input.expectedFromVersion).toBe(4n);expect(input.expectedToVersion).toBe(7n);
          expect(input.expectedVersion).toBe(transferFixture?.version||0n);
          expect(input.transfer.fromLedgerId).toBe("ledger-1");expect(input.transfer.toLedgerId).toBe("ledger-2");
          if(input.action==="create"){expect(input.transfer.amount).toBe("300");expect(input.transfer.currency).toBe("USD");}
          if(input.action==="amend"){expect(input.transfer.amount).toBe("250");expect(input.reason).toBe("correction");}
          if(input.action==="void") expect(input.reason).toBe("cancel");
          const {$typeName,...plainTransfer}=input.transfer;
          transferFixture={...plainTransfer,id:"transfer-1",status:input.action==="void"?"voided":"posted",version:(transferFixture?.version||0n)+1n,occurredAt:{seconds:1704070800n}};
          data={transfer:transferFixture};
        }
        if (method.name === "SaveInvestmentOperation") {
          const wire=request.request().postDataBuffer()!;
          const input:any=fromBinary(method.input,wire.subarray(5,5+wire.readUInt32BE(1)));
          expect(input.expectedLedgerVersion).toBe(4n);
          if (input.operation.kind === "amortization" || input.operation.kind === "redemption") {
            expect(input.operation.amount).toBe(input.operation.kind === "amortization" ? "200" : "800");
            expect(input.operation.nominalBefore).toBe(input.operation.kind === "amortization" ? "1000" : "800");
            expect(input.operation.nominalAfter).toBe(input.operation.kind === "amortization" ? "800" : "0");
            expect(input.operation.quantity).toBe(input.operation.kind === "amortization" ? "0" : "1");
            expect(input.source).toBeUndefined();
          } else if (input.operation.kind === "opening_position") {
            expect(input.operation.occurredAt.seconds).toBe(1704067200n);
            expect(input.operation.quantity).toBe("1");
            expect(input.operation.amount).toBe(input.operation.openingCostKnown ? "80" : "0");
            expect(input.operation.openingAccruedKnown).toBe(input.operation.openingCostKnown);
            expect(input.operation.accrued).toBe("0");expect(input.operation.note).toBe("Opening evidence");
          } else if (input.operation.kind === "fx") {
            expect(input.operation.amount).toBe("100");
            expect(input.operation.currency).toBe("USD");
            expect(input.operation.toCurrency).toBe("RUB");
            expect(input.operation.toAmount).toBe("9000");
            expect(input.operation.fee).toBe("1");
            expect(input.source).toBeUndefined();
          } else if (input.operation.kind === "distribution") {
            expect(input.operation.amount).toBe("0");
            expect(input.operation.instrumentKey).toBe("OLD");
            expect(input.operation.distribution.eventKey).toBe("reviewed-event");
            expect(input.operation.distribution.targetInstrumentKey).toBe("NEW");
            expect(input.operation.distribution.newUnits).toBe("7.0135");
            expect(input.operation.distribution.oldUnits).toBe("1");
            expect(input.operation.distribution.costNumerator).toBe("1");
            expect(input.operation.distribution.costDenominator).toBe("1");
            expect(input.operation.distribution.consumeSource).toBe(true);
          } else if (input.operation.kind === "coupon") {
            expect(input.operation.taxReviewRequired).toBe(true);
            expect(input.operation.couponAllocationConfirmed).toBe(true);
            expect(input.operation.accruedRecoveries).toHaveLength(1);
            expect(input.operation.accruedRecoveries[0].originId).toBe("coupon-lot");
            expect(input.operation.accruedRecoveries[0].allRemaining).toBe(true);
            expect(input.operation.accruedRecoveries[0].amount).toBe("0");
            expect(input.operation.amount).toBe("30");
          } else {
          expect(input.operation.amount).toBe("25.000000000001");
          expect(input.operation.kind).toBe("deposit");
          expect(input.operation.currency).toBe("USD");
          expect(input.operation.quantity).toBe("0");
          journalSaves++;
          if (journalSaves === 1) expect(input.source).toBeUndefined();
          else {
            expect(input.source.importId).toBe("import-1");
            expect(input.source.record).toBe(1);
            expect(input.source.expectedVersion).toBe(1n);
            expect(input.source.reviewReason).toBe("Confirmed amount, timezone and cash against statement");
          }
          journalCash=journalSaves === 1 ? "1025.000000000001" : "1050.000000000002";
          }
          data={operation:{...input.operation,id:"saved-op",status:"posted",version:1n}};
        }
        if (method.name === "ListMyTenants") data = { memberships: [{tenant:{id:"test-tenant",name:"Smoke tenant"},role:1}] };
        if (method.name === "ListInvestmentImports") data = {imports:[{id:"import-1",filename:"history.csv",sourceSha256:"c".repeat(64),previewHash:"b".repeat(64),rowCount:3,version:1n,state:"requires_mapping"}]};
        if (method.name === "GetInvestmentImport") data = {import:{id:"import-1",filename:"history.csv",sourceSha256:"c".repeat(64),previewHash:"b".repeat(64),rowCount:3,version:1n,state:"requires_mapping",accounts:[{id:"proposal-a",name:"Broker ordinary",rowCount:1},{id:"proposal-b",name:"Unassigned",rowCount:0}]}};
        if (method.name === "ListInvestmentImportRows") {
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          const record=(input.offset||0)+1;
          data={rows:[route === "/investments/journal" ? {...importRow(),record,operationId:record===1&&journalSaves>=2?"saved-op":"",operationStatus:record===1&&journalSaves>=2?"posted":"",sourceFields:{Event:"CASH_IN",Date:"2026-01-01 12:00:00",Price:"25.000000000001",Currency:"USD"}}:importRow()]};
        }
        if(method.name==="PreviewInvestmentFinancialMapping"){
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          const mapping=JSON.parse(new TextDecoder().decode(input.mappingJson));
          expect(mapping.timezone).toBe("UTC");expect(mapping.settlement_policy).toBe("assume_trade_time");
          expect(mapping.ledger_by_record).toEqual({"1":"ledger-1","2":"ledger-1","3":"ledger-1"});
          expect(mapping.trade_evidence["2"].payment).toBe("-2501.23");
          expect(mapping.income_evidence["3"].credit.payment).toBe("100.123456789");
          expect(mapping.income_evidence["3"].tax.payment).toBe("-13");
          expect(mapping.cash_evidence["1"].operation.payment).toBe("100.123456789");
          expect(mapping.rules[0]).toMatchObject({Kind:"buy",AmountBasis:"quantity_price",AmountFactor:"1",QuantityFactor:"1",AccruedBasis:"zero_only",FeePolicy:"same_currency",Reason:"Confirmed broker quantities and fees"});
          const operations=[1,2].map(record=>({record,expectedRowVersion:2n,reviewReason:mapping.rules[0].Reason,operation:{ledgerId:"ledger-1",kind:"buy",instrumentKey:"isin:SBER",currency:"RUB",quantity:"10",amount:record===2&&mapping.trade_evidence?.["2"]?"2501.23":"2501.23456789",accrued:"0",fee:"0",tax:"0",occurredAt:{seconds:1767278400n},settlementAt:{seconds:1767278400n},settlementAssumed:true}}));
          data={hash:"d".repeat(64),ready:2,unresolved:1,total:3,rows:[...operations.map(entry=>({record:entry.record,sourceRowId:`source:${entry.record}`,reviewReason:entry.reviewReason,operation:entry.operation})),{record:3,sourceRowId:"source:3",issues:["source_row_already_consumed"]}],batch:{importId:input.importId,previewHash:"b".repeat(64),expectedImportVersion:1n,requestKey:input.requestKey,expectedLedgerVersions:{"ledger-1":4n},entries:operations,preparationEvidenceJson:JSON.stringify({Version:1,ReviewHash:"d".repeat(64),Mapping:mapping})}};
        }
        if(method.name==="ListInvestmentImportDrafts")data={drafts:savedBatch?[{id:savedBatch.id,importId:savedBatch.batch.importId,version:savedBatch.version,rowCount:savedBatch.batch.entries.length,committed:!!savedBatch.preview?.committed,cancelled:!!savedBatch.cancellation?.cancelled,updatedAt:savedBatch.updatedAt}]:[]};
        if(method.name==="GetInvestmentImportDraft"){
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.id).toBe(savedBatch.id);data={draft:savedBatch};
        }
        if(method.name==="SaveInvestmentImportDraft"){
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          batchSaveAttempts++;
          if(!savedBatch){
            expect(input.expectedVersion).toBe(0n);
            savedBatch={id:input.id,version:1n,batch:input.batch,updatedAt:{seconds:1767278400n}};
          }else{
            expect(input.id).toBe(savedBatch.id);
            expect(input.batch.requestKey).toBe(savedBatch.batch.requestKey);
            if(input.expectedVersion===savedBatch.version-1n)expect(input.batch).toEqual(savedBatch.batch);
            else{
              expect(input.expectedVersion).toBe(savedBatch.version);
              savedBatch={...savedBatch,version:savedBatch.version+1n,batch:input.batch,preview:undefined};
            }
          }
          data={draft:savedBatch};
          if(batchSaveAttempts===1)grpcStatus=14; // saved, acknowledgement lost
        }
        if(method.name==="PreviewInvestmentBatchCancellation"||method.name==="CommitInvestmentBatchCancellation"){
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.cancellation.draftId).toBe(savedBatch.id);
          expect(input.cancellation.expectedDraftVersion).toBe(savedBatch.version);
          expect(input.cancellation.reason).toBe("Duplicate imported history");
          const result={hash:"c".repeat(64),reason:input.cancellation.reason,cancelled:false,operations:savedBatch.preview.operations.map((op:any,index:number)=>({...op,source:create(InvestmentOperationSourceSchema,{record:savedBatch.batch.entries[index].record}),status:"voided",version:2n})),alreadyVoided:[],snapshots:[{ledger:{id:"ledger-1",name:"Journal fixture",version:7n},cash:[{currency:"USD",economic:"1000.000000000003",settled:"1000.000000000003",receivable:"0",payable:"0"}],positions:[],issues:[]}]};
          if(method.name==="CommitInvestmentBatchCancellation"){
            expect(input.expectedHash).toBe(result.hash);
            if(!savedBatch.cancellation){cancellationPosts++;savedBatch.cancellation={...result,cancelled:true,cancelledAt:{seconds:1767278400n}};journalCash="1000.000000000003";}
            grpcStatus=14; // applied, acknowledgement lost
          }
          data={result};
        }
        if(method.name==="PreviewInvestmentImportDraft"||method.name==="CommitInvestmentImportDraft"){
          const input:any=fromBinary(method.input,request.request().postDataBuffer()!.subarray(5));
          expect(input.id).toBe(savedBatch.id);expect(input.expectedVersion).toBe(savedBatch.version);
          if(method.name==="PreviewInvestmentImportDraft"){
            batchFixture=savedBatch.batch;
            savedBatch.preview={hash:"a".repeat(64),committed:false,operations:savedBatch.batch.entries.map((entry:any)=>entry.operation),snapshots:[{ledger:{id:"ledger-1",name:"Journal fixture",version:6n},cash:[{currency:"USD",economic:"1100.000000000004",settled:"1100.000000000004",receivable:"0",payable:"0"}],positions:[],issues:[]}]};
          }else{
            batchAttempts++;expect(savedBatch.batch).toEqual(batchFixture);expect(input.expectedHash).toBe("a".repeat(64));
            if(batchAttempts===1)grpcStatus=10;
            else if(batchAttempts===2)grpcStatus=14; // no commit, retry is required
            else{
              if(!savedBatch.preview.committed){batchPosts++;savedBatch.preview.committed=true;journalCash="1100.000000000004";}
              if(batchAttempts===3)grpcStatus=14; // committed, response lost
            }
          }
          data={draft:savedBatch};
        }
        if (method.name === "ReassignInvestmentImportRow") {
          const wire = request.request().postDataBuffer()!;
          const input: any = fromBinary(method.input, wire.subarray(5, 5 + wire.readUInt32BE(1)));
          expect(input.expectedVersion).toBe(1n);
          expect(input.reason).toBe("Confirmed broker statement");
          expect(input.accountId).toBe("proposal-b");
          importAccountID = input.accountId;
          data = {row:importRow()};
        }
        if (method.name === "UploadInvestmentImport") {
          const wire = request.request().postDataBuffer()!;
          const input: any = fromBinary(method.input, wire.subarray(5, 5 + wire.readUInt32BE(1)));
          expect(input.filename).toBe("synthetic.csv");
          expect(Buffer.from(input.csv).toString()).toContain("Event,Date,Symbol");
          expect(input.accounts[0].sourceKey).toBe("other:unassigned");
          expect(input.requestKey).not.toBe("");
          data = {import:{id:"import-1",filename:"history.csv",sourceSha256:"c".repeat(64),previewHash:"b".repeat(64),rowCount:3,version:1n,state:"requires_mapping"}};
        }
        if (method.name === "BatchGetRates") data = { rates: [{ fromCurrencyCode: "USD", toCurrencyCode: "RUB", rateDecimal: "80.25", provider: "cbr", asOf: { seconds: 1791417600n } }] };
        if (method.name === "GetAccount") data = { account };
        if (method.name === "ListAccounts" || method.name === "GetOverview") data = { accounts: [account], total: { currencyCode: "RUB", minorUnits: 112931988n } };
        const body = Buffer.from(toBinary(method.output, create(method.output, data)));
        const header = Buffer.alloc(5); header.writeUInt32BE(body.length, 1);
        const trailer = Buffer.from(`grpc-status: ${grpcStatus}\r\n`);
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
