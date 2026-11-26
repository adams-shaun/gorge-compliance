package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestForgeRolledDie: the Forge request carries every die roll the lowered
// item recorded, on the step that made it, so the driver can force the same
// natural roll instead of rolling its own. The roll is not a gorge decision
// (Forge poses no decision for a die), so it must not be exported as one: a
// decision the driver never consumes would be an unconsumed leftover. The
// side field is also in Raw(), which is all gorge's runner reads, so the runner
// forces the same roll instead of drawing its own.
// The driver-side half of the behaviour -- that MyRandom.setRandom's wrapper
// forwards the bounded draw to the installed generator -- is pinned by
// forge-oracle's ForcedDieRollTest.productionSetRandomForwardsTheBound.
func TestForgeRolledDie(t *testing.T) {
	roll := oraclegen.DieRoll{Step: 1, Seat: 0, Sides: 6, Natural: 3, Result: 3}
	it := oraclegen.Item{
		ID:       "xe:die-roll",
		Template: lower.Template,
		DieRolls: []oraclegen.DieRoll{roll},
		Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
			{Op: "pass", Seat: 0},
			{Op: "activate", Seat: 0, Card: "p0:Ebony Fly"},
		}},
	}
	rq := forgeBuildRequest(nil, it, rules.OracleResult{}, nil, true)
	if !reflect.DeepEqual(rq.Item.DieRolls, []oraclegen.DieRoll{roll}) {
		t.Fatalf("request die rolls %+v, want %+v", rq.Item.DieRolls, roll)
	}
	if len(rq.GorgeDecisions) != 0 {
		t.Fatalf("die roll exported as a gorge decision: %+v", rq.GorgeDecisions)
	}
	if raw := string(rq.Item.Raw()); !bytes.Contains([]byte(raw), []byte(`"die_rolls":[{"step":1,"seat":0,"sides":6,"natural":3,"result":3}]`)) {
		t.Fatalf("Raw does not force the recorded roll on gorge's runner: %s", raw)
	}
	b, err := json.Marshal(rq)
	if err != nil {
		t.Fatal(err)
	}
	const want = `"die_rolls":[{"step":1,"seat":0,"sides":6,"natural":3,"result":3}]`
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("request JSON lost the forced roll:\n%s", b)
	}
}

// forgeOracleDieRollRef is the driver commit the die-roll contract was run
// against: 7f287e888f adds ForcedDieRandom, d2939e9b61 makes MyRandom forward
// the bounded draw to it, and the current FORGE_ORACLE_REF (95020b95d4 = the same driver re-picked onto upstream and merged with the fork's gorge branch; the die-roll commits are 880a93f52d and 8922a729bb there; 2aff202bc9 = b33362f600 plus
// a resolution-time payment hook that does not touch dice; becb01adc6 = 95020b95d4 plus the setup-token keyword-counter commit, die-roll code unchanged; 0791efdeb1 = becb01adc6 plus the coin-flip force, die-roll code unchanged and
// ForcedDieRollTest re-run green beside the new ForcedCoinFlipTest; 963576e5d2 = 96acdef442 plus the waterbend_generic tap pick consumed by the scripted controller, die-roll code unchanged,
// ForcedDieRollTest and ForcedCoinFlipTest re-run green on it; ff2049dc97 = 963576e5d2 plus the MayPlay land play outside the hand,
// die-roll code unchanged, the driver's unit tests re-run green on it; cda262a7ef = ff2049dc97 plus the OriginAlternative$/endure/same-name-order mapping, die-roll code unchanged,
// ForcedDieRollTest and the full driver suite (including LandPlayMayPlayTest, DecisionQueueTest and the AltSourceAnswer/OrderCards behavior tests) re-run green on it; f01192ae90 = cda262a7ef plus the remove-counter amount, Blight cost, lone-Chooser and pile-pick mapping, die-roll code unchanged, the full driver suite (including ForcedDieRollTest and ForcedCoinFlipTest) re-run green on it; c92f40d4f49 = f01192ae90 plus the counter-amount ask routing fix (only RemoveCounter takes the per-counter picks; CountersPutEffect counter_upto stays numeric) and its regression test, die-roll code unchanged, the full driver suite (121 tests) re-run green on it; 8b2a5e3141f = c92f40d4f49 plus the forced GenericChoice mode and different-name library order mappings, die-roll code unchanged, the full driver suite (124 tests) re-run green on it; ff02f60e839 = c92f40d4f49 plus the forced unless-type discard with no typed card in hand, the 0..0 empty pick and the ask-after-the-last-step guard, with ForcedAskTest, die-roll code unchanged, the full driver suite (124 tests) re-run green on it; 19b0c2902c5 = c92f40d4f49 plus the Phyrexian life payment in applyManaToCost, die-roll code unchanged, the full driver suite (123 tests) re-run green on it; f789f1e46fb = the clean merge of 8b2a5e3141f and 19b0c2902c5, die-roll code unchanged, the full driver suite (126 tests, including ForcedDieRollTest, PhyrexianLifePaymentTest and LoneModeLibraryOrderTest) re-run green on it; 14f1f8537c = ff02f60e839 + 19b0c2902c5, a clean merge of the forced-tail branch and the Phyrexian payment, which adds no code of its own, die-roll code unchanged; f822c4bfccdd = the clean merge of f789f1e46fb and 14f1f8537c, so the pinned driver keeps the lone-mode/library-order, forced-tail and Phyrexian fixes, die-roll code unchanged, the full driver suite (129 tests, including ForcedDieRollTest, ForcedAskTest, PhyrexianLifePaymentTest and LoneModeLibraryOrderTest) re-run green on it; 11936833e46 = f822c4bfccdd plus cast_mode suspend replayed as the no-stack Suspend special action with SuspendFromHandTest, die-roll code unchanged, the full driver suite (130 tests, including ForcedDieRollTest) re-run green on it; cd13258c4b7 = 11936833e46 plus the folded optional-trigger confirm in ScriptedController.confirmTrigger with FoldedOptionalConfirmTest, die-roll code unchanged; 7d2c3206368c = the merge of cd13258c4b7 and 348d2b59493d (11936833e46 plus the three optional-ask mappings: Optional$ sacrifice confirm, TapOrUntap binary, later unless-payer) plus the empty-tap and strict-optional-sacrifice fixes, die-roll code unchanged, the full driver suite (140 tests, including ForcedDieRollTest and FoldedOptionalConfirmTest) re-run green on it; ed6016bfeb = 7d2c3206368c plus the clash placement answer, die-roll code unchanged, the full driver suite (142 tests, including ForcedDieRollTest and ClashPlacementTest) re-run green on it; 1bb3cb16bb = ed6016bfeb plus the dredge replacement answer, die-roll code unchanged, the full driver suite (146 tests, including ForcedDieRollTest and DredgeReplacementTest) re-run green on it; c873c2c939 = 1bb3cb16bb plus the Unleash replacement answer, die-roll code unchanged, the full driver suite (148 tests, including ForcedDieRollTest and UnleashReplacementTest) re-run green on it; 08b8a9ba5d9 = 1bb3cb16bb9 plus Delve's exile picks, with DelvePicksTest, die-roll code unchanged, the full driver suite (150 tests, including ForcedDieRollTest and DelvePicksTest) re-run green on it; f77c569808 = the clean merge of 08b8a9ba5d9 and c873c2c939, so the pinned driver keeps the Delve and Unleash answers, plus the Delve role restricted to kind choose_n/gorge_kind choose so an arrange's "exile" remainder is not taken, die-roll code unchanged, the full driver suite (152 tests, including ForcedDieRollTest, DelvePicksTest and UnleashReplacementTest) re-run green on it; 08e1ddd978 = f77c569808 plus ForgetOtherRemembered cleared on an empty Dig (Primal Surge), with PrimalSurgeEmptyDigTest, die-roll code unchanged, the full driver suite (153 tests, including ForcedDieRollTest and PrimalSurgeEmptyDigTest) re-run green on it; bdc90a6b94 = c873c2c939 plus the trigger-order tie of copies and the venture dungeon pick, die-roll code unchanged, the full driver suite (151 tests, including ForcedDieRollTest and TriggerOrderTiedCopiesTest) re-run green on it; 41968040cb = the clean merge of f77c569808 and bdc90a6b94, so the pinned driver keeps both answers, die-roll code unchanged, the full driver suite (155 tests, including ForcedDieRollTest, TriggerOrderTiedCopiesTest, DelvePicksTest and UnleashReplacementTest) re-run green on it; 051f555862 = the clean merge of 08e1ddd978 (the Primal Surge empty-dig engine fix) and 41968040cb (the trigger-order tie of copies and the venture dungeon pick), so the pinned driver keeps both answers, die-roll code unchanged; 1eef2a952a = the clean merge of efdbea5198 and 051f555862, so the pinned driver keeps the zero-candidate decline, the Primal Surge empty-dig fix and the trigger-order tie of copies, die-roll code unchanged, the merge adds no code of its own and its parents ran 152 and 155 driver tests green; c12c06bcef = 051f555862 plus the energy confirm inside a scripted unless-pay with UnlessEnergyConfirmTest, die-roll code unchanged (it does NOT contain 1eef2a952a); 3494ec7d2bdc0ac257d891a230443c2b9b730ed2 = the clean merge of c12c06bcef and 1eef2a952a (parents c12c06bcef and 1eef2a952a), so the pinned driver keeps the unless-pay energy confirm and the zero-candidate decline, die-roll code unchanged, the full driver suite (162 tests, including ForcedDieRollTest, UnlessEnergyConfirmTest and ZeroCandidateConfirmTest) re-run green on it; 37726abafbec8036e1db34661ba9968098e9eb9e = 3494ec7d2bdc0ac257d891a230443c2b9b730ed2 plus the state-based-action graveyard batch order (engine change in GameAction.checkStateEffects; production driver source unchanged, adds the SbaGraveyardOrderTest driver test), die-roll code unchanged; c674c0977f6186a249ea18fba17d1af7f90345c3 = 37726abafbec plus the later-pass fix (the mixed-batch union no longer sets the per-list ordered flags, it remembers the ordered cards, so a later pass with one multi-card list still asks) and its two-pass regression test, die-roll code unchanged, ForcedDieRollTest re-run green on the pin's engine classes) contains both. TestForgeRolledDie only sees the Go request and stays green if the pin
// is reverted to 0958a1aca2, where Forge rolls its own d6 again; this pin check
// is the repo-side half. A bump must re-run the pinned driver's
// ForcedDieRollTest and update the constant deliberately.
//
// 2bf0a1cb36 = 3494ec7d2b plus the one-card order pin, die-roll code unchanged, ForcedDieRollTest re-run green in the full driver suite (166 tests, LoneCardOrderTest included).
// c3b04cf264b = the clean merge of 2bf0a1cb36 and c674c0977f (the SBA graveyard batch order pinned above), both branched
// from 3494ec7d2b; the two touch disjoint files (forge-oracle ScriptedController/StepMachine vs forge-game GameAction),
// so the pin keeps both fixes, die-roll code unchanged.
// 147dd2f00f = the clean merge of c3b04cf264 (the SBA graveyard batch order) and ba5d908b14 (the modal DFC land back
// face play), die-roll code unchanged, the full driver suite (170 tests, including ForcedDieRollTest,
// SbaGraveyardOrderTest and ModalLandPlayTest) re-run green on it.
// This check only compares the Makefile pin to this constant; it cannot tell whether the
// hash holds a fix. The behavioural evidence is the fork's LoneCardOrderTest (2bf0), SbaGraveyardOrderTest (c674)
// and ModalLandPlayTest (ba5d), and that the pin resolves on origin is checked by scripts/forge-oracle-setup.sh (git ls-remote / --check-pin).
// 9e368ca172 = 147dd2f00f plus the lone forced entity pick (ScriptedController/StepMachine only), die-roll code unchanged,
// the full driver suite (172 tests, including ForcedDieRollTest) re-run green on it.
// d1b608ce5d = 9e368ca172 plus the Join Forces number announce, LeftOrRight binary and Optional Attach confirm
// (ScriptedController/StepMachine only), die-roll code unchanged, the full driver suite (177 tests, including
// ForcedDieRollTest) re-run green on it.
// d0f16bee2e0 = the merge of a4fa8d45a56 and fef74c5308f, carrying all three fixes; die-roll code unchanged throughout.
// a4fa8d45a56 = the merge of 65c6664d90 and a445a858b2, carrying both fixes.
// 65c6664d90 = d1b608ce5d plus the ChooseEvenOddEffect binary (OddsOrEvens, true = Odd: Extinction Event's
// choose_n[odd] and Gollum, Riddle Master's choose_n[evenodd]), ScriptedController only, die-roll code unchanged,
// the full driver suite (179 tests, including ForcedDieRollTest) re-run green on it.
// 0c4ca7afb3d = d1b608ce5d plus repeated Confluence modes, the NICKNAME trigger-order label and the zero-option
// card ask (ScriptedController/StepMachine/DecisionQueue only), die-roll code unchanged, the full driver suite
// (181 tests, including ForcedDieRollTest) re-run green on it.
// a445a858b2 = d1b608ce5d plus the Myriad-only AtEOT empty-set guard (forge-game SpellAbilityEffect/CardFactoryUtil, driver and
// die-roll code unchanged), the full driver suite (177 tests, including ForcedDieRollTest) re-run green on it.
// fef74c5308f = the clean merge of 0c4ca7afb3d and a445a858b2: the two fixes touch disjoint files (forge-oracle driver vs
// forge-game), so the pin keeps both; die-roll and driver code are unchanged from 0c4ca7afb3d.
// f7b730149a = oracle/pin-1125: the merge of the two fixes below (both built on d0f16bee2e).
// a1bcd1166a = d0f16bee2e0 plus the cost-choice census (ScriptedCostDecision only), die-roll code unchanged, the full
// driver suite (185 tests, including ForcedDieRollTest and CostReturnPickTest) re-run green on it.
// 752947823d = d0f16bee2e plus the forge-game PhaseHandler second-combat-damage-step priority fix; die-roll and driver
// code unchanged.
// 70b77c0ff08 = the clean merge of 9b17c1fc83 (the coin-force fix) and 24a3344b81 (the fateseal Dig order); the two
// touch disjoint files (ForcedDieRandom vs ScriptedController/StepMachine), so the pin keeps both, die-roll code unchanged.
// 9b17c1fc83 = d0f16bee2e0 plus the removal of ForcedDieRandom's unbounded nextInt() flip fallback (an unrelated unbounded draw spent the
// armed coin flip); die-roll code (the bounded nextInt) unchanged, ForcedDieRollTest and ForcedCoinFlipTest re-run green on it.
// 24a3344b81 = d0f16bee2e0 plus the Dig order-to-Library answer from gorge's dig pick (ScriptedController/StepMachine
// only), die-roll code unchanged.
// 9d7baedf34 = d0f16bee2e plus the echo/cumulative-upkeep pay-or-sacrifice answer (ScriptedController only), die-roll
// code unchanged, the full driver suite (188 tests, including ForcedDieRollTest and EchoCumulativeUpkeepTest) re-run green on it.
// a3c422f79a6 = the clean merge of 9d7baedf34 and 70b77c0ff08 (echo/cumulative upkeep, coin force and fateseal Dig order;
// disjoint driver files), die-roll code unchanged, the full driver suite (192 tests, including ForcedDieRollTest,
// EchoCumulativeUpkeepTest, ForcedCoinFlipTest and FatesealDigOrderTest) re-run green on it.
// 528c55d25b4 = the clean merge of 8f537eddcbb (cast_mode "alternative", StepMachine.spellForMode only) and 304ae5ffe0e
// (the elected unless payment's whole-cost card pick, ScriptedController.chooseCardsForCost only); die-roll code unchanged.
// 244c5908482 = 528c55d25b4 plus cast_mode "mayplay" (StepMachine.spellForMode/spellFor only); die-roll code unchanged,
// ForcedDieRollTest and ForcedCoinFlipTest (18 tests) re-run green on it, and scripts/forge-oracle-test-mayplay-cast.sh
// replays the xproof Hurl Through Hell request.
// 3e6bd830a6f = 244c5908482 plus the discard_may election (ScriptedController.chooseCardsToDiscardFrom only); die-roll code
// unchanged, and the full driver suite (198 tests, ForcedDieRollTest and ForcedCoinFlipTest among them) is green on it.
// 7d506c5192 = oracle/fpin-1125: f7b730149a merged with 26b4658101 (chooseCounterType peeks the counter-kind pick for a
// RemoveCounter ask, driver-only), then with 244c590848 (the mayplay pin above); die-roll code unchanged.
// a6227e70a3 = 7d506c5192 plus cast_mode "evoked" (StepMachine.spellForMode picks the AlternativeCost.Evoke spell,
// CR 702.74a); afe18881c6 = oracle/fpin-1125: a6227e70a3 joined with 3e6bd830a6 (the discard_may election,
// ScriptedController.chooseCardsToDiscardFrom only); die-roll code unchanged.
// afe18881c6 = oracle/fpin-1125: 7d506c5192 (above) + evoked cast_mode a6227e70a3 + discard_may 3e6bd830a6 (above); driver-only.
// 3bb8f906e3 = afe18881c6 plus TokenEffect.resolve registering the AtEOT delayed trigger on a zero token amount (forge-game only);
// driver and die-roll code unchanged.
// 66671165f3 = afe18881c6 plus the up-to remove-any-type Done pick (StepMachine.peekCounterKind and
// ScriptedController.chooseCounterType only); die-roll code unchanged, ForcedDieRollTest re-run green in the full driver suite.
// 149a03bbbec4 = the clean merge of 66671165f31 and 3bb8f906e3 (disjoint files: forge-oracle driver vs forge-game TokenEffect);
// driver and die-roll code unchanged from 66671165f31.
// cf742d0aefc = afe18881c6e plus the forced remove-counter amount (ScriptedController.chooseNumber's RemoveCounter branch
// only), die-roll code unchanged, ForcedDieRollTest re-run green on it.
// f1e3e00905c = the merge of 149a03bbbec4 and cf742d0aefc (UnmappedAskTest keeps both cases): one pin carries the Done
// pick, the Mobilize X=0 fix and the forced remove-counter amount; die-roll code unchanged, ForcedDieRollTest re-run green on it.
// bfdb3a8051 = 149a03bbbec4 merged with 46203751a5 (foretell cast mode, Riot choice; StepMachine/ScriptedController only);
// die-roll code unchanged.
// c9419dd87a = oracle/fpin-1125: bfdb3a8051 (foretell, Riot; above) joined with f1e3e00905 (Done pick, Mobilize, forced remove-counter amount; above); driver-only.
// 0b7fde3680d = f1e3e00905c plus the zero-mode Charm fix (ScriptedController.chooseModeForAbility and StepMachine.hasMode
// only); die-roll code unchanged, ForcedDieRollTest re-run green on it.
// 9f2d35a4679 = the clean merge of c9419dd87a3 (foretell, Riot) and 0b7fde3680d (zero-mode Charm); both fork from f1e3e00905
// and touch forge-oracle only. Die-roll code unchanged; ForcedDieRollTest, ZeroModeCharmTest, ForetellSpecialActionTest and
// RiotChoiceTest re-run green on it.
// 2b591baf9c = c9419dd87a + ScriptedController.chooseCardName (empty face list) only; die-roll code unchanged.
// 0558bd00b88 = c9419dd87a merged with c332e2b33f5 (the unpinned transit Graveyard/Battlefield order, StepMachine.orderCards
// and OrderCardsTest only); die-roll code unchanged, ForcedDieRollTest re-run green on it.
// a1990dd5f833 = the clean merge of 9f2d35a4679 (zero-mode Charm) and 0558bd00b88 (the unpinned transit
// Graveyard/Battlefield order); the two touch disjoint hunks of StepMachine (hasMode vs orderCards) and separate
// ScriptedController/test files. Die-roll code unchanged.
// eb695169f9 = 2b591baf9c merged with 0558bd00b88 (the empty-face chooseCardName and the unpinned transit order); die-roll
// code unchanged.
// 141e1ff5403 = the clean merge of a1990dd5f833 (zero-mode Charm and the transit order) and eb695169f9 (the empty-face
// chooseCardName and the transit order); both carry the same 0558bd00b88 transit-order half and touch separate
// ScriptedController methods and test files. Die-roll code unchanged.
// 3d8f89a00d = eb695169f9 merged with 1a2655bbd3 (the Evoke sacrifice label binding, StepMachine.triggerScore only) and 2d4eac3d93 (the DigMultiple
// pick and AlternativeDecider position answers); die-roll code unchanged, ForcedDieRollTest re-run green on it (7 of 7).
// 68d36f8247a = the clean merge of 141e1ff5403 (zero-mode Charm, the empty-face chooseCardName and the transit order) and
// 3d8f89a00d (the Evoke trigger label, the DigMultiple pick and the AltDestination library position); both carry the same
// eb695/0558 halves and touch separate ScriptedController methods and test files. Die-roll code unchanged.
// 1da9dc5c96c = 68d36f8247a plus the one-card TwoPiles split (StepMachine.loneTwoPilesSplit and
// ScriptedController.chooseCardsForEffect only); die-roll code unchanged, ForcedDieRollTest re-run green on it.
// 65d8060f39ba = 1da9dc5c96c plus the empty Optional$ Animate confirm (ScriptedController.emptyOptionalAnimateConfirm only);
// die-roll code unchanged, ForcedDieRollTest re-run green on it.
// 931d6309cc3 = 65d8060f39ba plus the Optional$ Attach attach_optional yes/no (ScriptedController.attachConfirm only);
// die-roll code unchanged, ForcedDieRollTest re-run green on it (full driver suite, 227 tests).
// e99e48e2b50f = 65d8060f39ba plus the forced-pick-leaves-the-queued-pick fix (StepMachine.pickObjects only); die-roll code
// unchanged, ForcedDieRollTest re-run green on it.
// 85e4e85fde = 2ece06813a (1da9dc5c96c plus the dual-typed land colour filter, StepMachine.abilityFor only) merged with
// 65d8060f39ba; die-roll code unchanged, ForcedDieRollTest re-run green on it.
// 34f0585c97 = oracle/fpin-1125: 85e4e85fde (dual-land colour; above) joined with e99e48e2b5 (Gitrog lone forced pick; above); driver-only.
// 7cae17380d = 34f0585c97 plus the Room second-door cast mode (StepMachine.spellForMode only); die-roll code unchanged,
// ForcedDieRollTest re-run green on it.
// 0fe8c90e549 = oracle/fpin-1125-attach: 34f0585c97 (fpin-1125; above) joined with 931d6309cc3 (attach_optional; above); driver-only.
// d950b048823 = oracle/fpin-1125-room-alt: 0fe8c90e549 (attach_optional; above) joined with 7cae17380d (room_alt; above); driver-only.
// 1707ccc4018 = 0fe8c90e549 plus K'rrik's pay-2-life-for-{B} payment (ScriptedController); driver-only, die-roll code unchanged.
// 22d31c3e9d0 = oracle/fpin-1125-room-alt: d950b048823 (room_alt + attach_optional; above) joined with 1707ccc4018 (K'rrik pay life; above); driver-only.
// e76652fae3 = c5ab80906 (movectr + cantprevent; above) joined with 1707ccc4018 (K'rrik pay life; above); driver-only.
// a2201414482 = oracle/fpin-1125-room-alt: 22d31c3e9d0 (room_alt + attach_optional + K'rrik; above) joined with e76652fae3 (movectr + cantprevent + K'rrik; above); driver-only.
// 793a015fcf = oracle/fpin-1125-join3: a2201414482 (above) joined with 1947f2b322 (ScenarioReplay --serve warm worker; additive); driver-only, die-roll code unchanged.
// a8512930733 = 793a015fcf plus the unaffordable UnlessCost$ decline (ScriptedController.payCostToPreventEffect and
// UnlessCostUnaffordableTest); driver-only, die-roll code unchanged.
// 846c4191e6 = 793a015fcf (above) plus the unpinned order to Hand (StepMachine.keepsForgeOrderWithoutPin only);
// die-roll code unchanged, ForcedDieRollTest re-run green on it (7 of 7).
// 15a7c960ab2 = 793a015fcf plus the setup Room locked convention (SetupBuilder only); die-roll code unchanged.
// d6e26fd043 = 793a015fcf plus the checkpoint snapshot's offered (SnapshotWriter, RefTable.refOf, BridgeController.offer's withMana); die-roll code unchanged,
// ForcedDieRollTest and ForcedCoinFlipTest re-run green on it.
// 098e9c802e = oracle/fpin-1125-join5: 846c4191e6 (order to Hand; above) joined with d6e26fd043 (offered; above); die-roll code unchanged.
// 9cc07f0d340 = oracle/fpin-1125-join4: 15a7c960ab2 (setup Room locked) joined with d6e26fd043 (offered); die-roll code unchanged.
// 032ce65c73b = oracle/fpin-1125-join-hand-offered-room: 098e9c802e (order to Hand) joined with 9cc07f0d340 (setup Room locked); both already carry d6e26fd043 (offered); die-roll code unchanged.
// a862cd8abc = 793a015fcf plus the empty hidden-origin search fix (ScriptedController only); die-roll code unchanged, ForcedDieRollTest re-run green on it.
// 0218db82d91 = oracle/fpin-1125-join6: 9cc07f0d340 (setup Room locked + offered) joined with a862cd8abc (empty hidden search); die-roll code unchanged.
// 1b593040f66 = oracle/fpin-1125-join7: 032ce65c73b (order to Hand + setup Room locked + offered) joined with a862cd8abc (empty hidden search); die-roll code unchanged.
// 926004bb5396 = oracle/fpin-1125-join8: 032ce65c73b (order to Hand + setup Room locked + offered) joined with a8512930733
// (unaffordable UnlessCost$ decline); die-roll code unchanged.
// 6fcacdc21814 = oracle/fpin-1125-join9: 1b593040f66 (empty hidden search + order to Hand + setup Room locked + offered) joined with
// a8512930733 (unaffordable UnlessCost$ decline); die-roll code unchanged.
// b0ddb8556a = d6e26fd04306 plus the empty Optional$ ChangeTargets confirm (ScriptedController.emptyChangeTargetsConfirm only);
// die-roll code unchanged, ForcedDieRollTest re-run green on it (make forge-driver-tests).
// 019d50429f = b0ddb8556a plus the targeted ChangeTargets retarget pick (DecisionQueue/ScriptedController/StepMachine only);
// die-roll code unchanged, ForcedDieRollTest re-run green on it (make forge-driver-tests).
// 53059b9bbf2 = oracle/fpin-1125-join10: 6fcacdc21814 (join9; above) joined with 019d50429f (the ChangeTargets fix; above);
// die-roll code unchanged, ForcedDieRollTest re-run green on it (make forge-driver-tests).
// e45e911953c = 53059b9bbf2 (join10; above) plus the target ask whose candidates cannot reach min (TargetsBelowMinTest
// and fixtures targets-below-min/); die-roll code unchanged. Its pin bump left this constant stale and the TestForge
// suite red on develop; the re-run below is on the pin's descendant, which contains it.
// 8b89eabc655 = e45e911953c plus the PINNED transit graveyard order (forge-oracle only: Request.java parses
// graveyard_ends, StepMachine.orderGraveyardPin, OrderCardsTest's three pinned-transit tests); die-roll code unchanged,
// ForcedDieRollTest re-run green on it (forge-oracle/run-unit-tests.sh, 7 tests).
// 99e8228cba = 53059b9bbf2 plus the pending-declaration offered rule (SnapshotWriter only); die-roll code unchanged,
// ForcedDieRollTest re-run green on it (make forge-driver-tests).
// a4380dd47f = 99e8228cba with the pending-declaration rule narrowed to a pass entering the step (SnapshotWriter/StepMachine
// offered only); die-roll code unchanged, ForcedDieRollTest re-run green on it (make forge-driver-tests).
// 2ac6012982e = oracle/fpin-1125-join11: a4380dd47f (above) merged with e45e911953c (53059b9bbf2 plus the below-min target decline,
// StepMachine.targets only); die-roll code unchanged, ForcedDieRollTest re-run green on it (make forge-driver-tests).
// 6d6b2d485ab = oracle/fpin-1125-join12: 2ac6012982e (join11, above) merged with 8b89eabc655 (the pinned transit
// graveyard order, above), no textual conflicts; die-roll code unchanged, ForcedDieRollTest re-run green on it
// (make forge-driver-tests). r0's 1656bcb2a73 was described here but never pushed to the fork; this is the pushed merge.
// ad4cd3ba0f8 = oracle/fpin-1125-join14: 6d6b2d485ab (join12, above) merged with 892f8bd63b6 (the PayLife<X>
// paylife announcement, ScriptedCostDecision/ScriptedController only), no textual conflicts; die-roll code unchanged.
// 6095b085edc = ad4cd3ba0f8 (join14, above) plus the face-down cast modes morphed/megamorphed/disguised
// (StepMachine.spellForMode only, FaceDownCastModeTest); die-roll code unchanged, ForcedDieRollTest and
// ForcedCoinFlipTest re-run green on the pin (run-unit-tests.sh, 18 tests).
// 686e3f82713 = oracle/fpin-1125-join17: join15 (9124c696744, join14 plus the altaddcost tap ability choice) merged with
// 6095b085edc (above), no textual conflicts; die-roll code unchanged, ForcedDieRollTest re-run green on the pin.
// 9b233511947 = 686e3f82713 (above) plus the forced Hand->Library put-back placement order (StepMachine.seedHandLibraryPlacement,
// DecisionQueue.isLibraryPlacementOrder, ScriptedController zone-change asks); die-roll code unchanged.
// 0ca7a220bd0 = oracle/fpin-1125-join18 tip: join17 (above) merged with join16 (852c41880dd, the Speedball optional
// ChangeTargets confirm regression test) and a2954de67aa / 0ca7a220bd0 (the no-legal-new-target guard and its review r1
// retarget-count fix, ChangeTargetsNoCandidateTest), no textual conflicts; die-roll code unchanged, ForcedDieRollTest
// re-run green on the pin (run-unit-tests.sh).
// 47dd7358b32 = oracle/fpin-1125-join19: c72762cfadea (join18 tip: 0ca7a220bd0 above plus 53635f189c, the unbound optional-untap
// election answer) merged with 9b233511947 (the forced Hand->Library put-back, above), no textual conflicts (StepMachine/
// DecisionQueue vs ScriptedController ChangeTargets touch different areas; merge tree 5d6202a0c7cb); die-roll code unchanged,
// ForcedDieRollTest re-run green on the pin (run-unit-tests.sh).
// 495ed200e9f = 0ca7a220bd0 (join18, above) plus setup player_counters (SetupBuilder.placePlayerCounters, PlayerCountersSetupTest);
// die-roll code unchanged, ForcedDieRollTest re-run green on the pin (run-unit-tests.sh).
// 19c49ba7117 = 0ca7a220bd0 plus the "pN:ability:Name" stack-ability target ref (xpt-1d873918); die-roll code unchanged.
// d38946a0fd9 = 19c49ba7117 merged with 495ed200e9f, no textual conflicts (the two sides touch disjoint files); die-roll
// code unchanged.
// f3065a4da86 = oracle/fpin-1125-join20: 495ed200e9f (above) merged with 53635f189cc (the unbound optional-untap election
// answered at the ask; ScriptedController/StepMachine/SnapshotWriter only), no textual conflicts; die-roll code unchanged,
// ForcedDieRollTest run on the pin (run-unit-tests.sh).
// (merge) the pin stays at d38946a0fd9: f3065a4da86 lacks the ability-target ref; a fork join of both is pending.
// af167fe93c3 = 495ed200e9f plus the setup life written before any permanent exists (SetupBuilder.placeLife, SetupLifeReplacementTest);
// die-roll code unchanged, ForcedDieRollTest re-run green on the pin (run-unit-tests.sh).
// 18e28a7af59 = oracle/fpin-1125-join-setup-life: d38946a0fd9 merged with af167fe93c3, no textual conflicts; die-roll code unchanged.
// 64be1e6a7fe = oracle/fpin-1125-join-setup-life-untap: 18e28a7af59 merged with f3065a4da86 (untap election); die-roll code unchanged.
// 8765c682b2f = oracle/fpin-1125-join22: d38946a0fd9 (above) merged with a8da5c69719 (join20 f3065a4da86 = 495ed200e9f plus the
// optional-untap election answer, merged with 9b233511947), no textual conflicts; die-roll code unchanged, ForcedDieRollTest
// re-run green on the pin (run-unit-tests.sh).
// 30cbb3854d2 = oracle/fpin-1125-join-setup-life-handlib: 8765c682b2f merged with af167fe93c3 (setup life written before any permanent
// exists), no textual conflicts; die-roll code unchanged, ForcedDieRollTest re-run on the pin (run-unit-tests.sh).
// 52ddaba3075 = 30cbb3854d2 plus the keyword-expanded ability identity (StepMachine.abilityFor, Encore vs CopyPermanent); die-roll
// code unchanged, ForcedDieRollTest re-run on the pin (run-unit-tests.sh).
// efd75d00b34 = 52ddaba3075 plus the monarch trigger label role binding (StepMachine.triggerScore); die-roll code unchanged,
// ForcedDieRollTest re-run on the pin (run-unit-tests.sh).
// 71fd77c15c0 = efd75d00b34 plus StepMachine.pickObjects/pickFrom clamping a pick's min to the option count (ChooseFewerThanMinTest);
// die-roll code unchanged, ForcedDieRollTest re-run on the pin (run-unit-tests.sh).
// bdad3bd38b1 = 71fd77c15c0 plus the values-list chooseNumber answered from gorge's number pick (StepMachine.chooseNumberFrom);
// die-roll forcing unchanged, ForcedDieRollTest re-run green on the pin (run-unit-tests.sh, 278 tests).
// 4385467a9eb = bdad3bd38b1 merged with gorge's FORGE_REF 5bc5851a54 (three card scripts); `git diff bdad3bd38b1 4385467a9eb`
// touches only forge-gui/res/cardsfolder, so the driver and die-roll code are byte-identical to bdad3bd38b1.
// 7b6ff5656c8 = 4385467a9eb plus the cost-shape fallback in StepMachine.abilityFor (commander ninjutsu identity); die-roll code
// unchanged, ForcedDieRollTest re-run green on the pin (run-unit-tests.sh).
// 1dea7b45ce0 = 4385467a9eb plus the unpinned order-to-Exile ask kept (StepMachine.UNOBSERVABLE_ORDER_ZONES gains Exile,
// OrderCardsTest gains unpinnedExileOrderKeepsForgeOrder and loses Exile from unpinnedOtherZoneStillLooseMisses); StepMachine's
// order gate is not on the die-roll path, so the forcing is unchanged, and ForcedDieRollTest was re-run green on the pin
// (run-unit-tests.sh).
// 3526a90f719 = 1dea7b45ce0 plus the unpinned order-to-Command ask kept (UNOBSERVABLE_ORDER_ZONES gains Command,
// OrderCardsTest gains unpinnedCommandOrderKeepsForgeOrder); the order gate is still not on the die-roll path, so the forcing
// is unchanged, and ForcedDieRollTest was re-run green on the pin (run-unit-tests.sh).
// 5ceed32d687 = the merge of 7b6ff5656c8 and 1dea7b45ce0 (siblings on 4385467a9eb); neither touches die-roll forcing, and
// ForcedDieRollTest was re-run green on the merged pin (run-unit-tests.sh).
// edf8473c4d6 = join23 on the fork: 5ceed32d687 merged with 3526a90f719, so the pin carries the Command-order gate, the ninjutsu
// identity fix and the unpinned exile order together; none of the three touches the die-roll path, and ForcedDieRollTest was
// re-run green on the merged pin (run-unit-tests.sh with FORGE_ORACLE_DIR=forgeoracle-1120; the default env's forge-game jar
// (10-29) predates the record GameEventRollDie and does not compile any 11-2x pin, this one included).
const forgeOracleDieRollRef = "edf8473c4d64d76ccd35280b666aaa2257b5eff5"

// TestForgeOracleRefPinsForcedDieRoll fails when the Makefile's default driver
// pin moves off the commit whose forced-die behaviour was verified. It reads
// the committed Makefile rather than the environment: the pin is what a pass
// resolves when FORGE_ORACLE_REF is unset.
func TestForgeOracleRefPinsForcedDieRoll(t *testing.T) {
	// go test runs in the package directory. Walk up rather than using
	// runtime.Caller: test-targets builds with -trimpath, so the caller path
	// is the import path, not a file on disk.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var b []byte
	for {
		b, err = os.ReadFile(filepath.Join(dir, "Makefile"))
		if err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no Makefile above %s: %v", dir, err)
		}
		dir = parent
	}
	m := regexp.MustCompile(`(?m)^FORGE_ORACLE_REF\s*\?=\s*(\S+)`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s/Makefile has no FORGE_ORACLE_REF default", dir)
	}
	if got := string(m[1]); got != forgeOracleDieRollRef {
		t.Fatalf("FORGE_ORACLE_REF = %s, want %s: re-run the pinned driver's ForcedDieRollTest and update forgeOracleDieRollRef deliberately", got, forgeOracleDieRollRef)
	}
}
