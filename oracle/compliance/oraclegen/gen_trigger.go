package oraclegen

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// loneTriggerCostDecline is a triggered-cost ask offered with only its
// decline option (engine/rules/cumulative.go: the cost is unpayable for gorge),
// answered with it, over a cost XMage's DoIfCostPaid asks about regardless
// (rules.OracleDecision.CostPosed). A decline-only ask over any other cost is
// not posed by XMage and stays forced away.
func loneTriggerCostDecline(d rules.OracleDecision) bool {
	return d.CostPosed && d.Kind == "choose_n" && d.Options == 1 && len(d.Picks) == 1 &&
		len(d.PickKinds) == 1 && d.PickKinds[0] == "trigger_cost_decline"
}

// triggerOrderChoice is the XMage choice for pick k of a trigger-order
// decision: the pick's source object's plain NAME ("Adrenaline Jockey"),
// which XMage's chooseTriggeredAbility matches against the ability's source
// object name. Gorge's label carries the card's TriggerDescription$, which is
// XMage's rule wording only by coincidence ("put a +1/+1 counter on this
// creature" against XMage's "...put a +1/+1 counter on {this}"), so the text
// form misses and the replay's fallback player orders the stack itself (the
// Adrenaline Jockey/trigger#0.1 drift). A plain name, not a ref: the trigger
// ask resolves no aliases (TestPlayer's assertAliasSupportInChoices), so the
// driver's "@" alias form would fail the replay outright. A pick whose ref is
// not a scenario ref (an emblem or other sourceless trigger; PickRefs then
// carries the label) falls back to the label's rule text, the pre-name
// behaviour.
func triggerOrderChoice(d rules.OracleDecision, k int) string {
	if k < len(d.PickRefs) {
		if ref := d.PickRefs[k]; isScenarioRefShaped(ref) {
			return oraclediffRefName(ref)
		}
	}
	if k < len(d.Picks) {
		return triggerRule(d.Picks[k])
	}
	return ""
}

// triggerOrderNamesDistinct reports whether XMage can order the picks by their
// source object NAMES. XMage's chooseTriggeredAbility matches a choice against
// the source's name, and its ask loop auto-pushes the last remaining ability,
// so a source named more than once is only a problem when its picks are
// DIFFERENT abilities: two triggers of the same ability of the same object are
// interchangeable (CR 603.3b, their mutual order is unobservable). Collapse
// each (ref, rule text) pair -- one interchangeable ability -- and require at
// least two such groups whose source names are unique. The Harmonic Prodigy
// case is one group for Harmonic Prodigy's Prowess and one for Soul-Scar
// Mage's two Prowess triggers: the names order Prodigy against Soul-Scar, and
// the second Soul-Scar is the auto-pushed last pick. Thundertrap Trainer's ETB
// and Offspring share a ref but not a text, so they stay two groups with one
// name and take the rule-text form. A pick with no scenario ref falls back to
// text anyway, so it is skipped here (it cannot collide); an order with fewer
// than two scenario refs keeps that old text fallback. One pick is never
// ambiguous.
func triggerOrderNamesDistinct(d rules.OracleDecision) bool {
	if len(d.Picks) < 2 {
		return true
	}
	groups := map[string]bool{}
	var refs []string
	allRefs := true
	for k := range d.Picks {
		if k >= len(d.PickRefs) || !isScenarioRefShaped(d.PickRefs[k]) {
			allRefs = false
			continue
		}
		ref := d.PickRefs[k]
		key := ref + "\x00" + strings.TrimSpace(triggerRule(d.Picks[k]))
		if TriggerReadsEvent(d.Picks[k]) {
			// Not interchangeable: each instance reads its own event (Abomination's
			// "that many"), so the repeat is a pick the name cannot order.
			key += "\x00" + strconv.Itoa(k)
		}
		if groups[key] {
			continue
		}
		groups[key] = true
		refs = append(refs, ref)
	}
	if len(refs) < 2 {
		// One named source is ambiguous only when every pick is a scenario ref
		// (two triggers of one object). A non-ref pick falls back to text, the
		// pre-name behaviour.
		return !allRefs
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		name := oraclediffRefName(ref)
		if name == "" {
			continue
		}
		if seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

// trigSpan records where one trigger-order decision's name answers sit in a
// step's answer list, and the inert text answers that replace them when the
// step also scripts other asks.
type trigSpan struct {
	step  int
	start int
	count int
	old   []XAnswer
	// textBlind marks a span whose picks share rule text (two Prowess
	// triggers): the inert text form cannot tell them apart, so XMage orders
	// them itself and the extra text entry stays queued for a later ask.
	// Such a span keeps its source-name answers even on a shared step.
	textBlind bool
}

// triggerOrderTextBlind reports whether two picks carry the same rule text
// (after the source prefix is dropped), so only the source name can order them.
func triggerOrderTextBlind(d rules.OracleDecision) bool {
	seen := map[string]bool{}
	for _, label := range d.Picks {
		t := strings.TrimSpace(triggerRule(label))
		if seen[t] {
			return true
		}
		seen[t] = true
	}
	return false
}

// triggerOrderTextAnswers is the pre-name behaviour: the label's rule text
// for every pick. XMage's rule wording matches gorge's TriggerDescription$
// only by coincidence, so these are inert -- never consumed, never steering
// the order -- and against most dialogs unconsumable; NOT against a firing
// colour dialog, which pops the head and throws (measured: Ashling,
// Rekindled/trigger#1.1). manaHoistSpan replaces the span with the colour
// answer when the step later asks one.
func triggerOrderTextAnswers(d rules.OracleDecision) []XAnswer {
	out := make([]XAnswer, 0, len(d.Picks))
	for _, label := range d.Picks {
		out = append(out, XAnswer{d.Seat, "choice", triggerRule(label)})
	}
	return out
}

// demotedTriggerOrderAnswers is what a distinct-source trigger order demotes
// to on a step that scripts other asks too: XMage's rule-text keys
// (xmageTriggerOrderKeys) when the picks key apart, else the inert text form.
// A key is consumed only by the trigger ask it steers (TestPlayer's
// chooseTriggeredAbility matches getRule() by prefix, or by containment for
// a delayed trigger's timing-last rule text) and names no object, so
// no other makeChoose takes it, which is the reason names are demoted at all.
// The verbatim text form is not inert in strict mode: the order ask finds no
// match and stops the replay at "Missing CHOICE def ... Triggered list"
// (Clement, the Worrywort beside Valley Mightcaller, whose step also scripts
// Clement's target; Harvestrite Host beside Hinterland Sanctifier).
func demotedTriggerOrderAnswers(d rules.OracleDecision) []XAnswer {
	if keys, ok := xmageTriggerOrderKeys(d); ok {
		out := make([]XAnswer, 0, len(keys))
		for _, key := range keys {
			out = append(out, XAnswer{d.Seat, "choice", key})
		}
		return out
	}
	return triggerOrderTextAnswers(d)
}

// demoteTriggerOrderSpans rewrites a step's trigger-order name answers back to
// the inert text form when the step's answer list also carries other asks'
// answers: a plain name is consumable by any later makeChoose (a target or
// order dialog), so a leftover one derails them, while the text form is not
// (measured: Baron Strucker/static#0.0, whose ETB order ask XMage poses at
// another point than gorge recorded, replays agree only with the text form).
// The text form is not inert against EVERY dialog: a firing colour dialog
// pops and throws on it, which is the manaHoistSpan routing's job. A step
// whose ONLY answers are trigger-order names keeps the name form: the
// names are consumed by the order ask itself and steer it. A span whose
// picks share rule text (trigSpan.textBlind) is never demoted: the text form
// cannot order identical triggers and leaves a stray answer queued
// (BLB Dawn's Truce with two Prowess triggers).
func demoteTriggerOrderSpans(out [][]XAnswer, spans []*trigSpan) {
	if len(spans) == 0 {
		return
	}
	byStep := map[int][]*trigSpan{}
	for _, sp := range spans {
		byStep[sp.step] = append(byStep[sp.step], sp)
	}
	steps := make([]int, 0, len(byStep))
	for step := range byStep {
		steps = append(steps, step)
	}
	sort.Ints(steps)
	for _, step := range steps {
		sp := byStep[step]
		if step >= len(out) {
			continue
		}
		covered := map[int]bool{}
		for _, s := range sp {
			for k := 0; k < s.count; k++ {
				covered[s.start+k] = true
			}
		}
		shared := false
		for i := range out[step] {
			if !covered[i] {
				shared = true
				break
			}
		}
		if !shared {
			continue
		}
		for i := len(sp) - 1; i >= 0; i-- {
			s := sp[i]
			if s.textBlind {
				continue
			}
			tail := append([]XAnswer(nil), out[step][s.start+s.count:]...)
			out[step] = append(out[step][:s.start], append(append([]XAnswer(nil), s.old...), tail...)...)
		}
	}
}

// triggerRule drops gorge's "<SourceName>: " prefix from a trigger-order
// label: XMage's chooseTriggeredAbility compares its choice against the
// ability's rule text (getRule), which carries no source prefix.
func triggerRule(label string) string {
	if i := strings.Index(label, ": "); i >= 0 {
		return label[i+2:]
	}
	return label
}
