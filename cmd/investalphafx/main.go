// investalphafx previews reviewed local FX checkpoints offline, or publishes
// them atomically under an already registered personal retrospective policy.
// It never reads Keychain or calls a provider. Default mode performs no writes.
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
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	av "github.com/positron48/budget/internal/adapter/alphavantage"
	"github.com/positron48/budget/internal/adapter/postgres"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type document struct {
	Policy  md.FXPolicy
	Version int64
}
type plan struct {
	Tenant   string
	Document document
	Audits   []av.Audit
}

func load(path string, v any) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 8<<20 {
		return errors.New("input must be a private regular file up to 8 MiB")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return errors.New("cannot read input")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(v) != nil {
		return errors.New("invalid input document")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("trailing input data")
	}
	return nil
}
func loadPlan(dir, file, tenant string) (plan, string, int, error) {
	p := plan{Tenant: tenant}
	if id, err := uuid.Parse(tenant); err != nil || id.String() != tenant {
		return p, "", 0, md.ErrInvalid
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return p, "", 0, errors.New("checkpoint directory must be private and not a symlink")
	}
	if err = load(file, &p.Document); err != nil {
		return p, "", 0, err
	}
	if p.Document.Version < 1 || p.Document.Policy.Alpha == nil || md.ValidateFXPolicy(p.Document.Policy) != nil {
		return p, "", 0, md.ErrInvalid
	}
	var manifest struct{ Source, From, To string }
	if err = load(filepath.Join(dir, "manifest.json"), &manifest); err != nil {
		return p, "", 0, err
	}
	if manifest.Source != "alphavantage" {
		return p, "", 0, md.ErrInvalid
	}
	count := 0
	for _, pair := range []string{"USD-RUB", "EUR-RUB"} {
		var a av.Audit
		if err = load(filepath.Join(dir, pair+".json"), &a); err != nil {
			return p, "", 0, err
		}
		if a.Source != manifest.Source || a.From != manifest.From || a.To != manifest.To || a.Pair != pair[:3]+"/"+pair[4:] || a.RetrievedAt.After(time.Now().UTC()) {
			return p, "", 0, md.ErrInvalid
		}
		points, e := av.RetrospectivePoints(p.Document.Policy, a)
		if e != nil {
			return p, "", 0, e
		}
		count += len(points)
		p.Audits = append(p.Audits, a)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return p, "", 0, err
	}
	sum := sha256.Sum256(raw)
	return p, hex.EncodeToString(sum[:]), count, nil
}
func run(args []string) error {
	flags := flag.NewFlagSet("investalphafx", flag.ContinueOnError)
	dir := flags.String("checkpoints", "", "private alphafxcheck directory")
	file := flags.String("policy-file", "", "private reviewed JSON {Policy,Version}; policy must already be registered for apply")
	tenant := flags.String("tenant", "", "tenant UUID")
	apply := flags.Bool("apply", false, "publish the exact previewed plan under the registered policy")
	expected := flags.String("plan-sha256", "", "digest from offline preview")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return md.ErrInvalid
	}
	p, digest, count, err := loadPlan(*dir, *file, *tenant)
	if err != nil {
		return err
	}
	if !*apply {
		return json.NewEncoder(os.Stdout).Encode(struct {
			PlanSHA256, Policy, Convention                              string
			Version                                                     int64
			Points                                                      int
			SourceWrites, DatabaseWrites, HistoricalPublicationVerified bool
		}{digest, p.Document.Policy.Key, md.AlphaRetrospectiveConvention, p.Document.Version, count, false, false, false})
	}
	if *expected != digest {
		return errors.New("plan changed; preview tenant, policy and checkpoints again")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required for apply")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		return errors.New("database configuration failed")
	}
	defer pool.Close()
	repo := postgres.NewMarketDataRepo(pool)
	saved, _, err := repo.ReadFXSnapshot(ctx, p.Tenant, p.Document.Policy.Key, time.Now(), time.Time{})
	if err != nil {
		return errors.New("registered policy unavailable")
	}
	want, _ := json.Marshal(p.Document.Policy)
	got, _ := json.Marshal(saved.FXPolicy)
	if saved.Version != p.Document.Version || !bytes.Equal(want, got) {
		return errors.New("registered policy differs from preview")
	}
	n, err := repo.SaveAlphaFX(ctx, p.Tenant, saved.Key, saved.Version, p.Audits)
	if err != nil {
		return errors.New("FX publication failed; no checkpoint changes committed")
	}
	fmt.Printf("Retrospective Alpha FX published: points=%d policy=%s@%d; historical publication time remains unknown\n", n, saved.Key, saved.Version)
	return nil
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
