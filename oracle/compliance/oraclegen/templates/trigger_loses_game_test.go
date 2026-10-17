package templates

// The three-seat LosesGame class (trigger_recipes_loses_game.go): which
// cards of the declared sets it serves, that its item really seats three and
// shows the loss the two-seat game could not, and that it moves no two-seat
// item's bytes.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// losesGameCensus is every Mode$ LosesGame trigger requirement of the
// declared sets' printed cards, with its outcome: "served", or the skip
// reason. A new LosesGame card in a declared set, or a row whose outcome
// moves, fails here and names itself.
var losesGameCensus = map[string]string{
	// Shinryu's Burning Chains: "When the chosen player loses the game, you
	// win the game." The chooser is Shinryu's own "as this transforms,
	// choose an opponent", run by the front face's leaves-the-battlefield
	// transform scenario (trigger#0.1) replayed with three seats.
	"Zenos yae Galvus trigger#1.0": "served",
}

func TestLosesGameThreeSeatCensus(t *testing.T) {
	reg := loadGenRegistry(t)
	declared, err := compliance.EmbeddedDeclared()
	if err != nil {
		t.Fatal(err)
	}
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	sets := make([]string, 0, len(declared))
	for set := range declared {
		sets = append(sets, set)
	}
	sort.Strings(sets)
	got := map[string]string{}
	seen := map[string]bool{}
	for _, set := range sets {
		printed, err := compliance.EmbeddedPrinted(set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok || seen[card] {
				continue
			}
			seen[card] = true
			c, _ := reg.Lookup(card)
			for _, r := range levelb.Requirements(c) {
				if r.Family != "trigger" || !strings.EqualFold(triggerMode(c, r), "LosesGame") {
					continue
				}
				outcome := "served"
				if r.Gap != "" {
					outcome = "gap: " + r.Gap
				} else if _, skip := GenerateB(reg, card, r); skip != nil {
					outcome = skip.Reason
				}
				got[card+" "+r.Key] = outcome
			}
		}
	}
	for k, want := range losesGameCensus {
		if got[k] != want {
			t.Errorf("%s: %q, want %q", k, got[k], want)
		}
	}
	for k, v := range got {
		if _, ok := losesGameCensus[k]; !ok {
			t.Errorf("unlisted LosesGame requirement %s: %q (add it to losesGameCensus)", k, v)
		}
	}
}

// triggerMode is the Mode$ of the trigger requirement r names.
func triggerMode(c *cards.Card, r levelb.Requirement) string {
	f := c.Faces[r.Face]
	for i := range f.Triggers {
		if r.Slot == strconv.Itoa(i) {
			return f.Triggers[i].Mode
		}
	}
	return ""
}

// TestLosesGameThreeSeatZenos: the served item seats three; the Shock's loss
// leaves p1 out of a game that goes on with Burning Chains on the stack (CR
// 800.4a), and its resolution ends the game with p0 the winner (CR 104.2c),
// which the two-seat game, already over at p1's loss, could never show.
func TestLosesGameThreeSeatZenos(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Zenos yae Galvus", "trigger#1.0", levelb.LosesGameSub)
	if it.Players != oraclegen.ThreeSeats || !strings.Contains(string(it.Raw()), `"players":3`) {
		t.Fatalf("item seats %d, want %d: %s", it.Players, oraclegen.ThreeSeats, it.Raw())
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok {
		t.Fatalf("does not play through gorge: %v", res.Fails)
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	if !last.Over || last.Winner == nil || *last.Winner != 0 || len(last.Players) != 3 {
		t.Fatalf("final checkpoint: over %v winner %v seats %d; want p0 to win a three-seat game", last.Over, last.Winner, len(last.Players))
	}
	c, _ := reg.Lookup("Zenos yae Galvus")
	back := c.Faces[1]
	probe := it.Scenario
	probe.Steps = append(append([]oraclegen.Step(nil), it.Steps[:len(it.Steps)-1]...),
		oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1}, oraclegen.Step{Op: "pass", Seat: 2})
	// The probe stops with the trigger on the stack, so it never settles:
	// only its checkpoints are read.
	pres, _ := oraclegen.ProbeTargets(reg, probe)
	if len(pres.Snapshots) == 0 {
		t.Fatalf("probe made no checkpoint: %v", pres.Fails)
	}
	mid := pres.Snapshots[len(pres.Snapshots)-1]
	if mid.Over || !mid.Players[1].Lost || mid.Players[2].Lost {
		t.Fatalf("after p1's loss: over %v lost p1 %v p2 %v; want the game to go on without p1", mid.Over, mid.Players[1].Lost, mid.Players[2].Lost)
	}
	if !abilityOnStack(pres.Snapshots, stackSourceWants(reg, "Zenos yae Galvus", back), "0") {
		t.Fatalf("Burning Chains is not on the stack after p1's loss")
	}
}

// TestTwoSeatItemShaUnchanged: the seat-count field is omitempty, so a
// two-seat item's bytes, and the scenario sha its verdict row records, are
// what they were before the field existed. Zenos's own two-seat rows keep
// their committed shas (oracle/compliance/verdicts/z.jsonl).
func TestTwoSeatItemShaUnchanged(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ key, sha string }{
		{"combat#1.attack", "3d844f3558666ee65676fbec86d930c78cc9d7f2824334bb37240421bf07c2f2"},
		{"combat#1.block", "ab52e15f22ed2204d809d219754dae70e0ef6de7cbdc4286ef57f137dd8b38b9"},
	} {
		c, _ := reg.Lookup("Zenos yae Galvus")
		var it oraclegen.Item
		for _, r := range levelb.Requirements(c) {
			if r.Key == tc.key {
				var skip *oraclegen.Skip
				if it, skip = GenerateB(reg, "Zenos yae Galvus", r); skip != nil {
					t.Fatalf("%s skipped: %s", tc.key, skip.Reason)
				}
			}
		}
		b, _ := json.Marshal(it)
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != tc.sha || strings.Contains(string(b), `"players"`) {
			t.Fatalf("%s: sha %s, want %s (bytes %s)", tc.key, got, tc.sha, b)
		}
	}
}
