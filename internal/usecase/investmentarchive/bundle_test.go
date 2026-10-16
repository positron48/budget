package investmentarchive

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const tenant = "11111111-1111-4111-8111-111111111111"

func fixture() Dataset {
	d := Dataset{TenantID: tenant, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Excluded: map[string]string{}}
	for k, v := range Excluded {
		d.Excluded[k] = v
	}
	for _, name := range Included {
		d.Tables = append(d.Tables, Table{Name: name, Columns: []Column{{Name: "tenant_id", Type: "uuid", NotNull: true}}, Rows: []json.RawMessage{}})
	}
	return d
}
func TestArchiveRoundTripExactRows(t *testing.T) {
	data := fixture()
	data.Tables[0].Columns = append(data.Tables[0].Columns, Column{Name: "amount", Type: "numeric(36,12)"})
	data.Tables[0].Rows = []json.RawMessage{json.RawMessage(`{"tenant_id":"` + tenant + `","amount":9007199254740993.123456789012}`)}
	Canonicalize(&data)
	sealed, e := Seal(data)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(sealed)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := Decode(bytes.NewReader(raw))
	if e != nil || decoded.SHA256 != sealed.SHA256 {
		t.Fatal(e)
	}
	if !bytes.Contains(raw, []byte("9007199254740993.123456789012")) {
		t.Fatal("decimal rounded")
	}
	decoded.Data.CreatedAt = decoded.Data.CreatedAt.Add(time.Second)
	tampered, _ := json.Marshal(decoded)
	if _, e = Decode(bytes.NewReader(tampered)); e == nil {
		t.Fatal("tampered hash accepted")
	}
	for _, input := range []string{string(raw) + "{}", `{"Format":"future"}`, "null"} {
		if _, e = Decode(strings.NewReader(input)); e == nil {
			t.Fatal("invalid archive accepted")
		}
	}
}
func TestArchiveManifestAndTenantBoundaries(t *testing.T) {
	for _, change := range []func(*Dataset){
		func(d *Dataset) { d.Tables = d.Tables[1:] },
		func(d *Dataset) { d.Tables[0].Name = "users" },
		func(d *Dataset) {
			d.Tables[0].Rows = []json.RawMessage{json.RawMessage(`{"tenant_id":"22222222-2222-4222-8222-222222222222"}`)}
		},
		func(d *Dataset) {
			d.Tables[0].Rows = []json.RawMessage{json.RawMessage(`{"tenant_id":"` + tenant + `","unknown":1}`)}
		},
		func(d *Dataset) { d.Excluded["investment_fx_points"] = "silently included" },
	} {
		d := fixture()
		change(&d)
		if _, e := Seal(d); e == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
}

func TestArchivePreActivationManifestCompatibility(t *testing.T) {
	data := fixture()
	delete(data.Excluded, "investment_asset_activations")
	delete(data.Excluded, "investment_cash_links")
	if _, e := Seal(data); e != nil {
		t.Fatal("pre-activation v1 bundle rejected", e)
	}
	data.Excluded["unknown_investment_table"] = "omitted"
	if _, e := Seal(data); e == nil {
		t.Fatal("unknown table omission accepted")
	}
}

func TestArchivePredatingExportJobsRemainsReadable(t *testing.T) {
	for _, legacyIntegration := range []bool{false, true} {
		data := fixture()
		delete(data.Excluded, "investment_export_jobs")
		if legacyIntegration {
			delete(data.Excluded, "investment_cash_links")
			delete(data.Excluded, "investment_asset_activations")
		}
		b, e := Seal(data)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := json.Marshal(b)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = Decode(bytes.NewReader(raw)); e != nil {
			t.Fatal("legacy exclusions rejected", e)
		}
	}
}

func TestArchivePredatingChartProjectionsRemainsReadable(t *testing.T) {
	data := fixture()
	delete(data.Excluded, "investment_tracker_charts")
	bundle, e := Seal(data)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(bundle)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Decode(bytes.NewReader(raw)); e != nil {
		t.Fatal("pre-projection archive rejected", e)
	}
}
