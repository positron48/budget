package tinvest

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFindInstrument(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-test-credential" || r.Method != "POST" {
			t.Error("invalid authorization/method")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["query"] != "SBER" {
			t.Error("invalid query")
		}
		if _, err := fmt.Fprint(w, `{"instruments":[{"uid":"example","ticker":"SBER","classCode":"TQBR"}]}`); err != nil {
			t.Error(err)
		}
	}))
	defer s.Close()
	c, _ := newClient("fake-test-credential")
	c.baseURL = s.URL + "/"
	items, err := c.FindInstrument(context.Background(), "SBER")
	if err != nil || len(items) != 1 || items[0].UID != "example" {
		t.Fatalf("unexpected result: %v %v", items, err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", c, c, c), "fake-test-credential") {
		t.Fatal("credential exposed by formatting")
	}
}

func TestScopedCertificateTrust(t *testing.T) {
	c, err := newClient("fake-test-credential")
	if err != nil {
		t.Fatal(err)
	}
	transport := c.http.Transport.(*http.Transport)
	if transport == http.DefaultTransport || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS must use an isolated verifying transport")
	}
	block, _ := pem.Decode(russianTrustedRoot)
	if block == nil {
		t.Fatal("missing embedded certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%X", sha256.Sum256(cert.Raw)) != "D26D2D0231B7C39F92CC738512BA54103519E4405D68B5BD703E9788CA8ECF31" {
		t.Fatal("bundled root changed; verify provenance")
	}
	if !cert.IsCA || cert.CheckSignatureFrom(cert) != nil {
		t.Fatal("expected self-signed root CA")
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: transport.TLSClientConfig.RootCAs, CurrentTime: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	var received atomic.Bool
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(true)
		if _, err := fmt.Fprint(w, `{"instruments":[]}`); err != nil {
			t.Error(err)
		}
	}))
	defer s.Close()
	c.baseURL = s.URL + "/"
	if _, err := c.FindInstrument(context.Background(), "SBER"); err == nil || received.Load() {
		t.Fatal("untrusted TLS endpoint received credential")
	}
	// Trust the synthetic server only in this test, then verify hostname checks.
	transport.TLSClientConfig.RootCAs.AddCert(s.Certificate())
	if _, err := c.FindInstrument(context.Background(), "SBER"); err != nil {
		t.Fatal(err)
	}
	transport.CloseIdleConnections()
	received.Store(false)
	transport.TLSClientConfig.ServerName = "wrong.invalid"
	if _, err := c.FindInstrument(context.Background(), "SBER"); err == nil || received.Load() {
		t.Fatal("hostname verification was bypassed")
	}
}

func TestErrorsAndRedirectsDoNotExposeCredentials(t *testing.T) {
	for _, code := range []int{302, 401, 429, 500, 200} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			forwarded := false
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
			defer destination.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(code)
				if _, err := fmt.Fprint(w, "fake-test-credential"); err != nil {
					t.Error(err)
				}
			}))
			defer s.Close()
			c, _ := newClient("fake-test-credential")
			c.baseURL = s.URL + "/"
			_, err := c.FindInstrument(context.Background(), "SBER")
			if err == nil || strings.Contains(err.Error(), "fake-test-credential") || forwarded {
				t.Fatalf("unsafe result: %v", err)
			}
		})
	}
}

func TestCatalogResponseLimitIsSeparate(t *testing.T) {
	payload := `{"instruments":[],"padding":"` + strings.Repeat("x", 3<<20) + `"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fmt.Fprint(w, payload); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, _ := newClient("fake-test-credential")
	client.baseURL = server.URL + "/"
	if _, err := client.FindInstrument(context.Background(), "C"); err != nil {
		t.Fatal(err)
	}
	var result any
	if err := client.read(context.Background(), "MarketDataService/GetCandles", nil, &result); err == nil {
		t.Fatal("ordinary endpoint exceeded 2 MiB limit")
	}
	payload = `{"padding":"` + strings.Repeat("x", 16<<20) + `"}`
	if _, err := client.FindInstrument(context.Background(), "C"); err == nil {
		t.Fatal("catalog response exceeded 16 MiB limit")
	}
}

func TestInstrumentKindFilterKeepsHistoricalListings(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["instrumentKind"] != "INSTRUMENT_TYPE_SHARE" || request["query"] != "C" {
			t.Error("missing explicit kind filter")
		}
		if _, ok := request["apiTradeAvailableFlag"]; ok {
			t.Error("historical nontradable instruments were filtered")
		}
		if _, err := fmt.Fprint(w, `{"instruments":[{"uid":"candidate","ticker":"C","classCode":"SPBXM"}]}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, _ := newClient("fake-test-credential")
	client.baseURL = server.URL + "/"
	got, err := client.FindInstrumentOfKind(context.Background(), "C", "INSTRUMENT_TYPE_SHARE")
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if _, err = client.FindInstrumentOfKind(context.Background(), "C", "unknown"); err == nil || calls != 1 {
		t.Fatal("invalid kind reached provider")
	}
}
