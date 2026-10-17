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
