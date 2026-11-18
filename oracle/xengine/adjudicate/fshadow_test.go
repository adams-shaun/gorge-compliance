package adjudicate

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// fshadowFixture writes synthetic fshadow findings, a tiny manifest TSV and
// its decklists (never the private ones), and a registry of synthetic cards.
type fshadowFixture struct {
	dir, manifest, decklists string
	reg                      *cards.Registry
}

func newFShadowFixture(t *testing.T, findings []Divergence) fshadowFixture {
	t.Helper()
	dir := t.TempDir()
	fx := fshadowFixture{
		dir:       filepath.Join(dir, "out"),
		manifest:  filepath.Join(dir, "edh-manifest.tsv"),
		decklists: filepath.Join(dir, "edh-decks.jsonl"),
		reg:       cards.NewRegistry(),
	}
	for i, d := range findings {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(fx.dir, d.Game, "divergence-"+string(rune('0'+i))+".json")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Three manifest decks hold the cards below; deck-x is in the decklists
	// but not the manifest, so it never counts.
	tsv := "source\tid\tkind\tname\tcommander\tbracket\toverall_rank\tbracket_rank\n" +
		"mtgjson\tdeck-a\tprecon\tDeck A\tCmdr A\t\t\t\n" +
		"mtgjson\tdeck-b\tprecon\tDeck B\tCmdr B\t\t\t\n" +
		"archidekt\tdeck-c\tpopular\tDeck C\tCmdr C\t2\t1\t1\n"
	lists := []string{
		`{"source":"mtgjson","id":"deck-a","kind":"precon","commanders":["Cmdr A"],"cards":["Alpha Pumper","Paper Drawer","A-Digital Drawer"]}`,
		`{"source":"mtgjson","id":"deck-b","kind":"precon","commanders":["Cmdr B"],"cards":["Beta Pumper","Paper Drawer","A-Digital Drawer"]}`,
		`{"source":"archidekt","id":"deck-c","kind":"popular","commanders":["Cmdr C"],"cards":["Alpha Pumper","Beta Pumper"]}`,
		`{"source":"archidekt","id":"deck-x","kind":"popular","commanders":["Cmdr X"],"cards":["Paper Drawer","A-Digital Drawer"]}`,
	}
	if err := os.WriteFile(fx.manifest, []byte(tsv), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fx.decklists, []byte(strings.Join(lists, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, api string }{
		{"Alpha Pumper", "Pump"}, {"Beta Pumper", "Pump"},
		{"Paper Drawer", "Draw"}, {"A-Digital Drawer", "Draw"},
	} {
		fx.reg.Add(&cards.Card{Faces: []*cards.Face{{
			Name: c.name, Types: []string{"Creature"},
			Abilities: []*cards.SA{{Kind: "AB", API: "Dig"}, {Kind: "AB", API: c.api}},
		}}})
	}
	return fx
}

func (fx fshadowFixture) run(t *testing.T) Result {
	t.Helper()
	divs, err := LoadFShadow(fx.dir)
	if err != nil {
		t.Fatal(err)
	}
	decks, err := LoadDeckCounts(fx.manifest, fx.decklists)
	if err != nil {
		t.Fatal(err)
	}
	return Run(Input{FShadow: divs, Rank: Ranker{
		Primitive: RegistryPrimitive(fx.reg),
		Decks:     decks,
		Paper:     RegistryPaper(fx.reg, nil),
	}})
}

func ability(i int) *int { return &i }

func syntheticDivergence(game string, offset int64, card string, abilityIndex int) Divergence {
	return Divergence{
		Schema: DivergenceSchema, Game: game, Seed: 7, Decks: []string{"deck-a", "deck-b", "deck-c", "deck-a"},
		Log: game + ".jsonl", Offset: offset, Pair: "F|G", What: "snapshot", Kind: "priority",
		Field: "Battlefield[3].Power", Gorge: "2", Other: "3",
		Cards: []Implicated{{Name: card, Ability: ability(abilityIndex)}},
	}
}

// TestFShadowCensusOnePrimitiveOneBrief is the ticket's Repro: two findings on
// two different cards whose implicated ability is the same IR primitive
// (api:Pump) are two signatures of one root-cause class, so they produce one
// brief, which lists both.
func TestFShadowCensusOnePrimitiveOneBrief(t *testing.T) {
	fx := newFShadowFixture(t, []Divergence{
		syntheticDivergence("g1", 40, "Alpha Pumper", 1),
		syntheticDivergence("g2", 12, "Beta Pumper", 1),
	})
	res := fx.run(t)
	if res.Census == nil || len(res.Census.Classes) != 1 {
		t.Fatalf("census classes = %+v, want exactly one", res.Census)
	}
	c := res.Census.Classes[0]
	if c.Key != "snapshot|priority|Battlefield[].Power|api:Pump" {
		t.Errorf("class key = %q", c.Key)
	}
	if len(c.Signatures) != 2 {
		t.Fatalf("signatures = %d, want 2", len(c.Signatures))
	}
	for _, s := range c.Signatures {
		if len(s.SHA12) != 12 {
			t.Errorf("signature %q hash %q is not 12 hex", s.Key, s.SHA12)
		}
	}
	if got := c.Signatures[0].Key; got != "F|G|snapshot|priority|Battlefield[].Power|Alpha Pumper" {
		t.Errorf("signature key = %q", got)
	}
	// Alpha Pumper is in deck-a and deck-c, Beta Pumper in deck-b and deck-c:
	// one game each, paper, so 1x2x1 + 1x2x1.
	if c.Score != 4 {
		t.Errorf("class score = %v, want 4", c.Score)
	}
	out := filepath.Join(t.TempDir(), "adj")
	if err := Write(out, res); err != nil {
		t.Fatal(err)
	}
	briefs, _ := filepath.Glob(filepath.Join(out, "tickets", "*.md"))
	if len(briefs) != 1 {
		t.Fatalf("briefs = %v, want one", briefs)
	}
	raw, err := os.ReadFile(briefs[0])
	if err != nil {
		t.Fatal(err)
	}
	brief := string(raw)
	for _, want := range []string{
		"Title: snapshot|priority|Battlefield[].Power|api:Pump: ",
		"Repo: gorge\n",
		"\nSuspect: ",
		"\nRepro first: ",
		"\nEvidence: 2 games, ",
		"Forge log offset 12 (g2.jsonl)",
		"\nCorroboration: ",
		"\nAcceptance: ",
		"\nNot in scope: ",
		c.Signatures[0].SHA12, c.Signatures[1].SHA12,
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief lacks %q:\n%s", want, brief)
		}
	}
	if i, j := strings.Index(brief, "Suspect: "), strings.Index(brief, "Repro first: "); i < 0 || j < i || strings.Index(brief, "Evidence: ") < j {
		t.Errorf("brief does not lead with Suspect then Repro:\n%s", brief)
	}
	if filepath.Base(briefs[0]) != c.SHA12+".md" {
		t.Errorf("brief file %s, want %s.md", briefs[0], c.SHA12)
	}
}

// TestFShadowCensusDigitalRanksTenTimesLower is the ticket's second Repro: the
// same finding on an Alchemy A- card ranks at a tenth of its paper twin.
func TestFShadowCensusDigitalRanksTenTimesLower(t *testing.T) {
	paper := syntheticDivergence("g1", 5, "Paper Drawer", 1)
	digital := syntheticDivergence("g2", 5, "A-Digital Drawer", 1)
	digital.Field = "Hand[0]" // a different class, so each ranks alone
	fx := newFShadowFixture(t, []Divergence{digital, paper})
	res := fx.run(t)
	if len(res.Census.Classes) != 2 {
		t.Fatalf("classes = %d, want 2", len(res.Census.Classes))
	}
	first, second := res.Census.Classes[0], res.Census.Classes[1]
	if first.Signatures[0].Cards[0] != "Paper Drawer" || second.Signatures[0].Cards[0] != "A-Digital Drawer" {
		t.Fatalf("rank order = %s, %s; want the paper card first", first.Key, second.Key)
	}
	if r := first.Score / second.Score; math.Abs(r-10) > 1e-9 {
		t.Errorf("paper/digital score = %v/%v = %v, want 10", first.Score, second.Score, r)
	}
}

// TestFShadowHarnessRankedSeparately: a harness finding (an answer with no
// mapping) is a follower ticket in its own ranking, never a gorge class.
func TestFShadowHarnessRankedSeparately(t *testing.T) {
	h := syntheticDivergence("g1", 9, "Alpha Pumper", 1)
	h.What, h.Harness = "answer-missing", "answer-unmapped"
	fx := newFShadowFixture(t, []Divergence{h, syntheticDivergence("g2", 3, "Beta Pumper", 1)})
	res := fx.run(t)
	if len(res.Census.Classes) != 1 || len(res.Census.Harness) != 1 {
		t.Fatalf("classes %d harness %d, want 1 and 1", len(res.Census.Classes), len(res.Census.Harness))
	}
	out := filepath.Join(t.TempDir(), "adj")
	if err := Write(out, res); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "tickets", res.Census.Harness[0].SHA12+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Repo: gorge-compliance\n") {
		t.Errorf("harness brief is not a follower ticket:\n%s", raw)
	}
	tsv, err := os.ReadFile(filepath.Join(out, "census.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tsv), "\tharness\t") || !strings.Contains(string(tsv), "\tgorge\t") {
		t.Errorf("census.tsv lacks both rankings:\n%s", tsv)
	}
}
