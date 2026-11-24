package main

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestForgeAbilityLinesUseSetupFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Ojer Taq, Deepest Foundation"
	card, ok := reg.Lookup(name)
	if !ok || len(card.Faces) < 2 {
		t.Fatalf("%s missing from corpus or does not have a back face", name)
	}
	back := card.Faces[1]
	abilityIndex := -1
	for i, ability := range back.Abilities {
		if ability != nil {
			abilityIndex = i
			break
		}
	}
	if abilityIndex < 0 {
		t.Fatalf("%s back face has no non-nil ability", name)
	}

	idx := abilityIndex
	backItem := oraclegen.Item{Scenario: oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{"p0": {BackFace: []string{name}}},
		Steps: []oraclegen.Step{{Op: "activate", Card: "p0:" + name, AbilityIndex: &idx}},
	}}
	backLines := forgeAbilityLines(reg, backItem, rules.OracleResult{})
	gotBack, exists := backLines["0"]
	if !exists || gotBack != back.Abilities[abilityIndex].Line {
		t.Fatalf("back-face ability line = %q (present %v), want %q", gotBack, exists, back.Abilities[abilityIndex].Line)
	}

	frontFace := card.Faces[0]
	frontItem := backItem
	frontItem.Scenario.Setup = map[string]oraclegen.Seat{"p0": {}}
	frontLines := forgeAbilityLines(reg, frontItem, rules.OracleResult{})
	frontIndex := -1
	for i, ability := range frontFace.Abilities {
		if ability != nil {
			frontIndex = i
			break
		}
	}
	if frontIndex < 0 {
		if _, exists := frontLines["0"]; exists {
			t.Errorf("front face has no ability but exported %q", frontLines["0"])
		}
		return
	}
	frontStep := frontItem.Steps[0]
	frontStep.AbilityIndex = &frontIndex
	frontItem.Steps = []oraclegen.Step{frontStep}
	frontLines = forgeAbilityLines(reg, frontItem, rules.OracleResult{})
	if got, want := frontLines["0"], frontFace.Abilities[frontIndex].Line; got != want {
		t.Errorf("front-face ability line = %q, want %q", got, want)
	}
}

// TestForgeAbilityLinesUseSnapshotFaceForModalLand covers a modal DFC played as
// its land back face (CR 712.12) without a setup back_face entry. Its front
// spell and back land ability can share printed ability index 0, so the setup
// face resolves to the front's spell line; the snapshot entering the step names
// the face that is up, and that face must win.
func TestForgeAbilityLinesUseSnapshotFaceForModalLand(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Turntimber Symbiosis"
	card, ok := reg.Lookup(name)
	if !ok || len(card.Faces) < 2 {
		t.Fatalf("%s missing from corpus or does not have a back face", name)
	}
	front, back := card.Faces[0], card.Faces[1]
	k := -1
	for i, ability := range back.Abilities {
		if ability != nil {
			k = i
			break
		}
	}
	if k < 0 {
		t.Fatalf("%s back face has no non-nil ability", name)
	}
	if k >= len(front.Abilities) || front.Abilities[k] == nil {
		t.Fatalf("%s front face has no ability %d for the resolver to pick by mistake", name, k)
	}
	idx := k
	it := oraclegen.Item{Scenario: oraclegen.Scenario{
		Steps: []oraclegen.Step{{Op: "activate", Card: "p0:" + name, AbilityIndex: &idx}},
	}}
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{{Ref: "p0:" + name, Name: back.Name}}},
	}}
	got := forgeAbilityLines(reg, it, g)
	if want := back.Abilities[k].Line; got["0"] != want {
		t.Errorf("modal land back-face line = %q, want %q (front line was %q)", got["0"], want, front.Abilities[k].Line)
	}
}
