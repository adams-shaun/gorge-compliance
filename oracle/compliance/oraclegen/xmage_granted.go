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
//
// recipient is the face the ability is granted to. A cost portion is only a
// prefix of the granted ability's rule text, so on a recipient with another
// selectable text that starts the same way ("{T}" against a land's
// "{T}: Add {U}.") it is extended into the SA's description a word at a time,
// as XMageAbility does for printed lines, and refused when it still collides
// or would pass through a self reference. A nil recipient cannot be checked,
// so a cost-portion grant is refused; a keyword line is not affected.
func XMageGrantedAbilityPrefix(sa *cards.SA, sourceName string, recipient *cards.Face) (string, bool) {
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
	if !ok || recipient == nil {
		return "", false
	}
	others := xmageRecipientTexts(recipient)
	if !prefixCollides(p, others) {
		return p, true
	}
	desc := strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription))
	if desc == "" || strings.Contains(desc, "CARDNAME") || namesShortName(desc, sourceName) ||
		(sourceName != "" && strings.Contains(strings.ToLower(desc), strings.ToLower(sourceName))) {
		return "", false
	}
	full := append([]string{p + ": " + desc}, others...)
	ext := extendPrefix(p, full, 0)
	if prefixCollides(ext, others) {
		return "", false
	}
	return ext, true
}

// prefixCollides reports whether XMage's startsWith selection by prefix would
// also reach one of the other selectable texts.
func prefixCollides(prefix string, others []string) bool {
	for _, o := range others {
		if strings.HasPrefix(o, prefix) {
			return true
		}
	}
	return false
}

// xmageRecipientTexts lists every activated-ability text of f XMage's
// selection can reach: its printed activated lines in the XMage spelling, the
// injected basic-land mana abilities (CR 305.6) and its keyword abilities.
func xmageRecipientTexts(f *cards.Face) []string {
	var out []string
	for _, line := range removeKeywordAbilityLines(f, abilityLines(f.Oracle)) {
		out = append(out, xmageRuleLine(line, f.Name))
	}
	for _, sa := range f.Abilities {
		if !sa.IsActivated() {
			continue
		}
		if intrinsicLandMana(sa) {
			out = append(out, intrinsicManaText(sa))
		} else if sa.ParamStr(cards.PKKeyword) != "" {
			if text, ok := keywordPrefix(f, sa); ok {
				out = append(out, text)
			}
		}
	}
	return out
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
