package scen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// A focus play that targets a card in a graveyard ("return target creature
// card from your graveyard", "exile target card from a graveyard") is offered
// only when a graveyard holds one (CR 115.1, 601.2c). focusPlayRefusal reads
// such a target as met, so a start whose graveyards held none (or only the
// basics a graveyard need places) was written, never offered, and refused by
// play as start-not-exercised. placeGraveTargets puts the most inert card
// that meets each such target in the graveyard the target names.

// graveTgt is one graveyard target a play names: its ValidTgts filter and how
// many distinct cards it needs (TargetMin, at least 1).
type graveTgt struct {
	filter string
	n      int
}

// graveZone reports whether an ability line's targets are cards in a
// graveyard: TgtZone$ names the graveyard (and not the battlefield), or a
// ChangeZone moves its targets from Origin$ Graveyard with no TgtZone$.
func graveZone(p map[string]string) bool {
	z := p["TgtZone"]
	if z == "" && strings.HasPrefix(p["API"], "ChangeZone") {
		z = p["Origin"]
	}
	return strings.Contains(z, "Graveyard") && !strings.Contains(z, "Battlefield")
}

// chainGraveTgts is the required graveyard targets of the ability chain
// starting at line p (api is the line's API, which a Params map does not
// hold for the chain's head), following SubAbility$ as chainReqs does.
func chainGraveTgts(api string, p map[string]string, svars map[string]string, sub *cards.SA) []graveTgt {
	var out []graveTgt
	for depth := 0; depth < 8 && p != nil; depth++ {
		q := p
		if api != "" {
			q = map[string]string{"API": api}
			for k, v := range p {
				q[k] = v
			}
		}
		if v := q["ValidTgts"]; v != "" && q["TargetMin"] != "0" && q["Optional"] == "" && graveZone(q) {
			n, err := strconv.Atoi(q["TargetMin"])
			if err != nil || n < 1 {
				n = 1
			}
			out = append(out, graveTgt{filter: v, n: n})
		}
		api = ""
		switch {
		case sub != nil:
			p, api, sub = sub.Params, sub.API, sub.Sub
		case p["SubAbility"] != "":
			line := svars[p["SubAbility"]]
			p, api = parseSVar(line), svarAPI(line)
		default:
			p = nil
		}
	}
	return out
}

// svarAPI is the API of an SVar ability line ("DB$ ChangeZone | ..."), or "".
func svarAPI(line string) string {
	head, _, _ := strings.Cut(line, "|")
	_, api, ok := strings.Cut(head, "$")
	if !ok {
		return ""
	}
	return strings.TrimSpace(api)
}

// focusGraveTgts is the graveyard targets of the focus play of a start key:
// the spell's abilities for a cast, the activated ability, or the trigger's
// effect.
func focusGraveTgts(face *cards.Face, fam string, sa *cards.SA, trig *cards.Trigger) []graveTgt {
	var out []graveTgt
	switch {
	case fam == "cast":
		for _, a := range face.Abilities {
			if a.Kind == "SP" && a.API != "Charm" {
				out = append(out, chainGraveTgts(a.API, a.Params, face.SVars, a.Sub)...)
			}
		}
	case sa != nil && sa.API != "Charm":
		out = chainGraveTgts(sa.API, sa.Params, face.SVars, sa.Sub)
	case trig != nil && trig.Effect != nil && trig.Effect.API != "Charm":
		out = chainGraveTgts(trig.Effect.API, trig.Effect.Params, face.SVars, trig.Effect.Sub)
	}
	return out
}

// placeGraveTargets meets each graveyard target of the focus play: a card
// already in the graveyard the filter names counts, else the most inert
// carrier (carrierPool) that meets the filter goes there, the opponent's for
// an OppOwn/OppCtrl filter and the card's controller's (seat 0) otherwise. A
// filter no carrier meets places nothing, and play refuses the start as
// before.
func placeGraveTargets(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, fam string, sa *cards.SA, trig *cards.Trigger, f *StartFile) {
	used := map[[2]int]bool{} // graveyard cards already counted for a target: {seat, index}
	for _, t := range focusGraveTgts(face, fam, sa, trig) {
		for i := 0; i < t.n; i++ {
			placeGraveTarget(reg, b, card, t.filter, used, f)
		}
	}
}

func placeGraveTarget(reg *cards.Registry, b *certdeck.Builder, card, filter string, used map[[2]int]bool, f *StartFile) {
	graves := [2]*[]string{&f.Setup.P0.Graveyard, &f.Setup.P1.Graveyard}
	alts := parseFilter(filter)
	for _, a := range alts {
		if !a.known || a.self {
			continue
		}
		for s, g := range graves {
			if a.ctrl == ctrlYou && s != 0 || a.ctrl == ctrlOpp && s != 1 {
				continue
			}
			for i, name := range *g {
				if used[[2]int{s, i}] || name == card {
					continue
				}
				if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 && a.test(c.Faces[0]) {
					used[[2]int{s, i}] = true
					return
				}
			}
		}
	}
	for _, a := range alts {
		if !a.known || a.self {
			continue
		}
		name := pickCarrier(reg, b, card, a.test)
		if name == "" {
			continue
		}
		s := 0
		if a.ctrl == ctrlOpp {
			s = 1
		}
		*graves[s] = append(*graves[s], name)
		used[[2]int{s, len(*graves[s]) - 1}] = true
		return
	}
}
