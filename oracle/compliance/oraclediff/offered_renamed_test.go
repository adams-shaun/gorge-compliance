package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// An offer's source is labelled by the permanent's current name: Humble Work
// enchants p1's Wall of Stone and names it "Humble Merchant", and both engines
// offer the same permanent's {T} ability -- gorge by its setup ref, XMage by
// the current name.
func TestOfferedRenamedPermanentSource(t *testing.T) {
	g := rules.OracleSnapshot{Permanents: []rules.OracleSnapPerm{
		{Ref: "p1:Wall of Stone", Name: "Humble Merchant", Controller: 1, Owner: 1},
	}, Offered: []rules.OracleSnapOffered{
		{Source: "p1:Wall of Stone", Kind: "activate", Label: "{T}: Add {C}"},
	}}
	x := rules.OracleSnapshot{Permanents: []rules.OracleSnapPerm{
		{Ref: "p1:Humble Merchant", Name: "Humble Merchant", Controller: 1, Owner: 1},
	}, Offered: []rules.OracleSnapOffered{
		{Source: "p1:Humble Merchant", Kind: "activate", Label: "Humble Merchant: Add {C}"},
	}}
	if v, ok := compareSnap(g, x, []string{CompareOffered}, nil); !ok {
		t.Fatalf("gorge %+v, xmage %+v: %s / %s", g.Offered, x.Offered, v.Field, v.Gorge)
	}
	if got := offeredKeys(g.Offered, g.Permanents); got != "p1:Humble Merchant|activate|" {
		t.Fatalf("relabelled key %q", got)
	}
	// a token's offer keeps its ref: tokens are named by their engine's
	// spelling, which tokens compare by characteristics, not by name.
	tok := rules.OracleSnapshot{Permanents: []rules.OracleSnapPerm{
		{Ref: "p1:Dragon", Name: "Dragon", Controller: 1, Owner: 1, Token: true},
	}, Offered: []rules.OracleSnapOffered{
		{Source: "p1:Dragon", Kind: "activate"},
	}}
	if got := offeredKeys(tok.Offered, tok.Permanents); got != "p1:Dragon|activate|" {
		t.Fatalf("token key %q", got)
	}
}
