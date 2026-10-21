package mtgish

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase 2, unit b: residual T4 effect classes found triaging the
// alphabetical second third of the corpus.

// mayCostActionsP2B are the costs of a "you may <X>. If you do, ..."
// (mtgish MayCost/MustCost) that Forge's cost grammar has no cost for, so
// a script always writes X as an effect: putting a card from hand, a
// graveyard, exile or the library onto the battlefield or into a hand,
// looking, searching, destroying, transforming, untapping another
// permanent, creating tokens or a continuous effect. Those are mapped as
// the action. A cost Forge can write as a cost (sacrifice, exile, discard,
// pay life or energy, tap, return to hand, counters, reveal, mill, draw)
// stays unmapped on both sides: scripts write it as a Cost$ or UnlessCost$,
// which the gorge side does not compare either (measured: mapping those
// adds more rows than it removes).
var mayCostActionsP2B = map[string]bool{
	"PutACardFromHandOnBattlefield":               true,
	"PutACardFromGraveyardIntoHand":               true,
	"PutGraveyardCardOntoBattlefield":             true,
	"PutACardOfTypeMilledThisWayIntoHand":         true,
	"PutExiledCardOntoBattlefield":                true,
	"PutGraveyardCardOnTopOfLibrary":              true,
	"PutTopCardOfLibraryOfTypeOnBattlefield":      true,
	"LookAtTheTopCardOfPlayersLibrary":            true,
	"LookAtTopOfLibrary":                          true,
	"SearchLibrary":                               true,
	"DestroyPermanent":                            true,
	"TransformPermanent":                          true,
	"UntapPermanent":                              true,
	"CreateTokens":                                true,
	"CreatePermanentLayerEffectUntil":             true,
	"AbandonScheme":                               true,
	"PutTopCardOfEachPlayersLibraryInGraveyard":   true,
	"LookAtPlayersHandAndChooseACardToExileUntil": true,
}

// mayCostEffectP2B maps a MayCost cost listed in mayCostActionsP2B as the
// action it names, and reports whether it did. A MustCost ("exile it. If
// you do, ...": the action is mandatory, its "if you do" makes it a cost)
// is mapped for every action except a sacrifice, a discard or a life
// payment, which scripts write as a Cost$ or UnlessCost$ (measured).
func mayCostEffectP2B(e *effects, must bool, c any) bool {
	v := variantOf(c)
	switch {
	case must && (strings.HasPrefix(v, "Sacrifice") || v == "DiscardACard" || v == "PayLife"):
		return false
	case !must && !mayCostActionsP2B[v]:
		return false
	}
	sub := &effects{}
	sub.costAsAction(c)
	for _, t := range sub.effects {
		if IsUnmapped(t) {
			return false
		}
	}
	if len(sub.effects) == 0 {
		return false
	}
	e.add(sub.effects...)
	return true
}

// replCanonP2B folds mtgish's spellings of one replaced event into the
// token the gorge side uses: Forge has one CreateToken replacement event
// whichever way the Oracle text words it ("an effect would create one or
// more tokens", "one or more tokens would be created", "a player would
// create a token").
var replCanonP2B = map[string]string{
	"replace:anynumberoftokenswouldbecreated": "replace:aneffectwouldcreateanynumberoftokens",
	"replace:aplayerwouldcreatetokens":        "replace:aneffectwouldcreateanynumberoftokens",
	"replace:aplayerwouldcreateatoken":        "replace:aneffectwouldcreateanynumberoftokens",
}

// paramTokensP2B maps the second action some APIs carry in their own
// parameters: PutCounter(All)'s CounterType2$ ("a +1/+1 counter on each
// creature and a loyalty counter on each planeswalker") and a zone
// change's WithCountersType$ ("exile it with N time counters on it").
func paramTokensP2B(a *cards.SA) []string {
	p := a.Params
	var out []string
	switch a.API {
	case "PutCounter", "PutCounterAll":
		if ct := p["CounterType2"]; ct != "" {
			n := p["CounterNum2"]
			if n == "" {
				n = p["CounterNum"]
			}
			out = append(out, gorgeCounterTokens(ct, literalOr(n, "1"))...)
		}
	case "Dig":
		// "Look at the top card. If it's a land, you may put it onto the
		// battlefield. Otherwise put it into your hand": the card not
		// chosen goes to DestinationZone2$ even when as many are looked at
		// as may be chosen (the core case adds it only when fewer may be).
		n, m := literalOr(p["DigNum"], "1"), literalOr(p["ChangeNum"], "1")
		d2 := p["DestinationZone2"]
		if p["ChangeNum"] != "All" && n == m && n != "*" && (d2 == "Hand" || d2 == "Graveyard" || d2 == "Exile" || d2 == "Library" && p["LibraryPosition2"] == "-1") &&
			(p["Optional"] == "True" || p["ChangeValid"] != "") {
			out = append(out, "move:library>"+strings.ToLower(d2))
		}
		if p["ChangeNum"] == "All" && p["DestinationZone"] != "" && p["ChangeValid"] != "" && p["RestRandomOrder"] == "True" {
			// "Put all lands among them onto the battlefield and the rest
			// on the bottom in a random order": the rest moves too.
			out = append(out, "move:library>library")
		}
		if p["ChangeNum"] == "All" && p["ChangeValid"] != "" && p["DestinationZone2"] == "Library" && p["LibraryPosition2"] == "-1" {
			// "If it's a creature card, put it into your hand. Otherwise,
			// put it on the bottom": the other card moves too.
			out = append(out, "move:library>library")
		}
	case "ChangeZone", "ChangeZoneAll":
		out = append(out, otherOriginsP2B(a)...)
		if alt := p["DestinationAlternative"]; alt != "" && alt != p["Destination"] && !strings.Contains(p["Origin"], "Library") {
			// "Return it to your hand. If <condition>, put it onto the
			// battlefield instead": either destination.
			o := strings.ToLower(strings.TrimSpace(strings.SplitN(p["Origin"], ",", 2)[0]))
			out = append(out, "move:"+o+">"+strings.ToLower(alt))
		}
		if ct := p["WithCountersType"]; ct != "" {
			out = append(out, gorgeCounterTokens(ct, literalOr(p["WithCountersAmount"], "1"))...)
		}
	}
	return out
}

// enterFlagP2B maps the counters a permanent is put onto the battlefield
// with or exiled with (EntersWith*/ExilesWith* flags), as the gorge side
// maps a zone change's WithCountersType$ (paramTokensP2B).
func enterFlagP2B(n node, v string) []string {
	switch v {
	case "EntersWithACounter", "ExilesWithACounter":
		return []string{"counters:" + counterType(rawArgs(n)) + ":1"}
	case "EntersWithNumberCounters", "ExilesWithNumberCounters":
		return []string{"counters:" + counterType(argN(n, 1)) + ":" + gameNumber(argN(n, 0))}
	}
	return nil
}

// reflexiveHeadP2B maps the action half of mtgish's
// "Reflexive_<Action>_When..." ("return it to the battlefield. When you
// do, ...") for actions a script writes as an effect, keyed by <Action>.
// A cost-shaped half (sacrifice, discard, pay, tap, remove counters,
// collect evidence) stays a cost, as the core case keeps it.
var reflexiveHeadP2B = map[string]func(n node) []string{
	"PutOnBattlefield": func(n node) []string {
		return []string{"move:" + reflexiveZoneP2B(argN(n, 0)) + ">battlefield"}
	},
	"Exile": func(n node) []string {
		var out []string
		for _, x := range list(argN(n, 0)) {
			if z := exilableZone(x); z != "graveyard" {
				// Exiling from a graveyard "when you do" is a cost in
				// scripts (Cost$ ExileFromGrave), as in mtgish.
				out = append(out, "move:"+z+">exile")
			}
		}
		return dedup(out)
	},
	"DealDamage":                       fixedP2B("damage:*"),
	"Fight":                            fixedP2B("fight"),
	"Surveil":                          fixedP2B("surveil:*"),
	"Amass":                            fixedP2B("amass"),
	"Attach":                           fixedP2B("attach"),
	"HavePlayerGainControlOfPermanent": fixedP2B("gaincontrol"),
	"RollDice":                         fixedP2B("dice"),
	"FlipACoin":                        fixedP2B("coin"),
	"CreatureConnives":                 fixedP2B("connive"),
	"AddMana":                          fixedP2B("mana"),
	"PutAPermanentInOwnersHand":        fixedP2B("move:battlefield>hand"),
	"SearchLibrary":                    fixedP2B("search:*"),
	"RevealTheTopNumberCardsOfLibrary": fixedP2B("dig:*"),
	"RevealCardsFromTheTopOfLibraryUntilACardOfTypeIsRevealed": fixedP2B("diguntil"),
}

func fixedP2B(t ...string) func(node) []string { return func(node) []string { return t } }

func reflexiveP2B(n node, v string) []string {
	head, _, _ := strings.Cut(strings.TrimPrefix(v, "Reflexive_"), "_When")
	if f, ok := reflexiveHeadP2B[head]; ok {
		return f(n)
	}
	return nil
}

// reflexiveZoneP2B is the zone of the first card a reflexive put moves
// (CardInHand, CardInGraveyard, CardInExile, ...).
func reflexiveZoneP2B(objs any) string {
	l := list(objs)
	if len(l) == 0 {
		return "*"
	}
	v := variantOf(l[0])
	switch {
	case strings.Contains(v, "Hand"):
		return "hand"
	case strings.Contains(v, "Graveyard"):
		return "graveyard"
	case strings.Contains(v, "Exile"):
		return "exile"
	case strings.Contains(v, "Librar"):
		return "library"
	}
	return "*"
}

// allListsP2B reports whether every element is a list (ChooseAnAction's
// branches, one action list each).
func allListsP2B(xs []any) bool {
	for _, x := range xs {
		if list(x) == nil {
			return false
		}
	}
	return true
}

// otherOriginsP2B maps the further origins of an exile that lists several
// (Origin$ Hand,Graveyard: "exile a card from their hand or graveyard"):
// one move per origin, as mtgish writes one Exile per zone in a choice of
// actions (merged as alternatives). The first origin is the core token.
// Other destinations are not split: mtgish writes most "from your hand or
// graveyard" puts as one action of one zone (measured).
func otherOriginsP2B(a *cards.SA) []string {
	origins := strings.Split(a.Params["Origin"], ",")
	if len(origins) < 2 || effectSelfExile(a) || a.Params["Destination"] != "Exile" {
		return nil
	}
	first := changeZoneToken(a.API, a.Params)
	if !strings.HasPrefix(first, "move:") {
		// A search ("library and/or graveyard") is one action.
		return nil
	}
	d := strings.ToLower(strings.TrimSpace(a.Params["Destination"]))
	var out []string
	for _, o := range origins[1:] {
		if t := "move:" + strings.ToLower(strings.TrimSpace(o)) + ">" + d; t != first {
			out = append(out, t)
		}
	}
	return dedup(out)
}

// lookActionP2B are look/reveal steps (_LookAtTopOfLibraryAction,
// _RevealTheTopNumberCardsOfLibraryAction) that are ordinary actions with
// the _Action's shape: a continuous or perpetual effect on the card put
// onto the battlefield, damage from the spell.
var lookActionP2B = map[string]bool{
	"CreatePermanentLayerEffect":      true,
	"CreatePermanentLayerEffectUntil": true,
	"CreatePerpetualPermanentEffect":  true,
	"SpellDealsDamage":                true,
}

// exilableZonesP2B is the zones an Exilable takes cards from: one, except
// "permanents and/or cards from hand (or graveyard)", which a script
// writes as one ChangeZone with several origins.
func exilableZonesP2B(x any) []string {
	switch variantOf(x) {
	case "NumberPermanentsOrCardsInHand":
		return []string{"battlefield", "hand"}
	case "NumberPermanentsCardsInHandOrCardsInGraveyards":
		return []string{"battlefield", "hand", "graveyard"}
	}
	return []string{exilableZone(x)}
}

// grantTokenP2B names a granted rule the keyword path cannot: a keyword
// written with a rider ("protection from the chosen color; this effect
// doesn't remove <this>", "bands with other legendary creatures"), or
// one keyword with several parameters ("protection from black and from
// red", which the keyword path reads as several keywords). "" if none.
func grantTokenP2B(r any, s *Summary) string {
	switch variantOf(r) {
	case "ProtectionAndDoesntRemovePermanents", "Protection":
		return "grant:protection"
	case "BandsWithOthers":
		return "grant:bandswithother"
	}
	if len(s.Abilities) == 0 && len(s.Keywords) > 1 {
		head := keywordHeadToken(s.Keywords[0])
		for _, k := range s.Keywords[1:] {
			if keywordHeadToken(k) != head {
				return ""
			}
		}
		return "grant:" + head
	}
	return ""
}

// canonGrantP2B folds a granted keyword's mtgish spelling into the gorge
// side's (grantKeyword: every Protection* is "protection").
func canonGrantP2B(t string) string {
	if strings.HasPrefix(t, "grant:protection") {
		return "grant:protection"
	}
	return t
}

// searchFollowUpP2B reports a library ChangeZone that only distributes
// cards an earlier search of the same ability found and remembered
// ("search for two basic lands, put one onto the battlefield and the other
// into your hand", scripted as a search to the library, then one hidden
// ChangeZone per destination among the remembered cards): mtgish writes
// it as one SearchLibrary, so these are no further search.
func searchFollowUpP2B(a *cards.SA) bool {
	p := a.Params
	return a.API == "ChangeZone" && p["Origin"] == "Library" && p["Defined"] == "" &&
		strings.Contains(p["ChangeType"], "IsRemembered") && p["ValidTgts"] == ""
}

func init() {
	// "[+2], then ..." inside an effect (a die result): a loyalty change,
	// as the script's PutCounter/RemoveCounter LOYALTY.
	extAction["Loyalty"] = func(e *effects, n node) {
		if n := intOr(rawArgs(n)); n < 0 {
			e.add("countersremove")
		} else {
			e.add("counters:loyalty:" + itoaP2B(n))
		}
	}
	// "Attach it to a player" (a Curse): the attach, as AttachPermanentToPermanent.
	extAction["AttachPermanentToAPlayer"] = func(e *effects, _ node) { e.add("attach") }
}

func itoaP2B(n int) string { return strconv.Itoa(n) }
