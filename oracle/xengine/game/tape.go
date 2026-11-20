package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/adams-shaun/gorge/engine/state"
)

// TapeFormat is the Tape.Format value Play writes. xengine-tape-3 adds the
// die rolls of an entry to xengine-tape-2's runner refs (Pick.Ref,
// Pick.AttackerRef, Pick.BattleRef, ManaRec.Ref), the decision's Max and the
// coin flips (each also marked Forced when a FlipCoinMod static made it
// without an RNG draw, and carrying Draws when a FlipCoinDoubler made it two).
// Both earlier formats are still read: tape-2 has no die rolls, tape-1 has no
// refs, Max or flips either. A reader rejects any other format.
const TapeFormat = "xengine-tape-3"

// TapeFormat2 is the format before the entry die rolls.
const TapeFormat2 = "xengine-tape-2"

// GameFormatCommander is Tape.GameFormat of a Commander game.
const GameFormatCommander = "commander"

// CommanderStartingLife is each seat's opening life in a 1v1 Commander game
// (CR 903.7).
const CommanderStartingLife int32 = 40

// TapeFormat1 is the previous format, accepted by ReadTape.
const TapeFormat1 = "xengine-tape-1"

// Tape is one recorded game: the header (set, decks, seed, starter, opening
// zones), every committed decision, and the outcome. Play produces it; the
// same (pair, seed) produces byte-identical JSON.
type Tape struct {
	Format string `json:"format"`
	// GameFormat is "commander" for a Commander game, "" for a constructed one.
	GameFormat string `json:"game_format,omitempty"`
	Set        string `json:"set,omitempty"`
	Seed       uint64 `json:"seed"`
	Decks      []Deck `json:"decks"`
	// Starter is the starting player: the active seat of turn 1.
	Starter int       `json:"starter"`
	Open    []Opening `json:"opening"`
	// GenesisRNG and GenesisMana are the engine draws and mana abilities
	// before the first decision (the opening shuffle and deal).
	GenesisRNG  uint64    `json:"genesis_rng,omitempty"`
	GenesisMana []ManaRec `json:"genesis_mana,omitempty"`
	Entries     []Entry   `json:"entries"`
	Outcome     Outcome   `json:"outcome"`
}

// Deck is one seat's list as played: 40 cards from decks.Pair, 100 for a
// Commander deck, or a fixed deck's main deck under play -decks.
type Deck struct {
	Name      string   `json:"name"`
	Colours   string   `json:"colours,omitempty"`
	Cards     []string `json:"cards"`
	Commander string   `json:"commander,omitempty"`
}

// Opening is one seat's hand, command zone and library as the first decision
// found them: the hand and command zone in zone order and the whole library
// top first.
type Opening struct {
	Hand    []string `json:"hand"`
	Command []string `json:"command,omitempty"`
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
	// Rolls are the die rolls committing this intent made, in log order.
	Rolls []Roll `json:"rolls,omitempty"`
	// Reanswered marks an entry whose intent the engine refused: gorge's
	// refusal ladder committed a different answer, which the tape does not
	// carry.
	Reanswered bool   `json:"reanswered,omitempty"`
	Digest     Digest `json:"digest"`
}

// Flip is one coin-flip result: the flipper's seat and whether it won.
// Forced marks a flip a FlipCoinMod static turned heads without drawing from
// the engine's RNG (Edgar, King of Figaro's "those coins come up heads"). It
// is recorded so the lowering gate can tell a flip that accounts for a draw
// from one that does not; a winning flip the static did not cover is a real
// draw. Absent (false) in a tape written before this mark, which is the safe
// reading: an unmarked flip is counted as a draw. A follower replaying the
// same script reproduces a forced flip through the static, so it needs no
// answer for it.
//
// Draws is how many engine RNG values the flip consumed: 0 for a forced
// flip, 2 while a FlipCoinDoubler static (Krark's Thumb) makes coinResult
// flip two coins for one instruction, and omitted for the ordinary one-draw
// flip. A tape written before this field read it as 0; Forced tells the two
// zero cases apart, so such a flip is counted as one draw.
type Flip struct {
	Seat   int  `json:"seat"`
	Win    bool `json:"win"`
	Forced bool `json:"forced,omitempty"`
	Draws  int  `json:"draws,omitempty"`
}

// DrawCount is the number of engine RNG values the flip consumed. An absent
// Draws on an older tape means the ordinary one draw (or none when Forced);
// a forced flip never draws, even under a doubler, because gorge reads
// FlipCoinForced before it calls coinResult.
func (f Flip) DrawCount() uint64 {
	if f.Forced {
		return 0
	}
	if f.Draws > 0 {
		return uint64(f.Draws)
	}
	return 1
}

// Roll is one die-roll result: the roller's seat, the die's sides, the
// natural roll and the result after any modifier. Both values are recorded
// because a follower must force the natural roll (the modifier is the
// follower's own) while the live game's outcome is the modified one.
type Roll struct {
	Seat    int `json:"seat"`
	Sides   int `json:"sides"`
	Natural int `json:"natural"`
	Result  int `json:"result"`
}

// Pick is one option an intent chose. Which fields matter follows Kind: Ability
// is the index into the source face's Abilities for an "ability" option (a
// missing field is index 0, as in gorge), Attacker the attacked creature of a
// block option, Player the defending or targeted seat, Battle the attacked
// battle/planeswalker.
//
// SVar, GrantSource, GainedSource, GainedIdx and Keyword carry a granted or
// gained ability's identity (decision.Option's same-named anchors, mirrored
// from decision to tape): the option is not the card's printed
// Face.Abilities[Ability] — for an SVar grant Ability is the zero value, for a
// keyword grant it is -1 — so the lowering and the follower exporters need
// the grant identity (and the offered Label beside it) to name the right
// ability. Absent on every printed-ability pick, so existing tapes
// serialise byte-identically.
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
	SVar     string      `json:"svar,omitempty"`
	Keyword  string      `json:"keyword,omitempty"`
	// GrantSource, GainedSource and GainedIdx are the decision.Option
	// anchors of an SVar-granted (AddAbilities$/AddAbility$) and a
	// gained (GainsAbilitiesOf$) ability: the grantor object and, for a
	// gained ability, the index into its face's Abilities. Zero-valued
	// on every other option. Keyword is the derived keyword line
	// ("Cycling:1 U") a layer-6 AddKeyword$ grant offered.
	GrantSource  state.ObjID `json:"grant_source,omitempty"`
	GainedSource state.ObjID `json:"gained_source,omitempty"`
	GainedIdx    int         `json:"gained_idx,omitempty"`
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
	if t.Format != TapeFormat && t.Format != TapeFormat2 && t.Format != TapeFormat1 {
		return nil, fmt.Errorf("game: tape format %q, want %q, %q or %q", t.Format, TapeFormat, TapeFormat2, TapeFormat1)
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
