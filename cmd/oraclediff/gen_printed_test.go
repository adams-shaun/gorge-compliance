package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
)

// TestGenPrintedAddsCardsXMageLacks: -printed clears the unfinished list and
// adds the printed cards XMage's manifest omits, without stamping an XMage
// spelling on a card XMage does not have.
func TestGenPrintedAddsCardsXMageLacks(t *testing.T) {
	reg, err := loadReg(filepath.Join("..", "..", ".cards"))
	if err != nil {
		t.Fatal(err)
	}
	m := compliance.Manifest{Code: "ZZP", Cards: []compliance.ManifestCard{
		{Name: "Shock"}, {Name: "Lightning Bolt"},
	}, Unfinished: []string{"Lightning Bolt"}}
	// The last name is a punctuation alias: it resolves to the corpus's
	// "With Great Power . . .", so the alias differs from the generated
	// spelling. That is what makes a missing !PrintedOnly stamp guard fail.
	pr := compliance.Printed{Code: "ZZP", Cards: []string{
		"Shock", "Lightning Bolt", "Giant Growth", "With Great Power...",
	}}
	tmp := t.TempDir()

	plain := filepath.Join(tmp, "plain.jsonl")
	if err := genManifest(reg, m, plain, "A"); err != nil {
		t.Fatal(err)
	}
	if got := readLinesBytes(t, plain); len(got) != 1 {
		t.Fatalf("plain gen wrote %d lines, want 1 (Shock; Lightning Bolt is unfinished)", len(got))
	}

	printed := filepath.Join(tmp, "printed.jsonl")
	if err := genManifest(reg, withPrinted(m, pr), printed, "A"); err != nil {
		t.Fatal(err)
	}
	got := readLinesBytes(t, printed)
	if len(got) != 4 {
		t.Fatalf("printed gen wrote %d lines, want 4", len(got))
	}
	items := map[string]oraclegen.Item{}
	for _, b := range got {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			t.Fatal(err)
		}
		items[it.Card] = it
	}
	for _, tc := range []struct {
		printed, corpus string
	}{
		{"Giant Growth", "Giant Growth"},
		// gen writes the corpus spelling and must not stamp the printed alias
		// on it: XMage has neither spelling.
		{"With Great Power...", "With Great Power . . ."},
	} {
		it, ok := items[tc.corpus]
		if !ok {
			t.Fatalf("printed gen did not write %q (printed as %q)", tc.corpus, tc.printed)
		}
		if it.XMageName != "" {
			t.Errorf("%q carries xmage_name %q; XMage has no such card", tc.corpus, it.XMageName)
		}
		want, skip := templates.Generate(reg, tc.corpus)
		if skip != nil {
			t.Fatal(skip.Reason)
		}
		if gate.ItemSHA(it) != gate.ItemSHA(want) {
			t.Errorf("%q's scenario sha differs from templates.Generate's", tc.corpus)
		}
	}
}
