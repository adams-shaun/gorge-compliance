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
	Hand        []string       `json:"hand,omitempty"`
	Battlefield []string       `json:"battlefield,omitempty"`
	Graveyard   []string       `json:"graveyard,omitempty"`
	Exile       []string       `json:"exile,omitempty"`
	Library     []string       `json:"library,omitempty"`
	Tapped      []string       `json:"tapped,omitempty"`
	Counters    []StartCounter `json:"counters,omitempty"`
	Life        *int32         `json:"life,omitempty"`
}

// StartCounter puts N counters of Kind on the first battlefield copy of Card
// not yet bound.
type StartCounter struct {
	Card string `json:"card"`
	Kind string `json:"kind"`
	N    int32  `json:"n"`
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
	// Times is how many times the focus takes its action (0 or absent: once):
	// "whenever you cast your second spell each turn" is two casts.
	Times int `json:"times,omitempty"`
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
