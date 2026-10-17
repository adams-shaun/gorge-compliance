package templates

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Level B wave 5, class D (lb5-driver): rows that waited on an XMage driver op
// and the generator plumbing that emits it. Each row names the mechanism its
// item exercises; the item must be served, play through gorge, and carry the
// shape the driver op reads (a cast_mode, a "^X=" share, an offered compare,
// a setup answer, a craft activation). The census tests below hold each
// mechanism's corpus carriers in both directions, so a newly served or newly
// lost carrier fails by name.

func lb5Item(t *testing.T, name, key string) oraclegen.Item {
	t.Helper()
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		it, skip := GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
		}
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Fails) != 0 {
			t.Fatalf("%s %s does not play through gorge: ok=%v fails=%v", name, key, ok, res.Fails)
		}
		return it
	}
	t.Fatalf("%s carries no requirement %s", name, key)
	return oraclegen.Item{}
}

func stepCastModes(it oraclegen.Item) []string {
	var out []string
	for _, st := range it.Steps {
		if st.CastMode != "" {
			out = append(out, st.CastMode)
		}
	}
	return out
}

func hasXAnswer(it oraclegen.Item, kind, sub string) bool {
	for _, step := range it.XAnswers {
		for _, a := range step {
			if a.Kind == kind && strings.Contains(a.Value, sub) {
				return true
			}
		}
	}
	return false
}

func TestLB5DriverRowsServed(t *testing.T) {
	for _, tc := range []struct {
		card, key, mechanism string
		check                func(oraclegen.Item) bool
	}{
		{"Codie, Ravenous Codex", "trigger#0.0", "cast_mode prepared_copy",
			func(it oraclegen.Item) bool { return slices.Contains(stepCastModes(it), "prepared_copy") }},
		{"The Earth Crystal", "activate#0.0", "divided activation target answered as alias^X=share",
			func(it oraclegen.Item) bool { return hasXAnswer(it, "target", "p0:Grizzly Bears^X=2") }},
		{"Chandra, Flameshaper", "activate#0.2", "divided activation target answered as alias^X=share",
			func(it oraclegen.Item) bool { return hasXAnswer(it, "target", "^X=8") }},
		{"Raiding Schemes", "static#0.0", "cast_mode conspired (granted conspire)",
			func(it oraclegen.Item) bool { return slices.Contains(stepCastModes(it), "conspired") }},
		{"Silverquill, the Disputant", "static#0.0", "cast_mode casualty (granted casualty)",
			func(it oraclegen.Item) bool { return slices.Contains(stepCastModes(it), "casualty") }},
		{"Agent Maria Hill", "trigger#0.0", "cast_mode teamworked (teamwork tap)",
			func(it oraclegen.Item) bool { return slices.Contains(stepCastModes(it), "teamworked") }},
		{"Archangel of Tithes", "static#0.1", "paid-block checkpoint",
			func(it oraclegen.Item) bool { return len(it.Steps) == 2 && it.Steps[1].Op == "block" }},
		{"The Enigma Jewel", "static#1.0", "craft activation, then a gained mana ability",
			func(it oraclegen.Item) bool {
				return len(it.XAbility) == 3 && strings.HasPrefix(it.XAbility[0], "Craft with") && it.XAbility[2] == craftedGainedManaText
			}},
		{"Unstable Glyphbridge", "static#1.0", "opponent-turn attack, offered compare",
			func(it oraclegen.Item) bool {
				return slices.Contains(it.Compare, oraclediff.CompareOffered) && it.Steps[0].Op == "attack" && it.Steps[0].Seat == 1
			}},
		{"Etrata, Deadly Fugitive", "static#0.0", "granted ability activated on a face-down creature",
			func(it oraclegen.Item) bool {
				return slices.Contains(stepCastModes(it), "disguised") && len(it.XAbility) == 4 && it.XAbility[2] == "{2}{U}{B}"
			}},
		{"Avatar Aang", "trigger#1.0", "setup_answers decline of the turn-1 optional transform",
			func(it oraclegen.Item) bool { return hasXAnswer(it, "setup_choice", "no") && len(it.SetupAnswers) > 0 }},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			it := lb5Item(t, tc.card, tc.key)
			if !tc.check(it) {
				t.Fatalf("item does not carry the %s shape: steps=%+v xanswers=%+v xability=%v compare=%v",
					tc.mechanism, it.Steps, it.XAnswers, it.XAbility, it.Compare)
			}
		})
	}
}

// lb5Census returns the corpus statics st on face f of card c (with its
// requirement) that pick selects, and which of them GenerateB serves.
func lb5Census(t *testing.T, pick func(*cards.Card, *cards.Face, cards.Static, levelb.Requirement) bool) (all, served []string) {
	t.Helper()
	reg := loadGenRegistry(t)
	for _, c := range reg.AllCards() {
		if len(c.Faces) == 0 || strings.HasPrefix(c.Faces[0].Name, "A-") {
			continue
		}
		for _, req := range levelb.Requirements(c) {
			if req.Family != "static" || req.Face >= len(c.Faces) {
				continue
			}
			f := c.Faces[req.Face]
			i, err := strconv.Atoi(req.Slot)
			if err != nil || i < 0 || i >= len(f.Statics) {
				continue
			}
			st, _ := staticSlotOf(f, req)
			if !pick(c, f, st, req) {
				continue
			}
			row := c.Faces[0].Name + " " + req.Key
			all = append(all, row)
			if _, skip := GenerateB(reg, c.Faces[0].Name, req); skip == nil {
				served = append(served, row)
			}
		}
	}
	sort.Strings(all)
	sort.Strings(served)
	return all, served
}

// TestGrantedCopyCostCensus: every stack-scoped conspire/casualty grant in the
// corpus, and the ones the elected-copy-cost observation serves.
func TestGrantedCopyCostCensus(t *testing.T) {
	all, served := lb5Census(t, func(_ *cards.Card, _ *cards.Face, st cards.Static, _ levelb.Requirement) bool {
		_, ok := grantedCopyCostOf(st)
		return ok
	})
	// Ashad, the Lone Cyberman (casualty 2 on an artifact spell) and
	// Rassilon, the War President (conspire on a spell cast from exile) stay
	// unserved: Harmonize, the probe, is neither an artifact nor in exile.
	wantAll := []string{"Anhelo, the Painter static#0.0", "Ashad, the Lone Cyberman static#0.0", "Raiding Schemes static#0.0",
		"Rassilon, the War President static#0.0", "Silverquill, the Disputant static#0.0", "Wort, the Raidmother static#0.0"}
	wantServed := []string{"Anhelo, the Painter static#0.0", "Raiding Schemes static#0.0", "Silverquill, the Disputant static#0.0", "Wort, the Raidmother static#0.0"}
	if !slices.Equal(all, wantAll) || !slices.Equal(served, wantServed) {
		t.Fatalf("copy-cost grant census moved:\n carriers %v\n served %v\nwant\n carriers %v\n served %v", all, served, wantAll, wantServed)
	}
}

// TestFaceDownGrantCensus: every grant of an activated ability over face-down
// creatures you control, and the ones the face-down activation serves.
func TestFaceDownGrantCensus(t *testing.T) {
	all, served := lb5Census(t, func(_ *cards.Card, f *cards.Face, st cards.Static, _ levelb.Requirement) bool {
		a := st.ParamStr(cards.PKAffected)
		return st.HasParam(cards.PKAddAbility) && strings.Contains(a, "faceDown") && strings.Contains(a, "YouCtrl")
	})
	want := []string{"Etrata, Deadly Fugitive static#0.0"}
	if !slices.Equal(all, want) || !slices.Equal(served, want) {
		t.Fatalf("face-down grant census moved: carriers %v served %v, want both %v", all, served, want)
	}
}

// TestCraftedGainsCensus: every crafted back face that has the activated
// abilities of its craft materials, and the ones served.
func TestCraftedGainsCensus(t *testing.T) {
	all, served := lb5Census(t, func(c *cards.Card, _ *cards.Face, st cards.Static, req levelb.Requirement) bool {
		return craftedGainsShape(c, req, st)
	})
	want := []string{"The Enigma Jewel static#1.0"}
	if !slices.Equal(all, want) || !slices.Equal(served, want) {
		t.Fatalf("crafted-gains census moved: carriers %v served %v, want both %v", all, served, want)
	}
}
