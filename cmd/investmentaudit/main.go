// investmentaudit inspects a local export without database or network access.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/positron48/budget/internal/usecase/investmentimport"
)

func run() error {
	file := flag.String("file", "", "local Snowball CSV (read only)")
	stagePath := flag.String("stage", "", "write a private lossless staging JSON; never posts financial operations")
	previewPath := flag.String("preview", "", "write private mapping preview JSON; never posts financial operations")
	mappingPath := flag.String("mapping", "", "explicit financial mapping JSON for this source checksum")
	financialPath := flag.String("financial-preview", "", "write private financial review candidates; never posts")
	resolutionPath := flag.String("resolution-report", "", "private read-only review requirements for every source row; requires financial mapping")
	replayPath := flag.String("candidate-replay", "", "write private replay diagnostics for prepared candidates; does not prove complete history")
	combined := flag.Bool("combined", false, "accept one combined unassigned portfolio")
	flag.Parse()
	if *resolutionPath != "" && (*mappingPath == "" || *financialPath == "") {
		return fmt.Errorf("-resolution-report requires financial review")
	}
	if *replayPath != "" && (*mappingPath == "" || *financialPath == "") {
		return fmt.Errorf("-candidate-replay requires financial review")
	}
	if *file == "" {
		return fmt.Errorf("-file is required")
	}
	f, err := os.Open(*file)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if *mappingPath != "" || *financialPath != "" {
		if *mappingPath == "" || *financialPath == "" || !*combined || *stagePath != "" || *previewPath != "" {
			return fmt.Errorf("financial review requires -combined -mapping -financial-preview, without -stage or -preview")
		}
		mf, err := os.Open(*mappingPath)
		if err != nil {
			return err
		}
		defer func() { _ = mf.Close() }()
		decoder := json.NewDecoder(io.LimitReader(mf, investmentimport.MaxSourceBytes+1))
		decoder.DisallowUnknownFields()
		var mapping investmentimport.FinancialMapping
		if err = decoder.Decode(&mapping); err != nil {
			return fmt.Errorf("invalid financial mapping JSON")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return fmt.Errorf("unexpected trailing mapping data")
		}
		report, err := investmentimport.MapSnowballFinancial(f, mapping)
		if err != nil {
			return err
		}
		if err = writePrivateJSON(*financialPath, report); err != nil {
			return err
		}
		if *resolutionPath != "" {
			if _, err = f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			resolution, reviewErr := investmentimport.ReviewSourceResolution(f, mapping)
			if reviewErr != nil {
				return reviewErr
			}
			if err = writePrivateJSON(*resolutionPath, resolution); err != nil {
				return err
			}
			fmt.Printf("Source resolution: %d rows, %d prepared, %d require review; suggestions are not approved mappings\n", resolution.Rows, resolution.Prepared, resolution.Unresolved)
		}
		if *replayPath != "" {
			replay := replayCandidates(report)
			if err = writePrivateJSON(*replayPath, replay); err != nil {
				return err
			}
			passed, failed := 0, 0
			for _, ledger := range replay.Ledgers {
				if ledger.State == "candidate_replay_passed" {
					passed++
				}
				if ledger.State == "replay_failed" {
					failed++
				}
			}
			fmt.Printf("Candidate replay: %d passed, %d failed; unresolved source rows: %d; not a completeness check\n", passed, failed, report.Unresolved)
		}
		fmt.Printf("Financial review: %d candidates, %d unresolved; hash=%s; no operations posted\n", report.Ready, report.Unresolved, report.Hash)
		return nil
	}
	if *stagePath != "" && *previewPath != "" {
		return fmt.Errorf("choose either -stage or -preview")
	}
	if *previewPath != "" {
		preview, err := investmentimport.PreviewSnowball(f, *combined)
		if err != nil {
			return err
		}
		if err := writePrivateJSON(*previewPath, preview); err != nil {
			return err
		}
		fmt.Printf("Previewed %d rows; state=%s; hash=%s\n", len(preview.Rows), preview.State, preview.Hash)
		return nil
	}
	if *stagePath != "" {
		stage, err := investmentimport.StageSnowball(f, *combined)
		if err != nil {
			return err
		}
		if err := writePrivateJSON(*stagePath, stage); err != nil {
			return err
		}
		fmt.Printf("Staged %d source rows; scope=%s; state=%s; sha256=%s\n", len(stage.Rows), stage.Scope, stage.State, stage.Audit.SHA256)
		return nil
	}
	a, err := investmentimport.AuditSnowball(f)
	if err != nil {
		return err
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(a); err != nil {
		return err
	}
	if len(a.Issues) > 0 {
		return fmt.Errorf("source has %d syntax issues", len(a.Issues))
	}
	return nil
}

func writePrivateJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := out.Write(data)
	closeErr := out.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Investment source audit:", err)
		os.Exit(1)
	}
}
