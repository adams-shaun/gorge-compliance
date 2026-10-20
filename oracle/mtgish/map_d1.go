package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit D1: trigger event classes that had no mapping on either side, the
// gorge Forge trigger modes (T:Mode$) and the mtgish Trigger variants, mapped
// together so the two spellings meet on one class. A class already in the
// vocabulary (etb, ltb, zone, counters, ...) is reused wherever the event is
// the same; a new class is the lowercased Forge mode, with the batching
// suffixes Once/All dropped the way CounterAddedOnce/DiscardedAll already
// share counters/discard.

// d1GorgeModes maps a Forge trigger mode whose class does not depend on its
// parameters.
var d1GorgeModes = map[string]string{
	// Planechase, Archenemy, Attractions and stickers are real triggers on
	// real cards; they compare like any other event.
	"ChaosEnsues":      "chaosensues",
	"PlaneswalkedTo":   "planeswalkedto",
	"PlaneswalkedFrom": "planeswalkedfrom",
	"PlanarDice":       "planardice",
	"SetInMotion":      "setinmotion",
	"VisitAttraction":  "visit",
	"ClaimPrize":       "claimprize",
	"StickerPlaced":    "stickerplaced",

	"TurnFaceUp":          "turnfaceup",
	"Cycled":              "cycled",
	"Specializes":         "specializes",
	"LandPlayed":          "landplayed",
	"Transformed":         "transformed",
	"Mutates":             "mutates",
	"UnlockDoor":          "unlockdoor",
	"FullyUnlock":         "fullyunlock",
	"Scry":                "scry",
	"Surveil":             "surveil",
	"Exploited":           "exploited",
	"CommitCrime":         "commitcrime",
	"BecomeMonstrous":     "becomemonstrous",
	"BecomeRenowned":      "becomerenowned",
	"DayTimeChanges":      "daytimechanges",
	"Attached":            "attached",
	"Unattached":          "unattached",
	"RingTemptsYou":       "ringtemptsyou",
	"Crewed":              "crewed",
	"BecomesCrewed":       "becomescrewed",
	"Saddled":             "saddled",
	"BecomesSaddled":      "becomessaddled",
	"LosesGame":           "losesgame",
	"Proliferate":         "proliferate",
	"ChangesController":   "changescontroller",
	"Clashed":             "clashed",
	"Explores":            "explores",
	"Exerted":             "exerted",
	"NewGame":             "newgame",
	"BecomeMonarch":       "becomemonarch",
	"DungeonCompleted":    "dungeoncompleted",
	"SearchedLibrary":     "searchedlibrary",
	"Shuffled":            "shuffled",
	"Enlisted":            "enlisted",
	"Connives":            "connives",
	"Vote":                "vote",
	"BecomesPlotted":      "becomesplotted",
	"CollectEvidence":     "collectevidence",
	"Discover":            "discover",
	"Evolved":             "evolved",
	"FacesDilemma":        "facesdilemma",
	"Investigated":        "investigated",
	"ManaAdded":           "manaadded",
	"AbilityResolves":     "abilityresolves",
	"AbilityTriggered":    "abilitytriggered",
	"CaseSolved":          "casesolved",
	"Championed":          "championed",
	"Devoured":            "devoured",
	"ElementalBend":       "elementalbend",
	"Foretell":            "foretell",
	"ManifestDread":       "manifestdread",
	"Mentored":            "mentored",
	"PayEcho":             "payecho",
	"PayLife":             "paylife",
	"Stationed":           "stationed",
	"Trains":              "trains",
	"Destroyed":           "destroyed",
	"Countered":           "countered",
	"PhaseIn":             "phasein",
	"PayCumulativeUpkeep": "paycumulativeupkeep",
	"DamagePreventedOnce": "damageprevented",
	"AbilityCast":         "abilitycast",
	"TurnBegin":           "turnbegin",
	"Abandoned":           "abandoned",

	// Batching variants share the single event's class.
	"RolledDie":             "rolleddie",
	"RolledDieOnce":         "rolleddie",
	"FlippedCoin":           "flippedcoin",
	"FlippedCoinOnce":       "flippedcoin",
	"CounterRemoved":        "counterremoved",
	"CounterRemovedOnce":    "counterremoved",
	"Milled":                "milled",
	"MilledOnce":            "milled",
	"MilledAll":             "milled",
	"TokenCreated":          "tokencreated",
	"TokenCreatedOnce":      "tokencreated",
	"PhaseOut":              "phaseout",
	"PhaseOutAll":           "phaseout",
	"Fight":                 "fight",
	"FightOnce":             "fight",
	"ConjureAll":            "conjure",
	"SeekAll":               "seek",
	"AttackerUnblocked":     "unblocked",
	"AttackerUnblockedOnce": "unblocked",

	// Existing classes: the same event under another Forge mode.
	"CounterPlayerAddedAll": "counters",
	"CounterTypeAddedAll":   "counters",
	"AttackerBlockedOnce":   "blocked",
	"BlockersDeclared":      "blocks",
	"TapAll":                "tap",
	"UntapAll":              "untap",
	// Excess damage is a damage event with a filter on the amount, the way
	// mtgish's excess-damage variants already class as damage.
	"ExcessDamage":    "damage",
	"ExcessDamageAll": "damage",
}

// d1Phase maps the Phase$ values (and phase-less first-upkeep flags) the
// Phase case does not name. Forge reads phase names case-insensitively.
func d1Phase(p map[string]string) string {
	switch strings.ToLower(p["Phase"]) {
	case "endcombat":
		return "end-combat"
	case "end of turn":
		return "end-step"
	case "":
		if p["FirstUpkeep"] == "True" || p["FirstUpkeepThisGame"] == "True" {
			return "upkeep"
		}
	}
	return ""
}

// d1Bookkeeping reports a trigger that is engine bookkeeping rather than a
// printed ability: it has no TriggerDescription$, so it is never shown.
// Across the corpus these are the Static$ True reset/remember/track lines
// and the undescribed halves behind a static or another trigger (Execute$
// DBCleanup, DBForget, TrigReset, StoreVar, ...). It maps to nothing, the
// way mtgish has no node for it. A keyword's expansion carries no
// description of its own and is kept. A Static$ True Secondary$ True line
// is bookkeeping too even when described: in the corpus each one forgets
// remembered cards or is the trigger half of a static permission ("you may
// cast ... from your graveyard; if you do, ...").
func d1Bookkeeping(p map[string]string) bool {
	if _, _, ok := kwTag(p); ok {
		return false
	}
	if p["Static"] == "True" && p["Secondary"] == "True" {
		return true
	}
	return p["TriggerDescription"] == ""
}

// gorgeTriggerEvents is the event classes of one T: line: usually one,
// none for bookkeeping, two for a spell-or-ability trigger (one ability per
// event, the way mtgish splits an Or).
func gorgeTriggerEvents(mode string, p map[string]string) []string {
	if d1Bookkeeping(p) {
		return nil
	}
	switch mode {
	case "SpellAbilityCast", "SpellAbilityCopy":
		return spellAbilityEvents(p["ValidSA"])
	case "ChangesZone", "ChangesZoneAll":
		if strings.Contains(p["Destination"], ",") {
			return changesZoneEvents(mode, p)
		}
	}
	return []string{gorgeTriggerClass(mode, p)}
}

// spellAbilityEvents splits a SpellAbilityCast ValidSA$ into the spell half
// (cast) and the activated half (abilitycast).
func spellAbilityEvents(validSA string) []string {
	spell, ability := false, false
	for _, alt := range strings.Split(validSA, ",") {
		head, _, _ := strings.Cut(strings.TrimSpace(alt), ".")
		switch head {
		case "Activated", "Ability":
			ability = true
		case "SpellAbility", "":
			spell, ability = true, true
		default: // Spell, Instant, Sorcery, Creature, ...: a spell filter
			spell = true
		}
	}
	var out []string
	if spell {
		out = append(out, "cast")
	}
	if ability {
		out = append(out, "abilitycast")
	}
	return out
}

// changesZoneEvents splits a multi-destination ChangesZone into one class
// per destination: "dies or is put into exile" (Graveyard,Exile from the
// battlefield) is dies plus ltb, the two abilities mtgish writes for it, or
// the two events of its Or.
func changesZoneEvents(mode string, p map[string]string) []string {
	q := make(map[string]string, len(p))
	for k, v := range p {
		q[k] = v
	}
	var out []string
	for _, d := range strings.Split(p["Destination"], ",") {
		q["Destination"] = strings.TrimSpace(d)
		out = append(out, gorgeTriggerClass(mode, q))
	}
	return dedup(out)
}

// d1Exiled classes Forge's Exiled mode by origin: from the battlefield it is
// a permanent leaving, as ChangesZone Battlefield>Exile is.
func d1Exiled(p map[string]string) string {
	if p["Origin"] == "Battlefield" {
		return "ltb"
	}
	return "zone"
}

// d1MtgishClasses maps the mtgish Trigger variants triggerClass's word
// checks do not reach.
var d1MtgishClasses = map[string]string{
	"WhenChaosEnsues":                             "chaosensues",
	"WhenAPlayerPlaneswalksToAPlane":              "planeswalkedto",
	"WhenAPlayerEncountersAPhenomenon":            "planeswalkedto", // you encounter a phenomenon by planeswalking to it; Forge: PlaneswalkedTo
	"WhenAPlayerPlaneswalksAwayFromAPlane":        "planeswalkedfrom",
	"WhenAPlayerRollsThePlanarDie":                "planardice",
	"WhenAPlayerRollsABlankOnThePlanarDie":        "planardice",
	"WhenAPlayerSetsASchemeInMotion":              "setinmotion",
	"WhenAPlayerVisitsAnAttraction":               "visit", // the event an Attraction's own Visit ability triggers on
	"WhenAPlayerClaimsThePrizeOfAnAttraction":     "claimprize",
	"WhenAPlayerOpensAnAttraction":                "etb", // an Attraction put onto the battlefield; Forge: ChangesZone AttractionDeck>Battlefield
	"WhenAPlayerPutsAStickerOnAPermanent":         "stickerplaced",
	"WhenAPlayerPutsANameStickerOnAPermanent":     "stickerplaced",
	"WhenAPlayerPutsAnAbilityStickerOnAPermanent": "stickerplaced",
	"WhenAPlayerPutsAnArtStickerOnAPermanent":     "stickerplaced",
	"WhenAPlayerPlacesASticker":                   "stickerplaced",

	"WhenAPermanentIsTurnedFaceUp":                                         "turnfaceup",
	"WhenAPlayerTurnsAPermanentFaceUp":                                     "turnfaceup",
	"WhenAPlayerCyclesACard":                                               "cycled",
	"WhenAPlayerCyclesACardForTheFirstTimeEachTurn":                        "cycled",
	"WhenACreatureSpecializes":                                             "specializes",
	"WhenAGraveyardCardSpecializes":                                        "specializes",
	"WhenCardSpecializes":                                                  "specializes",
	"WhenAPlayerPlaysALand":                                                "landplayed",
	"WhenAPlayerPlaysALandFromExile":                                       "landplayed",
	"WhenAPlayerPlaysALandFromAmongCardsInExile":                           "landplayed",
	"WhenAPlayerPlaysALandFromAnywhereOtherThanTheirHand":                  "landplayed",
	"WhenAPermanentTransforms":                                             "transformed",
	"WhenAPermanentTransformsFromIntoAPermanent":                           "transformed",
	"WhenACreatureMutates":                                                 "mutates",
	"WhenAPlayerUnlocksADoor":                                              "unlockdoor",
	"WhenAPlayerFullyUnlocksARoom":                                         "fullyunlock",
	"WhenAPlayerScrys":                                                     "scry",
	"WhenAPlayerChoosesToPutAnyCardsOnTheBottomOfTheirLibraryWhileScrying": "scry",
	"WhenAPlayerSurveils":                                                  "surveil",
	"WhenAPlayerSurveilsForTheFirstTimeEachTurn":                           "surveil",
	"WhenAPermanentExploitsAPermanent":                                     "exploited",
	"WhenAPlayerCommitsACrime":                                             "commitcrime",
	"WhenAPermanentBecomesMonstrous":                                       "becomemonstrous",
	"WhenACreatureBecomesRenowned":                                         "becomerenowned",
	"WhenDayBecomesNightOrNightBecomesDay":                                 "daytimechanges",
	"WhenAPermanentBecomesAttachedToAPermanent":                            "attached",
	"WhenAPermanentBecomesUnattachedFromAPermanent":                        "unattached",
	"WhenTheRingTemptsAPlayer":                                             "ringtemptsyou",
	"WhenAPlayerChoosesARingBearer":                                        "ringtemptsyou", // the choice is part of being tempted; Forge: RingTemptsYou
	"WhenACreatureCrewsAVehicle":                                           "crewed",
	"WhenAVehicleBecoemsCrewed":                                            "becomescrewed",
	"WhenAVehicleBecoemsCrewedForTheFirstTimeEachTurn":                     "becomescrewed",
	"WhenACreatureSaddlesAMount":                                           "saddled",
	"WhenAPermanentBecomesSaddledForTheFirstTimeInATurn":                   "becomessaddled",
	"WhenAPlayerLosesTheGame":                                              "losesgame",
	"WhenAPlayerProliferates":                                              "proliferate",
	"WhenAPlayerGainsControlOfAPermanentFromAPlayer":                       "changescontroller",
	"WhenAPlayerClashes":                                                   "clashed",
	"WhenAPlayerClashesAndWins":                                            "clashed",
	"WhenAPermanentExplores":                                               "explores",
	"WhenAPermanentExploresACardOfType":                                    "explores",
	"WhenAPlayerExertsACreature":                                           "exerted",
	"AtTheBeginningOfTheGame":                                              "newgame",
	"WhenAPlayerBecomesTheMonarch":                                         "becomemonarch",
	"WhenAPlayerCompletesADungeon":                                         "dungeoncompleted",
	"WhenAPlayerSearchesTheirLibrary":                                      "searchedlibrary",
	"WhenAPlayerShufflesTheirLibrary":                                      "shuffled",
	"WhenASpellOrAbilityCausesAPlayerToShuffleTheirLibrary":                "shuffled",
	"WhenASpellOrAbilityCausesItsControllerToShuffleTheirLibrary":          "shuffled",
	"WhenACreatureEnlistsACreature":                                        "enlisted",
	"WhenAPermanentConnives":                                               "connives",
	"WhenPlayersFinishVoting":                                              "vote",
	"WhenACardBecomesPlotted":                                              "becomesplotted",
	"WhenAPlayerCollectsEvidence":                                          "collectevidence",
	"WhenAPlayerDiscovers":                                                 "discover",
	"WhenAPermanentEvolves":                                                "evolved",
	"WhenAPlayerFacesADilemma":                                             "facesdilemma",
	"WhenAPlayerSelectsAModeAtRandom":                                      "facesdilemma", // choosing modes is facing a dilemma; Forge: FacesDilemma Random$ True
	"WhenAPlayerInvestigates":                                              "investigated",
	"WhenAPlayerInvestigatesForTheFirstTimeEachTurn":                       "investigated",
	"WhenAnAbilityCausesAPlayerToAddMana":                                  "manaadded",
	"WhenTheFinalChapterOfASagaResolves":                                   "abilityresolves",
	"WhenAManaAbilityOfAPermanentResolves":                                 "abilityresolves",
	"WhenTheFinalChapterOfASagaTriggers":                                   "abilitytriggered",
	"WhenAPermanentEnteringTheBattlefieldCausesAnAbilityToTrigger":         "abilitytriggered",
	"WhenAPermanentEnteringTheBattlefieldUnderAPlayersControlCausesItsAbilityToTrigger": "abilitytriggered",
	"WhenAPlayerSolvesACase":                   "casesolved",
	"WhenAPermanentIsChampionedWithAPermanent": "championed",
	"WhenACreatureIsDevoured":                  "devoured",
	"WhenAPlayerWaterEarthFireOrAirBends":      "elementalbend",
	"WhenAPlayerForetellsACard":                "foretell",
	"WhenAPlayerManifestsDread":                "manifestdread",
	"WhenACreatureMentorsACreature":            "mentored",
	"WhenAnEchoCostOfAPermanentIsPaid":         "payecho",
	"WhenAPlayerPaysLife":                      "paylife",
	"WhenAPermanentStationsAPermanent":         "stationed",
	"WhenAPermanentTrains":                     "trains",
	"WhenAPermanentIsDestroyed":                "destroyed",
	"WhenASpellOrAbilityDestroysAPermanent":    "destroyed",
	"WhenASpellIsCountered":                    "countered",
	"WhenAPermanentPhasesIn":                   "phasein",
	"WhenAPermanentPhasesOut":                  "phaseout",
	"WhenAnyNumberOfPermanentsPhaseOut":        "phaseout",
	"WhenAPlayerActivatesAnAbility":            "abilitycast",
	"WhenAnAbilityIsActivated":                 "abilitycast",
	"WhenAPlayerPaysLifeToActivateAnAbility":   "abilitycast", // Forge: AbilityCast Condition$ LifePaid

	"WhenAPlayerRollsADie":                                       "rolleddie",
	"WhenAPlayerRollsAValueOnADie":                               "rolleddie",
	"WhenAPlayerRollsAnyNumberOfDice":                            "rolleddie",
	"WhenAPlayerRollsANatural20":                                 "rolleddie",
	"WhenAPlayerRollsTheirNthDieEachTurn":                        "rolleddie",
	"WhenAPlayerRollsToVisitTheirAttractions":                    "rolleddie", // a d6 roll; Forge: RolledDie RolledToVisitAttractions$ True
	"WhenAPlayerWinsACoinFlip":                                   "flippedcoin",
	"WhenAPlayerLosesACoinFlip":                                  "flippedcoin",
	"WhenAPlayerFlipsAnyNumberOfCoins":                           "flippedcoin",
	"WhenACounterOfTypeIsRemovedFromAnExiledCard":                "counterremoved",
	"WhenACounterOfTypeIsRemovedFromAPermanent":                  "counterremoved",
	"WhenTheLastCounterOfTypeIsRemovedFromAnExiledCard":          "counterremoved",
	"WhenTheLastCounterOfTypeIsRemovedFromAPermanent":            "counterremoved",
	"WhenAPlayerRemovesACounterOfTypeFromAnExiledCard":           "counterremoved",
	"WhenAPlayerRemovesTheLastCounterOfTypeFromAPermanent":       "counterremoved",
	"WhenAPlayerMillsASpecificCard":                              "milled",
	"WhenAPlayerMillsAnyNumberOfCards":                           "milled",
	"WhenAPlayerMillsAnyNumberOfSpecificCards":                   "milled",
	"WhenAnyNumberOfSpecificCardsAreMilled":                      "milled",
	"WhenAPlayerCreatesAToken":                                   "tokencreated",
	"WhenAPlayerCreatesAnyNumberOfTokens":                        "tokencreated",
	"WhenAPlayerCreatesAnyNumberOfTokensForTheFirstTimeEachTurn": "tokencreated",
	"WhenAPermanentFights":                                       "fight",
	"WhenAnyNumberOfPermanentsFight":                             "fight",
	"WhenAPlayerConjuresAnyNumberOfCards":                        "conjure",
	"WhenAPlayerConjuresAnyNumberOfOtherCards":                   "conjure",
	"WhenAPlayerSeeksAnyNumberOfCards":                           "seek",

	// Existing classes.
	"AtTheEndOfCombat":                                                      "end-combat",
	"AtTheEndOfTheFirstCombat":                                              "end-combat",
	"WhenAnyNumberOfCreaturesOrPlaneswalkersDie":                            "dies",
	"WhenAPermanentIsSacrificed":                                            "sacrifice",
	"WhenAnyNumberOfCreaturesBecomeBlocked":                                 "blocked",
	"WhenAnyNumberOfCreaturesBlock":                                         "blocks",
	"WhenAPlayerTapsAPermanent":                                             "tap",
	"WhenAnyNumberOfPermanentsBecomeTapped":                                 "tap",
	"WhenAPlayerUntapsAnyNumberOfPermanentDuringTheirUntapStep":             "untap",
	"WhenAPlayerRevealsFirstCardDrawn":                                      "draw", // Forge: Drawn of the revealed first card
	"WhenAPlayerKicksASpell":                                                "cast",
	"WhenAPlayerChoosesTargetsForASpellOrAbility":                           "becomes-target",
	"WhenAnyNumberOfPlayersAndOrPermanentsBecomeTheTargetOfASpellOrAbility": "becomes-target",
	"WhenAPlayerSelectsAnyNumberOfTargetsAtRandom":                          "becomes-target", // Forge: BecomesTargetOnce Random$ True
	"WhenASpellOrAbilityCausesAPlayerToDiscardACard":                        "discard",
	"WhenASpellOrAbilityCausesAPlayerToDiscardAnyNumberOfCards":             "discard",
	"WhenAnyNumberOfPlayersDiscardAnyNumberOfCards":                         "discard",
	"WhenAPlayerPutsAPermanentOnTheBattlefield":                             "etb",
	// Counters put on a permanent or a player (energy).
	"WhenTheNthCounterOfTypeIsPutOnAPermanent":  "counters",
	"WhenACounterOfTypeIsPutOnAPermanent":       "counters",
	"WhenAPlayerPutsACounterOfTypeOnAPermanent": "counters",
	"WhenAPlayerGetsEnergy":                     "counters",
	// A permanent leaving the battlefield for a zone other than a graveyard.
	"WhenAPermanentIsExiled": "ltb",
	"WhenAPermanentIsExiledFromTheBattlefieldWhileAPlayerIsActivatingACraftAbility": "ltb",
	"WhenAPermanentIsPutIntoTheCommandZone":                                         "ltb",
	"WhenAPermanentIsReturnedToAPlayersHand":                                        "ltb",
	"WhenAnyNumberOfPermanentsAreReturnedToHand":                                    "ltb",
	"WhenASpellOrAbilityExilesAnyNumberOfPermanents":                                "ltb",
	// A card moving between zones off the battlefield (gorge: ChangesZone
	// with neither side the battlefield nor a graveyard destination).
	"WhenAnyNumberOfGraveyardCardsLeave":                                    "zone",
	"WhenAGraveyardCardLeaves":                                              "zone",
	"WhenAGraveyardCardIsPutIntoHand":                                       "zone",
	"WhenASpecificCardIsPutIntoAPlayersHandFromTheirLibrary":                "zone",
	"WhenAnyNumberOfCardsArePutIntoAPlayersLibraryFromAnywhere":             "zone",
	"WhenAnyNumberOfCardsArePutIntoExile":                                   "zone",
	"WhenAnyNumberOfCardsArePutIntoExileFromAPlayersGraveyard":              "zone",
	"WhenAnyNumberOfCardsArePutIntoExileFromAPlayersGraveyardAndOrLibrary":  "zone",
	"WhenAnyNumberOfGenericCardsArePutIntoExileFromAPlayersHand":            "zone",
	"WhenAnyNumberOfPermanentsAndOrGraveyardCardsArePutIntoExile":           "zone",
	"WhenASpellOrAbilityCausesAnyNumberOfCardToBeExiledFromAPlayersLibrary": "zone",
	"WhenACardIsPutIntoExile":                                               "zone",
	"WhenACardIsPutIntoTheCommandZoneFromAnywhere":                          "zone",
	// State triggers (CR 603.8); gorge: Mode$ Always.
	"WhenAPlayerControlsNoPermanents":                                    "state",
	"WhenPlayersControlsNoPermanents":                                    "state",
	"WhenNoPermanentsAreOnTheBattlefield":                                "state",
	"WhenAPlayerControlsAPermanent":                                      "state",
	"WhenAPlayerControlsNumberPermanents":                                "state",
	"WhenAnyNumberOfPermanentsAreOnTheBattlefield":                       "state",
	"WhenAPlayerHasAnAmountOfLife":                                       "state",
	"WhenAPlayerHasNoCardsInTheirGraveyard":                              "state",
	"WhenAPlayerHasNumberCardsInHand":                                    "state",
	"WhenAPermanentHasAbility":                                           "state",
	"WhenAPermanentHasPower":                                             "state",
	"WhenAColorIsntTheMostCommonOrTiedForMostCommonColorAmongPermanents": "state",
}

// d1MtgishOverrides corrects variants triggerClass's word checks catch under
// the wrong class; consulted before them.
var d1MtgishOverrides = map[string]string{
	// "Attacks" matched, but the event is the attacker going unblocked.
	"WhenACreatureAttacksAndIsntBlocked":                  "unblocked",
	"WhenACreatureAttacksAPlayerAndIsntBlocked":           "unblocked",
	"WhenANumberOfCreaturesAttacksAPlayerAndArentBlocked": "unblocked",
	// "Counters" matched the verb (a spell countered) or a removal.
	"WhenASpellOrAbilityCountersASpell":                     "countered",
	"WhenAnyNumberOfCountersAreRemovedFromAPermanent":       "counterremoved",
	"WhenAnyNumberOfCountersOfTypeAreRemovedFromAPermanent": "counterremoved",
	// "Upkeep" matched, but the event is the cost being paid (or not).
	"WhenAPermanentsCumulativeUpkeepCostIsPaid":           "paycumulativeupkeep",
	"WhenAPlayerPaysAPermanentsCumulativeUpkeepCost":      "paycumulativeupkeep",
	"WhenAPlayerDoesntPayAPermanentsCumulativeUpkeepCost": "paycumulativeupkeep",
	// "Damage" matched, but no damage is dealt: it was prevented.
	"WhenDamageThatWouldBeDealtToAPlayerIsPrevented": "damageprevented",
	// "Dies" matched the die's ("a die's highest natural result").
	"WhenAPlayerRollsADiesHighestNaturalResult": "rolleddie",
	// "Attack" matched, but the event is an ability triggering (Forge:
	// AbilityTriggered ValidMode$ Attacks).
	"WhenAPermanentAttackingCausesItsAbilityToTrigger": "abilitytriggered",
	// "Sacrifices" matched, but the trigger is the activation; Forge:
	// AbilityCast Condition$ Sacrificed.
	"WhenAPlayerSacrificesAnyNumberOfPermanentsToActivateAnAbility": "abilitycast",
}

// d1MtgishEvents maps a variant that names two events at once to both, one
// ability each, the way a script writes one T: line per event.
var d1MtgishEvents = map[string][]string{
	// Playing a card is casting a spell or playing a land.
	"WhenAPlayerPlaysACard":          {"cast", "landplayed"},
	"WhenAPlayerPlaysACardFromExile": {"cast", "landplayed"},
	// Losing control is the permanent leaving or changing controller;
	// Forge: an ltb T: line plus a Secondary$ ChangesController one.
	"WhenAPlayerLosesControlOfAPermanent":        {"ltb", "changescontroller"},
	"WhenAPlayerCastsASpellOrActivatesAnAbility": {"cast", "abilitycast"},
	"WhenASpellOrAbilityIsPutOntoTheStack":       {"cast", "abilitycast"},
}

func init() {
	for mode, class := range d1GorgeModes {
		class := class
		extTrigger[mode] = func(map[string]string) string { return class }
	}
	extTrigger["Exiled"] = d1Exiled
	extKeyword["Visit"] = d1Visit
	for v, class := range d1MtgishClasses {
		extTriggerClass[v] = class
	}
}

// d1Visit summarises K:Visit:<SVar>: the keyword plus the Attraction's
// "Visit —" ability, a triggered ability on visiting it (the Chapter
// precedent), as mtgish's Visit rule gives both.
func d1Visit(g *gorgeNorm, param string) {
	g.kw("visit")
	e := &effects{}
	g.chain(e, cards.ResolveSVar(g.svars(), strings.SplitN(param, ":", 2)[0]), 1)
	g.ability(Ability{Kind: "triggered", Event: "visit", Targets: e.targets, Effects: e.effects})
}
