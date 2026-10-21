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

func init() {
	// "You may pay <cost> rather than pay the mana cost for ...": an
	// alternative cost (gorge S:Mode$ AlternativeCost, or a MayPlay with
	// MayPlayAltManaCost$ and no zone permission, mayPlayP2E).
	for _, v := range []string{"MayPayAlternateCostForSpells", "MayPayAlternateCostForASpellOnceEachTurn",
		"MayPayAlternateCostForFirstPowerUpCostEachTurn", "MayPayAlternateCostForFirstCyclingCostEachTurn"} {
		d2PlayerSynonyms[v] = "alternativecost"
	}
}

// gorgeAPIP2E maps the SA shapes unit e found rendered with the wrong
// verb; ok false falls through to gorgeAPIEffects' switch.
func gorgeAPIP2E(a *cards.SA) (out []string, ok bool) {
	p := a.Params
	switch a.API {
	case "Dig":
		if p["DigNum"] == "1" && p["DestinationZone"] == "Library" && p["LibraryPosition"] == "-1" &&
			p["Reveal"] != "True" && (p["ChangeNum"] == "" || p["ChangeNum"] == "1" || p["ChangeNum"] == "All") {
			// "Put that card on the bottom of your library": the top card
			// already seen moves; nothing new is looked at.
			return []string{"move:library>library"}, true
		}
	case "DelayedTrigger":
		if p["Mode"] == "SpellCast" && p["ThisTurn"] == "True" && p["Static"] == "True" {
			// "The next <kind> spell you cast this turn <gets X>": the
			// spell's rider (Execute), mtgish's CreateFutureSpellEffect;
			// no delayed trigger is printed.
			return nil, true
		}
	}
	return nil, false
}

// gorgeAPIExtraP2E adds the tokens of parameters gorgeAPIEffects does not
// read: a Clone that enters tapped, a face-down exile you may look at.
func gorgeAPIExtraP2E(a *cards.SA) []string {
	p := a.Params
	var out []string
	if a.API == "Clone" && p["IntoPlayTapped"] == "True" {
		out = append(out, "etbtapped")
	}
	if a.API == "Dig" && p["WithMayLook"] == "True" {
		out = append(out, "player:maylookatfacedownexiledcards")
	}
	return out
}

// mayPlayP2E maps a Continuous MayPlay$: "you may cast it by paying <cost>
// rather than its mana cost" with no zone permission is an alternative
// cost; "as though it had flash" is flash (mtgish's ...AsThoughTheyHadFlash
// player effects).
func mayPlayP2E(p map[string]string) string {
	switch {
	case p["MayPlayAltManaCost"] != "" && p["MayPlayDontGrantZonePermissions"] == "True":
		return "alternativecost"
	case p["MayPlayWithFlash"] == "True":
		return "flash"
	}
	return "mayplay"
}

// spellEffectP2E maps the rules a CreateSpellEffect gives a spell ("target
// spell can't be countered", "becomes the colour of your choice",
// "gains <keyword>"), like a permanent's layer effect.
func spellEffectP2E(rs []any) []string {
	var out []string
	for _, r := range rs {
		v := variantOf(r)
		if strings.HasPrefix(v, "CantBeCountered") {
			out = append(out, "spell:cantbecountered")
			continue
		}
		t := layerEffects([]any{r})
		for _, x := range t {
			if IsUnmapped(x) {
				t = []string{"effect"}
				break
			}
		}
		out = append(out, t...)
	}
	if len(out) == 0 {
		return []string{"effect"}
	}
	return dedup(out)
}

// lookActionP2E adjusts a look whose card is then moved: "look at the top
// card of a library, then reveal it and put it into your hand" or "...then
// exile it" is one look (or none, for the exile: gorge's Dig to exile is a
// move). It returns true when it emitted the action itself.
func (e *effects) lookActionP2E(n node, v string) bool {
	last := len(e.effects) - 1
	prevLook := last >= 0 && e.effects[last] == "dig:1"
	switch {
	case v == "RevealTopCardOfLibraryAndPutIntoHand" && prevLook:
		e.add("move:library>hand")
		return true
	case v == "Exile" && prevLook && strings.Contains(canonicalJSON(n), "TheTopCardOfPlayersLibrary"):
		e.effects = e.effects[:last]
	}
	return false
}

// selfEffectP2E: "This card is the colour of your choice / your
// commander's colours" (mtgish SelfEffect SetColor) is a characteristic-
// defining colour, gorge's CharacteristicDefining static.
func selfEffectP2E(v string, a []any) []string {
	if v != "SelfEffect" || len(a) == 0 {
		return nil
	}
	rs := list(a[len(a)-1])
	if len(rs) == 0 {
		return nil
	}
	for _, r := range rs {
		if variantOf(r) != "SetColor" {
			return nil
		}
	}
	return []string{"cda"}
}

// dedupPreventP2E: one Effect's prevention shields ("prevent all damage
// that would be dealt to and dealt by it") are one prevention effect, as
// mtgish's one CreatePreventDamageUntil with an Or of both events.
func dedupPreventP2E(ts []string) []string {
	var out []string
	seen := false
	for _, t := range ts {
		if t == "prevent" {
			if seen {
				continue
			}
			seen = true
		}
		out = append(out, t)
	}
	return out
}
