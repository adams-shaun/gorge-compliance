package fshadow

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// updateGolden rewrites testdata/vanilla-2seat.jsonl from writeGolden. The
// fixture stands in for a Forge-written log until forge-oracle's recorder
// lands (design section 9 ticket 2): it is the line format of section 3, with
// Forge-style ids (100*(seat+1)+index) that differ from gorge's ObjIDs so the
// (seat, decklist index) binding is exercised, and snapshots in the oracle
// shape. Regenerate only when the format changes:
//
//	go test ./oracle/xengine/fshadow -run '^TestWriteGolden$' -update-golden
var updateGolden = flag.Bool("update-golden", false, "rewrite the fshadow golden fixture")

const goldenPath = "testdata/vanilla-2seat.jsonl"

func TestWriteGolden(t *testing.T) {
	if !*updateGolden {
		t.Skip("-update-golden not set")
	}
	reg := corpus(t)
	l := writeGolden(t, reg)
	var b bytes.Buffer
	if err := Write(&b, l); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.FromSlash(goldenPath), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// goldenDeck is each seat's 9-card vanilla deck: two cards left after the
// opening hand, so the game ends by an empty-library draw within a few turns.
var goldenDeck = []string{"Forest", "Grizzly Bears", "Forest", "Grizzly Bears", "Forest", "Grizzly Bears", "Forest", "Grizzly Bears", "Forest"}

// writeGolden plays a scripted 2-seat game in gorge and writes what a Forge
// recorder would have: seat 1 starts, seat 0 mulligans once and bottoms one
// card, each seat plays a land, then casts a Grizzly Bears paying with the
// first plan, attacks with everything and blocks with its first option.
func writeGolden(t *testing.T, reg *cards.Registry) *Log {
	t.Helper()
	const start = 1
	h := Header{T: THeader, Format: Format, Seed: 7, ForgeRef: "golden", GameFormat: "constructed", Mulligans: 1, MaxTurns: 60, MaxMS: 600000}
	toF := map[state.ObjID]int{}
	cfg := rules.Config{Seed: h.Seed, Tokens: reg.Tokens, NameUniverse: reg.Universe(), Mulligans: h.Mulligans}
	next := state.ObjID(1)
	for s := 0; s < 2; s++ {
		seat := Seat{Seat: s, Name: fmt.Sprintf("vanilla-%d", s)}
		var deck []*cards.Card
		for k, n := range goldenDeck {
			c, ok := reg.Lookup(n)
			if !ok {
				t.Fatalf("%q not in the corpus", n)
			}
			id := 100*(s+1) + k
			seat.Cards = append(seat.Cards, Card{ID: id, Name: n})
			toF[next] = id
			next++
			deck = append(deck, c)
		}
		h.Seats = append(h.Seats, seat)
		cfg.Names = append(cfg.Names, seat.Name)
		cfg.Decks = append(cfg.Decks, deck)
	}
	l := &Log{Header: h}
	add := func(e Entry) { l.Entries = append(l.Entries, e) }
	fids := func(ids []state.ObjID) []int {
		out := make([]int, len(ids))
		for i, id := range ids {
			out[i] = toF[id]
		}
		return out
	}
	lcg := uint64(12345)
	shuffle := func(c rules.ShuffleContext) ([]state.ObjID, error) {
		out := make([]state.ObjID, len(c.Library))
		for i, sc := range c.Library {
			out[i] = sc.ID
		}
		for i := len(out) - 1; i > 0; i-- {
			lcg = lcg*6364136223846793005 + 1442695040888963407
			j := int((lcg >> 33) % uint64(i+1))
			out[i], out[j] = out[j], out[i]
		}
		add(Entry{T: TShuffle, Shuffle: &Shuffle{T: TShuffle, Seat: int(c.Player), Order: fids(out)}})
		return out, nil
	}
	chance := func(c rules.ChanceContext) (int, error) {
		if c.Ordinal == 0 {
			add(Entry{T: TStart, Start: &Start{T: TStart, Seat: start}})
			return start, nil
		}
		return 0, fmt.Errorf("the golden game draws no chance past the toss")
	}
	e, err := rules.NewHypotheticalChancePlanned(cfg, nil, shuffle, chance)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	asks, snaps := 0, 0
	lastTurn, lastStep := int32(-1), ""
	mulled := map[state.PlayerID]bool{}
	var keep *Mull
	for n := 0; n < 2000 && !e.G.Over; n++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if e.G.Turn >= 1 && len(e.G.Stack) == 0 && (e.G.Turn != lastTurn || e.G.Step.String() != lastStep) {
			lastTurn, lastStep = e.G.Turn, e.G.Step.String()
			snaps++
			add(Entry{T: TSnap, Snap: &Snap{T: TSnap, N: snaps, Snapshot: Snapshot(e, fmt.Sprintf("snap %d", snaps))}})
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player}
		ask := func(kind string, opts []Option, ans Answer) {
			asks++
			add(Entry{T: TAsk, Ask: &Ask{T: TAsk, N: asks, Seat: int(d.Acting()), Turn: int(e.G.Turn), Step: e.G.Step.String(), Kind: kind, Options: opts, Answer: ans}})
		}
		switch d.Kind {
		case decision.KMulligan:
			if d.Options[0].Kind == "bottom" {
				for i := 0; i < d.Min; i++ {
					in.Choices = append(in.Choices, i)
					keep.Bottom = append(keep.Bottom, toF[d.Options[i].Obj])
				}
				add(Entry{T: TMull, Mull: keep})
				keep = nil
				break
			}
			if d.Player == 0 && !mulled[0] {
				mulled[0] = true
				add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: 0, Keep: false}})
				in.Choices = []int{indexOf(d, "mulligan")}
				break
			}
			in.Choices = []int{indexOf(d, "keep")}
			m := &Mull{T: TMull, Seat: int(d.Player), Keep: true}
			if mulled[d.Player] {
				keep = m // its bottom comes with the next ask
			} else {
				add(Entry{T: TMull, Mull: m})
			}
		case decision.KPriority:
			var opts []Option
			land := -1
			for i, o := range d.Options {
				if o.Kind == "play_land" {
					opts = append(opts, Option{Action: "land", Card: toF[o.Obj]})
					if land < 0 {
						land = i
					}
				}
			}
			acts := e.EnsurePaymentPlanAlternatives()
			for _, a := range acts {
				opts = append(opts, Option{Action: "cast", Card: toF[a.Cast.Object]})
			}
			switch {
			case land >= 0:
				in.Choices = []int{land}
				ask(KindPriority, opts, Answer{Action: "land", Card: toF[d.Options[land].Obj]})
			case len(acts) > 0 && len(acts[0].Plans) > 0:
				p := acts[0].Plans[0]
				var tapped []state.ObjID
				for _, x := range p.Activations {
					tapped = append(tapped, x.Source)
				}
				in.Payment = &decision.PaymentSelection{ActionID: acts[0].ID, Plan: decision.ClonePaymentPlan(p)}
				ask(KindPriority, opts, Answer{Action: "cast", Card: toF[acts[0].Cast.Object], Tapped: fids(tapped)})
			default:
				in.Choices = []int{indexOf(d, "pass")}
				ask(KindPriority, opts, Answer{Action: "pass"})
			}
		case decision.KAttackers:
			var opts []Option
			var ans Answer
			in.Choices = []int{}
			for i, o := range d.Options {
				p := int(o.Player)
				opts = append(opts, Option{Card: toF[o.Obj], Player: &p})
				in.Choices = append(in.Choices, i)
				ans.Attacks = append(ans.Attacks, Attack{Card: toF[o.Obj], Defender: p})
			}
			ask(KindAttack, opts, ans)
		case decision.KBlockers:
			var opts []Option
			var ans Answer
			in.Choices = []int{}
			for _, o := range d.Options {
				opts = append(opts, Option{Card: toF[o.Obj], Attacker: toF[o.Attacker]})
			}
			if len(d.Options) > 0 {
				o := d.Options[0]
				in.Choices = []int{0}
				ans.Blocks = []Block{{Card: toF[o.Obj], Attacker: toF[o.Attacker]}}
			}
			ask(KindBlock, opts, ans)
		default:
			t.Fatalf("the golden game poses %s", d.Kind)
		}
		if err := e.SubmitHypothetical(in); err != nil {
			t.Fatalf("decision %d (%s): %v", n, d.Kind, err)
		}
		if err := e.AdvanceHypothetical(); err != nil {
			t.Fatal(err)
		}
	}
	if !e.G.Over {
		t.Fatal("the golden game did not end")
	}
	end := &End{T: TEnd, Reason: "empty library"}
	if !e.G.Draw {
		w := int(e.G.Winner)
		end.Winner = &w
	}
	add(Entry{T: TEnd, End: end})
	return l
}

func indexOf(d *decision.Decision, kind string) int {
	for i, o := range d.Options {
		if o.Kind == kind {
			return i
		}
	}
	return -1
}
