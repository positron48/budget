package investmentimport

import (
	"strings"
	"testing"
)

func TestAuditPreservesIdenticalAndUnknownEvents(t *testing.T) {
	row := "BUY,2020-01-01 12:00:00,TEST,10,2,USD,0,NASDAQ,,USD,False,\n"
	source := "\xef\xbb\xbf" + strings.Join(snowballHeader, ",") + "\n" + row + row + "UNMAPPED,2020-01-02 00:00:00,TEST,0,0,USD,0,NASDAQ,,,False,\n"
	a, err := AuditSnowball(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if a.Rows != 3 || a.IdenticalRows != 1 || a.Events["UNMAPPED"] != 1 || len(a.Issues) != 0 || len(a.Instruments) != 1 {
		t.Fatalf("unexpected audit: %+v", a)
	}
	if a.FirstDate != "2020-01-01 12:00:00" || a.LastDate != "2020-01-02 00:00:00" || len(a.SHA256) != 64 {
		t.Fatal(a)
	}
}

func TestAuditRejectsBrokenStructure(t *testing.T) {
	for _, source := range []string{"", "Event,Date\n", strings.Join(snowballHeader, ",") + "\n", strings.Join(snowballHeader, ",") + "\nBUY,broken\n", strings.Repeat("x", MaxSourceBytes+1)} {
		if _, err := AuditSnowball(strings.NewReader(source)); err == nil {
			t.Fatal("accepted malformed source")
		}
	}
}

func TestAuditReportsInvalidFieldsWithoutLeakingValues(t *testing.T) {
	source := strings.Join(snowballHeader, ",") + "\nBUY,not-a-date,TEST,NaN,1/2,USD,0,NASDAQ,,,maybe,private-note\n"
	a, err := AuditSnowball(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Issues) != 4 {
		t.Fatalf("issues: %+v", a.Issues)
	}
}

func TestAuditAcceptsDecimalCommaAndSplitBlankFlag(t *testing.T) {
	source := strings.Join(snowballHeader, ",") + "\nSPLIT,2020-01-01 00:00:00,TEST,0,2,USD,0,NASDAQ,,,,\nBUY,2020-01-02 00:00:00,TEST,\"12,34\",1,USD,0,NASDAQ,,,False,\"quoted, note\"\n"
	a, err := AuditSnowball(strings.NewReader(source))
	if err != nil || len(a.Issues) != 0 {
		t.Fatalf("%+v %v", a, err)
	}
}
