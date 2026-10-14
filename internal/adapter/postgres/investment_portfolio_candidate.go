package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"time"

	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

type PortfolioCandidateInput struct {
	OriginalCSV []byte
	Sources     []PortfolioCandidateSource
}
type PortfolioCandidateSource struct {
	Filename, LedgerID, AccountKey string
	CSV                            []byte
	Mapping                        imp.FinancialMapping
	Lineage                        []PortfolioCandidateLineage
	AcceptedRecords                []int
	ExpectedPositions              []d.InvestmentPosition
}
type PortfolioCandidateLineage struct {
	SourceRecord int
	Reason       string
}
type PortfolioCandidateReport struct {
	Ledgers, Operations, Payouts, PendingRows int
	Archive                                   archive.Bundle
}

// BuildPortfolioCandidate imports checksum-bound source into an EMPTY isolated
// DB seeded with the target account IDs. It never accepts a production reset.
func (r *InvestmentRepo) BuildPortfolioCandidate(ctx context.Context, actor string, before archive.Bundle, input PortfolioCandidateInput) (PortfolioCandidateReport, error) {
	var out PortfolioCandidateReport
	original, e := imp.StageSnowball(bytes.NewReader(input.OriginalCSV), true)
	if e != nil {
		return out, e
	}
	if len(input.Sources) == 0 || len(input.Sources) > 20 {
		return out, d.ErrInvestmentInvalid
	}
	originalUses := map[int]int{}
	ledgers := map[string]bool{}
	for _, s := range input.Sources {
		if ledgers[s.LedgerID] {
			return out, d.ErrInvestmentInvalid
		}
		ledgers[s.LedgerID] = true
		derived, e := imp.StageSnowball(bytes.NewReader(s.CSV), true)
		if e != nil {
			return out, e
		}
		if len(s.Lineage) != len(derived.Rows) || len(s.AcceptedRecords) == 0 || s.Mapping.SourceSHA256 != derived.Audit.SHA256 {
			return out, d.ErrInvestmentPrecondition
		}
		for i, row := range derived.Rows {
			line := s.Lineage[i]
			if line.SourceRecord < 1 || line.SourceRecord > len(original.Rows) || line.Reason == "" || !reflect.DeepEqual(row.Raw, original.Rows[line.SourceRecord-1].Raw) || s.Mapping.LedgerByRecord[i+1] != s.LedgerID {
				return out, d.ErrInvestmentPrecondition
			}
			originalUses[line.SourceRecord]++
			if originalUses[line.SourceRecord] > 1 && row.Raw["Event"] != "SPLIT" {
				return out, d.ErrInvestmentPrecondition
			}
		}
	}
	if e = r.SeedPortfolioCandidateScope(ctx, actor, before); e != nil {
		return out, e
	}
	tenant := before.Data.TenantID
	svc := inv.NewService(r, NewTenantRepo(r.pool))
	importer := imp.NewImportService(NewInvestmentImportRepo(r.pool), NewTenantRepo(r.pool))
	for _, s := range input.Sources {
		upload := imp.Upload{Filename: s.Filename, RequestKey: "candidate-" + s.LedgerID, CSV: s.CSV, Accounts: []imp.ImportAccount{{SourceKey: s.AccountKey, Name: "Source portfolio"}}}
		for i, l := range s.Lineage {
			upload.Assignments = append(upload.Assignments, imp.Assignment{Record: i + 1, AccountKey: s.AccountKey, Evidence: l.Reason})
		}
		source, e := importer.Upload(ctx, tenant, actor, upload)
		if e != nil {
			return out, e
		}
		prepared, e := importer.PreviewFinancial(ctx, tenant, actor, imp.FinancialReviewRequest{ImportID: source.ID, ExpectedImportVersion: source.Version, RequestKey: "candidate-review-" + s.LedgerID, Mapping: s.Mapping, Limit: 5000})
		if e != nil {
			return out, e
		}
		if prepared.Batch == nil {
			return out, d.ErrInvestmentPrecondition
		}
		accepted := map[int]bool{}
		for _, record := range s.AcceptedRecords {
			if record < 1 || record > source.Rows || accepted[record] {
				return out, d.ErrInvestmentInvalid
			}
			accepted[record] = true
		}
		batch := *prepared.Batch
		batch.Entries = nil
		for _, entry := range prepared.Batch.Entries {
			if accepted[entry.Record] {
				batch.Entries = append(batch.Entries, entry)
			}
		}
		if len(batch.Entries) != len(accepted) {
			return out, d.ErrInvestmentPrecondition
		}
		preview, e := svc.PreviewImportBatch(ctx, tenant, actor, batch)
		if e != nil {
			return out, e
		}
		posted, e := svc.CommitImportBatch(ctx, tenant, actor, batch, preview.Hash)
		if e != nil {
			return out, e
		}
		state, e := svc.Snapshot(ctx, tenant, actor, s.LedgerID, time.Now().UTC())
		if e != nil {
			return out, e
		}
		if !reflect.DeepEqual(portfolioPositionControl(state.Positions), portfolioPositionControl(s.ExpectedPositions)) {
			return out, d.ErrInvestmentPrecondition
		}
		out.Operations += len(posted.Operations)
		out.PendingRows += source.Rows - len(posted.Operations)
		for _, o := range posted.Operations {
			if o.Kind == "dividend" || o.Kind == "coupon" || o.Kind == "repo_income" || o.Kind == "cash_interest" {
				out.Payouts++
			}
		}
	}
	out.Archive, e = r.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		return out, e
	}
	verified, e := archive.VerifyJournal(out.Archive.Data)
	if e != nil {
		return out, e
	}
	if verified.Operations != out.Operations {
		return out, d.ErrInvestmentPrecondition
	}
	out.Ledgers = verified.Ledgers
	return out, nil
}

func portfolioPositionControl(positions []d.InvestmentPosition) []string {
	out := []string{}
	for _, p := range positions {
		p.Lots = append([]d.InvestmentLot(nil), p.Lots...)
		for i := range p.Lots {
			p.Lots[i].OriginID = ""
			p.Lots[i].AcquisitionOperationID = ""
			p.Lots[i].OriginLedgerID = ""
			p.Lots[i].OriginSequence = 0
		}
		raw, _ := json.Marshal(p)
		out = append(out, string(raw))
	}
	sort.Strings(out)
	return out
}
