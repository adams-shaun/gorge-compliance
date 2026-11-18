package adjudicate

// The fshadow input: the hard divergences `xengine fshadow` (the Forge-led
// gorge follower) writes as divergence-<n>.json, turned into signatures, a
// root-cause class census, a rank and one brief per class
// (docs/design/2026-11-17-xengine-commander-diffs.md in gorge, sections 3.5
// and 5). The briefs go to the same tickets/ directory, through the same Write,
// as the three-way clusters: this is an input to adjudicate, not a second
// writer.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DivergenceSchema is the schema tag of one divergence-<n>.json.
const DivergenceSchema = "fshadow-divergence-1"

// Divergence is one hard divergence of a Forge-hosted game (or a G|D duel) with
// gorge in shadow: design section 3 step 5's fields plus the signature's
// parts. The producer writes one per file; every field but the signature parts
// is evidence the brief quotes.
type Divergence struct {
	Schema string   `json:"schema"`
	Game   string   `json:"game"`            // game id, unique within a run
	Seed   int64    `json:"seed"`            // the deal's seed
	Decks  []string `json:"decks,omitempty"` // manifest deck ids, by seat
	// Log is the forge-shadow-1 log and Offset the line of the divergent
	// record in it.
	Log    string `json:"log,omitempty"`
	Offset int64  `json:"offset"`
	// Pair is F|G or G|D; Resynced > 0 marks a finding after the k-th re-sync
	// (the signature's pair then reads F|G:resynced).
	Pair     string `json:"pair"`
	Resynced int    `json:"resynced,omitempty"`
	// What is options, answer-missing, snapshot, kind, terminal or error.
	What string `json:"what"`
	// Kind is gorge's decision.Kind at the divergence.
	Kind string `json:"kind"`
	// Field is the first differing field, indices included
	// (Battlefield[3].Tapped); Gorge and Other are both sides' values there.
	Field string `json:"field,omitempty"`
	Gorge string `json:"gorge,omitempty"`
	Other string `json:"other,omitempty"`
	// Cards are the implicated cards (the one-sided options, else the first
	// card field, else the last sourced action; Implicates for G|D).
	Cards []Implicated `json:"cards,omitempty"`
	// Record is the gorge match record prefix (gorge-match-1 with Chance), and
	// Repro the in-worktree command that replays it, when the producer has one.
	Record string `json:"record,omitempty"`
	Repro  string `json:"repro,omitempty"`
	// Harness is non-empty when the difference is the follower's, not an
	// engine's: answer-unmapped (a Forge answer with no gorge mapping),
	// setup-inexpressible, chance-misaligned. Such a finding ranks with the
	// follower tickets, never as a gorge defect.
	Harness string `json:"harness,omitempty"`
	Msg     string `json:"msg,omitempty"`
}

// Implicated is one implicated card and, when the producer knows it, the
// ability of it that was in play: Ability indexes the front face's abilities
// and Trigger its triggers, in IR order; Primitive, when set, names the IR
// primitive outright (api:Pump, kw:Flying) and wins.
type Implicated struct {
	Name      string `json:"name"`
	Token     bool   `json:"token,omitempty"`
	Ability   *int   `json:"ability,omitempty"`
	Trigger   *int   `json:"trigger,omitempty"`
	Primitive string `json:"primitive,omitempty"`
}

// signatureName is the card's part of a signature: a token is token:<name>.
func (c Implicated) signatureName() string {
	if c.Token {
		return "token:" + c.Name
	}
	return c.Name
}

// Ranker supplies what the census reads beyond the findings. A nil func takes
// its default: primitive unresolved ("?<card>"), one deck, paper 1, and a
// generic suspect line.
type Ranker struct {
	// Primitive is the IR primitive of the implicated ability.
	Primitive func(Implicated) string
	// Decks is how many manifest decks hold the card.
	Decks func(card string) int
	// Paper is 1 for a paper Commander card and 0.1 for a digital or
	// non-tournament one.
	Paper func(card string) float64
	// Suspect is the file:function of the primitive's handler.
	Suspect func(primitive string) string
}

// Signature is one distinct difference: pair|what|kind|field|cards.
type Signature struct {
	Key, SHA12              string
	Pair, What, Kind, Field string
	Cards                   []string // up to 2, sorted
	Primitive               string
	Findings                []Divergence // by game, then offset
	Games                   int          // distinct games
	Decks                   int          // manifest decks holding the card (floor 1)
	Paper                   float64
	Score                   float64 // Games x Decks x Paper
}

// Class is one root-cause class, what|kind|field|primitive, with its
// signatures, by key. A harness class's primitive is harness:<reason>.
type Class struct {
	Key, SHA12                   string
	What, Kind, Field, Primitive string
	Harness                      bool
	Signatures                   []Signature
	Score                        float64 // sum of the signatures' scores
}

// Census is the ranked classes: gorge defects, and the harness classes (the
// follower tickets) apart. Each list is by score, highest first, then key.
type Census struct {
	Classes []Class
	Harness []Class
}

// FieldPath strips a field's indices as dirac/oracle's fieldClass does:
// Battlefield[3].Tapped -> Battlefield[].Tapped, Life[0] -> Life[].
func FieldPath(field string) string {
	var b strings.Builder
	skip := false
	for _, r := range field {
		switch {
		case r == '[':
			skip = true
			b.WriteRune(r)
		case r == ']':
			skip = false
			b.WriteRune(r)
		case !skip:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sigPair is the signature's pair part.
func sigPair(d Divergence) string {
	if d.Resynced > 0 && !strings.HasSuffix(d.Pair, ":resynced") {
		return d.Pair + ":resynced"
	}
	return d.Pair
}

// sigCards is the signature's cards: up to 2 implicated names, sorted.
func sigCards(d Divergence) []string {
	seen := map[string]bool{}
	var names []string
	for _, c := range d.Cards {
		n := c.signatureName()
		if c.Name == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 2 {
		names = names[:2]
	}
	return names
}

// SignatureKey is d's signature, pair|what|kind|field|cards.
func SignatureKey(d Divergence) string {
	return strings.Join([]string{sigPair(d), d.What, d.Kind, FieldPath(d.Field), strings.Join(sigCards(d), ",")}, "|")
}

// primitiveOf is the class's primitive for d: the first implicated card (in
// signature order) whose ability resolves, else the first card unresolved.
func primitiveOf(d Divergence, r Ranker) string {
	if d.Harness != "" {
		return "harness:" + d.Harness
	}
	cs := append([]Implicated(nil), d.Cards...)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].signatureName() < cs[j].signatureName() })
	first := ""
	for _, c := range cs {
		if c.Name == "" {
			continue
		}
		p := c.Primitive
		if p == "" && r.Primitive != nil {
			p = r.Primitive(c)
		}
		if p != "" && !strings.HasPrefix(p, "?") {
			return p
		}
		if first == "" {
			first = "?" + c.signatureName()
		}
	}
	if first == "" {
		return "?"
	}
	return first
}

// cardWeight is decks x paper of the signature's most widely held card. The
// deck count has a floor of 1, so a card no manifest deck lists (a token, a
// card a deck gained) still ranks.
func cardWeight(names []string, r Ranker) (decks int, paper float64) {
	decks, paper = 1, 1
	best := -1.0
	for _, n := range names {
		d, p := 1, 1.0
		if !strings.HasPrefix(n, "token:") {
			if r.Decks != nil {
				d = max(r.Decks(n), 1)
			}
			if r.Paper != nil {
				p = r.Paper(n)
			}
		}
		if w := float64(d) * p; w > best {
			best, decks, paper = w, d, p
		}
	}
	return decks, paper
}

// BuildCensus groups ds into signatures and classes and ranks them.
func BuildCensus(ds []Divergence, r Ranker) *Census {
	sigs := map[string]*Signature{}
	for _, d := range ds {
		key := SignatureKey(d)
		prim := primitiveOf(d, r)
		// The class splits on the primitive too: one signature can only sit
		// in one class, so the primitive joins its identity.
		id := key + "\x00" + prim
		s := sigs[id]
		if s == nil {
			s = &Signature{Key: key, SHA12: sha12(key), Pair: sigPair(d), What: d.What, Kind: d.Kind,
				Field: FieldPath(d.Field), Cards: sigCards(d), Primitive: prim}
			sigs[id] = s
		}
		s.Findings = append(s.Findings, d)
	}
	classes := map[string]*Class{}
	for _, s := range sigs {
		sort.Slice(s.Findings, func(i, j int) bool {
			a, b := s.Findings[i], s.Findings[j]
			if a.Game != b.Game {
				return a.Game < b.Game
			}
			return a.Offset < b.Offset
		})
		games := map[string]bool{}
		for _, f := range s.Findings {
			games[f.Game] = true
		}
		s.Games = len(games)
		s.Decks, s.Paper = cardWeight(s.Cards, r)
		s.Score = float64(s.Games) * float64(s.Decks) * s.Paper
		ck := strings.Join([]string{s.What, s.Kind, s.Field, s.Primitive}, "|")
		c := classes[ck]
		if c == nil {
			c = &Class{Key: ck, SHA12: sha12("fshadow|" + ck), What: s.What, Kind: s.Kind, Field: s.Field,
				Primitive: s.Primitive, Harness: strings.HasPrefix(s.Primitive, "harness:")}
			classes[ck] = c
		}
		c.Signatures = append(c.Signatures, *s)
		c.Score += s.Score
	}
	out := &Census{}
	for _, c := range classes {
		sort.Slice(c.Signatures, func(i, j int) bool { return c.Signatures[i].Key < c.Signatures[j].Key })
		if c.Harness {
			out.Harness = append(out.Harness, *c)
		} else {
			out.Classes = append(out.Classes, *c)
		}
	}
	for _, l := range [][]Class{out.Classes, out.Harness} {
		sort.Slice(l, func(i, j int) bool {
			if l[i].Score != l[j].Score {
				return l[i].Score > l[j].Score
			}
			return l[i].Key < l[j].Key
		})
	}
	return out
}

var divergenceFile = regexp.MustCompile(`^divergence-[0-9]+\.json$`)

// LoadFShadow reads every divergence-<n>.json under dir (at any depth), in
// path order.
func LoadFShadow(dir string) ([]Divergence, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && divergenceFile.MatchString(e.Name()) {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]Divergence, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var d Divergence
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("%s: %v", p, err)
		}
		if d.Schema != DivergenceSchema {
			return nil, fmt.Errorf("%s: schema %q, want %q", p, d.Schema, DivergenceSchema)
		}
		if d.Pair == "" || d.What == "" {
			return nil, fmt.Errorf("%s: no pair or what", p)
		}
		out = append(out, d)
	}
	return out, nil
}
