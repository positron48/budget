package investmentvaluation

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
)

// Groups are the current analytical classification, never a journal event.
type InstrumentGroup struct{ ID, Name string }
type GroupMembership struct{ LedgerID, InstrumentKey, GroupID string }
type InstrumentGroups struct {
	Version     int64
	Groups      []InstrumentGroup
	Memberships []GroupMembership
}
type SaveInstrumentGroups struct {
	InstrumentGroups
	RequestKey, Reason string
}
type GroupPosition struct{ LedgerID, LedgerName, InstrumentKey string }

func groupText(s string, max int) bool {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func groupUUID(s string) bool { v, e := uuid.Parse(s); return e == nil && v.String() == s }
func NormalizeInstrumentGroups(p SaveInstrumentGroups) (SaveInstrumentGroups, error) {
	p.Reason = strings.TrimSpace(p.Reason)
	if p.Version < 0 || p.Version == math.MaxInt64 || !groupText(p.RequestKey, 128) || !groupText(p.Reason, 2000) || len(p.Groups) > 100 || len(p.Memberships) > 10000 {
		return p, d.ErrInvestmentInvalid
	}
	p.Groups = append([]InstrumentGroup{}, p.Groups...)
	p.Memberships = append([]GroupMembership{}, p.Memberships...)
	ids := map[string]bool{}
	names := map[string]bool{}
	for i, g := range p.Groups {
		g.Name = strings.TrimSpace(g.Name)
		key := strings.ToLower(g.Name)
		if !groupUUID(g.ID) || !groupText(g.Name, 100) || ids[g.ID] || names[key] {
			return p, d.ErrInvestmentInvalid
		}
		ids[g.ID] = true
		names[key] = true
		p.Groups[i] = g
	}
	positions := map[string]bool{}
	for _, m := range p.Memberships {
		key := m.LedgerID + "\x00" + m.InstrumentKey
		if !groupUUID(m.LedgerID) || !groupText(m.InstrumentKey, 200) || !ids[m.GroupID] || positions[key] {
			return p, d.ErrInvestmentInvalid
		}
		positions[key] = true
	}
	sort.Slice(p.Groups, func(i, j int) bool { return p.Groups[i].ID < p.Groups[j].ID })
	sort.Slice(p.Memberships, func(i, j int) bool {
		a, b := p.Memberships[i], p.Memberships[j]
		if a.LedgerID != b.LedgerID {
			return a.LedgerID < b.LedgerID
		}
		return a.InstrumentKey < b.InstrumentKey
	})
	return p, nil
}

// SelectGroupPositions uses union semantics. Empty selection means all securities;
// cash is intentionally outside this classifier. Empty group ID means ungrouped.
func SelectGroupPositions(positions []GroupPosition, groups InstrumentGroups, selected []string) []GroupPosition {
	memberships := map[string]string{}
	for _, m := range groups.Memberships {
		memberships[m.LedgerID+"\x00"+m.InstrumentKey] = m.GroupID
	}
	wanted := map[string]bool{}
	for _, id := range selected {
		wanted[id] = true
	}
	seen := map[string]bool{}
	out := []GroupPosition{}
	for _, p := range positions {
		key := p.LedgerID + "\x00" + p.InstrumentKey
		if !seen[key] && (len(wanted) == 0 || wanted[memberships[key]]) {
			out = append(out, p)
			seen[key] = true
		}
	}
	return out
}
