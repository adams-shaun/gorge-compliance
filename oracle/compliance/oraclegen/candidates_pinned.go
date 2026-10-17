package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// The corpus-order subtype pickers (registrySubtype, registryQuietSubtype)
// return the first registry card that carries the subtype AND that XMage's
// database holds (XMageKnown). Both halves are global state, not the card
// under test: XMageKnown is the union of every committed set manifest, so a
// new set's manifest moves the pick for every unrelated row, and a unit test
// that installs no manifests sees another card again. Rydia, Summoner of
// Mist's activate#0.0 fixture measured all three (Age of Ultron in the pass
// once MSH's manifest landed, A Golden Opportunity in a test registry, none
// for a lone manifest).
//
// The pins below make a subtype's fixture a function of the subtype alone:
// each is the card the scan returned for the 20 printed sets' level-B
// generation at the time of pinning (measured 2026-10-13, so no committed
// scenario moved), and it is used whenever it is in the registry and still
// passes the picker's own predicate. A subtype with no pin, or a pin the
// registry cannot serve, keeps the corpus-order scan.
var pinnedSubtypeFixtures = map[string]string{
	"bat":        "Aclazotz, Deepest Betrayal",
	"cave":       "Captivating Cave",
	"egg":        "Biolume Egg",
	"elf":        "Aberrant Mind Sorcerer",
	"equipment":  "Accorder's Shield",
	"gate":       "Azorius Guildgate",
	"goblin":     "Adder-Staff Boggart",
	"mount":      "Alacrian Jaguar",
	"room":       "Bottomless Pool",
	"saga":       "Age of Ultron",
	"spacecraft": "Atmospheric Greenhouse",
	"town":       "Adventurer's Inn",
	"vampire":    "Acolyte of Aclazotz",
	"vehicle":    "Adrestia",
	"villain":    "A.I.M. Bot",
	"zombie":     "Abattoir Ghoul",
}

// pinnedQuietSubtypeFixtures is registryQuietSubtype's table, measured the
// same way (plus Mutant, the MayPlay rider's creature-subtype probe).
var pinnedQuietSubtypeFixtures = map[string]string{
	"ally":      "Affa Protector",
	"artifact":  "A.I.M. Bot",
	"bear":      "Alpine Grizzly",
	"creature":  "A.I.M. Bot",
	"detective": "Forensic Researcher",
	"dwarf":     "Aerial Responder",
	"elemental": "Aeromoeba",
	"elf":       "Accomplished Alchemist",
	"frog":      "Anurid Brushhopper",
	"goblin":    "Akki Avalanchers",
	"hero":      "Brave Brawler",
	"human":     "Abbey Matron",
	"kithkin":   "Amrou Scout",
	"lizard":    "Basking Rootwalla",
	"merfolk":   "Arctic Merfolk",
	"mouse":     "Brave-Kin Duo",
	"mutant":    "Aeromunculus",
	"ninja":     "Blade-Blizzard Kitsune",
	"otter":     "Sledding Otter-Penguin",
	"pirate":    "Angrath's Marauders",
	"rat":       "Chittering Skitterling",
	"skeleton":  "Asphodel Wanderer",
	"spider":    "Ancient Spider",
	"treasure":  "Goldhound",
	"vampire":   "Acolyte of Aclazotz",
	"villain":   "A.I.M. Bot",
	"wizard":    "Aerie Mystics",
}

// pinnedFixture returns the pin for subtype when the registry holds it and
// its front face passes keep (the picker's own predicate, XMageKnown
// included); false sends the caller to its corpus-order scan.
func pinnedFixture(reg *cards.Registry, pins map[string]string, subtype string, keep func(*cards.Card) bool) (string, bool) {
	name, ok := pins[strings.ToLower(strings.TrimSpace(subtype))]
	if !ok {
		return "", false
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 || c.Faces[0].Name != name || !keep(c) {
		return "", false
	}
	return name, true
}
