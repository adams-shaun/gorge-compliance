package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// A step that also scripts a lone cost_posed decline must answer its
// trigger-cost pay chooseUse yes, AHEAD of that decline's "no": XMage asks
// every optional triggered cost in resolution order, so Smuggler's Copter's
// may-draw chooseUse comes before Balthier and Fran's {1}{R}{G} one, and with
// the pay skipped the "no" shifted onto the Copter and XMage never looted what
// gorge looted (verdict row Balthier and Fran/trigger#0.0/v1, step 3
// p0.graveyard gorge "[Wastes]" against xmage "[]").
func TestTriggerCostPayScriptsYesBeforeDecline(t *testing.T) {
	pay := rules.OracleDecision{Step: 3, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Draw 1 card"},
		PickRefs: []string{"p0:Smuggler's Copter"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Smuggler's Copter"}, Via: "answer"}
	decline := rules.OracleDecision{Step: 3, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Balthier and Fran"}, PickKinds: []string{"trigger_cost_decline"},
		CostPosed: true, ObjectPicks: []string{"p0:Balthier and Fran"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{pay, decline}, 4, nil, nil)
	want := [][]XAnswer{nil, nil, nil, {{0, "choice", "yes"}, {0, "choice", "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// A blight trigger-cost pay with NO cost_posed decline on its step is still
// XMage's DoIfCostPaid chooseUse: BlightCost.canPay holds (gorge offered the
// pay side), DoIfCostPaid asks, and only then does BlightCost.doBlight ask for
// the creature. Scripting the yes leads that creature pick instead of letting
// the label shift onto the next ask (Boggart Mischief, Dream Seizer).
func TestBlightTriggerCostPayAloneScriptsYes(t *testing.T) {
	pay := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Blight 1"},
		PickRefs: []string{"p0:Scuzzback Scrounger"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Scuzzback Scrounger"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{pay}, 4, nil, nil)
	want := [][]XAnswer{nil, nil, {{0, "choice", "yes"}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// A cost_posed decline on a DIFFERENT step does not suppress this step's
// blight pay: Grub's decline (step 0) and blight pay (step 1) are separate
// asks; the pay has its own chooseUse and scripts yes.
func TestBlightTriggerCostPayOtherStepThanDeclineScriptsYes(t *testing.T) {
	pay := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Blight 2"},
		PickRefs: []string{"p0:Grub, the Fat"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Grub, the Fat"}, Via: "answer"}
	decline := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Grub, the Fat"}, PickKinds: []string{"trigger_cost_decline"},
		CostPosed: true, ObjectPicks: []string{"p0:Grub, the Fat"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{decline, pay}, 4, nil, nil)
	want := [][]XAnswer{{{0, "choice", "no"}}, {{0, "choice", "yes"}}, nil, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// The pay side of a MANA triggered cost keeps its pool-payment skip (the
// pre-existing payment() path): a "Pay {G}" label is paid from the pool, so
// even a step that also scripts a decline never answers it.
func TestManaTriggerCostPayStaysSkipped(t *testing.T) {
	pay := rules.OracleDecision{Step: 3, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Pay {G}"},
		PickRefs: []string{"p0:Trystan, Callous Cultivator"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Trystan, Callous Cultivator"}, Via: "answer"}
	decline := rules.OracleDecision{Step: 3, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Trystan, Callous Cultivator"}, PickKinds: []string{"trigger_cost_decline"},
		CostPosed: true, ObjectPicks: []string{"p0:Trystan, Callous Cultivator"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{pay, decline}, 4, nil, nil)
	want := [][]XAnswer{nil, nil, nil, {{0, "choice", "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// An X-only triggered cost ("you may pay {X}", Wildborn Preserver) is XMage's
// DoIfCostPaid chooseUse like any mana cost, but gorge's TriggerCostMana is
// false for it (measured FDN xe:FDN:0:3:w24: "Missing CHOICE def ... Pay
// {X}?"). XMage asks the boolean BEFORE announceX, so the queue is "yes" then
// "X=<n>"; a decline returns before announceX, so it scripts the lone "no".
// A non-X, non-mana pay (a blight) still scripts nothing.
func TestXAnswersXOnlyTriggerCostPay(t *testing.T) {
	x := rules.OracleDecision{Step: 14, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 3, Min: 1, Max: 1, Picks: []string{"X = 0"}, PickKinds: []string{"trigger_cost_x"},
		PickRefs: []string{"X = 0"}, Via: "answer"}
	for _, label := range []string{"Pay X", "Pay {X}"} {
		pay := rules.OracleDecision{Step: 14, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 2, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{0},
			PickRefs: []string{"p0:Wildborn Preserver"}, PickKinds: []string{"trigger_cost_pay"},
			ObjectPicks: []string{"p0:Wildborn Preserver"}, Via: "answer"}
		got := XAnswers([]rules.OracleDecision{x, pay}, 15, nil)
		if want := []XAnswer{{0, "choice", "yes"}, {0, "choice", "X=0"}}; !reflect.DeepEqual(got[14], want) {
			t.Fatalf("%q answers = %v, want exactly %v", label, got[14], want)
		}
		if !xOnlyCostPay(pay) || triggerCostAnswer(pay) != "yes" {
			t.Fatalf("%q not recognised as an X-only trigger cost pay", label)
		}
	}
	decline := rules.OracleDecision{Step: 14, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{1},
		PickRefs: []string{"p0:Wildborn Preserver"}, PickKinds: []string{"trigger_cost_decline"},
		ObjectPicks: []string{"p0:Wildborn Preserver"}, Via: "answer"}
	got := XAnswers([]rules.OracleDecision{x, decline}, 15, nil)
	if want := []XAnswer{{0, "choice", "no"}}; !reflect.DeepEqual(got[14], want) {
		t.Fatalf("declined X-only answers = %v, want exactly %v (no announceX ask)", got[14], want)
	}
	for _, label := range []string{"Blight 1", "Pay {2}X", "Pay ", "Pay 2 life"} {
		d := rules.OracleDecision{Kind: "choose_n", Picks: []string{label}, PickKinds: []string{"trigger_cost_pay"}}
		if xOnlyCostPay(d) {
			t.Fatalf("%q misread as an X-only cost", label)
		}
	}
}

// A trigger-cost sacrifice the engine folded into its pay election still
// needs the sacrificed creature's name on XMage's CHOICE queue where XMage
// poses the sacrifice as an optional pick of its own. The Gitrog, Ravenous
// Ride's "you may sacrifice a creature that saddled it this turn" has one
// legal candidate (Memnite), so gorge logs only the pay pick and the
// checkpoint's battlefield->graveyard move; XMage's card-specific effect
// folds the "you may" into a TargetSacrifice(0, 1), whose makeChoose reads
// the same CHOICE queue. Without the name the effect's own hand pick (the
// land) is consumed by the sacrifice ask: "Found wrong choice command
// (invalid target or miss skip command): Swamp" (xpt-77cb30f4/xpt-cfdb102c).
// The alias must lead the land answer, as the forage branch's graveyard names
// do.
func TestTriggerCostSacrificeLonePickScriptsAlias(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pay := func(src, label string) rules.OracleDecision {
		return rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 2, Min: 1, Max: 1, Picks: []string{label},
			PickRefs: []string{"p0:" + src}, PickKinds: []string{"trigger_cost_pay"},
			ObjectPicks: []string{"p0:" + src}, Via: "answer"}
	}
	land := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Min: 0, Max: 1, Picks: []string{"Swamp"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Swamp"}, PickKinds: []string{"hand_move"}, Resume: "hand_move",
		ObjectPicks: []string{"p0:Swamp"}, Via: "answer"}
	perm := func(name string, token bool) rules.OracleSnapPerm {
		return rules.OracleSnapPerm{Name: name, Controller: 0, Token: token}
	}
	// snaps is the before/after checkpoint pair: the battlefield loses every
	// permanent named in left and the payer's graveyard gains graveyard.
	snaps := func(before []rules.OracleSnapPerm, left []string, graveyard ...string) []rules.OracleSnapshot {
		gone := map[string]bool{}
		for _, n := range left {
			gone[n] = true
		}
		var after []rules.OracleSnapPerm
		for _, p := range before {
			if !gone[p.Name] {
				after = append(after, p)
			}
		}
		return []rules.OracleSnapshot{
			{},
			{Players: []rules.OracleSnapPlayer{{Seat: 0}, {Seat: 1}}, Permanents: before},
			{Players: []rules.OracleSnapPlayer{{Seat: 0, Graveyard: graveyard}, {Seat: 1}}, Permanents: after},
		}
	}
	run := func(d rules.OracleDecision, sn []rules.OracleSnapshot) [][]XAnswer {
		ds := []rules.OracleDecision{d, land}
		extra := xanswersExtra{snaps: sn, loneSacrifice: loneTriggerCostSacrifice(reg, ds, sn)}
		return xanswersSetup(ds, 2, nil, nil, nil, nil, extra)
	}
	gitrog := pay("The Gitrog, Ravenous Ride", "Sacrifice creature that saddled it this turn")
	one := snaps([]rules.OracleSnapPerm{perm("The Gitrog, Ravenous Ride", false), perm("Memnite", false)}, []string{"Memnite"}, "Memnite")

	got := run(gitrog, one)
	want := [][]XAnswer{nil, {{0, "choice", "yes"}, {0, "choice", "Memnite"}, {0, "choice", "Swamp"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}

	// Without the card registry the helper cannot tell XMage's optional
	// TargetSacrifice from an auto-resolving cost, so it scripts nothing.
	if got := loneTriggerCostSacrifice(nil, []rules.OracleDecision{gitrog, land}, one); got != nil {
		t.Fatalf("nil registry scripted %v", got)
	}

	bare := [][]XAnswer{nil, {{0, "choice", "yes"}, {0, "choice", "Swamp"}}}
	for name, tc := range map[string]struct {
		d  rules.OracleDecision
		sn []rules.OracleSnapshot
	}{
		// Two creatures died: not a folded lone pick, and two scripted names
		// would both be consumed by XMage's single TargetSacrifice slot.
		"two deaths": {gitrog, snaps([]rules.OracleSnapPerm{perm("The Gitrog, Ravenous Ride", false), perm("Memnite", false), perm("Ornithopter", false)}, []string{"Memnite", "Ornithopter"}, "Memnite", "Ornithopter")},
		// A token sacrifice reaches no graveyard; an unrelated real death on
		// the same step must not be read as the sacrifice.
		"token plus unrelated death": {gitrog, snaps([]rules.OracleSnapPerm{perm("The Gitrog, Ravenous Ride", false), perm("Treasure", true), perm("Grizzly Bears", false)}, []string{"Treasure", "Grizzly Bears"}, "Grizzly Bears")},
		// A source sacrifice (Servant of the Stinger's Sac<1/CARDNAME>) has no
		// choice or target ask for an alias to answer: it would be an
		// unconsumed leftover, so the label alone must not script it.
		"source sacrifice": {pay("Servant of the Stinger", "Sacrifice 1 permanent"),
			snaps([]rules.OracleSnapPerm{perm("Servant of the Stinger", false)}, []string{"Servant of the Stinger"}, "Servant of the Stinger")},
		// A mandatory 1..1 SacrificeTargetCost (Killmonger) auto-resolves a
		// lone candidate: no alias is needed, so its rows stay unmoved.
		"target sacrifice cost": {pay("Killmonger, Scourge of Wakanda", "Sacrifice another creature"),
			snaps([]rules.OracleSnapPerm{perm("Killmonger, Scourge of Wakanda", false), perm("Memnite", false)}, []string{"Memnite"}, "Memnite")},
	} {
		if got := run(tc.d, tc.sn); !reflect.DeepEqual(got, bare) {
			t.Fatalf("%s: xanswers = %#v, want %#v (no guessed sacrifice name)", name, got, bare)
		}
	}
}
