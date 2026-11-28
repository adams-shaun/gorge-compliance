package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// forcedSingleOption reports a one-option ask XMage never poses: exactly one
// legal answer (Min==Max==Options==1) that is not a target, order or mode ask.
// A target pick is still a chooseTarget XMage asks for even with one legal
// option, a mode ask is posed up to Max picks even when only one mode is
// affordable, and a min-0 single option is a real decline XMage still asks
// (Break Out, Destined Confrontation).
func forcedSingleOption(d rules.OracleDecision) bool {
	if d.Kind == "mode" && d.Resume == "generic_players" && d.Options == 1 && d.Min == 1 && d.Max == 1 {
		// A per-player punisher's forced one-mode ask (Rottenmouth Viper's
		// "you lose 4 life unless ..."): XMage does not pose it; the unless
		// choice that follows is the real decision.
		return true
	}
	if hasNamePick(d) {
		// A card-name dialog is always posed over XMage's whole card pool
		// (CR 201.3), however narrowly gorge's deck-scoped name universe
		// scoped the ask (Cabal Therapy, Phyrexian Revoker, Silverquill
		// Silencer). It is a real choice-queue ask, never a forced fold.
		return false
	}
	if d.Kind == "target" || d.Kind == "order" || d.Kind == "mode" {
		return false
	}
	if d.Retarget {
		// A forced "change the target" with one other legal target is still
		// XMage's chooseTarget: its loop counts the old target among the
		// possible ones, and unscripted its AI keeps picking the old target.
		return false
	}
	if loneTriggerCostDecline(d) {
		// A triggered optional cost gorge could not pay (a "may discard your
		// hand" over an empty hand, a "may pay {2}" over an empty pool) is
		// offered decline-only, but XMage's DoIfCostPaid still poses its
		// chooseUse and its AI answers yes, paying the cost gorge declined
		// (Byway Barterer, Esoteric Duplicator). The decline is a real ask.
		return false
	}
	if hasTargetPick(d) {
		return false
	}
	if hasCostPick(d) {
		// A tap, sacrifice or exile COST chooser is a real ask even with one
		// legal object: XMage's TargetPermanent("another untapped creature")
		// still poses the choice, and a dropped answer leaves the cost unpaid,
		// so TestPlayer reports "Can't find ability" (Saddle/Crew with one
		// untapped creature left). The decision's picks are object identities,
		// not the single-option LABEL asks this predicate exists for.
		return false
	}
	if phyrexianLifePicks(d) {
		// The life face of a Phyrexian pip is XMage's chooseUse ("Pay 2
		// life instead of {R}?"), and XMage poses it even when gorge's
		// feasibility gate offered ONLY that face: a cast with no coloured
		// mana (Gut Shot's {R/P}, Dismember's {B/P}{B/P}) records
		// Options == 1. Dropping the one-option ask leaves the driver's
		// queue empty, and its starved-ask arm answers no -- the opposite
		// of gorge's recorded pick. Every other payment face (pay_W,
		// pay_generic) is still dropped by the final Options check.
		return false
	}
	if announcesX(d) {
		// An X announcement is always posed (XMage's announceXMana), even
		// when the available mana leaves one legal value: an "{X}{X}, {T}"
		// activation (Gogo, Master of Mimicry) with CC offers only X = 1,
		// and an unscripted announce left the cost unpaid.
		return false
	}
	return d.Options == 1 && d.Min == 1 && d.Max == 1
}

// forcedChoicePosed reports whether a forced one-option choose_n ask is still
// posed by XMage's makeChoose (Unstable Glyphbridge's "choose a creature").
// A ChangeTargets redirect shares the same decision shape (Resume "choice")
// but is not a choice ask: XMage retargets through chooseTarget, which
// auto-selects a sole candidate in non-strict mode and reads the target queue
// otherwise, so a scripted choice would never be consumed. The redirect is
// told apart by the earlier cast that targeted a stack object (Bolt Bend,
// Redirect Lightning).
func forcedChoicePosed(ds []rules.OracleDecision, i int) bool {
	d := ds[i]
	if d.Kind != "choose_n" || d.Resume != "choice" {
		return false
	}
	for _, e := range ds[:i] {
		if e.Kind != "target" || e.Step >= d.Step {
			continue
		}
		for k := range e.Picks {
			switch pickKind(e, k) {
			case "spell", "ability", "trigger":
				return false
			}
		}
	}
	return true
}

// allChoiceQueue reports whether every pick in a decision reaches XMage's
// choice queue (makeChoose). A decision with a target or skipped pick is not
// one makeChoose dialog and must not be joined with '^'. It asks pickQueue,
// the same per-pick route the answer emission uses, so every shape-gated route
// (a fixed-size exile cost, a Craft material cost) is joined or kept separate
// exactly as its answers are.
func allChoiceQueue(d rules.OracleDecision, craft bool) bool {
	if d.Retarget {
		return false
	}
	if hasExileCostPick(d) && !(d.Min > 0 && d.Min == d.Max) {
		// An open-ended exile cost (a group exile's "one or more") settles one
		// card per TargetImpl.choose iteration, so each pick is its own
		// makeChoose record; joining them would leave all but the first card
		// unexiled. A fixed-size selection (min == max) is the one joined
		// record.
		return false
	}
	for k, label := range d.Picks {
		if pickQueue(d, k, label, false, craft) != "choice" {
			return false
		}
	}
	return true
}

// yesNo recognises a bare two-way boolean choice. The engine's option kind
// and exact label/ref identity must both agree; composed choices such as
// "Yes — discard" are ordinary makeChoose picks, not boolean answers. Older
// snapshots without PickKinds use the same exact-label/ref rule. The one
// composed exception is the unless-discard election (kind "unless" "Yes —
// discard one <spec>" / kind "ordinary" "No — discard normally"): XMage
// models it as DoIfCostPaid, so it is a chooseUse yes/no.
func yesNo(d rules.OracleDecision) (string, bool) {
	if (d.Options != 2 && !loneTriggerCostDecline(d)) || len(d.Picks) != 1 || len(d.PickRefs) != 1 {
		return "", false
	}
	switch kind := pickKind(d, 0); kind {
	case "trigger_cost_decline":
		// The "do not pay" side of a triggered optional cost is XMage's
		// DoIfCostPaid chooseUse answered no; its AI defaults to yes and
		// would pay what gorge declined.
		return "no", true
	case "yes", "no":
		// The engine's own boolean option ("Yes — shuffle", a may
		// trigger): XMage's chooseUse.
		return kind, true
	case "gift_promise":
		// The promise side is chooseUse yes whichever option index the
		// engine listed it at (Jolly Gerbils' Blooming Blast offers "Don't
		// promise a gift" first, so the promise is option 1).
		return "yes", true
	case "gift_decline":
		return "no", true
	case "unless":
		// "Discard one <spec> instead" (Thirst for Discovery): XMage's
		// DoIfCostPaid chooseUse before the DiscardCardCost target, so the
		// elected side is chooseUse yes and the card pick follows as the
		// cost's own choice. The label is never a literal choice.
		return "yes", true
	case "ordinary":
		// The same ask's decline side ("No — discard normally"): chooseUse no.
		if d.Picks[0] == "No — discard normally" {
			return "no", true
		}
	case "endure_counters":
		// Endure is XMage's chooseUse "Put N +1/+1 counters on
		// <it> or create a N/N Spirit token?" (EndureSourceEffect.doEndure,
		// true text "Add counters"); TestPlayer reads only Yes/No for a
		// text-choice chooseUse ("Use Yes/No in unit tests for text
		// choices"), so the label strict-missed (Dusyut Earthcarver,
		// Inspirited Vanguard).
		return "yes", true
	case "endure_spirit":
		return "no", true
	case "altaddcost", "primary":
		// An optional additional cost, a gift promise and a two-way
		// primary/secondary pick are chooseUse asks in XMage too
		// (measured: Silence the Echo, Kitnap, Lost in Space agree only
		// with a yes/no answer). Option 0 is the "do it" side.
		l := strings.ToLower(d.Picks[0])
		for _, neg := range []string{"do not", "don't", "no", "decline", "skip"} {
			if strings.HasPrefix(l, neg) {
				return "no", true
			}
		}
		if len(d.PickIdx) == 1 && d.PickIdx[0] == 0 {
			return "yes", true
		}
		return "no", true
	case "":
		// A snapshot without PickKinds: only an exact Yes/No label.
		label := strings.ToLower(strings.TrimSpace(d.Picks[0]))
		if d.Picks[0] == d.PickRefs[0] && (label == "yes" || label == "no") {
			return label, true
		}
	}
	return "", false
}

// bandAnswers scripts a band declaration for XMage's Combat.handleBanding:
// a chooseUse for each member, then that member's card selection. Gorge records
// one band pick per accepted member ("pN:<creature>") and one per decline (the
// "No more creatures in <leader>'s band" label), so the pick's shape, not its
// label text, decides the answer. Keyed on the engine pick kind "band": the next
// band-declaration flavour (a future "bands with other") takes the same route
// without another label census.
//
// The member is a kind "choice", not "target": XMage reaches the band's
// TargetControlledPermanent through Target.choose -> Player.choose, whose
// TestPlayer answer is makeChoose on the CHOICE queue. A "target" answer would
// go to the separate targets queue that chooseTarget reads, and makeChoose
// would consume the next band's yes/no instead (measured: "Found wrong choice
// command ... No ... Select an attacking creature to band with").
//
// The member value is the same-name-aware object choice, not the raw scenario
// ref. XMage's band target filters out the leader, so a plain name is unique
// among the remaining candidates; a raw ref is not. When two same-name copies
// attack, registerAliases can bind a scenario ref to the copy that is the
// current leader (its occurrence count follows battlefield iteration, not
// setup order), and the target then selects nothing: measured on the SC15
// certdecks as "Found wrong choice command (invalid target ...):
// @p0:Kjeldoran Warrior ... selected 0, more possible 2" for the second
// copy's band ask. disambiguatedObjectChoice keeps the exact-ref alias for a
// genuinely ambiguous same-name pick and the plain name otherwise.
func bandAnswers(d rules.OracleDecision) ([]XAnswer, bool) {
	if len(d.PickKinds) == 0 {
		return nil, false
	}
	for _, kind := range d.PickKinds {
		if kind != "band" {
			return nil, false
		}
	}
	as := make([]XAnswer, 0, len(d.Picks)*2)
	for k := range d.Picks {
		ref := ""
		if k < len(d.PickRefs) {
			ref = d.PickRefs[k]
		}
		if isScenarioRefShaped(ref) {
			memberName := bandMemberLiveName(d, k)
			if memberName == "" {
				memberName = oraclediffRefName(ref)
			}
			// The label's member field is the object's live face by
			// construction, so it names the pick even when it differs from
			// the ref's setup name (a transformed DFC): the same-name alias
			// and the owner-qualified ref must both stay available despite
			// namesObject comparing against the setup name.
			member := disambiguatedObjectChoiceNamed(d, k, memberName, true)
			as = append(as, XAnswer{d.Seat, "choice", "yes"}, XAnswer{d.Seat, "choice", member})
			continue
		}
		as = append(as, XAnswer{d.Seat, "choice", "no"})
	}
	return as, true
}

// pickKind is the engine option kind of the k-th pick. A snapshot written
// before PickKinds existed returns "", which every caller treats as the
// choice queue.
func pickKind(d rules.OracleDecision, k int) string {
	if k >= 0 && k < len(d.PickKinds) {
		if (d.Resume == "opp_pick" || d.Resume == "choice") && d.PickKinds[k] == "player" {
			// The TargetingPlayer$ Opponent flow's controller-facing
			// which-opponent ask: XMage's ChoicePlayer, the choice queue.
			return "opponent_choice"
		}
		return d.PickKinds[k]
	}
	return ""
}

// xmQueue says which TestPlayer queue an engine option kind reaches. The
// choice queue is makeChoose/setChoice -- XMage's choose(Cards, TargetCard),
// choose(Choice) and choose(ChoicePlayer) all land there. The target queue is
// addTarget/chooseTarget, reached by a real TargetXxx. "skip" is for a pick
// XMage never asks TestPlayer about: a library search (TestPlayer.searchLibrary
// delegates to the computer player, exactly as doSurveil does) and a
// mana-tapping cost (paid from the pool). The engine option kind, not the
// label text, is the authority -- the mechanism the census test pins.
func xmQueue(kind, label string) string {
	switch kind {
	case "search":
		// A library search (TargetCardInLibrary) is answered from the target
		// queue: measured on the std pass, 47 search carriers (Shared Roots,
		// Nature's Rhythm, Solemn Simulacrum, ...) agree only with a target
		// answer.
		return "target"
	case "exilecost":
		// An exile COST is not a TargetXxx target selection:
		// ExileFromGraveCost/ExileTargetCost/ExileFromHandCost (and
		// ExileXFromYourGraveCost's generated ExileFromGraveCost) all pay with
		// getTargets().choose() -> TargetImpl.choose -> player.choose, which
		// TestPlayer serves from makeChoose, the CHOICE queue (Ludevic's
		// ExileXFromYourGraveCost left a target answer unused). A fixed-size
		// selection is one joined makeChoose record; an open-ended group exile
		// is one record per card (allChoiceQueue keeps it unjoined). Craft's
		// material is the sole target-queue exception, marked by craft in
		// pickQueue (craft_cost_pick.go). The census pins this kind mapping.
		return "choice"
	case "trigger_cost_pay", "trigger_cost_pay_manual":
		return "skip"
	case "mana":
		if _, ok := manaColourLabel(label); ok {
			// A mana ability's "add one mana of any colour" pick is a real
			// choice dialog; only a mana-tapping cost is paid silently.
			return "choice"
		}
		return "skip"
	case "permanent", "player":
		return "target"
	case "opponent_choice":
		return "choice"
	}
	return "choice"
}

func isSeat(s string) bool {
	return len(s) >= 2 && s[0] == 'p' && strings.Trim(s[1:], "0123456789") == ""
}

// twoPilesPileAnswer maps gorge's pile pick to XMage's boolean chooseUse
// answer: pile A ("First pile", the separator's face-down pile) is "yes",
// pile B ("Second pile", the face-up pile) is "no".
func twoPilesPileAnswer(d rules.OracleDecision) string {
	if len(d.PickKinds) == 1 && d.PickKinds[0] == "pile-b" {
		return "no"
	}
	return "yes"
}

// pickQueue is xmQueue for the k-th pick, except that a search of ANOTHER
// player's library (Ancient Vendetta's "search target opponent's ... library")
// reaches XMage's choice queue: only a search of your own library is the
// TargetCardInLibrary target ask (measured on the std pass). bolster marks
// the decision as a Bolster tie election, whose counter_pick XMage asks
// through chooseTarget (bolster_pick.go). craft marks a Craft keyword's
// material exile, which CraftAbility pays through chooseTarget rather than the
// choice queue every other exile cost uses (craft_cost_pick.go).
func pickQueue(d rules.OracleDecision, k int, label string, bolster, craft bool) string {
	if bolster && pickKind(d, k) == "counter_pick" {
		return "target"
	}
	if craft && pickKind(d, k) == "exilecost" {
		// Craft material is a TargetCardInGraveyardBattlefieldOrStack chosen
		// by CraftAbility.CraftCost.pay -> player.chooseTarget: the TARGET
		// queue, one answer per material. The kind-level choice mapping must
		// not capture it just because a Craft cost also has min == max.
		return "target"
	}
	if d.Retarget {
		// A ChangeTargets redirect (Bolt Bend) is XMage's chooseTarget:
		// the new target rides the target queue even though gorge poses it
		// as a choose ask, so a "choice" answer is never consumed and the
		// original target survives.
		return "target"
	}
	q := xmQueue(pickKind(d, k), label)
	if q == "target" && pickKind(d, k) == "search" && k < len(d.PickRefs) {
		if s, ok := refSeat(d.PickRefs[k]); ok && s != d.Seat {
			return "choice"
		}
	}
	return q
}
