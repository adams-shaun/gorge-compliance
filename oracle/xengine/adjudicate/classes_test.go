package adjudicate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const classRegisterPath = "../diagnosed-classes.tsv"

func writeClasses(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "classes.tsv")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClassRegisterWellFormed(t *testing.T) {
	rows, err := ReadClasses(classRegisterPath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(classRegisterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), ClassHeader+"\n") {
		t.Errorf("the register does not start with its header")
	}
	t.Logf("%d diagnosed class(es)", len(rows))
	if rows, err := ReadClasses(filepath.Join(t.TempDir(), "absent.tsv")); err != nil || rows != nil {
		t.Errorf("a missing file: %v, %v; want no rows", rows, err)
	}
}

func TestClassRegisterRejects(t *testing.T) {
	good := "0123456789ab\tFX|G\t*\tactivate:Mana\t-\t702.4b\t-\tt1"
	for _, c := range []struct{ name, body, want string }{
		{"bad header", "sig\tpattern\n" + good + "\n", ":1:"},
		{"7 columns", ClassHeader + "\n" + "0123456789ab\tFX|G\t*\tactivate:Mana\t-\t702.4b\t-\n", ":2: 7 columns"},
		{"bad sig", ClassHeader + "\n" + strings.Replace(good, "0123456789ab", "0123456789AB", 1) + "\n", "sig"},
		{"short sig", ClassHeader + "\n" + strings.Replace(good, "0123456789ab", "0123", 1) + "\n", "sig"},
		{"bad cr", ClassHeader + "\n" + strings.Replace(good, "702.4b", "rule 7", 1) + "\n", "cr"},
		{"no reason", ClassHeader + "\n" + strings.Replace(good, "702.4b", "-", 1) + "\n", "neither cr nor upstream"},
		{"empty ticket", ClassHeader + "\n" + strings.TrimSuffix(good, "t1") + "\n", "empty ticket"},
		{"empty matcher", ClassHeader + "\n" + strings.Replace(good, "\t*\t", "\t\t", 1) + "\n", "matcher"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := ReadClasses(writeClasses(t, c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want an error containing %q", err, c.want)
			}
		})
	}
	if rows, err := ReadClasses(writeClasses(t, ClassHeader+"\n"+good+"\n")); err != nil || len(rows) != 1 {
		t.Fatalf("the good row: %v, %v", rows, err)
	}
}

func TestClassRegisterMatch(t *testing.T) {
	s := SigInput{Pattern: "FX|G", Field: "p.life", Family: "activate", Desc: "Mana", Cost: "T", Step: "activate", Follower: "x=diverge,f=diverge"}
	other := s
	other.Desc = "Draw"
	rows := []ClassRow{
		{Sig: "000000000001", Pattern: "GF|X", Field: "*", Desc: "*", Cost: "*", CR: "702.4b", Upstream: "-", Ticket: "a"},
		{Sig: "000000000002", Pattern: "FX|G", Field: "*", Desc: "activate:Mana", Cost: "*", CR: "-", Upstream: "xmage mana", Ticket: "b"},
		{Sig: "000000000003", Pattern: "*", Field: "*", Desc: "*", Cost: "*", CR: "702.4b", Upstream: "-", Ticket: "c"},
	}
	// Wildcard match, in file order: row 2 matches s before the catch-all row 3.
	if r, ok := Match(rows, s); !ok || r.Ticket != "b" {
		t.Errorf("wildcard match: %+v, %v; want row b", r, ok)
	}
	// Row 2 does not match Draw: the catch-all does (first row of the matching ones).
	if r, ok := Match(rows, other); !ok || r.Ticket != "c" {
		t.Errorf("fall through: %+v, %v; want row c", r, ok)
	}
	// A row matches by its own signature whatever its matcher says.
	bySig := []ClassRow{{Sig: KeySignature(s), Pattern: "GF|X", Field: "x", Desc: "x", Cost: "x", CR: "702.4b", Upstream: "-", Ticket: "d"}}
	if r, ok := Match(bySig, s); !ok || r.Ticket != "d" {
		t.Errorf("sig match: %+v, %v", r, ok)
	}
	if _, ok := Match(bySig, other); ok {
		t.Error("a row matched a signature that is neither its own nor in its matcher")
	}
	if _, ok := Match(nil, s); ok {
		t.Error("no rows matched")
	}
}

func TestSignatureCardIndependent(t *testing.T) {
	mk := func(card, fieldClassOf string) Finding {
		return Finding{Row: Row{ID: "xe:" + card, Card: card, Pattern: "FX|G", XClass: "diverge", FClass: "diverge"}, FieldClassOf: fieldClassOf}
	}
	k := KeyDesc{Family: "activate", Desc: "Mana", Cost: "Forage+T"}
	a := KeySignature(InputOf(mk("Llanowar Elves", "p.life"), k, "activate"))
	b := KeySignature(InputOf(mk("Birds of Paradise", "p.life"), k, "activate"))
	if a != b || !sigRE.MatchString(a) {
		t.Errorf("two cards: %s vs %s, want one 12-hex signature", a, b)
	}
	if c := KeySignature(InputOf(mk("Llanowar Elves", "p.hand"), k, "activate")); c == a {
		t.Error("a different field class gave the same signature")
	}
	if c := KeySignature(InputOf(mk("Llanowar Elves", "p.life"), KeyDesc{Family: "activate", Desc: "Mana", Cost: "T"}, "activate")); c == a {
		t.Error("a different cost gave the same signature")
	}
	f := mk("Llanowar Elves", "p.life")
	f.FClass = "agree"
	if c := KeySignature(InputOf(f, k, "activate")); c == a {
		t.Error("a different follower mix gave the same signature")
	}
}
