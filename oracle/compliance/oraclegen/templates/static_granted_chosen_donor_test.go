package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// chosenDonorLabel is Wellwisher's activated ability, as the granted option
// is labelled once Koh has chosen it.
const chosenDonorLabel = "You gain 1 life for each Elf on the battlefield"

// kohSteps is the item's steps with the half-open index range [from, to)
// removed: the control variants drop the ChooseCard activation (and its
// resolve) or the turn boundary while keeping every other step.
func kohSteps(it oraclegen.Item, from, to int) oraclegen.Item {
	out := it
	out.Steps = slices.Concat(it.Steps[:from], it.Steps[to:])
	return out
}

// TestStaticGainedAbilitiesOfChosenCardDonorIsOfferedOnlyWhenChosen serves
// Koh, the Face Stealer's Card.ChosenCard grant: the scenario casts Koh, its
// ETB exiles the donor, its own ChooseCard activation chooses it, and after a
// turn boundary (Wellwisher's ability costs {T}, and Koh is summoning-sick the
// turn it is cast) Koh is offered the donor's activation. Each control removes
// one link of that chain and the same assertion must then fail.
func TestStaticGainedAbilitiesOfChosenCardDonorIsOfferedOnlyWhenChosen(t *testing.T) {
	const koh = "Koh, the Face Stealer"
	it, skip := zoneItem(t, koh, "static#0.0")
	if skip != nil {
		t.Fatalf("GenerateB: %s (the ChosenCard donor observation regressed to a skip)", skip.Reason)
	}
	if want := koh + "/static#0.0/v1"; it.ID != want {
		t.Fatalf("identity = %q, want the level-B row %q", it.ID, want)
	}

	// The prelude's shape: cast, ETB-target resolve, ChooseCard activation
	// (ability index 0) and its choose answer, two turn-boundary passes, and
	// the positive offered assertion.
	ops := make([]string, len(it.Steps))
	for i, st := range it.Steps {
		ops[i] = st.Op
	}
	if want := []string{"cast", "resolve", "activate", "resolve", "pass_to", "pass_to", "pass_to"}; !slices.Equal(ops, want) {
		t.Fatalf("step ops = %v, want %v", ops, want)
	}
	if it.Steps[1].Targets == nil || it.Steps[1].Targets[0] != "p0:"+donorWellwisher {
		t.Fatalf("ETB target = %v, want the donor", it.Steps[1].Targets)
	}
	if a := it.Steps[2]; a.AbilityIndex == nil || *a.AbilityIndex != 0 || a.Card != "p0:"+koh {
		t.Fatalf("activation = %+v, want Koh's ChooseCard ability index 0", a)
	}
	if ans := it.Steps[3].Answers; len(ans) != 1 || ans[0].Kind != "choose" || !slices.Equal(ans[0].Pick, []string{"p0:" + donorWellwisher}) {
		t.Fatalf("choose answer = %+v, want the donor picked", ans)
	}
	if it.Steps[4].Active != "p1" || it.Steps[5].Active != "p0" {
		t.Fatalf("turn boundary = %q then %q, want p1 then p0", it.Steps[4].Active, it.Steps[5].Active)
	}
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
		t.Fatalf("scenario lacks a positive offered assertion: %+v", last.Expect)
	}
	if got := last.Expect[0].Offered; got.Kind != "activate" || got.Card != "p0:"+koh || !strings.Contains(got.Label, chosenDonorLabel) {
		t.Fatalf("offered assertion = %+v, want activate on Koh labelled %q", got, chosenDonorLabel)
	}

	// Preconditions: the donor is on the battlefield for the ETB to exile, and
	// Koh is in hand (cast, not placed, so ExiledWithSource and ChosenCard are
	// the engine's own records).
	p0 := it.Setup["p0"]
	if !slices.Contains(p0.Battlefield, donorWellwisher) {
		t.Fatalf("precondition: donor %q is not on p0's battlefield %v", donorWellwisher, p0.Battlefield)
	}
	if slices.Contains(p0.Battlefield, koh) || !slices.Contains(p0.Hand, koh) {
		t.Fatalf("precondition: Koh must start in hand, battlefield=%v hand=%v", p0.Battlefield, p0.Hand)
	}
	if res := runZone(t, it); len(res.Fails) != 0 {
		t.Fatalf("with the donor chosen: %v", res.Fails)
	}

	notOffered := func(t *testing.T, c oraclegen.Item) {
		t.Helper()
		res := runZone(t, c)
		if len(res.Fails) == 0 {
			t.Fatalf("the donor ability is still offered: the assertion is not the chosen binding's")
		}
		if !strings.Contains(res.Fails[0], "offered=false") {
			t.Fatalf("control failed for a reason other than the missing offer: %v", res.Fails)
		}
	}
	t.Run("no donor", func(t *testing.T) {
		// Nothing to exile or choose; the activation is dropped with the
		// ETB target it would have chosen from.
		c := withoutDonor(kohSteps(it, 2, 4), donorWellwisher, "Battlefield")
		c.Steps = slices.Clone(c.Steps)
		c.Steps[1].Targets = nil
		notOffered(t, c)
	})
	t.Run("exiled but never chosen", func(t *testing.T) {
		// The donor is exiled with Koh by the ETB, but ChooseCard never
		// activates: ExiledWithSource alone must not grant the ability.
		notOffered(t, kohSteps(it, 2, 4))
	})
	t.Run("same turn", func(t *testing.T) {
		// Without the turn boundary Koh is summoning-sick and the gained {T}
		// ability is not offered: the boundary is what the observation needs.
		notOffered(t, kohSteps(it, 4, 6))
	})
}
