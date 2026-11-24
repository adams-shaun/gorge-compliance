package oraclegen

import (
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
// poses as ONE boolean chooseUse, not a mode or a Choice dialog. The FIRST
// Choices$ label is the true branch and the second the false branch, so the
// pick must be answered as "yes"/"no" for the seat that chose -- the targeted
// opponent on the "target opponent may have you X; if they don't, Y" shape
// (Terrapact Intimidator), the controller on the card's own election.
//
// Membership is proven against the XMage source at XMAGE_REF fdde07931f, one
// chooseUse per entry with the first Forge label on its true branch:
//
//	BaneLordOfDarkness       "Have <controller> draw a card or put a creature into play?" ("Draw card"/"Play creature")
//	CombustibleGearhulk      "Have <controller> draw three cards?"                    (draw three / mill and damage)
//	CovenantOfMinds          "Put the revealed cards into <controller>'s hand?"       (to hand / graveyard and draw five)
//	DecoyGambit              "Have <controller> draw a card? If you don't, ..."       (draw / return to hand)
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
// TestChooseUseFacesModeNumbersCensus counts the corpus faces so a new entry is
// a deliberate, source-proven edit.
var xmageChooseUseFaces = map[string]bool{
	"bane, lord of darkness":    true,
	"combustible gearhulk":      true,
	"covenant of minds":         true,
	"decoy gambit":              true,
	"distant memories":          true,
	"inspirit, flagship vessel": true,
	"liar's pendulum":           true,
	"palantír of orthanc":       true,
	"sin prodder":               true,
	"soul echo":                 true,
	"terrapact intimidator":     true,
}

// chooseUseLabels lists the [yes, no] label pairs of every two-option
// GenericChoice on a face in xmageChooseUseFaces, keyed as the engine labels
// its mode picks (effects.CharmModeLabel). nil for any other face. ok is false
// when a listed face has no GenericChoice body or one that does not offer
// exactly two choices, so a listed face cannot silently lose one side.
func chooseUseLabels(f *cards.Face) (pairs [][2]string, ok bool) {
	if f == nil || !xmageChooseUseFaces[strings.ToLower(f.Name)] {
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
		pairs = append(pairs, [2]string{
			effects.CharmModeLabel(cards.ResolveSVar(f.SVars, names[0]), names[0]),
			effects.CharmModeLabel(cards.ResolveSVar(f.SVars, names[1]), names[1]),
		})
		return true
	}
	for _, sa := range f.Abilities {
		if sa.API == "GenericChoice" && !add(sa.Params) {
			return nil, false
		}
	}
	for _, body := range f.SVars {
		if p := svarParams(body); p["DB"] == "GenericChoice" && !add(p) {
			return nil, false
		}
	}
	return pairs, len(pairs) > 0
}
