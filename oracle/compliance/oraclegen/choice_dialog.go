package oraclegen

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
)

// xmageChoiceDialogFaces names the faces whose plain DB$/AB$ GenericChoice
// XMage poses as ONE Choice dialog over the option LABELS
// (player.choose(Outcome, new ChoiceImpl(..), game) with setChoices of the
// same words), which TestPlayer reads from the CHOICE queue by label. Gorge
// records the pick as a "modes" decision; scripted on the numeric mode queue
// (the positional fallback) the answer is never consumed and XMage's AI picks
// by its own default direction (Transmutation Font: gorge's Blood, XMage's
// Clue).
//
// Membership is proven against the XMage source, not guessed: the card's
// implementation builds a ChoiceImpl and every Forge option label appears in
// it verbatim. A GenericChoice whose XMage form is a chooseUse, a Modes ask or
// a Choice with different wording is NOT listed (TestChoiceDialogFacesCensus
// counts the corpus faces so a new entry is a deliberate edit).
var xmageChoiceDialogFaces = map[string]bool{
	"transmutation font":   true,
	"the third doctor":     true,
	"teferi's realm":       true,
	"fatespinner":          true,
	"master of ceremonies": true,
}

// choiceDialogLabels lists the option labels of every GenericChoice of a face
// in xmageChoiceDialogFaces, keyed as the engine labels its mode picks
// (effects.CharmModeLabel); nil for any other face.
func choiceDialogLabels(f *cards.Face) []string {
	if f == nil || !xmageChoiceDialogFaces[strings.ToLower(f.Name)] {
		return nil
	}
	var out []string
	add := func(params map[string]string) {
		for _, name := range strings.Split(params["Choices"], ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				out = append(out, effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name))
			}
		}
	}
	for _, sa := range f.Abilities {
		if sa.API == "GenericChoice" {
			add(sa.Params)
		}
	}
	for _, body := range f.SVars {
		if p := svarParams(body); p["DB"] == "GenericChoice" {
			add(p)
		}
	}
	return out
}

// xmageChooseUseFaces names the faces whose two-option GenericChoice XMage
// poses as ONE boolean chooseUse, not a mode or a Choice dialog. The map value
// says which Choices$ label XMage's TRUE branch takes: true = the FIRST label
// (the usual case), false = the SECOND (Decoy Gambit, whose Forge Choices$ list
// the "return to hand" label first while XMage's true branch draws). The pick
// is answered "yes" for the true-branch label and "no" for the other, for the
// seat that chose -- the targeted opponent on the "target opponent may have you
// X; if they don't, Y" shape (Terrapact Intimidator), the controller on the
// card's own election.
//
// Membership and polarity are proven against the XMage source at XMAGE_REF
// fdde07931f, one chooseUse per entry (true branch in the last column's first
// parenthesis side unless noted):
//
//	BaneLordOfDarkness       "Have <controller> draw a card or put a creature into play?" ("Draw card"/"Play creature")
//	CombustibleGearhulk      "Have <controller> draw three cards?"                    (draw three / mill and damage)
//	CovenantOfMinds          "Put the revealed cards into <controller>'s hand?"       (to hand / graveyard and draw five)
//	DecoyGambit              "Have <controller> draw a card? If you don't, ..."       (return to hand / draw) -- TRUE = draw = the SECOND label
//	DistantMemories          "Have <controller> put <card> in their hand?"            (to hand / leave exiled)
//	InspiritFlagshipVessel   "Put a +1/+1 counter or two charge counters on ...?"     ("one +1/+1 counter"/"two charge counters")
//	LiarsPendulum            "Is the chosen card (...) in <controller>'s hand?"       (guess in / guess not in)
//	PalantirOfOrthanc        "Have <controller> draw a card?"                         (draw / mill and lose life)
//	SinProdder               "Put <card> in <controller>'s graveyard?"                (graveyard and damage / to hand)
//	SoulEcho                 "Have all damage dealt to <controller> be ..."           (remove counters / deal damage)
//	TerrapactIntimidator     "Have <controller> create two Lander tokens?"            (Lander tokens / +1/+1 counters)
//
// A face with a multi-ask XMage form is NOT listed: the pay-sacrifice-or-discard
// punishers (Thornplate Intimidator, Starseer Mentor, Tergrid's Lantern) ask a
// chooseUse and then a second cost pick, so no single yes/no answers them. The
// rest of the corpus's two-choice opponent-targeted faces have no XMage
// implementation at the pin (Bronze Tablet, Feed the Machine, ...) or a
// different ask (Indulgent Tormentor's sacrifice and pay-life chooseUse pair).
// TestChooseUseFacesModeNumbersCensus counts the corpus faces and compares each
// against an independently written true-branch label, so a new entry or a
// polarity change is a deliberate, source-proven edit.
var xmageChooseUseFaces = map[string]bool{
	"bane, lord of darkness":    true,
	"combustible gearhulk":      true,
	"covenant of minds":         true,
	"decoy gambit":              false, // XMage true = draw = the second Choices$ label
	"distant memories":          true,
	"inspirit, flagship vessel": true,
	"liar's pendulum":           true,
	"palantír of orthanc":       true,
	"sin prodder":               true,
	"soul echo":                 true,
	"terrapact intimidator":     true,
}

// chooseUseLabels lists the [yes, no] label pairs of every two-option
// GenericChoice on a face in xmageChooseUseFaces (yes = the label on XMage's
// true branch, per the map value), keyed as the engine labels its mode picks
// (effects.CharmModeLabel). nil for any other face. ok is false
// when a listed face has no GenericChoice body or one that does not offer
// exactly two choices, so a listed face cannot silently lose one side.
func chooseUseLabels(f *cards.Face) (pairs [][2]string, ok bool) {
	if f == nil {
		return nil, false
	}
	firstIsTrue, listed := xmageChooseUseFaces[strings.ToLower(f.Name)]
	if !listed {
		return nil, false
	}
	add := func(params map[string]string) bool {
		var names []string
		for _, name := range strings.Split(params["Choices"], ",") {
			if name = strings.TrimSpace(name); name != "" {
				names = append(names, name)
			}
		}
		if len(names) != 2 {
			return false
		}
		first := effects.CharmModeLabel(cards.ResolveSVar(f.SVars, names[0]), names[0])
		second := effects.CharmModeLabel(cards.ResolveSVar(f.SVars, names[1]), names[1])
		if !firstIsTrue {
			first, second = second, first
		}
		pairs = append(pairs, [2]string{first, second})
		return true
	}
	for _, sa := range f.Abilities {
		if sa.API == "GenericChoice" && !add(sa.Params) {
			return nil, false
		}
	}
	// Sorted SVar order, and one GenericChoice body per listed face: two
	// bodies could share a label, and map order would pick the winner.
	keys := make([]string, 0, len(f.SVars))
	for k := range f.SVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if p := svarParams(f.SVars[k]); p["DB"] == "GenericChoice" && !add(p) {
			return nil, false
		}
	}
	return pairs, len(pairs) == 1
}
