// investmentconventions publishes a reviewed local price policy document. It
// does not infer calendars, fetch provider facts or change financial operations.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/positron48/budget/internal/adapter/postgres"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

func readReviewedProfile[T any](reader io.Reader) (T, error) {
	var profile T
	raw, e := io.ReadAll(io.LimitReader(reader, 4*1024*1024+1))
	if e != nil || len(raw) > 4*1024*1024 {
		return profile, errors.New("profile file cannot be read or exceeds 4 MiB")
	}
	// Reject unknown fields and trailing documents: a misspelled convention must
	// not silently become a zero value accepted under a different interpretation.
	var envelope map[string]json.RawMessage
	if e = json.Unmarshal(raw, &envelope); e != nil || envelope == nil {
		return profile, errors.New("invalid profile JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&profile); e != nil {
		return profile, errors.New("invalid profile schema")
	}
	return profile, nil
}
func readProfile(reader io.Reader) (nav.ConventionProfile, error) {
	return readReviewedProfile[nav.ConventionProfile](reader)
}

func run() error {
	kind := flag.String("kind", "price", "price or benchmark convention")
	tenant := flag.String("tenant", "", "tenant UUID")
	actor := flag.String("actor", "", "owner/admin user UUID")
	path := flag.String("file", "", "reviewed ConventionProfile JSON file")
	expected := flag.Int64("expected-version", -1, "current revision; 0 for a new profile")
	flag.Parse()
	if *path == "" || *expected < 0 {
		return errors.New("file and explicit expected-version are required")
	}
	file, e := os.Open(*path)
	if e != nil {
		return errors.New("cannot open profile file")
	}
	defer func() { _ = file.Close() }()
	var profile nav.ConventionProfile
	var benchmark nav.BenchmarkProfile
	switch *kind {
	case "price":
		profile, e = readProfile(file)
		if e == nil && nav.ValidateConventionProfile(*tenant, profile) != nil {
			e = errors.New("profile conventions are invalid")
		}
	case "benchmark":
		benchmark, e = readReviewedProfile[nav.BenchmarkProfile](file)
		if e == nil && !nav.ValidateBenchmarkProfile(benchmark) {
			e = errors.New("benchmark conventions are invalid")
		}
	default:
		return errors.New("kind must be price or benchmark")
	}
	if e != nil {
		return e
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, e := postgres.NewPool(ctx, url)
	if e != nil {
		return errors.New("database configuration failed")
	}
	defer db.Close()
	if *kind == "benchmark" {
		result, e := postgres.NewMarketDataRepo(db).RegisterBenchmark(ctx, *tenant, *actor, benchmark, *expected)
		if e != nil {
			return errors.New("benchmark not published; check membership, price profile, source bindings and expected revision")
		}
		fmt.Printf("Benchmark published at revision %d\n", result.Version)
		return nil
	}
	result, e := postgres.NewMarketDataRepo(db).RegisterPriceConventions(ctx, *tenant, *actor, profile, *expected)
	if e != nil {
		return errors.New("profile not published; check membership, source bindings and expected revision")
	}
	fmt.Printf("Price convention profile published at revision %d\n", result.Version)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
