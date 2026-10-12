package investmentimport

import (
	"slices"
	"strings"
	"testing"
)

func TestPreviewPreservesAmbiguityAndPrecision(t *testing.T) {
	header := strings.Join(snowballHeader, ",") + "\n"
	buy := "BUY,2020-01-01 10:00:00,TEST,0.0000000000000001,3,USD,1,X,0,,False,\n"
	split := "SPLIT,2020-01-02 10:00:00,TEST,4,0,USD,0,X,0,,,\n"
	input := header + buy + buy + split + split
	p, err := PreviewSnowball(strings.NewReader(input), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 4 || p.Rows[0].SourceRowID == p.Rows[1].SourceRowID {
		t.Fatal("lost source identities")
	}
	if p.Rows[0].CandidatePrincipal != "0.0000000000000003" {
		t.Fatal(p.Rows[0].CandidatePrincipal)
	}
	if !slices.Equal(p.Rows[0].IdenticalRecords, []int{1, 2}) {
		t.Fatal(p.Rows[0])
	}
	if p.IssueCounts["corporate_action_scope_required"] != 2 || p.IssueCounts["fee_currency_required"] != 2 {
		t.Fatal(p.IssueCounts)
	}
	again, err := PreviewSnowball(strings.NewReader(input), true)
	if err != nil || again.Hash != p.Hash {
		t.Fatal("preview not deterministic", err)
	}
	changed, err := PreviewSnowball(strings.NewReader(strings.Replace(input, "TEST", "OTHER", 1)), true)
	if err != nil || changed.Hash == p.Hash {
		t.Fatal("preview failed to bind input", err)
	}
}

func TestPreviewUnknownAndInvalidEconomics(t *testing.T) {
	input := strings.Join(snowballHeader, ",") + "\nBUY,2020-01-01 10:00:00,T,-1,0,USD,0,X,0,,True,\nNEW,2020-01-01 10:00:00,T,1,1,USD,0,X,0,,False,\n"
	p, err := PreviewSnowball(strings.NewReader(input), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"negative_Price", "positive_quantity_required", "positive_price_required", "cash_suppression_requires_review", "unsupported_event"} {
		if p.IssueCounts[code] != 1 {
			t.Fatalf("missing %s", code)
		}
	}
	if _, err := PreviewSnowball(strings.NewReader(input), false); err == nil {
		t.Fatal("combined consent missing")
	}
}
