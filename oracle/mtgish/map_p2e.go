package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase 2, unit e: residual T4 effect classes found triaging the shard
// that continues unit b's.

func init() {
	for k, v := range p2ePlayerSynonyms {
		d2PlayerSynonyms[k] = v
	}
	// Statics an Effect creates (no printed S: line uses these modes):
	// "it assigns no combat damage this turn", "can be the target of
	// spells and abilities as though it didn't have shroud", "can't phase
	// out", "you can't lose life".
	extStatic["AssignNoCombatDamage"] = fixed("rule:assignsnocombatdamage")
	extStatic["IgnoreShroud"] = fixed("rule:canbethetargetofspellsorabilitiesasthoughitdidnthaveshroud")
	extStatic["CantPhaseOut"] = fixed("rule:cantphaseout")
	extStatic["CantLoseLife"] = fixed("player:cantloselife")
}

// p2ePlayerSynonyms are PlayerEffect variants that are one gorge static
// under another spelling.
var p2ePlayerSynonyms = map[string]string{
	// S:Mode$ CantBeActivated (Activator$, AffectedZone$, ValidSA$ and a
	// non-active-player Activator$ are its filters).
	"CantActivateNonManaAbilities":             "rule:cantbeactivated",
	"CantActivateAbilities":                    "rule:cantbeactivated",
	"CantActivateAbilitiesOfCardsInGraveyards": "rule:cantbeactivated",
	"CanActivateAbilitiesOnlyDuringTheirTurn":  "rule:cantbeactivated",
	// S:Mode$ CantBeCast with Caster$ Player.NonActive / a not-from-hand
	// filter.
	"CanCastSpellsOnlyDuringTheirTurn": "player:cantcastspells",
	"CanOnlyCastSpellsFromThierHand":   "player:cantcastspells",
	// Player keywords (Continuous AddKeyword$ on a player), the gorge side
	// spelling the keyword's own words.
	"SpellsAndAbilitiesTheyCantCantCauseThemToSearchTheirLibrary": "player:spellsandabilitiesyoucontrolcantcauseyoutosearchyourlibrary",
	"ChoosesHowCreaturesBlock":                                    "player:chooseswhichcreaturesblockandhowtheyblock",
	"AssignsCombatDamageOfAttackingCreatures":                     "player:youassigncombatdamageofeachcreatureattackingyou",
	"CanForetellCardsDuringEachPlayersTurn":                       "player:foretellonanyplayersturn",
	"CantUntapMoreThanNumberPermanentsDuringTheirUntapStep":       "player:untapadjust",
}

// p2eSpellSynonyms are StackSpellsEffect rules that are a gorge static of
// another family: "creature spells can't be cast" is S:Mode$ CantBeCast.
var p2eSpellSynonyms = map[string]string{
	"CantBeCast": "player:cantcastspells",
}

// stackSpellToken maps one StackSpellsEffect rule.
func stackSpellToken(v string) string {
	if t, ok := p2eSpellSynonyms[v]; ok {
		return t
	}
	return "spell:" + strings.ToLower(v)
}

// effectStaticP2E maps one static an Effect creates. An SVar static body
// keeps a compound "Mode$ CantAttack,CantBlock" whole (gorge splits only
// printed S: lines), so each mode is mapped on its own.
func effectStaticP2E(st cards.Static) []string {
	var out []string
	for _, mode := range strings.Split(st.Mode, ",") {
		out = append(out, gorgeStaticEffects(strings.TrimSpace(mode), st.Params)...)
	}
	return out
}

// effectReplP2E maps the replacement effects an Effect creates that are a
// static rule printed ("you can't lose the game this turn", "spells you
// cast this turn can't be countered", "damage that would reduce your life
// total to less than 1 reduces it to 1 instead"), the same way a printed
// R: line of that shape is mapped (gorgeNorm.repl).
func effectReplP2E(a *cards.SA, svars map[string]string) []string {
	var out []string
	for _, name := range strings.Split(a.Params["ReplacementEffects"], ",") {
		body, ok := svars[strings.TrimSpace(name)]
		if !ok {
			continue
		}
		r, ok := cards.ParseReplacementLine(body)
		if !ok {
			continue
		}
		if eff, ok := replStatic(&r, svars); ok {
			out = append(out, eff...)
			continue
		}
		switch {
		case r.Event == "GameLoss" && r.Params["ReplaceWith"] == "":
			out = append(out, "player:cantlosethegame")
		case r.Event == "GameWin":
			out = append(out, "player:cantwinthegame")
		case r.Event == "Counter" && r.Params["ReplaceWith"] == "":
			out = append(out, "spell:cantbecountered")
		}
	}
	return out
}

// playerEffectTokensP2E maps one PlayerEffect node. "Spells you cast have
// <ability>" (SpellsCastHaveAbility, SpellsControlledHaveAbility) is the
// granted spell ability itself, as the script's Continuous Affected$
// <spells> AffectedZone$ Stack static writes it (spell:<keyword>, or
// spell:cantbecountered for an R:Event$ Counter Layer$ CantHappen).
func playerEffectTokensP2E(x any) []string {
	v := variantOf(x)
	if v != "SpellsCastHaveAbility" && v != "SpellsControlledHaveAbility" {
		return []string{playerEffectToken(v)}
	}
	var out []string
	for _, r := range list(argN(x, 1)) {
		if strings.HasPrefix(variantOf(r), "CantBeCountered") {
			out = append(out, "spell:cantbecountered")
			continue
		}
		out = append(out, spellEffectTokens([]any{r})...)
	}
	return out
}

// playerPumpP2E reports a Pump whose object is a player ("you gain
// hexproof until your next turn"): its keywords are player effects.
func playerPumpP2E(api string, p map[string]string) bool {
	if api != "Pump" {
		// PumpAll's ValidTgts$ Player chooses whose creatures.
		return false
	}
	if p["ValidTgts"] != "" {
		return strings.HasPrefix(p["ValidTgts"], "Player") || strings.HasPrefix(p["ValidTgts"], "Opponent")
	}
	switch p["Defined"] {
	case "You", "Player", "Opponent", "Player.Opponent", "TargetedPlayer":
		return true
	}
	return false
}

// splitSpellP2E: a granted spell rule ("spells you cast can't be
// countered") is one static of its own (splitStatics), as a script writes
// it beside a cost static in one mtgish PlayerEffect list.
func splitSpellP2E(t string) bool { return strings.HasPrefix(t, "spell:") }
