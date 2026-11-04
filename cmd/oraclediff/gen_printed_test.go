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
	pr := compliance.Printed{Code: "ZZP", Cards: []string{"Shock", "Lightning Bolt", "Giant Growth"}}
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
	if len(got) != 3 {
		t.Fatalf("printed gen wrote %d lines, want 3", len(got))
	}
	var giant oraclegen.Item
	found := false
	for _, b := range got {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			t.Fatal(err)
		}
		if it.Card == "Giant Growth" {
			giant, found = it, true
		}
	}
	if !found {
		t.Fatal("printed gen did not write Giant Growth")
	}
	if giant.XMageName != "" {
		t.Errorf("Giant Growth carries xmage_name %q; XMage has no such card", giant.XMageName)
	}
	want, skip := templates.Generate(reg, "Giant Growth")
	if skip != nil {
		t.Fatal(skip.Reason)
	}
	if gate.ItemSHA(giant) != gate.ItemSHA(want) {
		t.Errorf("Giant Growth's scenario sha differs from templates.Generate's")
	}
}
