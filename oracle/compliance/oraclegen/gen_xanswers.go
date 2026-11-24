package oraclegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// xanswersSetup turns gorge's recorded decisions into XMage's scripted
// answers, grouped by the step that posed them. setup is the scenario's setup
// (nil when the caller has none): it lets a trigger-order decision recorded
// before step zero be retimed as setup_choice when its sources are the
// setup-placed permanents (triggerOrderSetupPosed). flips are the run's
// recorded coin-flip results (rules.OracleResult.Flips): a drive-window flip
// (step < 0) queues on the setup family, a step flip leads its own step's
// answer stream, each as the XMage flip token the choice queue consumes.
// The callers with a scenario pass one xanswersExtra: digs resolves a Dig
// decision's step to its source card's IR (DestinationZone2), how
// xanswersForScenario tells the exile family from the battlefield/graveyard
// one, and pips carries the per-step flexible-pip plan (phyrexianPipPlan) so
// a step that pays an earlier pip with mana and a later one with life scripts
// each of XMage's per-pip asks positionally instead of one flat yes.
// Callers with no scenario/snapshots omit it and keep the historical
// shape-only routing.
func xanswersSetup(ds []rules.OracleDecision, steps int, modes map[string]int, castSteps map[int]bool, setup map[string]Seat, flips []rules.OracleFlip, extras ...xanswersExtra) [][]XAnswer {
	var digSources map[int]digSource
	var pipPlans map[int][]pipAsk
	var casts map[int]bool
	var activates map[int]int
	var searchZones map[int]searchZoneAsk
	var forage map[int][]string
	var ownBounce map[int]bool
	var tapThenUntap map[int]bool
	var snaps []rules.OracleSnapshot
	var orderAsks map[int]bool
	for _, ex := range extras {
		if ownBounce == nil {
			ownBounce = ex.ownBounce
		}
		if tapThenUntap == nil {
			tapThenUntap = ex.tapThenUntap
		}
		if forage == nil {
			forage = ex.forage
		}
		if digSources == nil {
			digSources = ex.digs
		}
		if pipPlans == nil {
			pipPlans = ex.pips
		}
		if snaps == nil {
			snaps = ex.snaps
		}
		if orderAsks == nil {
			orderAsks = ex.orderAsks
		}
		if casts == nil {
			casts = ex.casts
		}
		if activates == nil {
			activates = ex.activates
		}
		if searchZones == nil {
			searchZones = ex.searchZones
		}
	}
	// A hand_move/library_placement goes top-first (reverse the answers for
	// XMage's successive top asks) or bottom-first (keep them) by where the
	// after-step checkpoint shows the picks, not by a list of cards. An
	// explicit changezone_alternative "bottom" overrides a checkpoint that
	// cannot tell a bottom-into-empty-library placement from a top one.
	topPlacement := LibraryTopPlacements(ds, snaps)
	out := make([][]XAnswer, steps)
	any := false
	// The plan's pip decisions: the ordinal of each pip decision among its
	// step's pip decisions (the cost order XMage asks them in) and whether the
	// plan covers that ordinal. A covered decision is scripted from the plan;
	// an uncovered one (no plan for the step, a mis-derived cost) keeps the
	// historical single-yes/skip behaviour.
	pipPlanned := make([]bool, len(ds))
	pipOrdinal := make([]int, len(ds))
	seenPips := map[int]int{}
	for i, d := range ds {
		pipOrdinal[i] = -1
		if !isPipDecision(d) {
			continue
		}
		plan, ok := pipPlans[d.Step]
		if !ok {
			continue
		}
		k := seenPips[d.Step]
		seenPips[d.Step]++
		pipOrdinal[i] = k
		if k < len(plan) && plan[k].asks {
			pipPlanned[i] = true
		}
	}
	// A step whose card name is then searched for (Ancient Vendetta's "choose
	// a card name. Search ... for cards with that name"): XMage poses the name
	// dialog however narrowly gorge offered it, and the search must find it.
	namedSearch := map[int]bool{}
	modal := map[int]bool{}
	for _, d := range ds {
		if pickKind(d, 0) == "search" {
			namedSearch[d.Step] = true
		}
		if d.Kind == "mode" && d.Step >= 0 {
			modal[d.Step] = true
		}
	}
	routing := newAnswerRouting(ds, digSources)
	if ownBounce != nil {
		routing.ownBounce = ownBounce
	}
	routing.tapThenUntap = tapThenUntap
	if searchZones != nil {
		routing.searchZones = searchZones
	}
	searchZoneSuppress := searchZoneRedundantBooleans(ds, searchZones)
	// RemoveCounter's CounterType$ Any/All election is one per-counter run of
	// KChoose picks in gorge but one multi-amount dialog in XMage: fold the
	// run into one "X=<n>" amount per kind message before the generic choice
	// route scripts the raw kind labels (remove_counter_answers.go).
	counterAnswers, counterOwned := foldRemoveCounterKindAnswers(ds, snaps)
	// Trigger-order name answers are demoted back to the inert text form when
	// the step also scripts other asks (see the trigger_order case).
	var trigSpans []*trigSpan
	var pendingTrig *trigSpan
	// Setup ETB replacement choices (colour or creature type) are answered
	// before XMage places the seeded permanents, so they must LEAD step zero's
	// answer stream: the driver queues them before build() and XMage's as-enters
	// dialog consumes the head of the choice queue. Collect them apart rather
	// than appending in decision order, which would put a step-0 gameplay
	// answer that happens to precede the setup decision (Lifecraft Engine's
	// crew pick) in front of the as-enters dialog. Prepended after the loop.
	var setupAnswers []XAnswer
	// Setup-posed trigger orders (a seeded permanent's ETB order pends while
	// build() places it) share that pre-build queue. They queue AFTER every
	// as-enters choice: XMage poses the placement dialogs during build() and
	// the ordering ask only at the first priority, so a colour answer must
	// precede a name even when the runner recorded the order first.
	var setupTrigAnswers []XAnswer
	// The setup drive's other answers (a modal pick, an optional boolean or a
	// labelled choice the runner answered with its fallback) queue after the
	// as-enters choices and the trigger orders: XMage consumes the placement
	// dialogs during build(), the ordering ask at the first priority, and
	// these asks as the setup drive reaches their trigger.
	var setupAsks []XAnswer
	// Drive-window coin flips (Mana Crypt's turn-1 upkeep) queue after the
	// as-enters choices: the placement dialogs are consumed during build()
	// and the flip while the drive walks to the target turn. A flip that
	// shares the drive with a setup ask (setupAsks) has no recorded
	// interleaving against it — the flips are read off the log after the
	// drive — so the queue order between the two is the drive's phase order
	// only where the scenario poses no setup ask (the measured flip rows:
	// Mana Crypt trigger/activate pose none).
	var setupFlips []XAnswer
	// Gameplay-window flips, per step, in log order; prepended ahead of the
	// step's decision answers once the decisions are placed (a gameplay
	// flip resolves before the step's scripted dialogs re-read the queue;
	// the measured rows script nothing else on the flip's step).
	stepFlips := make([][]XAnswer, steps)
	for _, f := range flips {
		v := map[bool]string{true: "win", false: "lose"}[f.Win]
		if f.Step < 0 {
			setupFlips = append(setupFlips, XAnswer{f.Seat, "setup_flip", v})
			any = true
			continue
		}
		if f.Step < steps {
			stepFlips[f.Step] = append(stepFlips[f.Step], XAnswer{f.Seat, "flip", v})
			any = true
		}
	}
	for i, d := range ds {
		if d.Step < 0 {
			switch {
			case IsSetupChoice(d):
				pick := d.Picks[0]
				if colour, ok := bareColourLabel(d, 0); ok {
					pick = colour
				}
				setupAnswers = append(setupAnswers, XAnswer{d.Seat, "setup_choice", pick})
				any = true
			case isSetupNumber(d):
				a, _ := SetupNumberChoice(d)
				setupAnswers = append(setupAnswers, a)
				any = true
			case IsSetupTargetDecline(d):
				// The driver queues a target skip before build() so XMage
				// declines the optional ask instead of auto-picking.
				setupAnswers = append(setupAnswers, XAnswer{d.Seat, "setup_target", "[target_skip]"})
				any = true
			case triggerOrderSetupPosed(d, setup):
				// The last pick is never consumed: XMage asks only while more
				// than one ability is pending and pushes the last without an
				// ask (same drop as the gameplay name form below).
				for _, key := range setupTriggerOrderKeys(d) {
					setupTrigAnswers = append(setupTrigAnswers, XAnswer{d.Seat, "setup_choice", key})
				}
				any = true
			default:
				if as := setupDriveAnswers(d, modes); len(as) > 0 {
					setupAsks = append(setupAsks, as...)
					any = true
				}
			}
			continue
		}
		if searchZoneSuppress[i] {
			// The mapped SearchLibraryGraveyard* effect folds the optional
			// search choices into its own library chooseUse.
			continue
		}
		folded := d.Via == "target" || (d.Kind == "target" && modal[d.Step])
		if d.Step >= steps || (folded && d.Resume != "copy_targets" && castSteps[d.Step]) {
			// A cast step's own targets reach XMage through castSpell; a
			// target decision posed at a resolve step is scripted below. A
			// modal cast's fallback target is folded the same way (the mode
			// pick precedes it, so the runner records it Via "cast fallback").
			// A copy's new-target election is never a cast step's targets even
			// if it shares the step, so it is scripted below regardless.
			continue
		}
		if counterOwned[i] {
			if as := counterAnswers[i]; len(as) > 0 {
				out[d.Step] = append(out[d.Step], as...)
				any = true
			}
			continue
		}
		if as, ok := bandAnswers(d); ok {
			// A band declaration (CR 702.22c) is the pair of asks XMage's
			// Combat.handleBanding poses: a chooseUse "Form a band ...?" for
			// each member, then a TargetControlledPermanent pick of that
			// member. Gorge records one KChoose per accepted member or
			// decline, so an accepted object pick is yes + its ref and the
			// decline label is no. The generic choice route would queue the
			// gorge label, which chooseUse cannot read (it wants Yes/No) and
			// which would then be consumed by the target pick.
			if len(as) > 0 {
				out[d.Step] = append(out[d.Step], as...)
				any = true
			}
			continue
		}
		if as, owned := routing.route(i); owned {
			if len(as) > 0 {
				out[d.Step] = append(out[d.Step], as...)
				any = true
			}
			continue
		}
		if pickKind(d, 0) == "name" && namedSearch[d.Step] && len(d.Picks) == 1 {
			out[d.Step] = append(out[d.Step], XAnswer{d.Seat, "choice", d.Picks[0]})
			any = true
			continue
		}
		if forcedSingleOption(d) && !pipPlanned[i] && !forcedChoicePosed(ds, i) && !announcesSacrificeX(ds, i) && !castAnnouncesX(d, casts) && !activationAnnouncesX(d, activates) {
			// A forced one-option ask: XMage does not pose it. A forced
			// target-kind pick is the exception -- one legal opponent is still
			// a chooseTarget XMage asks for -- and so is a mode ask, which
			// XMage keeps posing even when only one mode is affordable.
			continue
		}
		var as []XAnswer
		switch d.Kind {
		case "target":
			// A CR 603.3d chain link's forced single target (Mechanical
			// Mobster's "target creature you control" with only itself) is
			// scripted like any other: XMage's Targets.chooseTargets asks
			// every target object that still has a candidate, so a missing
			// answer is "Missing TARGET def" (measured on SPM-s103 xe:SPM:0:3:w10,
			// with the first object picked and again with it skipped; an earlier
			// std-pass note that XMage picks it unasked no longer holds).
			as = targetDecisionAnswers(routing, i)
		case "mode":
			if d.Resume == "play" {
				// Optional Play effects (Discover, Cascade, and other carriers)
				// are XMage chooseUse asks, not numeric mode choices. Gorge's
				// recorded pick means cast; no pick means decline to hand.
				yes := len(d.Picks) > 0
				as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
				break
			}
			if d.Resume == "unless_pay" || d.Resume == "unless_decline" {
				// An unless-pay (UnlessCost$) is XMage's boolean chooseUse,
				// not a mode ask; the engine models it as a one-option mode
				// decision whose label is the decline ("Don't pay"). The
				// driver routes only the literals "yes"/"no" to setChoice
				// (boolean), so the raw label reaches setChoice(String) and
				// never answers the ask (std3: Dispelling Exhale, Spectral
				// Denial).
				if unlessPolarity(d) == "no" {
					as = append(as, XAnswer{d.Seat, "choice", "no"})
					break
				}
				if unlessPaidObjects(ds, i) {
					// A paid unless cost whose components are OBJECT picks
					// (Sac<, Discard<, Exile<, tapXType<, ...) is two asks in
					// XMage: the boolean chooseUse "Pay the cost?", then the
					// cost's own target group, which XMage poses as a
					// Player.choose -> makeChoose over the CHOICE queue (not
					// the target queue: SacrificeTargetCost.pay ->
					// TargetImpl.choose). The engine records the boolean as
					// the mode here and the picks as the following
					// "unless_cost" choose_n, which the ordinary choose_n
					// route below already joins into one aliased choice.
					// Answer the boolean yes here so chooseUse consumes
					// it; leaving the paid pick as the mode answer queued the
					// alias choice at chooseUse's head, which
					// assertAliasSupportInChoices rejects (Primeval Force,
					// Rathi Dragon).
					as = append(as, XAnswer{d.Seat, "choice", "yes"})
					break
				}
				// A paid mana/life unless cost (Lithobraking, Rottenmouth
				// Viper, Meathook Massacre II) has no component pick and agrees
				// with XMage only as the ordinary mode pick below.
			}
			if pickKind(d, 0) == "discard" || d.Resume == "discard" {
				// Gorge's discard card picker is KModes; XMage uses
				// TargetDiscard.choose -> makeChoose (not chooseMode). A
				// discard among same-named cards needs the exact-object
				// discriminator, exactly as the ordinary choice path does.
				for k, ref := range d.PickRefs {
					// discardLabel is the card name XMage's TargetDiscard matches;
					// the pick LABEL is often "Discard <name>", which would not
					// match. Only the exact-object discriminator is added.
					as = append(as, XAnswer{d.Seat, "choice", disambiguatedObjectChoice(d, k, oraclediffRefName(ref))})
				}
				if d.Max > len(d.PickRefs) {
					as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
				}
				break
			}
			// gorge offers only the modes with legal targets, so an option
			// index is not always the mode number; the label is when it names
			// a charm mode. A label outside the charm map is still a mode ask
			// for a non-charm modal (a plain modal), so fall back to the
			// option's 1-based position; the discard-style picker that shares
			// the kind is routed above.
			for k, i := range d.PickIdx {
				if m, ok := modeNumberFor(d, k, modes); ok && m == ModeChoiceQueue {
					// A DB$ GenericChoice | SetChosenMode$ True body (a
					// Theros-style Siege): XMage reads the pick through its
					// ChooseModeEffect -> controller.choose(Outcome.Neutral,
					// Choice, game), the CHOICE queue, showing the option
					// LABEL ("Abzan", "Khans") -- never a numeric mode. The
					// label is what gorge's mode decision already carries.
					as = append(as, XAnswer{d.Seat, "choice", d.Picks[k]})
					continue
				}
				if m, ok := modeNumberFor(d, k, modes); ok && (m == ModeYesQueue || m == ModeNoQueue) {
					// A two-way GenericChoice XMage asks as chooseUse: a
					// Timetwister-style per-player "may shuffle" (Turtles in
					// Time) or a source-proven xmageChooseUseFaces punisher
					// (Terrapact Intimidator's targeted opponent). The answer
					// is yes/no for whichever seat chose.
					as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[m == ModeYesQueue]})
					continue
				}
				n := i + 1
				if m, ok := modeNumberFor(d, k, modes); ok {
					n = m
				}
				as = append(as, XAnswer{d.Seat, "mode", fmt.Sprint(n)})
			}
			if max(d.Max, printedModeMax(d, modes)) > len(d.Picks) {
				// XMage keeps choosing modes up to Max -- the printed CharmNum
				// when gorge clamped Max to the modes it could offer (Sentinel
				// of Lost Lore); stop it with the mode queue's skip token
				// (including when gorge picked none).
				as = append(as, XAnswer{d.Seat, "mode", "[mode_skip]"})
			}
		case "yesno":
			yes := len(d.PickIdx) > 0 && d.PickIdx[0] == 0
			if len(d.Picks) > 0 {
				l := strings.ToLower(d.Picks[0])
				yes = strings.HasPrefix(l, "yes") || strings.HasPrefix(l, "accept") || strings.HasPrefix(l, "pay") || l == "true"
			}
			as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
		case "choose_n":
			if d.Resume == "changezone_dest_alt" {
				// gorge's DestinationAlternative ask (Dina's Guidance's "put it
				// into your hand or graveyard"). XMage asks it as a boolean
				// chooseUse("Hand", "Graveyard"), so a zone label is not an
				// answer (it is left over, unconsumed): script yes for gorge's
				// primary destination (option 0, also its default) and no for
				// the alternative.
				yes := len(d.PickIdx) == 0 || d.PickIdx[0] == 0
				as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
				break
			}
			if routing.divideOwned[i] {
				// The division ask's shares were emitted with its target
				// ask (dividedTargetAnswers).
				continue
			}
			if routing.manaHoistedPick(i) {
				// The colour answer was hoisted to the trigger-order span's
				// queue position (manaHoistSpan); emitting it here too would
				// double it.
				continue
			}
			if d.Resume == "damage_split" {
				if routing.ownsSplit(i) {
					// The shares were emitted at the target ask's own queue
					// position (before that ask's chain skips); emitting them
					// here as well would duplicate every "^X=" answer.
					continue
				}
				// Divided damage: the engine's split ask repeats one option index
				// per damage assigned (Fury, Forked Bolt, Twin Bolt). XMage's
				// chooseTargetAmount consumes one "<ref>^X=<share>" per chosen
				// target on the target queue, not makeChoose choices.
				as = damageSplitAnswers(d)
				break
			}
			if d.Resume == "twopiles_pick" && len(d.Picks) == 1 {
				// The pile pick (Curator of Destinies, Fact or Fiction) is
				// XMage's opponent.chooseUse("Face-down", "Face-up"), a
				// boolean choice, never gorge's "First pile"/"Second pile"
				// label (measured 2026-10-14: the label is "Found wrong
				// choice command"). Taking pile A -- the separator's pile,
				// the face-down pile XMage's TargetCard selected -- is the
				// boolean true.
				as = append(as, XAnswer{d.Seat, "choice", twoPilesPileAnswer(d)})
				break
			}
			if d.Resume == "mana_color" && len(d.ManaColours) > 1 {
				// A Produced$ "Combo <colours>" ask whose offered span
				// exceeds one colour is XMage's AddManaInAnyCombinationEffect
				// at every unit count: one multi-amount message per offered
				// colour, in the set's own order, zeroes included (Muerra,
				// Trash Tactician's Add R / Add G pair). The set rides the
				// decision (ManaColours); a produced-Any ask stays on the
				// routing below, which is what its colour dialog and WUBRG
				// multi-amount already agree with. A SINGLE-colour Combo set
				// (engine/effects/params/produced.go returns ok for one colour too)
				// keeps the ordinary choice routing: no committed row exists
				// to measure its dialog shape against, and switching it to
				// amount answers on a hunch would serve an unmeasured guess.
				as = manaColourAllocation(d)
				break
			}
			if d.Resume == "mana_color" && d.Min == d.Max && d.Max > 1 {
				// A multi-amount allocation (Combo Any, Desolation of Smaug):
				// one unit per picked option, options laid out unit*5+colour
				// over WUBRG. XMage poses one multi-amount message per colour
				// and needs every one filled, zeroes included.
				as = manaAllocationAnswers(d)
				break
			}
			if pickKind(d, 0) == "trigger_cost_pay" && len(d.Picks) == 1 && nonManaTriggerCostPay(d) {
				// The pay side of a triggered optional cost XMage poses as a
				// chooseUse. Gorge elected the cost, so answer XMage's
				// chooseUse yes. This must run BEFORE payment(): payment()
				// classifies by the "Pay " label prefix, which every non-mana
				// cost phrase ("Pay 1 life", "Pay 2 energy", "Pay 2 B and pay
				// 1 life") also carries, so letting it skip first would drop
				// exactly the asks DoIfCostPaid poses whenever canPay holds
				// (Melded Moxite's "Discard a card and draw two cards?",
				// Vampire Gourmand's "Sacrifice another creature and ...?").
				// A pure mana pay is resolved earlier by triggerCostAnswer
				// (TriggerCostMana); a raw-mana label that is not a pay kind
				// stays excluded by nonManaTriggerCostPay. A blight cost is a
				// non-mana cost like any other here: its creature pick follows
				// the accepted chooseUse (Boggart Mischief).
				as = append(as, XAnswer{d.Seat, "choice", "yes"})
				if names := forage[i]; len(names) > 0 {
					// The exile arm's three graveyard cards: XMage's
					// ForageCost asks them through a TargetCardInYourGraveyard
					// makeChoose once the chooseUse is accepted; gorge exiles
					// them without an ask (forageSettle), so they come from
					// the checkpoint.
					as = append(as, XAnswer{d.Seat, "choice", strings.Join(names, "^")})
				}
				break
			}
			if pipPlanned[i] {
				// A plan-covered pip: XMage poses this pip's own "Pay 2 life
				// instead of ... ?" chooseUse at its cost-order position
				// (handlePhyrexianCosts/handlePhyrexianManaCosts, printed
				// pips then granted ones, both engines in AnnouncePip order),
				// so the answer is this pip's recorded face, not a shared
				// yes: a mana-face pip scripts no and a life-face one yes
				// (dismember's pip1-mana/pip2-life mixed step).
				as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[pipPlans[d.Step][pipOrdinal[i]].life]})
				break
			}
			if phyrexianLifePicks(d) {
				// XMage asks "Pay 2 life instead of {X} (phyrexian cost)?"
				// before it will pay a Phyrexian pip (AbilityImpl
				// .handlePhyrexianCosts at CR 601.2b; ManaCostsImpl
				// .handlePhyrexianManaCosts for a cost paid later), so a
				// recorded life face is a chooseUse "yes", not payment UI.
				// Keyed on the engine's pay_life option kind, so every
				// source of the ask takes it (cast, activate, triggered
				// cost, K'rrik's PayLifeInsteadOf grant). A hybrid or
				// twobrid face (pay_W/pay_generic) is still paid from the
				// pool without a question and keeps the payment() skip.
				for range d.Picks {
					as = append(as, XAnswer{d.Seat, "choice", "yes"})
				}
				break
			}
			if payment(d.Picks) {
				// Hybrid/phyrexian halves are payment UI; XMage pays from
				// the pool without asking.
				continue
			}
			if n := colourPickCount(d); n > 1 && n == len(d.Picks) {
				// A multi-unit "any combination of colors" mana ability
				// (Baxter Building's "Add four mana in any combination of
				// colors", Amount$ 4) is not one XMage colour dialog. XMage
				// asks a multi-amount distribution among colours, so a joined
				// "^" colour selection throws "Missing choice in multi
				// amount". Emit nothing and let XMage distribute it itself,
				// as it did before this routing existed.
				continue
			}
			if len(d.Picks) == 1 && strings.HasPrefix(d.Picks[0], "X = ") {
				answer := XAnswer{d.Seat, "choice", "X=" + strings.TrimPrefix(d.Picks[0], "X = ")}
				as = append(as, answer)
				for n := 1; activationAnnouncesX(d, activates) && n < activates[d.Step]; n++ {
					// XMage's VariableManaCost and a separate VariableCostImpl
					// announce X independently (Ludevic, Necrogenius's mana
					// {X} then ExileXFromYourGraveCost).
					as = append(as, answer)
				}
				break
			}
			if len(d.Picks) == 1 && pickKind(d, 0) == "number" {
				if _, err := strconv.Atoi(d.Picks[0]); err == nil {
					// "Choose a number": XMage's getAmount reads an "X=<n>"
					// choice, exactly like announceX (TestPlayer.getAmount).
					as = append(as, XAnswer{d.Seat, "choice", "X=" + d.Picks[0]})
					break
				}
			}
			if yn, ok := yesNo(d); ok {
				as = append(as, XAnswer{d.Seat, "choice", yn})
				break
			}
			if d.Resume == "sacrifice" && d.Min == 0 && len(d.Picks) > 0 {
				// An accepted optional sacrifice ("any opponent may sacrifice
				// a creature"): XMage asks chooseUse before the pick
				// (DesecrationDemon.java:77, DoIfCostPaid), so the pick needs
				// a "yes" ahead of it. The decline already scripts "no".
				as = append(as, XAnswer{d.Seat, "choice", "yes"})
			}
			if hiddenPickUnanswerable(d) {
				// A hidden-zone pick (a graveyard "you may return a land
				// card") among same-named cards no exact answer can name: the
				// bare label would let XMage return whichever one it meets
				// first. Script nothing; the replay reports the missing
				// answer instead of a false divergence (CR 608.2d).
				break
			}
			if len(d.Picks) > 1 && allChoiceQueue(d) {
				// One makeChoose dialog consumes ONE definition, whose own
				// parser splits on '^' into the multi-card selection (Dig's
				// "put two of them into your hand", a discard-two). Emitting
				// one answer per pick would let each pick answer a separate
				// dialog and leave the rest as an unused leftover.
				as = append(as, XAnswer{d.Seat, "choice", JoinedChoice(d)})
			} else {
				for k, label := range d.Picks {
					switch pickQueue(d, k, label) {
					case "skip":
						// XMage resolves this pick inside its computer player (a
						// library search) or pays it from the pool (a mana-tapping
						// cost); a scripted answer would only be an unused leftover.
						continue
					case "target":
						as = append(as, XAnswer{d.Seat, "target", targetPickValue(d, k, label)})
						continue
					}
					// The choice queue: makeChoose shows the option's label, which
					// for an unlabelled object pick is the object's name.
					if label == "" && k < len(d.PickRefs) {
						label = oraclediffRefName(d.PickRefs[k])
					}
					if colour, ok := bareColourLabel(d, k); ok {
						label = colour
					}
					label = disambiguatedObjectChoice(d, k, label)
					if colour, ok := manaColourLabel(label); ok {
						label = colour
					}
					as = append(as, XAnswer{d.Seat, "choice", label})
				}
			}
			switch {
			case len(d.Picks) == 0:
				// Declined: XMage may pose it as a yes/no or as an "up to"
				// pick; script both (measured: Zimone's Experiment agrees
				// only with this pair).
				as = append(as, XAnswer{d.Seat, "choice", "no"}, XAnswer{d.Seat, "target", "[target_skip]"})
			case d.Max > len(d.Picks) && len(as) > 0 && as[len(as)-1].Kind == "target":
				// Fewer than "up to N" on the target queue: stop XMage
				// picking more.
				as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
			case d.Max > len(d.Picks) && len(as) > 0:
				// A short makeChoose; its queue has its own skip token.
				as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
			}
		case "order":
			if d.GorgeKind == "trigger_order" {
				// XMage's chooseTriggeredAbility matches a scripted choice
				// against the ability's rule text (getRule) or its source
				// object's NAME. Gorge's label carries the card's
				// TriggerDescription$, which is XMage's rule wording only by
				// coincidence ("put a +1/+1 counter on this creature" against
				// XMage's "...put a +1/+1 counter on {this}"), so a text
				// choice misses and the replay's fallback player orders the
				// stack itself (the Adrenaline Jockey/trigger#0.1 drift). Name
				// the pick's source object instead: that is the name XMage
				// matches (a ref would carry the "@" alias form the trigger
				// ask rejects). XMage asks for the next ability only while
				// more than one is pending (its loop pushes the last
				// remaining one without an ask), so the final pick is never
				// consumed and is dropped here.
				//
				// The name only steers when the picks' sources are DISTINCT:
				// two simultaneous triggers off ONE source (Thundertrap
				// Trainer's ETB and Offspring, both p0:Thundertrap Trainer)
				// answer with the same name, which XMage cannot tell apart --
				// there the rule text is the only distinguishing answer, and
				// it already agrees. So a same-source order keeps the old
				// inert text form; only a distinct-source order names each
				// source. (This is also what keeps the level-A cast-resolve
				// scenarios whose order ask is incidental -- Thundertrap
				// Trainer, Molten Man -- byte-identical to the frozen verdict.)
				//
				// A plain name is also CONSUMABLE by any later makeChoose XMage
				// poses, unlike the inert rule text: when the step's answers
				// also script other asks, a leftover name (the ask XMage posed
				// at another point than gorge recorded, e.g. Baron Strucker's
				// ETB order) derails them. So the name form is kept only when
				// this step scripts nothing else; shared steps keep the old
				// inert text form (demoteTriggerOrderSpans).
				if !triggerOrderNamesDistinct(d) {
					// A same-source span whose step later asks a mana colour
					// pick (Ashling, Rimebound's OrTriggeredAbility mana
					// trigger): the colour dialog fires while the span's inert
					// rule texts sit at the queue head and pops the first one
					// -- the text form is unconsumable only while no other
					// dialog fires. So the colour answer leads the stream and
					// the span scripts nothing; any later order ask falls to
					// the replay's fallback player, as the inert form already
					// let it.
					if ans, ok := routing.manaHoistSpan(i); ok {
						as = append(as, ans)
						break
					}
					// XMage's rule-text form steers the ask and is consumed
					// by it (xmageTriggerOrderKeys); picks it cannot key
					// apart keep the inert text form.
					if keys, ok := xmageTriggerOrderKeys(d); ok {
						for _, key := range keys {
							as = append(as, XAnswer{d.Seat, "choice", key})
						}
						break
					}
					as = append(as, triggerOrderTextAnswers(d)...)
					break
				}
				n := len(d.Picks)
				for k := 0; k < n-1; k++ {
					as = append(as, XAnswer{d.Seat, "choice", triggerOrderChoice(d, k)})
				}
				if len(as) > 0 {
					pendingTrig = &trigSpan{step: d.Step, count: len(as), old: demotedTriggerOrderAnswers(d), textBlind: triggerOrderTextBlind(d)}
				}
				break
			}
			// Arrange first asks which cards move; its follow-up ordering is
			// also on XMage's choice queue. Keeping all cards is a choice skip.
			if d.GorgeKind == "arrange" && arrangeEmpty(d) && (d.ArrangeKind == "graveyard" || d.ArrangeKind == "bottom") {
				// Every looked-at card goes away (gorge's empty kept pile).
				// XMage's selection IS the cards that go, so script each looked-at card's name on the choice queue: a
				// leading card name takes precedence over the skip
				// (librarySelectionQueue), the queue then ends with nothing
				// further to consume, and no order answers follow. The generic
				// partial-selection arm below would emit [choice_skip], which
				// XMage reads as keep-all -- the opposite outcome.
				for _, label := range d.ArrangeLabels {
					as = append(as, XAnswer{d.Seat, "choice", label})
				}
			} else if d.GorgeKind == "arrange" {
				forcedOrder := d.Min == d.Max && d.Max == d.Options
				// The producer, not the option Kind, decides which question the
				// forced order answers: a full top reorder
				// (effRearrangeTopOfLibrary), a CR 401.4 library_placement
				// arrangement on top, and a Dig all-to-bottom remainder all
				// offer every card with one shared Kind, but they ask opposite
				// orders. gorge's ResumeKind plus the after-step library top
				// (topPlacement) is what tells them apart.
				fullTopReorder := d.Resume == "arrange" && d.ArrangeKind == "bottom" &&
					len(d.Picks) == d.Options ||
					d.ArrangeKind == "library_placement" && topPlacement[i] &&
						len(d.Picks) == d.Options
				if forcedOrder && fullTopReorder {
					// gorge's picks are the final top-first order, but XMage's
					// putCardsOnTopOfLibrary asks only while more than one card
					// is left and auto-tops the card never named (last one
					// chosen is topmost). Script the last N-1 picks reversed:
					// picks[0] is the topmost card and stays unnamed.
					for k := len(d.Picks) - 1; k >= 1; k-- {
						as = append(as, XAnswer{d.Seat, "choice", d.Picks[k]})
					}
				} else if forcedOrder && d.ArrangeKind == "dig_bottom" && len(d.Picks) > 0 {
					// A Dig's all-to-bottom remainder: gorge's answer order is
					// the bottom order, picks[0] highest. XMage's
					// putCardsOnBottomOfLibrary asks "card ORDER to put on the
					// BOTTOM" only while more than one card is left (last one
					// chosen is bottommost) and places the final card unasked,
					// so script the first N-1 picks: a scripted last pick was
					// left over ("(Choices of PlayerA) Count are not equal
					// (found [Forest])", Growing Rites of Itlimoc's four).
					for _, label := range d.Picks[:len(d.Picks)-1] {
						as = append(as, XAnswer{d.Seat, "choice", label})
					}
				} else if forcedOrder {
					// A forced bottom order (Min == Max == Options) has no
					// selection dialog: the answer order IS the bottom order.
					for _, label := range d.Picks {
						as = append(as, XAnswer{d.Seat, "choice", label})
					}
				} else if len(d.PickIdx) == d.Options {
					// Keeping every card where it is: XMage's surveil/scry
					// selection is a TargetCard on the target queue, and a
					// skip dismisses it. Measured against XMage on the std
					// pass (Refute Destiny, Proctor of Potential, ... -- 25
					// cards that agree only with this answer); no ORDER
					// answer follows, XMage keeps the cards in place.
					as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
				} else if binned, kept, ok := libraryArrangePartition(d.ArrangeLabels, d.PickIdx, d.Options); ok {
					// XMage's selection is pile B, the cards that go to the
					// graveyard/bottom; the labels after the skip are pile A's
					// top order, in PickIdx order. Naming the kept cards as the
					// selection sends them away too and drops the order.
					for _, label := range binned {
						as = append(as, XAnswer{d.Seat, "choice", label})
					}
					if len(kept) > 0 {
						as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
						for _, label := range kept {
							as = append(as, XAnswer{d.Seat, "choice", label})
						}
					}
				}
				// A decision without the full offered label list cannot be
				// split into the two piles (libraryArrangePartition refuses
				// it) and is left unscripted: guessing a split would send the
				// kept cards away. gorge records the labels of every KArrange.
			} else {
				continue
			}
		default:
			continue
		}
		if topPlacement[i] && HandMoveBatch(d) && orderAsks[i] {
			// XMage asks the hand selection first, then its "card ORDER to
			// put … from the TOP" dialog N-1 times and auto-tops the card
			// never named. The selection answer above already names all
			// picks; the order answers follow it, last N-1 picks reversed,
			// so picks[0] stays the unnamed topmost card. Only a verified
			// order-dialog effect gets these answers: a self put-back
			// (Brainstorm, Dream Cache) has no such ask, and an extra
			// choice would derail the next dialog.
			for k := len(d.Picks) - 1; k >= 1; k-- {
				as = append(as, XAnswer{d.Seat, "choice", d.Picks[k]})
			}
		}
		if len(as) > 0 {
			if pendingTrig != nil {
				pendingTrig.start = len(out[d.Step])
				trigSpans = append(trigSpans, pendingTrig)
				pendingTrig = nil
			}
			out[d.Step] = append(out[d.Step], as...)
			any = true
		}
	}
	if !any {
		return nil
	}
	// Gameplay flips lead their steps: prepend in log order, which the
	// per-step slices accumulated, ahead of every decision answer the loop
	// appended. AFTER demoteTriggerOrderSpans, whose span start/count were
	// recorded against the loop's pre-prepend indices (gen.go above): a
	// prepend first would shift them onto the step's other answers and splice
	// the inert text over them (dropping the scripted answers). Demoting on
	// the unshifted list keeps the flips leading either way.
	demoteTriggerOrderSpans(out, trigSpans)
	for st, fs := range stepFlips {
		if len(fs) > 0 {
			out[st] = append(fs, out[st]...)
		}
	}
	if len(setupAnswers) > 0 || len(setupTrigAnswers) > 0 || len(setupAsks) > 0 || len(setupFlips) > 0 {
		// Setup answers are read from xmage_answers[0] before build(), even
		// when the scenario has no gameplay steps.
		if len(out) == 0 {
			out = append(out, nil)
		}
		lead := make([]XAnswer, 0, len(setupAnswers)+len(setupTrigAnswers)+len(setupAsks)+len(setupFlips))
		lead = append(lead, setupAnswers...)
		lead = append(lead, setupFlips...)
		lead = append(lead, setupTrigAnswers...)
		lead = append(lead, setupAsks...)
		out[0] = append(lead, out[0]...)
	}
	return out
}
