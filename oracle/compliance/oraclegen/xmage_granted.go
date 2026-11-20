package oraclegen

// XMage rule-text prefixes for granted abilities. XMage's TestPlayer selects an
// activated ability with ability.toString().startsWith(text); a granted or
// gained ability's rule text is not one of the recipient face's printed lines,
// so the prefix is derived from the resolved *cards.SA instead: its keyword
// line when it is a derived keyword ability, else its cost portion in XMage's
// spelling. The caller (xprep) resolves the SA through
// ResolveActivatedAbility, so the next grant kind gets the same treatment.

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// XMageGrantedAbilityPrefix returns the rule-text prefix that selects sa on
// sourceName, or false when the SA carries neither a keyword line nor a cost
// the XMage spelling can derive. It is exact for the shapes XMage exposes: a
// derived keyword line ("Cycling:1 U" -> "Cycling {1}{U}", "Crew:2" ->
// "Crew 2") and a cost portion ("R" -> "{R}"). Anything else fails closed
// rather than shipping the printed index's prefix.
func XMageGrantedAbilityPrefix(sa *cards.SA, sourceName string) (string, bool) {
	if sa == nil {
		return "", false
	}
	if line := strings.TrimSpace(sa.ParamStr(cards.PKKeywordLine)); line != "" {
		return xmageGrantedKeywordPrefix(line)
	}
	cost := strings.TrimSpace(sa.ParamStr(cards.PKCost))
	if cost == "" {
		return "", false
	}
	// A granted body's self reference is CARDNAME, not the recipient's printed
	// name: the engine label embeds the raw SVar text. Spell it as the generic
	// "this creature" before the cost renderer's Sacrifice/Exile source rules.
	cost = strings.ReplaceAll(cost, "CARDNAME", "this creature")
	cost = selfRef(cost, sourceName)
	p, ok := xmageForgeManaCost(cost)
	if !ok {
		return "", false
	}
	return p, true
}

// xmageGrantedKeywordPrefix spells a derived keyword line in XMage's rule
// text. The line is the same "Head:param" shape cards.GrantedKeywordAbility
// consumes, so this is the inverse for the heads that mint an activated
// ability. An unknown head still returns the head alone: it is a valid prefix
// of the rule text ("Station ..."), and a wrong longer spelling would select
// nothing.
func xmageGrantedKeywordPrefix(line string) (string, bool) {
	head, param, has := strings.Cut(line, ":")
	head = strings.TrimSpace(head)
	if head == "" {
		return "", false
	}
	if !has {
		return head, true
	}
	param = strings.TrimSpace(param)
	switch head {
	case "Cycling":
		cost, ok := xmageForgeManaCost(param)
		if !ok {
			return head, true
		}
		return head + " " + cost, true
	case "TypeCycling":
		// Forge's TypeCycling:Sliver:3 mints the printed word XMage's
		// CyclingAbility carries in its rule text ("Slivercycling {3}"), not
		// the keyword head. typeCyclingWord is the same spelling the printed
		// path (keywordPrefix) matches against the Oracle line, and XMage
		// capitalises the printed word at the start of the rule.
		typ, rest, _ := strings.Cut(param, ":")
		word := typeCyclingWord(typ)
		if word == "" {
			return "", false
		}
		if word[0] >= 'a' && word[0] <= 'z' {
			word = strings.ToUpper(word[:1]) + word[1:]
		}
		prefix := word + "cycling"
		cost, ok := xmageForgeManaCost(strings.TrimSpace(rest))
		if !ok {
			return prefix, true
		}
		return prefix + " " + cost, true
	case "Crew", "Saddle":
		if fields := strings.Fields(param); len(fields) > 0 {
			return head + " " + fields[0], true
		}
		return head, true
	case "Equip", "Fortify", "Outlast", "Unearth", "Embalm", "Eternalize", "Ninjutsu":
		cost, ok := xmageForgeManaCost(param)
		if !ok {
			return head, true
		}
		return head + " " + cost, true
	default:
		return head, true
	}
}

// xmageForgeManaCost renders Forge's space-separated mana cost ("RW RW", "3",
// "G") in XMage's braced spelling, or false for any non-mana part the prefix
// cannot spell. It mirrors xprep's helper of the same purpose and the
// templates' generator: the two must agree on the cost a step pays.
func xmageForgeManaCost(cost string) (string, bool) {
	var b strings.Builder
	for _, sym := range strings.Fields(cost) {
		switch {
		case sym == "0":
			b.WriteString("{0}")
		case strings.Trim(sym, "0123456789") == "":
			b.WriteString("{" + sym + "}")
		case len(sym) == 1 && strings.Contains("WUBRGC", sym):
			b.WriteString("{" + sym + "}")
		case len(sym) == 2 && strings.Contains("WUBRG", sym[:1]) && strings.Contains("WUBRG", sym[1:]):
			b.WriteString("{" + sym[:1] + "/" + sym[1:] + "}")
		case len(sym) == 2 && sym[0] == '2' && strings.Contains("WUBRG", sym[1:]):
			b.WriteString("{2/" + sym[1:] + "}")
		case sym == "T" || sym == "Q":
			b.WriteString("{" + sym + "}")
		default:
			return "", false
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}
