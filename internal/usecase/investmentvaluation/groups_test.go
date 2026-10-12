package investmentvaluation

import (
	"github.com/google/uuid"
	"testing"
)

func TestGroupsAreAccountSpecificAndUnionDeduplicated(t *testing.T) {
	a, b, g, h := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	p := SaveInstrumentGroups{InstrumentGroups: InstrumentGroups{Groups: []InstrumentGroup{{g, " Strategy "}, {h, "Bonds"}}, Memberships: []GroupMembership{{a, "ABC", g}, {b, "ABC", h}}}, RequestKey: "create", Reason: "Classification"}
	got, e := NormalizeInstrumentGroups(p)
	if e != nil {
		t.Fatal(e)
	}
	positions := []GroupPosition{{a, "One", "ABC"}, {b, "Two", "ABC"}, {a, "One", "XYZ"}, {a, "One", "ABC"}}
	selected := SelectGroupPositions(positions, got.InstrumentGroups, []string{g, h, g})
	if len(selected) != 2 {
		t.Fatal(selected)
	}
	selected = SelectGroupPositions(positions, got.InstrumentGroups, []string{""})
	if len(selected) != 1 || selected[0].InstrumentKey != "XYZ" {
		t.Fatal(selected)
	}
	if len(SelectGroupPositions(positions, got.InstrumentGroups, nil)) != 3 {
		t.Fatal("duplicate positions")
	}
	p.Groups = p.Groups[:1]
	if _, e = NormalizeInstrumentGroups(p); e == nil {
		t.Fatal("dangling group accepted")
	}
}
func TestGroupsRejectAmbiguousMembershipAndNames(t *testing.T) {
	g, a := uuid.NewString(), uuid.NewString()
	base := SaveInstrumentGroups{InstrumentGroups: InstrumentGroups{Groups: []InstrumentGroup{{g, "Strategy"}}, Memberships: []GroupMembership{{a, "ABC", g}}}, RequestKey: "save", Reason: "review"}
	for _, change := range []func(*SaveInstrumentGroups){
		func(p *SaveInstrumentGroups) { p.Memberships = append(p.Memberships, p.Memberships[0]) },
		func(p *SaveInstrumentGroups) {
			p.Groups = append(p.Groups, InstrumentGroup{uuid.NewString(), " strategy "})
		},
		func(p *SaveInstrumentGroups) { p.Version = -1 }, func(p *SaveInstrumentGroups) { p.Reason = " " },
		func(p *SaveInstrumentGroups) { p.Groups = []InstrumentGroup{{g, "bad\nname"}} },
	} {
		p := base
		change(&p)
		if _, e := NormalizeInstrumentGroups(p); e == nil {
			t.Fatal("invalid classification accepted", p)
		}
	}
}
