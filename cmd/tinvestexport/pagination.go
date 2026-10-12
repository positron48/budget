package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/positron48/budget/internal/adapter/tinvest"
)

type PageEvidence struct {
	Cursor      string `json:"cursor"`
	NextCursor  string `json:"next_cursor"`
	HasNext     bool   `json:"has_next"`
	Count       int    `json:"count"`
	ItemsSHA256 string `json:"items_sha256"`
}
type HistoryEvidence struct {
	Version  int            `json:"version"`
	Complete bool           `json:"complete"`
	Pages    []PageEvidence `json:"pages"`
}

func (h *HistoryEvidence) append(cursor string, page tinvest.OperationsPage) error {
	if h.Complete {
		return fmt.Errorf("history already completed")
	}
	expected := ""
	if len(h.Pages) > 0 {
		expected = h.Pages[len(h.Pages)-1].NextCursor
	}
	if cursor != expected {
		return fmt.Errorf("pagination chain mismatch")
	}
	if page.HasNext {
		if page.NextCursor == "" || page.NextCursor == cursor {
			return fmt.Errorf("invalid pagination cursor")
		}
		for _, previous := range h.Pages {
			if previous.Cursor == page.NextCursor {
				return fmt.Errorf("pagination cycle")
			}
		}
	}
	raw, err := json.Marshal(page.Items)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	h.Version = 1
	h.Pages = append(h.Pages, PageEvidence{Cursor: cursor, NextCursor: page.NextCursor, HasNext: page.HasNext, Count: len(page.Items), ItemsSHA256: hex.EncodeToString(hash[:])})
	h.Complete = !page.HasNext
	return nil
}

// verifyHistory checks stored page boundaries against the saved operations.
// It verifies evidence integrity, not an independent guarantee from the broker.
func verifyHistory(s Snapshot) error {
	h := s.HistoryEvidence
	if h.Version != 1 || !h.Complete || len(h.Pages) == 0 {
		return fmt.Errorf("completed pagination evidence required")
	}
	var reconstructed HistoryEvidence
	offset := 0
	for i, p := range h.Pages {
		if p.Count < 0 || p.Count > len(s.Operations)-offset {
			return fmt.Errorf("invalid page count")
		}
		items := s.Operations[offset : offset+p.Count]
		// Empty provider responses may encode items as null rather than [].
		if p.Count == 0 {
			emptyHash := sha256.Sum256([]byte("null"))
			if p.ItemsSHA256 == hex.EncodeToString(emptyHash[:]) {
				items = nil
			}
		}
		if err := reconstructed.append(p.Cursor, tinvest.OperationsPage{Items: items, HasNext: p.HasNext, NextCursor: p.NextCursor}); err != nil {
			return err
		}
		if reconstructed.Pages[i].ItemsSHA256 != p.ItemsSHA256 {
			return fmt.Errorf("page content checksum mismatch")
		}
		offset += p.Count
	}
	if offset != len(s.Operations) || !reconstructed.Complete {
		return fmt.Errorf("incomplete history evidence")
	}
	return nil
}
