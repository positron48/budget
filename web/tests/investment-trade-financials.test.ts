import {it,expect} from "vitest";
import {create,toBinary,fromBinary} from "@bufbuild/protobuf";
import {InvestmentOperationSchema} from "../proto/budget/v1/investment_pb";
import {InvestmentImportSchema} from "../proto/budget/v1/investment_import_pb";
import {tradeFinancialDraft,tradeFinancialPayload} from "../components/InvestmentTradeFinancialsForm";
import {reviewedBatch,storedBatchRows} from "../lib/investment-batch";
it("preserves exact native money, FX ratios and timestamp precision through amendment and saved import review",()=>{
 const at={seconds:1704412799n,nanos:999999000},fx={seconds:1704153600n,nanos:123456000};
 const original=create(InvestmentOperationSchema,{ledgerId:"ledger",kind:"buy",importedCashDeficitReason:"Original export lacks funding",currency:"USD",quantity:"2",tradeFinancials:{price:"9007199254740993.123456789012",priceCurrency:"EUR",priceFxNumerator:"1",priceFxDenominator:"3",priceFxSource:"Reviewed execution rate",priceFxEvidence:"Original EUR quote",priceFxAt:fx,priceUnit:"currency_per_unit",actualSettlement:"9007199254740993.123456789012",principal:{amount:"9007199254740993.123456789012",currency:"RUB",at,fxNumerator:"1",fxDenominator:"90",fxSource:"Contract",fxEvidence:"Dated report",fxAt:at},accrued:{amount:"0",currency:"RUB",at},fee:{amount:"1",currency:"EUR",at,fxNumerator:"1",fxDenominator:"3",fxSource:"Fee date rate",fxEvidence:"Dated rate",fxAt:at},tax:{amount:"2",currency:"USD",at}}});
 const draft=tradeFinancialDraft(original.tradeFinancials,"USD"),payload=tradeFinancialPayload(draft,"USD",original.tradeFinancials);
 expect(payload.principal.amount).toBe("9007199254740993.123456789012");expect(payload.principal.at?.nanos).toBe(999999000);expect(payload.principal.fxAt?.nanos).toBe(999999000);expect(payload.priceFxAt?.nanos).toBe(123456000);expect(payload.priceFxDenominator).toBe("3");
 const candidate=create(InvestmentOperationSchema,{...original,tradeFinancials:undefined});candidate.tradeFinancials=create(InvestmentOperationSchema,{tradeFinancials:payload}).tradeFinancials;
 const batch=reviewedBatch([{importId:"source",previewHash:"hash",filename:"source.csv",record:1,rowVersion:9007199254740993n,reviewReason:"Reviewed native settlement",ledgerName:"Broker",operation:candidate}],create(InvestmentImportSchema,{id:"source",version:1n,previewHash:"hash"}),{ledger:7n},"review");
 const restored=storedBatchRows(batch,"source.csv",{ledger:"Broker"})[0].operation;
 const wire=fromBinary(InvestmentOperationSchema,toBinary(InvestmentOperationSchema,restored));expect(wire.importedCashDeficitReason).toBe("Original export lacks funding");expect(wire.tradeFinancials?.principal?.amount).toBe(payload.principal.amount);expect(wire.tradeFinancials?.priceCurrency).toBe("EUR");expect(wire.tradeFinancials?.fee?.fxDenominator).toBe("3");expect(wire.tradeFinancials?.principal?.fxAt?.nanos).toBe(999999000);
});
it("clears inactive currency conversions when the user explicitly switches to the book currency",()=>{
 const draft=tradeFinancialDraft(undefined,"USD");draft.enabled=true;draft.principal.amount="1";draft.principal.fxNumerator="90";draft.principal.fxDenominator="1";draft.principal.fxSource="Old conversion";draft.principal.fxEvidence="Old report";
 const output=tradeFinancialPayload(draft,"USD");expect(output.principal.fxNumerator).toBe("");expect(output.principal.fxAt).toBeUndefined();
});
