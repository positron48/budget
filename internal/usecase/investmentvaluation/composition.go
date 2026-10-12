package investmentvaluation

import (
	"math/big"
	"sort"
	"strings"

	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
)

type CompositionSegment struct {
	GroupID, Kind, CategoryKey string
	KnownValue                 *big.Rat
	Complete                   bool
	Weight                     *big.Rat
}
type Composition struct {
	SecuritiesKnown, CashKnown                 *big.Rat
	SecuritiesComplete, CashComplete, Complete bool
	Segments                                   []CompositionSegment
	Issues                                     []calc.QualityIssue
}
type CompositionDay struct {
	DailyNAV
	Composition *Composition
}
type CompositionHistory struct {
	Classification                     InstrumentGroups
	Currency, FXPolicyKey, BreakdownBy string
	Days                               []CompositionDay
}

func NormalizeGroupSelection(ids []string, groups InstrumentGroups) ([]string, error) {
	if len(ids) > 101 {
		return nil, d.ErrInvestmentInvalid
	}
	valid := map[string]bool{"": true}
	for _, g := range groups.Groups {
		valid[g.ID] = true
	}
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if !valid[id] {
			return nil, d.ErrInvestmentInvalid
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ComposeGroups classifies exact, date-specific converted components. It never
// infers group cash flows or inherits account returns. All groups in the pie are
// disjoint, while the selected securities use union semantics.
func ComposeGroups(result Result, groups InstrumentGroups, selected []string) (Composition, error) {
	return ComposeComposition(result, groups, selected, "group")
}
func NormalizeCompositionDimension(dimension string) (string, error) {
	if dimension == "" {
		dimension = "group"
	}
	switch dimension {
	case "group", "account", "currency", "type":
		return dimension, nil
	}
	return "", d.ErrInvestmentInvalid
}
func ComposeComposition(result Result, groups InstrumentGroups, selected []string, dimension string) (Composition, error) {
	dimension, e := NormalizeCompositionDimension(dimension)
	if e != nil {
		return Composition{}, e
	}

	out := Composition{SecuritiesKnown: new(big.Rat), CashKnown: new(big.Rat), SecuritiesComplete: true, CashComplete: true, Complete: result.Complete, Segments: []CompositionSegment{}, Issues: []calc.QualityIssue{}}
	selected, e = NormalizeGroupSelection(selected, groups)
	if e != nil {
		return out, e
	}
	if result.Components == nil || result.ComponentValues == nil {
		out.SecuritiesKnown = nil
		out.CashKnown = nil
		out.SecuritiesComplete = false
		out.CashComplete = false
		out.Complete = false
		out.Issues = append(out.Issues, calc.QualityIssue{Code: "component_breakdown_missing"})
		return out, nil
	}
	if len(result.Components) != len(result.ComponentValues) {
		return out, d.ErrInvestmentPrecondition
	}
	wanted := map[string]bool{}
	for _, id := range selected {
		wanted[id] = true
	}
	memberships := map[string]string{}
	for _, m := range groups.Memberships {
		memberships[m.LedgerID+"\x00"+m.InstrumentKey] = m.GroupID
	}
	segments := map[string]*CompositionSegment{}
	componentSegments := map[string]string{}
	selectedComponent := map[string]bool{}
	hasNegative := false
	for id, identity := range result.Components {
		kind := identity.Kind
		group := ""
		key := "cash"
		if kind == "position" {
			group = memberships[identity.LedgerID+"\x00"+identity.Key]
			category := group
			switch dimension {
			case "account":
				category = identity.LedgerID
			case "currency":
				category = identity.Currency
			case "type":
				category = identity.InstrumentType
			}
			if dimension != "group" && category == "" {
				category = "unknown"
			}
			key = "position:" + category
		} else if kind != "cash" {
			return out, d.ErrInvestmentPrecondition
		}
		componentSegments[id] = key
		segment := segments[key]
		if segment == nil {
			segmentGroup := group
			if dimension != "group" {
				segmentGroup = ""
			}
			segment = &CompositionSegment{GroupID: segmentGroup, CategoryKey: strings.TrimPrefix(key, "position:"), Kind: kind, KnownValue: new(big.Rat), Complete: true}
			segments[key] = segment
		}
		amount, exists := result.ComponentValues[id]
		known := exists && amount != nil
		chosen := kind == "position" && (len(wanted) == 0 || wanted[group])
		selectedComponent[id] = chosen
		if known {
			if amount.Sign() < 0 {
				hasNegative = true
			}
			segment.KnownValue.Add(segment.KnownValue, amount)
		} else {
			segment.Complete = false
			out.Complete = false
		}
		if kind == "cash" {
			if known {
				out.CashKnown.Add(out.CashKnown, amount)
			} else {
				out.CashComplete = false
			}
		}
		if chosen {
			if known {
				out.SecuritiesKnown.Add(out.SecuritiesKnown, amount)
			} else {
				out.SecuritiesComplete = false
			}
		}
	}
	// An unidentified ledger-level coverage gap affects every classification.
	for _, issue := range result.Issues {
		identity, identified := result.Components[issue.ComponentID]
		if identified {
			if selectedComponent[issue.ComponentID] {
				out.SecuritiesComplete = false
			}
			if identity.Kind == "cash" {
				out.CashComplete = false
			}
			key := componentSegments[issue.ComponentID]
			if segment := segments[key]; segment != nil {
				segment.Complete = false
			}
		}
		if !identified {
			out.SecuritiesComplete = false
			out.CashComplete = false
			for _, s := range segments {
				s.Complete = false
			}
		}
		if !identified || selectedComponent[issue.ComponentID] || identity.Kind == "cash" {
			out.Issues = append(out.Issues, issue)
		}
	}
	total := new(big.Rat)
	weights := out.Complete && !hasNegative
	keys := []string{}
	for key, s := range segments {
		keys = append(keys, key)
		total.Add(total, s.KnownValue)
		if !s.Complete || s.KnownValue.Sign() < 0 {
			weights = false
		}
	}
	sort.Strings(keys)
	weights = weights && total.Sign() > 0
	for _, key := range keys {
		s := segments[key]
		if weights {
			s.Weight = new(big.Rat).Quo(s.KnownValue, total)
		}
		out.Segments = append(out.Segments, *s)
	}
	return out, nil
}
