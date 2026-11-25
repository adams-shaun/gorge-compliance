package scen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// StartFormat is the format tag of a start file.
const StartFormat = "xproof-start-1"

// StartFile is the xproof-start-1 document: a scripted position `xengine play
// -start` plays on from, with one focus seat taking the action that exercises
// the owed key.
type StartFile struct {
	Format   string     `json:"format"`
	Card     string     `json:"card"`
	Key      string     `json:"key"`
	Players  int        `json:"players"`
	Active   int        `json:"active"`
	MaxTurns int        `json:"max_turns"`
	Setup    StartSetup `json:"setup"`
	Filler   string     `json:"filler"`
	Seeds    int        `json:"seeds"`
	// SeedBase is where `xengine play -start` starts its seed search (0 or
	// absent: 1): a retried key sets it to play new games.
	SeedBase uint64      `json:"seed_base,omitempty"`
	Focus    *StartFocus `json:"focus,omitempty"`
	// Compare is the item's opt-in comparison fields (oraclediff.Compare*) the
	// lowered item carries: a static start whose effect is a keyword is
	// observable only with "keywords".
	Compare []string `json:"compare,omitempty"`
}

// StartSetup holds the two seats' placements.
type StartSetup struct {
	P0 StartSeat `json:"p0"`
	P1 StartSeat `json:"p1"`
}

// StartSeat is one seat's placements, card names; a repeat is a separate copy.
type StartSeat struct {
	Hand        []string `json:"hand,omitempty"`
	Battlefield []string `json:"battlefield,omitempty"`
	Graveyard   []string `json:"graveyard,omitempty"`
	Exile       []string `json:"exile,omitempty"`
	Library     []string `json:"library,omitempty"`
	Tapped      []string `json:"tapped,omitempty"`
	// BackFace names battlefield cards that enter on their back face (a
	// double-faced card's transformed side), as rules.StartSeat.BackFace.
	BackFace []string       `json:"back_face,omitempty"`
	Counters []StartCounter `json:"counters,omitempty"`
	// Attach binds a battlefield Aura of this seat to a bearer of the same
	// seat (gorge's rules.StartAttach). The Aura is listed in Battlefield too;
	// placeAttach makes it enter attached, which a start placing a bare Aura
	// cannot do (FieldBlock aura-unattached).
	Attach []StartAttach `json:"attach,omitempty"`
	Tokens []StartToken  `json:"tokens,omitempty"`
	Life   *int32        `json:"life,omitempty"`
}

// StartAttach is one attachment: the Aura Card, also in Battlefield, enters
// attached to To (also in Battlefield).
type StartAttach struct {
	Card string `json:"card"`
	To   string `json:"to"`
	// Opp: the bearer is on the other seat's battlefield, not this seat's.
	Opp bool `json:"opp,omitempty"`
}

// Attached reports whether name is an Aura the seat lists in Attach.
func (s *StartSeat) Attached(name string) bool {
	for _, a := range s.Attach {
		if a.Card == name {
			return true
		}
	}
	return false
}

// TappedCopies reports, for each Battlefield entry in order, whether that copy
// is tapped. Tapped names bind to the first copy of the name not bound yet, as
// the start file's lowering does (cmd/xengine's startDeck): a repeated name
// (four Mountains, one tapped to sacrifice) marks one copy, not all of them.
// A name with no battlefield copy is ignored here; the lowering refuses it.
func (s *StartSeat) TappedCopies() []bool {
	flags := make([]bool, len(s.Battlefield))
	used := make([]bool, len(s.Battlefield))
	for _, name := range s.Tapped {
		for i, card := range s.Battlefield {
			if !used[i] && card == name {
				used[i], flags[i] = true, true
				break
			}
		}
	}
	return flags
}

// StartCounter puts N counters of Kind on the first battlefield copy of Card
// not yet bound.
type StartCounter struct {
	Card string `json:"card"`
	Kind string `json:"kind"`
	N    int32  `json:"n"`
}

// StartToken is a token a start mints onto the seat's battlefield: the
// Config.Tokens script key (the token definitions corpus), entering tapped
// when Tapped. A token is not a deck card, so unlike the placements above it
// names no card.
type StartToken struct {
	Script string `json:"script"`
	Tapped bool   `json:"tapped,omitempty"`
}

// StartFocus names the seat and action that exercises the key.
type StartFocus struct {
	Seat    *int   `json:"seat,omitempty"`
	Card    string `json:"card,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Ability int    `json:"ability"`
	// Mode, for a cast focus, is the cast option's payment kind ("kicked"):
	// a key reading "if it was kicked" is reached by the kicked cast itself.
	Mode string `json:"mode,omitempty"`
	// Targets are the permanents the focus play must target, in the order the
	// play asks for them (a removal spell's one target, Magnetic Theft's
	// Equipment then creature). A forced start names them because its other
	// placed cards can be legal alternatives, and the bot's own pick would
	// then aim the destruction at an enabler instead of the key object.
	Targets []string `json:"targets,omitempty"`
	// Times is how many times the focus takes its action (0 or absent: once):
	// "whenever you cast your second spell each turn" is two casts.
	Times int `json:"times,omitempty"`
	// Bait, for a play that targets a spell, names the spell the focus seat's
	// opponent casts at turn 1's first main-phase priority before the focus
	// play, so the play's target is on the stack (CR 115.1a). The start places
	// it in the opponent's hand with the lands its cost takes, and the opponent
	// is the active seat when the bait is no instant (a creature spell is cast
	// on its controller's main phase).
	Bait string `json:"bait,omitempty"`
	// Attack and Block are the scripted combat the focus play waits for: the
	// active seat attacks with exactly Attack, the defending seat blocks it
	// with exactly Block (no blocks when "" while Attack is set), and only
	// then does the focus seat take its play ("target attacking creature",
	// "target creature blocking this").
	Attack string `json:"attack,omitempty"`
	Block  string `json:"block,omitempty"`
}

// Marshal is the file's bytes: indented JSON and a final newline.
func (f *StartFile) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// DecodeStartFile reads a start file strictly: an unknown field or a format
// other than StartFormat is an error.
func DecodeStartFile(raw []byte) (*StartFile, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f StartFile
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	if f.Format != StartFormat {
		return nil, fmt.Errorf("start file format %q, want %q", f.Format, StartFormat)
	}
	if f.Card == "" || f.Key == "" {
		return nil, errors.New("start file needs card and key")
	}
	return &f, nil
}

// Slug is a card name lowercased, every run of non-[a-z0-9] characters one
// "-", trimmed: the name part of an item id and a file.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	return b.String()
}

// KeyID is a key with "/" and "#" removed (f0/a2 -> f0a2).
func KeyID(key string) string {
	return strings.NewReplacer("/", "", "#", "").Replace(key)
}

// StartID is the id of a start item: xs:<set>:<slug>:<keyid>:s<n>. It and
// StepsID are the only constructors of an xs: item id; ParseID is the only
// reader, so the key an item was built for cannot drift from its id.
func StartID(set, card, key string, n int) string {
	return fmt.Sprintf("xs:%s:%s:%s:s%d", set, Slug(card), KeyID(key), n)
}

// StepsID is the id of a scripted-steps item: xs:<set>:<slug>:<keyid>:steps.
func StepsID(set, card, key string) string {
	return fmt.Sprintf("xs:%s:%s:%s:steps", set, Slug(card), KeyID(key))
}

// ParseID splits an xs: item id written by StartID or StepsID into its set,
// card slug, key and ordinal ("s<n>" or "steps"). KeyID throws away "/" and
// "#", so the key is rebuilt from its family (f0/a2, static#0.0,
// combat#0.block, cast, land): the id names the key the item was built for,
// which no tape entry does for a static or combat key.
func ParseID(id string) (set, slug, key, ordinal string, ok bool) {
	rest, found := strings.CutPrefix(id, "xs:")
	if !found {
		return "", "", "", "", false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[3] == "" {
		return "", "", "", "", false
	}
	key, ok = keyFromID(parts[2])
	if !ok {
		return "", "", "", "", false
	}
	return parts[0], parts[1], key, parts[3], true
}

var (
	abilityKeyIDRE = regexp.MustCompile(`^f(\d+)([at])(\d+)$`)
	staticKeyIDRE  = regexp.MustCompile(`^static(\d+)\.(\d+)$`)
	combatKeyIDRE  = regexp.MustCompile(`^combat(\d+)\.(attack|block)$`)
)

// keyFromID is KeyID's inverse over the key families an item id can name.
func keyFromID(keyid string) (string, bool) {
	switch keyid {
	case "cast", "land":
		return keyid, true
	}
	if m := abilityKeyIDRE.FindStringSubmatch(keyid); m != nil {
		return "f" + m[1] + "/" + m[2] + m[3], true
	}
	if m := staticKeyIDRE.FindStringSubmatch(keyid); m != nil {
		return "static#" + m[1] + "." + m[2], true
	}
	if m := combatKeyIDRE.FindStringSubmatch(keyid); m != nil {
		return "combat#" + m[1] + "." + m[2], true
	}
	return "", false
}
