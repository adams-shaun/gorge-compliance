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

// chainGraveTgts is the graveyard targets of the ability chain starting at
// line p (api is the line's API, which a Params map does not hold for the
// chain's head), following SubAbility$ as chainReqs does. An optional target
// still gets a card when one fits, so the play's effect is exercised, but it
// is no refusal when none does (placeGraveTargets only places).
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
		if v := q["ValidTgts"]; v != "" && q["TargetMin"] != "0" && graveZone(q) {
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
// effect. A Charm's choices each name a target of their own (Aid the Fallen,
// Fortuitous Find, Remember the Fallen), so each is followed like a
// SubAbility$ chain.
func focusGraveTgts(face *cards.Face, fam string, sa *cards.SA, trig *cards.Trigger) []graveTgt {
	var out []graveTgt
	add := func(api string, p map[string]string, sub *cards.SA) {
		if api == "Charm" {
			for _, name := range strings.Split(p["Choices"], ",") {
				line := face.SVars[strings.TrimSpace(name)]
				out = append(out, chainGraveTgts(svarAPI(line), parseSVar(line), face.SVars, nil)...)
			}
			return
		}
		out = append(out, chainGraveTgts(api, p, face.SVars, sub)...)
	}
	switch {
	case fam == "cast":
		for _, a := range face.Abilities {
			if a.Kind == "SP" {
				add(a.API, a.Params, a.Sub)
			}
		}
	case sa != nil:
		add(sa.API, sa.Params, sa.Sub)
	case trig != nil && trig.Effect != nil:
		add(trig.Effect.API, trig.Effect.Params, trig.Effect.Sub)
	}
	return out
}

// placeGraveTargets meets each graveyard target of the focus play and of the
// key's own trigger effect: a card already in the graveyard the filter names
// counts, else the most inert carrier (carrierPool) that meets the filter goes
// there, else the corpus's least busy matching card (a multicoloured card, or
// a planeswalker, no inert carrier is). The opponent's graveyard is used for an
// OppOwn/OppCtrl filter and the card's controller's (seat 0) otherwise. A
// filter no card meets places nothing, and play refuses the start as before.
func placeGraveTargets(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, fam string, sa *cards.SA, trig *cards.Trigger, f *StartFile) {
	// Both lists, not one instead of the other: a carrier focus (Regrowth for
	// a ChangesZoneAll leaves-your-graveyard trigger) names a graveyard
	// target, and a dies trigger whose effect returns a card from the
	// graveyard (Myr Retriever) names its own. Overriding the key's with the
	// focus's dropped the artifact Myr Retriever must return.
	targets := focusGraveTgts(face, fam, sa, trig)
	targets = append(targets, focusPlayGraveTgts(reg, card, f)...)
	seen := map[graveTgt]bool{}
	used := map[[2]int]bool{} // graveyard cards already counted for a target: {seat, index}
	for _, t := range targets {
		if seen[t] {
			continue
		}
		seen[t] = true
		for i := 0; i < t.n; i++ {
			placeGraveTarget(reg, b, card, t.filter, used, f)
		}
	}
}

// focusPlayGraveTgts is the graveyard targets of the focus play when it is a
// card other than the key's own (a carrier spell such as Reanimate cast for a
// ChangesZoneAll leaves-your-graveyard trigger), or nil when the focus is the
// key card and its targets are already read from the trigger.
func focusPlayGraveTgts(reg *cards.Registry, card string, f *StartFile) []graveTgt {
	fc := f.Focus
	if fc == nil || fc.Card == "" || fc.Card == card || fc.Seat == nil {
		return nil
	}
	c, ok := reg.Lookup(fc.Card)
	if !ok || len(c.Faces) == 0 {
		return nil
	}
	ff := c.Faces[0]
	switch fc.Kind {
	case "cast":
		return focusGraveTgts(ff, "cast", nil, nil)
	case "ability":
		if fc.Ability >= 0 && fc.Ability < len(ff.Abilities) {
			return focusGraveTgts(ff, "", ff.Abilities[fc.Ability], nil)
		}
	}
	return nil
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
				if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 && a.matches(c.Faces[0], false) {
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
		keep := func(f *cards.Face) bool { return a.matches(f, false) }
		name := pickCarrier(reg, b, card, keep)
		if name == "" {
			name = pickZoneFixture(reg, b, card, keep, nil, true)
		}
		if name == "" {
			// the carrier and zone pools hold no lands: a graveyard target
			// may still be a land card (Life from the Loam)
			for _, basic := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest", "Wastes"} {
				if c, ok := reg.Lookup(basic); ok && len(c.Faces) > 0 && keep(c.Faces[0]) {
					name = basic
					break
				}
			}
		}
		if name == "" {
			name = pickLandFixture(reg, b, keep)
		}
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
