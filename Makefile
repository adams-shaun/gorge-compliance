SHELL := /bin/bash

# gorge-compliance builds against the gorge commit go.mod requires (GORGE_PIN,
# docs/PINNING.md); a local, gitignored go.work may point at another checkout.
#
# GORGE_ROOT is the gorge checkout whose .cards corpus `make cards` links here.
# GORGE_MODULE_DIR is where go resolves gorge's code, usually the module cache,
# which has no corpus; gorge's corpus pin is read from its Makefile.
GORGE_ROOT ?= ../gorge
GORGE_MODULE_DIR = $(shell go list -m -f '{{.Dir}}' github.com/adams-shaun/gorge 2>/dev/null)

export GOMEMLIMIT ?= 5GiB
export GOFLAGS ?= -p=2 -trimpath
GO_TEST_FLAGS ?= -p=2

# The XMage commit the compliance oracle (manifests, out-of-tree driver)
# is pinned to. XMage is MIT; nothing from it is a build dependency.
# 7a68b79c92 = 4142596fb7 + fork PRs #11 (enters-tapped sets tapped without a TappedEvent: Attentive Sunscribe) and #12 (a
# static ability's conditional Custom/WhileControlled effect is not discarded on a false condition, CR 611.3a: Undercover Skrull).
# 33704ee4fe = 7a68b79c92 + fork PR #13 (Hellfire deals 3 when no creature died, X = 0, CR 608.2c).
# fdde07931f = b34a6018 (fork gorge) + fork PR #18 (the imprinted mana value X cost adjuster reads X = 0, not
# Integer.MAX_VALUE, when nothing is imprinted, CR 107.3c: Soul Foundry, Elite Arcanist, Prototype Portal).
# b964ce92f0 = fdde07931f + the playable check credits the untapped artifacts and creatures that can pay a
# Waterbend {X} cost's generic mana (CR 701.67a; CR 701.67b caps it to the minimum X part): Katara, Water Tribe's Hope
# with an empty pool, Waterbend {X}, X = 1, tapping Katara herself. Only WaterbendXCost with a minimum X above 0 is
# credited (the three spells with a Waterbend {X} cost have minimum X 0 and are unchanged; a fixed WaterbendCost
# already offers a {0} option). Fork branch fix/waterbend-playable, WaterbendTest.testXFromEmptyPool.
# 3481be1d2b = b964ce92f0 + fork #19 (Gruul Scrapper's "if {R} was spent to cast it" is an intervening-if, CR 603.4:
# with no red spent the ETB haste trigger does not go on the stack at all, where the plain trigger plus a
# ConditionalContinuousEffect put it there and read the condition only at resolution). Gorge now also carries
# the b964ce92f0 ancestry, so the pin is a commit on the gorge branch itself.
# 7aeb547a86 = 3481be1d2b + fork #20 (an "X target ..." spell whose count can be 0 is playable with no candidate for
# its blueprint target, CR 601.2b/601.2c: Death Denied with no creature card in its controller's graveyard, X = 0;
# the same gap covered every TargetsCountAdjuster user with a min-1 blueprint). Fork branch fix/xtargets-x-zero,
# DeathDeniedTest.
# 228d8a4bd9 = 7aeb547a86 + fork branch fix/champion-sacx-zero ("Sacrifice X other creatures": the cost's maximum X counted the
# source itself, so Champion of Stray Souls alone could announce X = 1 and an unscripted announce then failed the activation
# for want of a graveyard target; CR 601.2b/602.2b), ChampionOfStraySoulsTest.
# ec270409d0 = 228d8a4bd9 + fork #21 (declare blockers and combat damage are skipped only when no creature was
# declared as an attacker or put onto the battlefield attacking; a declared attacker that later leaves combat does not
# un-declare the attack, CR 508.8: Daemogoth Titan's own attack trigger sacrifices it, Duergar Assailant sacrifices
# itself to its damage ability). Fork branch fix/attacker-removed-combat-steps, AttackerRemovedCombatStepsTest.
# f07513ff8f = ec270409d0 + fork PR #22 (re-stacked on the #21 pin so both are carried): TRK's Play Booster
# collation (the set still carried the spoiler-period `// this.enablePlayBooster(276)` waiver, so
# VerifyCardDataTest failed the set check "wrong booster settings (set must have boosters, but it does not)"),
# HML 56 Sengir Autocrat as Rare (mtgjson, Scryfall and upstream master agree; the same Verify run reported the
# stale Uncommon once the mtgjson snapshot updated), and surefire failIfNoSpecifiedTests=false so a targeted
# `mvn -pl Mage.Verify -am test -Dtest=VerifyCardDataTest` reaches Mage.Verify instead of failing in a dependency
# module with no matching test. Set metadata and test-runner config only: no card behaviour, so the manifests are
# unchanged.
# 464e1a6b23 = f07513ff8f merged with fork gorge c259b72036 (which carries #22's three commits, so the merge adds only):
# fork #23 (Volatile Arsonist's attack trigger damages each chosen target, not only the creature slot, CR 601.2c:
# EachTargetPointer, VolatileArsonistTest) and fork #24 (Artifact Ward is its three Oracle abilities, not protection
# from artifacts, so artifact Equipment stays attached, CR 702.16d: ArtifactWardTest). Fork branch pin/xmage-ref-arsonist-ward.
# 791c9c554a = 464e1a6b23 + fork PR #25 (re-stacked on the #23/#24 pin so it is carried: "if a card is exiled with it"
# is an intervening-if, CR 603.4. Redemptor Dreadnought's attack trigger and Evercoat Ursine's combat-damage trigger
# went on the stack with nothing exiled with the permanent and read the exile zone only at resolution; both now use
# .withInterveningIf with a condition reading the source's exile zone). Fork branch pin/redemptor-iif-464, the same
# five-file change as 9bb2f33757; the card fix itself is fix/redemptor-dreadnought-intervening-if,
# RedemptorDreadnoughtTest and EvercoatUrsineTest. Card behaviour change only for those two cards, so the manifests
# are unchanged.
# dc6da81e81 = 791c9c554a + fork branch fix/stackobject-self-target-on-791 (TargetStackObject drops the casting spell's
# own stack object from canTarget / possibleTargets, CR 115.5: Katara's Reversal's "up to four target spells and/or
# abilities" offered the Reversal itself as its one candidate on an empty stack, so the group could not be skipped
# and the lone-candidate rule countered the spell itself). One target class, no set data: the manifests are unchanged.
# fdbae29b12 = dc6da81e81 + fork branch fix/no-witnesses-zero-creatures (No Witnesses: "each player who controls the most
# creatures investigates" counts a player with no creatures as zero, so with nobody controlling a creature every player is
# tied for the most and each investigates, CR 608.2; the effect returned early on an empty creature map so nobody did).
# NoWitnessesTest. Card behaviour change only for that card, so the manifests are unchanged.
# 30c62e641b = fdbae29b12 + fork branch fix/leaves-graveyard-self-enter (CardsLeaveGraveyardTriggeredAbility.checkTrigger returns
# false when the batch holds the source's own non-battlefield-to-battlefield move, CR 603.10a: a leaves-the-graveyard
# ability looks back to the state before the event, where Skeleton Crew / Rot Farm Mortipede sat in the graveyard and
# their battlefield-only trigger did not function, so returning themselves from it triggered nothing; XMage checked the
# batch after the move and stacked the trigger). RotFarmMortipedeTest and SkeletonCrewTest.test_ReturnItself_NoTrigger
# (the fork carried it @Ignore'd). Card behaviour change only for the CardsLeaveGraveyardTriggeredAbility cards, so the
# manifests are unchanged.
XMAGE_REF  ?= 30c62e641b379a0a888f04412ffa59b8ebe0a618
# The out-of-tree oracle builds default to gitignored directories here. They run
# to several GB, so point these at a shared disk to reuse one build across clones.
XMAGE_ORACLE_DIR ?= .oracle-build/xmage
# The XMage *seat* engine (scripts/xmage-sbv2.sh, docs/design/2026-11-24-xmage-seat-route.md): SpellBench
# upstream's engines/xmage (MIT) at this revision, building the XMage commit below (its pins.env must agree).
# Not the oracle's XMAGE_REF: the two builds coexist and a verdict is never measured on one and cited on the other.
XMAGE_SBV2_REF ?= 1945abdae4c22c2a3a95cc8acaabcf49bd777bdf
XMAGE_SBV2_XMAGE_REF ?= fd40ad5c29a92cef824cf12ba6d0e4daa25db975
XMAGE_SBV2_DIR ?= .oracle-build/xmage-sbv2
# The mtgish commit the card-definition cross-check reads its data at
# (oracle/mtgish/MTGISH.lock holds the same ref and the files' sha256).
# mtgish is MIT; its data carries Oracle text and is never committed.
MTGISH_REF ?= 1553384c9a970a02514b337db9c86697313530e8
# gorge's corpus pin and Forge fork, read from gorge's Makefile.
FORGE_REPO ?= $(shell awk '/^FORGE_REPO/ {print $$3}' $(GORGE_MODULE_DIR)/Makefile 2>/dev/null)
FORGE_REF  ?= $(shell awk '/^FORGE_REF/ {print $$3}' $(GORGE_MODULE_DIR)/Makefile 2>/dev/null)
# Forge oracle driver commit (fork, branch oracle-controlled-player-mana, based on
# oracle-untap-upto-noop-pick): a descendant of FORGE_REF with identical script trees.
# d2939e9b61 = 7f287e888f (below) + MyRandom's counting wrapper forwards bounded
# nextInt(bound) to the supplied generator: at 7f287e888f the wrapper routed it through
# next(bits), which calls the delegate's UNBOUNDED nextInt(), so ForcedDieRandom.nextInt(bound)
# never ran on RollDiceEffect's MyRandom.getRandom().nextInt(sides) path and the follower
# rolled its own d6 (Ebony Fly 5/5). Same cardsfolder and tokenscripts trees as
# FORGE_REF 418ba5215f.
# 7f287e888f = 0958a1aca2 (oracle-harness-setup-entry-counter: 327ad7fb80 + setup keeps every entry
# counter the real move applies, e.g. Tonberry's stun counter) + the driver forces each step's
# recorded die result (item.die_rolls) before the step runs, so a follower accepts the tape's natural
# roll instead of rolling its own (Ebony Fly's d6); same cardsfolder and tokenscripts trees as
# FORGE_REF 418ba5215f.
# 2c9710b20a = f4ab928cb3 (below) + setup states a back-face instant/sorcery straight on the
# battlefield: flipping first made a front-permanent/back-spell modal DFC reach GameAction.moveTo
# as a spell, which CR 307.4 refuses and leaves in hand (Augmenter Pugilist // Echoing Equation and
# the nine such modal DFCs); the attached-Aura path and gorge's setup fold already add directly.
# f4ab928cb3 = d2939e9b61 + oracle-harness-setup-backface: setup resolves a back_face card's back
# face before the battlefield move, so a modal DFC whose front is an instant/sorcery enters as its
# back face instead of staying in hand (CR 307.4, 712.8f; Sea Gate Restoration // Sea Gate, Reborn
# and every such modal DFC); the entering face's own replacements apply, and a back-face Saga keeps
# its entry lore and chapter I. Same cardsfolder and tokenscripts trees as FORGE_REF 418ba5215f.
# b33362f600 = 2c9710b20a + an up-to object pick that Forge does not offer is
# consumed as a no-op instead of falling back to the AI or binding it to a
# same-named sibling: UntapEffect offers only permanents that can untap
# (forge-game/.../UntapEffect.java:116-119), so Peregrine Drake's recorded
# untap of an already-untapped Island stays a no-op (CR 608.2d, 701.26b) and
# p0:Island#5 stays tapped. The same helper answers every object pick, so a
# tap/untap/sacrifice/discard up-to pick absent from Forge's candidates can no
# longer change the board. Same cardsfolder and tokenscripts trees as
# FORGE_REF 418ba5215f.
# 2aff202bc9 = b33362f600 + a play/cast that happens while a spell resolves
# pays from the sidecar's named decider's own sources when the pool is short:
# Word of Command's Play of the chosen card (CR 723.2, 723.5a, 723.7, 701.18b)
# and every Play with Controller$ TargetedPlayer/TriggeredPlayer that casts
# during its own resolution (Spell Queller's RememberedOwner cast). The resolve
# arm arms the decider from its "play" decision and the payment hook activates
# that player's own mana abilities in zone order; Forge's CantBeActivated
# statics still filter them. The hook consumes the play decision on both the
# shortfall and the pool-covered paths, so a recorded play that the pool
# already pays for cannot leave a leftover decision. A cast with no such
# decision keeps paying from the recorded pool. Same cardsfolder and
# tokenscripts trees as FORGE_REF 418ba5215f.
# 95020b95d4 = the fork's master: the oracle driver re-picked onto upstream (every driver
# commit of 2aff202bc9, which it carries as a cherry-pick) merged with the fork's gorge branch
# (ff076d4432 = Card-Forge/forge master 9db1009ef8 plus the card-script fixes). A descendant of
# FORGE_REF ff076d4432 with identical cardsfolder and tokenscripts trees.
# becb01adc6 = 95020b95d4 + setup tokens' keyword counters grant their keyword (Card.setCounters(Multiset) once;
# keyword counters resolve before custom ones: Zombie Druid LIFELINK, Alesha's Legacy). Same trees as FORGE_REF ff076d4432.
# 0791efdeb1 = becb01adc6 + each recorded coin flip result forced on its step (ForcedDieRandom arms item.coin_flips
# beside the die rolls: nextBoolean returns the recorded result, a forced flip takes no draw, a doubled flip one per
# result; MyRandom's CountingRandom forwards nextBoolean). Same trees as FORGE_REF ff076d4432.
# 963576e5d2 = 96acdef442 (compliance #432's pin: the fork's master + gorge branch, re-synced 11-24) + the scripted controller
# consumes gorge's waterbend_generic tap picks (Forge asks for waterbend taps through chooseCardsForConvokeOrImprovise, which took
# only convoke*/improvise* kinds: Katara's Waterbend {X} paid from an empty pool and was refused). Driver-only change; same trees
# as FORGE_REF ff076d4432. Fork branch fix/waterbend-tap-pick-driver.
# ff2049dc97 = 963576e5d2 + landFor plays a land from outside the hand through the copy a MayPlay static grants (Ka-Zar's
# library top, the card Irascible Wolverine / Kulrath Zealot / Crimson Operative exile, Tablet of Discovery's milled card):
# the card's own land ability plays only from the hand, so the play did nothing ("step N (play) p0:<land> was not played").
# Driver-only change; same trees as FORGE_REF ff076d4432. Fork branch fix/land-play-mayplay.
# cda262a7ef = ff2049dc97 + the scripted controller answers gorge's OriginAlternative$ zone election: one choose_n over zones
# becomes Forge's initial "search the library?" confirm plus one confirm per alternative zone, and the Optional$ search_confirm is
# left for Forge's later ChangeZoneGeneral gate; Endure's endure_counters/endure_spirit map to yes/no; and a same-name library
# order (dig/hideaway bottom) is consumed though its order is unobservable, so it is not a leftover. Driver-only change; same
# trees as FORGE_REF ff076d4432. Fork branch fix/xp-forge-driver-unmapped-asks, rebased onto ff2049dc97 so the MayPlay
# land-play fix is kept beside the zone-election/endure/order fix.
# f01192ae90 = cda262a7ef + the scripted controller maps the four asks gorge records but Forge poses differently:
# remove-any-type counters (CountersRemoveEffect's chooseCounterType then the params chooseNumber; gorge's one choose_n per
# counter removed is the amount), Blight as a one-creature cost pick, a lone Separator$/Chooser$ as a forced entity (not an
# object pick), and gorge's twopiles_pick as chooseCardsPile. The params chooseBinary overload is overridden too, so it cannot
# fall through to PlayerControllerAi. Driver-only change; same trees as FORGE_REF ff076d4432. Fork branch
# fix/xp-forge-driver-counter-blight-piles, based on cda262a7ef.
# c92f40d4f49 = f01192ae90 + the params chooseNumber branches on the ask's API: only RemoveCounter's per-counter
# kind election takes the counter picks, while CountersPutEffect's UpTo$/SplitAmount$ number (gorge resume
# counter_upto) keeps the four-argument path, so an up-to put answers the recorded amount instead of 0. A
# counter_upto regression fixture/test covers it, and the params chooseBinary comment now states the miss it
# really records. Driver-only change; same trees as FORGE_REF ff076d4432. Fork branch
# fix/xp-forge-driver-counter-blight-piles.
# ff02f60e839 = c92f40d4f49 + the three forced / out-of-window asks xproof found. chooseCardsToDiscardUnlessType
# asks min..min when no unless-type card is in hand, so Alpharael's two-card hand is taken as the forced discard
# (the old lower bound of 1 read it as 1..2 over two options); pickObjects answers a 0..0 ask with the empty pick,
# so Death Cloud cast with X = 0 sacrifices none of its Swamps instead of missing at 3 options; and miss() notes and
# answers asks that arrive after the last step's priority, when the following pass resolves a spell the last step
# left on the stack (Coil and Catch's discard after the Marang item ends). ForcedAskTest replays the three xproof
# requests in strict mode. Driver-only change; same trees as FORGE_REF ff076d4432. Fork branch
# fix/xp-forge-driver-forced-tail-asks, based on c92f40d4f49.
# 19b0c2902c5 = c92f40d4f49 + the scripted controller pays a Phyrexian pip with 2 life (CR 107.4f, 118.3b) when gorge
# recorded a pay_life pick: applyManaToCost takes the step's pay_life picks first (payPhyrexian + payLife(2)), then the
# recorded spend and the pool; a pip with no pay_life pick stays a mana pip. Before, Vault Skirge's {B/P} stayed unpaid and the
# cast was refused. PhyrexianLifePaymentTest covers Vault Skirge and Immolating Souleater. Driver-only change; same trees as
# FORGE_REF ff076d4432. Fork branch fix/xp-forge-driver-phyrexian-life.
# 8b2a5e3141f = c92f40d4f49 + two logged gorge decisions the driver never consumed: a forced GenericChoice mode
# (ChooseGenericEffect drops every choice whose UnlessCost the chooser cannot pay and still calls
# chooseSpellAbilitiesForEffect with the survivors, so Forge asks nothing while gorge logs the one-option mode)
# and a different-name library order (dig/hideaway bottom, library placement: orderCards missed every
# non-graveyard order instead of answering it from the card-order picks, with the graveyard pin kept separate).
# Both shapes have a forge-export fixture replayed by LoneModeLibraryOrderTest. Driver-only change; same trees
# as FORGE_REF ff076d4432. Fork branch fix/xp-forge-driver-lone-mode-library-order.
# f789f1e46fb = the merge of 8b2a5e3141f (this lone-mode/library-order fix) and 19b0c2902c5 (the Phyrexian life
# payment fix), both children of c92f40d4f49; the merge is clean and the full driver suite (126 tests, including
# PhyrexianLifePaymentTest and LoneModeLibraryOrderTest) is green on it. Fork branch fix/xp-forge-driver-lone-mode-library-order.
# 14f1f8537c = ff02f60e839 + 19b0c2902c5: the forced-tail branch merged with the Phyrexian life payment, so the pinned
# driver keeps both fixes (the branch had been based on c92f40d4f49, before the Phyrexian fix landed on develop). The
# merge is clean and changes no file of its own; cardsfolder/tokenscripts are unchanged (same as FORGE_REF ff076d4432).
# Fork branch fix/xp-forge-driver-forced-tail-asks.
# f822c4bfccdd = the clean merge of f789f1e46fb (the lone-mode/library-order fix plus the Phyrexian life payment)
# and 14f1f8537c (the forced-tail fix plus the Phyrexian life payment), so the pinned driver keeps all three fixes.
# The merge changes no file of its own; cardsfolder/tokenscripts are unchanged (same as FORGE_REF ff076d4432).
# The full driver suite (129 tests, including ForcedAskTest, PhyrexianLifePaymentTest and LoneModeLibraryOrderTest)
# is green on it. Fork branch fix/xp-forge-driver-lone-mode-library-order.
# 11936833e46 = f822c4bfccdd + cast_mode "suspend" (CR 702.62a, 116.2f): Suspend from hand is a special action that
# uses no stack. StepMachine.spellForMode picks the card's Suspend ability (the non-spell ability built for
# K:Suspend:N:cost) and checkActed accepts it when the card is in exile with a time counter, noting the count, instead
# of insisting the card be on the stack ("was not put on the stack"). SuspendFromHandTest replays Lotus Bloom's xproof
# request in strict mode; the full driver suite (130 tests) is green on it. Driver-only change; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-suspend-cast-mode.
# cd13258c4b7 = 11936833e46 + the optional-trigger confirm gorge folded into the body's Cost$ election (CR 603.5): a
# trigger whose body has a non-Mandatory Cost$ ("you may tap X untapped Myr") gets no logged yes/no when the cost is
# forced (X = 0), yet Forge's OptionalDecider confirms anyway. ScriptedController.confirmTrigger now answers yes when
# gorge logged an unconsumed X or object pick for the seat in the step, else no (noted); a trigger that is not
# cost-folded still strict-misses. FoldedOptionalConfirmTest pins it (driver suite 134 tests, 132 green plus the two
# ForcedDieRollTest/LoneModeLibraryOrderTest cases that also fail on 11936833e46 in the borrowed build). Driver-only
# change; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-folded-optional-confirm.
# 7d2c3206368c = the merge of 348d2b59493d (the three optional asks xproof found) and cd13258c4b7 (the folded
# optional-trigger confirm above), so the pinned driver keeps both fixes. 348d2b59493d: an Optional$ sacrifice's
# yes/no (SacrificeEffect asks confirmAction before choosePermanentsToSacrifice; gorge logs the whole 0..N pick as one
# choose_n, so a non-empty pick answers yes and stays queued for the pick, an empty pick answers no and is
# consumed, and a SacValid$ Self sacrifice consumes its pick at the confirm), a TapOrUntap binary (gorge's
# taporuntap/taporuntapall decisions, true means tap), and a later unless-payer once one has paid
# (AbilityUtils.handleUnlessCost asks every payer with alreadyPaid; gorge asks each payer only until one pays, so
# a later seat with no decision declines without a miss). On top of it (02db800b3): an EMPTY tap-or-untap answer
# reads as tap (gorge's taporuntap contract), and an Optional$ + StrictAmount$ sacrifice (Giant Opportunity) reads
# its yes/no from gorge's sacrifice_optional mode ask, consumed at the confirm, the exact pick left for
# choosePermanentsToSacrifice. OptionalAskTest (6 tests) and FoldedOptionalConfirmTest (4) pin them; the full driver
# suite (140 tests, including ForcedDieRollTest) is green on the merge. Driver-only change; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-optional-asks.
# ed6016bfebb6 = 7d2c3206368c plus the clash placement ask (xproof decision_shape: Adder-Staff Boggart, Release the
# Ants, Titan's Revenge, Gilt-Leaf Ambush, Recross the Paths, Woodland Guidance). CR 701.30a: gorge asks each clashing
# player once, a 1-of-2 pick (resume clash_placement, option kinds top/bottom); Forge's ClashEffect asks
# willPutCardOnTop (true means top), which the scripted controller now answers from that pick instead of missing.
# ClashPlacementTest (2 tests: bottom/bottom, and top for one seat with bottom for the other) pins it; the full driver
# suite (142 tests, including ForcedDieRollTest) is green. Driver-only change; cardsfolder/tokenscripts unchanged
# (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-clash-placement.
# 1bb3cb16bb9 = ed6016bfebb6 plus Dredge's replacement confirm (xproof decision_shape: Grave-Shell Scarab). CR 702.52a:
# gorge asks once per draw as a mode decision (resume dredge) over every graveyard dredger plus the ordinary draw; Forge
# builds Dredge as an Optional$ replacement and asks chooseSingleReplacementEffect (several dredgers) then
# confirmReplacementEffect, which the scripted controller now answers from that pick instead of missing. A dredge pick
# naming another host is declined without consuming it, and the ordinary draw is consumed only on the last dredger, so
# several dredgers all get the same no. DredgeReplacementTest (4 tests) pins it. Driver-only change;
# cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-dredge-replacement.
# c873c2c9399 = 1bb3cb16bb9 plus Unleash's optional ETB replacement confirm (xproof decision_shape: Dead Reveler and the other
# Unleash cards). CR 702.98a: gorge asks the controller once as a choose_n (resume etb) whose two options both have pick
# kind unleash, "Enter with a +1/+1 counter" and "Enter without a counter"; Forge builds Unleash as an Optional$ ETB
# replacement and asks confirmReplacementEffect, which the scripted controller now answers from that pick (the counter is
# yes, the other label or an empty pick is no) instead of missing and leaving the pick unconsumed. UnleashReplacementTest
# (2 tests: both answers) pins it; the full driver suite (148 tests, including ForcedDieRollTest) is green. Driver-only
# change; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-unleash-replacement.
# 14070cbbac4 = c873c2c9399 plus the zero-candidate Optional$ hidden ChangeZone decline (xproof decision_shape: Bruna, Light of
# Alabaster, 4 items). ZoneAuras ("any number of Aura cards that could enchant it from your hand and/or graveyard", ChangeNum$
# CountAuras) asks "Search your hand and graveyard?" with no Aura card there; gorge, a choice of zero cards, logs no ask, so the
# confirm was an unanswered strict miss. ScriptedController.confirmAction answers no when no yes/no is queued for the seat and
# nothing can move. ZeroCandidateConfirmTest (2 tests) pinned it; efdbea5198 below fixes its candidate filter and cancel handling.
# The full driver suite (150 tests, including ForcedDieRollTest) was green on it. Driver-only change; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-zero-candidate-confirm.
# efdbea5198 = 14070cbbac4 plus the two findings on that decline: the candidate scan now uses ChangeZoneEffect's own filter
# (AbilityUtils.filterListByType, with an EACH ... & ... type split per part) instead of Card.isValid on the raw ChangeType$,
# so EACH (Michelangelo, Improviser; Yuna's Decision) and Triggered.sameName (Verdant Succession) keep their strict miss when
# a candidate is present; and Forge's "Cancel Search" ChangeZoneGeneral prompt (the effect's post-pick cancel loop, not the
# Optional$ gate) is excluded, so with no search carry in flight -- including after a recorded empty pick -- it stays a
# strict miss instead of answering no and making the effect loop. The origin check now precedes the ChangeNum$ shortcut, so
# Battlefield/Stack origins are never declined. ZeroCandidateConfirmTest (4 tests) pins it; the full driver suite (152 tests,
# including ForcedDieRollTest) is green. Driver-only change; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432).
# Fork branch fix/xp-forge-driver-zero-candidate-confirm.
# 08b8a9ba5d9 = 1bb3cb16bb9 plus Delve's exile picks (CR 702.66a; xproof harness: Dig Through Time, Empty the Pits,
# Dead Drop). gorge poses the picks as one choose whose every pick kind is "exile"; Forge's CostAdjustment asks
# chooseCardsToDelve, which returned an empty collection, so the generic mana stayed unpaid and the cast was refused.
# The driver consumes the ask, maps each pick to a distinct offered graveyard card (same-named copies one per pick) and
# caps at the unpaid generic. DelvePicksTest (4 tests) pins it. Driver-only change; cardsfolder/tokenscripts unchanged
# (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-delve-picks.
# f77c569808 = the clean merge of 08b8a9ba5d9 (Delve) and c873c2c939 (Unleash), so the pinned driver keeps both answers.
# The Delve role is restricted to kind choose_n / gorge_kind choose: an arrange also uses "exile" as the destination of
# its unpicked cards, so the bare pick-kind test could take the wrong ask. DelvePicksTest pins that boundary. The full
# driver suite (152 tests, including ForcedDieRollTest, DelvePicksTest and UnleashReplacementTest) is green on the merge.
# Driver-only change; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-delve-picks.
# 08e1ddd978 = f77c569808 plus the one forge-game engine fix for xproof decision_shape Primal Surge (4 items). DigEffect honoured
# ForgetOtherRemembered only when something was dug; Primal Surge repeats while Remembered is a permanent, so on the pass over
# the empty library the last Forest stayed Remembered (already on the battlefield) and DBChangeZone asked a spurious
# "move Forest from exile" confirm (the 30th over a 29-Forest library; gorge stops at 29). The clear
# now also runs when nothing is dug. Primal Surge is the only card pairing Dig with ForgetOtherRemembered. Driver tests: 153
# (PrimalSurgeEmptyDigTest counts exactly 29 confirms; it fails without the engine change). cardsfolder/tokenscripts unchanged
# (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-primal-surge-empty-dig.
# bdc90a6b94b = c873c2c9399 plus the trigger-order tie of copies (xproof decision_shape: Dromoka, the Eternal). CR 603.3b: with
# two Dromoka triggers and Nadaar's venture simultaneous, gorge orders [Dromoka, Dromoka, Nadaar]; the two Dromoka triggers tie
# on the label and are copies of one trigger, but orderTriggers tested sameEffect on ALL remaining triggers (Nadaar's included)
# and strict-missed "binds no unique Forge trigger". It now tests only the triggers tied at the best score and takes the first
# copy (their order is unobservable); a tie between different effects still misses. TriggerOrderTiedCopiesTest (2 tests) pins
# both. The find's replay (dromoka-the-eternal.request.jsonl) also pins Nadaar's venture: once the order binds, Forge's
# chooseSingleCardFace asks for the dungeon and gorge's choose_n[dungeon] pick (CR 701.49a) now answers it instead of going
# unconsumed (a second strict miss the order miss had masked). The full driver suite (151 tests) is green. Driver-only change; cardsfolder/tokenscripts unchanged. Fork branch
# fix/xp-forge-driver-trigger-order-tied-copies.
# 41968040cb = the clean merge of f77c569808 (Delve + Unleash) and bdc90a6b94 (the trigger-order tie of copies and the
# venture dungeon pick), so the pinned driver keeps both answers. The merged driver's full suite (155 tests, including
# ForcedDieRollTest, TriggerOrderTiedCopiesTest, DelvePicksTest and UnleashReplacementTest) is green. Driver-only change;
# cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-trigger-order-tied-copies.
# 051f555862 = the clean merge of 08e1ddd978 (the Primal Surge empty-dig engine fix) and 41968040cb (the trigger-order tie
# of copies and the venture dungeon pick), so the pinned driver keeps both answers. The merge adds no file of its own and
# leaves cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-primal-surge-empty-dig.
# 1eef2a952a = the clean merge of efdbea5198 (the zero-candidate Optional$ hidden ChangeZone decline) and 051f555862
# (the Primal Surge empty-dig engine fix plus the trigger-order tie of copies and the venture dungeon pick), so the pinned
# driver keeps all of them. The merge adds no file of its own and leaves cardsfolder/tokenscripts unchanged (same as
# FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-zero-candidate-confirm.
# c12c06bcef = 051f555862 plus the energy confirm inside a scripted unless-pay (xproof harness cluster "AI is not
# supposed to reach this code": Die Young, Harnessed Lightning, Lathnu Hellion). gorge logs one unless_pay answer, which is
# the payer's whole choice (CR 118.12); payCostToPreventEffect pays it through payCostDuringAbilityResolve, whose
# non-mandatory CostPayEnergy part asks confirmPayment. ScriptedController.confirmPayment now answers yes while that
# payment runs (every per-part confirm: energy, counters, discard, exile, shards) instead of missing and falling to
# AiController.confirmPayment, which throws; a confirmPayment outside a scripted unless-pay still misses.
# UnlessEnergyConfirmTest pins it (driver suite 158 tests green). Driver-only change; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-unless-energy-confirm.
# 3494ec7d2b = the clean merge of c12c06bcef (the energy confirm above) and 1eef2a952a (the zero-candidate decline), so the
# pinned driver keeps both. The merge adds no file of its own; driver suite 162 tests green (UnlessEnergyConfirmTest,
# ZeroCandidateConfirmTest, ForcedDieRollTest). cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432).
# Fork branch fix/xp-forge-driver-unless-energy-confirm.
# 2bf0a1cb36 = 3494ec7d2b plus the one-card order pin: gorge logs a one-pick "order" decision for a look-at-the-top-card
# put-back (DB$ RearrangeTopOfLibrary | NumCards$ 1: Dewdrop Spy, Merfolk Observer, Saheeli's Silverwing) while Forge calls
# orderMoveToZoneList with one card, which ScriptedController returned at once, leaving the pin as a leftover (xproof
# decision_shape strict_miss "unconsumed gorge decision(s): ... order[bottom] [Plains]"). A lone card now goes through
# StepMachine.orderCards, which takes the order pin only when its pick names that card and otherwise leaves the queue
# alone. LoneCardOrderTest pins it (driver suite 166 tests). Driver-only change; cardsfolder/tokenscripts unchanged
# (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-lone-card-order.
# 37726abafb = 3494ec7d2b plus the state-based-action graveyard batch order: one SBA pass that moves cards from more than one of
# noRegCreats / desCreats / sacrificeList now puts their union to the owners once (CR 704.3, 404.3) and dispatches each card down
# its own path in that order; a single list keeps its existing path. `SbaGraveyardOrderTest` (forge-oracle) fails before with no
# ask at all and passes after (Volcanic Salvo's 0/3 + 0-loyalty planeswalker). Production driver source unchanged (the commit adds
# SbaGraveyardOrderTest); cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/forge-sba-graveyard-order.
# c674c0977f = 37726abafb plus the later-pass fix: the union branch no longer sets the per-list ordered flags (a later pass in the same
# checkStateEffects call with one multi-card list, e.g. two creatures at 0 toughness after a lord died in pass 0, still asks as before);
# it remembers the ordered cards instead. The two-pass test laterPassWithOneMultiCardListStillAsks fails on 37726abafb and passes here.
# c3b04cf264 = the clean merge of 2bf0a1cb36 (the one-card order pin) and c674c0977f (the SBA batch order), both branched from
# 3494ec7d2b; the two touch disjoint files (forge-oracle's ScriptedController and StepMachine vs forge-game's GameAction), so the
# pinned driver keeps both fixes. cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/forge-sba-graveyard-order.
# 147dd2f00f = the clean merge of c3b04cf264 (the SBA graveyard batch order above) and ba5d908b14 (the modal DFC land back
# face play: xproof harness "has no land play", 22 clusters, every modal DFC with a land back - Glasswing Grace,
# Turntimber Symbiosis, Pinnacle Monk, Agadeem's Awakening, ...). CR 712.12: a player playing a modal DFC as a land
# chooses one of its faces that's a land, and it enters with that face up. StepMachine.landFor scanned only
# Card.getSpellAbilities(), the front state's, so a spell front exposed no land ability; with none, it now takes the land
# ability of the Backside state of a modal card (an API scan, no card names), and LandAbility.resolve turns the card to
# that face before the move so its entry choice ("Pay 3 life") is asked of the back face and read from gorge's pick. The
# MayPlay branch (xp-forge-land-play-mayplay) is untouched. ba5d908b14 is 2bf0a1cb36 plus ModalLandPlayTest (Age-Graced
# Chapel; Turntimber, Serpentine Wood paying 3 life), which fails on 2bf0a1cb36 with "has no land play"; the merge keeps the
# SBA and modal tests (full driver suite 170 tests, 168 + SbaGraveyardOrderTest's 2). Driver-only change;
# cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-mdfc-land-face.
# Its ancestry also carries cd13258c4b7 (the folded optional-trigger confirm: a non-Mandatory Cost$ body answers yes from
# gorge's unconsumed X/object pick, else no; FoldedOptionalConfirmTest), so the pinned driver keeps that fix too.
# 9e368ca172 = 147dd2f00f plus the lone forced entity pick: ScriptedController.chooseSingleEntityForEffect's lone-option shortcut
# (made for TwoPiles/ChangeZone Chooser$ asks gorge never logs) now consumes gorge's one-option pick when its single ref IS that
# entity (StepMachine.takeLonePick). xproof cluster xpt-83c211ae: Benevolent Offering / Intellectual Offering / Akroan Horse
# (ChoosePlayer over the one opponent, `choose_n[player]` x2 left over) and Cataclysmic Gearhulk (ChooseEach$ categories with the
# Gearhulk as the lone artifact and creature, `choose_n[card]` x2 left over). LoneOpponentChoiceTest and EmptyCategoryChoiceTest
# fail before with exactly those leftovers and pass after; UnmappedAskTest's two lone-Chooser tests (Curator of Destinies) still
# pass; full driver suite 172 tests. Driver-only; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-lone-entity-consume.
# d1b608ce5d = 9e368ca172 plus three asks the driver never mapped (xproof clusters xpt-110a8ad3, xpt-4d26982b, xpt-704ee28b,
# xpt-b8ebd380): announceRequirements with a non-X title (Join Forces' ListTitle$ "amount of mana to pay": Alliance of Arms, Minds
# Aglow, Shared Trauma, Mana-Charged Dragon) takes gorge's choose_n[number] pick per player (StepMachine.announceNumber, in min..max);
# chooseBinary(LeftOrRight) (ChooseDirectionEffect: Mystic Barrier) takes gorge's choose_n[direction] pick, true = "left"; an
# Optional$ Attach's per-attachment confirmAction(sa, null, "Do you want to attach X to Y?") is answered from gorge's one attachment
# pick (resume attach_subset / attach_choice): yes for an attachment it names, no for the rest and for an empty pick, and the Choices$
# form's entity pick (the lone Equipment the lone-forced shortcut consumes) is remembered as that attachment's yes. Test
# UnmappedAsksJoinDirectionAttachTest (5 tests, five fixtures/unmapped requests); full driver suite 177 tests. Driver-only;
# cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-number-direction-attach.
# d0f16bee2e0 = the merge of a4fa8d45a56 and fef74c5308f, carrying all three fixes (the OddsOrEvens binary,
# the repeated-mode/NICKNAME/zero-option driver work and the Myriad-only AtEOT guard).
# a4fa8d45a56 = the merge of 65c6664d90 and a445a858b2, carrying both fixes.
# 65c6664d90 = d1b608ce5d plus the ChooseEvenOddEffect binary: chooseBinary(OddsOrEvens), true = Odd, now takes gorge's
# choose_n whose pick kind is odd/even/evenodd -- Extinction Event's resolution ask `choose_n[odd] [odd]` and Gollum, Riddle
# Master's as-enters ask `choose_n[evenodd] [Odd]` (resume "etb", so the role is the pick kind, not a resume). binaryPick
# and binaryAnswer also hold LeftOrRight (same behaviour and note), so the tap/untap default is TapOrUntap's alone and an
# unmapped binary kind still misses loudly. OddsOrEvensBinaryTest replays both exported requests; the full driver suite is
# 179 tests. Driver-only; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-odd-even-binary.
# 0c4ca7afb3d = d1b608ce5d plus three matching gaps from xproof clusters xpt-7553d8f4, xpt-7852725e, xpt-b0865a52 and
# xpt-a3ee64aa. CR 700.2d: ScriptedController.chooseModeForAbility passes allowRepeat to StepMachine.chooseModes, which
# binds a mode already in the answer again (Verdant Confluence [Return, Search, Return], Wretched Confluence [draw x3]).
# CR 603.3b: StepMachine.norm folds the raw NICKNAME token and the host's short name (before the first comma) to one
# placeholder, so Uro's sacrifice and draw ETB triggers bind in gorge's order. CR 608.2c/700.2: pickObjects with zero
# options consumes only an explicit empty answer that is next, leaving Dawnbreak Reclaimer's p1 ChoosePlayer pick queued;
# the following Optional ChangeZoneAll with nothing remembered answers no like the zero-candidate ChangeZone gate.
# Test RepeatModesNicknameZeroPickTest (4 tests, four fixtures/repeat-modes-nickname-zeropick requests), run by
# scripts/forge-oracle-test-repeat-modes-nickname-zeropick.sh; full driver suite (181 tests) green on it. Driver-only;
# cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-repeat-modes-nickname-zeropick.
# a445a858b2 = d1b608ce5d (the 177-test driver) plus the Myriad-only AtEOT empty-set guard: the fork's Myriad fix (f30d448731) made
# SpellAbilityEffect.registerDelayedTrigger return on an empty card set for every AtEOT caller, which dropped the delayed
# trigger CR 603.7a creates for an unconditional "exile it at the beginning of the next end step" (Mordor on the March with
# no creature card to exile; the Devastating Onslaught X=0 / Foggy Swamp Visions class). The guard now needs AtEOTNeedsCard$ True,
# which only CardFactoryUtil's Myriad trigger carries (CR 702.116a). xproof cluster xpt-0eb9c20e. MordorOnTheMarchNothingExiledTest
# fails on d1b608ce5d and passes after; MyriadNoTokensCreatesNoDelayedTriggerTest still passes; the driver suite (177 tests) passes on
# the patched forge-game. cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-mordor-empty-delayed-trigger-on-d1b6
# (the 8653216df2 commit cherry-picked onto d1b608ce5d, so the Join Forces / LeftOrRight / Attach mappings stay).
# d0f16bee2e0 = the clean merge of a4fa8d45a56 and fef74c5308f: they touch disjoint files (the OddsOrEvens and
# repeat-mode driver work vs the forge-game guard), so the pin keeps all three fixes; die-roll code is unchanged.
# fef74c5308f = the clean merge of 0c4ca7afb3d and a445a858b2: the repeated-mode/NICKNAME/zero-option driver work and the
# forge-game Myriad-only AtEOT guard touch disjoint files, so the pin keeps both. Driver and die-roll code are unchanged from
# 0c4ca7afb3d; MordorOnTheMarchNothingExiledTest and MyriadNoTokensCreatesNoDelayedTriggerTest keep the a445a858b2 evidence.
# Fork branch fix/xp-forge-driver-repeat-modes-nickname-zeropick-merge.
# 70b77c0ff08 = the clean merge of 9b17c1fc83 (the coin force no longer spent by an unrelated unbounded draw) and 24a3344b81
# (the Dig order-to-Library answer for Spin into Myth's fateseal): they touch disjoint files (ForcedDieRandom vs
# ScriptedController/StepMachine), so the pin keeps both; die-roll code unchanged.
# 9b17c1fc83 = d0f16bee2e0 plus the coin force no longer spent by an unrelated unbounded draw: ForcedDieRandom.nextInt() polled the armed
# flips for a next(bits)-routing MyRandom, which the pin's MyRandom (nextBoolean forwarded) made dead for the flip and live for every
# other unbounded draw. Aggregates.random over a Set draws nextInt() per item and Goblin Kaboomist's Land Mine token art pick does, on
# the resolution that holds its flip, so the recorded lost flip was spent first and Forge flipped from its seeded stream (xproof rng
# xs:XP:goblin-kaboomist:f0t0:s2, step 37, xpt-825fefd2). The override is gone; the fork's ForcedCoinFlipTest pins the Set pick leaving
# the flip armed (anUnrelatedUnboundedDrawDoesNotSpendTheArmedFlip is the structural guard; its fixtures/coin win/lose rows carry
# different ids, so they are not by themselves a guarantee that an unforced build fails one). scripts/forge-oracle-test-coinflip.sh
# (make forge-driver-tests) replays the find twice under ONE id, flip lost then won, so an unforced build gives the same game both
# times and fails exactly one; scripts/forge-oracle-run.sh refuses a ForcedDieRandom that declares the unbounded nextInt().
# Driver-only: forge-game, cardsfolder and tokenscripts unchanged from d0f16bee2e0. Fork branch fix/xp-forge-driver-kaboomist-coinflip-step.
# 24a3344b81 = d0f16bee2e0 plus Spin into Myth's fateseal (xproof cluster xpt-be0cfba7): Forge's DigEffect asks
# orderMoveToZoneList for the cards a Dig sent to the library bottom (to their OWNER's controller), but gorge logs only its
# one choose_n[dig] pick and no order decision. StepMachine.pickObjects remembers the consumed dig pick and orderCards answers
# a Dig LibraryPosition$ -1 order ask from it (first pick moved first, so it ends highest; CR 401.4), matched by name multiset
# at the same step, seat not compared. Test FatesealDigOrderTest (fixtures/unmapped/fateseal-dig-order.jsonl), run by
# scripts/forge-oracle-test-fateseal-dig-order.sh. Driver-only (ScriptedController/StepMachine), die-roll code unchanged;
# cardsfolder/tokenscripts unchanged. Fork branch fix/xp-forge-driver-fateseal-dig-order.
# 9d7baedf34 = d0f16bee2e plus the echo and cumulative upkeep pay-or-sacrifice answer: Forge builds both keywords as an
# upkeep trigger whose DB$ Sacrifice asks payCostToPreventEffect for Echo$/CumulativeUpkeep$, but the driver only took an
# unless_pay decision there, so gorge's one-pick election became a strict miss plus a leftover. payCostToPreventEffect
# now also takes that decision by pick kind: echo_pay/echo_pay_manual/cumulative_pay/cumulative_pay_manual pay through
# payCostDuringAbilityResolve with payingUnlessCost set, and echo_sac/cumulative_sac decline (Forge sacrifices the source).
# The payment's own cost choose consumes the follow-up echo_action_pick / cumulative_action_sac / discard pick through
# ScriptedCostDecision and the sacrifice/discard overrides. EchoCumulativeUpkeepTest (5 tests: echo pay, echo sacrifice,
# cumulative pay, cumulative pay with a creature sacrifice, a non-mana echo cost) and the full driver suite (188 tests)
# are green on it. Driver-only change; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432).
# Fork branch fix/xp-forge-driver-echo-cumulative-upkeep.
# a3c422f79a6 = the clean merge of 9d7baedf34 (the echo/cumulative-upkeep answer above) and 70b77c0ff08 (the coin-force and
# Dig order-to-Library driver above): the echo election touches ScriptedController.payCostToPreventEffect while the coin force
# touches ForcedDieRandom and the Dig path ScriptedController.orderMoveToZoneList/StepMachine, disjoint enough to merge cleanly,
# so the pinned driver keeps all three fixes. The merge adds no code of its own; the full driver suite (192 tests, including
# EchoCumulativeUpkeepTest, ForcedCoinFlipTest, FatesealDigOrderTest and ForcedDieRollTest) is green on it; cardsfolder and
# tokenscripts unchanged. Fork branch fix/xp-forge-driver-echo-cumulative-upkeep.
# 528c55d25b4 = the clean merge of 8f537eddcbb (cast_mode "alternative", StepMachine.spellForMode only) and 304ae5ffe0e
# (the elected unless payment's whole-cost card pick, ScriptedController.chooseCardsForCost only): the two fixes touch disjoint
# driver files, so the pinned driver keeps both; the merge adds no code of its own. Driver-only; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-alternative-cast-mode.
# 8f537eddcbb = a3c422f79a6 plus cast_mode "alternative": StaticAbilityAlternativeCost builds a static's alternative-cost
# cast as a non-basic copy of the spell with AlternativeCost unset (that field is only for the keyword alternatives such as
# Warp and Escape), while the plain cast stays basic; spellForMode now takes the first such spell. Submerge's free cast
# (CR 118.9) and a granted or may-play alternative (Leyline of Mutation, Warped Space) no longer fall through to the plain
# cast and fail "was not put on the stack". scripts/forge-oracle-test-alternative-cast-mode.sh (make forge-driver-tests)
# replays the exported xs:XP:submerge:cast:s1 request and requires Submerge on the stack at step 0 with no harness row;
# make forge-driver-tests is green on it. Driver-only; cardsfolder/tokenscripts unchanged.
# 304ae5ffe0e = a3c422f79a6 plus the whole-cost card pick of an elected unless payment (xproof cluster xpt-4561863a, Ill-Timed Explosion's
# UnlessCost$ Discard<2/Card> and Ghostly Dancers + Rent Is Due's tap-two): ScriptedController.chooseCardsForCost asked pickObjects for
# 0..amount whenever Forge passed isOptional, which every UnlessCost$ payment does, so an option list of exactly the amount (the 2-card
# hand, the 2 untapped creatures) strict-missed "cost card" although gorge logs no pick for a forced one. gorge's unless_pay answer has
# already elected the payment (payingUnlessCost), and an elected payment pays the whole cost (CR 118.12, 601.2h), so the ask is now
# amount..amount there: the exact-size list is forced and a logged pick is still consumed first; outside an elected unless payment the
# 0..amount ask is unchanged. UnlessCostForcedPickTest (3 tests: both finds replayed, plus a 3-card hand that consumes gorge's logged
# discard pick) fails without the change; the full driver suite (195 tests) is green on it. Driver-only: cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-unless-cost-cards-forced.
# 244c5908482 = 528c55d25b4 + cast_mode "mayplay": a cast from exile under a MayPlay static (Hurl Through Hell's "you may
# cast that card") is the MayPlay-bound copy c.getAllPossibleAbilities offers with getMayPlay() set, not the card's own
# hand-restricted spell, which SpellAbilityRestriction.checkZoneRestrictions refuses ("was not put on the stack", CR
# 601.2a). spellForMode takes that copy for the mode, and spellFor does the same for any cast whose card is outside the
# hand and not in the command zone; a basic copy is preferred as commandZoneSpell does, and Forge granting none is a loud
# HarnessError. The command-zone pick now uses the same helper, so one selector covers every MayPlay cast.
# scripts/forge-oracle-test-mayplay-cast.sh (make forge-driver-tests) replays the exported
# xs:XP:hurl-through-hell:cast:s1 request and requires p1's Shield Sphere on the stack (controller p0) at step 7 with no
# harness row. Driver-only; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-mayplay-cast.
FORGE_ORACLE_REF ?= 244c590848288b35a4b6a0a2dd0da122a1f5dca9
FORGE_ORACLE_DIR ?= .oracle-build/forge

# Out-of-tree follower builds (Manabrew, ...) live under one cache directory.
FOLLOWERS_CACHE ?= $(or $(XDG_CACHE_HOME),$(HOME)/.cache)/gorge-compliance
# Manabrew (AGPL-3.0, a Rust port of Forge's engine): upstream until the operator creates the fork
# https://github.com/adams-shaun/manabrew; driver commits sit on branch `oracle` of the local clone.
MANABREW_REPO ?= https://github.com/witchesofthehill/manabrew
# 8684462a6522 = upstream dadee731 + oracle patches (place_for_setup, take_state_edit, diagnostics sink, profile.oracle) + the manabrew-oracle driver crate: CLI, preflight, snapshot writer, board setup, the step machine (play, activate, cast, pass, resolve, mana; pool payment; ScriptedRng) and the answerer (targets, yes/no, modes, object picks, library, names and numbers, optional costs from cast_mode; adventure_alt and warped play modes) and combat (pass_to, attack, block, damage assignment from a logged division, exert/enlist/combat costs), the raw move/attach preludes with an engine settle (priority_round settles SBA and triggers after an agent edit), and whole-game tapes with no turn cap, the snapshot writer printing a changeling's creature types as gorge's allcreaturetypes, ScriptedRng completing a shuffle as an identity shuffle with a note (library order is not compared), and the driver stopping the batch and exiting 3 after a panic row so the runner restarts (local branch oracle; pushed to adams-shaun/manabrew by the operator)
MANABREW_ORACLE_REF ?= 8684462a65228208029c26547284038148d08ce6
MANABREW_ORACLE_DIR ?= $(FOLLOWERS_CACHE)/manabrew
CARGO ?= $(HOME)/.cargo/bin/cargo
# phase.rs follower: pinned build + card data (followers/phase/PHASE_PIN), out of tree.
PHASE_ORACLE_DIR ?= $(FOLLOWERS_CACHE)/phase

.PHONY: help
help:
	@echo "gorge-compliance targets:"
	@echo "  make cards [GORGE_ROOT=../gorge] — link a gorge checkout's .cards corpus here (run gorge's make fetch-cards compile-cards first)"
	@echo "  make test           — go test ./... (capped like gorge's)"
	@echo "  make test-one PKG=./p RUN=TestX — one capped, corpus-checked test run with one verdict line"
	@echo "  make test-select BASE=ref — print the Go test packages the touched-package gate selects for BASE..HEAD"
	@echo "  make pr [PR_BASE=main] [DRY=1] — open/refresh a GitHub PR with a generated change overview"
	@echo "  make boundary       — the licence guards (no Forge content is committed)"
	@echo "  make carddb-test    — the carddb tool's Python tests (cmd/carddb/py/tests; git + python3 only, no network)"
	@echo "  make oracle-audit   — the hand Oracle-text audit + XMage ports (audit/; ORACLE_RUN=<Card> to filter)"
	@echo "  make compliance-status — per-set/per-format compliance status (oracle/compliance/status.md, never committed) + ratchet check"
	@echo "  make compliance-sets — both levels over every printed set -> .coverage/compliance-sets.md"
	@echo "  make compliance-manifests — regenerate oracle/compliance/manifests from XMage set classes at XMAGE_REF"
	@echo "  make xmage-oracle-setup — build XMage at XMAGE_REF out of tree (heavy; run alone)"
	@echo "  make xmage-sbv2-setup — build the XMage seat engine (SpellBench upstream) out of tree (heavy; run alone)"
	@echo "  make forge-oracle-setup — build the Forge oracle driver at FORGE_ORACLE_REF out of tree (heavy)"
	@echo "  make manabrew-oracle-setup — build Manabrew at MANABREW_ORACLE_REF and its card archive from gorge's FORGE_REF scripts, out of tree (heavy)"
	@echo "  make manabrew-engine-test [RUN=filter] — run the Manabrew engine's lib tests in the MANABREW_ORACLE_REF build tree, capped (heavy)"
	@echo "  make manabrew-oracle-test RUN=filter — cargo test for the manabrew-oracle driver crate in the MANABREW_ORACLE_REF build tree, capped (heavy)"
	@echo "  make manabrew-preflight POOL=FILE — which pool cards Manabrew would silently improvise (builds the driver; prints NAME<TAB>REASON, then 'refused K of P')"
	@echo "  make compliance-pass SETS='FRA BLB' — XMage compliance pass over the sets; reruns only stale verdicts"
	@echo "  make follower-check ENGINE=phase [ARGS='--setup-only --pool F'] — level-A scenarios through one follower (scripts/follower-check.sh)"
	@echo "  make follower-sweep-smoke ENGINE=phase [GAMES=2] — one xengine chunk with one follower (or several: ENGINE='phase manabrew'), pool narrowed by their preflights (scripts/follower-sweep-smoke.sh)"
	@echo "  make phase-oracle-setup — build the phase.rs follower and fetch its card data at followers/phase/PHASE_PIN, out of tree (heavy; one cold build per pin)"
	@echo "  make phase-follower-test [RUN=filter] — cargo test for the phase.rs follower (needs phase-oracle-setup; --profile follower, PHASE_CARD_DATA set)"
	@echo "  make phase-seat-test [RUN=filter] — cargo test for the phase.rs v2 seat (phase-seat; 8G scope, --profile follower; needs phase-oracle-setup). Not runnable under a 4G cap: the engine's codegen alone exceeds it"
	@echo "  make phase-preflight POOL=FILE — which pool cards phase.rs lacks (rebuilds the follower; prints NAME<TAB>REASON, then 'refused K of P')"
	@echo "  make smoke          — the script smokes (stubs only; no Java, no network)"
	@echo "  make mtgish-fetch   — download the pinned mtgish data into .mtgish/<ref>/ (gitignored) and verify its sha256"
	@echo "  make mtgish-xc      — the mtgish card-definition cross-check: corpus report + the corpus ledger ratchet"
	@echo "  make follower-proto-test [RUN=name] — cargo test for the engine-neutral follower crate (followers/proto; output under FOLLOWERS_CACHE)"
	@echo "  make xmage-driver-tests — the Java driver's unit tests (needs xmage-oracle-setup)"
	@echo "  make forge-driver-tests — the Forge replay/unit driver tests (needs forge-oracle-setup)"

.PHONY: cards test boundary oracle-audit smoke follower-proto-test phase-oracle-setup phase-follower-test phase-seat-test phase-preflight
cards:
	@GORGE_ROOT='$(GORGE_ROOT)' FORGE_REF='$(FORGE_REF)' scripts/link-cards.sh

test:
	go test $(GO_TEST_FLAGS) ./...

# test-one: PKG=./path RUN='^TestX$' [SKIP=re] [V=1] [TIMEOUT=2m] [TAGS=a,b]
# (scripts/test-targets.sh one; the last stdout line is the verdict).
.PHONY: test-one
test-one: export PKG := $(PKG)
# RUN/SKIP go to the recipe through $(value ...), verbatim: a command-line RUN re-expands on export and make eats
# `$|` / `$)` in it (gorge 8117b8ec09; '^TestA$|^TestB$' became '^TestA^TestB$' and matched nothing).
test-one: export V := $(V)
test-one: export TIMEOUT := $(TIMEOUT)
test-one: export TAGS := $(TAGS)
test-one:
	@RUN='$(value RUN)' SKIP='$(value SKIP)' bash scripts/test-targets.sh one

# test-select: BASE=ref [HEAD=ref] — the touched-package gate's package list
# (scripts/test-select.sh, internal/testselect; the last stdout line is a package).
.PHONY: test-select
test-select:
	@bash scripts/test-select.sh '$(BASE)' '$(HEAD)'

follower-proto-test:
	systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env CARGO_TARGET_DIR=$(FOLLOWERS_CACHE)/target/proto $(CARGO) test --locked --manifest-path followers/proto/Cargo.toml $(RUN)

phase-oracle-setup:
	FOLLOWERS_CACHE='$(FOLLOWERS_CACHE)' PHASE_ORACLE_DIR='$(PHASE_ORACLE_DIR)' CARGO='$(CARGO)' scripts/phase-oracle-setup.sh

# The follower profile is used for tests too, so the engine is built once, not again in debug. Cargo
# runs inside followers/phase (env -C) so its rust-toolchain.toml selects the nightly.
phase-follower-test:
	systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% env -C followers/phase CARGO_HOME=$(FOLLOWERS_CACHE)/cargo-home RUSTC=$(dir $(CARGO))rustc CARGO_TARGET_DIR=$(PHASE_ORACLE_DIR)/target PHASE_CARD_DATA=$(PHASE_ORACLE_DIR)/card-data-$(shell cut -d' ' -f2 followers/phase/PHASE_PIN).json RUST_MIN_STACK=16777216 $(CARGO) test --locked --profile follower $(RUN)

# The replay-determinism test needs a deck catalog; by default the two seeds that play to a natural end in
# seconds (3 and 5) over gorge's fdn-limited decks, so `make phase-seat-test` can fail on a determinism break.
# A missing catalog is an error (a worktree whose ../gorge is not the checkout needs GORGE_ROOT=...), never a
# silent skip; PHASE_SEAT_DECKS=none (or empty) skips the replay test on purpose.
# PHASE_SEAT_SEEDS=0,1,2,3,4,5 is the full six-seed run (~12 min).
PHASE_SEAT_DECKS ?= $(abspath $(GORGE_ROOT)/bots/spellbench/decks/fdn-limited)
PHASE_SEAT_SEEDS ?= 3,5

# phase-seat is the SpellBench v2 engine role over phase.rs (followers/phase/phase-seat; scripts/phase-sbv2.sh runs it).
phase-seat-test:
	@d='$(PHASE_SEAT_DECKS)'; if [ -n "$$d" ] && [ "$$d" != none ] && [ ! -d "$$d" ]; then echo "phase-seat-test: deck catalog '$$d' not found; set GORGE_ROOT=<gorge checkout> or PHASE_SEAT_DECKS=none to skip the replay test" >&2; exit 2; fi; \
	[ "$$d" = none ] && d=''; \
	systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% env -C followers/phase CARGO_HOME=$(FOLLOWERS_CACHE)/cargo-home RUSTC=$(dir $(CARGO))rustc RUSTDOC=$(dir $(CARGO))rustdoc CARGO_TARGET_DIR=$(PHASE_ORACLE_DIR)/target PHASE_CARD_DATA=$(PHASE_ORACLE_DIR)/card-data-$(shell cut -d' ' -f2 followers/phase/PHASE_PIN).json PHASE_SEAT_DECKS="$$d" PHASE_SEAT_SEEDS=$(PHASE_SEAT_SEEDS) RUST_MIN_STACK=16777216 $(CARGO) test --locked --profile follower -p phase-seat $(RUN)

# Builds the follower, then asks it which names of POOL it lacks. Comment and blank lines of the pool
# are dropped; the names go through a temp file under .oracle-build/ (the protocol's NAMES.txt).
phase-preflight:
	@test -n '$(POOL)' || { echo "usage: make phase-preflight POOL=oracle/xengine/pools/fdn.txt" >&2; exit 2; }
	systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% env -C followers/phase CARGO_HOME=$(FOLLOWERS_CACHE)/cargo-home RUSTC=$(dir $(CARGO))rustc CARGO_TARGET_DIR=$(PHASE_ORACLE_DIR)/target RUST_MIN_STACK=16777216 $(CARGO) build --locked --profile follower
	@mkdir -p .oracle-build && names=.oracle-build/phase-preflight-names.txt && \
	/usr/bin/grep -v '^[[:space:]]*\(#\|$$\)' '$(POOL)' | LC_ALL=C sort -u > $$names && \
	out=.oracle-build/phase-preflight-out.txt && \
	RUST_MIN_STACK=16777216 $(PHASE_ORACLE_DIR)/target/follower/phase-follower --preflight $$names --card-data $(PHASE_ORACLE_DIR)/card-data-$(shell cut -d' ' -f2 followers/phase/PHASE_PIN).json > $$out && \
	cat $$out && echo "refused $$(wc -l < $$out) of $$(wc -l < $$names)"

boundary:
	GORGE_TEST_WRAPPER=1 go test -count=1 ./internal/boundary/

# carddb-test runs the Python carddb tool's tests (put, publish, merge driver, build, fetch);
# CARDDB_UPDATE_GOLDEN=1 rewrites cmd/carddb/py/tests/golden after a deliberate schema change.
.PHONY: carddb-test
carddb-test:
	python3 -I -m unittest discover -s cmd/carddb/py/tests -t cmd/carddb/py

# oracle-audit runs both scenario corpora through gorge's runner: the hand
# Oracle-text audit (scenarios/oracle plus gorge's residual
# engine/rules/testdata/oracle) and the XMage ports (scenarios/xmageport).
# ORACLE_RUN filters the audit per card, e.g. ORACLE_RUN='Fatal_Push'.
ORACLE_RUN ?=
oracle-audit:
	go test $(GO_TEST_FLAGS) -count=1 ./audit -run 'TestOracleScenarioFilesWellFormed|TestOracleAudit/$(ORACLE_RUN)|TestXMagePortScenarios' -v

smoke:
	bash ops/scripts/tests/make_cards_smoke.sh
	bash ops/scripts/tests/forge_pass_smoke.sh
	bash ops/scripts/tests/forge_oracle_pin_smoke.sh
	bash ops/scripts/tests/forge_sbv2_smoke.sh
	bash ops/scripts/tests/xmage_sbv2_smoke.sh
	bash ops/scripts/tests/phase_oracle_pin_smoke.sh
	bash ops/scripts/tests/phase_sbv2_smoke.sh
	bash ops/scripts/tests/manabrew_oracle_pin_smoke.sh
	bash ops/scripts/tests/manabrew_oracle_run_smoke.sh
	bash ops/scripts/tests/xengine_sweep_smoke.sh
	bash ops/scripts/tests/xengine_random_smoke.sh
	bash ops/scripts/tests/follower_check_smoke.sh
	bash ops/scripts/tests/follower_sweep_smoke_smoke.sh
	bash ops/scripts/tests/test_targets_smoke.sh
	bash ops/scripts/tests/test_select_smoke.sh

# xengine-sweep runs the chunked, resumable cross-engine sweep
# (scripts/xengine-sweep.sh): SETS='FDN BLB' CHUNKS=4 GAMES=50. Heavy: it starts
# XMage and Forge JVMs, and needs both oracle builds.
CHUNKS ?= 4
GAMES ?= 50
.PHONY: xengine-sweep
xengine-sweep:
	scripts/xengine-sweep.sh --sets '$(or $(SETS),FDN BLB)' --chunks $(CHUNKS) --games $(GAMES)

# mtgish-fetch downloads mtgish.lines.json and LICENSE at MTGISH_REF into
# .mtgish/<ref>/ and verifies them against oracle/mtgish/MTGISH.lock.
# mtgish-xc writes the whole-corpus cross-check report (with FDN and BLB tables
# of their own) to .coverage/mtgish/ and runs the ledger ratchet: the
# corpus ledger oracle/mtgish/known/T1..T4.tsv, the exclusions in
# known/excluded.tsv, and the shrink-only maxNormalizerRows/maxPendingRows.
.PHONY: mtgish-fetch mtgish-xc
mtgish-fetch:
	MTGISH_REF=$(MTGISH_REF) scripts/mtgish-fetch.sh
mtgish-xc:
	go run ./cmd/mtgishxc -all
	go test -count=1 ./oracle/mtgish -run TestMtgishLedger -v

# xmage-driver-tests compiles the Java replay driver and runs its unit tests
# (oracle/xmageoracle/test) against the xmage-oracle-setup build. Needs Java
# from that build; no game, no H2. The first recipe line is the structural
# guard for the per-class wrapper convention: every runnable driver test must
# be named by a scripts/xmage-oracle-test-*.sh, or the glob below silently
# skips it.
XMAGE_DRIVER_OUT ?= $(CURDIR)/.coverage/xmage-driver
.PHONY: xmage-driver-tests
xmage-driver-tests:
	@missing=; for f in oracle/xmageoracle/test/org/mage/test/oracle/*Test.java; do \
	  /usr/bin/grep -q 'public static void main' "$$f" || continue; \
	  b=$$(basename "$$f"); \
	  /usr/bin/grep -q "$$b" scripts/xmage-oracle-test-*.sh || missing="$$missing $$b"; \
	done; \
	if [ -n "$$missing" ]; then \
	  echo "driver tests with a main not wired into scripts/xmage-oracle-test-*.sh:$$missing" >&2; \
	  exit 1; \
	fi
	@test -s "$(XMAGE_ORACLE_DIR)/tests.cp" || { echo "no XMage build at $(XMAGE_ORACLE_DIR): run make xmage-oracle-setup, or set XMAGE_ORACLE_DIR" >&2; exit 1; }
	for t in scripts/xmage-oracle-test-*.sh scripts/forge-oracle-test-*.sh; do echo "== $$t"; XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) bash "$$t" "$(XMAGE_DRIVER_OUT)/$$(basename $$t .sh)" || exit 1; done

# forge-driver-tests is the Forge-only half of xmage-driver-tests: the exporter
# regression (testdata/forge/mdfc-spell-ability.jsonl through forge-export)
# plus the replay/unit wrappers scripts/forge-oracle-test-*.sh at
# FORGE_ORACLE_REF. Needs the out-of-tree build from make forge-oracle-setup.
.PHONY: forge-driver-tests
forge-driver-tests:
	@test -s "$(FORGE_ORACLE_DIR)/cp.txt" || { echo "no Forge build at $(FORGE_ORACLE_DIR): run make forge-oracle-setup, or set FORGE_ORACLE_DIR" >&2; exit 1; }
	PKG=./cmd/oraclediff RUN='^TestForgeAbilityLinesUseSnapshotFaceForShatterskull' bash scripts/test-targets.sh one
	for t in scripts/forge-oracle-test-*.sh; do echo "== $$t"; FORGE_ORACLE_DIR=$(FORGE_ORACLE_DIR) XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) bash "$$t" "$(XMAGE_DRIVER_OUT)/$$(basename $$t .sh)" || exit 1; done

.PHONY: compliance-manifests
compliance-manifests:
	go run ./cmd/compliance manifest -xmage $(XMAGE_ORACLE_DIR)/sets-only -ref $(XMAGE_REF) -out oracle/compliance/manifests

.PHONY: xmage-sbv2-setup
# Heavy (about 10 minutes, ~1 GB of downloads and builds, 260 MB card database): run alone, like xmage-oracle-setup.
xmage-sbv2-setup:
	XMAGE_SBV2_REF=$(XMAGE_SBV2_REF) XMAGE_SBV2_XMAGE_REF=$(XMAGE_SBV2_XMAGE_REF) XMAGE_SBV2_DIR=$(XMAGE_SBV2_DIR) scripts/xmage-sbv2.sh --setup

.PHONY: xmage-oracle-setup forge-oracle-setup manabrew-oracle-setup manabrew-engine-test manabrew-oracle-test manabrew-preflight
xmage-oracle-setup:
	XMAGE_REF=$(XMAGE_REF) XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) scripts/xmage-oracle-setup.sh
forge-oracle-setup:
	FORGE_REPO='$(FORGE_REPO)' FORGE_REF='$(FORGE_REF)' FORGE_ORACLE_REF='$(FORGE_ORACLE_REF)' FORGE_ORACLE_DIR='$(FORGE_ORACLE_DIR)' scripts/forge-oracle-setup.sh
manabrew-oracle-setup:
	FORGE_REPO='$(FORGE_REPO)' FORGE_REF='$(FORGE_REF)' MANABREW_REPO='$(MANABREW_REPO)' MANABREW_ORACLE_REF='$(MANABREW_ORACLE_REF)' MANABREW_ORACLE_DIR='$(MANABREW_ORACLE_DIR)' FOLLOWERS_CACHE='$(FOLLOWERS_CACHE)' scripts/manabrew-oracle-setup.sh

MANABREW_ORACLE_REF12 = $(shell printf '%.12s' '$(MANABREW_ORACLE_REF)')
manabrew-engine-test:
	cd '$(MANABREW_ORACLE_DIR)/build-$(MANABREW_ORACLE_REF12)/manabrew-rs' && CARGO_HOME='$(FOLLOWERS_CACHE)/cargo-home' CARGO_TARGET_DIR='$(MANABREW_ORACLE_DIR)/target' PATH='$(dir $(CARGO))':"$$PATH" systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% $(CARGO) test --locked --profile oracle -p manabrew-engine --lib $(RUN)

# The driver crate (manabrew-rs/crates/manabrew-oracle on branch oracle) in the same build tree.
MANABREW_BUILD_CARGO = cd '$(MANABREW_ORACLE_DIR)/build-$(MANABREW_ORACLE_REF12)/manabrew-rs' && CARGO_HOME='$(FOLLOWERS_CACHE)/cargo-home' CARGO_TARGET_DIR='$(MANABREW_ORACLE_DIR)/target' PATH='$(dir $(CARGO))':"$$PATH" systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% $(CARGO)
manabrew-oracle-test:
	$(MANABREW_BUILD_CARGO) test --locked --profile oracle -p manabrew-oracle $(RUN)

# Builds the driver, then asks it which names of POOL Manabrew would silently improvise (NAME<TAB>REASON,
# then 'refused K of P'). Comment and blank lines of the pool are dropped.
manabrew-preflight:
	@test -n '$(POOL)' || { echo "usage: make manabrew-preflight POOL=oracle/xengine/pools/fdn.txt" >&2; exit 2; }
	$(MANABREW_BUILD_CARGO) build --locked --profile oracle -p manabrew-oracle
	@mkdir -p .oracle-build && names=.oracle-build/manabrew-preflight-names.txt && out=.oracle-build/manabrew-preflight-out.txt && \
	/usr/bin/grep -v '^[[:space:]]*\(#\|$$\)' '$(POOL)' | LC_ALL=C sort -u > $$names && \
	'$(MANABREW_ORACLE_DIR)/target/oracle/manabrew-oracle' --preflight $$names --archive '$(MANABREW_ORACLE_DIR)/cardset-$(shell printf '%.12s' '$(FORGE_REF)').rkyv' > $$out && \
	cat $$out && echo "refused $$(wc -l < $$out) of $$(wc -l < $$names)"

# forge-verdicts runs a level-A Forge pass with gen -printed and writes the
# scenarios Forge agrees with as Forge-only verdict rows.
.PHONY: forge-verdicts
forge-verdicts:
	FORGE_ORACLE_REF='$(FORGE_ORACLE_REF)' FORGE_ORACLE_DIR='$(FORGE_ORACLE_DIR)' XMAGE_REF='$(XMAGE_REF)' XMAGE_ORACLE_DIR='$(XMAGE_ORACLE_DIR)' scripts/forge-pass.sh --verdicts $(SETS)

# compliance-pass runs the XMage compliance pass (gen, plan, XMage replay of
# the stale scenarios only, diff -write into oracle/compliance/verdicts, summary)
# over SETS (default: every set with a printed list). Needs the xmage-oracle-setup
# build. HEAVY_LOCK, when set, names a heavy-job lock (scripts/heavy-lock.sh).
SETS ?=
.PHONY: compliance-pass
compliance-pass:
	XMAGE_REF=$(XMAGE_REF) XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) scripts/compliance-pass.sh $(SETS)

# compliance-status writes the generated per-set / per-format status page
# (oracle/compliance/status.md, gitignored: never committed) and checks the
# certification ratchet (oracle/compliance/ratchet.json).
.PHONY: compliance-status
compliance-status:
	go run ./cmd/oraclediff status -all -out oracle/compliance/status.md

# compliance-sets runs the compliance gate at BOTH levels over every
# committed printed set and writes the per-set table to
# .coverage/compliance-sets.md (gitignored, generated, never committed).
.PHONY: compliance-sets
compliance-sets:
	@mkdir -p .coverage
	go run ./cmd/oraclediff sets -out .coverage/compliance-sets.md

# ---- pull requests ------------------------------------------------------------
# make pr                 open (or refresh) a GitHub PR: current branch -> origin's default branch
#   PR_HEAD=<branch>      branch to propose (default: the checked-out branch)
#   PR_BASE=<branch>      target branch (default: origin's default branch)
#   TITLE="..."           PR title (default: the commit subject, or "<head> → <base>: N commits")
#   SUMMARY="..."         a paragraph placed under the overview table
#   DRAFT=1               open as a draft      DRY=1   print the title and body only
.PHONY: pr
pr: export HEAD = $(PR_HEAD)
pr: export BASE = $(PR_BASE)
pr: export TITLE := $(TITLE)
pr: export SUMMARY := $(SUMMARY)
pr: export DRAFT := $(DRAFT)
pr: export DRY := $(DRY)
pr:
	@bash scripts/open-pr.sh

# follower-check runs level-A scenarios through one registered follower and
# diffs them against gorge (scripts/follower-check.sh): ENGINE=phase
# ARGS='--set FDN --setup-only --pool FILE --limit N --min-agree PCT'.
.PHONY: follower-check
follower-check:
	scripts/follower-check.sh $(ENGINE) $(ARGS)

# follower-sweep-smoke runs one smoke-sized xengine chunk with one or several followers (ENGINE='phase manabrew'), the
# pool narrowed by the follower's preflight (scripts/follower-sweep-smoke.sh):
# ENGINE=phase [GAMES=2] [ARGS='--pool F --max-class harness=50'].
.PHONY: follower-sweep-smoke
follower-sweep-smoke:
	scripts/follower-sweep-smoke.sh "$(ENGINE)" --games $(or $(GAMES),2) $(ARGS)
