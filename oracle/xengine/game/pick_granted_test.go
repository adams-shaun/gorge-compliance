package game

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/decision"
)

// TestPickOfCarriesGrantedIdentity: a granted/gained/keyword-granted option's
// anchors (decision.Option's SVar, GrantSource, GainedSource, GainedIdx,
// Keyword) must reach the tape pick beside its Label, or the lowering cannot
// tell a granted ability's pick (whose Ability aliases printed index 0, or is
// -1 for a keyword grant) from the card's printed ability 0.
func TestPickOfCarriesGrantedIdentity(t *testing.T) {
	r := &recorder{}
	granted := decision.Option{Index: 3, Kind: "ability", Obj: 9, Ability: 0,
		Label: "Dalakos, Crafter of Wonders: CARDNAME gets +1/+0 until end of turn.",
		SVar:  "Pump", GrantSource: 4, GainedSource: 0, GainedIdx: 0}
	p := r.pickOf(nil, granted)
	if p.SVar != "Pump" || p.GrantSource != 4 || p.Label != granted.Label {
		t.Errorf("granted pick = %+v, want SVar Pump, GrantSource 4, the label", p)
	}
	keyword := decision.Option{Index: 4, Kind: "ability", Obj: 9, Ability: -1,
		Label: "Homura Pathbreaker: Cycling {2}", Keyword: "Cycling:2"}
	p = r.pickOf(nil, keyword)
	if p.Keyword != "Cycling:2" || p.Ability != -1 || p.SVar != "" {
		t.Errorf("keyword pick = %+v, want Keyword, Ability -1, no SVar", p)
	}
	gained := decision.Option{Index: 5, Kind: "ability", Obj: 9, Ability: 0,
		Label: "X: Y", GainedSource: 7, GainedIdx: 2}
	p = r.pickOf(nil, gained)
	if p.GainedSource != 7 || p.GainedIdx != 2 {
		t.Errorf("gained pick = %+v, want GainedSource 7, GainedIdx 2", p)
	}
	printed := decision.Option{Index: 6, Kind: "ability", Obj: 9, Ability: 0, Label: "X: Z"}
	p = r.pickOf(nil, printed)
	if p.SVar != "" || p.Keyword != "" || p.GrantSource != 0 || p.GainedSource != 0 || p.GainedIdx != 0 {
		t.Errorf("printed pick = %+v, want no grant identity", p)
	}
}
