package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestForgeLandPlayFromOutsideHand: the two xproof harness finds (Ka-Zar of
// the Savage Land f0t0 s2, Irascible Wolverine f0t0 s2) play a land that is
// not in the hand: the library top Ka-Zar lets p0 play from, the card the
// Wolverine exiled. CR 305.1 lets an effect permit a land play from another
// zone. The request carries the plain play step and gorge plays the land from
// there; the Forge driver at FORGE_ORACLE_REF (ff2049dc97, fork branch
// fix/land-play-mayplay) plays it through the MayPlay copy of the land ability
// (forge-oracle LandPlayMayPlayTest replays these two requests). Before it the
// driver took the hand-only land ability and wrote "step N (play) p0:<land>
// was not played".
func TestForgeLandPlayFromOutsideHand(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		fixture, land string
		step          int
		from          func(rules.OracleSnapPlayer, string) bool
	}{
		{"land-play-library-top.jsonl", "p0:Forest#8", 10, func(p rules.OracleSnapPlayer, name string) bool {
			return len(p.LibraryTop) > 0 && p.LibraryTop[0] == name
		}},
		{"land-play-exiled.jsonl", "p0:Mountain#4", 8, func(p rules.OracleSnapPlayer, name string) bool {
			for _, e := range p.Exile {
				if strings.HasPrefix(e, name) || strings.Contains(e, ":"+name) {
					return true
				}
			}
			return false
		}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/forge/" + tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			var it oraclegen.Item
			if err := json.Unmarshal(bytes.TrimSpace(raw), &it); err != nil {
				t.Fatal(err)
			}
			rq, g := forgeExportItem(reg, it, true)
			if len(g.Snapshots) == 0 {
				t.Fatalf("gorge wrote no checkpoints: %v", g.Fails)
			}
			steps := rq.Item.Scenario.Steps
			if tc.step >= len(steps) || steps[tc.step].Op != "play" || steps[tc.step].Card != tc.land {
				t.Fatalf("request step %d = %+v, want play %s", tc.step, steps[tc.step], tc.land)
			}
			if tc.step+1 >= len(g.Snapshots) {
				t.Fatalf("no checkpoint after step %d (%d snapshots)", tc.step, len(g.Snapshots))
			}
			name := tc.land[strings.Index(tc.land, ":")+1 : strings.Index(tc.land, "#")]
			before := g.Snapshots[tc.step]
			if snapshotPerm(before, tc.land) != nil {
				t.Fatalf("precondition: %s on the battlefield entering step %d", tc.land, tc.step)
			}
			for _, h := range before.Players[0].Hand {
				if strings.Contains(h, name) {
					t.Fatalf("precondition: a %s in p0's hand entering step %d (%v); the play must come from another zone", name, tc.step, before.Players[0].Hand)
				}
			}
			if !tc.from(before.Players[0], name) {
				t.Fatalf("precondition: %s not where the find plays it from entering step %d: %+v", name, tc.step, before.Players[0])
			}
			if snapshotPerm(g.Snapshots[tc.step+1], tc.land) == nil {
				t.Errorf("gorge: %s not on the battlefield after step %d", tc.land, tc.step)
			}
		})
	}
}
