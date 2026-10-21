package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit C2 of the mtgish cross-check: the mtgish Action variants M..Z the
// effect normaliser did not name. Most are zone moves, keyword actions and
// bookkeeping written as one variant per sentence shape; the families map
// onto the vocabulary both halves already share (move:<from>><to>, damage,
// sacrifice, ...), and a keyword action the gorge side spells as its own
// Forge API takes that API's lowercased name.

// c2Fixed maps a variant to the verbs it always contributes. An empty list
// is a node that is not an effect: a choice or a note whose result feeds a
// later action, a reveal, a payment, or control flow whose nested actions
// the walk visits on its own. The gorge side maps its counterparts (Reveal,
// RevealHand, ChooseCard, ChooseNumber, StoreSVar, Shuffle, TwoPiles,
// Repeat, RepeatEach, ImmediateTrigger) to nothing the same way.
var c2Fixed = map[string][]string{
	// Control flow: the nested actions are walked as children.
	"RepeatThisProcess": nil, "RepeatableActions": nil, "RepeatableActionsNumTimes": nil,
	"MayActionOnceEachTurn": nil, "MayHavePlayerAction": nil, "PlayerMayActions": nil,
	"ReflexiveTriggerNEW": nil, "SetRepeatablePlayers": nil,
	// A library shuffle on its own (gorge: Shuffle).
	"Shuffle": nil, "ShuffleLibraryIfSearched": nil,
	// Reveals and choices (gorge: Reveal, RevealHand, ChooseCard).
	"RevealAnyNumberOfCardsOfTypeFromHand": nil, "RevealACardFromHandAtRandom": nil,
	"RevealACardFromHand": nil, "RevealACardOfTypeFromHand": nil,
	"RevealACardOfTypeFromHandAtRandom": nil, "RevealAllCardsOfTypeFromHand": nil,
	"RevealCardFromHand": nil, "RevealNumberCardsFromHand": nil,
	"RevealNumberCardsFromHandAtRandom": nil, "RevealTheCardDrawnThisWay": nil,
	"RevealTheCardsDrawnThisWay": nil, "RevealTheCardPutInHandThisWay": nil,
	"RevealUptoNumberCardsOfTypeFromHand": nil, "RevealFaceDownPermanent": nil,
	"RevealHandAndPlayerChoosesACard": nil, "RevealAllCardsOfTypeFromHandAndPlayerChoosesCard": nil,
	// Payments: a cost paid, not an effect done.
	"PayAnyAmountOfMana": nil, "PayAnyAmountOfEnergy": nil, "PayAnyAmountOfLife": nil,
	"RepeatedMayCost": nil, "PlayerRepeatedMayCost": nil,
	// Notes and memory (gorge: StoreSVar, Remember$ parameters).
	"NoteAnUnnotedCreatureType": nil, "NoteNumber": nil, "NoteCountersOnPermanent": nil,
	"NoteManaTypeAndAmountSpentToActivateThisAbility": nil,
	"NoteTypeOfManaSpentToActivateThisAbility":        nil, "NumberEachPermanentStartingFromZero": nil,
	"RememberPlayer": nil, "RememberLifeTotal": nil, "SetStake": nil, "SwapWordChoice": nil,
	// Secret choices and their reveal (gorge: ChooseNumber, ChooseCard, ChoosePlayer).
	"SecretlyChooseANumber": nil, "SecretlyChooseAPermanent": nil,
	"SecretlyChooseLibraryFilter": nil, "SecretlyChooseACreatureTypeFromList": nil,
	"SecretlyChooseAGraveyardCard": nil, "SecretlyChooseANumberBetween": nil,
	"SecretlyChooseANumberBetweenThatHasntBeenChosen": nil,
	"SecretlyChooseANumberGreaterThanNumber":          nil,
	"SecretlyChooseANumberGreaterThanOrEqualToNumber": nil, "SecretlyChooseAPlayer": nil,
	"RevealSecretlyChosenNumbers": nil, "RevealSecretlyChosenPermanents": nil,
	"RevealTheSecretlyChosenNumber": nil,
	// The reveal half of a secret vote: the vote itself is SecretlyVote*.
	"RevealVotesForPermanent": nil, "RevealVotesForPlayer": nil, "RevealVotesForWord": nil,
	// Pile separation (gorge: TwoPiles, which walks the piles' abilities).
	"SeparatePermanentsIntoNumberPiles": nil, "SeparateExiledCardsIntoTwoPiles": nil,
	"SeparateCardsInPlayersGraveyardIntoTwoPiles": nil, "SeparatePermanentsIntoNamedPiles": nil,
	"SeparatePermanentsIntoTwoPilesAndAPlayerChoosesAPile": nil,
	// Modifiers of a neighbouring action, a parameter of it in a script:
	// excess damage redirected (DealDamage's ExcessDamage$) and the attack
	// restriction of an added combat (AddPhase's delayed trigger).
	"WouldDealExcessDamage_DealExcessToPlayerInstead":               nil,
	"OnlyAllowedAttackersDuringTheAdditionalCombatStepAddedThisWay": nil,
	// Paradigm is a keyword (Forge K:Paradigm); its exile-and-recast is the
	// keyword's own rule, not an effect verb of the spell.
	"Paradigm": nil,

	// Zone moves.
	"ReturnSpellToOwnersHand":                                         {"move:stack>hand"},
	"ShuffleSpellIntoLibrary":                                         {"move:stack>library"},
	"ReturnSpellToTopOrBottomOfLibrary":                               {"move:stack>library"},
	"PutSpellInOwnersLibraryNthFromTheTop":                            {"move:stack>library"},
	"PutSpellOnBottomOfOwnersLibrary":                                 {"move:stack>library"},
	"ReturnSpellToBottomOfLibrary":                                    {"move:stack>library"},
	"ShufflePermanentIntoLibrary":                                     {"move:battlefield>library"},
	"PutPermanentOnBottomOfOwnersLibrary":                             {"move:battlefield>library"},
	"PutPermanentInOwnersLibraryNthFromTheTop":                        {"move:battlefield>library"},
	"ShuffleEachPermanentIntoLibrary":                                 {"move:battlefield>library"},
	"PutEachPermanentOnBottomOfOwnersLibraryInOrderOfOwnersChoice":    {"move:battlefield>library"},
	"PutEachPermanentOnTheTopOfOwnersLibraryInOrderOfOwnersChoice":    {"move:battlefield>library"},
	"PutEachPermanentOnTopOfOwnersLibrariesThenShuffleThoseLibraries": {"move:battlefield>library"},
	"ReturnPermanentToBottomOfLibrary":                                {"move:battlefield>library"},
	"ReturnPermanentToLibraryUnderNumberCards":                        {"move:battlefield>library"},
	"PutEachPermanentToTopOrBottomOfLibrary":                          {"move:battlefield>library"},
	"PutPermanentInOwnersLibraryBeneathNumberCards":                   {"move:battlefield>library"},
	"PutEachPermanentInOwnersLibraryNthFromTheTop":                    {"move:battlefield>library"},
	"ReturnAPermanentToTopOfLibrary":                                  {"move:battlefield>library"},
	"ShuffleAPermanentIntoLibrary":                                    {"move:battlefield>library"},
	"ReturnAnyNumberOfPermanentsToTheirOwnersHands":                   {"move:battlefield>hand"},
	"PutNumberPermanentsIntoOwnersHand":                               {"move:battlefield>hand"},
	"ReturnUptoNumberPermanentsToOwnersHand":                          {"move:battlefield>hand"},
	"PutPermanentIntoOwnersGraveyard":                                 {"move:battlefield>graveyard"},
	"ReturnPermanentToCommandZone":                                    {"move:battlefield>command"},
	"ReturnEachPermanentToCommandZone":                                {"move:battlefield>command"},
	"ReturnSpellOrPermanentToOwnersHand":                              {"move:*>hand"},
	"ReturnPermanentOrExiledCardToOwnersHand":                         {"move:*>hand"},
	"PutCardFromAnywhereIntoPlayersGraveyard":                         {"move:*>graveyard"},

	"ReturnEachGraveyardCardToBattlefield":                                                         {"move:graveyard>battlefield"},
	"PutACardFromPlayersGraveyardOnBattlefield":                                                    {"move:graveyard>battlefield"},
	"PutAGraveyardCardOntoBattlefield":                                                             {"move:graveyard>battlefield"},
	"ReturnDeadGuestGraveyardCardToBattlefield":                                                    {"move:graveyard>battlefield"},
	"ReturnEnchantingGraveyardCardToBattlefield":                                                   {"move:graveyard>battlefield"},
	"PutACardOfTypeMilledThisWayOntoTheBattlefield":                                                {"move:graveyard>battlefield"},
	"ReturnACardFromPlayersGraveyardToBattlefield":                                                 {"move:graveyard>battlefield"},
	"ReturnAnyNumberOfGroupCardsFromPlayersGraveyardToBattlefield":                                 {"move:graveyard>battlefield"},
	"ReturnNumberCardsFromGraveyardToBattlefield":                                                  {"move:graveyard>battlefield"},
	"ReturnACardFromGraveyardToBattlefieldAtRandom":                                                {"move:graveyard>battlefield"},
	"ReturnACardMilledThisWayToBattlefield":                                                        {"move:graveyard>battlefield"},
	"ReturnAnyNumberOfCardsFromGraveyardToBattlefield":                                             {"move:graveyard>battlefield"},
	"ReturnNewGraveyardCardToBattlefield":                                                          {"move:graveyard>battlefield"},
	"PutACardDiscardedThisWayOntoTheBattlefield":                                                   {"move:graveyard>battlefield"},
	"PutACardFromEachPlayersGraveyardOntoTheBattlefield":                                           {"move:graveyard>battlefield"},
	"PutACardOfTypeAndOrACardOfTypeMilledThisWayOntoTheBattlefield":                                {"move:graveyard>battlefield"},
	"PutAnyNumberOfCardsOfTypeMilledThisWayOntoTheBattlefield":                                     {"move:graveyard>battlefield"},
	"PutEachCardOfTypeMilledThisWayOntoTheBattlefield":                                             {"move:graveyard>battlefield"},
	"PutEachCardOfTypeMilledThisWayOntoTheBattlefieldAsFaceDownArtifactCreatures":                  {"move:graveyard>battlefield"},
	"PutEachGraveyardCardOntoBattlefieldFaceDownAsAnArtifactCreature":                              {"move:graveyard>battlefield"},
	"PutNumberCardsFromAmongPlayersGraveyardsOntoTheBattlefield":                                   {"move:graveyard>battlefield"},
	"PutUptoNumberGraveyardCardsOntoTheBattlefield":                                                {"move:graveyard>battlefield"},
	"ReturnACardFromAnyPlayersGraveyardToBattlefield":                                              {"move:graveyard>battlefield"},
	"ReturnAnyNumberOfGroupCardsMilledThisWayToBattlefield":                                        {"move:graveyard>battlefield"},
	"ReturnEachCardFromEachPlayersGraveyardToBattlefieldThatWasPutThereFromAnywhereThisTurn":       {"move:graveyard>battlefield"},
	"ReturnEachCardFromEachPlayersGraveyardToBattlefieldThatWasPutThereFromTheBattlefieldThisTurn": {"move:graveyard>battlefield"},
	"ReturnEachCardFromPlayersGraveyardToBattlefieldThatWasPutThereFromBattlefieldThisTurn":        {"move:graveyard>battlefield"},
	"ReturnEachCardFromPlayersGraveyardToBattlefieldThatWasDestroyThisWay":                         {"move:graveyard>battlefield"},
	"ReturnMultipleCardsFromPlayersGraveyardToBattlefield":                                         {"move:graveyard>battlefield"},
	"ReturnNumberCardsFromGraveyardToBattlefieldAtRandom":                                          {"move:graveyard>battlefield"},
	"ReturnNumberGraveyardCardsToBattlefieldAtRandom":                                              {"move:graveyard>battlefield"},
	"ReturnUptoNumberCardsFromAmongPlayersGraveyardsToBattlefield":                                 {"move:graveyard>battlefield"},
	"ReturnUptoNumberCardsFromPlayersGraveyardToBattlefield":                                       {"move:graveyard>battlefield"},
	"ReturnUptoOneCardOfEachPermanentTypeInPlayersGraveyardToBattlefield":                          {"move:graveyard>battlefield"},

	"ReturnDeadGraveyardCardToHand":                                           {"move:graveyard>hand"},
	"ReturnAGraveyardCardToHand":                                              {"move:graveyard>hand"},
	"ReturnEachCardFromGraveyardToHand":                                       {"move:graveyard>hand"},
	"PutUptoNumberCardsFromGraveyardToHand":                                   {"move:graveyard>hand"},
	"ReturnACardFromGraveyardToHandAtRandom":                                  {"move:graveyard>hand"},
	"PutEachCardOfTypeMilledThisWayIntoHand":                                  {"move:graveyard>hand"},
	"PutACardFromGraveyardIntoHandAtRandom":                                   {"move:graveyard>hand"},
	"PutUptoNumberCardsOfTypeMilledThisWayIntoHand":                           {"move:graveyard>hand"},
	"ReturnEachCardFromEachPlayersGraveyardToOwnersHand":                      {"move:graveyard>hand"},
	"ReturnEachCardFromGraveyardToHandThatWasPutThereFromBattlefieldThisTurn": {"move:graveyard>hand"},
	"ReturnGraveyardCardsToHand":                                              {"move:graveyard>hand"},
	"ReturnNumberCardsFromPlayersGraveyardToHand":                             {"move:graveyard>hand"},
	"PutUptoNumberCardsFromGraveyardIntoHand":                                 {"move:graveyard>hand"},
	"ReturnAnyNumberOfGroupCardsFromGraveyardToHand":                          {"move:graveyard>hand"},
	"ReturnDeadGuestGraveyardCardToHand":                                      {"move:graveyard>hand"},
	"ReturnNumberCardsFromPlayersGraveyardToHandAtRandom":                     {"move:graveyard>hand"},
	"PutAGraveyardCardIntoHand":                                               {"move:graveyard>hand"},
	"PutAllCardsFromGraveyardIntoHand":                                        {"move:graveyard>hand"},
	"PutAnyNumberOfCardsFromGraveyardIntoHand":                                {"move:graveyard>hand"},
	"PutAnyNumberOfCardsOfTypeMilledThisWayIntoHand":                          {"move:graveyard>hand"},
	"ReturnAnyNumberCardsMilledThisWayToHand":                                 {"move:graveyard>hand"},
	"ReturnEachCardFromGraveyardToHandThatWasCycledOrDiscardedThisTurn":       {"move:graveyard>hand"},
	"ReturnEachCardFromGraveyardToHandThatWasPutThereFromAnywhereThisTurn":    {"move:graveyard>hand"},
	"ReturnEachCardFromPlayersGraveyardToHand":                                {"move:graveyard>hand"},
	"PutEachCommanderFromGraveyardIntoHand":                                   {"move:graveyard>hand"},

	"ShuffleEachGraveyardCardIntoLibrary":                    {"move:graveyard>library"},
	"ShuffleGraveyardCardIntoLibrary":                        {"move:graveyard>library"},
	"PutGraveyardCardInOwnersLibraryNthFromTheTop":           {"move:graveyard>library"},
	"ReturnGraveyardCardsToTopOfLibraryInAnyOrder":           {"move:graveyard>library"},
	"ReturnDeadGraveyardCardToBottomOfLibrary":               {"move:graveyard>library"},
	"ShuffleEachCardInGraveyardIntoLibrary":                  {"move:graveyard>library"},
	"PutACardFromPlayersGraveyardOnTopOfLibrary":             {"move:graveyard>library"},
	"ReturnDeadGraveyardCardToTopOfLibrary":                  {"move:graveyard>library"},
	"ReturnEachGraveyardCardToBottomOfLibraryInAnyOrder":     {"move:graveyard>library"},
	"ReturnGraveyardCardToTopOrBottomOfLibrary":              {"move:graveyard>library"},
	"PutACardOfTypeMilledThisWayOnTopOfLibrary":              {"move:graveyard>library"},
	"PutDeadPermanentOnBottomOfLibrary":                      {"move:graveyard>library"},
	"PutDeadPermanentOnTopOfLibrary":                         {"move:graveyard>library"},
	"PutDeadPermanentOnTopOfLibraryOrOnBottomOfLibrary":      {"move:graveyard>library"},
	"PutEachCardInGraveyardOntoBottomOfLibraryInRandomOrder": {"move:graveyard>library"},
	"PutNumberGraveyardCardsOnTopOfLibraryInAnyOrder":        {"move:graveyard>library"},
	"ReturnACardFromGraveyardToTopOfLibrary":                 {"move:graveyard>library"},
	"ShuffleEachCardInPlayersGraveyardIntoLibrary":           {"move:graveyard>library"},
	"ShuffleUptoNumberCardsFromPlayersGraveyardIntoLibrary":  {"move:graveyard>library"},

	"PutCardFromHandOnBattlefield":                                          {"move:hand>battlefield"},
	"PutAnyNumberOfCardsFromHandOntoBattlefield":                            {"move:hand>battlefield"},
	"PutUptoNumberCardsFromHandOntoBattlefield":                             {"move:hand>battlefield"},
	"PutCardsFromHandOnBattlefield":                                         {"move:hand>battlefield"},
	"PutACardAndOrACardFromHandOnBattlefield":                               {"move:hand>battlefield"},
	"PutAnyNumberOfCardsFromHandOntoBattlefieldAsFaceDownArtifactCreatures": {"move:hand>battlefield"},
	"PutUptoOneCardOfEachCardtypeAmongPermanentsFromHandOnTheBattlefield":   {"move:hand>battlefield"},
	// "From hand or graveyard": the existing PutACardFromHandOrGraveyardOnBattlefield's reading.
	"MayPutACardFromHandOrGraveyardOnBattlefieldForEachPermanent":    {"move:hand>battlefield"},
	"PutAnyNumberOfCardsFromHandOrFromPlayersGraveyardOnBattlefield": {"move:hand>battlefield"},
	"PutUptoNumberCardsFromHandAndOrGraveyardOnBattlefield":          {"move:hand>battlefield"},
	"RevealHandAndPlayerChoosesACardToPutOnBattlefield":              {"move:hand>battlefield"},

	"PutACardFromHandOnTopOfLibrary":                           {"move:hand>library:1"},
	"ShuffleHandIntoLibrary":                                   {"move:hand>library"},
	"PutACardFromHandOnBottomOfLibrary":                        {"move:hand>library:1"},
	"PutAllCardsFromHandOnBottomOfLibraryAnyOrder":             {"move:hand>library"},
	"PutAllCardsFromHandOnTopOfLibraryRandomOrder":             {"move:hand>library"},
	"PutAnyNumberOfCardsFromHandOnBottomOfLibraryInAnyOrder":   {"move:hand>library:*"},
	"PutCardFromHandOnBottomOfLibrary":                         {"move:hand>library:1"},
	"PutCardFromHandOnTopOfLibrary":                            {"move:hand>library:1"},
	"PutCardInHandIntoLibraryNthFromTop":                       {"move:hand>library:1"},
	"ShuffleCardsFromHandIntoLibrary":                          {"move:hand>library:*"},
	"ShuffleCardFromHandIntoLibrary":                           {"move:hand>library:1"},
	"ShuffleAnyNumberOfCardsFromHandIntoLibrary":               {"move:hand>library:*"},
	"ShuffleAllButNumberCardsInHandIntoLibrary":                {"move:hand>library:*"},
	"PutACardFromHandIntoGraveyard":                            {"move:hand>graveyard"},
	"PutCardFromHandIntoGraveyard":                             {"move:hand>graveyard"},
	"PutCardsInHandIntoGraveyard":                              {"move:hand>graveyard"},
	"PutCardFromHandIntoPlayersHand":                           {"move:hand>hand"},
	"RevealHandAndPlayerChoosesACardToExileUntil":              {"move:hand>exile"},
	"RevealAllCardsOfTypeFromHandAndPlayerChoosesACardToExile": {"move:hand>exile"},
	"RevealANumberOfCardsFromHandAndPlayerChoosesACardToExile": {"move:hand>exile"},
	"RevealHandAndAlternateExilingCardsWithPlayer":             {"move:hand>exile"},
	"RevealHandAndExileEachCardOfType":                         {"move:hand>exile"},
	"RevealHandAndPlayerChoosesNumberCardsToExile":             {"move:hand>exile"},
	"RevealHandAndPlayerMayChooseACardToExile":                 {"move:hand>exile"},
	"RevealNumberCardsFromHandAndPlayerChoosesACardToExile":    {"move:hand>exile"},

	"ReturnEachExiledCardToOwnersHand":                                {"move:exile>hand"},
	"PutAnExiledCardIntoOwnersHand":                                   {"move:exile>hand"},
	"ReturnAnExiledCardToOwnersHand":                                  {"move:exile>hand"},
	"ReturnUptoNumberCardsFromExileToOwnersHand":                      {"move:exile>hand"},
	"PutExiledPileIntoOwnersHand":                                     {"move:exile>hand"},
	"ReturnAnExiledCardToBattlefield":                                 {"move:exile>battlefield"},
	"PutAnExiledCardOntoBattlefield":                                  {"move:exile>battlefield"},
	"PutAnyNumberOfCardsFromAmongExileOntoBattlefield":                {"move:exile>battlefield"},
	"PutAnyNumberOfCardsFromExileOntoBattlefield":                     {"move:exile>battlefield"},
	"PutEachExiledCardOntoTheBattlefieldFaceDownAsAnArtifactCreature": {"move:exile>battlefield"},
	"PutNumberCardsFromExileOntoBattlefield":                          {"move:exile>battlefield"},
	"PutExiledCardsIntoOwnersGraveyards":                              {"move:exile>graveyard"},
	"PutExiledCardIntoOwnersGraveyard":                                {"move:exile>graveyard"},
	"ReturnEachExiledCardToGraveyard":                                 {"move:exile>graveyard"},
	"PutAnExiledCardIntoOwnersGraveyard":                              {"move:exile>graveyard"},
	"PutEachExiledCardOnTheBottomOfTheirOwnersLibraryInARandomOrder":  {"move:exile>library"},
	"PutEachExiledCardOnTheBottomOfTheirOwnersLibraryInAnyOrder":      {"move:exile>library"},
	"ReturnEachExiledCardToBottomOfOwnersLibraryRandomOrder":          {"move:exile>library"},
	"PutExiledCardInOwnersLibraryNthFromTheTop":                       {"move:exile>library"},
	"PutExiledCardOnTheBottomOfItsOwnersLibrary":                      {"move:exile>library"},
	"PutExiledCardsOnTopOfLibraryInAnyOrder":                          {"move:exile>library"},
	"ShuffleEachExiledCardIntoLibrary":                                {"move:exile>library"},
	"ShuffleExiledCardIntoLibrary":                                    {"move:exile>library"},
	"ShuffleExiledCardsAndPutOnTopOfLibrary":                          {"move:exile>library"},

	"PutTopOfLibraryOnBattlefield":                                                   {"move:library>battlefield"},
	"PutTopCardOfLibraryOfTypeOnBattlefield":                                         {"move:library>battlefield"},
	"PutCardsInLibraryOntoTheBattlefield":                                            {"move:library>battlefield"},
	"PutTheTopNumberCardsOfPlayersLibraryOntoBattlefieldAsFaceDownArtifactCreatures": {"move:library>battlefield"},
	"PutTopOfLibraryInGraveyard":                                                     {"move:library>graveyard"},
	"PutTheBottomCardOfPlayersLibraryIntoGraveyard":                                  {"move:library>graveyard"},
	"PutTheTopCardOfPlayersLibraryInGraveyard":                                       {"move:library>graveyard"},
	"PutTopOfOtherLibraryInGraveyard":                                                {"move:library>graveyard"},
	"PutCardsInLibraryIntoGraveyard":                                                 {"move:library>graveyard"},
	"PutTopOfLibraryOnBottomOfLibrary":                                               {"move:library>library"},
	"PutTheRemainingLibraryCardsOnTheBottomOfLibraryInARandomOrder":                  {"move:library>library"},
	"PutTheTopCardOfPlayersLibraryOnTheBottomOfTheirLibrary":                         {"move:library>library"},
	"PutCardsInLibraryOnTheBottomOfLibraryInAnyOrder":                                {"move:library>library"},
	"PutCardsInLibraryOnTheBottomOfLibraryInARandomOrder":                            {"move:library>library"},
	"PutLibraryCardsIntoHand":                                                        {"move:library>hand"},
	"PutTheTopNumberCardsOfLibraryInHand":                                            {"move:library>hand"},
	"RevealTopCardOfLibraryAndPutIntoHand":                                           {"dig:1", "move:library>hand"},
	"MayPutTheTopCardOfPlayersLibraryOfTypeInGraveyardForCost":                       {"dig:1", "move:library>graveyard"},
	// A random card from the library: gorge writes it as a hidden-origin
	// ChangeZone from the library, which the gorge side reads as a search.
	"PutARandomCardFromLibraryIntoGraveyard":          {"search:graveyard"},
	"PutARandomCardFromLibraryOntoBattlefield":        {"search:battlefield"},
	"PutARandomCardFromPlayersLibraryOntoBattlefield": {"search:battlefield"},

	"RevealCardFromOutsideGameAndPutInHand":                              {"move:sideboard>hand"},
	"PutACardFromOutsideGameInHand":                                      {"move:sideboard>hand"},
	"RevealCardFromOutsideGameAndPutInHandOrPutAnExiledCardInOwnersHand": {"move:sideboard>hand"},
	"RevealNumberGroupCardsFromOutsideGame":                              {"move:sideboard>hand"},
	"PutACardFromOutsideGameOnTopOfLibrary":                              {"move:sideboard>library"},
	"ShuffleUptoNumberCardsFromOutsideTheGameIntoLibrary":                {"move:sideboard>library"},
	"PutACommanderFromCommandZoneIntoHand":                               {"move:command>hand"},
	"PutEachCommanderFromCommandZoneIntoHand":                            {"move:command>hand"},
	"PutACommanderFromCommandZoneOntoBattlefield":                        {"move:command>battlefield"},
	"PutCommanderFromCommandZoneOntoBattlefield":                         {"move:command>battlefield"},
	"PutEachCommanderFromCommandZoneOntoBattlefield":                     {"move:command>battlefield"},
	"PutEachCardFromAnteInGraveyard":                                     {"move:ante>graveyard"},

	// Whole zones shuffled together: scripts write one ChangeZoneAll over
	// several origins, which the gorge side reads as one move; one
	// any-origin move matches it whichever origin the script lists first.
	// An object and a zone (a permanent and a graveyard) are two moves, as
	// scripts write two ChangeZones.
	"ShuffleHandAndGraveyardIntoLibrary":                                            {"move:*>library"},
	"ShuffleHandAndPermanentsIntoLibrary":                                           {"move:*>library"},
	"ShuffleHandGraveyardAndPermanentsIntoLibrary":                                  {"move:*>library"},
	"ShufflePermanentAndGraveyardIntoLibrary":                                       {"move:battlefield>library", "move:graveyard>library"},
	"ShuffleSpellAndGraveyardCardsIntoLibraries":                                    {"move:stack>library", "move:graveyard>library"},
	"SimultaneouslySacrificePermanentAndPutGraveyardCardOntoBattlefield":            {"sacrifice", "move:graveyard>battlefield"},
	"RestartTheGameLeavingEachExiledCardInExileThenPutThoseCardsOntoTheBattlefield": {"restartgame", "move:exile>battlefield"},

	// Looks at the library.
	"RevealTheTopCardOfPlayersLibrary":                     {"dig:1"},
	"PlayersRevealTopCardOfLibraryAndFindHighestManaValue": {"dig:1"},
	"RevealLibrary": {"dig:*"},
	"RevealCardsFromTheTopOfLibraryUntilANumberOfCardsOfTypeAreRevealed":                    {"diguntil"},
	"RevealCardsFromTheTopOfLibraryUntilACardOfTypeIsRevealedOrUntilNumberCardsAreRevealed": {"diguntil"},
	"MillCardsUntilACardOfTypeIsMilledOrUntilNumberCardsHaveBeenPutIntoGraveyardThisWay":    {"mill:*"},

	// Sacrifice, tap, untap.
	// "Sacrifice an artifact, a creature and a land": one sacrifice of
	// several permanents, as scripts write one Sacrifice over a list.
	"SacrificePermanents":                                                      {"sacrifice"},
	"SacrificeAnyNumberOfPermanents":                                           {"sacrifice"},
	"SacrificeAllPermanentsExceptForNum":                                       {"sacrifice"},
	"SacrificeHalfOfThePermanentsRoundedUp":                                    {"sacrifice"},
	"SacrificeAPermanentOfAPlayersChoice":                                      {"sacrifice"},
	"SacrificeAllPermanentsExceptForAPermanentOfEachLandType":                  {"sacrifice"},
	"SacrificeAllPermanentsExceptForASpecificPermanentOfEachTypeOfTheirChoice": {"sacrifice"},
	"TapAnyNumberOfPermanents":                                                 {"tap"},
	"TapNumberPermanents":                                                      {"tap"},
	"TapAllButNumberPermanents":                                                {"tap"},
	"UntapAPermanent":                                                          {"untap"},
	"SimultaneouslyTapEachPermanentAndUntapEachPermanent":                      {"tap", "untap"},

	// Discards after a reveal: the chosen count is not a literal.
	"RevealHandAndDiscardEachCard":                               {"discard:*"},
	"RevealANumberOfCardsFromHandAndPlayerChoosesACardToDiscard": {"discard:*"},
	"RevealHandAndPlayerChoosesNumberCardsToDiscard":             {"discard:*"},
	"RevealHandAndPlayerMayChooseACardToDiscard":                 {"discard:*"},
	"RevealHandAndPlayerChoosesMultipleCardsToDiscard":           {"discard:*"},
	"RevealNumberCardsFromHandAtRandomAndDiscardEachCardOfType":  {"discard:*"},
	"RevealHandAndDiscardACardOfTypeAtRandom":                    {"discard:1"},

	// Turn structure.
	"ThereIsAnAdditionalCombatPhaseAndAnAdditionalMainPhase": {"addphase"},
	"ThereIsAnAdditionalBeginningPhase":                      {"addphase"},
	"ThereIsAnAdditionalCombatPhaseWithTrigger":              {"addphase"},
	"ThereIsAnAdditionalUpkeepStep":                          {"addphase"},
	"ThereAreNumberAdditionalUpkeepSteps":                    {"addphase"},
	"ThereIsAnAdditionalEndStep":                             {"addphase"},
	"TakeANumberOfExtraTurns":                                {"extraturn"},
	"TakeAnExtraTurnAfterNextTurn":                           {"extraturn"},
	"TakeAnExtraTurnAndSkipUntapStepOfThatTurn":              {"extraturn", "player:skipsuntapstep"},
	"SkipNextTurn":                     {"skipturn"},
	"SkipNextNumberTurns":              {"skipturn"},
	"SkipNextUntapStep":                {"skipphase"},
	"SkipNextDrawStep":                 {"skipphase"},
	"SkipNextCombatPhase":              {"skipphase"},
	"SkipNextCombatPhaseThisTurn":      {"skipphase"},
	"SkipAllCombatPhasesTheirNextTurn": {"skipphase"},
	"ReverseTurnOrder":                 {"reverseturnorder"},

	// Casting and playing.
	"PlayExiledCardWithoutPaying":                                                  {"play"},
	"PlayACardFromExileWithoutPaying":                                              {"play"},
	"PlayTopCardOfLibraryWithoutPaying":                                            {"play"},
	"PlayAnyNumberOfLandsOrCastAnyNumberOfSpellsFromExileWithoutPaying":            {"play"},
	"PlayExiledCard":                                                               {"play"},
	"PlayGraveyardCardWithoutPaying":                                               {"play"},
	"MayCastASpellFromAmongCardsInHandsWithoutPaying":                              {"play"},
	"MayCastASpellFromAmongGraveyardCardsIntoExileOrBattlefield":                   {"play"},
	"MayCastASpellFromEachPlayersGraveyardWithoutPayingIntoExile":                  {"play"},
	"MayCastGraveyardCardAndMaySpendManaAsThoughAnyType":                           {"play"},
	"MayCastGraveyardCardIntoExileAndMaySpendManaAsThoughAnyType":                  {"play"},
	"RevealANumberOfCardsFromHandAndPlayerMayCastASpellFromAmongThemWithoutPaying": {"play"},
	"RevealHandAndPlayerMayCastASpellFromAmongThemWithoutPaying":                   {"play"},
	"PutExiledCardOnStackAsCopyOfSpell":                                            {"copyspell"},

	// Existing verbs.
	"Populate": {"tokencopy:1"}, "PopulateNumberTimes": {"tokencopy:1"}, "PopulateWithFlags": {"tokencopy:1"},
	"Support":                                 {"counters:+1/+1:1"},
	"TurnPermanentFaceUp":                     {"setstate:turnfaceup"},
	"TurnAPermanentFaceUp":                    {"setstate:turnfaceup"},
	"TurnExiledCardFaceUp":                    {"setstate:turnfaceup"},
	"TurnEachExiledCardFaceUp":                {"setstate:turnfaceup"},
	"TurnAnExiledPileFaceUp":                  {"setstate:turnfaceup"},
	"TurnPermanentFaceDown":                   {"setstate:turnfacedown"},
	"TurnPermanentFaceDownAsArtifactCreature": {"setstate:turnfacedown"},
	"TurnEachMorphPermanentFaceDown":          {"setstate:turnfacedown"},
	"TurnEachPermanentFaceDownAsCreature":     {"setstate:turnfacedown"},
	"TransformAPermanent":                     {"transform"},
	"TransformAnyNumberOfPermanents":          {"transform"},
	// "N times" is walked once, as the gorge side walks a Repeat once.
	"ProliferateNumberTimes": {"proliferate"},
	"MultiFight":             {"fight"},
	"RegeneratePermanent_WhenItRengeratesThisWay": {"regenerate"},
	"SeekCards": {"seek"},
	"SeekACardFromTheTopNumberCardsOfLibrary":               {"seek"},
	"SeekNumberCardsFromTheTopNumberCardsOfLibrary":         {"seek"},
	"ReselectTargetOfSpellOrAbilityAtRandom":                {"changetargets"},
	"RerollAnyNumberOfTheStoredD6Results":                   {"dice"},
	"TripleCreaturesPowerAndToughnessUntilEndOfTurn":        {"pump:*"},
	"PermanentDoesntUntapDuringControllersNextNumberUntaps": {"rule:doesntuntapduringcontrollersuntap"},
	// gorge's Phases covers phasing in and out alike.
	"PhaseInPermanent": {"phaseout"}, "PhaseInEachPermanent": {"phaseout"},
	"OnlyAllowedAttackersUntilEndOfTurn":        {"rule:cantattack"},
	"OnlyAllowedAttackingPlayersUntilEndOfTurn": {"rule:cantattack"},
	"OnlyAllowedBlockersUntilEndOfTurn":         {"rule:cantblock"},
	"OnlyAllowedCastingPlayersUntilEndOfTurn":   {"player:cantcastspells"},

	// Keyword actions and game actions the gorge side spells as their own
	// Forge API: the lowercased API name.
	"VentureIntoTheDungeon":                     {"venture"},
	"ManifestDread":                             {"manifestdread"},
	"ManifestDreadNumberTimes":                  {"manifestdread"},
	"ManifestTheTopCardOfPlayersLibrary":        {"manifest"},
	"ManifestTheTopNumberCardsOfPlayersLibrary": {"manifest"},
	"ManifestEachExiledCard":                    {"manifest"},
	"ManifestACardFromHand":                     {"manifest"},
	"ManifestCardFromHand":                      {"manifest"},
	"ManifestEachCardOfTypeFromHand":            {"manifest"},
	"ManifestNumberCardsFromHand":               {"manifest"},
	"SetLifeTotal":                              {"setlife"},
	"RedistributeLifeTotalsOfPlayers":           {"setlife"},
	"PlayersExchangeLifeTotals":                 {"exchangelife"},
	"MoveCounters":                              {"movecounter"},
	"PutOrRemoveCounters":                       {"addorremovecounter"},
	// AlterAttribute sets a permanent's designation: prepared, suspected,
	// saddled.
	"PreparePermanent":                         {"alterattribute"},
	"PrepareEachPermanent":                     {"alterattribute"},
	"UnpreparePermanent":                       {"alterattribute"},
	"SuspectPermanent":                         {"alterattribute"},
	"SuspectEachPermanent":                     {"alterattribute"},
	"UnsuspectPermanent":                       {"alterattribute"},
	"UnsuspectEachPermanent":                   {"alterattribute"},
	"SaddlePermanent":                          {"alterattribute"},
	"VoteForAWord":                             {"vote"},
	"VoteForAPermanent":                        {"vote"},
	"VoteForACardInGraveyard":                  {"vote"},
	"VoteForColor":                             {"vote"},
	"SecretlyVoteForAPlayer":                   {"vote"},
	"SecretlyVoteForAWord":                     {"vote"},
	"SecretlyVoteForUptoOnePermanent":          {"vote"},
	"SecretlyVoteForAPermanent":                {"vote"},
	"TakeTheInitiative":                        {"takeinitiative"},
	"OpenAnAttraction":                         {"openattraction"},
	"OpenNumberAttractions":                    {"openattraction"},
	"PutAStickerOnAPermanent":                  {"putsticker"},
	"PutANameStickerOnPermanent":               {"putsticker"},
	"PutANameStickerOnAPermanent":              {"putsticker"},
	"PutAPowerAndToughnessStickerOnAPermanent": {"putsticker"},
	"PutAStickerOnACardInPlayersGraveyard":     {"putsticker"},
	"PutAnAbilityStickerWithTicketCostOnPermanentWithoutPaying": {"putsticker"},
	"PutAnArtStickerOnAPermanent":                               {"putsticker"},
	"PutUptoNumberNameStickersOnPermanent":                      {"putsticker"},
	"RemoveCreatureFromCombat":                                  {"removefromcombat"},
	"RemoveEachCreatureFromCombat":                              {"removefromcombat"},
	"RemoveCreatureFromCombatAndUnblockBlockers":                {"removefromcombat"},
	"Recruit":                                {"recruit"},
	"Planeswalk":                             {"planeswalk"},
	"TimeTravel":                             {"timetravel"},
	"TimeTravelNumberTimes":                  {"timetravel"},
	"MeldExiledIntoNewPermanent":             {"meld"},
	"UnattachPermanent":                      {"unattach"},
	"UnattachEachPermanentFromEachPermanent": {"unattach"},
	"UnattachEachPermanentFromPermanent":     {"unattach"},
	"ReselectWhichPlayerOrPermanentCreatureIsAttacking":        {"changecombatants"},
	"MayReselectWhichPlayerOrPermanentEachCreatureIsAttacking": {"changecombatants"},
	"ReselectWhichPlayerCreatureIsAttacking":                   {"changecombatants"},
	"SetAttackAssignmentOfCreature":                            {"changecombatants"},
	"SetAttackAssignmentOfCreatures":                           {"changecombatants"},
	"SetCreatureAsBlocked":                                     {"becomesblocked"},
	"SetEachCreatureAsBlocked":                                 {"becomesblocked"},
	"ReducePlayersSpeed":                                       {"changespeed"},
	"ReorderPlayersGraveyard":                                  {"reorderzone"},
	"ShuffleGraveyard":                                         {"reorderzone"},
	"PlayAMagicSubgame":                                        {"subgame"},
	"SetSchemeInMotion":                                        {"setinmotion"},
	"SetValueXOfSpellOrAbility":                                {"changex"},
	"StartBiddingWarAmongPlayersAtAnyNumber":                   {"bidlife"},
	"StartBiddingWarAmongPlayersAtNumber":                      {"bidlife"},
	"StartBiddingWarWithPlayer":                                {"bidlife"},
	"TrySwappingBlockAssignmentsOfTwoAttackingCreatures":       {"switchblock"},
	"TrySwappingBlockAssignmentsOfTwoBlockingCreatures":        {"switchblock"},
	"UnlockADoorOfAPermanent":                                  {"unlockdoor"},
	"UnlockADoorOfPermanent":                                   {"unlockdoor"},
	"PerpetuallyIncreaseIntensityOfPermanent":                  {"intensify"},
	"PerpetuallyExchangePowerOfPermanentAndPermanent":          {"exchangepower"},
	// gorge: Goad with NoLonger$ True.
	"UngoadEachCreature": {"ungoad"},
}

// c2Damage maps a damage variant to the argument holding its amount; -1
// for an amount that is not a literal (per-each, divided, one per player).
var c2Damage = map[string]int{
	"SpellDealsDamageCantBePrevented":                   1,
	"PlaneDealsDamage":                                  1,
	"SchemeDealsDamage":                                 1,
	"VanguardDealsDamage":                               1,
	"PermanentOrDeadPermanentDealsDamage":               0,
	"PermanentDealsDamageExcessReplacable":              1,
	"SpellDealsDamageExcessReplacable":                  1,
	"PermanentDealsDamageForEach":                       -1,
	"SpellDealsDamageForEach":                           -1,
	"SpellDealsDamageEachPlayer":                        -1,
	"SpellDealsDamageEachPlayerForEach":                 -1,
	"SpellDealsDamageDividedAmongRecipientsRoundedDown": -1,
	"PermanentDealsDamageDividedAsPlayerChooses":        -1,
}

func init() {
	for v, toks := range c2Fixed {
		toks := toks
		extAction[v] = func(e *effects, _ node) { e.add(toks...) }
	}
	for v, i := range c2Damage {
		i := i
		extAction[v] = func(e *effects, n node) {
			if i < 0 {
				e.add("damage:*")
				return
			}
			e.add("damage:" + gameNumber(argN(n, i)))
		}
	}
	extAction["PermanentDealsDamageAndPermanentDealsDamage"] = func(e *effects, n node) {
		e.add("damage:"+gameNumber(argN(n, 1)), "damage:"+gameNumber(argN(n, 4)))
	}
	extAction["PermanentDealsDamageAndPreventSomeOfIt"] = func(e *effects, n node) {
		e.add("damage:"+gameNumber(argN(n, 1)), "prevent")
	}
	// Monstrosity N: put N +1/+1 counters on it (and it becomes monstrous).
	extAction["Monstrosity"] = func(e *effects, n node) {
		e.add("counters:+1/+1:" + gameNumber(rawArgs(n)))
	}
	extAction["PutCountersWithRestriction"] = func(e *effects, n node) {
		for _, pc := range list(argN(n, 0)) {
			e.add(putCounters(pc))
		}
	}
	// Cards from the hand onto the library carry their count (unit A's
	// convention: gorge's ChangeZone from the hand emits ChangeNum$, a
	// whole-hand ChangeZoneAll stays a plain move:hand>library).
	for _, v := range []string{"PutNumCardsFromHandOnTopOfLibraryAnyOrder", "PutNumCardsFromHandOnBottomOfLibraryAnyOrder"} {
		extAction[v] = func(e *effects, n node) { e.add("move:hand>library:" + gameNumber(rawArgs(n))) }
	}
	extAction["RevealTheTopNumberCardsOfLibraryNEW"] = func(e *effects, n node) {
		e.add("dig:" + gameNumber(rawArgs(n)))
	}
	extAction["PutARandomCardOfTypeFromAmongTheTopNumberCardsOfLibraryIntoHand"] = func(e *effects, n node) {
		e.add("dig:"+gameNumber(argN(n, 1)), "move:library>hand", "move:library>library")
	}
	extAction["MultiDraw"] = func(e *effects, n node) {
		for _, d := range list(rawArgs(n)) {
			if variantOf(d) == "DrawNumberCards" {
				e.add("draw:" + gameNumber(rawArgs(d)))
			} else {
				e.add("draw:1")
			}
		}
	}
	extAction["MultiCreateTokens"] = func(e *effects, n node) {
		for _, m := range list(rawArgs(n)) {
			if variantOf(m) == "EachPlayerCreatesTokens" {
				e.add("token:*")
				continue
			}
			for _, t := range list(argN(m, 0)) {
				e.add(tokenToken(t))
			}
		}
	}
	// "<player> must <cost>": the cost is the action done.
	extAction["PlayerMustCost"] = func(e *effects, n node) {
		e.add(costAction(argN(n, 1))...)
	}
	// "Search your library and/or graveyard (and/or hand, outside the
	// game) for a card": one pool, one search, as scripts write one
	// ChangeZone over several origins (read as a search when the library is
	// among them).
	for _, v := range []string{
		"SearchLibraryAndGraveyard", "SearchLibraryAndGraveyardAndHand",
		"SearchLibraryAndOrGraveyardAndOrHand", "SearchHandAndLibrary", "SearchHandAndOrLibrary",
		"SearchLibraryAndOrGraveyardAndOrOutsideTheGame", "SearchLibraryAndOrOutsideTheGame",
	} {
		extAction[v] = func(e *effects, n node) { e.add(searchToken(rawArgs(n))) }
	}
	extAction["SearchPlayersLibrary"] = func(e *effects, n node) { e.add(searchToken(argN(n, 1))) }
	extAction["SearchTheTopNumberCardsOfLibrary"] = func(e *effects, n node) { e.add(searchToken(argN(n, 1))) }
	// "Search target player's graveyard, hand and library for the named
	// card and exile them": the scripts (Cranial Extraction's shape) exile
	// from each zone with its own ChangeZone, so a search plus a move from
	// the graveyard and from the hand.
	extAction["SearchPlayersLibraryAndGraveyardAndHand"] = func(e *effects, n node) {
		s := searchToken(argN(n, 1))
		dest := strings.TrimPrefix(s, "search:")
		e.add(s, "move:graveyard>"+dest, "move:hand>"+dest)
	}
	// Alternatives: exactly one branch runs.
	extAction["ValueActions"] = func(e *effects, n node) {
		var branches []any
		for _, va := range list(argN(n, 1)) {
			branches = append(branches, argN(va, 1))
		}
		e.alternatives(branches...)
	}
	// The branches are its own arguments: summarised here, never walked
	// again as children.
	leafActions["PlayerChooseAnAction"] = true
	extAction["PlayerChooseAnAction"] = func(e *effects, n node) {
		e.alternatives(list(argN(n, 1))...)
	}
	extAction["PlayerChoosesCostActionForEachPermanent"] = func(e *effects, n node) {
		var branches []any
		for _, o := range list(argN(n, 2)) {
			if variantOf(o) == "DoNothingOption" {
				branches = append(branches, rawArgs(o))
			} else {
				branches = append(branches, argN(o, 1))
			}
		}
		e.alternatives(branches...)
	}
	extAction["PutGraveyardCardIntoHandOrOntoBattlefield"] = func(e *effects, _ node) {
		e.merge([]*effects{{effects: []string{"move:graveyard>hand"}}, {effects: []string{"move:graveyard>battlefield"}}})
	}
}

// costAction maps a cost a player must pay as an action ("that player
// sacrifices it") to the verb doing it does.
func costAction(c any) []string {
	v := variantOf(c)
	switch {
	case strings.HasPrefix(v, "Sacrifice"):
		return []string{"sacrifice"}
	case strings.HasPrefix(v, "DiscardACard"):
		return []string{"discard:1"}
	case strings.HasPrefix(v, "GainControl"):
		return []string{"gaincontrol"}
	case v == "ShufflePermanentIntoLibrary":
		return []string{"move:battlefield>library"}
	case strings.HasPrefix(v, "Reveal"), strings.HasPrefix(v, "Choose"), v == "PayMana":
		return nil
	}
	return []string{unmapped("Cost." + v)}
}

// c2Chase follows the SVars an SA runs that svarRefs does not name. Some
// are alternatives of which exactly one runs (merged, like modes): a die
// roll's result table (ResultSubAbilities$ "1-9:A,10-20:B"), which mtgish
// writes as ValueActions, and a vote's options. Others are summed, as
// mtgish's walk sums them: a coin flip's win and lose (heads and tails)
// branches, which mtgish writes as separate conditioned actions, a
// regeneration's "when it regenerates this way" ability and an added
// phase's delayed trigger.
func (g *gorgeNorm) c2Chase(e *effects, a *cards.SA, depth int) {
	p := a.Params
	var alts []string
	switch a.API {
	case "RollDice":
		for _, r := range strings.Split(p["ResultSubAbilities"], ",") {
			if _, name, ok := strings.Cut(strings.TrimSpace(r), ":"); ok && name != "" {
				alts = append(alts, name)
			}
		}
	case "Regenerate", "RegenerateAll":
		if name := p["RegenerationAbility"]; name != "" {
			g.chain(e, cards.ResolveSVar(g.svars(), name), depth+1)
		}
	case "AddPhase":
		// Forge spells the parameter "Excute".
		if name := p["ExtraPhaseDelayedTriggerExcute"]; name != "" {
			g.chain(e, cards.ResolveSVar(g.svars(), name), depth+1)
		}
	}
	if len(alts) == 0 {
		return
	}
	var subs []*effects
	for _, n := range alts {
		sub := &effects{}
		g.chain(sub, cards.ResolveSVar(g.svars(), n), depth+1)
		subs = append(subs, sub)
	}
	e.merge(subs)
}

// c2PlayerTargets reports whether a ValidTgts$ names only players: a
// ChangeZone from the library that targets a player searches that
// player's library ("search target opponent's library"), as mtgish's
// SearchPlayersLibrary writes it.
func c2PlayerTargets(vt string) bool {
	for _, alt := range strings.Split(vt, ",") {
		head := strings.TrimSpace(alt)
		if i := strings.IndexAny(head, ".+"); i >= 0 {
			head = head[:i]
		}
		if w := typeWord(head); w != "player" && w != "opponent" {
			return false
		}
	}
	return vt != ""
}

// c2HiddenSearch reports whether a ChangeZone Origin$ lists the library
// among several zones ("Library,Graveyard", "Graveyard,Hand,Library").
func c2HiddenSearch(origin string) bool {
	zs := strings.Split(origin, ",")
	if len(zs) < 2 {
		return false
	}
	for _, z := range zs {
		if strings.TrimSpace(z) == "Library" {
			return true
		}
	}
	return false
}
