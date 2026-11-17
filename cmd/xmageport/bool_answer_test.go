package main

import "testing"

func TestBoolAnswerMapsToBinaryAsk(t *testing.T) {
	// a villainous choice: a modes ask whose two labels are not yes/no
	modes := parseOptionDump(`answer "no" not offered for modes: [0 mode "You gain control of a creature of your choice." obj=0 pl=0 att=0 mode=] [1 mode "That player sacrifices a creature." obj=0 pl=0 att=0 mode=] `)
	if len(modes) != 2 {
		t.Fatalf("parsed %d options, want 2: %+v", len(modes), modes)
	}
	if got, ok := mapBoolAnswer("no", modes); !ok || got != "That player sacrifices a creature." {
		t.Errorf("no -> %q, %v; want the second label", got, ok)
	}
	if got, ok := mapBoolAnswer("yes", modes); !ok || got != "You gain control of a creature of your choice." {
		t.Errorf("yes -> %q, %v; want the first label", got, ok)
	}

	// cumulative upkeep: matched by Kind, whatever the order
	for _, opts := range [][]dumpOption{
		{{Index: 0, Kind: "cumulative_pay", Label: "Pay 1 per age (1 age counter(s))"}, {Index: 1, Kind: "sacrifice", Label: "Sacrifice Energy Storm"}},
		{{Index: 0, Kind: "sacrifice", Label: "Sacrifice Energy Storm"}, {Index: 1, Kind: "cumulative_pay", Label: "Pay 1 per age (1 age counter(s))"}},
	} {
		if got, ok := mapBoolAnswer("no", opts); !ok || got != "Sacrifice Energy Storm" {
			t.Errorf("no -> %q, %v; want the Sacrifice option (%+v)", got, ok, opts)
		}
		if got, ok := mapBoolAnswer("yes", opts); !ok || got != "Pay 1 per age (1 age counter(s))" {
			t.Errorf("yes -> %q, %v; want the pay option (%+v)", got, ok, opts)
		}
	}

	// the real ask also offers a choose-mana-sources variant of the payment
	real := parseOptionDump(`answer "x" not offered for choose: [0 cumulative_pay "Pay 1 per age (1 age counter(s))" obj=4 pl=0 att=0 mode=] [1 cumulative_sac "Sacrifice Energy Storm" obj=4 pl=0 att=0 mode=] [2 cumulative_pay_manual "Pay 1 per age (1 age counter(s)) (choose mana sources)" obj=4 pl=0 att=0 mode=] `)
	if got, ok := mapBoolAnswer("no", real); !ok || got != "Sacrifice Energy Storm" {
		t.Errorf("real cumulative ask: no -> %q, %v; want the Sacrifice option", got, ok)
	}

	// not a binary non-yes/no ask: no mapping
	for name, opts := range map[string][]dumpOption{
		"yes/no ask":  {{0, "yes", "Yes", ""}, {1, "no", "No", ""}},
		"three modes": {{0, "mode", "a", ""}, {1, "mode", "b", ""}, {2, "mode", "c", ""}},
		"one option":  {{0, "mode", "a", ""}},
	} {
		if got, ok := mapBoolAnswer("no", opts); ok {
			t.Errorf("%s: mapped to %q, want no mapping", name, got)
		}
	}
	if _, ok := mapBoolAnswer("Hill Giant", modes); ok {
		t.Error("a non-boolean pick must not map")
	}
}

func TestFallbackAsksIndexesByStep(t *testing.T) {
	tr := []string{"setup done: turn 1 main1, stack 0", "step resolve ", "step pass_to ",
		"  [pass_to fallback] p0 blockers -> []", "step cast p0:X", "  [mana] p0 +W",
		"step pass_to ", "  [pass_to fallback] p1 modes -> [\"a\"]"}
	got := fallbackAsks(tr)
	if len(got[1]) != 1 || got[1][0] != (fallbackAsk{0, "blockers"}) || len(got[3]) != 1 || got[3][0] != (fallbackAsk{1, "modes"}) {
		t.Fatalf("fallbackAsks = %+v", got)
	}
}
