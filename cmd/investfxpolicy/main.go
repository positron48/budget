// investfxpolicy previews and applies an operator-reviewed FX source binding.
// It reads no broker token and performs no provider calls. Market source terms
// must be reviewed independently; a successful preview is not proof of rights.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"github.com/positron48/budget/internal/adapter/postgres"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"io"
	"os"
	"time"
)

type document struct {
	Policy          md.FXPolicy
	ExpectedVersion int64
}
type plan struct {
	Tenant, Actor string
	Document      document
}

func loadPlan(path, tenant, actor string) (plan, string, error) {
	for _, s := range []string{tenant, actor} {
		u, e := uuid.Parse(s)
		if e != nil || u.String() != s {
			return plan{}, "", errors.New("valid tenant and actor UUIDs are required")
		}
	}
	file, e := os.Open(path)
	if e != nil {
		return plan{}, "", errors.New("cannot open policy file")
	}
	defer func() { _ = file.Close() }()
	raw, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return plan{}, "", errors.New("policy file exceeds 1 MiB or cannot be read")
	}
	var doc document
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&doc); e != nil {
		return plan{}, "", errors.New("invalid policy document")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return plan{}, "", errors.New("policy file must contain exactly one document")
	}
	if md.ValidateFXPolicy(doc.Policy) != nil || (len(doc.Policy.Series) == 0 && doc.Policy.Official == nil && doc.Policy.Alpha == nil) || doc.ExpectedVersion < 0 || doc.ExpectedVersion == 1<<63-1 {
		return plan{}, "", errors.New("invalid reviewed FX series or expected version")
	}
	p := plan{tenant, actor, doc}
	canonical, e := json.Marshal(p)
	if e != nil {
		return plan{}, "", e
	}
	sum := sha256.Sum256(canonical)
	return p, hex.EncodeToString(sum[:]), nil
}
func run() error {
	tenant := flag.String("tenant", "", "tenant UUID")
	actor := flag.String("actor", "", "owner/admin UUID")
	file := flag.String("file", "", "reviewed JSON policy file")
	apply := flag.Bool("apply", false, "apply exactly the previewed plan; default is offline preview")
	expected := flag.String("plan-sha256", "", "SHA-256 returned by the offline preview")
	flag.Parse()
	p, digest, e := loadPlan(*file, *tenant, *actor)
	if e != nil {
		return e
	}
	if !*apply {
		officialFrom := ""
		if p.Document.Policy.Official != nil {
			officialFrom = p.Document.Policy.Official.From.Format(time.DateOnly)
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			PlanSHA256, Policy, Source, AccessScope string
			ExpectedVersion                         int64
			Series                                  int
			Notice                                  string
			OfficialFrom                            string `json:",omitempty"`
		}{digest, p.Document.Policy.Key, p.Document.Policy.Source, p.Document.Policy.AccessScope, p.Document.ExpectedVersion, len(p.Document.Policy.Series), "Preview only. Verify source rights, historical nominal, publication offsets, validity and reviewed intervals before applying. No points are fabricated or gaps filled.", officialFrom})
	}
	if *expected != digest {
		return errors.New("plan hash does not match tenant, actor, policy and expected version; preview again")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required for apply")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, e := postgres.NewPool(ctx, url)
	if e != nil {
		return errors.New("database configuration failed")
	}
	defer pool.Close()
	if e = pool.Ping(ctx); e != nil {
		return errors.New("database unavailable")
	}
	saved, e := postgres.NewMarketDataRepo(pool).RegisterFXPolicy(ctx, p.Tenant, p.Actor, p.Document.Policy, p.Document.ExpectedVersion)
	if e != nil {
		return errors.New("policy not applied: check owner/admin access, expected version, source and migrations")
	}
	if saved.Official != nil {
		fmt.Printf("Saved official CBR policy %s@%d; run investcbr to load and verify daily history\n", saved.Key, saved.Version)
	} else if saved.Alpha != nil {
		fmt.Printf("Saved retrospective Alpha FX policy %s@%d; load reviewed checkpoints separately; historical publication time is unknown\n", saved.Key, saved.Version)
	} else {
		fmt.Printf("Saved reviewed FX policy %s@%d; stored observations replayed atomically; coverage not inferred\n", saved.Key, saved.Version)
	}
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
