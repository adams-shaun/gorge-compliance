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
// fdde07931f (the current pin b964ce92f0 differs only in PlayerImpl.java and
// WaterbendTest.java, no card class), one chooseUse per entry (true branch in
// the last column's first parenthesis side unless noted):
//
//	AbundantHarvest          chooseUse("Choose land or nonland", "Land", "Nonland")   (DigLand / DigNonland) -- added at XMAGE_REF 7aeb547a86
//	BaneLordOfDarkness       "Have <controller> draw a card or put a creature into play?" ("Draw card"/"Play creature")
//	CombustibleGearhulk      "Have <controller> draw three cards?"                    (draw three / mill and damage)
//	CovenantOfMinds          "Put the revealed cards into <controller>'s hand?"       (to hand / graveyard and draw five)
//	DecoyGambit              "Have <controller> draw a card? If you don't, ..."       (return to hand / draw) -- TRUE = draw = the SECOND label
//	DistantMemories          "Have <controller> put <card> in their hand?"            (to hand / leave exiled)
//	InspiritFlagshipVessel   "Put a +1/+1 counter or two charge counters on ...?"     ("one +1/+1 counter"/"two charge counters")
//	LiarsPendulum            "Is the chosen card (...) in <controller>'s hand?"       (guess in / guess not in)
//	PalantirOfOrthanc        "Have <controller> draw a card?"                         (draw / mill and lose life)
//	PetalsOfInsight          "Put the cards on the bottom of your library in any order?" (bottom and return / draw three)
//	MultiformWonder          "Should <card> get -2/+2 instead of +2/-2?"              (+2/-2 / -2/+2) -- TRUE = -2/+2 = the SECOND label; added at XMAGE_REF 7aeb547a86
//	SparkOfCreativity        "Let <card> deal <n> damage to <creature>?"              (deals damage / may play the card) -- added at XMAGE_REF 7aeb547a86
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
	"abundant harvest":          true,
	"bane, lord of darkness":    true,
	"combustible gearhulk":      true,
	"covenant of minds":         true,
	"decoy gambit":              false, // XMage true = draw = the second Choices$ label
	"distant memories":          true,
	"inspirit, flagship vessel": true,
	"liar's pendulum":           true,
	"multiform wonder":          false, // XMage true = -2/+2 = the second Choices$ label
	"palantír of orthanc":       true,
	"petals of insight":         true,
	"sin prodder":               true,
	"spark of creativity":       true,
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

// xmageKWChoiceDialogFaces names the faces whose Pump KWChoice$ XMage poses as
// ONE Choice dialog over the candidate abilities' RULE TEXTS
// (GainsChoiceOfAbilitiesEffect: new ChoiceImpl(true) with the message
// "Choose an ability to gain" and the choices abilityMap.keySet(), where each
// key is CardUtil.stripReminderText(ability.getRule())). TestPlayer reads that
// ask from the CHOICE queue by label; scripted on the numeric mode queue the
// answer is never consumed and XMage's own choice wins (Lunar Avenger
// measured: "Missing CHOICE def ... Choose an ability to gain / Possible
// choices: flying, haste, first strike", with mode 1 left unused).
//
// Membership is proven against the XMage source, not guessed: at XMAGE_REF
// fdde07931f exactly these 26 Mage.Sets card classes pass keyword abilities to
// GainsChoiceOfAbilitiesEffect, and each class's ability list is the same
// keyword set as the face's Forge KWChoice$ line (one Pump per face). The
// current pin b964ce92f0 adds only PlayerImpl.java (the Waterbend playable
// check) and WaterbendTest.java over fdde07931f and touches no Mage.Sets
// class, so the 26-class set cannot move:
//
//	AlchemistsGift, ArgivianAvenger, AssassinInitiate, AtraxasSkitterfang,
//	BallroomBrawlers, ButcherOfTheHorde, EzrimAgencyChief, GabrielAngelfire,
//	GideonBlackblade, GolemArtisan, HungeringPuppetbeast, HuntersAxe,
//	JodahsAvenger, LunarAvenger, ManifoldMouse, MultiformWonder,
//	OrcishMedicine, PracticedOffense, RattlebackApothecary, ShiftingCeratops,
//	SilverquillPledgemage, SteelSeraph, UrzasAvenger, VeteranWarleader,
//	ViperCruelConspirator, WingnutBatOnTheBelfry.
//
// The corpus's other six KWChoice$ faces (Angelic Skirmisher, Éowyn Lady of
// Rohan, Linvala Shield of Sea Gate, Mardu Thunderkite, Smogbelcher Chariot,
// Swiftspear's Teachings) have no GainsChoiceOfAbilitiesEffect implementation
// at the pin, so they are NOT listed here: Angelic Skirmisher's bespoke Choice
// is in xmageKWChoiceBespokeFaces; the rest are a different ask or absent.
// TestKWChoiceDialogFacesCensus counts the table and pins each label against an
// independently written XMage-label table.
var xmageKWChoiceDialogFaces = map[string]bool{
	"alchemist's gift":           true,
	"argivian avenger":           true,
	"assassin initiate":          true,
	"atraxa's skitterfang":       true,
	"ballroom brawlers":          true,
	"butcher of the horde":       true,
	"ezrim, agency chief":        true,
	"gabriel angelfire":          true,
	"gideon blackblade":          true,
	"golem artisan":              true,
	"hungering puppetbeast":      true,
	"hunter's axe":               true,
	"jodah's avenger":            true,
	"lunar avenger":              true,
	"manifold mouse":             true,
	"multiform wonder":           true,
	"orcish medicine":            true,
	"practiced offense":          true,
	"rattleback apothecary":      true,
	"shifting ceratops":          true,
	"silverquill pledgemage":     true,
	"steel seraph":               true,
	"urza's avenger":             true,
	"veteran warleader":          true,
	"viper, cruel conspirator":   true,
	"wingnut, bat on the belfry": true,
}

// kwChoiceDialogLabels lists every KWChoice$ candidate label of the Pumps on a
// face in xmageKWChoiceDialogFaces, exactly as gorge's pump decision shows them
// (the raw KWChoice$ entry: "Flying", "First Strike", "Rampage:3"). nil for any
// other face.
func kwChoiceDialogLabels(f *cards.Face) []string {
	if f == nil || !xmageKWChoiceDialogFaces[strings.ToLower(f.Name)] {
		return nil
	}
	return pumpKWChoiceLabels(f)
}

// xmageKWChoiceBespokeFaces names the faces whose Pump KWChoice$ XMage poses
// as ONE bespoke Choice dialog (the card class builds its own ChoiceImpl, not
// GainsChoiceOfAbilitiesEffect), read from the CHOICE queue by label like the
// xmageKWChoiceDialogFaces. The value maps each KWChoice$ candidate to its
// XMage label, read from the card class at XMAGE_REF 7aeb547a86:
//
//	AngelicSkirmisher  message "Choose ability for your creatures", choices
//	                   "First strike", "Vigilance", "Lifelink" (Forge
//	                   KWChoice$ First Strike,Vigilance,Lifelink)
//
// The other five faces stay out, each checked at the pin:
//
//	EowynLadyOfRohan       chooseUse("Choose first strike or vigilance") on the
//	                       unequipped branch, no choice at all when equipped
//	LinvalaShieldOfSeaGate chooseUse("Choose hexproof or indestructible")
//	MarduThunderkite, SmogbelcherChariot, SwiftspearsTeachings
//	                       no Mage.Sets class at the pin
//
// The labels differ from gorge's only in case ("First Strike" -> "First
// strike"), which xmageBespokeChoiceLabel maps; the census test pins every
// routed label against these values.
var xmageKWChoiceBespokeFaces = map[string]map[string]string{
	"angelic skirmisher": {
		"First Strike": "First strike",
		"Vigilance":    "Vigilance",
		"Lifelink":     "Lifelink",
	},
}

// kwChoiceBespokeLabels lists the KWChoice$ candidate labels of a face in
// xmageKWChoiceBespokeFaces; nil for any other face.
func kwChoiceBespokeLabels(f *cards.Face) []string {
	if f == nil || xmageKWChoiceBespokeFaces[strings.ToLower(f.Name)] == nil {
		return nil
	}
	return pumpKWChoiceLabels(f)
}

// xmageBespokeChoiceLabel is the label XMage's bespoke KWChoice dialogs show
// for a gorge KWChoice$ label: sentence case ("First Strike" -> "First
// strike"). The decision carries only the label, so the mapping is by shape;
// the census pins it to xmageKWChoiceBespokeFaces.
func xmageBespokeChoiceLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return label
	}
	return strings.ToUpper(label[:1]) + strings.ToLower(label[1:])
}

// pumpKWChoiceLabels lists every KWChoice$ entry of the face's Pumps.
func pumpKWChoiceLabels(f *cards.Face) []string {
	var out []string
	add := func(params map[string]string) {
		for _, name := range strings.Split(params["KWChoice"], ",") {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, name)
			}
		}
	}
	for _, sa := range f.Abilities {
		if sa.API == "Pump" {
			add(sa.Params)
		}
	}
	// Sorted SVar order: more than one Pump body must not let map order decide
	// the answer order.
	keys := make([]string, 0, len(f.SVars))
	for k := range f.SVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if p := svarParams(f.SVars[k]); p["DB"] == "Pump" {
			add(p)
		}
	}
	return out
}

// xmageKeywordRuleText is the label XMage's GainsChoiceOfAbilitiesEffect shows
// for a KWChoice$ candidate: the keyword ability's rule text. XMage's keyword
// abilities spell their rule text in lower case (FlyingAbility.getRule() ==
// "flying"), and a parameterised keyword writes the parameter after a space
// (RampageAbility(3).getRule() == "rampage 3"), so the Forge "Rampage:3"
// spelling maps across. The set of corpus KWChoice$ labels is closed
// (Banding, Deathtouch, Double Strike, First Strike, Flying, Haste, Hexproof,
// Indestructible, Lifelink, Menace, Protection from red, Prowess, Rampage:3,
// Reach, Shadow, Trample, Vigilance), and the census pins every routed one.
func xmageKeywordRuleText(label string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(label), ":", " "))
}
