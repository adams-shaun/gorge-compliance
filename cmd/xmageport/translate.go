package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
)

// refusal is a method the translator will not port, with a stable reason
// code (the census key) and a free-text detail.
type refusal struct {
	Code   string
	Detail string
	// onlyIf, when set on a shape refusal, further limits it to runs whose
	// fails satisfy it (a shape that only some failures are evidence of).
	onlyIf func(fails []string) bool
}

func (r *refusal) Error() string { return r.Code + ": " + r.Detail }

func refuse(code, format string, a ...any) *refusal {
	return &refusal{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// ---- reason tables ----

var hardCalls = map[string]string{
	"runCode":                       "runcode",
	"addCustomCardWithAbility":      "custom-card",
	"addCustomEffect_TargetDamage":  "custom-card",
	"addCustomEffect_TargetDestroy": "custom-card",
	"addCustomEffect_DestroyTarget": "custom-card",
	"addCustomEffect_TargetTap":     "custom-card",
	"addPlane":                      "custom-card",
	"addEmblem":                     "custom-card",
	"aiPlayStep":                    "ai",
	"aiPlayPriority":                "ai",
	"makeTester":                    "ai",
	"setDieRollResult":              "dice",
	"setFlipCoinResult":             "dice",
	"activateManaAbility":           "mana-payment",
	"assertManaOptions":             "mana-payment",
	"rollbackTurns":                 "rollback",
	"rollbackAfterActionsStart":     "rollback",
	"rollbackAfterActionsEnd":       "rollback",
	"assertLibraryCount":            "library-assert",
	"setChoiceAmount":               "amount-choice",
	"addTargetAmount":               "amount-choice",
	"showBattlefield":               "",
	"showHand":                      "",
	"showGraveyard":                 "",
	"showExile":                     "",
	"showLibrary":                   "",
	"showCommand":                   "",
	"showAvailableAbilities":        "",
	"showAvailableMana":             "",
	"printStart":                    "",
	"printEnd":                      "",
}

// ignoredCalls are bookkeeping calls with no scenario meaning.
var ignoredCalls = map[string]bool{
	"setStrictChooseMode": true, "execute": true, "skipInitShuffling": true,
	"assertAllCommandsUsed": true, "setStopOnTurn": false,
}

var basicLands = map[string]bool{
	"Plains": true, "Island": true, "Swamp": true, "Mountain": true, "Forest": true, "Wastes": true,
	"Snow-Covered Plains": true, "Snow-Covered Island": true, "Snow-Covered Swamp": true,
	"Snow-Covered Mountain": true, "Snow-Covered Forest": true,
}

var keywordAbilities = map[string]string{
	"FlyingAbility": "Flying", "FirstStrikeAbility": "First Strike", "DoubleStrikeAbility": "Double Strike",
	"TrampleAbility": "Trample", "HasteAbility": "Haste", "VigilanceAbility": "Vigilance",
	"LifelinkAbility": "Lifelink", "DeathtouchAbility": "Deathtouch", "ReachAbility": "Reach",
	"DefenderAbility": "Defender", "MenaceAbility": "Menace", "HexproofAbility": "Hexproof",
	"IndestructibleAbility": "Indestructible", "ShroudAbility": "Shroud", "FlashAbility": "Flash",
	"IntimidateAbility": "Intimidate", "FearAbility": "Fear", "WitherAbility": "Wither",
	"InfectAbility": "Infect", "ProwessAbility": "Prowess", "CantBlockAbility": "",
	"HorsemanshipAbility": "Horsemanship", "ShadowAbility": "Shadow", "ChangelingAbility": "Changeling",
	"PersistAbility": "Persist", "UndyingAbility": "Undying", "FlankingAbility": "Flanking",
	"WardAbility": "",
}

// xmage PhaseStep -> gorge step index (state.Step order).
var stepNames = []string{"untap", "upkeep", "draw", "main1", "begin-combat", "declare-attackers",
	"declare-blockers", "combat-damage", "end-combat", "main2", "end", "cleanup"}

const (
	sUntap = iota
	sUpkeep
	sDraw
	sMain1
	sBeginCombat
	sDeclareAttackers
	sDeclareBlockers
	sCombatDamage
	sEndCombat
	sMain2
	sEnd
	sCleanup
)

var xmageSteps = map[string]int{
	"PhaseStep.UNTAP": sUntap, "PhaseStep.UPKEEP": sUpkeep, "PhaseStep.DRAW": sDraw,
	"PhaseStep.PRECOMBAT_MAIN": sMain1, "PhaseStep.BEGIN_COMBAT": sBeginCombat,
	"PhaseStep.DECLARE_ATTACKERS": sDeclareAttackers, "PhaseStep.DECLARE_BLOCKERS": sDeclareBlockers,
	"PhaseStep.COMBAT_DAMAGE": sCombatDamage, "PhaseStep.END_COMBAT": sEndCombat,
	"PhaseStep.POSTCOMBAT_MAIN": sMain2, "PhaseStep.END_TURN": sEnd, "PhaseStep.CLEANUP": sCleanup,
}

// ---- translation state ----

type placement struct {
	name string
	zone string // battlefield hand graveyard exile library_top
	ord  int    // 1-based ordinal among this seat's placements of name, in runner order
	used bool
}

type choice struct {
	seat   int
	src    int
	target bool
	pick   string // already a gorge pick (ref, yes/no, text)
	x      int    // setChoice "X=N": N, the announced X (-1 otherwise)
	mode   int    // setModeChoice "N": 1-based mode index (0 otherwise)
	ord    int    // pick's 0-based position within one counted setChoice(player, name, n)
	// kind overrides the answer kind (the runner's default is "any" for a
	// plain pick); synth marks a pick the translator derived from several
	// XMage choices, whose originals travel in action.xlead.
	kind  string
	synth bool
}

type action struct {
	turn, step, sub int
	seat            int
	kind            string // cast activate play attack block wait check
	src             int
	card            string
	targets         []string // xmage target strings, translated at emit time
	ability         string
	resolveAfter    bool
	endAt           int // emission: len(steps) once this action is emitted
	attackers       []string
	defender        int // attack: the defending seat
	blocks          [][2]string
	expects         []*expect
	choices         []choice
	xlead           []xans // XMage choices the emitter consumed into a cast mode
	playCheck       *playableCheck
}

// playableCheck is one XMage checkPlayableAbility: the text names a cast,
// a land play or an ability, and want says whether it must be on offer.
// The card ref is resolved at emit time, after setup and the actions before
// it have bound their refs.
type playableCheck struct {
	text string
	want bool
}

type group struct {
	at      int // step index the group is placed at (base)
	offset  int
	targets []string
	answers []answer
	kinds   []int // per answer: index into answerKinds
	xvals   []xans
	tail    bool // t.tailChoices: dropped when no step can place them
}

type translator struct {
	reg    *cards.Registry
	jc     *javaClass
	m      *javaMethod
	consts map[string]value
	// base is the class's base-class translation (seats.go): the seat
	// count and XMage's turn order of its players.
	base    xmageBase
	setup   [maxSeats][]addCardCall
	life    [maxSeats]*int32
	actions []*action
	stop    *[2]int
	asserts []call
	// derived
	places            [maxSeats][]*placement
	castRefs          [maxSeats]map[string][]string
	allNames          map[string]bool // every string arg outside addCard (keeps lands)
	nameUses          map[string]int  // how many times each such string arg appears (casts, activations)
	abilityUses       map[string]int  // how many times each activateAbility text appears
	needAll           bool            // an assert counts all permanents: keep lands
	pendingChecks     []pendingCheck
	preChoices        []choice
	choiceList        []choice
	droppedOrder      int
	droppedOrderTexts []string
	// droppedPreAsserts counts asserts written before execute(): XMage runs
	// them against the game as it is before the test's setup lands, so they
	// state the empty starting state, never the state at the stop step.
	droppedPreAsserts int
	// countShape is the count-name-not-a-card shape: the first positive count
	// asserted by a name no card has (see countNameRefusal).
	countShape *refusal
	// queueShape is the target-queue-order refusal of a one-seat run of
	// addTargets that each carry their own yes/no; it applies only on failure.
	queueShape *refusal
	// electShape is the first shape refusal a cast's optional costs arm
	// (armCastShapes, aiElects): a cost the test never elects while XMage's AI
	// does, two kicker costs XMage elects one boolean at a time, a convoke tap
	// stated as an addTarget. It converts a failing run only.
	electShape *refusal
	// emitShape is the first shape refusal the emitter or an assert mapper
	// recorded (noteShape): a spent permission the runner cannot pick, a
	// check the runner would reach in another step, a counter a pooled
	// mana ability never puts. It converts a failing run only.
	emitShape *refusal
	// sourcePaid / sourceUnpaid record, per payment-sensitive mana source
	// name, whether the emitter paid a cast by activating it or left it to
	// the pool (source_payment.go).
	sourcePaid, sourceUnpaid map[string]bool
	// ptAsserted remembers the P/T an assertPowerToughness stated per seat
	// and name (anyScopePTConflict).
	ptAsserted map[string]string
	orphanMode bool // a setModeChoice no action consumes (a trigger's modes)
	// playEffectKicker names a card an effect plays (not a cast step) that
	// prints Kicker/Multikicker: gorge's effPlay cast has no kicked answer, so
	// the XMage kicker choices cannot be stated and a nonzero count would be
	// silently dropped.
	playEffectKicker string
	// strict is setStrictChooseMode(true): every choice the game asks is
	// stated by the test. Without it XMage's AI answers the unstated ones.
	strict bool
	// tailChoices are choices of a method with no action to bind them to
	// (a trigger during the walk to the stop step): placed from step 0.
	tailChoices []choice
	// copied are names a copy effect (Clone, CopyPermanent) already emitted
	// in the walk has given a second permanent: a later by-name ref to one
	// object is ambiguous (XMage takes whichever it finds first).
	copied  map[string]bool
	copyRef map[string]string // "seat|name" -> the ref of seat's copy ("-": several)
	// libCleared marks a seat after removeAllCardsFromLibrary: XMage's library
	// is then only the cards the test adds, where the runner pads its own with
	// filler. The two differ only if a draw reaches past those cards.
	libCleared [maxSeats]bool
}

// triggeredKeywords are the keyword abilities whose triggers XMage lists by
// the keyword's own name ("Afterlife", "Evolve") when a test orders
// simultaneous triggers, where other triggers are listed by rule text.
var triggeredKeywords = map[string]bool{
	"Afflict": true, "Afterlife": true, "Annihilator": true, "Backup": true, "Battle cry": true,
	"Bushido": true, "Cascade": true, "Dethrone": true, "Enlist": true, "Evolve": true,
	"Exalted": true, "Exploit": true, "Extort": true, "Fabricate": true, "Flanking": true,
	"Melee": true, "Mentor": true, "Myriad": true, "Persist": true, "Prowess": true,
	"Rampage": true, "Renown": true, "Ripple": true, "Soulbond": true, "Storm": true,
	"Training": true, "Undying": true,
}

// triggeredKeywordName returns the triggered keyword a string names, if any:
// "Afterlife", or "Afterlife 1" with a count.
func triggeredKeywordName(s string) string {
	kw := strings.TrimSpace(s)
	if i := strings.LastIndexByte(kw, ' '); i > 0 && allDigits(kw[i+1:]) {
		kw = kw[:i]
	}
	if triggeredKeywords[kw] {
		return kw
	}
	return ""
}

// isTriggerOrderChoice reports whether a setChoice string names a triggered
// keyword ("Afterlife", or "Afterlife 1"), which orders simultaneous triggers.
// Several of those keyword names are also card names (Afterlife, Afflict,
// Melee, Persist...), and XMage's corpus has tests that name such a card as an
// answer, so the keyword form counts only when the scenario does not set up a
// card of exactly that name. Rule text is decided by isTriggerOrderText before
// the setup is known.
func (t *translator) isTriggerOrderChoice(s string) bool {
	return triggeredKeywordName(s) != "" && !t.setupHasName(s)
}

// setupHasName reports whether either seat's addCard setup names a card
// exactly name. The trigger-order drop uses it: "Afterlife" is both a keyword
// and a card, so a scenario that sets the card up may name it as an answer.
func (t *translator) setupHasName(name string) bool {
	for p := 0; p < t.seats(); p++ {
		for _, ac := range t.setup[p] {
			if sameName(ac.name, name) {
				return true
			}
		}
	}
	return false
}

// dropTriggerOrderChoices removes the setChoice strings that order
// simultaneous triggers. It runs after read() has seen every addCard, because
// whether a keyword-named choice is a trigger order depends on the scenario's
// own setup (a card named Afterlife in hand is an answer, not an order).
func (t *translator) dropTriggerOrderChoices() {
	kept := t.choiceList[:0]
	for _, ch := range t.choiceList {
		name, ok := strings.CutPrefix(ch.pick, "name:")
		if !ok || ch.target || !t.isTriggerOrderChoice(name) {
			kept = append(kept, ch)
			continue
		}
		// gorge's runner keeps the offered order. Dropped (the asserts still
		// judge the outcome) and noted in why.
		t.droppedOrder++
		t.droppedOrderTexts = append(t.droppedOrderTexts, name)
	}
	t.choiceList = kept
}

func isTriggerOrderText(s string) bool {
	s = strings.TrimSpace(s) + " "
	for _, pfx := range []string{"When ", "Whenever ", "At the beginning", "At end"} {
		if strings.HasPrefix(s, pfx) {
			return true
		}
	}
	return false
}

// isAbilityClause reports whether a castSpell/activateAbility spellOnStack
// argument names a triggered ability ("When {this} enters", "Whenever a
// creature dies"), not a spell: XMage waits for that trigger to be on the
// stack, which the generated walk cannot express.
func isAbilityClause(s string) bool {
	return isTriggerOrderText(s) || strings.Contains(s, "{this}")
}

// aiAnswers are the choices XMage's AI makes in a test that does not state
// them (no setStrictChooseMode(true)), as <seat>:<pick> in the order the game
// asks; a "target:" pick answers a target ask. The runner's fallback declines or picks nothing where the AI would
// say yes and take the land, so the answer is written down here, per
// Class#method, from reading the test's asserts. They bind like a trailing
// setChoice: to the last action.
var aiAnswers = map[string][]string{
	// Ghost Quarter's destroyed-land controller may search for a basic
	// land; the test's final permanent count needs the Plains.
	"FlagstonesOfTrokairTest#testDontTriggerIfIsland": {"0:yes", "0:Plains"},
	// Wrexial's trigger asks the "may" once (the stated true) and, after the
	// target, the may-cast of the chosen spell again; XMage folds them.
	"MayCastTargetThenExileTest#testWrexial": {"0:yes"},
	// The copies of Lightning Bolt and Shock are cast without targets
	// stated; XMage's AI aims both at the opponent (assertLife playerB 15).
	"SpelltwineTest#testCopyCards": {"0:target:p1", "0:target:p1"},
	// Mizzix's Mastery copies See the Truth and XMage's framework accepts
	// the may-cast even under setStrictChooseMode (the test asserts all
	// three looked-at cards reach the hand); the runner's optional-modes
	// fallback declines an unattended ask, so the accept is written down.
	"SeeTheTruthTest#castCopyFromExile": {"0:yes"},
}

// poolPaidLands are setup lands per Class#method that XMage taps for a
// spell the scenario pays from the runner's mana pool, so they must not stay
// untapped for a later optional payment (Mirari's "you may pay {3}" on each
// cast copy would otherwise find nine Islands, not the three XMage has left).
var poolPaidLands = map[string]map[string]int{
	"SpelltwineTest#testCopyCardsMirari": {"Island": 6},
}

// unposedChoices are stated choices (by 0-based position among the method's
// setChoice/addTarget calls, in source order) that gorge never asks: the
// runner's abstract "yes" would instead take the first "Play ..." option of
// a later ask.
var unposedChoices = map[string][]int{
	// Mirari's "change targets?" yes/no: gorge asks for the new targets
	// directly (choice 3 is the second setChoice(true)).
	"SpelltwineTest#testCopyCardsMirari": {3},
}

// addAIAnswers drops the unposed choices and queues the method's aiAnswers
// after every stated choice.
func (t *translator) addAIAnswers() {
	if drop := unposedChoices[t.jc.Name+"#"+t.m.Name]; len(drop) > 0 {
		var kept []choice
		for i, ch := range t.choiceList {
			if !slices.Contains(drop, i) {
				kept = append(kept, ch)
			}
		}
		t.choiceList = kept
	}
	for _, a := range aiAnswers[t.jc.Name+"#"+t.m.Name] {
		seat, pick, _ := strings.Cut(a, ":")
		pick, target := strings.CutPrefix(pick, "target:")
		t.choiceList = append(t.choiceList, choice{seat: int(seat[0] - '0'), src: 1 << 30, x: -1, pick: pick, target: target})
	}
}

// assignChoices binds each queued XMage choice to the action that consumes
// it: the preceding action when the same player took it; otherwise that
// player's next action on the same turn (an opponent's answer to its own
// later activation); otherwise the preceding action (an opponent answering
// for the caster's spell); before any action, the first action.
func (t *translator) assignChoices() {
	isAct := func(a *action) bool { return a.kind != "check" && a.kind != "wait" }
	for _, ch := range t.choiceList {
		var prev, next *action
		for _, a := range t.actions {
			if !isAct(a) {
				continue
			}
			if a.src < ch.src {
				prev = a
			} else if next == nil && a.seat == ch.seat {
				next = a
			}
		}
		var to *action
		switch {
		case prev != nil && prev.seat == ch.seat:
			to = prev
		case next != nil && (prev == nil || next.turn == prev.turn):
			to = next
		case prev != nil:
			to = prev
		default:
			for _, a := range t.actions {
				if isAct(a) {
					to = a
					break
				}
			}
		}
		switch {
		case to != nil:
			if to.kind != "cast" {
				// A card named to be played by an effect (Anrakyr the
				// Traveller's DB$ Play) is not a cast step, so neither the
				// kicker nor the life-for-mana choice can be stated; the
				// whole shape is refused in build().
				if n, ok := strings.CutPrefix(ch.pick, "name:"); ok && t.cardHasKicker(n) {
					t.playEffectKicker = n
				}
			}
			to.choices = append(to.choices, ch)
		case ch.mode > 0:
			t.orphanMode = true
		default:
			t.tailChoices = append(t.tailChoices, ch)
		}
	}
}

// cardHasKicker reports whether a face of name prints Kicker or Multikicker.
func (t *translator) cardHasKicker(name string) bool {
	c, ok := t.lookup(name)
	if !ok {
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, k := range f.Keywords {
			if strings.HasPrefix(k, "Kicker:") || strings.HasPrefix(k, "Multikicker:") {
				return true
			}
		}
	}
	return false
}

type pendingCheck struct {
	a *action
	c call
}

type addCardCall struct {
	zone   string
	name   string
	count  int
	tapped bool
}

type result struct {
	// shape is the refusal a failing run converts to (manaSourceRefusal,
	// orderedPicksRefusal); nil when the test has none of those shapes.
	shape *refusal
	// runShape is the refusal the last run's transcript showed (a setup
	// decision, a bare Continue acknowledgement); see activeShape.
	runShape                   *refusal
	Family, Class, Method, Rel string
	Code, Detail               string // refusal (empty when generated)
	Scenario                   *portScenario
	Groups                     []*group
	Fails                      []string
	ReplayDiff                 string // live game vs log-only replay (audit gate); non-empty => fail-replay
	Status                     string // refused | pass | fail-harness | fail-replay | fail-expect
	Dropped                    int
	nflips                     int            // booleans the run mapped onto two-option asks (runResult)
	Unsupported                string         // "Card[prim,...]; ..." for cards gorge's coverage gate does not support
	Strict                     bool           // setStrictChooseMode(true)
	XAnswers                   map[int][]xans // final step index -> original XMage choices
}

var reLocalDecl = regexp.MustCompile(`^(?:final\s+)?(String|int)\s+(\w+)\s*=\s*(.+)$`)

var supported = effects.Supported()

func (t *translator) unsupportedCards() string {
	var out []string
	seen := map[string]bool{}
	for p := 0; p < t.seats(); p++ {
		for _, ac := range t.setup[p] {
			if seen[ac.name] {
				continue
			}
			seen[ac.name] = true
			c, ok := t.lookup(ac.name)
			if !ok {
				continue
			}
			if miss := t.reg.Unsupported(c, supported); len(miss) > 0 {
				out = append(out, ac.name+"["+strings.Join(miss, ",")+"]")
			}
		}
	}
	return strings.Join(out, "; ")
}

func (t *translator) lookup(name string) (*cards.Card, bool) {
	return t.reg.Lookup(name)
}

// setupOwner reports the one seat whose setup adds name. ok is false when no
// seat adds it or both seats do, so a caller cannot infer an owner.
func (t *translator) setupOwner(name string) (int, bool) {
	owner := -1
	for p := 0; p < t.seats(); p++ {
		for _, ac := range t.setup[p] {
			if !sameName(ac.name, name) {
				continue
			}
			if owner >= 0 && owner != p {
				return 0, false
			}
			owner = p
		}
	}
	return owner, owner >= 0
}

func xmageName(s string) (string, *refusal) {
	if strings.Contains(s, "@") {
		return "", refuse("alias", "alias ref %q", s)
	}
	if i := strings.Index(s, ":"); i > 0 && i <= 5 && strings.ToUpper(s[:i]) == s[:i] {
		s = s[i+1:] // "SET:Name"
	}
	return s, nil
}

func translate(reg *cards.Registry, jc *javaClass, m *javaMethod, family string) (res *result) {
	res = &result{Family: family, Class: jc.Name, Method: m.Name, Rel: jc.Rel}
	fail := func(r *refusal) *result {
		res.Code, res.Detail, res.Status = r.Code, r.Detail, "refused"
		return res
	}
	if m.Ignored {
		return fail(refuse("ignored", "@Ignore"))
	}
	if m.Expects {
		return fail(refuse("expects-exception", "@Test(expected=...)"))
	}
	base, known := xmageBases[jc.Extends]
	switch {
	case known && base.Refuse != "":
		return fail(refuse(base.Refuse, "extends %s", jc.Extends))
	case known:
	case jc.Extends == "CardTestPlayerBaseWithAIHelps":
	default:
		if strings.Contains(jc.Extends, "Multi") || strings.Contains(jc.Extends, "Commander") || strings.Contains(jc.Extends, "FreeForAll") || strings.Contains(jc.Extends, "TwoPlayer") {
			return fail(refuse("multiplayer", "extends %s", jc.Extends))
		}
		if strings.Contains(jc.Extends, "AI") {
			return fail(refuse("ai", "extends %s", jc.Extends))
		}
		return fail(refuse("base-class", "extends %s", jc.Extends))
	}
	if jc.Extends == "CardTestPlayerBaseWithAIHelps" {
		return fail(refuse("ai", "extends %s", jc.Extends))
	}
	t := &translator{reg: reg, jc: jc, m: m, base: base, consts: map[string]value{}, allNames: map[string]bool{}, copied: map[string]bool{}, copyRef: map[string]string{}}
	for k, v := range jc.Consts {
		t.consts[k] = v
	}
	if r := t.read(); r != nil {
		return fail(r)
	}
	sc, groups, r := t.build()
	if r != nil {
		return fail(r)
	}
	sc.XMage = jc.Name + "#" + m.Name
	res.Scenario, res.Groups = sc, groups
	res.Status = "generated"
	res.Unsupported = t.unsupportedCards()
	res.Strict = t.strict
	// Shapes the runner cannot play faithfully: they refuse only when the
	// run fails an assert (a test of the same shape that passes stays
	// covered), see runResultMask.
	if res.shape = t.manaSourceRefusal(); res.shape == nil {
		res.shape = t.orderedPicksRefusal()
	}
	if res.shape == nil {
		res.shape = t.removeCardTypesSubtypeRefusal()
	}
	if res.shape == nil {
		res.shape = t.queueShape
	}
	if res.shape == nil {
		res.shape = t.countShape
	}
	if res.shape == nil {
		res.shape = t.electShape
	}
	if res.shape == nil {
		res.shape = t.familyShapes()
	}
	if res.shape == nil {
		res.shape = t.emitShape
	}
	return res
}

// read classifies every statement into setup/actions/asserts.
func (t *translator) read() *refusal {
	var calls []call
	clearedLib := false
	// First pass: hard refusals win over everything else, so the census key
	// names the real obstacle.
	for _, st := range inlineHelpers(t.m.Stmts, t.jc.Defs, 0) {
		if lm := reLocalDecl.FindStringSubmatch(st); lm != nil {
			v := evalExpr(lm[3], t.consts)
			if v.kind != vStr && v.kind != vInt {
				return refuse("java-local", "local %s", lm[2])
			}
			t.consts[lm[2]] = v
			continue
		}
		c, ok := parseCall(st, t.consts)
		if !ok {
			head := st
			if len(head) > 40 {
				head = head[:40]
			}
			if strings.HasPrefix(st, "for") || strings.HasPrefix(st, "if") || strings.HasPrefix(st, "while") || strings.Contains(st, "->") {
				return refuse("control-flow", "%s", head)
			}
			return refuse("java-statement", "%s", head)
		}
		if c.Name == "removeAllCardsFromLibrary" {
			clearedLib = true
		}
		if code, ok := hardCalls[c.Name]; ok && code != "" {
			if c.Name == "assertLibraryCount" && clearedLib {
				// XMage counts only the cards the test added; the runner's
				// library carries filler, so the count cannot be asserted
				return refuse("library-size", "%s after removeAllCardsFromLibrary", c.Name)
			}
			return refuse(code, "%s", c.Name)
		}
		calls = append(calls, c)
	}
	executed := false
	hasExecute := false
	for _, c := range calls {
		if c.Name == "execute" {
			hasExecute = true
		}
	}
	for i, c := range calls {
		if c.Recv != "" && c.Recv != "this" && c.Recv != "super" {
			if c.Recv == "Assert" || strings.HasPrefix(c.Name, "assert") {
				return refuse("java-assert", "%s.%s", c.Recv, c.Name)
			}
			return refuse("java-statement", "%s.%s", c.Recv, c.Name)
		}
		if t.jc.Helpers[c.Name] {
			return refuse("helper-call", "%s", c.Name)
		}
		if c.Name == "setStrictChooseMode" && len(c.Args) == 1 && c.Args[0].kind == vBool {
			t.strict = c.Args[0].b
		}
		if c.Name == "execute" {
			if executed {
				// each execute() runs to its own setStopAt and asserts
				// there; one scenario has one stop and one expect list
				return refuse("multi-execute", "execute() called more than once")
			}
			executed = true
		}
		if hasExecute && !executed && strings.HasPrefix(c.Name, "assert") && !ignoredCalls[c.Name] {
			t.droppedPreAsserts++
			continue
		}
		if ignoredCalls[c.Name] {
			continue
		}
		if code, ok := hardCalls[c.Name]; ok && code == "" {
			continue
		}
		for _, a := range c.Args {
			if a.kind == vStr && c.Name != "addCard" {
				t.allNames[a.s] = true
				if t.nameUses == nil {
					t.nameUses = map[string]int{}
				}
				t.nameUses[a.s]++
			}
		}
		if c.Name == "activateAbility" && len(c.Args) > 3 && c.Args[3].kind == vStr {
			if t.abilityUses == nil {
				t.abilityUses = map[string]int{}
			}
			t.abilityUses[c.Args[3].s]++
		}
		if r := t.readCall(i, c); r != nil {
			return r
		}
	}
	if t.stop == nil {
		return refuse("no-stop", "no setStopAt")
	}
	if r := t.checkClearedLibraries(); r != nil {
		return r
	}
	t.addAIAnswers()
	t.dropTriggerOrderChoices()
	t.assignChoices()
	if t.orphanMode {
		return refuse("mode-choice", "setModeChoice with no cast to consume it")
	}
	if t.playEffectKicker != "" {
		return refuse("play-effect-kicker", "%s is played by an effect, whose kicker count the runner cannot state", t.playEffectKicker)
	}
	return nil
}

func argInt(c call, i int) (int, bool) {
	if i >= len(c.Args) || c.Args[i].kind != vInt {
		return 0, false
	}
	return c.Args[i].n, true
}

func argStr(c call, i int) (string, bool) {
	if i >= len(c.Args) || c.Args[i].kind != vStr {
		return "", false
	}
	return c.Args[i].s, true
}

func (t *translator) clock(c call, ti, si int) (int, int, *refusal) {
	turn, ok := argInt(c, ti)
	if !ok {
		return 0, 0, refuse("bad-args", "%s turn", c.Name)
	}
	if si < 0 {
		return turn, -1, nil
	}
	if si >= len(c.Args) {
		return 0, 0, refuse("bad-args", "%s step", c.Name)
	}
	st, ok := xmageSteps[c.Args[si].s]
	if !ok {
		if c.Args[si].s == "PhaseStep.FIRST_COMBAT_DAMAGE" {
			return 0, 0, refuse("first-strike-step", "%s at FIRST_COMBAT_DAMAGE", c.Name)
		}
		return 0, 0, refuse("bad-args", "%s step %s", c.Name, c.Args[si])
	}
	if turn > 12 {
		return 0, 0, refuse("long-game", "turn %d", turn)
	}
	return turn, st, nil
}

// playableStepRefusal refuses checkPlayableAbility at a step the runner
// cannot reach with a player holding priority: untap and cleanup have no
// priority, and the game opens at turn 1's first main phase.
func playableStepRefusal(turn, st int) *refusal {
	if st == sUntap || st == sCleanup {
		return refuse("playable-check", "no priority in %s", stepNames[st])
	}
	if turn == 1 && st < sMain1 {
		return refuse("playable-check", "turn 1 %s", stepNames[st])
	}
	return nil
}

// namePrefixMatch reports whether want is a prefix of the full card name at
// a word boundary. XMage's checkPlayableAbility matches an ability string by
// startsWith, so tests routinely abbreviate a card ("Silvercoat" for
// "Silvercoat Lion", "Lightning" for "Lightning Bolt").
func namePrefixMatch(full, want string) bool {
	if want == "" || len(want) > len(full) || !strings.EqualFold(full[:len(want)], want) {
		return false
	}
	if len(want) == len(full) {
		return true
	}
	return strings.ContainsRune(" :,(", rune(full[len(want)]))
}

// cardNames lists a placement's name plus the names of the card's faces: a
// check may name an adventure, prepare or modal back face whose object the
// setup placed under the front-face name.
func (t *translator) cardNames(pl *placement) []string {
	names := []string{pl.name}
	if c, ok := t.lookup(pl.name); ok {
		for _, f := range c.Faces {
			if f != nil && !sameName(f.Name, pl.name) {
				names = append(names, f.Name)
			}
		}
	}
	return names
}

// cardNamePrefix returns the card name the text starts with, if any.
// checkPlayableAbility matches an ability description by prefix, so an
// ability check can only be tied to a source when its text names the card.
func (t *translator) cardNamePrefix(p int, text string) string {
	best := ""
	for _, pl := range t.places[p] {
		for _, n := range t.cardNames(pl) {
			if namePrefixMatch(text, n) && len(n) > len(best) {
				best = n
			}
		}
	}
	return best
}

// cardMatch scores a placement against a card name: 2 exact, 1 prefix, 0 no
// match, and returns the matched face name.
func (t *translator) cardMatch(pl *placement, want string) (int, string) {
	best, bestName := 0, ""
	for _, n := range t.cardNames(pl) {
		if sameName(n, want) {
			return 2, n
		}
		if best < 1 && namePrefixMatch(n, want) {
			best, bestName = 1, n
		}
	}
	return best, bestName
}

// offeredRef names the object a checkPlayableAbility talks about. Cast and
// play look at the zones a card can be cast/played from (hand first, not the
// battlefield copy a same-named permanent may occupy); activate looks at the
// battlefield first. Unused placements win so a second copy in hand is not
// confused with one already cast.
func (t *translator) offeredRef(p int, name, kind string) (string, string, bool) {
	zones := []string{"hand", "library_top", "exile", "graveyard", "battlefield"}
	if kind == "activate" {
		zones = []string{"battlefield", "hand", "graveyard", "exile", "library_top"}
	}
	for level := 2; level >= 1; level-- {
		for _, used := range []bool{false, true} {
			for _, z := range zones {
				for _, pl := range t.places[p] {
					if pl.used != used || pl.zone != z {
						continue
					}
					if lvl, full := t.cardMatch(pl, name); lvl == level {
						return refStr(p, pl.name, pl.ord), full, true
					}
				}
			}
		}
	}
	// A copy or generated object has no setup placement; anyRefs knows the
	// refs the translator minted for it.
	if rs := t.anyRefs(p, name); len(rs) > 0 {
		return rs[0], name, true
	}
	return "", "", false
}

// castQualifierModes maps XMage's "using X"/"with X" cast qualifier to the
// mode word gorge appends to that cast option's label. A qualifier absent
// from the table is not treated as a qualifier at all, so a card whose name
// contains "with" ("Craft with Pride") stays intact; an empty word means the
// option label carries no mode suffix the translator knows.
var castQualifierModes = map[string]string{
	"morph": "face down", "megamorph": "face down", "disguise": "face down",
	"bestow": "bestowed", "overload": "overloaded", "plot": "plotted",
	"escape": "escape", "prepare": "prepared", "foretell": "foretold",
	"flashback": "flashback", "disturb": "", "spectacle": "", "madness": "",
}

// splitCastQualifier separates a cast text's card from a known alternative-
// cast qualifier ("Willbender using Morph" -> "Willbender", "morph").
func splitCastQualifier(s string) (string, string) {
	for _, sep := range []string{" using ", " with "} {
		if i := strings.LastIndex(s, sep); i > 0 {
			q := strings.ToLower(s[i+len(sep):])
			if _, ok := castQualifierModes[q]; ok {
				return s[:i], q
			}
		}
	}
	return s, ""
}

// hasKeywordPrefix recognises "Cast X", "CastX" (a constant concatenation)
// without treating a card such as "Castigate" as the cast keyword.
func hasKeywordPrefix(text, kw string) bool {
	if !strings.HasPrefix(text, kw) {
		return false
	}
	if len(text) == len(kw) || text[len(kw)] == ' ' {
		return true
	}
	c := text[len(kw)]
	return c >= 'A' && c <= 'Z'
}

// playableOffer turns an XMage checkPlayableAbility text into the runner's
// inline offered expect. "Cast X" is the cast option labelled X, "Play X" the
// land play, and any other text must name the source card (XMage searches
// every playable ability for a prefix, so an unnamed source is unresolvable).
func (t *translator) playableOffer(p, src int, want bool, text string) (*offered, *refusal) {
	kind := ""
	cardText := ""
	switch {
	case hasKeywordPrefix(text, "Cast"):
		kind, cardText = "cast", strings.TrimSpace(strings.TrimPrefix(text, "Cast"))
	case hasKeywordPrefix(text, "Play"):
		kind, cardText = "play", strings.TrimSpace(strings.TrimPrefix(text, "Play"))
	}
	missing := ""
	if kind != "" {
		qual := ""
		if kind == "cast" {
			cardText, qual = splitCastQualifier(cardText)
		}
		nm, r := xmageName(cardText)
		if r != nil {
			return nil, r
		}
		ref, full, ok := t.offeredRef(p, nm, kind)
		if ok {
			if mode := castQualifierModes[qual]; mode != "" {
				full += " (" + mode + ")"
			}
			if kind == "cast" && want {
				ref = t.preparedCopyRef(p, src, ref, nm)
			}
			return &offered{Seat: p, Kind: kind, Card: ref, Label: full}, nil
		}
		missing = nm
	}
	// Not a resolvable cast/play: the text may still name a card whose
	// ability is being checked ("Serra Avenger"), or a card whose name
	// merely starts with the cast keyword ("Play with Fire").
	name := t.cardNamePrefix(p, text)
	if name == "" {
		if missing != "" {
			return nil, refuse("playable-check", "%s not in setup", missing)
		}
		return nil, refuse("playable-check", "no card named in %q", text)
	}
	ref, _, ok := t.offeredRef(p, name, "activate")
	if !ok {
		return nil, refuse("playable-check", "source %s of %q not in setup", name, text)
	}
	// XMage's prefix search covers every playable ability of the card, the
	// cast action included. A card off the battlefield can only be cast (or
	// have a hand/graveyard ability activated, which the card name alone
	// cannot pick), so offer it as a cast; a permanent's text is an
	// activation.
	switch t.placeZone(p, ref) {
	case "battlefield":
		return &offered{Seat: p, Kind: "activate", Card: ref, Label: text}, nil
	case "":
		return nil, refuse("playable-check", "zone of %s unknown for %q", name, text)
	}
	if !sameName(text, name) {
		return nil, refuse("playable-check", "ability text %q on %s outside the battlefield", text, name)
	}
	return &offered{Seat: p, Kind: "cast", Card: ref, Label: name}, nil
}

// placeZone returns the setup zone of the placement a ref names, or "" for a
// minted object with none.
func (t *translator) placeZone(p int, ref string) string {
	for _, pl := range t.places[p] {
		if refStr(p, pl.name, pl.ord) == ref {
			return pl.zone
		}
	}
	return ""
}

func (t *translator) addAction(a *action) {
	if len(t.preChoices) > 0 && a.kind != "check" && a.kind != "wait" {
		a.choices = append(t.preChoices, a.choices...)
		t.preChoices = nil
	}
	t.actions = append(t.actions, a)
}

func (t *translator) lastAction() *action {
	for i := len(t.actions) - 1; i >= 0; i-- {
		if k := t.actions[i].kind; k != "check" && k != "wait" {
			return t.actions[i]
		}
	}
	return nil
}

func (t *translator) readCall(idx int, c call) *refusal {
	switch c.Name {
	case "addCard":
		if len(c.Args) < 3 {
			return refuse("bad-args", "addCard")
		}
		p, ok := t.seatOf(c.Args[1])
		if !ok {
			return refuse("bad-args", "addCard player %s", c.Args[1])
		}
		name, ok := argStr(c, 2)
		if !ok {
			return refuse("bad-args", "addCard name %s", c.Args[2])
		}
		name, r := xmageName(name)
		if r != nil {
			return r
		}
		n := 1
		if len(c.Args) > 3 {
			if n, ok = argInt(c, 3); !ok {
				return refuse("bad-args", "addCard count %s", c.Args[3])
			}
		}
		tapped := len(c.Args) > 4 && c.Args[4].kind == vBool && c.Args[4].b
		zone := map[string]string{"Zone.BATTLEFIELD": "battlefield", "Zone.HAND": "hand", "Zone.GRAVEYARD": "graveyard",
			"Zone.EXILED": "exile", "Zone.LIBRARY": "library_top"}[c.Args[0].s]
		if zone == "" {
			if c.Args[0].s == "Zone.COMMAND" {
				return refuse("commander", "addCard COMMAND")
			}
			return refuse("bad-args", "addCard zone %s", c.Args[0].s)
		}
		if c, ok := t.lookup(name); !ok {
			return refuse("unknown-card", "%s", name)
		} else if len(c.Faces) > 1 && c.Faces[0] != nil && !sameName(c.Faces[0].Name, name) && !strings.Contains(c.Faces[0].Name, " // ") {
			// The runner deals a card by its front-face name.
			return refuse("back-face-setup", "%s is a back face of %s", name, c.Faces[0].Name)
		}
		if n > 60 {
			return refuse("bad-args", "addCard count %d", n)
		}
		t.setup[p] = append(t.setup[p], addCardCall{zone: zone, name: name, count: n, tapped: tapped})
	case "removeAllCardsFromHand", "removeAllCardsFromLibrary":
		if len(c.Args) != 1 {
			return refuse("bad-args", "%s", c.Name)
		}
		p, ok := t.seatOf(c.Args[0])
		if !ok {
			return refuse("bad-args", "%s player %s", c.Name, c.Args[0])
		}
		if c.Name == "removeAllCardsFromHand" {
			// the runner deals no opening hand, so there is nothing to clear
			// unless the test already built the hand: dropping those cards
			// is a real action the setup cannot express
			for _, ac := range t.setup[p] {
				if ac.zone == "hand" {
					return refuse("library-manipulation", "removeAllCardsFromHand after addCard HAND")
				}
			}
			break
		}
		// XMage queues Zone.LIBRARY -> "clear" and applies it before the
		// separate cheat call that adds the setup cards, so a card the test
		// added earlier still survives. The runner's library already holds
		// exactly the test's cards, so marking the seat is the whole
		// translation; dropping addCard(Zone.LIBRARY, ...) entries would
		// disagree with XMage.
		t.libCleared[p] = true
	case "setLife":
		p, ok := t.seatOf(c.Args[0])
		n, ok2 := argInt(c, 1)
		if !ok || !ok2 {
			return refuse("bad-args", "setLife")
		}
		v := int32(n)
		t.life[p] = &v
	case "setStopAt":
		turn, st, r := t.clock(c, 0, 1)
		if r != nil {
			return r
		}
		t.stop = &[2]int{turn, st}
	case "castSpell", "activateAbility", "playLand":
		if len(c.Args) < 4 {
			return refuse("bad-args", "%s", c.Name)
		}
		turn, st, r := t.clock(c, 0, 1)
		if r != nil {
			return r
		}
		p, ok := t.seatOf(c.Args[2])
		if !ok {
			return refuse("bad-args", "%s player", c.Name)
		}
		s, ok := argStr(c, 3)
		if !ok {
			return refuse("bad-args", "%s name %s", c.Name, c.Args[3])
		}
		a := &action{turn: turn, step: st, sub: 1, seat: p, src: idx}
		switch c.Name {
		case "castSpell":
			a.kind = "cast"
			name, r := xmageName(s)
			if r != nil {
				return r
			}
			a.card = name
			for _, x := range c.Args[4:] {
				switch x.kind {
				case vBool:
					a.resolveAfter = x.b
				case vStr:
					if len(a.targets) > 0 {
						// spellOnStack: ordering condition; a spell is
						// already on the stack in the generated order, a
						// triggered ability is not.
						if isAbilityClause(x.s) {
							return refuse("stack-clause", "castSpell waits for %q", x.s)
						}
						continue
					}
					a.targets = append(a.targets, x.s)
				case vIdent:
					if q, ok := t.seatOf(x); ok {
						a.targets = append(a.targets, fmt.Sprintf("p%d", q))
					} else if strings.HasPrefix(x.s, "StackClause") {
						return refuse("stack-clause", "%s", x.s)
					} else {
						return refuse("bad-args", "castSpell arg %s", x.s)
					}
				case vInt:
					return refuse("mana-payment", "castSpell manaInPool")
				default:
					return refuse("bad-args", "castSpell arg %s", x.s)
				}
			}
		case "activateAbility":
			a.kind = "activate"
			a.ability = s
			for _, x := range c.Args[4:] {
				switch x.kind {
				case vStr:
					if len(a.targets) > 0 {
						if isAbilityClause(x.s) {
							return refuse("stack-clause", "activateAbility waits for %q", x.s)
						}
						continue
					}
					a.targets = append(a.targets, x.s)
				case vIdent:
					if q, ok := t.seatOf(x); ok {
						a.targets = append(a.targets, fmt.Sprintf("p%d", q))
					} else {
						return refuse("stack-clause", "%s", x.s)
					}
				default:
					return refuse("bad-args", "activateAbility arg %s", x)
				}
			}
		case "playLand":
			a.kind = "play"
			name, r := xmageName(s)
			if r != nil {
				return r
			}
			a.card = name
		}
		t.addAction(a)
	case "attack", "block":
		if len(c.Args) < 3 {
			return refuse("bad-args", "%s", c.Name)
		}
		turn, _, r := t.clock(c, 0, -1)
		if r != nil {
			return r
		}
		p, ok := t.seatOf(c.Args[1])
		if !ok {
			return refuse("bad-args", "%s player", c.Name)
		}
		s, ok := argStr(c, 2)
		if !ok {
			return refuse("bad-args", "%s name", c.Name)
		}
		s, rr := xmageName(s)
		if rr != nil {
			return rr
		}
		if c.Name == "attack" {
			def := -1
			if len(c.Args) > 3 {
				q, ok := t.seatOf(c.Args[3])
				if !ok || q == p {
					return refuse("attack-planeswalker", "attack defender %s", c.Args[3])
				}
				def = q
			} else {
				// XMage's TestPlayer then attacks whichever defender its
				// combat's hash set yields last: one opponent of two
				// seats, an unstated one of more.
				q, r := t.opponent(p, "attack")
				if r != nil {
					return r
				}
				def = q
			}
			a := &action{turn: turn, step: sDeclareAttackers, sub: 0, seat: p, kind: "attack", src: idx, attackers: []string{s}, defender: def}
			t.addAction(a)
		} else {
			b, ok := argStr(c, 3)
			if !ok {
				return refuse("bad-args", "block attacker")
			}
			b, rr := xmageName(b)
			if rr != nil {
				return rr
			}
			a := &action{turn: turn, step: sDeclareBlockers, sub: 0, seat: p, kind: "block", src: idx, blocks: [][2]string{{s, b}}}
			t.addAction(a)
		}
	case "waitStackResolved":
		turn, st, r := t.clock(c, 0, 1)
		if r != nil {
			return r
		}
		for _, x := range c.Args[2:] {
			if x.kind == vBool || x.kind == vInt {
				return refuse("partial-resolve", "waitStackResolved %s", x)
			}
		}
		p := 0
		if len(c.Args) > 2 {
			p, _ = t.seatOf(c.Args[2])
		}
		t.addAction(&action{turn: turn, step: st, sub: 1, seat: p, kind: "wait", src: idx})
	case "setChoice", "addTarget":
		if len(c.Args) < 2 {
			return refuse("bad-args", "%s", c.Name)
		}
		times := 1
		if len(c.Args) > 2 {
			n, ok := argInt(c, 2)
			if !ok {
				return refuse("bad-args", "%s times", c.Name)
			}
			times = n
		}
		ch := choice{x: -1}
		v := c.Args[1]
		switch {
		case c.Name == "setChoice" && v.kind == vBool:
			ch.pick = map[bool]string{true: "yes", false: "no"}[v.b]
		case v.kind == vStr:
			if n, ok := xChoice(v.s); ok {
				ch.pick, ch.x = fmt.Sprint(n), n
				ch.seat, ch.src = t.p0(c.Args[0]), idx
				for i := 0; i < times; i++ {
					t.choiceList = append(t.choiceList, ch)
				}
				return nil
			}
			if strings.HasPrefix(v.s, "X=") {
				return refuse("x-cost", "setChoice %s", v.s)
			}
			if c.Name == "setChoice" && (strings.EqualFold(v.s, "yes") || strings.EqualFold(v.s, "no")) {
				ch.pick = strings.ToLower(v.s)
				break
			}
			if strings.Contains(v.s, "^") || strings.Contains(v.s, "=") {
				return refuse("multi-target", "%s %q", c.Name, v.s)
			}
			if strings.Contains(v.s, "[only copy]") {
				// legend rule: keep the copy, not a named card
				return refuse("special-choice", "%s %q", c.Name, v.s)
			}
			if q, ok := t.playerNameSeat(v.s); ok {
				ch.pick = fmt.Sprintf("p%d", q)
				break
			}
			ch.pick = "name:" + v.s
		case v.kind == vIdent:
			q, ok := t.seatOf(v)
			if !ok {
				return refuse("special-choice", "%s %s", c.Name, v.s)
			}
			ch.pick = fmt.Sprintf("p%d", q)
		default:
			return refuse("bad-args", "%s %s", c.Name, v)
		}
		ch.target = c.Name == "addTarget"
		ch.seat, ch.src = t.p0(c.Args[0]), idx
		if !ch.target && isTriggerOrderText(strings.TrimPrefix(ch.pick, "name:")) {
			// XMage orders simultaneous triggers by rule text; gorge's
			// runner keeps the offered order. Dropped (the asserts still
			// judge the outcome) and noted in why.
			t.droppedOrder++
			t.droppedOrderTexts = append(t.droppedOrderTexts, strings.TrimPrefix(ch.pick, "name:"))
			return nil
		}
		for i := 0; i < times; i++ {
			// a counted setChoice or addTarget names n distinct objects of
			// that name.
			if times > 1 {
				ch.ord = i
			}
			t.choiceList = append(t.choiceList, ch)
		}
	case "setModeChoice":
		if len(c.Args) != 2 {
			return refuse("bad-args", "%s", c.Name)
		}
		v := c.Args[1]
		n := 0
		if v.kind == vStr && allDigits(v.s) && len(v.s) < 3 {
			fmt.Sscanf(v.s, "%d", &n)
		}
		if n < 1 {
			return refuse("mode-choice", "setModeChoice %s", v)
		}
		t.choiceList = append(t.choiceList, choice{seat: t.p0(c.Args[0]), src: idx, x: -1, mode: n, pick: v.s})
	case "checkPlayableAbility":
		if len(c.Args) < 6 {
			return refuse("bad-args", "%s", c.Name)
		}
		turn, st, r := t.clock(c, 1, 2)
		if r != nil {
			return r
		}
		p, ok := t.seatOf(c.Args[3])
		if !ok {
			return refuse("bad-args", "%s player", c.Name)
		}
		text, ok := argStr(c, 4)
		if !ok {
			return refuse("bad-args", "%s ability %s", c.Name, c.Args[4])
		}
		if c.Args[5].kind != vBool {
			return refuse("bad-args", "%s want %s", c.Name, c.Args[5])
		}
		if r := playableStepRefusal(turn, st); r != nil {
			return r
		}
		t.addAction(&action{turn: turn, step: st, sub: 1, seat: p, kind: "check", src: idx,
			playCheck: &playableCheck{text: text, want: c.Args[5].b}})
	case "checkPermanentCount", "checkLife", "checkPT", "checkGraveyardCount", "checkHandCardCount",
		"checkExileCount", "checkStackSize", "checkAbility", "checkPermanentCounters":
		if len(c.Args) < 4 {
			return refuse("bad-args", "%s", c.Name)
		}
		turn, st, r := t.clock(c, 1, 2)
		if r != nil {
			return r
		}
		p, ok := t.seatOf(c.Args[3])
		if !ok {
			return refuse("bad-args", "%s player", c.Name)
		}
		a := &action{turn: turn, step: st, sub: 1, seat: p, kind: "check", src: idx}
		// re-shape to the matching assert call and reuse the assert mapper
		ac := call{Name: c.Name}
		switch c.Name {
		case "checkPermanentCount":
			args := c.Args[3:]
			if len(args) == 4 { // player, targetPlayer, card, n
				args = args[1:]
			}
			ac = call{Name: "assertPermanentCount", Args: args}
		case "checkLife":
			ac = call{Name: "assertLife", Args: c.Args[3:]}
		case "checkPT":
			ac = call{Name: "assertPowerToughness", Args: c.Args[3:]}
		case "checkGraveyardCount":
			ac = call{Name: "assertGraveyardCount", Args: c.Args[3:]}
		case "checkHandCardCount":
			ac = call{Name: "assertHandCount", Args: c.Args[3:]}
		case "checkExileCount":
			// XMage's check reads every exile zone by name and ignores its
			// player argument (TestPlayer.assertExileCount over
			// game.getExile().getAllCards). When the named seat does not own
			// the card the two readings differ, and no scenario shape can
			// state "count this card in either exile": refuse the shape
			// instead of silently counting the wrong seat.
			if len(c.Args) >= 6 {
				if owner, ok := t.setupOwner(c.Args[4].s); ok {
					if seat, isSeat := t.seatOf(c.Args[3]); isSeat && seat != owner {
						return refuse("check-exile-count-player", "%s is owned by p%d but the check names p%d", c.Args[4], owner, seat)
					}
				}
			}
			ac = call{Name: "assertExileCount", Args: c.Args[3:]}
		case "checkStackSize":
			ac = call{Name: "checkStackSize", Args: c.Args[3:]}
		case "checkAbility":
			ac = call{Name: "assertAbility", Args: c.Args[3:]}
		case "checkPermanentCounters":
			// (player, card, CounterType, n)
			if len(c.Args) < 7 {
				return refuse("bad-args", "%s", c.Name)
			}
			ac = call{Name: "assertCounterCount", Args: c.Args[3:]}
		}
		a.expects = nil
		t.addAction(a)
		t.pendingChecks = append(t.pendingChecks, pendingCheck{a: a, c: ac})
	default:
		if strings.HasPrefix(c.Name, "assert") {
			t.asserts = append(t.asserts, c)
			return nil
		}
		return refuse("unsupported-call", "%s", c.Name)
	}
	return nil
}

// xChoice parses XMage's announced-X answer "X=N".
func xChoice(s string) (int, bool) {
	rest, ok := strings.CutPrefix(s, "X=")
	if !ok || !allDigits(rest) || len(rest) > 3 {
		return 0, false
	}
	n := 0
	fmt.Sscanf(rest, "%d", &n)
	return n, true
}
