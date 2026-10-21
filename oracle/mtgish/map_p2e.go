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
	// "can be the target of spells and abilities as though it didn't have
	// shroud", "can't phase out", "you can't lose life".
	extStatic["IgnoreShroud"] = fixed("rule:canbethetargetofspellsorabilitiesasthoughitdidnthaveshroud")
	extStatic["CantPhaseOut"] = fixed("rule:cantphaseout")
	extStatic["CantLoseLife"] = fixed("player:cantloselife")
}

// p2ePlayerSynonyms are PlayerEffect variants that are one gorge static
// under another spelling.
var p2ePlayerSynonyms = map[string]string{
	// S:Mode$ CantBeActivated (Activator$, AffectedZone$, ValidSA$ and a
	// non-active-player Activator$ are its filters).
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
	"AssignsCombatDamageOfAttackingCreatures":                     "player:youassigncombatdamageofeachcreatureattackingyou",
	"CanForetellCardsDuringEachPlayersTurn":                       "player:foretellonanyplayersturn",
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

// playerEffectTokensP2E maps one PlayerEffect node wherever a PlayerEffect
// list appears (static, CreatePlayerEffectUntil, emblem): "spells you cast
// have <ability>" is the granted spell ability (p2dPlayerSpellEffects).
func playerEffectTokensP2E(x any) []string {
	if t, ok := p2dPlayerSpellEffects(x); ok {
		return t
	}
	return []string{playerEffectToken(variantOf(x))}
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
