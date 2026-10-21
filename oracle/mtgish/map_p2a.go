package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase 2, unit a: normaliser fixes for T1-T3 rows where both encodings
// say the same thing and the summaries rendered them differently.

// p2aTargets adjusts one SA's target slot (gorgeTargets) for two script
// idioms whose targets are not spelt out in ValidTgts$/TargetMin$:
//
//   - Support N (PutCounter Support$ N): Forge builds "up to N other target
//     creatures" from the parameter alone, so the SA carries no ValidTgts$.
//     mtgish writes the target slot (UptoNumberTargetPermanents).
//   - "X target creatures" with X chosen as a cost: scripts write it as
//     TargetMin$ 0 | TargetMax$ <bound> and define X as the number of
//     targets (SVar X:TargetedObjects$Amount), which the cost then pays.
//     mtgish writes NumberTargetPermanents(ValueX), a non-literal count,
//     which is not compared (targetToken); the script's 0..<bound> is the
//     same freely chosen count, so it is not compared either.
func (g *gorgeNorm) p2aTargets(a *cards.SA, t string) string {
	p := a.Params
	if t == "" && p["Support"] != "" {
		return targetToken([]string{"creature"}, countRange("0", literalOr(p["Support"], "*")))
	}
	if strings.HasSuffix(t, "x0..*") && p["TargetMin"] == "0" && literalOr(p["TargetMax"], "*") == "*" &&
		strings.TrimSpace(g.svars()["X"]) == "TargetedObjects$Amount" && g.p2aCostHasX(p["Cost"]) &&
		!strings.Contains(strings.ToLower(p["SpellDescription"]), "up to") {
		// (An "up to X target" ability is scripted the same way; only its
		// description tells it apart, and mtgish's UptoNumber reads 0..*.)
		return strings.TrimSuffix(t, "x0..*")
	}
	return t
}

// p2aCostHasX reports an X the player chooses in the ability's cost or the
// face's mana cost ("X G G T", "R Discard<X/Card>", ManaCost "X G").
func (g *gorgeNorm) p2aCostHasX(cost string) bool {
	for _, c := range []string{cost, g.f.ManaCost} {
		for _, w := range strings.Fields(c) {
			if w == "X" || strings.Contains(w, "<X/") {
				return true
			}
		}
	}
	return false
}

// p2aRevealTrigger summarises the "reveal the first card you draw each
// turn" idiom: a Static$ Drawn trigger (Number$ 1, ForReveal$ True) whose
// chain reveals the card and runs one ImmediateTrigger per "whenever you
// reveal a <kind> card this way". The printed card, and mtgish, carry a
// player effect (RevealFirstCardDrawnDuring..., the "you may" variant with
// an OptionalDecider$, "on each of your turns" with PlayerTurn$) and one
// WhenAPlayerRevealsFirstCardDrawn trigger per reveal clause. It reports
// false for any other trigger.
func (g *gorgeNorm) p2aRevealTrigger(t *cards.Trigger) bool {
	p := t.Params
	if t.Mode != "Drawn" || p["ForReveal"] != "True" || p["Static"] != "True" {
		return false
	}
	tok := "revealfirstcarddrawnduringeachplayersturn"
	if p["PlayerTurn"] == "True" {
		tok = "revealfirstcarddrawnduringplayersturn"
	}
	if p["OptionalDecider"] != "" {
		tok = "may" + tok
	}
	g.ability(Ability{Kind: "static", Effects: []string{"player:" + tok}})
	eff := t.Effect
	if eff == nil {
		eff = cards.ResolveSVar(g.svars(), p["Execute"])
	}
	for a, n := eff, 0; a != nil && n < maxChainDepth; a, n = a.Sub, n+1 {
		if a.API != "ImmediateTrigger" {
			continue
		}
		e := &effects{}
		g.chain(e, cards.ResolveSVar(g.svars(), a.Params["Execute"]), 1)
		g.ability(Ability{Kind: "triggered", Event: "draw", Targets: e.targets, Effects: e.effects})
	}
	return true
}

// p2aMergeCantTarget merges, on the mtgish side, the two halves mtgish
// writes for "can't be the target of <quality> spells or abilities from
// <quality> sources" (one CantBeTheTargetOfSpells rule, one
// CantBeTheTargetOfAbilities rule) into the one static scripts write
// (S:Mode$ CantTarget with no ValidSA$, rule:cantbethetargetofspellsorabilities).
// Only a pair of single-token statics merges; anything else stays.
func p2aMergeCantTarget(s *Summary) {
	const sp, ab = "rule:cantbethetargetofspells", "rule:cantbethetargetofabilities"
	is := func(a Ability, tok string) bool {
		return a.Kind == "static" && len(a.Targets) == 0 && len(a.Effects) == 1 && a.Effects[0] == tok
	}
	var spells, abilities []int
	for i, a := range s.Abilities {
		switch {
		case is(a, sp):
			spells = append(spells, i)
		case is(a, ab):
			abilities = append(abilities, i)
		}
	}
	n := min(len(spells), len(abilities))
	if n == 0 {
		return
	}
	drop := map[int]bool{}
	for k := 0; k < n; k++ {
		s.Abilities[spells[k]].Effects = []string{"rule:cantbethetargetofspellsorabilities"}
		drop[abilities[k]] = true
	}
	out := s.Abilities[:0]
	for i, a := range s.Abilities {
		if !drop[i] {
			out = append(out, a)
		}
	}
	s.Abilities = out
}

// p2aShadowHelper reports the unprinted helper static a script pairs with
// "can block creatures with shadow as though they didn't have shadow"
// (S:Mode$ CanBlockIfShadow): a Secondary$ CantBlock of the blocker when it
// has shadow itself. The printed ability is the CanBlockIfShadow static.
func p2aShadowHelper(st *cards.Static) bool {
	p := st.Params
	return st.Mode == "CantBlock" && p["Secondary"] == "True" && strings.Contains(p["ValidCard"], "withShadow")
}

// p2aReplacementStatics makes a gorge static whose every effect is a
// replacement token ("replace:wouldflipacoin" from S:Mode$ FlipCoinDoubler,
// "replace:woulddealdamage" from S:Mode$ InfectDamage) the replacement
// ability the printed "if ... would ..., instead" text and mtgish's
// Replace* rule are.
func p2aReplacementStatics(s *Summary) {
	for i, a := range s.Abilities {
		if a.Kind != "static" || len(a.Effects) == 0 {
			continue
		}
		all := true
		for _, t := range a.Effects {
			if !strings.HasPrefix(t, "replace:") {
				all = false
				break
			}
		}
		if all {
			s.Abilities[i].Kind = "replacement"
		}
	}
}
