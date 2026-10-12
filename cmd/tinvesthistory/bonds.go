package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type bondProvider interface {
	LoadBondFacts(context.Context, md.JobRequest) (md.BondFacts, error)
}
type bondManifest struct {
	SourceCoverageSHA256 string
	Plan                 plan
}
type bondReference struct {
	Request                  md.JobRequest
	SHA256                   string
	Accrued, Coupons, Events int
}
type bondIndex struct {
	ManifestSHA256 string
	Windows        []bondReference
}

func collectBonds(ctx context.Context, base, dir string, p plan, offline bool, factory func() (bondProvider, error), out io.Writer) error {
	var coverage coverageIndex
	if err := load(filepath.Join(base, "coverage.json"), &coverage); err != nil {
		return err
	}
	sourceHash, err := digest(coverage)
	if err != nil {
		return err
	}
	bonds := map[string]bool{}
	for _, item := range coverage.Instruments {
		if item.State == "observed" && item.Instrument.Kind == "bond" {
			bonds[item.Instrument.ID] = true
		}
	}
	filtered := p
	filtered.Entries = nil
	for _, e := range p.Entries {
		if bonds[e.InstrumentID] {
			filtered.Entries = append(filtered.Entries, e)
		}
	}
	if len(filtered.Entries) == 0 {
		return errors.New("no confirmed bond listings in history")
	}
	manifest := bondManifest{sourceHash, filtered}
	if !offline {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("bond evidence directory must be private and not a symlink")
	}
	var saved bondManifest
	path := filepath.Join(dir, "manifest.json")
	err = load(path, &saved)
	if errors.Is(err, os.ErrNotExist) && !offline {
		if err = publish(path, manifest); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !reflect.DeepEqual(saved, manifest) {
		return errors.New("bond source/range changed; use a new evidence directory")
	}
	hash, err := digest(manifest)
	if err != nil {
		return err
	}
	index := bondIndex{ManifestSHA256: hash}
	var client bondProvider
	for _, e := range filtered.Entries {
		count, accrued, coupons, events := 0, 0, 0, 0
		for _, q := range windows(e) {
			q.Kind = "bond_facts"
			hash, err := digest(q)
			if err != nil {
				return err
			}
			path = filepath.Join(dir, hash+".json")
			var facts md.BondFacts
			err = load(path, &facts)
			if errors.Is(err, os.ErrNotExist) && !offline {
				if client == nil {
					client, err = factory()
					if err != nil {
						return err
					}
				}
				facts, err = client.LoadBondFacts(ctx, q)
				if err != nil {
					return err
				}
				if facts.Instrument.ID != q.InstrumentID || !facts.From.Equal(q.From) || !facts.To.Equal(q.To) {
					return errors.New("bond response identity/range mismatch")
				}
				if facts, err = md.NormalizeBondFacts(facts); err != nil {
					return err
				}
				if err = publish(path, facts); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if facts.Instrument.ID != q.InstrumentID || !facts.From.Equal(q.From) || !facts.To.Equal(q.To) {
				return errors.New("bond checkpoint identity/range mismatch")
			}
			if _, err = md.NormalizeBondFacts(facts); err != nil {
				return err
			}
			hash, err = digest(facts)
			if err != nil {
				return err
			}
			index.Windows = append(index.Windows, bondReference{q, hash, len(facts.Accrued), len(facts.Coupons), len(facts.Events)})
			count++
			accrued += len(facts.Accrued)
			coupons += len(facts.Coupons)
			events += len(facts.Events)
		}
		if _, err = fmt.Fprintf(out, "instrument=%s bond_windows=%d accrued_rows=%d coupon_rows=%d event_rows=%d financial_terms_approved=false\n", e.InstrumentID, count, accrued, coupons, events); err != nil {
			return err
		}
	}
	path = filepath.Join(dir, "coverage.json")
	var old bondIndex
	err = load(path, &old)
	if errors.Is(err, os.ErrNotExist) && !offline {
		return publish(path, index)
	}
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(old, index) {
		return errors.New("bond evidence digest/coverage changed")
	}
	return nil
}
