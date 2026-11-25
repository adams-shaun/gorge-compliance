package oraclegen

import (
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// xmageTriggerOrderKeys answers a SAME-SOURCE trigger-order decision in
// XMage's rule-text form: one choice per pick but the last (XMage pushes the
// last remaining ability without an ask), each a prefix of the pick's rule
// text as XMage's no-argument getRule() prints it -- or, for a delayed
// trigger whose XMage rule text puts the subject first and the timing last
// (Arcane Denial's caster draw: "You draw a card at the beginning of the
// next turn's upkeep." against Forge's "Draw a card."), a containment
// fragment the driver's fallback folds into it.
//
// XMage's chooseTriggeredAbility matches a scripted choice with
// ability.toString().startsWith(choice) (toString is getRule()) or against the
// source's name. Two triggers off one source share the name, so only the rule
// text can tell them apart -- and gorge's label carries Forge's
// TriggerDescription$, whose self-references are CARDNAME / NICKNAME tokens
// where getRule() leaves the literal "{this}", and whose ability-word header
// is "Word — " where XMage prints "<i>Word</i> &mdash; ". Spelled verbatim,
// the text never matches: XMage warns, orders the stack with its fallback
// player, and leaves the text at the head of the choice queue, where the next
// makeChoose (a mana trigger's colour dialog) pops it and throws (measured:
// Ashling, Rekindled/trigger#1.1 and #1.2, "Choice key [Whenever this
// creature transforms into CARDNAME ...] not found in [White, Blue, ...]").
//
// Each key is the rewritten text's first sentence, extended to the whole text
// when another pick that is NOT the same ability of the same object shares
// that sentence. A long key fails closed against the WRONG ability: a short
// prefix could match another ability of XMage's list. Wording XMage prints
// otherwise ("When" where XMage says "Whenever", a text XMage continues past
// the key's period) is not inert in strict mode -- an unmatched key is a
// strict_miss -- so the driver's ScriptedChoicePlayer.chooseTriggeredAbility
// folds that wording (leading when/whenever, case, {this}, trailing period)
// before it matches
// the key, and takes the ability only when exactly one fits; a key no rule
// starts with is also accepted when exactly one rule's normalised text
// contains it (the delayed-trigger shape above). ok is false when
// the picks cannot be keyed apart: a key that is also a prefix of another,
// different pick (a different source ref or a different text), including two
// different sources with identical text, which the same key would both match.
// The caller then keeps the old text form.
func xmageTriggerOrderKeys(d rules.OracleDecision) ([]string, bool) {
	n := len(d.Picks)
	if n < 2 {
		return nil, false
	}
	texts := make([]string, n)
	refs := make([]string, n)
	for k, label := range d.Picks {
		if k < len(d.PickRefs) {
			refs[k] = d.PickRefs[k]
		}
		source := ""
		if i := strings.Index(label, ": "); i >= 0 {
			source = label[:i]
		}
		texts[k] = xmageTriggerRuleText(triggerRule(label), source)
		if texts[k] == "" {
			return nil, false
		}
	}
	keys := make([]string, 0, n-1)
	for k := 0; k+1 < n; k++ {
		key, ok := distinctTriggerKey(texts, refs, k)
		if !ok {
			return nil, false
		}
		keys = append(keys, key)
	}
	return keys, true
}

// distinctTriggerKey is texts[k]'s first sentence, or the whole text when the
// sentence is also a prefix of another pick's text that is NOT the same
// ability of the same object; false when even the whole text is. A pick is
// interchangeable with k only when it shares both the ref and the text: two
// Prowess triggers of one Soul-Scar Mage are one group, but two Prowess
// triggers of different objects (or two different abilities of one object)
// must still be told apart.
func distinctTriggerKey(texts, refs []string, k int) (string, bool) {
	own := texts[k]
	ref := func(i int) string {
		if i < len(refs) {
			return refs[i]
		}
		return ""
	}
	clash := func(key string) bool {
		for j, other := range texts {
			if j == k {
				continue
			}
			if other == own && ref(j) == ref(k) {
				continue
			}
			if strings.HasPrefix(other, key) {
				return true
			}
		}
		return false
	}
	if i := strings.Index(own, ". "); i >= 0 {
		if key := own[:i+1]; !clash(key) {
			return key, true
		}
	}
	if clash(own) {
		return "", false
	}
	return own, true
}

// xmageTriggerRuleText rewrites a Forge TriggerDescription$ into the form
// XMage's getRule() prints: the CARDNAME / NICKNAME tokens and the source's
// printed name become "{this}" (selfRef), and a leading "Word — " ability or
// flavor word becomes "<i>Word</i> &mdash; " (flavorHeader).
func xmageTriggerRuleText(text, source string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "CARDNAME", "{this}")
	text = strings.ReplaceAll(text, "NICKNAME", "{this}")
	text = selfRef(text, source)
	if i := strings.Index(text, " — "); i > 0 && !strings.ContainsAny(text[:i], ".,:") {
		text = flavorHeader(text[:i+len(" — ")]) + text[i+len(" — "):]
	}
	return text
}

// eventReference names the words of a trigger's EFFECT that point back at the
// event that triggered it ("put that many counters", "return it", "its
// controller", "them"). A trigger whose resolution reads its event resolves
// differently per instance (Abomination's three combat-damage triggers each
// carry a different damage amount), so two picks of one ref and one rule text
// are NOT interchangeable: the pick says nothing of which instance goes first.
var eventReference = regexp.MustCompile(`(?i)\b(that|those|it|its|them|their|he|she|his|her)\b`)

// TriggerReadsEvent reports whether the trigger labelled label (gorge's
// "<Source>: <rule text>") reads its triggering event. The condition (up to
// the first ", ") is excluded -- "a spell you cast" is the event being
// described, not read -- and only the effect is searched. A keyword label
// ("Prowess") has no effect text and reads nothing. Conservative: a false
// positive keeps the harness excuse for a repeated pick, a false negative
// compares an order the follower chose itself.
func TriggerReadsEvent(label string) bool {
	text := strings.TrimSpace(triggerRule(label))
	if i := strings.Index(text, ", "); i >= 0 {
		text = text[i+2:]
	} else {
		return false
	}
	return eventReference.MatchString(text)
}
