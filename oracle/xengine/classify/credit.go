package classify

import (
	"cmp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Use is one coverage key (cov.Key.ID) of one card.
type Use struct{ Card, Key string }

// Uses counts the coverage keys the tape's entries lo..hi exercised: the
// cast/land key of each card a priority decision cast or played, and the
// ability key of each ability it activated or mana ability it fired (read
// from the entry's Mana records, so an auto-paid cast's taps count). With
// genesis set, the mana abilities fired before the first decision count too.
// Each trigger the entry put on the stack (Entry.Triggers, read from gorge's
// TriggerPush events) credits its f<face>/t<i> key; an ability pick or mana
// record credits f<face>/a<i> for the face it was activated on (Pick.Face,
// ManaRec.Face: 0 when absent). A key a card does not have (a mana ability of
// a gained kind, a granted trigger) is not credited. A trigger counts toward
// the window that holds the entry whose commit put it on the stack.
func Uses(reg *cards.Registry, t *game.Tape, lo, hi int, genesis bool) map[Use]int {
	out := map[Use]int{}
	add := func(name, want string) {
		c, ok := reg.Lookup(name)
		if !ok {
			return
		}
		ks := cov.Keys(c)
		if want == "" {
			want = ks[0].ID // cast, or land
		}
		for _, k := range ks {
			if k.ID == want {
				out[Use{name, want}]++
				return
			}
		}
	}
	// key is the f<face>/<kind><i> key a recorded source exercised.
	key := func(kind string, face, i int) string {
		return "f" + strconv.Itoa(face) + "/" + kind + strconv.Itoa(i)
	}
	mana := func(ms []game.ManaRec) {
		for _, m := range ms {
			if m.Ability >= 0 && m.Name != "" {
				add(cmp.Or(m.Card, m.Name), key("a", m.Face, m.Ability))
			}
		}
	}
	if genesis {
		mana(t.GenesisMana)
	}
	for i := max(lo, 0); i <= hi && i < len(t.Entries); i++ {
		e := t.Entries[i]
		if e.Kind == "priority" {
			for _, p := range e.Picks {
				name := oraclediff.RefName(p.Ref)
				if p.Ref == "" || isTokenOrAbility(p.Ref) {
					continue
				}
				switch p.Kind {
				case "cast", "play_land":
					add(name, "")
				case "ability":
					add(cmp.Or(p.Card, name), key("a", p.Face, p.Ability))
				}
			}
		}
		mana(e.Mana)
		for _, tr := range e.Triggers {
			add(tr.Name, key("t", tr.Face, tr.Index))
		}
	}
	return out
}

// isTokenOrAbility is true for a ref naming a token or a stack ability
// ("p1:token:Soldier", "p0:ability:Name"): not a card with keys of its own.
func isTokenOrAbility(ref string) bool {
	_, rest, _ := strings.Cut(ref, ":")
	return strings.HasPrefix(rest, "token:") || strings.HasPrefix(rest, "ability:")
}

// LoweredRange is the span of tape entries an item carries out: everything up
// to the first entry the lowering did not lower.
func LoweredRange(item lower.Lowered, t *game.Tape) (lo, hi int) {
	hi = len(t.Entries) - 1
	if item.Truncated != "" {
		hi = item.TruncatedAt - 1
	}
	return 0, hi
}

// Sorted lists the uses in card, key order.
func Sorted(m map[Use]int) []Use {
	out := make([]Use, 0, len(m))
	for u := range m {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Card != out[j].Card {
			return out[i].Card < out[j].Card
		}
		return out[i].Key < out[j].Key
	})
	return out
}
