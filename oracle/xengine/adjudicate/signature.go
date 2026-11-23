package adjudicate

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
)

// SigInput is what a diverging item's signature is made of. It carries no
// card: the same engine difference on forty cards is one signature, which is
// the point (a resolved class clears every key it matches).
type SigInput struct {
	Pattern  string // the oraclediff three-way pattern, e.g. "FX|G"
	Field    string // FieldClass of the first non-empty GX/GF/FX field
	Family   string // cast, land, activate, trigger or none
	Desc     string // the coverage key's Desc (Mana, ChangeZone, Attacks ...) or "-"
	Cost     string // the activated ability's non-mana cost words, "+"-joined, or "-"
	Step     string // the Op of the diverging step, or "setup"
	Follower string // "x=<xmage class>,f=<forge class>"
}

// KeyDesc describes the first coverage key of a diverging step.
type KeyDesc struct{ Family, Desc, Cost string }

// NoKey is the KeyDesc of a diverging step that exercised no coverage key.
var NoKey = KeyDesc{Family: "none", Desc: "-", Cost: "-"}

// InputOf is the signature input of finding f, whose diverging step exercised
// the key k first. stepOp is the Op of that step ("setup" when it is the
// setup checkpoint). The card never enters it.
func InputOf(f Finding, k KeyDesc, stepOp string) SigInput {
	return SigInput{
		Pattern: f.Pattern, Field: f.FieldClassOf, Family: k.Family, Desc: k.Desc, Cost: k.Cost,
		Step: stepOp, Follower: "x=" + f.XClass + ",f=" + f.FClass,
	}
}

// KeySignature is the 12-hex card-independent signature of s.
func KeySignature(s SigInput) string {
	return sha12(strings.Join([]string{s.Pattern, s.Field, s.Family + ":" + s.Desc, s.Cost, s.Step, s.Follower}, "\x00"))
}

// Rebuild is the Finding Run built for row, from the same input: the register
// and keyproof read findings.jsonl rows (no values) and need the item and the
// diverging engine's finding again.
func Rebuild(in Input, row Row) (Finding, bool) {
	item, ok := in.Items[row.ID]
	if !ok {
		return Finding{}, false
	}
	div := in.XFind[row.ID]
	if div.Class != classify.ClassDiverge {
		div = in.FFind[row.ID]
	}
	f := Finding{Row: row, Item: item, Diverging: div, XVal: in.XFind[row.ID].Other, FVal: in.FFind[row.ID].Other}
	f.Steps = len(classify.Truncate(item, div).Steps)
	f.FieldClassOf = FieldClass(firstNonEmpty(row.GXField, row.GFField, row.FXField))
	return f, true
}
