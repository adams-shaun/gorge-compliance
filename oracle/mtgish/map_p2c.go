package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase 2, unit c of the mtgish cross-check: normaliser fixes found
// triaging the T4 rows of the third shard (cards Giant Opportunity ..
// Quintorius, Loremaster). Each fix is a place where both encodings say the
// same thing and the two normalisers rendered it differently.

// p2cReplSynonyms are mtgish Replace* rules that are one Forge
// replacement event: every "create tokens" wording is R:Event$ CreateToken.
var p2cReplSynonyms = map[string]string{
	"ReplaceAnyNumberOfTokensWouldBeCreated": "replace:aneffectwouldcreateanynumberoftokens",
	"ReplaceAPlayerWouldCreateTokens":        "replace:aneffectwouldcreateanynumberoftokens",
	"ReplaceAPlayerWouldCreateAToken":        "replace:aneffectwouldcreateanynumberoftokens",
}

func init() {
	d2PlayerSynonyms["CantUntapMoreThanNumberPermanentsDuringTheirUntapStep"] = "player:untapadjust"
	d2PlayerSynonyms["ChoosesHowCreaturesBlock"] = "player:chooseswhichcreaturesblockandhowtheyblock"
	d2PlayerSynonyms["MayPayAlternateCostForFirstUnearthCostEachTurn"] = "alternativecost"
	// A search step "each opponent who ... loses N life".
	if extAction["EachPlayerLosesLife"] == nil {
		extAction["EachPlayerLosesLife"] = func(e *effects, n node) { e.add("loselife:" + gameNumber(argN(n, 1))) }
	}
	// "Each player secretly chooses a number; the lowest ...": the
	// chosen-number sub-abilities run (Goblin Game's shape).
	if extSVarRefs["ChooseNumber"] == nil {
		extSVarRefs["ChooseNumber"] = func(a *cards.SA) []string {
			return []string{a.Params["ChooseNumberSubAbility"], a.Params["Lowest"], a.Params["Highest"]}
		}
	}
	// "Players can't activate abilities that aren't mana abilities":
	// S:Mode$ CantBeActivated with a non-mana filter.
	d2PlayerSynonyms["CantActivateNonManaAbilities"] = "rule:cantbeactivated"
	// "You may have it assign its combat damage as though it weren't
	// blocked" (the "may" is printed; mtgish's variant drops it).
	d2RuleSynonyms["AssignsCombatDamageAsThoughNotBlocked"] = "rule:mayassigncombatdamageasthoughnotblocked"
	if extAction["PutNumberCardsFromPlayersGraveyardOnBottomOfLibrary"] == nil {
		extAction["PutNumberCardsFromPlayersGraveyardOnBottomOfLibrary"] = func(e *effects, n node) { e.add("move:graveyard>library") }
	}
	// "Can't be blocked unless defending player controls ...": S:Mode$
	// CantBlockBy with a condition, rule:cantbeblocked.
	d2RuleSynonyms["CantBeBlockedUnlessDefendingPlayer"] = "rule:cantbeblocked"
	// "Protection from green. This effect doesn't remove <this Aura>":
	// protection (Forge: Protection keyword with the aura exempted).
	if extRule["ProtectionAndDoesntRemovePermanents"] == nil {
		extRule["ProtectionAndDoesntRemovePermanents"] = func(m *mtgishNorm, r any) {
			for _, q := range strings.Split(protectable(argN(r, 0)), "|") {
				m.kw("protection:" + q)
			}
		}
	}
	// A rule an action gives permanents for a while ("the tokens are
	// goaded"): goading is the goad action (gorge: Goad), not the static
	// rule a card prints.
	for _, k := range []string{"CreateEachPermanentRuleEffect", "CreatePermanentRuleEffect"} {
		if f := extAction[k]; f != nil {
			extAction[k] = func(e *effects, n node) {
				sub := &effects{}
				f(sub, n)
				for _, t := range sub.effects {
					e.add(p2cGoadAction(t))
				}
				e.targets = append(e.targets, sub.targets...)
			}
		}
	}
	// "This creature assigns no combat damage this turn": S:Mode$
	// AssignNoCombatDamage (usually inside an Effect), mtgish's rule.
	if extStatic["AssignNoCombatDamage"] == nil {
		extStatic["AssignNoCombatDamage"] = func(p map[string]string) []string {
			return []string{"rule:assignsnocombatdamage"}
		}
	}
	// "Can't block unless ... attacking player": S:Mode$ CantBlockBy /
	// CantBlock with a condition, as CantAttackUnlessDefendingPlayer is
	// rule:cantattack.
	d2RuleSynonyms["CantBlockUnlessAttackingPlayer"] = "rule:cantblock"
	// "Players can't activate abilities ...": S:Mode$ CantBeActivated.
	d2PlayerSynonyms["CantActivateAbilities"] = "rule:cantbeactivated"
	// "You may pay ... rather than pay the mana cost": an alternative
	// cost static (S:Mode$ AlternativeCost), as castEffectToken's.
	d2PlayerSynonyms["MayPayAlternateCostForSpells"] = "alternativecost"
	// Cost variants a MustCost/MayCost carries (costAsAction falls back
	// to the action of the same name): tapping permanents.
	for _, v := range []string{"TapAPermanent", "TapNumberPermanents", "TapAnyNumberOfPermanents", "TapAnyNumberOfGroupPermanents", "TapNumberGroupPermanents"} {
		if extAction[v] == nil {
			extAction[v] = func(e *effects, n node) { e.add("tap") }
		}
	}
}

// p2cMustCost summarises a cost the player must pay ("sacrifice two
// Swamps. If you can't, ..."; mtgish's MustCost). Paying it is done, so it
// is the action, as EachPlayerMustCost and PlayerMustCost are; a payment of
// mana or energy is a payment and maps to nothing on both sides.
func (e *effects) p2cMustCost(c any) {
	v := variantOf(c)
	switch {
	case strings.HasPrefix(v, "Reveal"), v == "BeholdA":
		// "Look at the top card. You may reveal it": the reveal of a
		// card already seen is not an effect (gorge: RevealOptional$).
		return
	}
	switch v {
	case "PayEnergy", "PayOneOrMoreEnergy":
	case "PayAnyAmountOfLife":
		e.add("loselife:*")
	case "UnspecializeGraveyardCard":
		// Alchemy specialize, undone from the graveyard: gorge's
		// SetState Mode$ Unspecialize.
		e.add("setstate:unspecialize")
	case "AnteAPermanent":
		e.add("move:battlefield>ante")
	case "ShuffleACardFromHandIntoLibrary":
		e.add("move:hand>library:1")
	case "PutAGraveyardCardOnTheBottomOfItsOwnersLibrary":
		e.add("move:graveyard>library")
	case "PutANumberOfExiledCardsIntoOwnersGraveyard":
		e.add("move:exile>graveyard")
	case "PutTopCardOfEachPlayersLibraryInGraveyard":
		e.add("move:library>graveyard")
	case "LookAtPlayersHandAndChooseACardToExile", "LookAtPlayersHandAndChooseACardToExileUntil":
		e.add("move:hand>exile")
	case "UnattachAPermanentFromAPermanent":
		e.add("unattach")
	case "SuspectAPermanent":
		e.add("alterattribute")
	default:
		e.costAsAction(c)
	}
}

// p2cReflexive maps the cost half of mtgish's "Reflexive_<Cost>_<When>"
// ("you may sacrifice a creature. When you do, ..."): an optional cost paid
// while the ability resolves is done, as a MayCost is (p2cMustCost). The
// core Reflexive_ case keeps the keyword-action halves; a mana or energy
// payment stays nothing.
func (e *effects) p2cReflexive(v string, a []any) {
	half := strings.TrimPrefix(v, "Reflexive_")
	if i := strings.IndexByte(half, '_'); i >= 0 {
		half = half[:i]
	}
	switch half {
	case "Sacrifice", "PlayerSacrifices":
		e.add("sacrifice")
	case "Discard":
		e.add("discard:*")
	case "Exile":
		if len(a) > 0 {
			for _, x := range list(a[0]) {
				e.add("move:" + exilableZone(x) + ">exile")
			}
		}
	case "RemoveCounters":
		e.add("countersremove")
	case "Tap":
		e.add("tap")
	case "PutAPermanentInOwnersHand":
		e.add("move:battlefield>hand")
	case "PayLife":
		e.add("loselife:*")
	case "Draw":
		e.add("draw:*")
	case "Amass":
		e.add("amass")
	case "PutOnBattlefield":
		// "You may put any number of Equipment cards from among them
		// onto the battlefield. When you do, ...".
		if len(a) > 0 {
			for _, x := range list(a[0]) {
				if z := p2cCardZone(variantOf(x)); z != "" {
					e.add("move:" + z + ">battlefield")
				}
			}
		}
	case "CreatureConnives":
		e.add("connive")
	case "Surveil":
		e.add("surveil:*")
	}
}

// p2cCost is the gorge side of p2cMustCost and p2cReflexive: a cost paid
// while an ability resolves -- a sub-ability's or reflexive trigger's
// Cost$ ("you may sacrifice it. If you do, ..."; "Mandatory" when it must
// be paid), or an UnlessCost$ its controller pays ("tap it unless you
// sacrifice a creature", "you may discard a card. If you do", written
// UnlessSwitched$) -- is done, so the parts that are actions count as
// actions, as mtgish's MayCost/MustCost/Reflexive_ halves do. An activated
// ability's own Cost$ (depth 0) stays a cost on both sides. An UnlessCost$
// another player pays is mtgish's PlayerMayCost family, which stays a
// cost.
func (g *gorgeNorm) p2cCost(e *effects, a *cards.SA, depth int) {
	// "Create a token that's a copy of ... It gains haste": the keywords
	// a CopyPermanent grants its copies, as mtgish's separate layer effect
	// on the created token. (A Token's PumpKeywords$ is part of mtgish's
	// token action, which is a leaf.)
	// "Sacrifice them at the beginning of your next end step" written as
	// AtEOTTrig$ You_<action> (a delayed trigger its controller has): as
	// AtEOT$. A plain AtEOTTrig$ <action> is the copy's own printed
	// trigger ("except it has '... sacrifice this token'"), which mtgish
	// keeps inside the token.
	if v := a.Params["AtEOTTrig"]; strings.HasPrefix(v, "You_") {
		e.add(atEOTTokens(v)...)
	}
	if ct := a.Params["WithCountersType"]; ct != "" && ct != "TIME" && (a.Params["Destination"] == "Battlefield" || a.Params["DestinationZone"] == "Battlefield") {
		// "Return it to the battlefield with a finality counter on it"
		// (mtgish's EntersWith*Counter* flag, p2cEnterFlag). Time
		// counters are suspend's, which both sides name by the
		// suspend/exile action.
		e.add(gorgeCounterTokens(ct, literalOr(a.Params["WithCountersAmount"], "1"))...)
	}
	if ct := a.Params["CounterType2"]; ct != "" {
		// PutCounterAll's second group ("and a plan counter on this").
		e.add(gorgeCounterTokens(ct, literalOr(a.Params["CounterNum2"], "1"))...)
	}
	if name := a.Params["StaticEffect"]; name != "" && (a.API == "ChangeZone" || a.API == "ChangeZoneAll") {
		// "Return it ... That creature is a black Zombie in addition
		// to its other colors and types": the static the move gives the
		// moved card (mtgish's CreatePermanentLayerEffect).
		if st, ok := cards.ParseStaticLine(g.svars()[name]); ok {
			for _, t := range gorgeStaticEffects(st.Mode, st.Params) {
				if !IsUnmapped(t) {
					e.add(t)
				}
			}
		}
	}
	if a.API == "Counter" && a.Params["DestroyPermanent"] == "True" {
		// "If a permanent's ability is countered this way, destroy that
		// permanent."
		e.add("destroy")
	}
	if a.API == "CopyPermanent" {
		for _, k := range strings.Split(a.Params["PumpKeywords"], "&") {
			if k = strings.TrimSpace(k); k != "" {
				e.add("grant:" + grantKeyword(k))
			}
		}
	}
	if d := a.Params["Defined"]; a.API == "Discard" && a.Params["UnlessType"] != "" && (d == "" || d == "You") {
		// "Discard two cards unless you discard an artifact card": the
		// other discard, as mtgish's MayCost before its Unless.
		e.add("discard:1")
	}
	if c := a.Params["Cost"]; c != "" && depth > 0 {
		e.add(p2cCostActions(strings.TrimPrefix(c, "Mandatory "))...)
	}
	if c := a.Params["UnlessCost"]; c != "" {
		if payer := a.Params["UnlessPayer"]; payer == "" || payer == "You" {
			e.add(p2cCostActions(strings.TrimPrefix(c, "Mandatory "))...)
		}
	}
}

// p2cCostParts splits a Forge cost string on spaces outside <...>.
func p2cCostParts(c string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i <= len(c); i++ {
		switch {
		case i == len(c) || (c[i] == ' ' && depth == 0):
			if i > start {
				out = append(out, c[start:i])
			}
			start = i + 1
		case c[i] == '<':
			depth++
		case c[i] == '>' && depth > 0:
			depth--
		}
	}
	return out
}

// p2cCostActions maps the action parts of a Forge cost string; mana,
// energy, reveals and the like are payments and map to nothing.
func p2cCostActions(c string) []string {
	var out []string
	for _, part := range p2cCostParts(c) {
		head, rest, _ := strings.Cut(part, "<")
		rest = strings.TrimSuffix(rest, ">")
		args := strings.Split(rest, "/")
		n := literalOr(args[0], "*")
		kind := ""
		if len(args) > 1 {
			kind = args[1]
		}
		switch head {
		case "Sac":
			out = append(out, "sacrifice")
		case "PayLife":
			out = append(out, "loselife:"+n)
		case "Exile":
			out = append(out, "move:battlefield>exile")
		case "ExileFromGrave", "ExileAnyGrave":
			out = append(out, "move:graveyard>exile")
		case "ExileFromHand":
			out = append(out, "move:hand>exile")
		case "ExileFromTop":
			out = append(out, "move:library>exile")
		case "ExiledMoveToGrave":
			out = append(out, "move:exile>graveyard")
		case "Discard":
			if kind == "Hand" {
				n = "*"
			}
			out = append(out, "discard:"+n)
		case "Draw":
			out = append(out, "draw:"+n)
		case "Return":
			out = append(out, "move:battlefield>hand")
		case "tapXType":
			out = append(out, "tap")
		case "SubCounter", "RemoveAnyCounter":
			out = append(out, "countersremove")
		case "Mill":
			out = append(out, "mill:"+n)
		case "PutCardToLibFromGrave":
			out = append(out, "move:graveyard>library")
		case "Blight":
			out = append(out, "blight")
		}
	}
	return out
}

// p2cGorgeStaticEffects is the effect list g.static would give st as one
// static ability (nil when it adds none), without adding it.
func p2cGorgeStaticEffects(g *gorgeNorm, st *cards.Static) []string {
	saved := g.s.Abilities
	g.s.Abilities = nil
	g.static(st)
	var out []string
	for _, a := range g.s.Abilities {
		out = append(out, a.Effects...)
	}
	g.s.Abilities = saved
	return out
}

// p2cLookAction summarises a look-at/reveal-the-top sub-action that is
// not a move of the looked-at cards: an ordinary action written in the
// look's own enum ("if it's ..., this spell deals 8 damage to that
// player"), a wrapper whose nested sub-actions are walked, or casting a
// spell from among the cards (gorge: Play). ok is false for the rest
// (choices, piles), which stay nothing.
func (e *effects) p2cLookAction(n node, v string) (descend, ok bool) {
	switch {
	case strings.Contains(v, "MayCast") || strings.Contains(v, "MayPlay"):
		e.add("play")
		return false, true
	case wrapperActions[v] || v == "IfElse" || v == "RepeatThisProcess" || v == "RepeatableActions":
		if v == "MayCost" {
			// A look's optional pick ("you may put a land card from
			// among them onto the battlefield") is that look's move.
			if t := lookMove(variantOf(rawArgs(n))); t != "" {
				e.add(t)
			} else {
				e.action(n, v)
			}
			return false, true
		}
		return true, true
	}
	sub := &effects{}
	sub.action(n, v)
	if len(sub.effects) == 0 {
		return false, false
	}
	for _, t := range sub.effects {
		if IsUnmapped(t) {
			return false, false
		}
	}
	e.add(sub.effects...)
	e.targets = append(e.targets, sub.targets...)
	return !leafActions[v], true
}

// p2cTypeChange reports a type-changing parameter beyond the ones the
// core checks: "gains all creature types", "loses all land types", and
// the other Remove*Types$ forms (mtgish's HasAllCreatureTypes and
// RemoveAll*Types layer effects).
func p2cTypeChange(p map[string]string) bool {
	for _, k := range []string{"AddAllCreatureTypes", "RemoveLandTypes", "RemoveEnchantmentTypes",
		"RemoveArtifactTypes", "RemoveTypes", "RemoveSubTypes", "RemoveCreatureTypes", "RemoveCardTypes",
		"RemoveType", "AddType", "AddTypes", "SetCreatureTypes"} {
		if p[k] != "" {
			return true
		}
	}
	return false
}

// p2cEachPlayerTokens summarises "each player creates ...": one token verb
// per kind with a non-literal count, a copy as a copy.
func (e *effects) p2cEachPlayerTokens(ts any) {
	l := list(ts)
	if len(l) == 0 {
		e.add("token:*")
		return
	}
	for _, t := range l {
		head, _ := splitLast(tokenToken(t))
		e.add(head + ":*")
	}
}

// p2cGoadAction maps the is-goaded rule, given by an action, to the goad
// action.
func p2cGoadAction(t string) string {
	if t == "rule:isgoadedbyplayer" {
		return "goad"
	}
	return t
}

// p2cOneKeywordGrant reports a granted rule that is one keyword with
// several qualities ("protection from green and from white", which the
// normaliser splits into one keyword token per quality): Forge grants it
// as one keyword.
func p2cOneKeywordGrant(s *Summary) (string, bool) {
	if len(s.Keywords) < 2 || len(s.Abilities) != 0 {
		return "", false
	}
	head := keywordHeadToken(s.Keywords[0])
	for _, k := range s.Keywords[1:] {
		if keywordHeadToken(k) != head {
			return "", false
		}
	}
	return "grant:" + head, true
}

// p2cStaticEffects maps mtgish static rules the core leaves as
// "static:<variant>". "Each creature card in your graveyard has encore"
// (EachCardIn<zone>Effect) is a Continuous with AffectedZone$ granting the
// ability: its card effects are layer effects.
func p2cStaticEffects(v string, a []any) ([]string, bool) {
	if strings.HasPrefix(v, "EachCardIn") && strings.HasSuffix(v, "Effect") && len(a) > 0 {
		if l := list(a[len(a)-1]); l != nil {
			out := layerEffects(l)
			for _, t := range out {
				if IsUnmapped(t) {
					// "Can't be the target of ...": the gorge side keeps
					// the static's own spelling (unit B).
					return nil, false
				}
			}
			return out, true
		}
	}
	return nil, false
}

// p2cEffectRepl maps the replacements an Effect (an emblem, a "for as long
// as" effect) creates that mtgish writes as player rules: "you can't lose
// the game", "your opponents can't win the game" (the same tokens as the
// printed R:Event$ GameLoss/GameWin, gorgeNorm.repl).
func p2cEffectRepl(r cards.Repl, _ map[string]string) []string {
	switch {
	case r.Event == "GameLoss" && r.Params["ReplaceWith"] == "":
		return []string{"player:cantlosethegame"}
	case r.Event == "GameWin":
		return []string{"player:cantwinthegame"}
	}
	return nil
}

// p2cPlayerKeyword maps a keyword a Pump gives a player ("you gain shroud
// until end of turn"), mtgish's player effect of that name.
func p2cPlayerKeyword(p map[string]string, k string) (string, bool) {
	d := p["Defined"]
	if d != "You" && d != "Player" && d != "Opponent" && !strings.HasPrefix(d, "Player.") {
		return "", false
	}
	switch h := cards.KeywordHead(k); {
	case h == "Shroud", h == "Hexproof":
		return "player:" + strings.ToLower(h), true
	case strings.HasPrefix(h, "Protection"):
		return "player:protection", true
	}
	return "", false
}

// p2cSpellToken maps a spell effect variant ("this spell costs {2} more",
// "spells cost {1} less"): a cost change is the cost static's token, as
// the PlayerEffect cost variants are (d2PlayerToken).
func p2cSpellToken(v string) string {
	switch {
	case strings.HasPrefix(v, "IncreaseManaCost"):
		return "costincrease"
	case strings.HasPrefix(v, "DecreaseManaCost") || strings.HasPrefix(v, "ReduceManaCost"):
		return "costreduction"
	}
	return "spell:" + strings.ToLower(v)
}

// p2cEnterFlag maps an enter flag that puts counters on the entering
// permanent (gorge: WithCountersType$ on the move).
func p2cEnterFlag(n node, v string) []string {
	if v == "EntersFlipped" {
		return []string{"setstate:flip"}
	}
	if v == "EntersWithLayerEffect" || v == "EntersWithLayerEffectUntil" {
		// "It's an Enchantment in addition to its other types": the
		// moved card's layer effect (gorge: the move's StaticEffect$).
		if l := list(rawArgs(n)); l != nil {
			if len(l) > 0 && enumOf(l[0]) != "LayerEffect" {
				// EntersWithLayerEffectUntil: (effects, expiration).
				l = list(l[0])
			}
			return layerEffects(l)
		}
		return nil
	}
	if !strings.HasPrefix(v, "EntersWith") || !strings.Contains(v, "Counter") {
		return nil
	}
	a := argsOf(n)
	ct, num := "", "*"
	for _, x := range a {
		switch enumOf(x) {
		case "CounterType":
			ct = counterType(x)
		case "GameNumber":
			num = gameNumber(x)
		}
	}
	if ct == "" {
		return []string{"counters:*"}
	}
	if ct == "time" {
		return nil
	}
	if strings.Contains(v, "ACounter") || strings.Contains(v, "AnAdditional") {
		num = "1"
	}
	return []string{"counters:" + ct + ":" + num}
}

// p2cCardZone is the zone a card-selection variant names.
func p2cCardZone(v string) string {
	switch {
	case strings.Contains(v, "Library"):
		return "library"
	case strings.Contains(v, "Hand"):
		return "hand"
	case strings.Contains(v, "Graveyard"):
		return "graveyard"
	case strings.Contains(v, "Exile"):
		return "exile"
	}
	return ""
}
