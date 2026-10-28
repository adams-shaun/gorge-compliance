package lower

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// tokenCutIn plays the first n pairs of set (seeded 100+game) until a cut from
// turn 3 on that the gate accepts with at least one battlefield token, and
// returns a clone of the engine there. The pair index is logged.
func tokenCutIn(t *testing.T, reg *cards.Registry, set string, pairs [][2]decks.Deck) (*rules.Engine, int) {
	t.Helper()
	for g, p := range pairs {
		var got *rules.Engine
		var at int
		_, err := game.Play(reg, p, uint64(100+g), game.Options{Set: set, OnCut: func(e *rules.Engine, entry int) {
			if got != nil || e.G.Turn < 3 || len(battlefieldTokens(e.G)) == 0 {
				return
			}
			if len(capture(e, entry).Reasons) == 0 {
				got, at = e.Clone(), entry
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Logf("token cut: %s pair %d seed %d, entry %d, turn %d", set, g, 100+g, at, got.G.Turn)
			return got, at
		}
	}
	t.Fatalf("no expressible cut with a battlefield token in the first %d %s pairs", len(pairs), set)
	return nil, 0
}

func battlefieldTokens(g *state.Game) []*state.Object {
	var out []*state.Object
	for i := range g.Objs {
		if o := &g.Objs[i]; o.Zone == state.ZBattlefield && o.IsToken && o.Ability == nil {
			out = append(out, o)
		}
	}
	return out
}

// stemOf is the corpus stem of a token's card, found by scanning Game.Tokens
// (independent of the gate's reverse map).
func stemOf(g *state.Game, o *state.Object) string {
	var stems []string
	for k, c := range g.Tokens {
		if c == o.Card {
			stems = append(stems, k)
		}
	}
	sort.Strings(stems)
	if len(stems) == 0 {
		return ""
	}
	return stems[0]
}

// wantTokenRef is the runner ref (r.resolve's rule) of token o among seat's
// battlefield tokens: the face name, "#k" for the k-th (id order) token whose
// face name contains it, case-insensitively.
func wantTokenRef(g *state.Game, o *state.Object) string {
	name := strings.ToLower(o.Face().Name)
	k := 0
	for _, x := range battlefieldTokens(g) {
		if x.Controller != o.Controller {
			continue
		}
		if strings.Contains(strings.ToLower(x.Face().Name), name) {
			k++
		}
		if x == o {
			break
		}
	}
	ref := fmt.Sprintf("p%d:token:%s", o.Controller, o.Face().Name)
	if k > 1 {
		ref += fmt.Sprintf("#%d", k)
	}
	return ref
}

func TestGateTokens(t *testing.T) {
	reg, pairs := setPairs(t, "DSK", 40)
	e, at := tokenCutIn(t, reg, "DSK", pairs)
	g := e.G

	// (a) The cut as it is: no reasons, every seat's tokens listed in id order
	// with their full counter set, each bound as r.resolve would.
	// A counter on a token is part of its complete set.
	toks := battlefieldTokens(g)
	toks[0].Counters = []state.Counter{{Kind: "P1P1", N: 2}}
	check := func(label string) *cut {
		t.Helper()
		c := capture(e, at)
		if len(c.Reasons) != 0 {
			t.Fatalf("%s: refused: %v", label, c.Reasons)
		}
		for s := range c.Setup {
			var want []string
			for _, o := range battlefieldTokens(g) {
				if int(o.Controller) == s {
					want = append(want, stemOf(g, o))
				}
			}
			var got []string
			for _, tk := range c.Setup[s].Tokens {
				got = append(got, tk.Script)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: seat %d tokens %v, want %v", label, s, got, want)
			}
		}
		for _, o := range battlefieldTokens(g) {
			if got, want := c.Refs[o.ID], wantTokenRef(g, o); got != want {
				t.Errorf("%s: token %d (%s) ref %q, want %q", label, o.ID, o.Face().Name, got, want)
			}
		}
		return c
	}
	c := check("as played")
	first := toks[0]
	var counted bool
	for _, tk := range c.Setup[first.Controller].Tokens {
		if tk.Script == stemOf(g, first) && reflect.DeepEqual(tk.Counters, map[string]int32{"P1P1": 2}) {
			counted = true
		}
	}
	if !counted {
		t.Errorf("seat %d lists no %s token with P1P1 2: %+v", first.Controller, stemOf(g, first), c.Setup[first.Controller].Tokens)
	}
	// A token is not a card placement: it appears in no battlefield list, so
	// the lists hold exactly the seat's non-token permanents.
	for s := range c.Setup {
		var n int
		for _, o := range seatBattlefield(g, s) {
			if !o.IsToken {
				n++
			}
		}
		if got := len(c.Setup[s].Battlefield); got != n {
			t.Errorf("seat %d lists %d battlefield cards, want the %d non-token permanents", s, got, n)
		}
	}

	// (b) Same-name and containing names rank in id order: the planted tokens
	// are Soldier, Human Soldier, Soldier. The ref name "Soldier" is contained
	// in all three, so the third is "#3".
	stems := []string{"w_1_1_soldier", "w_1_1_human_soldier", "w_1_1_soldier"}
	for _, s := range stems {
		if g.Tokens[s] == nil {
			t.Fatalf("corpus has no token %s", s)
		}
	}
	active := int(g.Active)
	var plain []*state.Object
	for _, o := range seatBattlefield(g, active) {
		if o.Face() != nil && o.AttachedTo == 0 && len(o.Counters) == 0 && !o.IsToken && o.FaceIdx == 0 && o.ChosenColor == "" {
			plain = append(plain, o)
		}
	}
	if len(plain) < 3 {
		t.Fatalf("active seat has %d plain permanents, need 3", len(plain))
	}
	sort.Slice(plain, func(a, b int) bool { return plain[a].ID < plain[b].ID })
	for i, s := range stems {
		plain[i].SetCard(g.Tokens[s], 0)
		plain[i].IsToken = true
	}
	c = check("planted")
	third := plain[2]
	if got, want := c.Refs[third.ID], fmt.Sprintf("p%d:token:%s#3", active, third.Face().Name); got != want {
		t.Errorf("third Soldier ref %q, want %q", got, want)
	}

	// (c) A tapped token is a tapped_after_untap entry by its token ref.
	third.Tapped = true
	c = check("tapped")
	wantTap := strings.TrimPrefix(c.Refs[third.ID], fmt.Sprintf("p%d:", active))
	if !contains(c.Setup[active].TappedAfterUntap, wantTap) {
		t.Errorf("tapped_after_untap %v lacks %q", c.Setup[active].TappedAfterUntap, wantTap)
	}
	third.Tapped = false

	// (d) A copy, a stem-less card, a face-down token, a choice and a stem the
	// XMage driver cannot place are each refused with their own reason.
	refuse := func(label, reason string, plant func(), undo func()) {
		t.Helper()
		plant()
		defer undo()
		if c := capture(e, at); !hasReason(c, reason) {
			t.Errorf("%s: reasons %v, want %s", label, c.Reasons, reason)
		}
	}
	refuse("copy", reasonTokenUnscripted, func() { third.IsCopy = true }, func() { third.IsCopy = false })
	refuse("face down", reasonTokenUnscripted, func() { third.FaceDown = true }, func() { third.FaceDown = false })
	card := third.Card
	refuse("no stem", reasonTokenUnscripted, func() { c := *card; third.SetCard(&c, 0) }, func() { third.SetCard(card, 0) })
	refuse("colour choice", reasonTokenChoice, func() { third.ChosenColor = "Red" }, func() { third.ChosenColor = "" })
	refuse("unmapped", reasonTokenUnmapped+"w_1_1_soldier", func() { delete(xmageSetupTokens, "w_1_1_soldier") },
		func() { xmageSetupTokens["w_1_1_soldier"] = true })
	check("restored")
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// A scripted battlefield token at the cut is lowered as a setup token and the
// runner's rebuilt window agrees with the tape. The first 12 pairs of DSK
// alone carry no such window (its tokens come with Auras and exile links, the
// other gates), so the first 12 pairs of several sets are played.
func TestWindowsTokens(t *testing.T) {
	var tokenWin, tokenOK, refWin, refOK, nonTokenWin, nonTokenOK int
	for _, set := range []string{"DSK", "DFT", "EOE", "MKM", "TDM"} {
		reg, pairs := setPairs(t, set, 12)
		byStatus := map[string]int{}
		unmapped := map[string]int{}
		var refused int
		for g, p := range pairs {
			ls, rs, _, err := Windows(reg, p, uint64(100+g), game.Options{Set: set})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rs {
				refused++
				for _, why := range r.Reasons {
					if strings.HasPrefix(why, reasonTokenUnmapped) || why == reasonTokenUnscripted || why == reasonTokenChoice {
						unmapped[why]++
					}
				}
			}
			for _, l := range ls {
				var any bool
				for _, s := range l.Item.Setup {
					any = any || len(s.Tokens) > 0
				}
				c := CheckWindow(reg, l)
				if !any {
					nonTokenWin++
					if c.Status == StatusOK {
						nonTokenOK++
					}
					continue
				}
				// A step naming a token by its ref.
				steps, _ := json.Marshal(l.Item.Steps)
				byRef := bytes.Contains(steps, []byte(`token:`))
				tokenWin++
				byStatus[c.Status]++
				if byRef {
					refWin++
				}
				if c.Status == StatusOK {
					tokenOK++
					if byRef {
						refOK++
					}
					continue
				}
				// The other gaps of these windows are the lowering's own (an
				// enters-tapped land, a P/T an effect moves); a harness error is
				// a setup the runner could not build, e.g. a token it cannot bind.
				if c.Field == "harness" {
					t.Errorf("%s: harness: %s", l.Item.ID, c.Gorge)
				}
				t.Logf("%s: %s at step %d, %s: gorge %s, tape %s", l.Item.ID, c.Status, c.Step, c.Field, c.Gorge, c.Tape)
			}
		}
		var keys []string
		for k := range unmapped {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.Logf("%s: refusal %-48s %d", set, k, unmapped[k])
		}
		t.Logf("%s: refused %d; windows with a setup token by status %v", set, refused, byStatus)
	}
	t.Logf("windows with a setup token %d (lower_ok %d); of them naming a token in a step %d (lower_ok %d); token-free lowered windows %d (lower_ok %d)",
		tokenWin, tokenOK, refWin, refOK, nonTokenWin, nonTokenOK)
	if tokenOK == 0 {
		t.Fatal("no window with a setup token lowered to lower_ok")
	}
	// Without the bound refs (cut.remap returns "" for a pre-cut token) a window
	// is cut short at its first step naming one; measured 18 here, 2 unbound.
	if refOK < 10 {
		t.Fatalf("only %d lower_ok windows name a setup token in a step (want at least 10): the token refs are not bound", refOK)
	}
}

var setupTokenKey = regexp.MustCompile(`SETUP_TOKENS\.put\("([^"]+)"`)

// The Go set of stems the gate lets through is the Java driver's table.
func TestXMageSetupTokenTableMatchesDriver(t *testing.T) {
	src, err := os.ReadFile("../../xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	driver := map[string]bool{}
	for _, m := range setupTokenKey.FindAllStringSubmatch(string(src), -1) {
		driver[m[1]] = true
	}
	if len(driver) == 0 {
		t.Fatal("no SETUP_TOKENS.put entry found in the driver")
	}
	for k := range driver {
		if !xmageSetupTokens[k] {
			t.Errorf("driver maps %s, which xmageSetupTokens lacks", k)
		}
	}
	for k := range xmageSetupTokens {
		if !driver[k] {
			t.Errorf("xmageSetupTokens lists %s, which the driver does not map", k)
		}
	}
}
