package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
)

// refusal is a method the translator will not port, with a stable reason
// code (the census key) and a free-text detail.
type refusal struct {
	Code   string
	Detail string
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
	"checkPlayableAbility":          "playable-check",
	"rollbackTurns":                 "rollback",
	"rollbackAfterActionsStart":     "rollback",
	"rollbackAfterActionsEnd":       "rollback",
	"removeAllCardsFromLibrary":     "library-manipulation",
	"removeAllCardsFromHand":        "library-manipulation",
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
	attackers       []string
	blocks          [][2]string
	expects         []*expect
	choices         []choice
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
	reg     *cards.Registry
	jc      *javaClass
	m       *javaMethod
	consts  map[string]value
	setup   [2][]addCardCall
	life    [2]*int32
	actions []*action
	stop    *[2]int
	asserts []call
	// derived
	places        [2][]*placement
	castRefs      [2]map[string][]string
	allNames      map[string]bool // every string arg outside addCard (keeps lands)
	needAll       bool            // an assert counts all permanents: keep lands
	pendingChecks []pendingCheck
	preChoices    []choice
	choiceList    []choice
	droppedOrder  int
	orphanMode    bool // a setModeChoice no action consumes (a trigger's modes)
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
}

func p0(v value) int {
	p, _ := seatOf(v)
	return p
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
			to.choices = append(to.choices, ch)
		case ch.mode > 0:
			t.orphanMode = true
		default:
			t.tailChoices = append(t.tailChoices, ch)
		}
	}
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
	Family, Class, Method, Rel string
	Code, Detail               string // refusal (empty when generated)
	Scenario                   *portScenario
	Groups                     []*group
	Fails                      []string
	Status                     string // refused | pass | fail-harness | fail-expect
	Dropped                    int
	Unsupported                string         // "Card[prim,...]; ..." for cards gorge's coverage gate does not support
	Strict                     bool           // setStrictChooseMode(true)
	XAnswers                   map[int][]xans // final step index -> original XMage choices
}

var reLocalDecl = regexp.MustCompile(`^(?:final\s+)?(String|int)\s+(\w+)\s*=\s*(.+)$`)

func seatOf(v value) (int, bool) {
	if v.kind != vIdent {
		return 0, false
	}
	switch v.s {
	case "playerA":
		return 0, true
	case "playerB":
		return 1, true
	}
	return 0, false
}

var supported = effects.Supported()

func (t *translator) unsupportedCards() string {
	var out []string
	seen := map[string]bool{}
	for p := 0; p < 2; p++ {
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
	switch jc.Extends {
	case "CardTestPlayerBase", "CardTestPlayerBaseWithAIHelps", "":
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
	t := &translator{reg: reg, jc: jc, m: m, consts: map[string]value{}, allNames: map[string]bool{}, copied: map[string]bool{}, copyRef: map[string]string{}}
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
	return res
}

// read classifies every statement into setup/actions/asserts.
func (t *translator) read() *refusal {
	var calls []call
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
		if code, ok := hardCalls[c.Name]; ok && code != "" {
			return refuse(code, "%s", c.Name)
		}
		calls = append(calls, c)
	}
	executed := false
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
		if ignoredCalls[c.Name] {
			continue
		}
		if code, ok := hardCalls[c.Name]; ok && code == "" {
			continue
		}
		for _, a := range c.Args {
			if a.kind == vStr && c.Name != "addCard" {
				t.allNames[a.s] = true
			}
		}
		if r := t.readCall(i, c); r != nil {
			return r
		}
	}
	if t.stop == nil {
		return refuse("no-stop", "no setStopAt")
	}
	t.assignChoices()
	if t.orphanMode {
		return refuse("mode-choice", "setModeChoice with no cast to consume it")
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
		p, ok := seatOf(c.Args[1])
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
	case "setLife":
		p, ok := seatOf(c.Args[0])
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
		p, ok := seatOf(c.Args[2])
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
					if q, ok := seatOf(x); ok {
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
					if q, ok := seatOf(x); ok {
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
		p, ok := seatOf(c.Args[1])
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
			if len(c.Args) > 3 {
				if q, ok := seatOf(c.Args[3]); !ok || q == p {
					return refuse("attack-planeswalker", "attack defender %s", c.Args[3])
				}
			}
			a := &action{turn: turn, step: sDeclareAttackers, sub: 0, seat: p, kind: "attack", src: idx, attackers: []string{s}}
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
			p, _ = seatOf(c.Args[2])
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
				ch.seat, ch.src = p0(c.Args[0]), idx
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
			ch.pick = "name:" + v.s
		case v.kind == vIdent:
			q, ok := seatOf(v)
			if !ok {
				return refuse("special-choice", "%s %s", c.Name, v.s)
			}
			ch.pick = fmt.Sprintf("p%d", q)
		default:
			return refuse("bad-args", "%s %s", c.Name, v)
		}
		ch.target = c.Name == "addTarget"
		ch.seat, ch.src = p0(c.Args[0]), idx
		if !ch.target && isTriggerOrderText(strings.TrimPrefix(ch.pick, "name:")) {
			// XMage orders simultaneous triggers by rule text; gorge's
			// runner keeps the offered order. Dropped (the asserts still
			// judge the outcome) and noted in why.
			t.droppedOrder++
			return nil
		}
		for i := 0; i < times; i++ {
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
		t.choiceList = append(t.choiceList, choice{seat: p0(c.Args[0]), src: idx, x: -1, mode: n, pick: v.s})
	case "checkPermanentCount", "checkLife", "checkPT", "checkGraveyardCount", "checkHandCardCount",
		"checkExileCount", "checkStackSize", "checkAbility", "checkPermanentCounters":
		if len(c.Args) < 4 {
			return refuse("bad-args", "%s", c.Name)
		}
		turn, st, r := t.clock(c, 1, 2)
		if r != nil {
			return r
		}
		p, ok := seatOf(c.Args[3])
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
