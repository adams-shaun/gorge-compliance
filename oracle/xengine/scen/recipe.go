package scen

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// basicOf maps a colour letter to its basic land; C is Wastes.
var basicOf = map[string]string{"W": "Plains", "U": "Island", "B": "Swamp", "R": "Mountain", "G": "Forest", "C": "Wastes"}

var manaTokenRE = regexp.MustCompile(`^([0-9]+|[XWUBRGCS]|[WUBRGC2]/[WUBRGCP])$`)

// firstColour is the first coloured symbol of a mana cost, in cost order
// (hybrid: its first colour); "" for a colourless cost.
func firstColour(cost string) string {
	for _, tok := range strings.Fields(cost) {
		for _, part := range strings.Split(tok, "/") {
			if _, ok := basicOf[part]; ok && part != "C" {
				return part
			}
		}
	}
	return ""
}

// basicFor is the basic land of a card's first colour (Plains when colourless).
func basicFor(cost string) string {
	if c := firstColour(cost); c != "" {
		return basicOf[c]
	}
	return "Plains"
}

// landsFor is one basic per coloured symbol of the mana tokens (hybrid: its
// first colour), generic N as N basics of generic, X as none.
func landsFor(manaTokens []string, generic string) []string {
	var out []string
	for _, tok := range manaTokens {
		if n, err := strconv.Atoi(tok); err == nil {
			for i := 0; i < n; i++ {
				out = append(out, generic)
			}
			continue
		}
		if tok == "X" || tok == "S" {
			continue
		}
		parts := strings.Split(tok, "/")
		if parts[0] == "2" && len(parts) == 2 { // {2/W}: pay the colour
			parts = parts[1:]
		}
		if b, ok := basicOf[parts[0]]; ok {
			out = append(out, b)
		}
	}
	return out
}

// splitCost splits a Forge cost into space-separated tokens, keeping a
// <...> argument (which may hold spaces) inside its token.
func splitCost(cost string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range cost {
		switch {
		case r == '<':
			depth++
		case r == '>':
			depth--
		}
		if r == ' ' && depth == 0 {
			if start >= 0 {
				out = append(out, cost[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, cost[start:])
	}
	return out
}

var payLifeRE = regexp.MustCompile(`^PayLife<[0-9]+>$`)

// costTokens splits a cost into its mana tokens and its other tokens, refusing
// the first other token no start setup can pay.
func costTokens(cost string) (mana []string, refusal string) {
	for _, tok := range splitCost(cost) {
		switch {
		case manaTokenRE.MatchString(tok):
			mana = append(mana, tok)
		case tok == "T" || tok == "Q" || tok == "Sac<1/CARDNAME>" || payLifeRE.MatchString(tok):
		default:
			if i := strings.IndexByte(tok, '<'); i >= 0 {
				tok = tok[:i]
			}
			return nil, "steps-needed:cost:" + tok
		}
	}
	return mana, ""
}

// asEnters reports an as-enters choice on face: a replacement of the card's own
// move to the battlefield whose replacement effect is a Choose* API.
func asEnters(f *cards.Face) bool {
	for _, r := range f.Repls {
		if r.Event != "Moved" || !strings.Contains(r.Params["ValidCard"], "Card.Self") || r.Params["Destination"] != "Battlefield" {
			continue
		}
		if r.With != nil && strings.HasPrefix(r.With.API, "Choose") {
			return true
		}
	}
	return false
}

// selfZone reports whether a ChangesZone trigger is about the card itself.
func selfCard(t *cards.Trigger) bool { return strings.Contains(t.Params["ValidCard"], "Card.Self") }

func newStart(card, key string) *StartFile {
	return &StartFile{Format: StartFormat, Card: card, Key: key, Players: 2, MaxTurns: 1, Filler: "basics", Seeds: 2}
}

func seat(n int) *int { return &n }

// recipe is the start-file path: the placements by key family, then the needs.
func recipe(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, key, fam string, m []string,
	sa *cards.SA, trig *cards.Trigger, opt Options) (Result, error) {
	face := c.Faces[0]
	f := newStart(card, key)
	p0, p1 := &f.Setup.P0, &f.Setup.P1
	generic := basicFor(face.ManaCost)
	onBF := func() (Result, bool) { // the checks of every recipe that places the card on the battlefield
		if asEnters(face) {
			r, _ := refuse("as-enters")
			return r, true
		}
		return Result{}, false
	}
	switch fam {
	case "cast":
		p0.Hand = []string{card}
		p0.Battlefield = landsFor(splitCost(face.ManaCost), generic)
		f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "cast"}
	case "land":
		p0.Hand = []string{card}
		f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "land"}
	case "static", "combat":
		if r, ok := onBF(); ok {
			return r, nil
		}
		return refuse("no-generator:" + fam)
	case "activate", "trigger":
		if m[1] != "0" {
			return refuse("no-generator:face")
		}
		if r, ok := onBF(); ok {
			return r, nil
		}
		idx, _ := strconv.Atoi(m[3])
		if fam == "activate" {
			mana, why := costTokens(sa.Params["Cost"])
			if why != "" {
				return refuse(why)
			}
			p0.Battlefield = append([]string{card}, landsFor(mana, generic)...)
			f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "ability", Ability: idx}
			break
		}
		switch trig.Mode {
		case "ChangesZone":
			switch {
			case trig.Params["Destination"] == "Battlefield" && selfCard(trig):
				// An ETB is a cast: the card is not on the battlefield yet.
				p0.Hand = []string{card}
				p0.Battlefield = landsFor(splitCost(face.ManaCost), generic)
				f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "cast"}
			case trig.Params["Origin"] == "Battlefield" && trig.Params["Destination"] == "Graveyard" && selfCard(trig):
				murder, ok := reg.Lookup("Murder")
				if !ok || len(murder.Faces) == 0 {
					return Result{}, fmt.Errorf("scen: Murder is not in the corpus")
				}
				mc := murder.Faces[0].ManaCost
				p0.Battlefield = []string{card}
				p1.Hand = []string{"Murder"}
				p1.Battlefield = landsFor(splitCost(mc), basicFor(mc))
				f.Active = 1
				f.Focus = &StartFocus{Seat: seat(1), Card: "Murder", Kind: "cast"}
			default:
				return refuse("no-generator:trigger:ChangesZone")
			}
		case "Attacks":
			p0.Battlefield = []string{card}
			f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "attack"}
		case "Phase":
			if trig.Params["Phase"] != "Upkeep" {
				return refuse("no-generator:trigger:Phase")
			}
			p0.Battlefield = []string{card}
			f.Active, f.MaxTurns = 1, 2
		default:
			return refuse("no-generator:trigger:" + trig.Mode)
		}
	default:
		return refuse("no-generator")
	}
	if why, err := placeNeeds(b, c, card, generic, f); err != nil || why != "" {
		if err != nil {
			return Result{}, err
		}
		return refuse(why)
	}
	return Result{Path: PathStart, Start: f}, nil
}

// placeNeeds adds what certdeck.Derive says the card reads: graveyard fodder,
// and the first enabler certdeck picks for each other need. A need StartSpec
// cannot place (tokens, counters) is a refusal.
func placeNeeds(b *certdeck.Builder, c *cards.Card, card, generic string, f *StartFile) (string, error) {
	needs := certdeck.Derive(c, b.Types())
	if len(needs) == 0 {
		return "", nil
	}
	for _, n := range needs {
		if n.Class == certdeck.Token || n.Class == certdeck.Counters {
			return "steps-needed:" + n.Class, nil
		}
	}
	plan, err := b.Build(card, needs)
	if err != nil {
		return "", err
	}
	for _, n := range needs {
		switch {
		case n.Class == certdeck.Graveyard:
			for i := 0; i < 7; i++ {
				f.Setup.P0.Graveyard = append(f.Setup.P0.Graveyard, generic)
			}
		case certdeck.OppSide(n.Class):
			if got := plan.Opponent.Provided[n.String()]; len(got) > 0 {
				f.Setup.P1.Battlefield = append(f.Setup.P1.Battlefield, got[0])
			}
		default:
			if got := plan.Self.Provided[n.String()]; len(got) > 0 {
				f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, got[0])
			}
		}
	}
	return "", nil
}
