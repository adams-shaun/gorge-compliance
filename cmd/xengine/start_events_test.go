package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// startTapeUses generates the start file for card's key, plays it, and returns
// the keys the played tape exercised (classify.Uses over the tape, not the
// exercised ledger: playStart credits the key the start's id declares whether
// or not the game met it, so only the tape says the trigger fired).
func startTapeUses(t *testing.T, reg *cards.Registry, b *certdeck.Builder, card, key string) map[classify.Use]int {
	t.Helper()
	res, err := scen.Generate(reg, b, card, key, scen.Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != scen.PathStart || res.Start == nil {
		t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
	}
	raw, err := res.Start.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	in := t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "a.start.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := playStart(reg, []string{in}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	tapes, _ := filepath.Glob(filepath.Join(out, "tapes", "*.json"))
	if len(tapes) == 0 {
		t.Fatalf("no tape written (start %s)", raw)
	}
	f, err := os.Open(tapes[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tp, err := game.ReadTape(f)
	if err != nil {
		t.Fatal(err)
	}
	return classify.Uses(reg, tp, 0, len(tp.Entries)-1, true)
}

// TestStartEventKeyFires: the event-causer starts (scen.eventStart) make the
// trigger they were written for fire in the played game, which the tape
// records: a discard, a crime, a draw, a sacrifice, an attack tap, an untap
// and a target each put the card's trigger on the stack.
func TestStartEventKeyFires(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key, mode string }{
		{"Abyssal Nocturnus", "f0/t0", "Discarded"},
		{"Ajani's Last Stand", "f0/t1", "Discarded"},
		{"Vadmir, New Blood", "f0/t0", "CommitCrime"},
		{"Zimone and Dina", "f0/t0", "Drawn"},
		{"Sheoldred, the Apocalypse", "f0/t1", "Drawn"},
		{"Zodiark, Umbral God", "f0/t1", "Sacrificed"},
		{"Zar Ojanen, Scion of Efrava", "f0/t0", "Taps"},
		{"Aerie Worshippers", "f0/t0", "Untaps"},
		{"Wild Defiance", "f0/t0", "BecomesTarget"},
		{"Treacherous Blessing", "f0/t2", "BecomesTarget"},
	} {
		t.Run(c.mode+"/"+c.card, func(t *testing.T) {
			uses := startTapeUses(t, reg, b, c.card, c.key)
			if uses[classify.Use{Card: c.card, Key: c.key}] == 0 {
				t.Errorf("%s %s: the played tape never put the trigger on the stack; keys %v", c.card, c.key, uses)
			}
		})
	}
}
