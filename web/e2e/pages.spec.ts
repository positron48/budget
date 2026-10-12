import {checkInvestmentChartKeyboard} from "./investment-chart-helpers";
import { test, expect } from "@playwright/test";
import { create, toBinary, fromBinary } from "@bufbuild/protobuf";
import { readdirSync } from "node:fs";
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

const services = [InvestmentNAVService, MarketDataService, AuthService, UserService, TenantService, CategoryService, TransactionService, ReportService, FxService, AssetService, InvestmentImportService, InvestmentService, CurrencyExchangeService];
const routes = readdirSync("app", { recursive: true }).filter((path) => typeof path === "string" && /(^|\/)page\.tsx$/.test(path)).map((path) => "/" + String(path).replace(/(^|\/)page\.tsx$/, "")).sort();
const account = { id: "test-account", name: "Smoke account", kind: "bank", version: 1n, balances: [{ amount: { currencyCode: "RUB", minorUnits: 112931988n } }] };

for (const locale of ["en", "ru"]) {
  for (const route of routes) {
    test(`${locale} ${route} renders without JavaScript errors`, async ({ page, context }) => {
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
          if(route==="/investments/market")expect(input.instrumentKey).toBe("SBER@MOEX");
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
          if(route==="/investments"&&input.limit===200)data={rows:Array.from({length:3},(_,i)=>({...importRow(),record:i+1,sourceRowId:`source:${i+1}`}))};
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
          if(route==="/investments"){
            expect(input.batch.entries.map((entry:any)=>entry.record)).toEqual([2]);
            expect(input.batch.entries[0].operation.amount).toBe("2501.23");
            expect(JSON.parse(input.batch.preparationEvidenceJson).Mapping.timezone).toBe("UTC");
          }else{
          expect(input.batch.entries.map((entry:any)=>entry.record)).toEqual([2,3]);
          expect(input.batch.entries[0].operation.amount).toBe("25.000000000001");
          expect(input.batch.expectedLedgerVersions["ledger-1"]).toBe(4n);
          expect(input.batch.previewHash).toBe("b".repeat(64));
          }
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
      if (route === "/investments/value") {
        await expect(page.getByTestId("nav-chart")).toBeVisible();await checkInvestmentChartKeyboard(page.getByTestId("nav-chart"));
        await expect(page.getByTestId("performance-xirr")).toContainText(locale==="ru"?"Нет полной стоимости":"complete value");
        await expect(page.getByTestId("performance-xirr")).toContainText("—");
        await expect(page.getByTestId("twr-chart")).toBeVisible();
        await expect(page.getByTestId("twr-segment")).toHaveCount(2);
        await expect(page.getByTestId("twr-chart")).toHaveAttribute("aria-label",locale==="ru"?"TWR: индекс с базой 100":"TWR index, base 100");
        await expect(page.getByTestId("twr-chart").locator("circle title").first()).toContainText("100");await checkInvestmentChartKeyboard(page.getByTestId("twr-chart"));
        await page.getByTestId("twr-chart").screenshot({path:`/private/tmp/budget-twr-index-${locale}.png`});
        await page.getByText(locale==="ru"?"Дневные расчёты и причины разрывов":"Daily calculations and gap reasons",{exact:true}).first().click();
        await expect(page.getByRole("table",{name:locale==="ru"?"Дневные расчёты и причины разрывов":"Daily calculations and gap reasons",exact:true})).toContainText(locale==="ru"?"Стоимость за этот день неполная":"value on this day is incomplete");
        await expect(page.getByTestId("investment-performance")).not.toContainText("99900");
        await page.getByLabel(locale==="ru"?"С даты":"From",{exact:true}).fill("2024-01-03");
        await expect(page.getByTestId("performance-twr")).toContainText(locale==="ru"?"2,00%":"2.00%");
        await expect(page.getByTestId("performance-xirr")).toContainText(locale==="ru"?"0,00":"0.00");
        await expect(page.getByTestId("performance-profit")).toContainText("20 RUB");
        await page.getByLabel(locale==="ru"?"С даты":"From",{exact:true}).fill("2024-01-01");

        await expect(page.getByTestId("nav-segment")).toHaveCount(2);
        await page.getByRole("combobox",{name:"Benchmark",exact:true}).selectOption("test-index");
        await expect(page.getByTestId("benchmark-chart")).toBeVisible();await checkInvestmentChartKeyboard(page.getByTestId("benchmark-chart"));
        await expect(page.getByTestId("comparison-benchmark-segment")).toHaveCount(3);
        await expect(page.getByTestId("benchmark-comparison")).not.toContainText("999999");
        await page.getByRole("button",{name:locale==="ru"?"Рост · база 100":"Growth · base 100",exact:true}).click();
        await expect(page.getByTestId("comparison-benchmark-segment")).toHaveCount(2);
        await page.getByRole("button",{name:locale==="ru"?"Стоимость":"Value",exact:true}).click();

        await expect(page.getByText("999999",{exact:false})).toHaveCount(0);
        await expect(page.getByText(locale === "ru" ? "Только известная часть" : "Known subtotal only").first()).toBeVisible();
        await page.getByRole("button",{name:locale === "ru" ? "Пересчитать" : "Recalculate",exact:true}).click();
        await expect(page.getByRole("status")).toContainText(locale === "ru" ? "Пересчёт запрошен" : "Recalculation requested");
        await page.getByRole("button",{name:locale === "ru" ? "Пересчитать" : "Recalculate",exact:true}).click();
        await expect(page.locator("main").getByRole("alert")).toContainText("30");
        await page.setViewportSize({width:375,height:812});
        expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBeTruthy();
        await page.getByText(locale==="ru"?"Дневные расчёты и причины разрывов":"Daily calculations and gap reasons",{exact:true}).first().click();
        await expect(page.getByRole("table",{name:locale==="ru"?"Дневные расчёты и причины разрывов":"Daily calculations and gap reasons",exact:true})).toBeVisible();
        expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBeTruthy();
        if(process.env.E2E_SCREENSHOT_DIR) {
          await page.getByTestId("benchmark-comparison").evaluate(el=>el.scrollIntoView({block:"center"}));
          await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));
          await page.getByTestId("benchmark-comparison").screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/benchmark-mobile-${locale}.png`});
          await page.getByTestId("investment-performance").screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/performance-mobile-${locale}.png`});
          await page.evaluate(()=>window.scrollTo(0,0));await page.waitForFunction(()=>window.scrollY===0);await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));
          await page.screenshot({fullPage:true,path:`${process.env.E2E_SCREENSHOT_DIR}/nav-mobile-${locale}.png`});
        }
        await page.getByRole("button",{name:locale==="ru"?"Создать портфель":"Create portfolio",exact:true}).click();
        await page.getByLabel(locale==="ru"?"Название портфеля":"Portfolio name",{exact:true}).fill("My portfolio");
        await page.getByLabel("Journal fixture",{exact:true}).check();
        await page.getByLabel(locale==="ru"?"Правила цен":"Price rules",{exact:true}).selectOption("reviewed");
        await page.getByRole("combobox",{name:locale==="ru"?"Курсы валют":"Exchange rates",exact:true}).selectOption("market");
        await page.getByLabel(locale==="ru"?"Начало истории":"History start",{exact:true}).fill("2024-01-01");
        await page.getByLabel(locale==="ru"?"Конец истории":"History end",{exact:true}).fill("2024-01-05");
        await page.getByLabel(locale==="ru"?"Каждый день добавлять завершённые дни по Москве":"Automatically add completed days in Moscow time",{exact:true}).check();
        expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBeTruthy();
        if(process.env.E2E_SCREENSHOT_DIR) { await page.evaluate(()=>window.scrollTo(0,0)); await page.waitForFunction(()=>window.scrollY===0); await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve())))); await page.screenshot({fullPage:true,path:`${process.env.E2E_SCREENSHOT_DIR}/portfolio-form-${locale}.png`}); }
        await page.getByRole("button",{name:locale==="ru"?"Сохранить портфель":"Save portfolio",exact:true}).click();
        await expect(page.locator("main").getByRole("alert").filter({hasText:locale==="ru"?"Не удалось":"Could not"})).toBeVisible();
        await page.getByRole("button",{name:locale==="ru"?"Сохранить портфель":"Save portfolio",exact:true}).click();
        await expect(page.getByRole("button",{name:"My portfolio · RUB",exact:true})).toBeVisible();
        await expect(page.getByLabel(locale==="ru"?"По дату":"To",{exact:true})).toHaveAttribute("max","2024-01-06");
        await expect(page.getByLabel(locale==="ru"?"По дату":"To",{exact:true})).toHaveValue("2024-01-05");
        await page.getByRole("button",{name:locale==="ru"?"Изменить":"Edit",exact:true}).click();
        await page.getByLabel(locale==="ru"?"Название портфеля":"Portfolio name",{exact:true}).fill("Renamed portfolio");
        await page.getByRole("button",{name:locale==="ru"?"Сохранить портфель":"Save portfolio",exact:true}).click();
        await expect(page.getByRole("button",{name:"Renamed portfolio · RUB",exact:true})).toBeVisible();
        await page.getByRole("button",{name:locale==="ru"?"В архив":"Archive",exact:true}).click();
        await expect(page.getByRole("button",{name:locale==="ru"?"Восстановить":"Restore",exact:true})).toBeVisible();
        await expect(page.getByRole("button",{name:locale==="ru"?"Пересчитать":"Recalculate",exact:true})).toBeDisabled();
        await page.getByRole("button",{name:locale==="ru"?"Восстановить":"Restore",exact:true}).click();
        await expect(page.getByRole("button",{name:locale==="ru"?"В архив":"Archive",exact:true})).toBeVisible();
        await expect(page.getByRole("button",{name:locale==="ru"?"Пересчитать":"Recalculate",exact:true})).toBeEnabled();
        navEmpty=true;
        await page.goto("/investments/value");
        await expect(page.getByText(locale === "ru" ? /Нет подготовленных рядов/ : /No valuation series/)).toBeVisible();
      }
      if (route === "/investments/market") {
        await page.getByRole("textbox",{name:locale==="ru"?"Название, тикер или ISIN":"Name, ticker or ISIN",exact:true}).fill("SBER");
        await page.getByRole("button",{name:locale==="ru"?"Найти у источника":"Search provider",exact:true}).click();
        await expect(page.getByText(locale==="ru"?"Список ограничен. Уточните название или идентификатор бумаги.":"The result list is limited. Refine the name or security identifier.",{exact:true})).toBeVisible();
        await expect(page.getByRole("button",{name:/SBER · TQBR/})).toBeVisible();
        await expect(page.getByRole("button",{name:/SBER · SPEQ/})).toBeVisible();
        await page.getByRole("button",{name:/SBER · TQBR/}).click();
        await expect(page.getByRole("cell",{name:"9007199254740993.000000001",exact:true})).toBeVisible();
        await expect(page.getByRole("cell",{name:locale==="ru"?"Не подтверждены":"Unconfirmed",exact:true})).toBeVisible();
        await expect(page.getByRole("cell").filter({hasText:locale==="ru"?"Свеча не завершена":"Incomplete candle"})).toBeVisible();
        await page.getByRole("button",{name:locale==="ru"?"Обновить данные":"Refresh data",exact:true}).click();
        await expect(page.getByRole("status").filter({hasText:locale==="ru"?"Готово":"Ready"})).toBeVisible();
        expect(marketAttempts).toBe(2);
        const label=(ru:string,en:string)=>locale==="ru"?ru:en;
        await page.getByLabel(label("Ключ бумаги в журнале","Journal instrument key"),{exact:true}).fill("SBER@MOEX");
        await page.getByRole("button",{name:label("Открыть привязку","Open mapping"),exact:true}).click();
        await page.getByRole("button",{name:label("Добавить SBER · TQBR","Add SBER · TQBR"),exact:true}).click();
        await page.getByLabel(label("Начало периода (UTC, ISO 8601)","Period start (UTC, ISO 8601)"),{exact:true}).fill("2020-01-01T00:00:00.123456Z");
        await page.getByLabel(label("Валюта цены","Price currency"),{exact:true}).fill("RUB");
        await page.getByLabel(label("Основание: документ или источник условий","Evidence: document or source of terms"),{exact:true}).fill("Fixture issuer terms");
        await page.getByLabel(label("Причина подтверждения или исправления","Reason for confirmation or correction"),{exact:true}).fill("Reviewed fixture");
        await page.getByRole("button",{name:label("Проверить изменения","Review changes"),exact:true}).click();
        expect(mappingSaves).toBe(0);
        await page.setViewportSize({width:375,height:812});
        expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(376);
        await page.getByRole("button",{name:label("Подтвердить и сохранить","Confirm and save"),exact:true}).click();
        await expect(page.getByText(label("Не удалось сохранить. Проверьте условия и доступность источника; повтор отправляет тот же запрос.","Could not save. Check the terms and source availability; retrying sends the same request."),{exact:true})).toBeVisible();
        await page.getByRole("button",{name:label("Подтвердить и сохранить","Confirm and save"),exact:true}).click();
        await expect(page.getByText(label("Сохранённая версия: 1","Saved version: 1"),{exact:true})).toBeVisible();
        await expect(page.getByLabel(label("Начало периода (UTC, ISO 8601)","Period start (UTC, ISO 8601)"),{exact:true})).toHaveValue("2020-01-01T00:00:00.123456Z");
        await page.evaluate(()=>window.scrollTo(0,0));
        if(process.env.E2E_SCREENSHOT_DIR) await page.screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/mapping-mobile-${locale}.png`,fullPage:true});
        mappingConflict=true;
        await page.getByLabel(label("Причина подтверждения или исправления","Reason for confirmation or correction"),{exact:true}).fill("Concurrent correction");
        await page.getByRole("button",{name:label("Проверить изменения","Review changes"),exact:true}).click();
        await page.getByRole("button",{name:label("Подтвердить и сохранить","Confirm and save"),exact:true}).click();
        await expect(page.getByText(label("Привязка уже изменена. Загрузите актуальную версию и проверьте правки заново.","This mapping has changed. Reload the latest version and review your edits again."),{exact:true})).toBeVisible();
        await page.getByRole("button",{name:label("Загрузить заново (сбросить правки)","Reload (discard edits)"),exact:true}).click();
        await expect(page.getByLabel(label("Причина подтверждения или исправления","Reason for confirmation or correction"),{exact:true})).toHaveValue("");
        mappingConflict=false;
        await page.getByRole("button",{name:label("Удалить период","Remove period"),exact:true}).click();
        await page.getByLabel(label("Причина подтверждения или исправления","Reason for confirmation or correction"),{exact:true}).fill("Revoke fixture");
        await page.getByRole("button",{name:label("Проверить изменения","Review changes"),exact:true}).click();
        await expect(page.getByText(label("Все действующие привязки этой бумаги будут сняты. История сохранится.","All active mappings for this instrument will be revoked. History will be retained."),{exact:true})).toBeVisible();
        await page.getByRole("button",{name:label("Подтвердить и сохранить","Confirm and save"),exact:true}).click();
        await expect(page.getByText(label("Сохранённая версия: 2","Saved version: 2"),{exact:true})).toBeVisible();
        expect(mappingSaves).toBe(4);
        marketQuota=true;
        await page.getByRole("button",{name:locale==="ru"?"Обновить данные":"Refresh data",exact:true}).click();
        await expect(page.locator("main").getByRole("alert")).toContainText(locale==="ru"?"Лимит обновлений":"Refresh limit");
        await page.setViewportSize({width:375,height:812});
        expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(376);
        await page.evaluate(()=>{document.documentElement.classList.add("dark");window.scrollTo(0,0);});
        if(process.env.E2E_SCREENSHOT_DIR) await page.screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/market-mobile-${locale}.png`,fullPage:true});
        await page.getByRole("combobox",{name:locale==="ru"?"Источник":"Source",exact:true}).selectOption({label:"Other source"});
        await expect(page.getByText("9007199254740993.000000001",{exact:true})).not.toBeVisible();
        await expect(page.getByRole("button",{name:/SBER · TQBR/})).not.toBeVisible();
        marketHasSources=false;
        await page.reload();
        await expect(page.getByText(locale==="ru"?"Источник котировок для этой организации ещё не подключён.":"No market data source is connected for this organization yet.",{exact:true})).toBeVisible();
      }
      if (route === "/investments/journal") {
        await page.getByRole("button",{name:"Journal fixture",exact:true}).click();
        await expect(page.getByText("TEST@NYSE",{exact:true})).toBeVisible();
        await page.getByRole("button",{name:locale === "ru" ? "Добавить операцию" : "Add operation",exact:true}).click();
        const dialog=page.getByRole("dialog");
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Валюта" : "Currency",exact:true}).selectOption("USD");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма / чистый номинал сделки" : "Amount / clean trade principal",exact:true}).fill("25.000000000001");
        await dialog.getByRole("button",{name:locale === "ru" ? "Проверить изменения" : "Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true}).click();
        await expect(dialog).not.toBeVisible();
        await expect(page.getByText("1025.000000000001 USD",{exact:true})).toBeVisible();
        await page.getByRole("button",{name:locale === "ru" ? "Добавить операцию" : "Add operation",exact:true}).click();
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Исходный файл" : "Source file",exact:true}).selectOption("import-1");
        await expect(dialog.getByText("CASH_IN",{exact:true})).toBeVisible();
        await expect(dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true})).toBeDisabled();
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Что проверено и на основании каких данных" : "What was verified and which evidence supports it",exact:true}).fill("Confirmed amount, timezone and cash against statement");
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Валюта" : "Currency",exact:true}).selectOption("USD");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма / чистый номинал сделки" : "Amount / clean trade principal",exact:true}).fill("25.000000000001");
        await dialog.getByRole("button",{name:locale === "ru" ? "Проверить изменения" : "Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true}).click();
        await expect(dialog).not.toBeVisible();
        await expect(page.getByText("1050.000000000002 USD",{exact:true})).toBeVisible();
        expect(journalSaves).toBe(2);
        await page.getByRole("button",{name:locale === "ru" ? "Добавить операцию" : "Add operation",exact:true}).click();
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Исходный файл" : "Source file",exact:true}).selectOption("import-1");
        await expect(dialog.getByText(locale === "ru" ? /Строка уже связана/ : /This row is already linked/)).toBeVisible();
        await expect(dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true})).toBeDisabled();
        if (process.env.E2E_SCREENSHOT_DIR) { await page.evaluate(() => Promise.all(document.getAnimations().map(a => a.finished.catch(() => {})))); await page.screenshot({fullPage:true,path:`${process.env.E2E_SCREENSHOT_DIR}/journal-source-${locale}.png`}); }
        await page.keyboard.press("Escape");
        await expect(dialog).not.toBeVisible();
        await page.getByRole("button",{name:locale === "ru" ? "Добавить операцию" : "Add operation",exact:true}).click();
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Тип операции" : "Operation type",exact:true}).selectOption("distribution");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Бумага: ISIN или тикер + площадка" : "Security: ISIN or ticker + venue",exact:true}).fill("OLD");
        for(const [ru,en,value] of [["Уникальный ключ события","Unique event key","reviewed-event"],["Новая бумага","Target instrument","NEW"],["Новых единиц","New units","7.0135"],["На старых единиц","Per old units","1"],["Доля стоимости: числитель","Cost share: numerator","1"],["Доля стоимости: знаменатель","Cost share: denominator","1"]]) {
          await dialog.getByRole("textbox",{name:locale === "ru" ? ru : en,exact:true}).fill(value);
        }
        await dialog.getByRole("checkbox",{name:locale === "ru" ? "Заменить исходную бумагу" : "Replace source security",exact:true}).check();
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Заметка" : "Note",exact:true}).fill("Confirmed action terms");
        await expect(dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма / чистый номинал сделки" : "Amount / clean trade principal",exact:true})).toHaveCount(0);
        await dialog.getByRole("button",{name:locale === "ru" ? "Проверить изменения" : "Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true}).click();
        await expect(dialog).not.toBeVisible();
        for (const kind of ["amortization", "redemption"]) {
          await page.getByRole("button",{name:locale === "ru" ? "Добавить операцию" : "Add operation",exact:true}).click();
          await dialog.getByRole("combobox",{name:locale === "ru" ? "Тип операции" : "Operation type",exact:true}).selectOption(kind);
          await dialog.getByRole("textbox",{name:locale === "ru" ? "Бумага: ISIN или тикер + площадка" : "Security: ISIN or ticker + venue",exact:true}).fill("BOND");
          await dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма / чистый номинал сделки" : "Amount / clean trade principal",exact:true}).fill(kind === "amortization" ? "200" : "800");
          await dialog.getByRole("textbox",{name:locale === "ru" ? "Номинал одной бумаги до события" : "Per-unit nominal before event",exact:true}).fill(kind === "amortization" ? "1000" : "800");
          if (kind === "amortization") await dialog.getByRole("textbox",{name:locale === "ru" ? "Номинал одной бумаги после события" : "Per-unit nominal after event",exact:true}).fill("800");
          else await dialog.getByRole("textbox",{name:locale === "ru" ? "Количество" : "Quantity",exact:true}).fill("1");
          await dialog.getByRole("button",{name:locale === "ru" ? "Проверить изменения" : "Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true}).click();
          await expect(dialog).not.toBeVisible();
        }
        await page.getByRole("button",{name:locale === "ru" ? "Добавить операцию" : "Add operation",exact:true}).click();
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Тип операции" : "Operation type",exact:true}).selectOption("repo_income");
        await expect(dialog.getByRole("textbox",{name:locale === "ru" ? "Бумага: ISIN или тикер + площадка" : "Security: ISIN or ticker + venue",exact:true})).toBeVisible();
        await expect(dialog.getByRole("textbox",{name:locale === "ru" ? "Количество" : "Quantity",exact:true})).toHaveCount(0);
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Тип операции" : "Operation type",exact:true}).selectOption("coupon");
        await dialog.getByRole("checkbox",{name:locale === "ru" ? "Налоговая сверка не завершена" : "Tax reconciliation is incomplete",exact:false}).check();
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Бумага: ISIN или тикер + площадка" : "Security: ISIN or ticker + venue",exact:true}).fill("BOND");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма / чистый номинал сделки" : "Amount / clean trade principal",exact:true}).fill("30");
        await dialog.getByRole("checkbox",{name:locale === "ru" ? "Распределение проверено" : "Allocation verified",exact:true}).check();
        await dialog.getByRole("button",{name:locale === "ru" ? "Добавить лот" : "Add lot",exact:true}).click();
        await dialog.getByRole("combobox",{name:locale === "ru" ? "ID операции покупки (лот)" : "Purchase operation ID (lot)",exact:true}).fill("coupon-lot");
        await dialog.getByRole("checkbox",{name:locale === "ru" ? "Весь остаток НКД лота на дату купона" : "Entire lot accrued-interest balance at coupon date",exact:true}).check();
        await dialog.getByRole("button",{name:locale === "ru" ? "Проверить изменения" : "Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true}).click();
        await expect(dialog).not.toBeVisible();
        await page.getByRole("button",{name:locale === "ru" ? "Добавить операцию" : "Add operation",exact:true}).click();
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Тип операции" : "Operation type",exact:true}).selectOption("fx");
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Валюта" : "Currency",exact:true}).selectOption("USD");
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Валюта зачисления" : "Received currency",exact:true}).selectOption("RUB");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма / чистый номинал сделки" : "Amount / clean trade principal",exact:true}).fill("100");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Фактически зачисленная сумма" : "Actual received amount",exact:true}).fill("9000");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Комиссия по всей сделке" : "Total trade fee",exact:true}).fill("1");
        await dialog.getByRole("button",{name:locale === "ru" ? "Проверить изменения" : "Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true}).click();
        await expect(dialog).not.toBeVisible();
        await expect(page.getByRole("row").filter({has:page.getByRole("cell",{name:"UNKNOWN",exact:true})}).getByRole("cell",{name:"— USD",exact:true}).first()).toBeVisible();
        for(const known of [false,true]) {
          await page.getByRole("button",{name:locale === "ru" ? "Добавить операцию" : "Add operation",exact:true}).click();
          await dialog.getByRole("combobox",{name:locale === "ru" ? "Тип операции" : "Operation type",exact:true}).selectOption("opening_position");
          await dialog.getByRole("textbox",{name:locale === "ru" ? "Бумага: ISIN или тикер + площадка" : "Security: ISIN or ticker + venue",exact:true}).fill("OPENING");
          await dialog.getByRole("textbox",{name:locale === "ru" ? "Количество" : "Quantity",exact:true}).fill("1");
          if(known) {
            await dialog.getByRole("checkbox",{name:locale === "ru" ? "Чистая себестоимость известна" : "Clean acquisition cost is known",exact:true}).check();
            await dialog.getByRole("checkbox",{name:locale === "ru" ? "Остаток покупного НКД известен" : "Remaining acquired interest is known",exact:true}).check();
            await dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма / чистый номинал сделки" : "Amount / clean trade principal",exact:true}).fill("80");
            await dialog.getByRole("textbox",{name:locale === "ru" ? "НКД по всей сделке" : "Total trade accrued interest",exact:true}).fill("0");
          }
          await dialog.getByRole("textbox",{name:locale === "ru" ? "Заметка" : "Note",exact:true}).fill("Opening evidence");
          await dialog.getByRole("button",{name:locale === "ru" ? "Проверить изменения" : "Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить" : "Save",exact:true}).click();
          await expect(dialog).not.toBeVisible();
        }
        await page.setViewportSize({width:375,height:812});
        await page.getByRole("button",{name:locale === "ru" ? "Перевод между счетами" : "Transfer between accounts",exact:true}).click();
        await dialog.getByRole("combobox",{name:locale === "ru" ? "На счёт" : "To account",exact:true}).selectOption("ledger-2");
        await dialog.getByRole("combobox",{name:locale === "ru" ? "Валюта перевода" : "Transfer currency",exact:true}).selectOption("USD");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма перевода" : "Transfer amount",exact:true}).fill("300");
        if(process.env.E2E_SCREENSHOT_DIR) { await page.evaluate(() => Promise.all(document.getAnimations().map(a => a.finished.catch(() => {})))); await page.screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/transfer-mobile-${locale}.png`}); }
        await dialog.getByRole("button",{name:locale==="ru"?"Проверить изменения":"Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить перевод" : "Save transfer",exact:true}).click();
        await expect(dialog).not.toBeVisible();
        await page.locator("tbody").getByRole("button",{name:locale === "ru" ? "Перевод между счетами" : "Transfer between accounts",exact:true}).click();
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Сумма перевода" : "Transfer amount",exact:true}).fill("250");
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Причина исправления или отмены" : "Reason for correction or cancellation",exact:true}).fill("correction");
        await dialog.getByRole("button",{name:locale==="ru"?"Проверить изменения":"Check changes",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Сохранить перевод" : "Save transfer",exact:true}).click();
        await expect(dialog).not.toBeVisible();
        await page.locator("tbody").getByRole("button",{name:locale === "ru" ? "Перевод между счетами" : "Transfer between accounts",exact:true}).click();
        await dialog.getByRole("textbox",{name:locale === "ru" ? "Причина исправления или отмены" : "Reason for correction or cancellation",exact:true}).fill("cancel");
        await dialog.getByRole("button",{name:locale==="ru"?"Проверить отмену обеих сторон":"Check voiding both sides",exact:true}).click();
        await dialog.getByRole("button",{name:locale === "ru" ? "Отменить перевод" : "Void transfer",exact:true}).click();
        await expect(dialog).not.toBeVisible();
        expect(transferFixture.status).toBe("voided");expect(transferFixture.version).toBe(3n);
        for(const record of [2,3]){
          await page.getByRole("button",{name:locale==="ru"?"Добавить операцию":"Add operation",exact:true}).click();
          await dialog.getByRole("combobox",{name:locale==="ru"?"Исходный файл":"Source file",exact:true}).selectOption("import-1");
          await dialog.getByRole("spinbutton",{name:locale==="ru"?"Строка источника":"Source row",exact:true}).fill(String(record));
          await dialog.getByRole("textbox",{name:locale==="ru"?"Что проверено и на основании каких данных":"What was verified and which evidence supports it",exact:true}).fill("Confirmed batch cash");
          await dialog.getByRole("combobox",{name:locale==="ru"?"Валюта":"Currency",exact:true}).selectOption("USD");
          await dialog.getByRole("textbox",{name:locale==="ru"?"Сумма / чистый номинал сделки":"Amount / clean trade principal",exact:true}).fill("25.000000000001");
          await dialog.getByRole("checkbox",{name:locale==="ru"?"Добавить в пакет для общей проверки вместо немедленного проведения":"Add to a batch for review instead of posting immediately",exact:true}).check();
          await dialog.getByRole("button",{name:locale==="ru"?"Добавить в пакет":"Add to batch",exact:true}).click();
          await expect(dialog).not.toBeVisible();
        }
        expect(journalSaves).toBe(2);
        const batch=page.getByTestId("investment-import-batch");
        await expect(batch.getByRole("listitem")).toHaveCount(2);
        await batch.getByRole("button",{name:locale==="ru"?"Сохранить черновик":"Save draft",exact:true}).click();
        await expect(batch.getByRole("button",{name:locale==="ru"?"Отбросить несохранённые изменения":"Discard unsaved changes",exact:true})).toBeDisabled();
        await batch.getByRole("button",{name:locale==="ru"?"Повторить сохранение":"Retry save",exact:true}).click();
        await expect(batch.getByRole("status")).toHaveText(locale==="ru"?"Сохранено на сервере":"Saved on server");
        await batch.getByRole("button",{name:locale==="ru"?"Закрыть пакет":"Close batch",exact:true}).click();
        await page.reload();
        const saved=page.getByTestId("investment-saved-batches");
        await saved.getByRole("button",{name:locale==="ru"?"Открыть":"Open",exact:true}).click();
        await expect(batch.getByRole("listitem")).toHaveCount(2);
        expect(batchSaveAttempts).toBe(2);
        await batch.getByRole("button",{name:locale==="ru"?"Проверить весь пакет":"Preview entire batch",exact:true}).click();
        await expect(batch.getByText("1100.000000000004 USD",{exact:false})).toBeVisible();
        const postBatch=batch.getByRole("button",{name:locale==="ru"?"Провести пакет":"Post batch",exact:true});
        await expect(postBatch).toBeDisabled();
        await batch.getByRole("checkbox").check();
        await postBatch.click();
        await expect(postBatch).not.toBeVisible();
        await batch.getByRole("button",{name:locale==="ru"?"Проверить весь пакет":"Preview entire batch",exact:true}).click();
        await expect(postBatch).toBeDisabled();
        await batch.getByRole("checkbox").check();
        await batch.getByText(locale==="ru"?"Все поля операции":"All operation fields",{exact:true}).first().click();
        await expect(batch.getByText(locale==="ru"?"Дата и время операции":"Operation date and time",{exact:true}).first()).toBeVisible();
        if(process.env.E2E_SCREENSHOT_DIR){
          await page.setViewportSize({width:375,height:1400});
          await batch.evaluate(el=>el.scrollIntoView({block:"center"}));
          await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));
          await batch.screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/batch-mobile-${locale}.png`});
        }
        await postBatch.click();
        await expect(batch.getByRole("button",{name:locale==="ru"?"Закрыть пакет":"Close batch",exact:true})).toBeDisabled();
        await batch.getByRole("button",{name:locale==="ru"?"Проверить результат на сервере":"Check result on server",exact:true}).click();
        await expect(batch.getByRole("button",{name:locale==="ru"?"Закрыть пакет":"Close batch",exact:true})).toBeDisabled();
        await batch.getByRole("button",{name:locale==="ru"?"Повторить и проверить результат":"Retry and check outcome",exact:true}).click();
        await expect(batch.getByRole("button",{name:locale==="ru"?"Повторить и проверить результат":"Retry and check outcome",exact:true})).toBeEnabled();
        expect(batchPosts).toBe(1);
        await page.reload();
        await saved.getByRole("button",{name:locale==="ru"?"Посмотреть результат":"View result",exact:true}).click();
        await expect(batch.getByRole("status")).toContainText(locale==="ru"?"Пакет проведён целиком":"The entire batch was posted");
        await expect(batch.getByRole("button",{name:locale==="ru"?"Провести пакет":"Post batch",exact:true})).toHaveCount(0);
        await expect(batch.getByRole("button",{name:locale==="ru"?"Убрать":"Remove",exact:true})).toHaveCount(0);
        await expect(saved.getByRole("button",{name:locale==="ru"?"Удалить черновик":"Delete draft",exact:true})).toHaveCount(0);
        expect(batchAttempts).toBe(3);
        expect(batchPosts).toBe(1);
        if(process.env.E2E_SCREENSHOT_DIR){
          await page.setViewportSize({width:375,height:1400});
          for(const [element,name] of [[saved,"saved-batches"],[batch,"posted-batch"]] as const){
            await element.evaluate(el=>el.scrollIntoView({block:"center"}));
            await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));
            await element.screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/${name}-mobile-${locale}.png`});
          }
        }

        const cancellation=page.getByTestId("investment-batch-cancellation");
        await cancellation.getByRole("textbox").fill("Duplicate imported history");
        await cancellation.getByRole("button",{name:locale==="ru"?"Проверить отмену":"Preview cancellation",exact:true}).click();
        const cancelButton=cancellation.getByRole("button",{name:locale==="ru"?"Аннулировать пакет":"Cancel batch",exact:true});
        await expect(cancelButton).toBeDisabled();
        await cancellation.getByRole("checkbox").check();
        await cancelButton.click();
        await expect(batch.getByRole("button",{name:locale==="ru"?"Закрыть пакет":"Close batch",exact:true})).toBeDisabled();
        await expect(cancellation.getByRole("textbox")).toBeDisabled();
        await cancellation.getByRole("button",{name:locale==="ru"?"Проверить результат на сервере":"Check result on server",exact:true}).click();
        await expect(cancellation.getByRole("heading",{name:locale==="ru"?"Пакет аннулирован":"Batch cancelled",exact:true})).toBeVisible();
        expect(cancellationPosts).toBe(1);
        await page.reload();
        await saved.getByRole("button",{name:locale==="ru"?"Посмотреть результат":"View result",exact:true}).click();
        await expect(cancellation.getByRole("heading",{name:locale==="ru"?"Пакет аннулирован":"Batch cancelled",exact:true})).toBeVisible();
        await expect(cancellation.getByRole("textbox")).toHaveCount(0);
        expect(cancellationPosts).toBe(1);
        if(process.env.E2E_SCREENSHOT_DIR){
          await cancellation.evaluate(el=>el.scrollIntoView({block:"center"}));
          await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));
          await cancellation.screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/cancelled-batch-mobile-${locale}.png`});
        }

        if (process.env.E2E_SCREENSHOT_DIR) { await page.waitForLoadState("networkidle"); await page.evaluate(() => window.scrollTo(0,0)); await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))); await page.screenshot({fullPage:true,path:`${process.env.E2E_SCREENSHOT_DIR}/journal-${locale}.png`}); }
      }
      if (route === "/investments") {
        await page.getByRole("button",{name:/broker-account/}).click();
        await expect(page.getByLabel(locale==="ru"?"ID брокерского снимка":"Broker snapshot ID",{exact:true})).toHaveValue("11111111-1111-4111-8111-111111111111");
        await page.getByLabel(locale==="ru"?"JSON брокера":"Broker JSON",{exact:true}).setInputFiles({name:"broker.json",mimeType:"application/json",buffer:Buffer.from('{"fixture":"брокер","amount":"9007199254740993.123"}')});
        await page.getByRole("button",{name:locale==="ru"?"Сохранить брокерскую выгрузку":"Save broker snapshot",exact:true}).click();
        await expect(page.getByLabel(locale==="ru"?"ID брокерского снимка":"Broker snapshot ID",{exact:true})).toHaveValue("11111111-1111-4111-8111-111111111111");
        await expect(page.getByText(locale==="ru"?"Есть другая версия — требуется сверка":"Another version exists — review required",{exact:true})).toBeVisible();
        await page.locator("summary").filter({hasText:/^#1 · broker-op/}).click();
        await page.getByRole("button",{name:locale==="ru"?"Предпросмотр сделки":"Trade preview",exact:true}).click();
        const brokerPreview=page.getByLabel(locale==="ru"?"Предпросмотр сделки":"Trade preview",{exact:true});
        await brokerPreview.getByRole("combobox").selectOption("ledger-1");
        await brokerPreview.getByRole("button",{name:locale==="ru"?"Подготовить сделку":"Prepare trade",exact:true}).click();
        await expect(brokerPreview.getByText("123.45 RUB",{exact:true})).toBeVisible();
        await expect(brokerPreview.getByText("fixture history check failed",{exact:true})).toBeVisible();
        await expect(brokerPreview.getByText(/possible-snowball-duplicate/)).toBeVisible();
        await expect(brokerPreview.getByText("9007199254740993",{exact:true})).toBeVisible();
        const linkButton=brokerPreview.getByRole("button",{name:locale==="ru"?"Подтвердить связь":"Confirm link",exact:true});
        await expect(linkButton).toBeDisabled();
        await brokerPreview.getByLabel(locale==="ru"?"Запись журнала":"Journal operation",{exact:true}).selectOption("exact-journal-operation");
        await brokerPreview.getByLabel(locale==="ru"?"Основание сверки":"Reconciliation reason",{exact:true}).fill("Broker statement checked");
        await linkButton.click();
        await expect(brokerPreview.getByRole("alert")).toBeVisible();
        await expect(brokerPreview.getByLabel(locale==="ru"?"Основание сверки":"Reconciliation reason",{exact:true})).toHaveValue("Broker statement checked");
        await linkButton.click();
        await expect(brokerPreview.getByText(/saved-broker-link/)).toBeVisible();
        await expect(linkButton).toBeDisabled();
        expect(brokerLinks).toBe(2);
        await expect(page.getByText(locale==="ru"?"Запись журнала или версия источника отличается от подтверждённой. Требуется повторная сверка; брокерский ID остаётся занятым.":"The journal operation or source version differs from the confirmed evidence. Review is required; the broker ID remains claimed.",{exact:true})).toBeVisible();
        await brokerPreview.getByLabel(locale==="ru"?"Счёт назначения сделки":"Trade destination ledger",{exact:true}).selectOption("ledger-2");
        await expect(brokerPreview.getByText("123.45 RUB",{exact:true})).toHaveCount(0);
        await brokerPreview.getByRole("button",{name:locale==="ru"?"Подготовить сделку":"Prepare trade",exact:true}).click();
        await expect(brokerPreview.getByRole("alert")).toBeVisible();
        await expect(brokerPreview.getByText("123.45 RUB",{exact:true})).toHaveCount(0);
        await page.locator("details").filter({has:page.locator("summary").filter({hasText:"#2 · new-broker-op"})}).locator("summary").click();
        await page.locator("details").filter({has:page.locator("summary").filter({hasText:"#2 · new-broker-op"})}).getByRole("button").click();
        await brokerPreview.getByRole("combobox").selectOption("ledger-1");
        await brokerPreview.getByRole("button",{name:locale==="ru"?"Подготовить сделку":"Prepare trade",exact:true}).click();
        const postButton=brokerPreview.getByRole("button",{name:locale==="ru"?"Провести сделку":"Post trade",exact:true});
        await expect(postButton).toBeDisabled();
        await brokerPreview.getByLabel(locale==="ru"?"Основание проведения":"Posting reason",{exact:true}).fill("Reviewed new trade");
        await brokerPreview.getByRole("checkbox").check();
        await postButton.click();
        await expect(brokerPreview.getByRole("alert")).toBeVisible();
        await brokerPreview.getByRole("button",{name:locale==="ru"?"Повторить тот же запрос":"Retry the same request",exact:true}).click();
        await expect(brokerPreview.getByText(/posted-broker-operation/)).toBeVisible();
        expect(brokerPosts).toBe(2);
        const brokerDownload=page.waitForEvent("download");
        await page.getByRole("button",{name:locale==="ru"?"Скачать исходный JSON":"Download original JSON",exact:true}).click();
        expect((await brokerDownload).suggestedFilename()).toBe("broker-11111111-1111-4111-8111-111111111111.json");
        await page.getByRole("button", { name: /history.csv/ }).click();
        await expect(page.getByText("SBER", {exact:true})).toBeVisible();
        await expect(page.getByText("250.123456789 RUB", {exact:true})).toBeVisible();
        await page.getByRole("button", {name:locale === "ru" ? "Изменить счёт" : "Change account"}).click();
        await expect(page.getByRole("dialog")).toBeVisible();
        await page.getByRole("dialog").getByRole("combobox").selectOption("proposal-b");
        await page.getByRole("dialog").getByRole("textbox").fill("Confirmed broker statement");
        await page.getByRole("button", {name:locale === "ru" ? "Сохранить" : "Save", exact:true}).click();
        await expect(page.getByRole("dialog")).not.toBeVisible();
        await expect(page.locator("tbody").getByRole("cell", {name:/Unassigned/})).toBeVisible();
        await page.locator('input[type="file"][accept=".csv,text/csv"]').setInputFiles({name:"synthetic.csv",mimeType:"text/csv",buffer:Buffer.from("Event,Date,Symbol\n")});
        const uploadResponse = page.waitForResponse(r => r.url().endsWith("/UploadInvestmentImport"));
        await page.getByRole("button", {name:locale === "ru" ? "Загрузить для проверки" : "Upload for review"}).click();
        await uploadResponse;
        const preparation=page.getByTestId("investment-financial-preparation");
        await preparation.getByRole("button",{name:locale==="ru"?"Настроить подготовку":"Configure preparation",exact:true}).click();
        await preparation.getByRole("combobox",{name:"Unassigned",exact:true}).selectOption("ledger-1");
        await preparation.getByLabel(locale==="ru"?"Часовой пояс дат в CSV":"Timezone of dates in the CSV",{exact:true}).fill("UTC");
        await preparation.getByRole("combobox",{name:locale==="ru"?"Даты расчётов":"Settlement dates",exact:true}).selectOption("assume_trade_time");
        await preparation.getByRole("checkbox",{name:locale==="ru"?"Применить проверенное правило к этой группе":"Apply a reviewed rule to this group"}).check();
        await preparation.getByRole("combobox",{name:locale==="ru"?"Вид операции в журнале":"Journal operation kind",exact:true}).selectOption("coupon");
        await preparation.getByRole("combobox",{name:locale==="ru"?"Что означает исходная сумма":"Source amount interpretation",exact:true}).selectOption("quantity");
        await expect(preparation.getByText(locale==="ru"?"Деньги и налог будут учтены. До распределения купленного НКД по лотам доход по купонам останется неполным.":"Cash and withholding tax will be recorded. Coupon income remains incomplete until purchased accrued interest is allocated to lots.",{exact:true})).toBeVisible();
        await expect(preparation.getByLabel(locale==="ru"?"Идентификатор инструмента в журнале (для бумаг)":"Journal instrument identifier (for securities)",{exact:true})).toBeVisible();
        for(const [ru,en,value] of [["Вид операции в журнале","Journal operation kind","buy"],["Что означает исходная сумма","Source amount interpretation","quantity_price"],["Как задан НКД (для сделок)","Accrued interest interpretation (trades)","zero_only"],["Валюта комиссии или налога","Fee or tax currency","same_currency"]])await preparation.getByRole("combobox",{name:locale==="ru"?ru:en,exact:true}).selectOption(value);
        for(const [ru,en,value] of [["Идентификатор инструмента в журнале (для бумаг)","Journal instrument identifier (for securities)","isin:SBER"],["Множитель суммы (1 — без изменения)","Amount multiplier (1 means unchanged)","1"],["Штук в исходной единице количества (для сделок)","Security units per source quantity unit (trades)","1"],["На чём основана эта трактовка","Evidence for this interpretation","Confirmed broker quantities and fees"]])await preparation.getByLabel(locale==="ru"?ru:en,{exact:true}).fill(value);
        const evidenceFile=preparation.getByLabel(locale==="ru"?"Проверенные сделки брокера (необязательно)":"Reviewed broker trades (optional)",{exact:true});
        await evidenceFile.setInputFiles({name:"wrong-source.json",mimeType:"application/json",buffer:Buffer.from(JSON.stringify({source_sha256:"wrong",trade_evidence:{}}))});
        await expect(preparation.getByRole("alert")).toContainText(locale==="ru"?"другому исходному CSV":"different source CSV");
        await evidenceFile.setInputFiles({name:"reviewed-trades.json",mimeType:"application/json",buffer:Buffer.from(JSON.stringify({source_sha256:"c".repeat(64),trade_evidence:{"2":{source_row_id:"c".repeat(64)+":2",broker_account_id:"fixture",broker_operation_id:"trade",snapshot_sha256:"a".repeat(64),report_hash:"d".repeat(64),instrument_uid:"11111111-1111-4111-8111-111111111111",instrument_type:"share",ticker:"SBER",currency:"RUB",operation_type:"OPERATION_TYPE_BUY",state:"OPERATION_STATE_EXECUTED",quantity:"10",price:"250.123456789",payment:"-2501.23",commission:"0",broker_date:"2026-01-01T12:00:00Z",date_only:false,reason:"Confirmed broker cash"}}}))});
        const incomeFile=preparation.getByLabel(locale==="ru"?"Проверенные выплаты брокера (необязательно)":"Reviewed broker payouts (optional)",{exact:true});
        await incomeFile.setInputFiles({name:"income.json",mimeType:"application/json",buffer:Buffer.from(JSON.stringify({source_sha256:"c".repeat(64),income_evidence:{"3":{source_row_id:"c".repeat(64)+":3",credit:{payment:"100.123456789"},tax:{payment:"-13"}}}}))});
        const useIncome=preparation.getByRole("checkbox",{name:locale==="ru"?"Использовать проверенные выплаты для 1 строк":"Use reviewed payouts for 1 rows",exact:true});
        await expect(useIncome).not.toBeChecked();await useIncome.check();
        const cashFile=preparation.getByLabel(locale==="ru"?"Проверенные пополнения, налоги и РЕПО (необязательно)":"Reviewed deposits, taxes and repo income (optional)",{exact:true});
        await cashFile.setInputFiles({name:"cash.json",mimeType:"application/json",buffer:Buffer.from(JSON.stringify({source_sha256:"c".repeat(64),cash_evidence:{"1":{source_row_id:"c".repeat(64)+":1",operation:{payment:"100.123456789"}}}}))});
        const useCash=preparation.getByRole("checkbox",{name:locale==="ru"?"Использовать подтверждения денежных операций для 1 строк":"Use reviewed cash operations for 1 rows",exact:true});
        await expect(useCash).not.toBeChecked();await useCash.check();
        const useEvidence=preparation.getByRole("checkbox",{name:locale==="ru"?"Использовать проверенные денежные компоненты для 1 строк":"Use reviewed monetary components for 1 rows",exact:true});
        await expect(useEvidence).not.toBeChecked();await useEvidence.check();
        await preparation.getByRole("button",{name:locale==="ru"?"Подготовить и проверить кандидатов":"Prepare and review candidates",exact:true}).click();
        await expect(preparation.getByRole("status")).toContainText(locale==="ru"?"требуют решения 1":"1 need a decision");
        const factor=preparation.getByRole("textbox",{name:locale==="ru"?"Множитель суммы (1 — без изменения)":"Amount multiplier (1 means unchanged)",exact:true});
        await factor.fill("2");await expect(preparation.getByRole("status")).toHaveCount(0);
        await factor.fill("1");await preparation.getByRole("button",{name:locale==="ru"?"Подготовить и проверить кандидатов":"Prepare and review candidates",exact:true}).click();
        await preparation.getByRole("checkbox",{name:locale==="ru"?"Выбрать все готовые строки этого блока (2)":"Select all ready rows in this block (2)",exact:true}).check();
        await preparation.getByRole("checkbox",{name:/^#1/}).uncheck();
        if(process.env.E2E_SCREENSHOT_DIR){await page.setViewportSize({width:375,height:3200});await preparation.evaluate(el=>el.scrollIntoView({block:"center"}));await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));await preparation.screenshot({path:`${process.env.E2E_SCREENSHOT_DIR}/financial-preparation-mobile-${locale}.png`});}
        await preparation.getByRole("button",{name:locale==="ru"?"Сохранить выбранные операции (1)":"Save selected operations (1)",exact:true}).click();
        await expect(preparation.getByLabel(locale==="ru"?"Часовой пояс дат в CSV":"Timezone of dates in the CSV",{exact:true})).toBeDisabled();
        await preparation.getByRole("button",{name:locale==="ru"?"Повторить сохранение":"Retry save",exact:true}).click();
        await expect(preparation.getByRole("link",{name:locale==="ru"?"Открыть пакеты в журнале":"Open journal batches",exact:true})).toBeVisible();
        expect(batchSaveAttempts).toBe(2);expect(savedBatch.batch.entries).toHaveLength(1);expect(batchPosts).toBe(0);
        if (process.env.E2E_SCREENSHOT_DIR) { await page.waitForLoadState("networkidle"); await page.evaluate(() => window.scrollTo(0, 0)); await page.waitForFunction(() => window.scrollY === 0); await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))); await page.screenshot({fullPage:true,path:`${process.env.E2E_SCREENSHOT_DIR}/investments-${locale}.png`}); }
      }
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
