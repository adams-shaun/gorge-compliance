package xprep

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const manifestDir = "../../compliance/manifests"

type window struct {
	l lower.Lowered
	g rules.OracleResult
}

// okWindows plays the first n FDN games (the xengine window tests' pairs and
// seeds) and returns every window whose lowering gorge's own runner
// reproduced, with gorge's result for it.
func okWindows(t *testing.T, n int) (*cards.Registry, *decks.Spellings, []window) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []window
	for g := 0; g < n; g++ {
		pair := decks.Pair(cands, cov.New(), "FDN", 0, g, 7)
		ls, _, _, err := lower.Windows(reg, pair, uint64(100+g), game.Options{Set: "FDN"})
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range ls {
			if c := lower.CheckWindow(reg, l); c.Status == lower.StatusOK {
				out = append(out, window{l, c.Result})
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no lower_ok window")
	}
	return reg, sp, out
}

// prepared is every ok window Prepare serves; a refusal is logged and counted.
func prepared(t *testing.T, n int) (*cards.Registry, *decks.Spellings, []window, []oraclegen.Item) {
	t.Helper()
	reg, sp, ws := okWindows(t, n)
	var served []window
	var items []oraclegen.Item
	reasons := map[string]int{}
	for _, w := range ws {
		it, ref := Prepare(reg, sp, w.l, w.g)
		if ref != nil {
			if !slices.Contains(Reasons, ref.Reason) {
				t.Errorf("%s: reason %q is not in the closed list", ref.ID, ref.Reason)
			}
			reasons[ref.Reason]++
			continue
		}
		served = append(served, w)
		items = append(items, it)
	}
	t.Logf("%d ok windows, %d prepared, refused by reason %v", len(ws), len(served), reasons)
	if len(served) == 0 {
		t.Fatal("no window was prepared")
	}
	return reg, sp, served, items
}

// TestPrepareFillsAnswers: every prepared item has one answer list per step,
// and the answers scripted are gorge's own decisions less the declarations (the
// attack and block steps carry those) and less the target decisions
// ChooseTargets moved onto a cast or activate step.
func TestPrepareFillsAnswers(t *testing.T) {
	_, _, ws, items := prepared(t, 2)
	var moved, scripted int
	for k, it := range items {
		g := ws[k].g
		if len(it.XAnswers) != len(it.Steps) {
			t.Errorf("%s: %d xmage_answers for %d steps", it.ID, len(it.XAnswers), len(it.Steps))
			continue
		}
		_, castSteps := oraclegen.ChooseTargets(ws[k].l.Item.Scenario, g.Decisions)
		want := 0
		for _, d := range g.Decisions {
			switch {
			case d.Kind == "attackers" || d.Kind == "blockers":
			case d.Via == "target" && castSteps[d.Step]:
				moved++
			default:
				want++
			}
		}
		got := 0
		for _, a := range it.XAnswers {
			got += len(a)
		}
		scripted += got
		if got != want {
			t.Errorf("%s: %d scripted answers, want %d (decisions %d)", it.ID, got, want, len(g.Decisions))
		}
	}
	// The sample must hold both a moved target and a scripted answer, or the
	// count proves nothing.
	if moved == 0 || scripted == 0 {
		t.Errorf("sample has %d moved targets and %d scripted answers", moved, scripted)
	}
}

// TestPrepareAbilityPrefixes: every activate step of a prepared item has a
// prefix, and no other step has one; an activation XMage has no text for (a
// token's ability, a card the corpus does not know) is a refusal, not a blank.
func TestPrepareAbilityPrefixes(t *testing.T) {
	reg, sp, ws, items := prepared(t, 2)
	acts := 0
	for _, it := range items {
		for i, st := range it.Steps {
			if st.Op != "activate" {
				if len(it.XAbility) > i && it.XAbility[i] != "" {
					t.Errorf("%s step %d (%s) has prefix %q", it.ID, i, st.Op, it.XAbility[i])
				}
				continue
			}
			acts++
			if len(it.XAbility) != len(it.Steps) || it.XAbility[i] == "" {
				t.Errorf("%s step %d activates %s with no xmage_ability", it.ID, i, st.Card)
			}
		}
	}
	if acts == 0 {
		t.Fatal("sample has no activate step")
	}
	for k, w := range ws {
		l := w.l
		l.Item.Steps = slices.Clone(l.Item.Steps)
		for i, st := range l.Item.Steps {
			if st.Op != "activate" {
				continue
			}
			for _, card := range []string{"p0:token:Treasure", "p0:No Such Card"} {
				l.Item.Steps[i].Card = card
				if _, ref := Prepare(reg, sp, l, w.g); ref == nil || ref.Reason != ReasonAbilityUnknown {
					t.Errorf("%s: activating %q is not refused %s (%v)", l.Item.ID, card, ReasonAbilityUnknown, ref)
				}
			}
			l.Item.Steps[i].Card = st.Card
			l.Item.Steps[i].AbilityIndex = nil
			if _, ref := Prepare(reg, sp, l, w.g); ref == nil || ref.Reason != ReasonAbilityUnknown {
				t.Errorf("%s: an activation with no ability index is not refused (%v)", l.Item.ID, ref)
			}
			break
		}
		if k >= 2 {
			break
		}
	}
}

// TestPrepareRefusesSpelling: an item holding a card XMage spells differently
// is refused, whichever zone it sits in; the same item without it is served.
func TestPrepareRefusesSpelling(t *testing.T) {
	reg, sp, ws, _ := prepared(t, 1)
	var odd []string
	for _, name := range []string{"Lim-D\u00fbl's Vault", "Fire"} {
		if sp.Differs(reg, name) {
			odd = append(odd, name)
		}
	}
	if len(odd) == 0 {
		t.Fatal("no known card with a differing XMage spelling")
	}
	w := ws[0]
	for _, name := range odd {
		l := w.l
		l.Item.Setup = map[string]oraclegen.Seat{}
		for k, s := range w.l.Item.Setup {
			l.Item.Setup[k] = s
		}
		s := l.Item.Setup["p1"]
		s.Graveyard = append(slices.Clone(s.Graveyard), name)
		l.Item.Setup["p1"] = s
		_, ref := Prepare(reg, sp, l, w.g)
		if ref == nil || ref.Reason != ReasonSpelling || !strings.Contains(ref.Detail, name) {
			t.Errorf("%s in a graveyard: got %+v, want %s", name, ref, ReasonSpelling)
		}
	}
	if _, ref := Prepare(reg, sp, w.l, w.g); ref != nil {
		t.Errorf("control item refused: %+v", ref)
	}
}
