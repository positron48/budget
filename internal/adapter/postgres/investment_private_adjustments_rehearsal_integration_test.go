package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type privateAdjustmentEntry struct {
	Basis                                                                         string
	UID, ISIN, ValuationDate                                                      string
	ObservationAt                                                                 time.Time
	Multiplier, Divisor, ResolvedPrice, SourceReference, EvidenceSHA256, Evidence string
}

type privateAdjustmentPlan struct {
	Version   int
	Documents map[string]string
	Entries   []privateAdjustmentEntry
}

func loadPrivateAdjustmentPlan(t *testing.T) map[string]privateAdjustmentEntry {
	t.Helper()
	path := os.Getenv("INVESTMENT_REHEARSAL_PRICE_ADJUSTMENTS")
	if path == "" {
		return nil
	}
	var plan privateAdjustmentPlan
	readPrivateHistory(t, path, "", &plan)
	if plan.Version != 1 || len(plan.Entries) == 0 || len(plan.Entries) > 1000 || len(plan.Documents) == 0 || len(plan.Documents) > 20 {
		t.Fatal("invalid private price adjustment plan")
	}
	hashes := map[string]bool{}
	for name, expected := range plan.Documents {
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".html") {
			t.Fatal("invalid private evidence path")
		}
		doc := filepath.Join(filepath.Dir(path), name)
		info, err := os.Lstat(doc)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4<<20 {
			t.Fatal("invalid private source document")
		}
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal("read evidence document")
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != expected {
			t.Fatal("source document checksum changed")
		}
		hashes[expected] = true
	}
	out := map[string]privateAdjustmentEntry{}
	for _, entry := range plan.Entries {
		key := entry.UID + "/" + entry.ValuationDate
		if _, exists := out[key]; exists || !hashes[entry.EvidenceSHA256] {
			t.Fatal("ambiguous or unpinned private review")
		}
		out[key] = entry
	}
	return out
}

func privateAdjustment(t *testing.T, repo *MarketDataRepo, mapping md.MappingPeriod, entry privateAdjustmentEntry, instrument md.Instrument, scope md.Scope, selected md.SelectedPrice, observations []md.ScopedObservation, from, to time.Time) nav.ReviewedPriceAdjustment {
	t.Helper()
	if instrument.ID != entry.UID || instrument.ISIN != entry.ISIN || selected.Fact == nil {
		t.Fatal("private split identity mismatch")
	}
	for _, o := range observations {
		if o.ID == selected.ObservationID && o.Revision == selected.ObservationRevision {
			if !o.At.Equal(entry.ObservationAt) {
				t.Fatal("private split source date changed")
			}
			review := nav.ReviewedPriceAdjustment{Basis: entry.Basis, Scope: scope, ObservationID: o.ID, ObservationRevision: o.Revision, ObservationHash: nav.PriceObservationHash(o.Observation), Source: o.Observation, From: from, To: to, Kind: instrument.Kind, Multiplier: entry.Multiplier, Divisor: entry.Divisor, ResolvedPrice: entry.ResolvedPrice, SourceReference: entry.SourceReference, EvidenceSHA256: entry.EvidenceSHA256, Evidence: entry.Evidence}
			if entry.Basis == nav.NativeBondPercentBasis {
				facts, err := repo.BondFacts(context.Background(), scope, md.JobRequest{Kind: "bond_facts", InstrumentID: instrument.ID, From: o.At, To: from.Add(time.Second)})
				if err != nil || facts == nil || mapping.Kind != "bond" {
					t.Fatal("missing native bond nominal source", err)
				}
				basis := &nav.ReviewedNativeBondBasis{Nominal: mapping.Nominal, Currency: mapping.NominalCurrency}
				for date := o.At; !date.After(from); date = date.Add(24 * time.Hour) {
					found := false
					for _, point := range facts.Accrued {
						if point.Date.Equal(date) && point.Nominal != nil && nav.SameAccruedAmount(*point.Nominal, basis.Nominal) {
							found = true
							break
						}
					}
					if !found {
						t.Fatal("native bond quote crosses unknown or changed nominal")
					}
					basis.Points = append(basis.Points, nav.ReviewedBondNominalPoint{SnapshotID: facts.ID, SnapshotHash: facts.Hash, Date: date})
				}
				review.NativeBond = basis
			}
			return review
		}
	}
	t.Fatal("private split source observation missing")
	return nav.ReviewedPriceAdjustment{}
}
