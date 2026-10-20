package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit B: the gorge-side effect families that fell through to an unmapped
// token -- SA APIs, replacement events, phase trigger classes, hidden
// keywords -- and the counter-type spelling of both sides.
//
// A new token follows the shared naming convention, the lowercased Forge
// API name, so the mtgish-side mapping of the same action converges on it.

// Gorge-side hooks consulted by the core switches (added by unit B).
var (
	// extSVarRefs names the SVars an API runs besides SubAbility$ (its
	// outcomes, choices and win/lose branches).
	extSVarRefs = map[string]func(a *cards.SA) []string{}
	// extReplStatic maps a replacement that is printed as a static rule
	// ("skip your draw step", "unspent mana becomes colorless"). A nil
	// result falls through to the replacement path.
	extReplStatic = map[string]func(r *cards.Repl) []string{}
)

// apiTokenB is a one-token API mapping.
type apiTokenB struct{ api, tok string }

// simpleAPIB maps APIs that are one game action each. Where the mtgish side
// already has a token for the action it is used; otherwise the token is the
// lowercased API name.
var simpleAPIB = []apiTokenB{
	// Keyword actions and named game actions.
	{"Explore", "explore"},
	{"Earthbend", "earthbend"},
	{"Airbend", "airbend"},
	{"Venture", "venture"},
	{"Manifest", "manifest"},
	{"ManifestDread", "manifestdread"},
	{"Cloak", "cloak"},
	{"Incubate", "incubate"},
	{"Discover", "discover"},
	{"Clash", "clash"},
	{"Learn", "learn"},
	{"Endure", "endure"},
	{"Detain", "detain"},
	{"Recruit", "recruit"},
	{"Heist", "heist"},
	{"Blight", "blight"},
	{"TimeTravel", "timetravel"},
	{"TakeInitiative", "takeinitiative"},
	{"Vote", "vote"},
	{"VillainousChoice", "villainouschoice"},
	{"AssignGroup", "assigngroup"},
	{"BidLife", "bidlife"},
	{"DayTime", "daytime"},
	{"UnlockDoor", "unlockdoor"},
	{"ChangeSpeed", "changespeed"},
	// Digital-only (Alchemy) actions: still one comparable action each.
	{"Draft", "draft"},
	{"Empower", "empower"},
	{"Intensify", "intensify"},
	// Life, control and ownership.
	{"SetLife", "setlife"},
	{"ExchangeLife", "exchangelife"},
	{"ExchangeLifeVariant", "exchangelifevariant"},
	{"ExchangeControl", "exchangecontrol"},
	{"ExchangeControlVariant", "exchangecontrolvariant"},
	{"GainControlVariant", "gaincontrol"},
	{"ControlPlayer", "controlplayer"},
	{"ControlSpell", "controlspell"},
	{"GainOwnership", "gainownership"},
	{"ExchangePower", "exchangepower"},
	{"ExchangeTextBox", "exchangetextbox"},
	{"ExchangeZone", "exchangezone"},
	{"ChangeText", "changetext"},
	{"ChangeX", "changex"},
	// Turn structure.
	{"SkipPhase", "skipphase"},
	{"SkipTurn", "skipturn"},
	{"ReverseTurnOrder", "reverseturnorder"},
	{"EndCombatPhase", "endcombatphase"},
	// Combat.
	{"RemoveFromCombat", "removefromcombat"},
	{"ChangeCombatants", "changecombatants"},
	{"BecomesBlocked", "becomesblocked"},
	{"Block", "block"},
	{"SwitchBlock", "switchblock"},
	{"Fog", "prevent"}, // "prevent all combat damage", as mtgish's prevention action
	// Permanents, counters, cards.
	{"MoveCounter", "movecounter"},
	{"Unattach", "unattach"},
	{"LookAt", "lookat"},
	{"ReorderZone", "reorderzone"},
	{"MultiplePiles", "multiplepiles"},
	{"ActivateAbility", "activateability"},
	{"DrainMana", "drainmana"},
	{"TaporUntapAll", "tapuntap"}, // as TapOrUntapAll
	// Game-level.
	{"GameDrawn", "gamedrawn"},
	{"RestartGame", "restartgame"},
	{"Subgame", "subgame"},
	// Supplemental formats (Un-sets, Archenemy, Planechase): each still one
	// comparable action on both sides, so mapped rather than excluded.
	{"PutSticker", "putsticker"},
	{"OpenAttraction", "openattraction"},
	{"Abandon", "abandon"},
	{"SetInMotion", "setinmotion"},
	{"Planeswalk", "planeswalk"},
	{"ChaosEnsues", "chaosensues"},
	{"RunChaos", "runchaos"},
	{"RollPlanarDice", "dice"}, // a die roll, as mtgish's Roll* actions
}

// reflexiveB maps the action half of mtgish's "Reflexive_<X>_WhenYouDo"
// ("you may earthbend 2. When you do, ...") for the actions this unit's
// gorge tokens name as effects, which the core case otherwise drops as a
// cost. (A blight "When you do" stays a cost on both sides: scripts write
// it as an ImmediateTrigger's Cost$, mtgish mostly as a MayCost.)
var reflexiveB = map[string]string{
	"Earthbend":                  "earthbend",
	"PutANameStickerOnPermanent": "putsticker",
	"DraftACardFromSpellBook":    "draft",
}

// noEffectAPIB are APIs that are not game effects: a choice whose result a
// later effect reads, or a layout spacer. They map to nothing; the mtgish
// side's matching choices (ChooseLeftOrRight, ChooseEvenOrOdd, ChooseASector)
// must map to nothing too.
var noEffectAPIB = []string{"ChooseDirection", "ChooseEvenOdd", "ChooseSector", "BlankLine"}

// replTokenB maps replacement events compared by the event they replace,
// spelled as mtgish's Replace<Event> rule.
var replTokenB = []apiTokenB{
	{"ProduceMana", "replace:wouldproducemana"},
	{"BeginTurn", "replace:wouldbeginaturn"},
	{"Destroy", "replace:woulddestroy"},
	{"RollDice", "replace:wouldrolldice"},
	{"DrawCards", "replace:woulddraw"},
	{"Explore", "replace:wouldexplore"},
	{"Mill", "replace:wouldmill"},
	{"Scry", "replace:wouldscry"},
	{"Connive", "replace:wouldconnive"},
	{"CopySpell", "replace:wouldcopyaspell"},
	{"Learn", "replace:wouldlearn"},
	{"PayLife", "replace:wouldpaylife"},
	{"Planeswalk", "replace:wouldplaneswalk"},
	{"Proliferate", "replace:wouldproliferate"},
	{"RollPlanarDice", "replace:wouldrollplanardice"},
}

func init() {
	for _, m := range simpleAPIB {
		tok := m.tok
		extAPI[m.api] = func(*cards.SA, map[string]string) []string { return []string{tok} }
	}
	for _, api := range noEffectAPIB {
		extAPI[api] = func(*cards.SA, map[string]string) []string { return nil }
	}
	extAPI["AlterAttribute"] = alterAttributeB
	extAPI["RearrangeTopOfLibrary"] = func(a *cards.SA, _ map[string]string) []string {
		// "Look at the top N cards and put them back in any order."
		return []string{"dig:" + literalOr(a.Params["NumCards"], "1"), "move:library>library"}
	}
	extAPI["DigMultiple"] = digMultipleB
	extAPI["MustBlock"] = func(a *cards.SA, _ map[string]string) []string {
		if a.Params["BlockAllDefined"] == "True" {
			return []string{"rule:mustblockeachattacker"}
		}
		return []string{"rule:mustblockattacker"}
	}
	extAPI["Radiation"] = func(a *cards.SA, _ map[string]string) []string {
		// "Get N rad counters": a player counter, as mtgish writes it.
		return []string{"counters:rad:" + literalOr(a.Params["Num"], "1")}
	}
	extAPI["Balance"] = func(a *cards.SA, _ map[string]string) []string {
		// Each player discards down to / sacrifices down to the fewest.
		if a.Params["Zone"] == "Hand" {
			return []string{"discard:*"}
		}
		return []string{"sacrifice"}
	}
	extAPI["AddOrRemoveCounter"] = func(a *cards.SA, _ map[string]string) []string {
		// "Put a counter or remove one": both verbs, as mtgish's two
		// alternatives merge.
		return append(gorgeCounterTokens(a.Params["CounterType"], literalOr(a.Params["CounterNum"], "1")), "countersremove")
	}
	extAPI["Meld"] = func(*cards.SA, map[string]string) []string {
		// "Exile them, then meld them": two permanents exiled, one melded.
		return []string{"move:battlefield>exile", "move:battlefield>exile", "meld"}
	}

	// The SVars these APIs run: every outcome is summarised, additively, as
	// mtgish's walker descends into each nested option.
	choices := func(a *cards.SA) []string { return cards.SplitModeNames(a.Params["Choices"]) }
	extSVarRefs["Vote"] = func(a *cards.SA) []string {
		// The tie outcome is usually one of the choices: once.
		return dedup(append(choices(a), a.Params["VoteSubAbility"], a.Params["VoteTiedAbility"]))
	}
	extSVarRefs["VillainousChoice"] = choices
	extSVarRefs["AssignGroup"] = choices
	extSVarRefs["BidLife"] = func(a *cards.SA) []string { return []string{a.Params["BidSubAbility"]} }
	extSVarRefs["Clash"] = func(a *cards.SA) []string {
		return []string{a.Params["WinSubAbility"], a.Params["OtherwiseSubAbility"]}
	}

	for _, m := range replTokenB {
		tok := m.tok
		extRepl[m.api] = func(*cards.Repl) []string { return []string{tok} }
	}
	extRepl["BeginPhase"] = func(r *cards.Repl) []string {
		// "If you would begin your draw step" (the skip form is static).
		return []string{"replace:wouldbegin" + phaseStepB(r.Params["Phase"])}
	}
	extRepl["LifeReduced"] = func(r *cards.Repl) []string {
		if r.Params["Result"] != "" {
			// "If your life total would be reduced to 0 or less".
			return []string{"replace:wouldreducelife"}
		}
		return []string{"replace:wouldloselife"}
	}
	extRepl["RemoveCounter"] = func(r *cards.Repl) []string {
		// Damage removing loyalty counters, compared as the damage event.
		return []string{"replace:woulddealdamage"}
	}
	// "As this is turned face up / transforms / becomes attached, ...":
	// compared by what the replacement does, like an "as this enters".
	for _, ev := range []string{"TurnFaceUp", "Transform", "Attached", "Moved"} {
		extRepl[ev] = replWithB
	}

	extReplStatic["BeginPhase"] = func(r *cards.Repl) []string {
		if r.Params["Skip"] != "True" {
			return nil
		}
		return []string{"player:skips" + phaseStepB(r.Params["Phase"])}
	}
	extReplStatic["LoseMana"] = func(r *cards.Repl) []string {
		// "If you would lose unspent mana, that mana becomes <colour>
		// instead"; the colour is the ReplaceMana's ReplaceType$.
		if r.With != nil && r.With.Params["ReplaceType"] == "C" {
			return []string{"player:unspentmanabecomescolorless"}
		}
		return []string{"player:unspentmanabecomescolor"}
	}
	extReplStatic["LifeReduced"] = func(r *cards.Repl) []string {
		p := r.Params
		switch {
		case p["IsDamage"] != "True":
			return nil
		case p["Layer"] == "CantHappen" && p["ReplaceWith"] == "":
			return []string{"player:damagedoesntcauselifeloss"}
		case strings.HasPrefix(p["Result"], "LT"):
			return []string{"player:damagethatwouldreducelifetotaltolessthannumberreducesittothatnumberinstead"}
		}
		return nil
	}
	extReplStatic["RemoveCounter"] = func(r *cards.Repl) []string {
		if r.Params["Layer"] == "CantHappen" && r.Params["ReplaceWith"] == "" {
			return []string{"rule:canthavecountersoftyperemovedfromit"}
		}
		return nil
	}
	extReplStatic["TurnFaceUp"] = func(r *cards.Repl) []string {
		if r.Params["Layer"] == "CantHappen" && r.Params["ReplaceWith"] == "" {
			return []string{"rule:cantbeturnedfaceup"}
		}
		return nil
	}
	extReplStatic["Cascade"] = func(*cards.Repl) []string {
		// "As you cascade, you may put a land ... onto the battlefield":
		// mtgish's player effect, spelled as mtgish spells it.
		return []string{"player:astheycascadetheymayputacardfromamongexiledcardsonbattlefiled"}
	}
}

// alterAttributeB maps AlterAttribute. Every attribute it sets (suspected,
// prepared, saddled, harnessed, plotted, commander) is one action, except a
// Case's "solved": mtgish writes a Case's solve condition as the structure of
// its Mystery rule, not as an action, so it maps to nothing on both sides.
func alterAttributeB(a *cards.SA, _ map[string]string) []string {
	if strings.EqualFold(a.Params["Attributes"], "Solved") {
		return nil
	}
	return []string{"alterattribute"}
}

// digMultipleB maps "look at the top N, put any number of the matching
// cards into <zone>, the rest into <zone2>".
func digMultipleB(a *cards.SA, _ map[string]string) []string {
	p := a.Params
	dest := p["DestinationZone"]
	if dest == "" {
		dest = "Hand"
	}
	rest := p["DestinationZone2"]
	if rest == "" {
		rest = "Library"
	}
	return []string{
		"dig:" + literalOr(p["DigNum"], "1"),
		"move:library>" + strings.ToLower(dest),
		"move:library>" + strings.ToLower(rest),
	}
}

// replWithB summarises a replacement by the effects of its ReplaceWith$
// chain.
func replWithB(r *cards.Repl) []string {
	out := []string{}
	for a, n := r.With, 0; a != nil && n < maxChainDepth; a, n = a.Sub, n+1 {
		out = append(out, gorgeAPIEffects(a, nil)...)
	}
	return out
}

// phaseStepB spells a Forge Phase$ step as mtgish's player effects do:
// "Draw" -> "drawstep".
func phaseStepB(phase string) string {
	return strings.ToLower(phase) + "step"
}

// phaseClassB maps the Phase$ trigger values the core switch misses; ""
// when it has no class.
func phaseClassB(p map[string]string) string {
	switch {
	case strings.EqualFold(p["Phase"], "End of Turn"):
		return "end-step"
	case p["Phase"] == "" && (p["FirstUpkeep"] == "True" || p["FirstUpkeepThisGame"] == "True"):
		return "upkeep"
	case p["Phase"] == "EndCombat":
		return "endcombat"
	}
	return ""
}

// skipTriggerB reports a trigger that is engine bookkeeping, not a printed
// ability: a Static$ trigger whose whole chain has no effect and no target
// (it only remembers, forgets or cleans up state for a printed ability,
// such as a Phase$ Cleanup reset or a DamageDone that remembers a source).
func skipTriggerB(t *cards.Trigger, e *effects) bool {
	return t.Params["Static"] == "True" && len(e.effects) == 0 && len(e.targets) == 0
}

// hiddenB maps the hidden keywords the core switch misses; "" when unknown.
func hiddenB(k string) string {
	switch {
	case strings.HasPrefix(k, "All creatures able to block"):
		return "rule:allcreaturesmustblockit"
	case strings.Contains(k, "can only attack alone"):
		return "rule:canonlyattackalone"
	case k == "Unblockable":
		return "rule:cantbeblocked"
	case strings.Contains(k, "power and toughness are switched"):
		return "switchpt"
	case strings.HasPrefix(k, "CARDNAME count as "):
		// "counts as a card named X for spells named X": mtgish's rule
		// CountsAsACardWithNameForSpellsNamed.
		return "rule:countsasacardwithnameforspellsnamed"
	case strings.HasPrefix(k, "Damage that would be dealt to CARDNAME can't be redirected"):
		// One half of mtgish's "can't be prevented or redirected" rule
		// (the other half is a CantPreventDamage static).
		return "rule:damagedealttoitcantbepreventedorredirected"
	case strings.HasPrefix(k, "During your next untap step") && strings.Contains(k, "return CARDNAME to its owner's hand"):
		return "move:battlefield>hand"
	}
	return ""
}

// gorgeCounterTokens renders a Forge CounterType$ with an amount. A comma
// list is a choice of one type ("a +1/+1 or first strike counter"), and an
// "EACH a,b" list puts one of each: either way one token per type, as
// mtgish's per-type alternatives merge.
func gorgeCounterTokens(ct, n string) []string {
	ct = strings.TrimSpace(ct)
	ct = strings.TrimPrefix(ct, "EACH ")
	var out []string
	for _, t := range strings.Split(ct, ",") {
		out = append(out, "counters:"+gorgeCounterType(strings.TrimSpace(t))+":"+n)
	}
	return out
}

func digitB(c byte) bool { return c >= '0' && c <= '9' }
