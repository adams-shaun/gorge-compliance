package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
)

// Files certdeck writes under -out: the two decks of `play -decks`.
const (
	certdeckSelfFile = "target.json"
	certdeckOppFile  = "opponent.json"
)

// runCertdeck is `xengine certdeck`: the two decks that certify one card, the
// target's own (four copies, or one if legendary, up to 12 enabler slots, a
// colour-identity mana base and filler in its colours) and an opponent deck that
// supplies what the card needs from the other side. -out then holds exactly
// target.json and opponent.json, so `xengine play -decks DIR` plays them
// against each other. The decks are built at run time from -cards and the
// XMage manifests; the same corpus and target give byte-identical files.
func runCertdeck(args []string) error {
	fs := flag.NewFlagSet("certdeck", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	manifests := fs.String("manifests", "oracle/compliance/manifests", "set manifests dir (the cards XMage lists, under the spelling it uses)")
	target := fs.String("target", "", "the card to certify (required)")
	need := fs.String("need", "auto", "auto, or comma-separated classes (class or class:arg): snow, type:Elf, counters[:P1P1], token[:treasure], graveyard, equipment, aura, bodies, life[:gain|total], artifacts, enchantments, lands, landtype:Forest, banding, opp-attackers, opp-blockers, opp-targets:creature|artifact|enchantment|planeswalker|land|spell, opp-type:Elf, opp-landtype:Island, opp-burn")
	out := fs.String("out", "", "output dir (required): target.json and opponent.json")
	fs.Parse(args)
	if *target == "" || *out == "" {
		return errors.New("certdeck needs -target and -out")
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	sp, err := decks.LoadSpellings(*manifests)
	if err != nil {
		return err
	}
	b := certdeck.NewBuilder(reg, sp)
	tc, ok := reg.Lookup(*target)
	if !ok || len(tc.Faces) == 0 {
		return fmt.Errorf("certdeck: %q is not in the corpus", *target)
	}
	needs, err := certdeck.ParseNeeds(*need, tc, b.Types())
	if err != nil {
		return err
	}
	plan, err := b.Build(*target, needs)
	if err != nil {
		return err
	}
	if err := writeCertdeckPlan(*out, plan); err != nil {
		return err
	}
	fmt.Printf("certdeck %s: needs %v; target deck %s, opponent deck %s -> %s\n",
		plan.Target, plan.Needs, plan.Self.Colours, plan.Opponent.Colours, *out)
	for _, d := range []struct {
		side string
		p    map[string][]string
	}{{"target", plan.Self.Provided}, {"opponent", plan.Opponent.Provided}} {
		for _, n := range plan.Needs {
			if certdeck.OppSide(n.Class) != (d.side == "opponent") {
				continue
			}
			if got := d.p[n.String()]; len(got) > 0 {
				fmt.Printf("  %s %s: %v\n", d.side, n, got)
			} else {
				fmt.Printf("  %s %s: UNMET (no playable card in the deck's colours supplies it)\n", d.side, n)
			}
		}
	}
	return nil
}

// writeCertdeckPlan writes plan's two decks to dir (target.json, opponent.json).
func writeCertdeckPlan(dir string, plan *certdeck.Plan) error {
	for _, w := range []struct {
		file string
		d    certdeck.Deck
	}{{certdeckSelfFile, plan.Self}, {certdeckOppFile, plan.Opponent}} {
		raw, err := json.MarshalIndent(w.d.File, "", "  ")
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(dir, w.file), append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}
