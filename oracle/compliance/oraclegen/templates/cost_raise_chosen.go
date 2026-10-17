package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// The RaiseCost shapes whose recipient is not the probe's own card nor a
// plain opponent spell: a recipient the source names as it enters (a chosen
// card type, Arachne, Psionic Weaver; a chosen card name, Skyseer's Chariot)
// and an opponent's spell cast from a graveyard (Aven Interrupter). Each probe
// pays the printed price plus the raise exactly, so a raise gorge does not
// apply leaves the surplus floating in the pool -- a snapshot difference the
// replay surfaces as a divergence, not a skipped row.

// raiseChosenCardType is the card type a Card.ChosenType raise is led to; the
// probe spell is of that type.
const raiseChosenCardType = "Instant"

// raiseSpellProbe is the instant the chosen-type probe casts at p1.
const raiseSpellProbe = "Shock"

// raiseFlashbackProbe is the instant an opponent casts from its graveyard by
// flashback (CR 702.34a) under a graveyard-cast raise.
const raiseFlashbackProbe = "Think Twice"

// raiseCostChosenProbes serves the three recipient shapes above. handled is
// false for any other static, so the caller's chain runs unchanged.
func raiseCostChosenProbes(reg *cards.Registry, f *cards.Face, name string, idx int) (probes []costProbe, gap string, handled bool) {
	st := f.Statics[idx]
	if st.ModeKind() != cards.StaticRaiseCost {
		return nil, "", false
	}
	n, err := strconv.Atoi(strings.TrimSpace(st.Params["Amount"]))
	if err != nil || n < 1 {
		return nil, "", false
	}
	extra := strings.Repeat("C", n)
	valid := strings.TrimSpace(st.Params["ValidCard"])
	switch {
	case strings.EqualFold(valid, "Card.ChosenType") && strings.EqualFold(st.Params["Type"], "Spell"):
		if !chosesCardType(f) {
			return nil, "chosen-type entry is not a card-type choice", true
		}
		// Arachne's as-enters chain chooses the opponent whose hand it looks
		// at (p1, the only one; its hand is empty, so no look is
		// acknowledged), then the card type. A setup placement poses none of
		// it, so the card is cast.
		pre, ok := castSourcePrelude(f, name, "p1", raiseChosenCardType)
		if !ok {
			return nil, "source cost unpayable", true
		}
		return []costProbe{{
			spell: raiseSpellProbe, mana: extra + "R", hand: []string{name}, targeted: true,
			pre: pre, preXAbility: make([]string, len(pre)), mustReplay: true,
		}}, "", true
	case strings.EqualFold(valid, "Card.NamedCard") && strings.EqualFold(st.Params["ValidSpell"], "Activated"):
		probe, ok := reg.Lookup(activatedProbe)
		if !ok || len(probe.Faces) == 0 || len(probe.Faces[0].Abilities) != 1 {
			return nil, "named-source probe unavailable", true
		}
		prefixes, why := oraclegen.XMageAbility(probe.Faces[0])
		if why != "" || prefixes[0] == "" {
			return nil, "named-source probe xmage text unavailable", true
		}
		// The name is chosen as the card enters: the card is cast and the
		// probe named at its resolve (a setup placement offers no name).
		pre, ok := castSourcePrelude(f, name, activatedProbe)
		if !ok {
			return nil, "source cost unpayable", true
		}
		return []costProbe{{
			spell: activatedProbe, mana: extra, battlefield: []string{activatedProbe}, hand: []string{name},
			activate: &costActivation{index: 0, prefix: prefixes[0], targets: []string{"p1"}},
			pre:      pre, preXAbility: make([]string, len(pre)), mustReplay: true,
		}}, "", true
	case strings.EqualFold(st.Params["Activator"], "Opponent") && strings.EqualFold(st.Params["Type"], "Spell") &&
		strings.Contains(valid, "wasCastFromGraveyard"):
		probe, ok := reg.Lookup(raiseFlashbackProbe)
		if !ok || len(probe.Faces) == 0 {
			return nil, "flashback probe not in corpus", true
		}
		fb, ok := probe.Faces[0].KeywordCostParam("Flashback")
		if !ok {
			return nil, "flashback probe has no flashback cost", true
		}
		pool, why := oraclegen.PoolFor(fb)
		if why != "" {
			return nil, "flashback probe cost unpayable", true
		}
		return []costProbe{{
			spell: raiseFlashbackProbe, mana: extra + pool, battlefield: []string{name},
			opponent: true, castFrom: "graveyard", castMode: "flashback", mustReplay: true,
		}}, "", true
	}
	return nil, "", false
}

// castSourcePrelude casts the source from p0's hand and resolves it, the
// resolve answering its as-enters asks with picks in order.
func castSourcePrelude(f *cards.Face, name string, picks ...string) ([]oraclegen.Step, bool) {
	pool, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return nil, false
	}
	answers := make([]oraclegen.Answer, 0, len(picks))
	for _, p := range picks {
		answers = append(answers, oraclegen.Answer{Kind: "choose", Pick: []string{p}})
	}
	return []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: pool},
		{Op: "resolve", Answers: answers},
	}, true
}

// chosesCardType reports whether the face's as-enters replacement chain
// (ETBReplacement and its SubAbility$ links) chooses a CARD type, the choice
// a Card.ChosenType spell filter reads (Arachne's "choose a card type other
// than creature").
func chosesCardType(f *cards.Face) bool {
	for _, kw := range f.Keywords {
		head, tail, ok := strings.Cut(kw, ":")
		if !ok || head != "ETBReplacement" {
			continue
		}
		next := tail[strings.LastIndex(tail, ":")+1:]
		for hops := 0; next != "" && hops < 8; hops++ {
			sv := f.SVars[next]
			if strings.Contains(sv, "DB$ ChooseType") && strings.Contains(sv, "Type$ Card") {
				return true
			}
			next = ""
			for _, part := range strings.Split(sv, "|") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(part), "SubAbility$"); ok {
					next = strings.TrimSpace(v)
				}
			}
		}
	}
	return false
}
