package investmentimport

import (
	d "github.com/positron48/budget/internal/domain"
	"time"
)

func mappedTiming(row PreviewRow, mapping FinancialMapping) *d.InvestmentOperationTiming {
	timing := &d.InvestmentOperationTiming{Timezone: mapping.Timezone}
	if mapping.DateOnlyByRecord[row.Record] && len(row.WallTime) >= 10 {
		timing.OccurredDate = row.WallTime[:10]
		timing.SourceDate = timing.OccurredDate
	}
	if mapping.SettlementPolicy == "explicit" {
		timing.SettlementDate = mapping.SettlementDateByRecord[row.Record]
	}
	return timing
}

func applyMappedDateOnly(op *d.InvestmentOperation, row PreviewRow) {
	if op.Timing != nil && op.Timing.OccurredDate != "" && len(row.WallTime) == 19 && row.WallTime[11:] == "00:00:00" {
		op.OccurredAt = time.Time{}
	}
}
