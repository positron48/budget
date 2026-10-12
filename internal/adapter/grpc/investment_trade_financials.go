package grpcadapter

import (
	pb "github.com/positron48/budget/gen/go/budget/v1"
	d "github.com/positron48/budget/internal/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func protoTradeMoney(m d.InvestmentTradeMoney) *pb.InvestmentTradeMoney {
	return &pb.InvestmentTradeMoney{Amount: m.Amount, Currency: m.Currency, At: optionalInvestmentTimestamp(m.At), FxNumerator: m.FXNumerator, FxDenominator: m.FXDenominator, FxSource: m.FXSource, FxEvidence: m.FXEvidence, FxAt: optionalInvestmentTimestamp(m.FXAt)}
}
func protoTradeFinancials(f *d.InvestmentTradeFinancials) *pb.InvestmentTradeFinancials {
	if f == nil {
		return nil
	}
	return &pb.InvestmentTradeFinancials{ActualSettlementIncludesExpenses: f.ActualSettlementIncludesExpenses, PriceCurrency: f.PriceCurrency, PriceFxNumerator: f.PriceFXNumerator, PriceFxDenominator: f.PriceFXDenominator, PriceFxSource: f.PriceFXSource, PriceFxEvidence: f.PriceFXEvidence, PriceFxAt: optionalInvestmentTimestamp(f.PriceFXAt), CalculatedSettlement: f.CalculatedSettlement, SettlementDifference: f.SettlementDifference, PrincipalDifferenceNumerator: f.PrincipalDifferenceNumerator, PrincipalDifferenceDenominator: f.PrincipalDifferenceDenominator, Price: f.Price, PriceUnit: f.PriceUnit, Nominal: f.Nominal, ActualSettlement: f.ActualSettlement, DifferenceReason: f.DifferenceReason, Principal: protoTradeMoney(f.Principal), Accrued: protoTradeMoney(f.Accrued), Fee: protoTradeMoney(f.Fee), Tax: protoTradeMoney(f.Tax)}
}
func parseTradeFinancials(f *pb.InvestmentTradeFinancials) (*d.InvestmentTradeFinancials, error) {
	if f == nil {
		return nil, nil
	}
	out := &d.InvestmentTradeFinancials{ActualSettlementIncludesExpenses: f.GetActualSettlementIncludesExpenses(), PriceCurrency: f.GetPriceCurrency(), PriceFXNumerator: f.GetPriceFxNumerator(), PriceFXDenominator: f.GetPriceFxDenominator(), PriceFXSource: f.GetPriceFxSource(), PriceFXEvidence: f.GetPriceFxEvidence(), CalculatedSettlement: f.GetCalculatedSettlement(), SettlementDifference: f.GetSettlementDifference(), PrincipalDifferenceNumerator: f.GetPrincipalDifferenceNumerator(), PrincipalDifferenceDenominator: f.GetPrincipalDifferenceDenominator(), Price: f.GetPrice(), PriceUnit: f.GetPriceUnit(), Nominal: f.GetNominal(), ActualSettlement: f.GetActualSettlement(), DifferenceReason: f.GetDifferenceReason()}
	if f.GetPriceFxAt() != nil {
		if e := f.GetPriceFxAt().CheckValid(); e != nil {
			return nil, d.ErrInvestmentInvalid
		}
		out.PriceFXAt = f.GetPriceFxAt().AsTime()
	}
	for _, pair := range []struct {
		in  *pb.InvestmentTradeMoney
		out *d.InvestmentTradeMoney
	}{{f.GetPrincipal(), &out.Principal}, {f.GetAccrued(), &out.Accrued}, {f.GetFee(), &out.Fee}, {f.GetTax(), &out.Tax}} {
		if pair.in == nil {
			return nil, d.ErrInvestmentInvalid
		}
		m := pair.in
		*pair.out = d.InvestmentTradeMoney{Amount: m.GetAmount(), Currency: m.GetCurrency(), FXNumerator: m.GetFxNumerator(), FXDenominator: m.GetFxDenominator(), FXSource: m.GetFxSource(), FXEvidence: m.GetFxEvidence()}
		for _, v := range []struct {
			value *timestamppb.Timestamp
			fx    bool
		}{{m.GetAt(), false}, {m.GetFxAt(), true}} {
			if v.value != nil {
				if e := v.value.CheckValid(); e != nil {
					return nil, d.ErrInvestmentInvalid
				}
				if v.fx {
					pair.out.FXAt = v.value.AsTime()
				} else {
					pair.out.At = v.value.AsTime()
				}
			}
		}
	}
	return out, nil
}
