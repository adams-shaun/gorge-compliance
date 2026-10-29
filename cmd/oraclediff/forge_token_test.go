package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func tokenLine(t *testing.T, reg *cards.Registry, stem string, k int) string {
	t.Helper()
	c := reg.Tokens[stem]
	if c == nil || len(c.Faces) == 0 || k < 0 || k >= len(c.Faces[0].Abilities) || c.Faces[0].Abilities[k] == nil {
		t.Fatalf("token %s has no ability %d", stem, k)
	}
	return c.Faces[0].Abilities[k].Line
}

func TestForgeAbilityLinesTokenCards(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	t.Run("setup token", func(t *testing.T) {
		k := 0
		it := oraclegen.Item{Scenario: oraclegen.Scenario{
			Setup: map[string]oraclegen.Seat{"p0": {Tokens: []oraclegen.SetupToken{{Script: "c_a_treasure_sac"}}}},
			Steps: []oraclegen.Step{{Op: "activate", Card: "p0:token:Treasure Token", AbilityIndex: &k}},
		}}
		got := forgeAbilityLines(reg, it, rules.OracleResult{})
		if want := tokenLine(t, reg, "c_a_treasure_sac", k); got["0"] != want {
			t.Errorf("setup token line = %q (present %v), want %q", got["0"], got["0"] != "", want)
		}
	})

	t.Run("mid-window token", func(t *testing.T) {
		k := 0
		it := oraclegen.Item{Scenario: oraclegen.Scenario{
			Steps: []oraclegen.Step{
				{Op: "pass", Seat: 0},
				{Op: "activate", Card: "p0:token:Food Token", AbilityIndex: &k},
			},
		}}
		g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
			{},
			{Permanents: []rules.OracleSnapPerm{{Ref: "p0:token:Food Token", Name: "Food Token", Token: true}}},
		}}
		got := forgeAbilityLines(reg, it, g)
		if want := tokenLine(t, reg, "c_a_food_sac", k); got["1"] != want {
			t.Errorf("mid-window token line = %q (present %v), want %q", got["1"], got["1"] != "", want)
		}
	})

	t.Run("token copy of a card", func(t *testing.T) {
		c, ok := reg.Lookup("Mutavault")
		if !ok || len(c.Faces) == 0 || len(c.Faces[0].Abilities) < 2 || c.Faces[0].Abilities[1] == nil {
			t.Fatal("Mutavault ability 1 missing from the corpus")
		}
		k := 1
		it := oraclegen.Item{Scenario: oraclegen.Scenario{
			Steps: []oraclegen.Step{{Op: "activate", Card: "p0:token:Mutavault", AbilityIndex: &k}},
		}}
		g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
			{Permanents: []rules.OracleSnapPerm{{Ref: "p0:token:Mutavault", Name: "Mutavault", Token: true}}},
		}}
		got := forgeAbilityLines(reg, it, g)
		if want := c.Faces[0].Abilities[k].Line; got["0"] != want {
			t.Errorf("token copy line = %q (present %v), want %q", got["0"], got["0"] != "", want)
		}
	})

	t.Run("ambiguous token name", func(t *testing.T) {
		k := 0
		it := oraclegen.Item{Scenario: oraclegen.Scenario{
			Steps: []oraclegen.Step{{Op: "activate", Card: "p0:token:Vehicle Token", AbilityIndex: &k}},
		}}
		g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
			{Permanents: []rules.OracleSnapPerm{{
				Ref: "p0:token:Vehicle Token", Name: "Vehicle Token", Token: true,
				PT: "3/2", Keywords: []string{"Crew:1"},
			}}},
		}}
		got := forgeAbilityLines(reg, it, g)
		if want := tokenLine(t, reg, "c_3_2_a_vehicle_crew_1", k); got["0"] != want {
			t.Errorf("Vehicle Token line = %q (present %v), want the 3/2 Crew 1 line %q", got["0"], got["0"] != "", want)
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(forgeAbilityLines(reg, it, g))
		if !reflect.DeepEqual(a, b) {
			t.Errorf("two exports differ: %s vs %s", a, b)
		}
	})
}

func TestForgeAbilityLinesTransformedStep(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, back string }{
		{"Spring-Loaded Sawblades", "Bladewheel Chariot"},
		{"Twists and Turns", "Mycoid Maze"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok || len(c.Faces) < 2 || len(c.Faces[1].Abilities) < 2 || c.Faces[1].Abilities[1] == nil {
				t.Fatalf("%s back face ability 1 missing from the corpus", tc.name)
			}
			if c.Faces[1].Name != tc.back {
				t.Fatalf("%s back face name = %q, want %q", tc.name, c.Faces[1].Name, tc.back)
			}
			k := 1
			it := oraclegen.Item{Scenario: oraclegen.Scenario{
				Steps: []oraclegen.Step{
					{Op: "pass", Seat: 0},
					{Op: "activate", Card: "p0:" + tc.name, AbilityIndex: &k},
				},
			}}
			g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
				{},
				{Permanents: []rules.OracleSnapPerm{{Ref: "p0:" + tc.name, Name: tc.back}}},
			}}
			got := forgeAbilityLines(reg, it, g)
			if want := c.Faces[1].Abilities[k].Line; got["1"] != want {
				t.Errorf("transformed step line = %q (present %v), want %q", got["1"], got["1"] != "", want)
			}
		})
	}
}

func TestForgeAbilityLinesKeepsExistingStep(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Collector's Cage"
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 || len(c.Faces[0].Abilities) == 0 || c.Faces[0].Abilities[0] == nil {
		t.Fatalf("%s front ability 0 missing from the corpus", name)
	}
	k := 0
	it := oraclegen.Item{Scenario: oraclegen.Scenario{
		Steps: []oraclegen.Step{{Op: "activate", Card: "p0:" + name, AbilityIndex: &k}},
	}}
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{{Ref: "p0:" + name, Name: "Bladewheel Chariot"}}},
	}}
	got := forgeAbilityLines(reg, it, g)
	if want := c.Faces[0].Abilities[k].Line; got["0"] != want {
		t.Errorf("existing step line = %q, want the unchanged front-face line %q", got["0"], want)
	}
	if len(got) != 1 {
		t.Errorf("abilities = %v, want only step 0", got)
	}
}
