package templates

import (
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// censusPrintedCards returns the declared sets in sorted order and each set's
// printed cards folded to corpus names, deduplicated -- the iteration the
// mechanism censuses below share with TestShuffleEmittedItemCensus.
func censusPrintedCards(t *testing.T) ([]string, map[string][]string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..")
	declared, err := compliance.LoadDeclared(filepath.Join(root, "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	sets := make([]string, 0, len(declared))
	for set := range declared {
		sets = append(sets, set)
	}
	sort.Strings(sets)
	folded := compliance.FoldedNames(reg)
	has := func(name string) bool { _, ok := reg.Lookup(name); return ok }
	out := make(map[string][]string, len(sets))
	for _, set := range sets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "printed"), set)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, name := range printed.Cards {
			name, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				t.Fatalf("%s: %q absent from corpus", set, name)
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out[set] = append(out[set], name)
		}
	}
	return sets, out
}

// TestOptionalCostAnswerCensus: every level-B item whose cast step carries
// cast_mode "optionalcost" scripts XMage's "pay it?" chooseUse as a leading
// "yes" -- the answer ScenarioReplay.optionalAdditionalCostAnswer consumes
// (ticket lb-xoptcost rows 138/182). The counts are the corpus-wide inventory
// of the mechanism; a card that starts or stops scripting the yes, or a set
// whose served rows move, must be re-measured and re-pinned here.
// ECL 5: Celestial Reunion's compound "choose a creature type and behold
// two" joined the served rows (lb5-costgrammar).
var wantOptionalCostAnswerCensus = map[string]int{
	"BIG": 0, "BLB": 0, "DFT": 0, "DSK": 0, "ECL": 5,
	"EOE": 0, "FDN": 0, "FIN": 0, "FRA": 0, "HOB": 0,
	"LCI": 0, "MKM": 8, "MSH": 0, "OTJ": 0, "SOS": 0,
	"SPM": 0, "TDM": 3, "TLA": 3, "TMT": 0, "WOE": 0,
}

func TestOptionalCostAnswerCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sets, bySet := censusPrintedCards(t)
	got := make(map[string]int, len(sets))
	for _, set := range sets {
		got[set] = 0
		for _, name := range bySet[set] {
			card, _ := reg.Lookup(name)
			for _, req := range levelb.Requirements(card) {
				if req.Gap != "" || (req.Sub != "static.optional-cost" && req.Sub != "static.cost") {
					continue
				}
				it, skip := GenerateB(reg, name, req)
				if skip != nil {
					continue
				}
				for i, s := range it.Steps {
					if s.CastMode != optionalCostCastMode {
						continue
					}
					got[set]++
					t.Logf("%s %s", set, it.ID)
					if i >= len(it.XAnswers) || len(it.XAnswers[i]) == 0 {
						t.Fatalf("%s %s: optionalcost cast step scripts no answer", name, req.Key)
					}
					if lead := it.XAnswers[i][0]; lead.Kind != "choice" || lead.Value != "yes" {
						t.Fatalf("%s %s: optionalcost cast lead = %+v, want choice yes", name, req.Key, lead)
					}
				}
			}
		}
	}
	if !reflect.DeepEqual(got, wantOptionalCostAnswerCensus) {
		t.Fatalf("optionalcost answer census changed: got %#v, want %#v", got, wantOptionalCostAnswerCensus)
	}
}

// TestCraftMaterialAnswerCensus: every Craft activation scripts one target
// answer per material the cost names (the count ScenarioReplay's fill loop
// keeps consuming from the target queue; ticket lb-xoptcost row 140). The
// counts are the corpus-wide inventory of the mechanism.
var wantCraftMaterialAnswerCensus = map[string]int{
	"BIG": 0, "BLB": 0, "DFT": 0, "DSK": 0, "ECL": 0,
	"EOE": 0, "FDN": 0, "FIN": 0, "FRA": 0, "HOB": 0,
	"LCI": 19, "MKM": 0, "MSH": 0, "OTJ": 0, "SOS": 0,
	"SPM": 0, "TDM": 0, "TLA": 0, "TMT": 0, "WOE": 0,
}

func TestCraftMaterialAnswerCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sets, bySet := censusPrintedCards(t)
	got := make(map[string]int, len(sets))
	for _, set := range sets {
		got[set] = 0
		for _, name := range bySet[set] {
			card, _ := reg.Lookup(name)
			for _, req := range levelb.Requirements(card) {
				if req.Gap != "" || req.Family != "activate" {
					continue
				}
				ai, err := strconv.Atoi(req.Slot)
				if err != nil {
					t.Fatalf("%s: activate slot %q is not an index", req.Key, req.Slot)
				}
				cost := card.Faces[req.Face].Abilities[ai].ParamStr(cards.PKCost)
				if !craftCost(cost) {
					continue
				}
				it, skip := GenerateB(reg, name, req)
				if skip != nil {
					t.Fatalf("craft row skipped: %s %s: %s", name, req.Key, skip.Reason)
				}
				want := craftMaterialCount(cost)
				for i, s := range it.Steps {
					if s.Op != "activate" || i >= len(it.XAbility) || !strings.Contains(it.XAbility[i], "Craft with") {
						continue
					}
					got[set]++
					t.Logf("%s %s %s", set, it.ID, it.XAbility[i])
					answers := 0
					for _, a := range it.XAnswers[i] {
						if a.Kind != "target" {
							t.Fatalf("%s %s: craft material step scripts a %s answer: %+v", name, req.Key, a.Kind, a)
						}
						answers++
					}
					if answers != want {
						t.Fatalf("%s %s: craft scripts %d target answers, cost names %d materials", name, req.Key, answers, want)
					}
				}
			}
		}
	}
	if !reflect.DeepEqual(got, wantCraftMaterialAnswerCensus) {
		t.Fatalf("craft material answer census changed: got %#v, want %#v", got, wantCraftMaterialAnswerCensus)
	}
}

// craftMaterialCount is the number of materials the Craft cost names: the
// literal count of each ExileCtrlOrGrave token, or X's XMin value.
func craftMaterialCount(cost string) int {
	n := 0
	x := activationX(cost)
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "ExileCtrlOrGrave") {
			continue
		}
		payload, _ := bracketPayload(tok)
		count, _, _ := strings.Cut(payload, "/")
		if count == "X" {
			n += x
			continue
		}
		if v, err := strconv.Atoi(count); err == nil {
			n += v
		}
	}
	return n
}

// TestSolvedCasePrefixCensus: every solved Case line the corpus prints maps to
// an XMage prefix that keeps the card's own "Sacrifice this Case" cost text
// (XMage's Case abilities override SacrificeSourceCost's "sacrifice {this}";
// ticket lb-xoptcost row 139). The counts are the corpus-wide inventory of the
// mechanism.
var wantSolvedCasePrefixCensus = map[string]int{
	"BIG": 0, "BLB": 0, "DFT": 0, "DSK": 0, "ECL": 0,
	"EOE": 0, "FDN": 0, "FIN": 0, "FRA": 0, "HOB": 0,
	"LCI": 0, "MKM": 4, "MSH": 0, "OTJ": 0, "SOS": 0,
	"SPM": 0, "TDM": 0, "TLA": 0, "TMT": 0, "WOE": 0,
}

func TestSolvedCasePrefixCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sets, bySet := censusPrintedCards(t)
	got := make(map[string]int, len(sets))
	for _, set := range sets {
		got[set] = 0
		for _, name := range bySet[set] {
			card, _ := reg.Lookup(name)
			for fi := range card.Faces {
				f := card.Faces[fi]
				if !strings.Contains(f.Oracle, "Solved — ") {
					continue
				}
				prefixes, why := oraclegen.XMageAbility(f)
				if why != "" {
					continue
				}
				solved := 0
				for _, prefix := range prefixes {
					if !strings.Contains(prefix, "Solved") {
						continue
					}
					solved++
					if !strings.Contains(prefix, "Sacrifice this Case") || strings.Contains(prefix, "Sacrifice {this}") {
						t.Fatalf("%s: solved prefix %q lost the Case spelling", name, prefix)
					}
				}
				if solved > 0 {
					got[set]++
					t.Logf("%s %s", set, name)
				}
			}
		}
	}
	if !reflect.DeepEqual(got, wantSolvedCasePrefixCensus) {
		t.Fatalf("solved case prefix census changed: got %#v, want %#v", got, wantSolvedCasePrefixCensus)
	}
}
