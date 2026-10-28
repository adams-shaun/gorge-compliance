package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/adams-shaun/gorge/engine/state"
)

// TapeFormat is the Tape.Format value Play writes. xengine-tape-2 adds the
// runner refs (Pick.Ref, Pick.AttackerRef, Pick.BattleRef, ManaRec.Ref), the
// decision's Max and the coin flips of an entry; xengine-tape-1 has none of
// them and is still read. A reader rejects any other format.
const TapeFormat = "xengine-tape-2"

// TapeFormat1 is the previous format, accepted by ReadTape.
const TapeFormat1 = "xengine-tape-1"

// Tape is one recorded game: the header (set, decks, seed, starter, opening
// zones), every committed decision, and the outcome. Play produces it; the
// same (pair, seed) produces byte-identical JSON.
type Tape struct {
	Format string  `json:"format"`
	Set    string  `json:"set,omitempty"`
	Seed   uint64  `json:"seed"`
	Decks  [2]Deck `json:"decks"`
	// Starter is the starting player: the active seat of turn 1.
	Starter int        `json:"starter"`
	Open    [2]Opening `json:"opening"`
	// GenesisRNG and GenesisMana are the engine draws and mana abilities
	// before the first decision (the opening shuffle and deal).
	GenesisRNG  uint64    `json:"genesis_rng,omitempty"`
	GenesisMana []ManaRec `json:"genesis_mana,omitempty"`
	Entries     []Entry   `json:"entries"`
	Outcome     Outcome   `json:"outcome"`
}

// Deck is one seat's 40-card list as played.
type Deck struct {
	Name    string   `json:"name"`
	Colours string   `json:"colours,omitempty"`
	Cards   []string `json:"cards"`
}

// Opening is one seat's hand and library as the first decision found them:
// the hand in zone order and the whole library top first.
type Opening struct {
	Hand    []string `json:"hand"`
	Library []string `json:"library"`
}

// Entry is one committed decision. Digest is taken before the intent is
// submitted. Mana and RNG describe what committing THIS intent did: the mana
// abilities it fired (an auto-paid cast fires its payment's abilities here)
// and the engine draws it consumed. Anything between the decision's last
// entry and the game's end lands on the final entry.
type Entry struct {
	Seq  uint64 `json:"seq"`
	Seat int    `json:"seat"`
	Kind string `json:"kind"`
	Turn int32  `json:"turn"`
	Step string `json:"step"`
	// Choices and Rest are the raw intent indices into the decision's
	// options; Picks are the resolved Choices (or, for a payment/announce
	// intent, the cast it names).
	Choices []int  `json:"choices"`
	Rest    []int  `json:"rest,omitempty"`
	Picks   []Pick `json:"picks,omitempty"`
	// Max is the decision's Max (how many options it takes at most);
	// StackTop is the object on top of the stack when it was posed (0 for an
	// empty stack).
	Max      int         `json:"max,omitempty"`
	StackTop state.ObjID `json:"stack_top,omitempty"`
	// Payment is the plan's action id when the intent selected a payment
	// plan; Announce the action id when it announced a cast to pay manually.
	Payment  string    `json:"payment,omitempty"`
	Announce string    `json:"announce,omitempty"`
	Mana     []ManaRec `json:"mana,omitempty"`
	// Spent is the pool mana the commit actually spent, as WUBRGC letters
	// in the engine's emission order: one letter per unit. The engine logs a
	// negative ManaAdd per spent unit (plain, tagged or snow Counter), which
	// the recorder folds here. Empty when the decision spent no pool mana.
	Spent string `json:"spent,omitempty"`
	RNG   uint64 `json:"rng,omitempty"`
	// Flips are the coin flips committing this intent made, in log order.
	Flips []Flip `json:"flips,omitempty"`
	// Reanswered marks an entry whose intent the engine refused: gorge's
	// refusal ladder committed a different answer, which the tape does not
	// carry.
	Reanswered bool   `json:"reanswered,omitempty"`
	Digest     Digest `json:"digest"`
}

// Flip is one coin-flip result: the flipper's seat and whether it won.
type Flip struct {
	Seat int  `json:"seat"`
	Win  bool `json:"win"`
}

// Pick is one option an intent chose. Which fields matter follows Kind: Ability
// is the index into the source face's Abilities for an "ability" option (a
// missing field is index 0, as in gorge), Attacker the attacked creature of a
// block option, Player the defending or targeted seat, Battle the attacked
// battle/planeswalker.
type Pick struct {
	Index    int         `json:"index"`
	Kind     string      `json:"kind"`
	Obj      state.ObjID `json:"obj,omitempty"`
	Ability  int         `json:"ability,omitempty"`
	Mode     string      `json:"mode,omitempty"`
	AltCost  int         `json:"alt_cost,omitempty"`
	Attacker state.ObjID `json:"attacker,omitempty"`
	Player   int         `json:"player"`
	Battle   state.ObjID `json:"battle,omitempty"`
	Label    string      `json:"label,omitempty"`
	// Ref, AttackerRef and BattleRef name Obj, Attacker and Battle the way
	// the oracle runner's scenario refs do ("p0:Forest#2", "p1:token:Soldier"),
	// in the live game's seat numbering, as of the moment the decision was
	// posed. Empty when the object has no ref (or the field is unset).
	Ref         string `json:"ref,omitempty"`
	AttackerRef string `json:"attacker_ref,omitempty"`
	BattleRef   string `json:"battle_ref,omitempty"`
}

// ManaRec is one mana ability activation: the source object, its owner and
// name, who activated it, and the ability's index in the source face's
// Abilities (-1 for a gained, granted or triggered mana ability).
type ManaRec struct {
	Obj     state.ObjID `json:"obj"`
	Owner   int         `json:"owner"`
	Player  int         `json:"player"`
	Name    string      `json:"name"`
	Ability int         `json:"ability"`
	// Ref is the source's runner ref when the ability fired (see Pick.Ref).
	Ref string `json:"ref,omitempty"`
	// Produced is the mana the activation added, as WUBRGC letters in the
	// order they were added (best effort: read from the ManaAdd events logged
	// before the next activation or decision).
	Produced string `json:"produced,omitempty"`
}

// Outcome is how the game ended. Stall is "" for a finished game, otherwise
// the watchdog that ended it ("turns", "intents", "livelock", "panic",
// "bigboard").
type Outcome struct {
	Winner  *int   `json:"winner,omitempty"`
	Draw    bool   `json:"draw,omitempty"`
	Turns   int32  `json:"turns"`
	Intents int    `json:"intents"`
	Stall   string `json:"stall,omitempty"`
	Diag    string `json:"diag,omitempty"`
}

// WriteJSON writes the tape as indented JSON. Output is a pure function of
// the tape (struct field order, sorted map keys).
func (t *Tape) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	return enc.Encode(t)
}

// ReadTape reads a tape WriteJSON wrote, rejecting an unknown format or field.
func ReadTape(r io.Reader) (*Tape, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var t Tape
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("game: read tape: %w", err)
	}
	if t.Format != TapeFormat && t.Format != TapeFormat1 {
		return nil, fmt.Errorf("game: tape format %q, want %q or %q", t.Format, TapeFormat, TapeFormat1)
	}
	return &t, nil
}

// Bytes is the tape's JSON.
func (t *Tape) Bytes() ([]byte, error) {
	var b bytes.Buffer
	if err := t.WriteJSON(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
