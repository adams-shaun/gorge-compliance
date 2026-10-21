package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase-2 unit d: normaliser fixes found triaging the T4 rows of the
// corpus from "Raffine's Guidance" to "Zulaport Enforcer". Each family is
// general (corpus-wide), not per card.

// extReflexive maps the action half of a mtgish "Reflexive_<X>_<When...>"
// action, keyed by the variant with its "Reflexive_" prefix and the
// trigger-condition suffix cut ("Reflexive_Sacrifice_WhenYouDo" is
// "Sacrifice"), for the halves that are an action the player does while
// the ability resolves. Consulted by the core Reflexive_ case.
var extReflexive = map[string]func(e *effects, n node){}

// reflexiveHalf cuts a Reflexive_ variant down to its action half.
func reflexiveHalf(v string) string {
	v = strings.TrimPrefix(v, "Reflexive_")
	if i := strings.Index(v, "_When"); i >= 0 {
		v = v[:i]
	}
	return v
}

// discardableCount is the number of cards a mtgish Discardable discards.
func discardableCount(x any) string {
	switch variantOf(x) {
	case "ACard", "ACardOfType":
		return "1"
	case "NumberCards":
		return gameNumber(rawArgs(x))
	}
	return "*"
}

// p2dNestedCost maps the cost of a reflexive trigger ("you may sacrifice a
// creature. When you do, ...", Forge: an ImmediateTrigger whose Cost$ is
// the sacrifice) to the action paying it does. The player sacrifices or
// discards while the ability resolves, exactly as when a script writes the
// same sentence as a Sacrifice/Discard sub-ability followed by an
// ImmediateTrigger conditioned on it; mtgish writes both as
// Reflexive_Sacrifice/Reflexive_Discard actions (extReflexive).
func p2dNestedCost(a *cards.SA) []string {
	var out []string
	if a.API == "ImmediateTrigger" || strings.HasPrefix(a.Params["Cost"], "Mandatory ") {
		// "Mandatory": a trigger's "sacrifice it. If you do, ..." (mtgish
		// MustCost).
		out = costActions(a.Params["Cost"])
	}
	if a.Params["UnlessSwitched"] == "True" && a.API == "ImmediateTrigger" {
		// "UnlessCost$ X | UnlessSwitched$ True": the trigger happens only
		// if X is paid, the same reflexive cost.
		out = append(out, costActions(a.Params["UnlessCost"])...)
	}
	return out
}

// p2dExileCostZone is the zone each Forge exile cost exiles from.
var p2dExileCostZone = map[string]string{
	"Exile": "battlefield", "ExileFromGrave": "graveyard", "ExileAnyGrave": "graveyard",
	"ExileSameGrave": "graveyard", "ExileFromHand": "hand", "ExileFromTop": "library",
	"ExileFromStack": "stack",
}

// costActions maps the sacrifice, exile and discard parts of a Forge cost
// string.
func costActions(cost string) []string {
	var out []string
	for _, part := range strings.Fields(cost) {
		switch {
		case strings.HasPrefix(part, "Sac<"):
			out = append(out, "sacrifice")
		case strings.HasPrefix(part, "Exile"):
			if z, ok := p2dExileCostZone[strings.SplitN(part, "<", 2)[0]]; ok {
				out = append(out, "move:"+z+">exile")
			}
		case strings.HasPrefix(part, "Discard<"):
			n := strings.SplitN(strings.TrimPrefix(part, "Discard<"), "/", 2)[0]
			out = append(out, "discard:"+literalOr(n, "*"))
		}
	}
	return out
}

func init() {
	// "Protection from red. This effect doesn't remove <this Aura>": the
	// protection keyword (Forge: Protection:Card.Red:red:<exception>).
	extRule["ProtectionAndDoesntRemovePermanents"] = func(m *mtgishNorm, r any) {
		for _, q := range strings.Split(protectable(argN(r, 0)), "|") {
			m.kw("protection:" + q)
		}
	}
	// "A card from your hand or graveyard": one card from one of the two
	// zones, both moves as alternatives (each once), as the gorge side's
	// Origin$ Hand,Graveyard (p2dChangeZone). These override map_c2.go's
	// hand-only reading.
	for _, v := range []string{
		"MayPutACardFromHandOrGraveyardOnBattlefieldForEachPermanent",
		"PutAnyNumberOfCardsFromHandOrFromPlayersGraveyardOnBattlefield",
		"PutUptoNumberCardsFromHandAndOrGraveyardOnBattlefield",
	} {
		extAction[v] = func(e *effects, n node) { e.add("move:hand>battlefield", "move:graveyard>battlefield") }
	}
	extReflexive["Sacrifice"] = func(e *effects, n node) { e.add("sacrifice") }
	extReflexive["PlayerSacrifices"] = func(e *effects, n node) { e.add("sacrifice") }
	// "You may pay {2} and sacrifice an Aura": the mana is paid, the
	// sacrifice done.
	extReflexive["PayMana_Sacrifice"] = func(e *effects, n node) { e.add("sacrifice") }
	extReflexive["Discard_Draw"] = func(e *effects, n node) {
		e.add("discard:"+discardableCount(argN(n, 0)), "draw:"+drawableCount(argN(n, 1)))
	}
	extReflexive["Discard"] = func(e *effects, n node) { e.add("discard:" + discardableCount(argN(n, 0))) }
	// "You may exile a creature card from your graveyard. When you do":
	// the exile, as the Exile action.
	extReflexive["Exile"] = func(e *effects, n node) {
		for _, x := range list(argN(n, 0)) {
			e.add("move:" + exilableZone(x) + ">exile")
		}
	}
}

// drawableCount is the number of cards a mtgish Drawable draws.
func drawableCount(x any) string {
	switch variantOf(x) {
	case "ACard":
		return "1"
	case "NumberCards":
		return gameNumber(rawArgs(x))
	}
	return "*"
}

// p2dChangeZone maps a ChangeZone. One whose Origin$ is a p2dOriginChoice
// ("you choose a card from their hand or graveyard") moves one card from
// one of them: the moves are alternatives, as mtgish's ChooseAnAction
// branches are, so each origin's move counts once. Other origin lists are
// not alternatives (a "Graveyard,Exile" origin is a milled card's last
// known zone; a list with the library is a search, changeZoneToken).
// p2dOriginChoice are the Origin$ lists a single ChangeZone picks one card
// from ("from your hand or graveyard", "from your hand or the command
// zone").
var p2dOriginChoice = map[string]bool{
	"Hand,Graveyard": true, "Graveyard,Hand": true, "Hand,Command": true, "Command,Hand": true,
}

func p2dChangeZone(api string, p map[string]string) []string {
	zs := strings.Split(p["Origin"], ",")
	if o := strings.ReplaceAll(p["Origin"], " ", ""); api != "ChangeZone" || !p2dOriginChoice[o] {
		return []string{changeZoneToken(api, p)}
	}
	var out []string
	seen := map[string]bool{}
	for _, z := range zs {
		q := make(map[string]string, len(p))
		for k, v := range p {
			q[k] = v
		}
		q["Origin"] = strings.TrimSpace(z)
		if t := changeZoneToken(api, q); !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// p2dDigRevealOnly reports a Dig that reveals the top cards and leaves
// them where they are ("reveal the top card of your library": every card
// back on top, LibraryPosition$ 0): a look, not a move, as mtgish's
// RevealTopCardOfLibrary.
func p2dDigRevealOnly(p map[string]string) bool {
	if p["Optional"] == "True" {
		// "Put it on top or bottom": the bottom is a move.
		return false
	}
	all := p["ChangeNum"] == "All" || literalOr(p["ChangeNum"], "1") == literalOr(p["DigNum"], "1")
	return all && p["DestinationZone"] == "Library" && p["LibraryPosition"] == "0" && p["DestinationZone2"] == ""
}

// p2dDigUntilShuffledBack reports a DigUntil whose revealed (or found)
// cards go back into the library that is then shuffled ("then shuffle"):
// a shuffle, not a move, as mtgish's PutTheRemainingCardsBackIntoLibrary
// AndShuffle. Without Shuffle$ they go to the bottom, a move.
func p2dDigUntilShuffledBack(p map[string]string, key string) bool {
	return p[key] == "Library" && p["Shuffle"] == "True"
}

// p2dDigRestStays reports a Dig whose unchosen cards need no move of
// their own: they go back into the library that a following Shuffle
// shuffles ("put any number onto the battlefield, then shuffle", mtgish's
// PutTheRemainingCardsBackIntoLibraryAndShuffle).
func p2dDigRestStays(a *cards.SA, dest, rest string) bool {
	return rest == "Library" && dest != "Library" && a.Sub != nil && a.Sub.API == "Shuffle"
}

// p2dSameHeadGrant reads a granted rule that summarises to several
// keywords of one head ("protection from red and from blue" is one
// Protection rule over two colours, two protection keywords) as a grant of
// that keyword, as the gorge side's AddKeyword$ of each.
func p2dSameHeadGrant(s *Summary) (string, bool) {
	if len(s.Keywords) < 2 || len(s.Abilities) != 0 {
		return "", false
	}
	h := keywordHeadToken(s.Keywords[0])
	for _, k := range s.Keywords[1:] {
		if keywordHeadToken(k) != h {
			return "", false
		}
	}
	return "grant:" + h, true
}

// p2dPlayerGrants respells the keyword grants of a Pump or Protection
// whose recipient is a player ("you gain protection from everything",
// "you gain hexproof") as player rules, as mtgish writes them
// (player:protection, player:hexproof).
func p2dPlayerGrants(a *cards.SA, toks []string) []string {
	switch a.API {
	case "Pump", "PumpAll", "Protection", "ProtectionAll":
	default:
		return toks
	}
	if !p2dPlayerRecipient(a.Params) {
		return toks
	}
	out := make([]string, len(toks))
	for i, t := range toks {
		if k, ok := strings.CutPrefix(t, "grant:"); ok {
			t = "player:" + k
		}
		out[i] = t
	}
	return out
}

// p2dPlayerRecipient reports an SA that affects players only.
func p2dPlayerRecipient(p map[string]string) bool {
	if p["ValidCards"] != "" {
		// PumpAll over the creatures a player controls.
		return false
	}
	if p["ValidPlayers"] != "" {
		return true
	}
	if vt := p["ValidTgts"]; vt != "" {
		return c2PlayerTargets(vt)
	}
	switch p["Defined"] {
	case "You", "Player", "Opponent", "Player.Opponent", "TargetedPlayer":
		return true
	}
	return false
}

// Ability post-passes: spelling differences of one effect token, resolved
// once per ability as both normalisers emit it.

// p2dMtgishAlias respells mtgish-side effect tokens onto the gorge side's
// spelling of the same thing.
var p2dMtgishAlias = map[string]string{
	// One replacement event, whatever creates the tokens (Forge:
	// R:Event$ CreateToken).
	"replace:aplayerwouldcreatetokens":        "replace:aneffectwouldcreateanynumberoftokens",
	"replace:aplayerwouldcreateatoken":        "replace:aneffectwouldcreateanynumberoftokens",
	"replace:anynumberoftokenswouldbecreated": "replace:aneffectwouldcreateanynumberoftokens",
	// "You may cast spells by paying <cost> rather than their mana cost"
	// (Forge: S:Mode$ AlternativeCost).
	"player:maypayalternatecostforspells": "alternativecost",
	// A spell-cost change written as a stack-spell effect (Forge:
	// ReduceCost/RaiseCost statics), as spellEffectTokens maps it.
	"spell:decreasemanacost":        "costreduction",
	"spell:decreasemanacostforeach": "costreduction",
	"spell:increasemanacost":        "costincrease",
	// "<creatures> attack <that player> each combat if able" / "target
	// player's creatures attack each combat if able": the attack
	// requirement Forge writes as MustAttack.
	"rule:mustattackplayer":             "rule:mustattack",
	"player:mustattackwitheachcreature": "rule:mustattack",
}

// p2dGorgeAlias respells gorge-side effect tokens onto mtgish's spelling.
var p2dGorgeAlias = map[string]string{
	// Forge's player keyword UntapAdjust ("players can't untap more than
	// one land during their untap steps").
	"player:untapadjust": "player:cantuntapmorethannumberpermanentsduringtheiruntapstep",
}

func p2dAlias(toks []string, alias map[string]string) []string {
	var out []string
	for i, t := range toks {
		if a, ok := alias[t]; ok {
			if out == nil {
				out = append([]string(nil), toks...)
			}
			out[i] = a
		}
	}
	if out == nil {
		return toks
	}
	return out
}

func p2dMtgishAbility(a Ability) Ability {
	a.Effects = p2dAlias(a.Effects, p2dMtgishAlias)
	a.Effects = p2dPairTargetRules(a.Effects)
	return a
}

func p2dGorgeAbility(a Ability) Ability {
	a.Effects = p2dAlias(a.Effects, p2dGorgeAlias)
	return a
}

// p2dPairTargetRules folds "can't be the target of <spells>" and "can't be
// the target of <abilities>" in one ability ("white spells or abilities
// from white sources", two mtgish rules) into the one rule a script's
// CantTarget static over both is.
func p2dPairTargetRules(toks []string) []string {
	const sp, ab, both = "rule:cantbethetargetofspells", "rule:cantbethetargetofabilities", "rule:cantbethetargetofspellsorabilities"
	ns, na := 0, 0
	for _, t := range toks {
		switch t {
		case sp:
			ns++
		case ab:
			na++
		}
	}
	pairs := min(ns, na)
	if pairs == 0 {
		return toks
	}
	var out []string
	ds, da := pairs, pairs
	for _, t := range toks {
		switch {
		case t == sp && ds > 0:
			ds--
			out = append(out, both)
		case t == ab && da > 0:
			da--
		default:
			out = append(out, t)
		}
	}
	return out
}

// p2dEachCardEffects maps "each card in your graveyard (hand, library) has
// <ability>" statics by what they give the cards, as the gorge side maps
// the Continuous static over that zone: a granted ability is its grant
// token ("grant:escape"), losing abilities and gaining a creature type as
// in a layer effect. The other card effects keep the static token.
func p2dEachCardEffects(v string, r any) ([]string, bool) {
	switch v {
	case "EachCardInPlayersLibraryEffect", "EachCardInPlayersHandEffect", "EachCardInEachPlayersHandEffect",
		"EachCardInGraveyardEffect", "EachCardInAGraveyardEffect":
	default:
		return nil, false
	}
	var effs []any
	for _, a := range argsOf(r) {
		if l := list(a); l != nil {
			effs = l
		}
	}
	if len(effs) == 0 {
		return nil, false
	}
	var out []string
	for _, x := range effs {
		switch variantOf(x) {
		case "AddAbility":
			for _, rr := range argsOf(x) {
				if enumOf(rr) == "Rule" {
					out = append(out, grantToken(rr))
				}
			}
		case "LosesAllAbilities":
			out = append(out, "loseabilities")
		case "AddCreatureTypeVariable":
			out = append(out, "type")
		default:
			return nil, false
		}
	}
	return dedup(out), len(out) > 0
}

// p2dFoldTargetStatics folds a face's two one-rule statics "can't be the
// target of <spells>" and "can't be the target of <abilities>" (mtgish
// writes "white spells or abilities from white sources" as two rules) into
// the one static a script's CantTarget over both is.
func p2dFoldTargetStatics(s *Summary) {
	const sp, ab, both = "rule:cantbethetargetofspells", "rule:cantbethetargetofabilities", "rule:cantbethetargetofspellsorabilities"
	one := func(a Ability, t string) bool {
		return a.Kind == "static" && len(a.Effects) == 1 && a.Effects[0] == t
	}
	is, ia := -1, -1
	for i, a := range s.Abilities {
		if is < 0 && one(a, sp) {
			is = i
		}
		if ia < 0 && one(a, ab) {
			ia = i
		}
	}
	if is < 0 || ia < 0 {
		return
	}
	var out []Ability
	for i, a := range s.Abilities {
		switch i {
		case is:
			a.Effects = []string{both}
		case ia:
			continue
		}
		out = append(out, a)
	}
	s.Abilities = out
}

// p2dAnimateExtra maps the Animate parameters the core case does not:
// losing keywords is losing abilities, gaining every creature type or
// losing a type is a type change (mtgish's layer effects).
func p2dAnimateExtra(p map[string]string) []string {
	var out []string
	if p["RemoveKeywords"] != "" {
		out = append(out, "loseabilities")
	}
	if p["AddAllCreatureTypes"] == "True" || p["RemoveTypes"] != "" || p["RemoveCreatureTypes"] == "True" {
		out = append(out, "type")
	}
	return out
}

// p2dAnimateCarrier reports an Animate that changes nothing about the
// object: it only carries a leave-the-battlefield replacement (mapped as
// "effect" by the chain) or an AI hint (sVars$).
func p2dAnimateCarrier(p map[string]string) bool {
	return p["LeaveBattlefield"] != "" || p["sVars"] != ""
}

// A cost a player must pay ("sacrifice it. If you do, ...", "discard a
// card. If you do, ...") is the action paying it does, as a script's
// Sacrifice/Discard sub-ability or its "Cost$ Mandatory ..." (p2dNestedCost).
// A cost the player may pay (MayCost) stays a cost: scripts write it as an
// UnlessCost as often as an optional action. A mana or energy payment is
// never an effect.
func init() {
	delete(wrapperActions, "MustCost")
	extAction["MustCost"] = func(e *effects, n node) {
		if !strings.HasPrefix(variantOf(rawArgs(n)), "Pay") {
			e.costAsAction(rawArgs(n))
		}
	}
	// Cost variants met as must-costs, mapped as the action they are.
	extAction["UnspecializeGraveyardCard"] = func(e *effects, n node) { e.add("setstate:unspecialize") }
	extAction["AnteAPermanent"] = func(e *effects, n node) { e.add("move:battlefield>ante") }
	extAction["ShuffleACardFromHandIntoLibrary"] = func(e *effects, n node) { e.add("move:hand>library:1") }
}

// p2dPlayerSpellEffects maps "<spells you cast (from your hand, from exile,
// the Nth each turn)> have <ability>" player effects by what the spells
// get, as the gorge side's Continuous static over the stack does
// ("spell:cascade"). The SpellEffect list is the variant's last argument.
func p2dPlayerSpellEffects(x any) ([]string, bool) {
	switch variantOf(x) {
	case "SpellsCastFromHandHaveAbility", "SpellsCastFromExileHaveAbility", "SpellsCastHaveAbility",
		"SpellsControlledHaveAbility", "TheNthSpellCastEachTurnHasAbility":
	default:
		return nil, false
	}
	a := argsOf(x)
	if len(a) == 0 {
		return nil, false
	}
	t := spellEffectTokens(list(a[len(a)-1]))
	return t, len(t) > 0
}
