package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// xmageTriggerOrderKeys answers a SAME-SOURCE trigger-order decision in
// XMage's rule-text form: one choice per pick but the last (XMage pushes the
// last remaining ability without an ask), each a prefix of the pick's rule
// text as XMage's no-argument getRule() prints it.
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
// when another pick's DIFFERENT text shares that sentence. A long key fails
// closed: wording XMage prints otherwise leaves the key unmatched (the old
// inert behaviour), where a short prefix could match the wrong ability of
// XMage's list. ok is false when the picks cannot be keyed apart (a key that
// is also a prefix of another, different text), and the caller keeps the old
// text form.
func xmageTriggerOrderKeys(d rules.OracleDecision) ([]string, bool) {
	n := len(d.Picks)
	if n < 2 {
		return nil, false
	}
	texts := make([]string, n)
	for k, label := range d.Picks {
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
		key, ok := distinctTriggerKey(texts, k)
		if !ok {
			return nil, false
		}
		keys = append(keys, key)
	}
	return keys, true
}

// distinctTriggerKey is texts[k]'s first sentence, or the whole text when the
// sentence is also a prefix of another pick's different text; false when even
// the whole text is.
func distinctTriggerKey(texts []string, k int) (string, bool) {
	own := texts[k]
	clash := func(key string) bool {
		for j, other := range texts {
			if j != k && other != own && strings.HasPrefix(other, key) {
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
