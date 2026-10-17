package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

func TestComboManaPrefixNamesGorgesColour(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		card, key string
		// each marks the variable-amount shape (Forge `Each$ X`): the XMage
		// selector must read "{T}: Add X {C}", not the fixed-amount form.
		each bool
	}{
		{"Boros Guildgate", "activate#0.0", false},
		{"Simic Guildgate", "activate#0.0", false},
		{"Temple of Mystery", "activate#0.0", false},
		{"Creosote Heath", "activate#0.0", false},
		{"Blossoming Sands", "activate#0.0", false},
		{"Brigid, Clachan's Heart", "activate#1.0", true},
	} {
		t.Run(tc.card, func(t *testing.T) {
			it, req := activateRequirement(t, reg, tc.card, tc.key)
			card, ok := reg.Lookup(tc.card)
			if !ok || req.Face >= len(card.Faces) {
				t.Fatalf("precondition: %s face %d is available", tc.card, req.Face)
			}
			idx, err := strconv.Atoi(req.Slot)
			if err != nil || idx < 0 || idx >= len(card.Faces[req.Face].Abilities) {
				t.Fatalf("precondition: activate slot %q identifies an ability", req.Slot)
			}
			sa := card.Faces[req.Face].Abilities[idx]
			if sa.API != "Mana" || strings.TrimSpace(sa.ParamStr(cards.PKCost)) != "T" || !strings.HasPrefix(sa.ParamStr(cards.PKProduced), "Combo ") {
				t.Fatalf("precondition: %s ability has plain-tap Combo mana shape: API=%q cost=%q produced=%q", tc.card, sa.API, sa.ParamStr(cards.PKCost), sa.ParamStr(cards.PKProduced))
			}
			// Precondition: the Each$ variable-amount param is exactly where
			// the selector derivation looks for it (and absent otherwise).
			if (strings.TrimSpace(sa.Params["Each"]) != "") != tc.each {
				t.Fatalf("precondition: %s Each$ param = %q, want each=%v", tc.card, sa.Params["Each"], tc.each)
			}
			res, played := oraclegen.PlaysThrough(reg, it.Scenario)
			step := activateStepIndex(it.Scenario.Steps)
			if !played || len(res.Snapshots) <= step+1 {
				t.Fatalf("precondition: %s scenario plays through activate step %d", tc.card, step)
			}
			pool := res.Snapshots[step+1].Players[0].Pool
			var want string
			if tc.each {
				// Precondition: the scenario has no other creatures, so X=0
				// and gorge adds nothing (and asks no colour); the selector
				// falls back to the first produced colour.
				if pool != "" {
					t.Fatalf("precondition: %s Each$ X=0 pool %q is empty", tc.card, pool)
				}
				want = "{T}: Add X {" + strings.Fields(sa.ParamStr(cards.PKProduced))[1] + "}"
			} else {
				if len(pool) == 0 || !strings.ContainsRune("WUBRG", rune(pool[0])) || strings.Trim(pool, pool[:1]) != "" {
					t.Fatalf("precondition: gorge pool %q is one colour", pool)
				}
				want = "{T}: Add {" + string(pool[0]) + "}"
			}
			if step >= len(it.XAbility) || it.XAbility[step] != want {
				t.Errorf("XMage ability = %v at step %d, want %q (gorge pool %q)", it.XAbility, step, want, pool)
			}
			for _, answer := range it.XAnswers[step] {
				if answer.Kind == "choice" && isColourName(answer.Value) {
					t.Errorf("colour choice %+v should be omitted; XMage ability names gorge's colour", answer)
				}
			}
		})
	}
}
