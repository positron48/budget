package investmentimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const MaxBrokerSnapshotBytes = 32 << 20

type BrokerSourceRow struct {
	Identity, OperationID, AccountID, State, Type, SHA256 string
	Raw                                                   json.RawMessage
}
type BrokerSnapshot struct {
	SHA256, AccountID, AccountType string
	From, To                       time.Time
	StartsAtAccountOpening         bool
	Original                       []byte
	Rows                           []BrokerSourceRow
}

func brokerHash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

// PrepareBrokerSnapshot preserves every provider record, including cancelled
// orders and unrecognized types. This stages facts; it does not post operations
// or assert that the provider exposes every economic event.
func PrepareBrokerSnapshot(raw []byte) (BrokerSnapshot, error) {
	var result BrokerSnapshot
	fail := func(message string) (BrokerSnapshot, error) {
		return BrokerSnapshot{}, fmt.Errorf("invalid broker snapshot: %s", message)
	}
	if len(raw) == 0 || len(raw) > MaxBrokerSnapshotBytes {
		return fail("size limit")
	}
	var input struct {
		From, To time.Time
		Account  struct {
			ID, Type   string
			OpenedDate time.Time
		}
		Operations []json.RawMessage
		Evidence   struct {
			Version  int
			Complete bool
			Pages    []struct {
				Cursor      string
				NextCursor  string `json:"next_cursor"`
				HasNext     bool   `json:"has_next"`
				Count       int
				ItemsSHA256 string `json:"items_sha256"`
			}
		} `json:"history_evidence"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fail("malformed JSON")
	}
	identity := func(s string) bool {
		return len(s) > 0 && len(s) <= 200 && strings.TrimSpace(s) == s && !strings.ContainsAny(s, ":\r\n\t")
	}
	if !identity(input.Account.ID) || (input.Account.Type != "ACCOUNT_TYPE_TINKOFF" && input.Account.Type != "ACCOUNT_TYPE_TINKOFF_IIS") {
		return fail("account identity")
	}
	if input.From.IsZero() || input.To.Before(input.From) || input.Account.OpenedDate.IsZero() || input.From.Before(input.Account.OpenedDate) {
		return fail("history interval")
	}
	if input.Evidence.Version != 1 || !input.Evidence.Complete || len(input.Evidence.Pages) == 0 || len(input.Evidence.Pages) > 10000 || len(input.Operations) > 100000 {
		return fail("completed page evidence required")
	}
	offset := 0
	cursor := ""
	seenCursors := map[string]bool{}
	for i, p := range input.Evidence.Pages {
		if p.Cursor != cursor || p.Count < 0 || p.Count > len(input.Operations)-offset || p.HasNext != (i < len(input.Evidence.Pages)-1) {
			return fail("page chain")
		}
		if p.HasNext && (p.NextCursor == "" || p.NextCursor == p.Cursor || seenCursors[p.NextCursor]) {
			return fail("page cycle")
		}
		seenCursors[p.Cursor] = true
		cursor = p.NextCursor
		items := input.Operations[offset : offset+p.Count]
		if p.Count == 0 && p.ItemsSHA256 == brokerHash([]byte("null")) {
			items = nil
		}
		compact, err := json.Marshal(items)
		if err != nil || brokerHash(compact) != p.ItemsSHA256 {
			return fail("page checksum")
		}
		offset += p.Count
	}
	if offset != len(input.Operations) {
		return fail("uncovered operations")
	}
	seen := map[string]bool{}
	for _, value := range input.Operations {
		var op struct {
			ID          string
			AccountID   string `json:"brokerAccountId"`
			State, Type string
			Date        time.Time
		}
		if err := json.Unmarshal(value, &op); err != nil || !identity(op.ID) || op.AccountID != input.Account.ID || op.State == "" || op.Type == "" || op.Date.IsZero() || op.Date.Before(input.From) || op.Date.After(input.To) || seen[op.ID] {
			return fail("operation identity, scope or interval")
		}
		seen[op.ID] = true
		var compact bytes.Buffer
		if err := json.Compact(&compact, value); err != nil {
			return fail("operation JSON")
		}
		// Match exporter escaping for stable content hashes across indentation.
		normalized, err := json.Marshal(json.RawMessage(compact.Bytes()))
		if err != nil {
			return fail("operation JSON")
		}
		result.Rows = append(result.Rows, BrokerSourceRow{Identity: "tinvest:" + op.AccountID + ":" + op.ID, OperationID: op.ID, AccountID: op.AccountID, State: op.State, Type: op.Type, SHA256: brokerHash(normalized), Raw: append(json.RawMessage(nil), value...)})
	}
	result.Original = append([]byte(nil), raw...)
	result.SHA256 = brokerHash(raw)
	result.AccountID = input.Account.ID
	result.AccountType = input.Account.Type
	result.From, result.To = input.From, input.To
	result.StartsAtAccountOpening = input.From.Equal(input.Account.OpenedDate)
	return result, nil
}
