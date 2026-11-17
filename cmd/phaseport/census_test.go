package main

import (
	"reflect"
	"strings"
	"testing"
)

// The fixture is two invented tests (not phase.rs source): one on real cards
// only, one that builds an object from Oracle text.
const censusFixture = `
use crate::scenario::GameScenario;

/// CR 704.5a: a player at 0 life loses.
#[test]
fn bears_take_damage() {
    let mut scenario = GameScenario::new();
    scenario.at_phase(Phase::PreCombatMain);
    let bears = scenario.add_real_card(P0, "Grizzly Bears", Zone::Battlefield, db);
    let mut runner = scenario.build();
    // CR 120.3: damage dealt to a player causes life loss.
    runner.pass_priority();
    assert_eq!(runner.state().players[0].life, 20, "CR 119.1 starting life");
    assert!(runner.state().objects.contains_key(&bears));
}

#[test]
fn oracle_text_object() {
    let mut scenario = GameScenario::new();
    let id = scenario.add_creature_from_oracle(P0, "Test Beast", 2, 2, "Flying");
    let mut runner = scenario.build();
    assert_eq!(runner.state().objects[&id].power, 2);
}

#[test]
fn vanilla_with_real() {
    let mut scenario = GameScenario::new();
    scenario.add_real_card(P0, "Grizzly Bears", Zone::Hand, db);
    scenario.add_creature(P1, "Bear", 2, 2);
    let mut runner = scenario.build();
    assert_ne!(runner.state().turn_number, 0);
}

#[test]
fn builder_hidden_in_helper() {
    let runner = common_board();
    assert_eq!(runner.state().turn_number, 1);
}
`

func fakeSupported(name string) (supported, found bool) {
	return name == "Grizzly Bears", name == "Grizzly Bears"
}

func TestCensusClassifiesBuilderCalls(t *testing.T) {
	rows := censusFile("fixture.rs", censusFixture, fakeSupported)
	if len(rows) != 4 {
		t.Fatalf("want 4 tests, got %d: %+v", len(rows), rows)
	}
	by := map[string]testRow{}
	for _, r := range rows {
		by[r.Name] = r
	}

	a := by["bears_take_damage"]
	if a.Class != "real-only" {
		t.Errorf("bears_take_damage class = %q, want real-only", a.Class)
	}
	if !reflect.DeepEqual(a.Cards, []string{"Grizzly Bears"}) || !a.GorgeOK {
		t.Errorf("bears_take_damage cards=%v gorge_ok=%v", a.Cards, a.GorgeOK)
	}
	for _, want := range []string{"add_real_card", "at_phase", "build", "runner.pass_priority"} {
		if !contains(a.Calls, want) {
			t.Errorf("calls %v missing %q", a.Calls, want)
		}
	}
	for _, want := range []string{"assert_eq:life", "assert:contains_key"} {
		if !contains(a.Asserts, want) {
			t.Errorf("asserts %v missing %q", a.Asserts, want)
		}
	}
	if !reflect.DeepEqual(a.CRs, []string{"119.1", "120.3", "704.5a"}) {
		t.Errorf("CR cites = %v", a.CRs)
	}

	if b := by["oracle_text_object"]; b.Class != "synthetic" || b.GorgeOK {
		t.Errorf("oracle_text_object class=%q gorge_ok=%v, want synthetic/false", b.Class, b.GorgeOK)
	}
	if c := by["vanilla_with_real"]; c.Class != "real+vanilla" || !c.GorgeOK {
		t.Errorf("vanilla_with_real class=%q gorge_ok=%v", c.Class, c.GorgeOK)
	}
	if d := by["builder_hidden_in_helper"]; d.Class != "unparsed" {
		t.Errorf("builder_hidden_in_helper class=%q, want unparsed", d.Class)
	}
}

func TestCensusMasksStringsAndComments(t *testing.T) {
	src := `
#[test]
fn braces_in_strings() {
    let mut s = GameScenario::new();
    // a stray } and an add_creature_from_oracle( in a comment
    s.add_real_card(P0, "Gorion's \"Ward\" }", Zone::Hand, db);
    let c = '}';
    let r = r#"} add_creature_from_oracle("#;
    assert!(true);
}

#[test]
fn second() {
    let mut s = GameScenario::new();
    s.add_real_card(P0, "Island", Zone::Hand, db);
}
`
	rows := censusFile("f.rs", src, func(string) (bool, bool) { return true, true })
	if len(rows) != 2 {
		t.Fatalf("want 2 tests, got %d", len(rows))
	}
	if rows[0].Class != "real-only" || !reflect.DeepEqual(rows[0].Cards, []string{`Gorion's "Ward" }`}) {
		t.Errorf("first: class=%q cards=%q", rows[0].Class, rows[0].Cards)
	}
	if strings.Contains(strings.Join(rows[0].Calls, ","), "from_oracle") {
		t.Errorf("comment/string call leaked into calls: %v", rows[0].Calls)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
