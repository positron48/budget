// portfoliohistory imports reviewed raw native prices and historical mappings.
// It never imports operations, payouts, cash, FX, or ordinary Budget data.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/positron48/budget/internal/adapter/postgres"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func readInput(path, expected string) (postgres.HistoricalPriceBatch, error) {
	var batch postgres.HistoricalPriceBatch
	f, e := os.Open(path)
	if e != nil {
		return batch, errors.New("history input unavailable")
	}
	defer func() { _ = f.Close() }()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256<<20 {
		return batch, errors.New("private bounded history input required")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 256<<20))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&batch); e != nil {
		return batch, errors.New("invalid history input JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return batch, errors.New("multiple history input values")
	}
	if len(expected) != 64 || postgres.HistoricalPricesSHA256(batch) != expected {
		return batch, errors.New("history input checksum mismatch")
	}
	return batch, nil
}

func run() error {
	tenant := flag.String("tenant", "", "tenant UUID")
	actor := flag.String("actor", "", "owner/admin UUID for audit")
	input := flag.String("input", "", "private reviewed history JSON")
	checksum := flag.String("sha256", "", "canonical input SHA-256")
	prepare := flag.String("prepare-output", "", "freeze actual mapping versions to a new private file; no commit")
	replace := flag.Bool("replace-history", false, "prepare reviewed replacement of an existing historical schedule")
	apply := flag.Bool("apply", false, "commit reviewed input; default is full rollback rehearsal")
	flag.Parse()
	if *prepare != "" && *apply {
		return errors.New("prepare and apply are separate steps")
	}
	batch, e := readInput(*input, *checksum)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	pool, e := postgres.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if e != nil {
		return errors.New("history database configuration unavailable")
	}
	defer pool.Close()
	var version int
	var dirty bool
	if e = pool.DB.QueryRow(ctx, `SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty); e != nil || dirty || version < 62 {
		return errors.New("clean native-history schema required")
	}
	repo := postgres.NewMarketDataRepo(pool)
	if *prepare != "" {
		var role string
		if e = pool.DB.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, *tenant, *actor).Scan(&role); e != nil || role != "owner" && role != "admin" {
			return errors.New("history preparation requires owner/admin")
		}
		for i := range batch.Mappings {
			m, e := repo.GetMapping(ctx, *tenant, batch.Mappings[i].InstrumentKey)
			if errors.Is(e, md.ErrNotFound) {
				batch.Mappings[i].ExpectedVersion = 0
				continue
			}
			if e != nil {
				return errors.New("cannot read current mapping versions")
			}
			if len(m.HistoricalPeriods) > 0 && !*replace {
				return errors.New("existing historical schedule requires explicit reviewed replacement")
			}
			batch.Mappings[i].ExpectedVersion = m.Version
		}
		sha := postgres.HistoricalPricesSHA256(batch)
		proof, e := repo.ImportHistoricalPrices(ctx, *tenant, *actor, batch, sha, false)
		if e != nil {
			return errors.New("history preparation rehearsal failed; no commit confirmed")
		}
		raw, e := json.Marshal(batch)
		if e != nil {
			return e
		}
		f, e := os.OpenFile(*prepare, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return errors.New("prepared history output must be a new private file")
		}
		_, writeErr := f.Write(raw)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			return errors.New("prepared history output incomplete; do not apply")
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Status                   string
			BookWrites, MarketWrites bool
			InputSHA256              string
			Proof                    postgres.HistoricalPriceImport
		}{"history_input_prepared", false, false, sha, proof})
	}
	proof, e := repo.ImportHistoricalPrices(ctx, *tenant, *actor, batch, *checksum, *apply)
	if e != nil {
		return errors.New("history import did not confirm completion; retry the same reviewed input")
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		BookWrites, MarketWrites bool
		Proof                    postgres.HistoricalPriceImport
	}{false, proof.Applied, proof})
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
