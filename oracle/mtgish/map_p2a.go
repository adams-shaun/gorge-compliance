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

func init() {
	extReplStatic["Draw"] = p2aRevealedDraw
	// Choosing the exiled card a craft-copy equipment copies is a choice
	// feeding the copy effect, as the gorge side's ChooseCard (no token).
	extAction["ChooseAnExiledCardToCopy"] = func(*effects, node) {}
}

// p2aRevealedDraw maps "<players> reveal each card they draw", which
// scripts write as a Draw replacement that draws the card and reveals it
// (then only cleans up), to the player rule the printed card and mtgish
// (RevealsEachCardDrawn) carry. Any other Draw replacement stays one.
func p2aRevealedDraw(r *cards.Repl) []string {
	a := r.With
	if a == nil || a.API != "Draw" || a.Sub == nil || a.Sub.API != "Reveal" {
		return nil
	}
	if s := a.Sub.Sub; s != nil && (s.API != "Cleanup" || s.Sub != nil) {
		return nil
	}
	return []string{"player:revealseachcarddrawn"}
}

// p2aMayLookAt maps a Continuous MayLookAt$ zone by zone: one static may
// open several ("the top card of your library and face-down creatures you
// don't control", AffectedZone$ Library,Battlefield), which mtgish writes
// as one player effect each.
func p2aMayLookAt(p map[string]string) []string {
	zones := strings.Split(p["AffectedZone"], ",")
	if len(zones) < 2 {
		return []string{d2MayLookAt(p)}
	}
	var out []string
	for _, z := range zones {
		q := make(map[string]string, len(p))
		for k, v := range p {
			q[k] = v
		}
		q["AffectedZone"] = strings.TrimSpace(z)
		out = append(out, d2MayLookAt(q))
	}
	return dedup(out)
}

// p2aAttachedCopy summarises "enchanted (equipped) creature is a copy of
// the chosen card", which scripts write as a Static$ Attached trigger that
// clones onto the attached creature until unattached (Metamorphic
// Alteration, Dinosaur Headdress): the printed static, mtgish's copy layer
// effect. It reports false for any other trigger.
func (g *gorgeNorm) p2aAttachedCopy(t *cards.Trigger) bool {
	if t.Mode != "Attached" || t.Params["Static"] != "True" {
		return false
	}
	a := t.Effect
	if a == nil {
		a = cards.ResolveSVar(g.svars(), t.Params["Execute"])
	}
	if a == nil || a.API != "Clone" || a.Params["CloneTarget"] == "" {
		return false
	}
	g.ability(Ability{Kind: "static", Effects: []string{"copy"}})
	return true
}

// p2aPlaceholderSpell reports the effectless spell ability a script gives
// a card whose only printed abilities are keywords (cascade and rebound or
// retrace): "SP$ Pump" with nothing to pump, so that the card can be cast.
// It is no printed ability.
func p2aPlaceholderSpell(a *cards.SA) bool {
	if a.Kind != "SP" || a.API != "Pump" || a.Sub != nil {
		return false
	}
	for k := range a.Params {
		switch k {
		case "StackDescription", "SpellDescription", "AILogic":
		default:
			return false
		}
	}
	return true
}

// p2aReflexiveAlternative reports a ChooseAnAction whose later action
// lists hold a reflexive "when you do" action ("put a charge counter on
// it or remove one from it. When you remove one this way, ..."). Each list
// is one alternative there; walking only the first list drops the
// reflexive trigger's actions and targets. (Other multi-list
// ChooseAnAction shapes keep the first-list reading.)
func p2aReflexiveAlternative(alts []any) bool {
	for _, a := range alts[1:] {
		for _, x := range list(a) {
			if strings.HasPrefix(variantOf(x), "Reflexive_") {
				return true
			}
		}
	}
	return false
}
