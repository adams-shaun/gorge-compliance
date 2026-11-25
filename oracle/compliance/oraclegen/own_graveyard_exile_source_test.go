package oraclegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestOwnGraveyardExileChoiceRidesTargetQueue pins xproof
// xs:XP:bloodcurdler:f0t0:s1: Bloodcurdler's threshold end-step trigger
// "exile two cards from your graveyard" is XMage's
// ExileCardFromOwnGraveyardControllerEffect, which builds a
// TargetCardInYourGraveyard(Math.min(2, graveyard size)).withNotTarget(true)
// and asks player.chooseTarget once. TestPlayer serves that as one target
// definition on the TARGET queue, one "^" segment per pick, so gorge's one
// hidden_pick of two Swamps must script "Swamp^Swamp" with kind target. The
// generic choose_n arm joined the picks into one makeChoose choice
// ("Swamp^Swamp"), which no target ask reads: strict XMage stopped at
// "Missing TARGET def ... TargetCardInYourGraveyard" and left the joined
// choice unused.
//
// The scenario is the find's own (setup plus steps through the end step), run
// through gorge, so the route is proven from the real hidden_pick decision and
// the resolving source, not a hand-built decision shape.
func TestOwnGraveyardExileChoiceRidesTargetQueue(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	zero := 0
	sc := Scenario{
		Name: "Bloodcurdler threshold end step", Turn: 1,
		Why: "the own-graveyard exile picks are XMage's TARGET queue",
		Setup: map[string]Seat{
			"p0": {
				Battlefield: []string{"Swamp", "Swamp"},
				Hand:        []string{"Bloodcurdler"},
				Graveyard:   []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp"},
				LibraryTop:  []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp"},
			},
			"p1": {LibraryTop: []string{"Plains", "Plains", "Plains", "Plains", "Plains", "Plains"}},
		},
		Steps: []Step{
			{Op: "activate", Seat: 0, Card: "p0:Swamp", AbilityIndex: &zero},
			{Op: "activate", Seat: 0, Card: "p0:Swamp#2", AbilityIndex: &zero},
			{Op: "cast", Seat: 0, Card: "p0:Bloodcurdler"},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
		},
	}
	it := Item{ID: "xs:XP:bloodcurdler:f0t0:s1", Template: "xproof-start", Scenario: sc}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge rejects the window: %v", res.Fails)
	}
	var pick *rules.OracleDecision
	for i := range res.Decisions {
		if res.Decisions[i].Resume != "hidden_pick" {
			continue
		}
		pick = &res.Decisions[i]
	}
	if pick == nil {
		t.Fatalf("no hidden_pick decision: %+v", res.Decisions)
	}
	if pick.Min != 2 || pick.Max != 2 || len(pick.Picks) != 2 {
		t.Fatalf("end-step ask = %+v, want the mandatory two-card pick", *pick)
	}
	if !strings.EqualFold(pick.Picks[0], "Swamp") || !strings.EqualFold(pick.Picks[1], "Swamp") ||
		pick.PickRefs[0] == pick.PickRefs[1] {
		t.Fatalf("end-step picks = %v / %v, want two distinct Swamps", pick.Picks, pick.PickRefs)
	}
	got := XAnswersForScenario(reg, res, sc, nil, nil)[pick.Step]
	// The effect calls player.chooseTarget ONCE, so the answer is one TARGET
	// definition whose "^" segments are the two picks (the same spelling the
	// Dig graveyard take uses). Two separate definitions would let the one ask
	// consume only the first: measured on the local build, Bloodcurdler then
	// exiled one Swamp and left the second answer to a later ask.
	segment0 := targetPickValue(*pick, 0, pick.Picks[0])
	segment1 := targetPickValue(*pick, 1, pick.Picks[1])
	want := []XAnswer{{Seat: 0, Kind: "target", Value: segment0 + "^" + segment1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("step %d answers = %+v, want %+v (one TARGET definition, one segment per pick)", pick.Step, got, want)
	}
	if len(got) != 1 || got[0].Kind != "target" {
		t.Fatalf("step %d answers = %+v, want exactly one answer on the TARGET queue", pick.Step, got)
	}
	segments := strings.Split(got[0].Value, "^")
	if len(segments) != 2 || segments[0] != segment0 || segments[1] != segment1 {
		t.Fatalf("target definition %q = %v, want two Swamp segments %q^%q", got[0].Value, segments, segment0, segment1)
	}
	for _, a := range got {
		if a.Kind == "choice" {
			t.Fatalf("joined choice answer %+v scripted; XMage reads no makeChoose there", a)
		}
	}

	// The route owns only a decision whose step resolved to the proven face: a
	// bare hidden_pick shape with no source has no ask to answer.
	shapeOnly := newAnswerRouting([]rules.OracleDecision{*pick})
	if as, owned := shapeOnly.route(0); owned || len(as) != 0 {
		t.Fatalf("unproven step routed: answers=%+v owned=%v", as, owned)
	}
}

// TestOwnGraveyardExileFacesCensus pins the proven face set against both
// sides of the census. The XMage side is the `git grep -l
// 'new ExileCardFromOwnGraveyardControllerEffect' -- Mage.Sets` result at
// XMAGE_REF 464e1a6b23; the Forge side is every corpus face carrying the
// hidden, mandatory own-graveyard ChangeZone shape the route detects. The two
// must name the same two faces: a new Forge carrier fails the first loop (add
// it deliberately only after checking its XMage implementation), and a listed
// face that loses the shape fails the second.
func TestOwnGraveyardExileFacesCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	// xmageCensus is the XMage source census, written independently of the
	// production map so a name added there must be proven here too.
	xmageCensus := map[string]bool{
		"bloodcurdler":   true,
		"false memories": true,
	}
	if !reflect.DeepEqual(xmageOwnGraveyardExileFaces, xmageCensus) {
		t.Fatalf("proven face set = %v, want the XMage census %v", xmageOwnGraveyardExileFaces, xmageCensus)
	}
	corpus := map[string]bool{}
	for _, c := range reg.AllCards() {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			if f != nil && ownGraveyardExileOnFace(f) {
				corpus[strings.ToLower(f.Name)] = true
			}
		}
	}
	for name := range corpus {
		if !xmageOwnGraveyardExileFaces[name] {
			t.Errorf("%s: corpus face carries the own-graveyard exile shape but is not in the proven set", name)
		}
	}
	for name := range xmageOwnGraveyardExileFaces {
		if !corpus[name] {
			t.Errorf("%s: listed face has no own-graveyard exile shape in the corpus", name)
		}
	}
	if len(corpus) != 2 {
		t.Fatalf("corpus carries the own-graveyard exile shape on %d faces (%v), want the 2 XMage effect users",
			len(corpus), corpus)
	}
}
