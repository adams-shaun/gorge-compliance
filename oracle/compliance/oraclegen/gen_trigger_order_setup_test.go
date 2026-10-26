package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// setupWith lists setup battlefield permanents per seat, the shape a scenario
// records for the seeded permanents XMage places in build().
func setupWith(seat string, names ...string) map[string]Seat {
	return map[string]Seat{seat: {Battlefield: names}}
}

// TestTriggerOrderSetupPosedQueuesNameAsSetupChoice pins the retiming: a
// trigger-order decision the runner recorded before step zero whose picks all
// name setup-placed permanents is posed by XMage during setup placement, so
// its name answers must be queued as setup_choice in xmage_answers[0] (the
// driver reads that slot before build()) or XMage's chooseTriggeredAbility
// falls to the pending-list fallback. The last pick is dropped: XMage pushes
// the last remaining ability without an ask.
func TestTriggerOrderSetupPosedQueuesNameAsSetupChoice(t *testing.T) {
	d := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	setup := setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler")
	// Precondition: the sources differ (a same-source order cannot be named)
	// and both are setup permanents, so the setup membership is what the
	// predicate must decide on.
	if !triggerOrderNamesDistinct(d) {
		t.Fatalf("precondition: picks must have distinct source names, refs %v", d.PickRefs)
	}
	if !triggerOrderSetupPosed(d, setup) {
		t.Fatalf("precondition: %v must be setup-posed", d.PickRefs)
	}
	got := xanswersSetup([]rules.OracleDecision{d}, 2, nil, nil, setup, nil)
	want := [][]XAnswer{{{0, "setup_choice", "Adrenaline Jockey"}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (setup-posed order queues the name before build)", got, want)
	}
}

// TestTriggerOrderSetupPosedReachesXAnswersForScenario pins the wiring: the
// scenario-aware entry point the templates call must pass the scenario's setup
// through, or the retiming never fires in generated scenarios.
func TestTriggerOrderSetupPosedReachesXAnswersForScenario(t *testing.T) {
	d := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	sc := Scenario{
		Name:  "gen1-trigger#0.0",
		Setup: setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler"),
	}
	if len(sc.Setup["p0"].Battlefield) != 2 {
		t.Fatalf("precondition: setup must place both sources, got %v", sc.Setup)
	}
	got := xanswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{d}}, sc, nil, nil)
	want := [][]XAnswer{{{0, "setup_choice", "Adrenaline Jockey"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersForScenario = %#v, want %#v (setup threaded to the retiming)", got, want)
	}
}

// TestTriggerOrderSetupPosedRequiresSetupSources pins the predicate's second
// half: a before-step-zero order whose picks are NOT setup permanents keeps
// the old behaviour (no answers). A wrong predicate here would queue a name
// for an ask XMage poses at its own step and derail the step's dialogs.
func TestTriggerOrderSetupPosedRequiresSetupSources(t *testing.T) {
	d := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	setup := setupWith("p0", "Grizzly Bears")
	// Precondition: the sources differ, so ONLY the setup membership can make
	// the predicate false.
	if !triggerOrderNamesDistinct(d) {
		t.Fatalf("precondition: picks must have distinct source names, refs %v", d.PickRefs)
	}
	if triggerOrderSetupPosed(d, setup) {
		t.Fatalf("precondition: %v must not be setup-posed", d.PickRefs)
	}
	if got := xanswersSetup([]rules.OracleDecision{d}, 2, nil, nil, setup, nil); got != nil {
		t.Fatalf("xanswersSetup = %#v, want nil (non-setup sources keep the old drop)", got)
	}
}

// TestTriggerOrderSetupSameSourceQueuesRuleText pins the same-source setup
// retiming: a name matches both abilities' source, and Forge's verbatim text
// is never consumed (queued, it derailed the as-enters dialogs sharing the
// queue: Ashling, Rekindled and Gathering Stone HARNESSed). A pick keyed in
// XMage's rule-text form is consumed by the order ask itself, so it is queued
// as setup_choice; picks that cannot be keyed apart (one pick's whole text a
// prefix of another, different text) stay unqueued.
func TestTriggerOrderSetupSameSourceQueuesRuleText(t *testing.T) {
	d := ashlingOrder(-1)
	setup := setupWith("p0", "Ashling, Rekindled")
	if triggerOrderNamesDistinct(d) {
		t.Fatalf("precondition: picks must share one source name, refs %v", d.PickRefs)
	}
	got := xanswersSetup([]rules.OracleDecision{d}, 2, nil, nil, setup, nil)
	want := [][]XAnswer{{{0, "setup_choice", "Whenever this creature transforms into {this} and at the beginning of your first main phase, add two mana of any one color."}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (a keyable same-source setup order is queued)", got, want)
	}

	unkeyable := triggerOrderDecision(-1, "p0:Thundertrap Trainer", "p0:Thundertrap Trainer")
	unkeyable.Picks = []string{"Thundertrap Trainer: When this enters, draw a card", "Thundertrap Trainer: When this enters, draw a card and gain 1 life."}
	if _, ok := xmageTriggerOrderKeys(unkeyable); ok {
		t.Fatalf("precondition: %q is a prefix of the other pick and must not key", unkeyable.Picks[0])
	}
	if got := xanswersSetup([]rules.OracleDecision{unkeyable}, 2, nil, nil, setupWith("p0", "Thundertrap Trainer"), nil); got != nil {
		t.Fatalf("xanswersSetup = %#v, want nil (an unkeyable same-source setup order stays unqueued)", got)
	}
}

// TestTriggerOrderSetupQueueFollowsAsEntersChoice pins the shared-queue order:
// when a setup permanent also poses an as-enters colour/type choice, that
// answer must precede the order names -- XMage consumes the placement dialog
// during build() and the ordering ask only at the first priority. The order is
// recorded FIRST here, so a decision-order append would invert the queue.
func TestTriggerOrderSetupQueueFollowsAsEntersChoice(t *testing.T) {
	order := triggerOrderDecision(-1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	color := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb",
		Options: 1, Min: 1, Max: 1, Picks: []string{"White"}, PickIdx: []int{0},
		PickKinds: []string{"color"}}
	setup := setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler")
	if !IsSetupChoice(color) {
		t.Fatalf("precondition: %+v must be an as-enters setup choice", color)
	}
	got := xanswersSetup([]rules.OracleDecision{order, color}, 1, nil, nil, setup, nil)
	want := [][]XAnswer{{{0, "setup_choice", "White"}, {0, "setup_choice", "Adrenaline Jockey"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (as-enters choice leads the pre-build queue)", got, want)
	}
}

// TestTriggerOrderGameplaySharedStepStaysDemoted pins that the setup retiming
// does not leak into a gameplay step: a shared-step order at step 1 -- even
// when its sources are setup permanents -- keeps the inert text form, because
// XMage poses that ask at its own step and a leftover name would be consumed
// by the step's other dialogs (measured: Baron Strucker/static#0.0 HARNESSes
// under the name form; the demoted text form agrees).
func TestTriggerOrderGameplaySharedStepStaysDemoted(t *testing.T) {
	order := triggerOrderDecision(1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	target := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Max: 1, Picks: []string{"Wastes"}, PickRefs: []string{"p0:Wastes#9"},
		PickIdx: []int{0}, PickKinds: []string{"search"}}
	setup := setupWith("p0", "Adrenaline Jockey", "Rangers' Refueler")
	// Precondition: the order's sources ARE setup permanents, so only the
	// gameplay step (not the setup membership) keeps the demotion.
	if order.Step < 0 {
		t.Fatalf("precondition: the order must be recorded at a gameplay step")
	}
	if !triggerOrderNamesDistinct(order) {
		t.Fatalf("precondition: picks must have distinct source names, refs %v", order.PickRefs)
	}
	got := xanswersSetup([]rules.OracleDecision{target, order}, 2, nil, nil, setup, nil)
	want := [][]XAnswer{
		nil,
		{{0, "target", "Wastes"}, {0, "choice", "When this enters, do a thing."}, {0, "choice", "When this enters, do a thing."}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (a gameplay shared step keeps the inert text form)", got, want)
	}
}
