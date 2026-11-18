// The same-name census reads each option ref's SETUP name, but XMage matches a
// target or choice answer by the object's CURRENT name. A copy or becomes
// effect (Fleeting Reflection: "target creature you control becomes a copy of
// target creature an opponent controls until end of turn") gives two objects
// one current name while their scenario refs keep two setup names, so the
// census saw one distinct "Magebane Lizard", emitted the bare name, and XMage
// bound the first permanent currently NAMED that -- the copy -- instead of the
// recorded one (CR 601.2c: the player announces the choice of an appropriate
// object for each target).
//
// liveNamedSameNames restates such an option under the name XMage matches it
// by, once, before any answer is spelled, so ClassifySameName and every
// spelling helper that reads the decision (alias, copy marker, withheld) see
// the collision without each growing a names parameter.
package oraclegen

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// refWithName is ref with its name part replaced by name, keeping the seat
// prefix and the token marker, and dropping any "#rank" (a rank counts objects
// of the old name).
func refWithName(ref, name string) string {
	prefix := ""
	rest := ref
	if i := strings.IndexByte(rest, ':'); i >= 0 && strings.HasPrefix(rest, "p") {
		prefix, rest = rest[:i+1], rest[i+1:]
	}
	if strings.HasPrefix(rest, "token:") {
		prefix += "token:"
	}
	return prefix + name
}

// liveNameOf returns the current name of the battlefield permanent ref in
// snap, or "" when snap does not list it.
func liveNameOf(snap rules.OracleSnapshot, ref string) string {
	for _, p := range snap.Permanents {
		if p.Ref == ref {
			return p.Name
		}
	}
	return ""
}

// liveNamedSameNames returns ds with every non-pick option ref that a copy or
// becomes effect has given a pick's current name restated under that name.
// A decision's ask is posed between checkpoint d.Step (before the step) and
// d.Step+1 (after it), and a "until end of turn" copy may begin or end inside
// that span, so an option collides when EITHER checkpoint names it so.
//
// An option collides when its current name is the pick's setup name or the
// pick's own current name (both objects renamed to a third name). It is
// restated under the pick's SETUP name, the one the census compares.
//
// Only a decision's OptionRefs are rewritten, only for an option whose setup
// name differs from the pick's, and the pick's own ref (the identity the alias
// binds) is never touched. ds itself and its slices are left as they were.
func liveNamedSameNames(ds []rules.OracleDecision, snaps []rules.OracleSnapshot) []rules.OracleDecision {
	out := make([]rules.OracleDecision, len(ds))
	copy(out, ds)
	for i := range out {
		d := &out[i]
		if d.Step < 0 || d.Step >= len(snaps) || len(d.PickRefs) == 0 || len(d.OptionRefs) < 2 {
			continue
		}
		window := snaps[d.Step:min(d.Step+2, len(snaps))]
		var refs []string
		for _, pick := range d.PickRefs {
			if !strings.Contains(pick, ":") || isSeat(pick) {
				continue
			}
			name := oraclediffRefName(pick)
			// XMage knows the pick by its current name too: a pick that a copy
			// or becomes effect renamed is matched under that name, so another
			// option wearing it collides even when neither setup ref does.
			names := []string{name}
			for _, s := range window {
				if live := liveNameOf(s, pick); live != "" && !slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, live) }) {
					names = append(names, live)
				}
			}
			for j, opt := range d.OptionRefs {
				if opt == pick || strings.EqualFold(oraclediffRefName(opt), name) {
					continue
				}
				collides := slices.ContainsFunc(window, func(s rules.OracleSnapshot) bool {
					live := liveNameOf(s, opt)
					return slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(live, n) })
				})
				if !collides {
					continue
				}
				if refs == nil {
					refs = slices.Clone(d.OptionRefs)
				}
				renamed := refWithName(opt, name)
				// Distinct options stay distinct refs: the census counts them
				// by ref string, and a same-seat real object of that name may
				// already hold the spelling.
				for n := 2; slices.Contains(refs, renamed) || slices.Contains(d.PickRefs, renamed); n++ {
					renamed = refWithName(opt, name) + "#" + strconv.Itoa(n)
				}
				refs[j] = renamed
			}
		}
		if refs != nil {
			d.OptionRefs = refs
		}
	}
	return out
}
