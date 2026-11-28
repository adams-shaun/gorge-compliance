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
# 63c40e5be5 = 30c62e641b merged with fork commit 0777660 (adams-shaun/mage#1, branch fix/ghost-of-illyria-delayed-trigger):
# ExileReturnBattlefieldNextEndStepTargetEffect creates the "return it at the beginning of the next end step" delayed
# ability even when nothing was exiled (CR 603.7a), so Skyskipper Duo, Ghost of Illyria, Ennis, Debate Moderator,
# S.H.I.E.L.D. Flying Car and Eerie Interlude with zero targets stack it at the end step as gorge and Forge do; it
# returned before adding the delayed ability. SkyskipperDuoTest (zero targets fails before, passes after; one target).
# 43e83011cb = 63c40e5be5 plus the CR 205.1a type-setting fix: Fowl Play and Reprobation set the card type
# (LoseType.ALL_BUT_COLOR, so an artifact creature stops being an artifact and loses its other subtypes), and Swift
# Reconfiguration removes the creature subtypes with the creature type, keeping artifact subtypes. FowlPlayTest,
# ReprobationTest, SwiftReconfigurationTest. Card behaviour change only for those cards and the
# ExileReturnBattlefieldNextEndStepTargetEffect users, so the manifests are unchanged. Pin line oracle/xpin-1125.
# 1b6d284d10 = 43e83011cb + fork commits fbfdc22a15 and 990132654b (both on oracle/xpin-1125). fbfdc22a15: token-copy
# effects create their delayed sacrifice/exile trigger even when no token is made (CR 603.7a/603.7c): Foggy Swamp Visions
# and Devastating Onslaught with X = 0, Shredder, Shadow Master, Nacatl War-Pride with no defending creature and Phantom
# Steed with nothing exiled (the two docs/upstream-patches token-copy patches, now on the fork). FoggySwampVisionsTest,
# DevastatingOnslaughtTest, ShredderShadowMasterTest, NacatlWarPrideTest, PhantomSteedTest. 990132654b: Molten Man,
# Inferno Incarnate's leaves-the-battlefield "sacrifice a land" uses SacrificeControllerEffect (SacrificeEffect had no
# target pointer, so nobody sacrificed; CR 603.10a). MoltenManInfernoIncarnateTest. Each test fails before and passes
# after. Card behaviour change only for those cards, so the manifests are unchanged.
# 27b8bfe661 = oracle/xpin-1125: 1b6d284d10 + Necromaster Dragon's mill inside DoIfCostPaid (#557, CR 608.2c); re-applied 11-25: a develop merge resolved it back to 1b6d284d10.
# 6e1ecefc46 = 27b8bfe661 + Loam Dryad's tap cost uses the singular controlled-creature filter, so its rule
# text matches the printed Oracle text instead of reading "Tap an untapped creatures you control" (CR 605.1a:
# the replay's mana-ability text match). LoamDryadTest.testTapUntappedCreatureForMana; card behaviour unchanged.
# 98052067d4 = 6e1ecefc46 + Chandra, Acolyte of Flame's 0 registers one delayed sacrifice trigger for both tokens
# instead of one per token (CR 603.7a: "Sacrifice them" is one instruction). ChandraAcolyteOfFlameTest fails before
# (Missing CHOICE def ... Triggered list (total 2)) and passes after; the card now matches gorge and Forge at the
# beginning of the end step (one ability on the stack).
# 5144fc1304f = 98052067d4 + Wild Shape's layer-4 effect adds the chosen mode's subtype (Turtle, Spider or Elephant)
# instead of the hardcoded Human, so the target becomes that creature type and loses its printed creature types
# (CR 205.1a, 613.1d). WildShapeTest checks all three modes; card behaviour changes only for Wild Shape, so the
# manifests are unchanged.
# 8ddb51df0c = 98052067d4 + Extraordinary Journey resolves with no targets when X is 0 ("exile up to X target
# creatures", CR 601.2c, 115.6): XTargetsCountAdjuster leaves the ability's target list empty and the effect read
# getTargets().get(0), throwing IndexOutOfBoundsException. ExtraordinaryJourneyTest.testXZeroResolvesWithNoTargets
# fails before (Index 0 out of bounds for length 0) and passes after; card behaviour changes only at X = 0.
# 11940683b90 = 8ddb51df0c (above) joined with 5144fc1304f (Wild Shape; above): 5144fc1304f and 8ddb51df0c are
# siblings on 98052067d4 and touch only their own card and test, so the join carries both fork heads (it adds no
# code of its own). Card behaviour changes only for Wild Shape and at Extraordinary Journey's X = 0, so the
# manifests are unchanged. Fork branch fix/join-wild-shape-extraordinary-journey.
# 5cc61aaba0b = 11940683b90 (above) + Thought Collapse and Didn't Say Please counter the spell before its controller
# mills (CR 608.2c, 701.6a): both milled first, so the countered spell was last in the graveyard.
# CounterTargetThenMillEffect reads the target's controller, counters, then mills; CounterThenMillOrderTest fails
# before (countered spell last) and passes after, and the driver replay of xs:XP:thought-collapse:cast:s1/s2 now
# matches gorge and Forge. Fork branch fix/thought-collapse-counter-first (5566c5c1f4 cherry-picked onto
# 11940683b90, so the Wild Shape fix stays in the pin).
# bec665044ec = 5cc61aaba0b (above) + Madame Web, Clairvoyant and Lila, Hospitality Hostess use FilterNonlandCard: their
# "cast ..." filters matched a land on top of the library (noncreature, common), so PlayFromTopOfLibraryEffect offered
# to play it; lands are played, not cast (CR 305.1, 601.1). MadameWebClairvoyantTest fails before (the Forest on top is
# playable) and passes after; the driver replay of xs:XP:madame-web-clairvoyant:static0.1:s1 no longer offers
# p0:Island|play| at step 4. Fork branch fix/madame-web-clairvoyant-nonland, based directly on 5cc61aaba0b.
XMAGE_REF  ?= bec665044ecd9134639047e1156af2c70c1da7e5
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
# a1bcd1166a = d0f16bee2e0 plus the cost-choice census (xproof cluster xpt-5b85ea4e): every card-choosing cost that fell
# to ScriptedCostDecision.choice()'s strict miss (CostReturn on Quirion Ranger, CostUntapType, CostReveal, CostPutCardToLib,
# CostGainControl, CostExiledMoveToGrave, CostBehold/BeholdExile, CostCollectEvidence, CostExert, CostEnlist,
# CostPromiseGift, CostExile's All/FromTopGrave/total-mana-value forms, CostRemoveAnyCounter per-counter picks) now takes
# gorge's logged pick, forced when exact; CostChooseColor/CreatureType/RevealChosen keep the parent's answer. Only
# RemoveAnyCounter of any kind over several permanents stays on choice(). Test CostReturnPickTest (2 tests,
# fixtures/costreturn); full driver suite 185 tests. Driver-only. Fork branch fix/xp-forge-driver-cost-choice-census.
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
# f7b730149a = oracle/pin-1125: the merge of the two fixes below (both built on d0f16bee2e).
# 752947823d = d0f16bee2e plus the fork's second-combat-damage-step priority fix b16a076d04 (cherry-picked as 67df148f93):
# PhaseHandler.onPhaseBegin COMBAT_DAMAGE no longer clears givePriorityToPlayer when assignCombatDamage(false) assigns nothing,
# so a lone first striker's regular damage step gives priority (CR 510.3, 510.4; xproof cluster forge_skips_damage_step,
# triage xpt-c647818a: Akroma, Angel of Wrath and Goblin Striker). SecondCombatDamageStepPriorityTest fails on d0f16bee2e and
# passes after. Driver and die-roll code unchanged; cardsfolder/tokenscripts unchanged. Fork branch
# fix/xp-forge-oracle-damage-step-priority.
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
# 3e6bd830a6f = 244c5908482 + gorge's discard_may election (xproof cluster xpt-e4d29b48, Evie Frye and Glacial Dragonhunt's
# DB$ Discard | Mode$ TgtChoose | Optional$ True | NumCards$ 1): Forge asks chooseCardsToDiscardFrom for 0..1, gorge poses a yes/no
# election (effDiscardTgtChoose, resume discard_may) and then the card pick, skipped when forced. ScriptedController.chooseCardsToDiscardFrom
# took only the card pick, so the yes was a leftover and the AI discarded ("Missing discard answer ... 0..1 to pick"). It now
# takes a queued discard_may yes/no for a min 0 ask first: no is an empty pick, yes is the 1..max pick (a lone card is forced,
# otherwise gorge's logged discard pick); AnyNumber$ has no election and is unchanged. DiscardMayElectionTest (yes with a lone
# card, no, yes then gorge's card pick) fails without the change and the full driver suite (198 tests) is green on it;
# scripts/forge-oracle-test-discard-may-election.sh (make forge-driver-tests) runs it. Driver-only; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-discard-may-election.
# 7d506c5192 = oracle/fpin-1125: the merge of 26b4658101 into f7b730149a (d0f25fff70), then 244c590848 (the mayplay pin
# above). 26b4658101 = f7b730149a plus remove-any-type's counter-kind pick left for the amount ask: CountersRemoveEffect
# .removeAnyType asks chooseCounterType then chooseNumber, and gorge logs one counter-kind pick per counter removed, so with two
# kinds offered chooseCounterType took the first pick and the amount came up one short (Mabel, Bitter Recluse removed one loyalty
# from Ajani Resolute instead of two; CR 122.1, 704.5i). For a RemoveCounter ask chooseCounterType now peeks the kind pick
# (StepMachine.peekCounterKind); other asks and the remove-any-counter cost still consume theirs. UnmappedAskTest's mixed-kinds
# fixture (xs:XP:mabel-bitter-recluse:cast:s1) fails before it; scripts/forge-oracle-test-counter-any-mixed-kinds.sh runs it.
# Driver-only; die-roll code, cardsfolder and tokenscripts unchanged.
# a6227e70a3 = 7d506c5192 plus cast_mode "evoked". StepMachine.spellForMode had no case for it, so the driver took Emptiness's
# hard-cost spell and Forge refused the {W/B}{W/B} evoke cast (xproof xs:XP:emptiness:f0t0:s1, Catharsis, Emptiness; CR 702.74a);
# it now picks the AlternativeCost.Evoke spell. scripts/forge-oracle-test-evoked-cast-mode.sh fails before it. Fork branch
# fix/xp-driver-evoked-cast-mode.
# afe18881c6 = oracle/fpin-1125: 7d506c5192 (above) + evoked cast_mode a6227e70a3 + discard_may 3e6bd830a6 (above); driver-only.
# 3bb8f906e3 = afe18881c6 plus TokenEffect.resolve registering the AtEOT delayed trigger when the token amount is below 1 (Mobilize X
# with X = 0 on Avenger of the Fallen, and the Firecat Blitz / Valduk / Lightning Coils X = 0 class): CR 702.181a's "Sacrifice them at
# the beginning of the next end step" is unconditional, so CR 603.7a creates the delayed trigger at resolution and it does nothing
# (CR 603.7c); the early `return` before makeTokenTable skipped the only registration point. A script with AtEOTNeedsCard$ True
# (Myriad, CR 702.116a) is still dropped by registerDelayedTrigger over the empty set. xproof clusters
# gorge-suspect:avenger-of-the-fallen:f0t0:stack and :combat0.attack:stack (triage xpt-09b99e96, xpt-c1665aee).
# MobilizeZeroTokensStillSchedulesSacrificeTest fails on afe18881c6 ("expected [true] but found [false]") and passes after;
# MyriadNoTokensCreatesNoDelayedTriggerTest and MordorOnTheMarchNothingExiledTest stay green. Driver, cardsfolder and tokenscripts
# unchanged. Fork branch fix/xp-mobilize-zero-delayed-trigger.
# cf742d0aefc = afe18881c6e plus a forced remove-counter amount with no counter pick (fork branch
# fix/xp-forge-driver-remove-counter-forced-amount): Ferropede's combat-damage trigger removes "a counter" of any type and
# Wall of Stone has one +1/+1 counter, so a lone kind and the card's fixed CounterNum$ 1 leave no choice and gorge logs no
# counter-kind pick (CR 122.1). CountersRemoveEffect.removeAnyType still asks chooseNumber(1, 1); the RemoveCounter branch
# defaulted only min == 0 and strict-missed the forced amount. It now answers min when no counter pick is logged and min == max,
# and keeps the miss for a free min < max range. UnmappedAskTest.removeAnyTypeForcedAmountRemovesTheLoneCounter (fixture
# counter-remove-forced-amount.jsonl) fails on afe18881c6e and passes after; scripts/forge-oracle-test-counter-any-mixed-kinds.sh
# (make forge-driver-tests) runs UnmappedAskTest. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged
# (same as FORGE_REF ff076d4432).
# 66671165f31 = afe18881c6e plus the up-to remove-any-type Done pick (fork branch fix/xp-forge-driver-counter-any-done-v2):
# when the controller stops an UpTo removal early gorge logs remove_counter_done ("Done"), which names no Forge CounterType
# and which CountersRemoveEffect.removeAnyType has no option for, so the kind ask strict-missed and left it queued. For a
# RemoveCounter ask StepMachine.peekCounterKind (26b4658101's peek design) now also matches the Done by its pick kind,
# consumes it and answers the first offered type; that type's chooseNumber finds no counter pick and answers 0 (min is 0 under
# UpTo), which is how removeAnyType stops. The kind ask peeks before the lone-option shortcut so a single-kind up-to removal
# consumes its Done too. Test UnmappedAskTest.removeAnyTypeDoneStopsTheUpToRemoval (fixtures/unmapped/counter-remove-any-done.jsonl)
# fails on afe18881c6e and passes after; scripts/forge-oracle-test-counter-any-done.sh (make forge-driver-tests) replays the
# Mabel, Bitter Recluse request with LOYALTY then Done and requires Ajani Resolute still on p1's battlefield at 1 loyalty and
# one +1/+1. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged.
# f1e3e00905c = the merge of 149a03bbbec4 (develop's pin: Done pick 66671165f31 + Mobilize X=0 3bb8f906e3) with cf742d0aefc
# (the forced remove-counter amount); the only conflict was UnmappedAskTest.java, which keeps removeAnyTypeDoneStopsTheUpToRemoval
# and removeAnyTypeForcedAmountRemovesTheLoneCounter. One pin carries all three driver fixes: ancestors 66671165f31, 3bb8f906e3
# and cf742d0aefc. Die-roll code unchanged; ForcedDieRollTest, MobilizeZeroTokensStillSchedulesSacrificeTest and every
# UnmappedAskTest case green on it (make forge-driver-tests runs the UnmappedAskTest cases). Fork branch
# fix/xp-forge-driver-remove-counter-forced-amount.
# 149a03bbbec4 = the clean merge of 66671165f31 and 3bb8f906e3 (the Mobilize X=0 AtEOT fix above): the two touch disjoint
# files (forge-oracle's up-to remove-any-type Done pick vs forge-game's TokenEffect), so the pin keeps both and the merge adds
# no code of its own. Driver and die-roll code unchanged from 66671165f31; cardsfolder/tokenscripts unchanged (same as FORGE_REF
# ff076d4432). Fork branch oracle/fpin-1125-mobilize-zero.
# bfdb3a8051 = oracle/fpin-1125: 149a03bbbec4 (above) merged with 46203751a5 (fork branch fix/xp-forge-driver-foretell-riot):
# cast_mode "foretell" takes the K:Foretell special action (StepMachine.spellForMode; checkActed accepts it with no stack once
# the card is exiled foretold, CR 702.143a/116.2h; Sozin's Comet, xpt-42b07d82), and Riot's UnlessCost$ AddCounter<1/P1P1>
# reads gorge's choose_n[riot] pick (ScriptedController.payCostToPreventEffect: the counter pick pays, "Gain haste" declines,
# CR 702.136a; Spider-Punk, xpt-cbd8207b). ForetellSpecialActionTest and RiotChoiceTest fail before it;
# scripts/forge-oracle-test-foretell-cast-mode.sh and forge-oracle-test-riot-choice.sh replay the finds. Driver-only;
# die-roll code, cardsfolder and tokenscripts unchanged.
# c9419dd87a = oracle/fpin-1125: bfdb3a8051 (foretell, Riot; above) joined with f1e3e00905 (Done pick, Mobilize, forced remove-counter amount; above); driver-only.
# 2b591baf9c = c9419dd87a + one driver commit: ScriptedController.chooseCardName(sa, faces, message) answers an empty face list with "" and a note
# (ChooseFromDefinedCards$ Remembered with no nonland card revealed, Alhammarret, High Arbiter; ChooseCardNameAi returns "" and the effect names
# nothing; gorge logs no decision) instead of a 'Missing card name answer' strict miss (xpt-e750d7a5). ChooseCardNameEmptyFacesTest fails before and
# passes after. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged.
# 0b7fde3680d = f1e3e00905c plus the zero-mode Charm driver fix (fork branch
# fix/xp-forge-driver-zero-mode-charm): Bumi, King of Three Trials's ETB is
# DB$ Charm | MinCharmNum$ 0 | CharmNum$ X with X = the Lesson cards in your
# graveyard. With none, CR 700.2 allows no mode and CR 603.3c removes the
# modal ability from the stack; gorge poses no KModes decision for that ask
# (engine/effects/charm.go returns before the decision when max < 1), so Forge's
# ask has exactly one answer, the empty list. chooseModeForAbility used to
# strict-miss because the offered mode list was not the ask's num. A num == 0
# ask now returns empty without consuming a mode decision queued for a later
# ask; every num > 0 ask keeps the strict miss. ZeroModeCharmTest (fixtures
# zero-mode/) replays Bumi with no Lesson (empty answer, no harness/leftover)
# and with one Lesson and no decision (num = 1 still strict-misses);
# scripts/forge-oracle-test-zero-mode-charm.sh runs it and replays the
# zero-Lesson request through forge-oracle-run.sh. Driver-only; die-roll code,
# cardsfolder and tokenscripts unchanged.
# 9f2d35a4679 = the clean merge of c9419dd87a3 (above: foretell, Riot) and 0b7fde3680d (above: zero-mode Charm). Both fork
# from f1e3e00905 and touch forge-oracle only, so one pin carries every driver fix: Done pick, Mobilize X=0, forced
# remove-counter amount, foretell cast mode, Riot choice and the zero-mode Charm answer. Die-roll code unchanged;
# ForcedDieRollTest, ZeroModeCharmTest, ForetellSpecialActionTest and RiotChoiceTest green on it.
# Fork branch fix/xp-forge-driver-zero-mode-charm.
# 0558bd00b88 = oracle/fpin-1125: c9419dd87a (foretell, Riot, Done pick, Mobilize, forced remove-counter; above) merged with
# c332e2b33f5 (the unpinned simultaneous zone order, xproof cluster xpt-34c28f9c): Sorin, Lord of Innistrad's -6 Destroy
# moves p1's Crimson Kobolds and Tibalt, the Fiend-Blooded to the graveyard, then a ChangeZoneAll returns them in the SAME
# resolution. They are in neither checkpoint, so forge_order.go derives no graveyard_order pin, and Forge asks "order to
# Graveyard" (and the return asks "order to Battlefield"). StepMachine.orderCards keeps Forge's offered order for an
# unpinned ask at exactly those two destinations; every other zone (library arrange/dig, exile, hand, command, stack,
# sideboard) still strict-misses, and a visible graveyard arrival still consumes its pin, so a real ordering difference on
# a move a checkpoint can see is still caught. OrderCardsTest's unpinnedGraveyardOrderKeepsForgeOrder,
# unpinnedBattlefieldOrderKeepsForgeOrder, unpinnedOtherZoneStillLooseMisses and pinnedGraveyardOrderStillApplies cover
# both halves; scripts/forge-oracle-test-graveyard-transit-order.sh (make forge-driver-tests) runs them. Driver-only;
# cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-transit-graveyard-order.
# a1990dd5f833 = the clean merge of 9f2d35a4679 (above: foretell, Riot, Done pick, Mobilize X=0, the forced remove-counter
# amount and the zero-mode Charm answer) and 0558bd00b88 (above: the unpinned transit Graveyard/Battlefield order). The two
# sides touch disjoint hunks of StepMachine.java (hasMode vs orderCards/keepsForgeOrderWithoutPin) and separate
# ScriptedController and test files, so one pin carries every driver fix: the zero-mode charm answer and the transit-order
# fallback. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged. Fork branch
# fix/xp-forge-driver-zero-mode-charm (advanced with the transit-order pin).
# eb695169f9 = 2b591baf9c (the empty face list above) merged with 0558bd00b88 (the transit zone order above), so one pin
# carries both driver fixes: chooseCardName answers an empty face list with "" and orderCards keeps Forge's offered order
# for an unpinned Graveyard/Battlefield transit. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged. Fork
# branch fix/xp-forge-driver-name-empty-faces.
# 141e1ff5403 = the clean merge of a1990dd5f833 (above: zero-mode Charm and the transit order) and eb695169f9 (above: the
# empty-face chooseCardName and the transit order). The two touch separate ScriptedController methods (chooseModeForAbility
# and StepMachine.hasMode vs chooseCardName) and separate test files, and both carry the same 0558bd00b88 transit-order
# half, so one pin carries every driver fix: the zero-mode charm answer, the empty-face chooseCardName answer and the
# transit-order fallback. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged. Fork branch
# fix/xp-forge-driver-zero-mode-charm (advanced with the empty-face chooseCardName pin).
# 3d8f89a00d = eb695169f9 (above) + fork branch fix/xp-forge-driver-evoke-trigger-label: StepMachine.triggerScore binds gorge's fixed
# Evoke label "<card>: sacrifice it (evoked)" (CR 702.74a, trigger_queue.go) to the host's Forge trigger whose keyword is Evoke, so
# orderTriggers no longer misses it beside an ETB trigger (CR 603.3b; Emptiness, Catharsis; xpt-58b75f98, xpt-93baef91);
# TriggerOrderEvokeLabelTest fails before it (1a2655bbd3 is that commit). Also joins oracle/fpin-1125's 2d4eac3d93 (DigMultiple pick,
# AlternativeDecider library position). Driver-only; cardsfolder and tokenscripts unchanged.
# 68d36f8247a = the clean merge of 141e1ff5403 (above: zero-mode Charm, the empty-face chooseCardName and the transit order) and
# 3d8f89a00d (above: the Evoke trigger label, the DigMultiple pick and the AltDestination library position). The two touch
# separate ScriptedController methods (chooseModeForAbility/chooseCardName vs triggerScore/chooseDig/alt-destination) and
# separate test files; the shared eb695/0558 halves are the same on both sides, so one pin carries every driver fix: the
# zero-mode charm answer, the empty-face chooseCardName answer, the transit-order fallback, the Evoke trigger label and the
# DigMultiple/AltDestination picks. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged. Fork branch
# fix/xp-forge-driver-zero-mode-charm (advanced with the evoke/DigMultiple pin).
# 1da9dc5c96c = 68d36f8247a (above: every driver fix on develop) plus the one-card TwoPiles split (fork branch
# fix/xp-forge-driver-twopiles-one-card-split-v3; xproof cluster xpt-fc88455d): Epiphany at the Drownyard cast with X = 0 reveals one
# card, and gorge's effTwoPiles poses no split for a one-card set (the two splits are the same partition, CR 700.3), logging only
# the opponent's pile pick. Forge's TwoPilesEffect still asks the separator "Divide cards into two piles" (1 option, 0..1), which
# strict-missed. StepMachine.loneTwoPilesSplit recognises that ask when no gorge object pick is queued and ScriptedController
# answers the card as pile 1 (gorge's pile A), so chooseCardsPile reads the "First pile" pick as before; a queued split pick still
# goes through pickObjects. TwoPilesOneCardSplitTest fails before it with the exact miss;
# scripts/forge-oracle-test-twopiles-one-card-split.sh (make forge-driver-tests) runs it. Driver-only; die-roll code,
# cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432). The empty-face chooseCardName fix (2b591baf9c) is
# carried: ChooseCardNameEmptyFacesTest, OrderCardsTest, TriggerOrderEvokeLabelTest and ForcedDieRollTest are green on it.
# 65d8060f39ba = 1da9dc5c96c plus the empty Optional$ Animate confirm (xproof cluster xpt-fc2992c4, Sita Varma, Masked Racer's Exhaust
# with X = 0 and no other creature): "you may have the base power and toughness of each other creature you control become ..." with no
# other creature is an election with no board effect either way (CR 608.2d), so gorge logs no ask, while Forge's AnimateEffect still
# calls confirmAction("It has base power and toughness N/N until end of turn.") and the driver strict-missed with nothing queued.
# ScriptedController.emptyOptionalAnimateConfirm answers no, noted as "empty animate confirm", when the ability is an Optional$ Animate,
# no yes/no is queued for the seat and none of the cards AnimateEffect would animate (its targets, else Defined$, plus
# ThisDefinedAndTgts$) is on the battlefield and not phased out; a target spell or any such card keeps the strict miss.
# EmptyOptionalAnimateConfirmTest (fixtures/animate: the find, and the same request with a Grizzly Bears) fails before it with the exact
# miss; scripts/forge-oracle-test-empty-optional-animate.sh (make forge-driver-tests) runs it. Driver-only; die-roll code, cardsfolder
# and tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-empty-optional-animate.
# 931d6309cc3 = 65d8060f39ba plus the Optional$ Attach per-attachment yes/no (xproof cluster xpt-0ae740c1, Bruna, Light of Alabaster):
# its RepeatEach over the Auras that can enchant it (CR 303.4) runs BrunaAttach once per Aura, so gorge poses one yes/no per Aura
# (resume attach_optional, pick kind yes/no, no card ref) where it poses the attach_subset card pick for Heavenly Blademaster. The
# attachConfirm that read only card refs found none, left attachYes empty and answered every confirm no. ScriptedController.attachConfirm
# now answers from the FIRST attach decision of the step: an attach_optional yes/no at the head answers that very confirm, noted
# "attach-confirm step N: yes|no X (attach_optional)"; an earlier effect's attach_subset / attach_choice pick is taken first, as before
# (so a later effect's yes/no is never spent on it). Decisions carry no Aura ref, so several Auras bind to the yes/no's in gorge's
# log order. UnmappedAsksJoinDirectionAttachTest: brunaAttachOptionalYesAttaches (fixtures/unmapped/attach-optional-yes.jsonl, the find;
# fails before the fix with "attach pick step 10: []", and asserts Cowed by Wisdom attached_to Bruna in the step-10 snapshot) and
# attachOptionalDoesNotPreemptAnEarlierSubsetPick (attach-optional-mixed.jsonl: the subset fixture plus a later attach_optional No);
# scripts/forge-oracle-test-attach-optional-yes.sh (make forge-driver-tests) runs the class (7 tests) with the Heavenly Blademaster and
# Nahiri cases. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432); full driver suite
# 227 tests. Fork branch fix/xp-forge-driver-attach-optional-yes.
# e99e48e2b50f = 65d8060f39ba plus the forced-pick leaves a queued pick for the later ask (xproof clusters xpt-5e60af5c and
# xpt-f2f006f3, The Gitrog, Ravenous Ride: Memnite saddles it and it hits, and at the trigger's resolution Forge's Sacrifice cost
# offers the one saddler, so the pick is forced and gorge logs none; the queued p0:Swamp/p0:Forest pick answers the LATER "put up to
# X land cards from your hand onto the battlefield" ask of the same step, but StepMachine.pickObjects took the queued pick before it
# checked the forced case, gave it to the Sacrifice cost and strict-missed "gorge pick p0:Swamp is not among Forge's 1 options").
# pickObjects now returns the options of a forced ask (opts.size() <= min) without consuming a queued pick whose refs are not among
# them (queuedPickBelongsToLaterAsk), noted as "forced, queued pick left for a later ask"; a queued pick naming an option is taken
# as before. LoneForcedCostPickTest (fixtures/gitrog: the f0a0_s1 find) fails before it with the exact miss;
# scripts/forge-oracle-test-lone-forced-cost-pick.sh (make forge-driver-tests) runs it. Driver-only; die-roll code, cardsfolder
# and tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-lone-forced-cost-pick.
# 85e4e85fde = 2ece06813a merged with 65d8060f39ba. 2ece06813a (on 1da9dc5c96c) is the dual-typed land colour (xproof cluster
# xpt-079c8ffe, 102 items: shock, battle, cycling and surveil duals): a land with two basic land types (CR 305.6) has two
# intrinsic Secondary$ mana abilities, gorge's choice between them is dropped from the request (the step names the ability by
# index), and StepMachine.abilityFor's basic-land tier kept both and threw "ability identity ... candidates: Mana T; Mana T".
# forge-export now sends "intrinsic: basic land mana <C>" for such a land and the tier filters by that colour; a bare line is
# unchanged. scripts/forge-oracle-test-dual-land-colour.sh (make forge-driver-tests) replays Underground Sea's U and B abilities;
# it fails at 1da9dc5c96c and 65d8060f39ba with the exact miss and passes here. Driver-only; die-roll code, cardsfolder and
# tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-dual-colour-on-fpin-65d8.
# 34f0585c97 = oracle/fpin-1125: 85e4e85fde (dual-land colour; above) joined with e99e48e2b5 (Gitrog lone forced pick; above); driver-only.
# 7cae17380d = 34f0585c97 plus cast_mode "room_alt" (xproof clusters xpt-34d42446, xpt-ea3ecf04 and xpt-eab5898f: Central
# Elevator // Promising Stairs and Dazzling Theater // Prop Room). A Room is a split card cast as one of its doors (CR 709.3,
# 709.3a); gorge pays three mana for the second door (Promising Stairs {2}{U}, Prop Room {2}{W}), whose front door ({3}{U},
# {3}{W}) is unaffordable. StepMachine.spellForMode had no case for the mode, so spellFor returned the first (front) spell and
# Forge refused the cast ("was not put on the stack"). It now selects the spell whose CardStateName is RightSplit.
# scripts/forge-oracle-test-room-alt-cast-mode.sh (make forge-driver-tests) replays the Dazzling Theater find; it fails before
# with the exact miss and passes here. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF
# ff076d4432). Fork branch fix/xp-forge-driver-room-alt-cast-mode.
# 0fe8c90e549 = oracle/fpin-1125-attach: 34f0585c97 (fpin-1125; above) joined with 931d6309cc3 (attach_optional; above); driver-only.
# d950b048823 = oracle/fpin-1125-room-alt: 0fe8c90e549 (attach_optional; above) joined with 7cae17380d (room_alt; above); driver-only.
# 1707ccc4018 = 0fe8c90e549 plus K'rrik's "pay 2 life rather than {B}" (xproof cluster xpt-eaa38e91, Muck Rats under K'rrik, Son of Yawgmoth):
# ScriptedController.payPhyrexianWithLife paid only Phyrexian pips with life, so gorge's pay_life pick on a plain {B} pip was never taken,
# the cost stayed unpaid and the cast was refused "was not put on the stack". While the player has PayLifeInsteadOf:B (CR 601.2h, 118.9)
# the pick now drops one black shard and pays 2 life, as Forge's ComputerUtilMana and InputPayManaOfCostPayment do; no pick, no life.
# PhyrexianLifePaymentTest.krrikPaysTheBlackPipWithTwoLife (fails on 0fe8c90e549) and a no-pick control, run by
# scripts/forge-oracle-test-krrik-life-instead-of-b.sh (make forge-driver-tests). Driver-only; cardsfolder/tokenscripts unchanged.
# Fork branch fix/xp-forge-driver-krrik-life-instead-of-b (PR adams-shaun/forge#12).
# 22d31c3e9d0 = oracle/fpin-1125-room-alt: d950b048823 (room_alt + attach_optional; above) joined with 1707ccc4018 (K'rrik pay life; above); driver-only.
# e76652fae3 = c5ab80906 (movectr + cantprevent; above) joined with 1707ccc4018 (K'rrik pay life; above); driver-only.
# a2201414482 = oracle/fpin-1125-room-alt: 22d31c3e9d0 (room_alt + attach_optional + K'rrik; above) joined with e76652fae3 (movectr + cantprevent + K'rrik; above); driver-only.
# 793a015fcf = oracle/fpin-1125-join3: a2201414482 (above) joined with 1947f2b322 (ScenarioReplay --serve warm worker + FORGE_ORACLE_AI_TIMEOUT_S; additive, unset keeps the one-shot run); driver-only, die-roll code unchanged.
# a8512930733 = 793a015fcf plus the unaffordable UnlessCost$ decline (ScriptedController.payCostToPreventEffect notes
# "unless-pay step N: unaffordable, declined" and returns false; UnlessCostUnaffordableTest covers it and the affordable
# control that still misses); driver-only, die-roll code unchanged.
# 846c4191e6 = 793a015fcf (above) + StepMachine.keepsForgeOrderWithoutPin keeps Forge's order for an unpinned multi-name
# "order to Hand": the snapshot sorts a hand's names, so gorge records no order for Wrenn and Seven's -8 returning the
# planeswalker and seven Forests, and the ask strict-missed with an empty leftover. OrderCardsTest.unpinnedHandOrderKeepsForgeOrder,
# run by scripts/forge-oracle-test-graveyard-transit-order.sh (make forge-driver-tests). Driver-only; cardsfolder/tokenscripts
# unchanged. Fork branch fix/xp-forge-driver-hand-order.
# 15a7c960ab2 = 793a015fcf plus the setup Room convention (xproof cluster xpt-e4b207b7, Greenhouse // Rickety Gazebo): CR 709.5d gives a Room that
# enters without being cast neither unlocked designation, so it has no name (CR 709.5). SetupBuilder.unlockNamedDoor used to unlock the door the
# setup name named, so Forge's setup read "Greenhouse" where gorge's fieldName and the XMage driver read "". Setup now places the Room locked and
# the name only binds the card; scripts/forge-oracle-test-room-setup-locked.sh (make forge-driver-tests) pins it. Driver-only; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-setup-room-locked.
# d6e26fd04306 = 793a015fcf + the checkpoint snapshot's "offered" (xproof cluster xpt-a64b83f5: every compare:[offered] item diverged at setup
# because the driver wrote no offered field): SnapshotWriter writes the priority holder's playable abilities, from the listing the bridge
# answers priority from plus mana abilities as activations, when the item's compare names offered; sources by RefTable.refOf (gorge's objRef).
# scripts/forge-oracle-test-setup-offered.sh (make forge-driver-tests) replays the Muldrotha find (a graveyard Plains offered as a play) and runs
# RefTableTest.refOfNamesAnObjectAsGorgeSnapshotsIt; it fails on 793a015fcf. Driver-only; cardsfolder/tokenscripts unchanged.
# Fork branch fix/xp-forge-driver-setup-offered.
# 098e9c802e = oracle/fpin-1125-join5: 846c4191e6 (order to Hand; above) joined with d6e26fd04306 (offered; above); driver-only, die-roll code unchanged.
# 9cc07f0d340 = oracle/fpin-1125-join4: 15a7c960ab2 (setup Room locked; above) joined with d6e26fd04306 (offered; above); driver-only, die-roll code unchanged.
# 032ce65c73b = oracle/fpin-1125-join-hand-offered-room: 098e9c802e (order to Hand; above) joined with 9cc07f0d340 (setup Room locked; above); both already carry d6e26fd04306 (offered); driver-only, die-roll code unchanged.
# a862cd8abc = 793a015fcf plus the empty hidden-origin search fix (ScriptedController only, fork branch fix/xp-forge-driver-empty-hidden-search):
# Dina's Guidance finding no creature still makes Forge ask ChangeZoneToAltDestination; keep the primary destination with a note, no miss (CR 609.3),
# and consume the ordinary changezone_dest_alt answer when a card was found. Driver-only; die-roll code unchanged.
# 0218db82d91 = oracle/fpin-1125-join6: 9cc07f0d340 (setup Room locked + offered; above) joined with a862cd8abc (empty hidden-origin search; above);
# driver-only, die-roll code unchanged.
# 1b593040f66 = oracle/fpin-1125-join7: 032ce65c73b (order to Hand + setup Room locked + offered; above) joined with a862cd8abc (empty hidden-origin search; above);
# driver-only, die-roll code unchanged.
# 926004bb5396 = oracle/fpin-1125-join8: 032ce65c73b (order to Hand + setup Room locked + offered; above) joined with
# a8512930733 (unaffordable UnlessCost$ decline; above); driver-only, die-roll code unchanged.
# 6fcacdc21814 = oracle/fpin-1125-join9: 1b593040f66 (empty hidden-origin search + order to Hand + setup Room locked + offered; above) joined with
# a8512930733 (unaffordable UnlessCost$ decline; above); driver-only, die-roll code unchanged.
# b0ddb8556a = d6e26fd04306 plus the empty Optional$ ChangeTargets confirm (xproof cluster xpt-bf9a34cd: Redirect on Reach
# Through Mists, a targetless spell, still makes Forge ask "Do you want to change targets?" while gorge's effChangeTargets
# skips a declaration with no old targets and logs no decision, CR 115.7): ScriptedController.emptyChangeTargetsConfirm
# answers no, noted as "empty change targets confirm", only when no yes/no is queued and no subject spell or subability holds
# a target; a targeted subject still takes the queued answer and keeps the strict miss when none was logged.
# 019d50429f = b0ddb8556a plus the targeted ChangeTargets retarget pick (CR 115.7): a subject that DOES hold a target logs a
# gorge choose_n marked retarget, so DecisionQueue.Decision reads the marker, ScriptedController.retargetConfirm answers the
# optional confirm from it (an empty pick declines and is consumed; picks answer yes and stay for the new-target ask), and
# StepMachine.retargetTargets applies the pick to the declaration. ChangeTargetsConfirmTest (fixtures/changetargets) fails
# before it with the exact miss; scripts/forge-oracle-test-change-targets.sh (make forge-driver-tests) runs it. Driver-only.
# 53059b9bbf2 = oracle/fpin-1125-join10: 6fcacdc21814 (join9; above) joined with 019d50429f (the ChangeTargets accept/retarget
# fix; above); driver-only, die-roll code unchanged (same as FORGE_REF ff076d4432).
# 99e8228cba = 53059b9bbf2 plus the pending-declaration offered rule (xproof cluster setup:offered, a-realm-reborn, muldrotha, icetill-explorer,
# mm-menon-the-right-hand): SnapshotWriter.offered is empty at the declare-attackers/declare-blockers step before anything is declared while the
# active player has a legal attacker / the defender a legal blocker (CR 508.1, 509.1, 117.3a: the turn-based declaration precedes priority, and gorge's
# checkpoint is that pending decision). Forge hands the seat priority once its callback returns without a declaration and used to list the seat's mana
# abilities. scripts/forge-oracle-test-declare-attackers-offered.sh (make forge-driver-tests) fails on 53059b9bbf2 and passes here. Driver-only;
# cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-offered-declaration.
# a4380dd47f = 99e8228cba, narrowed: only a pass that ENTERS the declaration step offers nothing (StepMachine passes enteredStep to
# SnapshotWriter.offered); an explicit empty attack/block is answered, so the priority after it still offers the holder's options.
# The test script gains empty-attack/empty-block rows (red on 99e8228cba). Driver-only, die-roll code unchanged.
# e45e911953c = 53059b9bbf2 plus the below-min target decline (xproof cluster xpt-6e2fff25, Kitsune, Dragon's Daughter
# with p1's lone Grizzly Bears): the trigger's "exchange control of two other target creatures controlled by different
# players" needs exactly two targets (TargetMin$ 2 | TargetMax$ 2 | TargetsWithDifferentControllers) and only one other
# creature exists, so the required number of targets cannot be chosen and the ability is removed without going on the
# stack (CR 603.3d with 601.2c); gorge logs no target ask. Forge's TargetRestrictions.hasCandidates is true with one
# candidate, so Forge asked for targets gorge never posed and StepMachine.targets ended in "Missing target answer at
# step 9" (Forge's own TargetSelection would also fail to reach min and drop the trigger). targets now mirrors
# TargetSelection's gate right before the miss: when no pending decision has unused refs and even the largest legal set
# cannot reach min (getAllCandidates already filters TargetUnique and already-chosen through canTarget; the distinct
# controller count is mirrored for TargetsWithDifferentControllers/ForEachPlayer), it notes "fewer candidates than min,
# ability not put on the stack" and returns false without a miss; enough candidates plus no gorge answer keeps the miss.
# TargetsBelowMinTest (fork fixtures targets-below-min/) fails on 53059b9bbf2 with the exact miss and passes here;
# scripts/forge-oracle-test-targets-below-min.sh (make forge-driver-tests) runs it. Driver-only; die-roll code,
# cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-targets-below-min.
# 61f44264a28 = e45e911953c plus the mandatory ChangeTargets retarget regression test (xproof cluster xpt-6b85f89a,
# Revolutionary Rebuff / Redirect Lightning, gorge~forge): p0's Rebuff targets Reach Through Mists, p1 casts Redirect
# Lightning (additional cost 5 life) targeting the Rebuff, and as Redirect resolves gorge's effChangeTargets retargets
# the Rebuff to Redirect Lightning (still a nonartifact spell on the stack, CR 608.2) through a one-candidate choose ask
# (CR 115.7a: a mandatory change cannot keep the old target, so the Mists is not offered) whose pick travels in
# pick_refs alone (no option label), marked retarget. The 019d50429f retarget pick path already consumes it at Forge's
# chooseNewTargetsFor, so the find replays clean at e45e911953c and this pin adds no driver code: only the test class
# ChangeTargetsMandatoryTest and its fixture (fork fixtures/changetargets/redirect-lightning-rebuff.jsonl). The test
# fails on b0ddb8556a (before the 019d50429f targeted retarget) with the exact "Missing new targets answer at step 11:
# Counter target nonartifact spell unless its controller pays {2}" miss; scripts/forge-oracle-test-change-targets-mandatory.sh
# (make forge-driver-tests) runs it. Driver tests only (test class + fixture); cardsfolder and tokenscripts unchanged
# (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-new-targets-resolve.
# NOTE (merge): ChangeTargetsMandatoryTest lives only on fork commit 61f44264a28 (= e45e911953c + the test), which lacks the
# pending-declaration offered rule (a4380dd47f). The ref below (join12) keeps that driver fix, so
# scripts/forge-oracle-test-change-targets-mandatory.sh needed a fork join of 61f44264a28 into join12 before make
# forge-driver-tests passed it; join14 below is that join.
# 8b89eabc655 = e45e911953c (above) with the PINNED half of the transit graveyard order (xproof cluster xpt-18625cef,
# Thassa's Rebuff): Seed of Hope's mill moves Island and Forest together, Island returns to the hand within the step, so
# forge_order.go's checkpoint delta pins only [Forest, Seed of Hope] while Forge's "order to Graveyard" ask offers the
# milled pair, and StepMachine.orderByNames strict-missed whenever an offered card was not named by the pin. forge-export
# now writes graveyard_ends ("<step>:<seat>" to the names in that seat's graveyard at the step's end) and
# StepMachine.orderGraveyardPin orders the named cards by the pin and keeps Forge's offered order after them for an
# unnamed card whose name is absent from the end graveyard (its position is not observable at the next checkpoint, so no
# gorge answer can exist), with a note; a name the end graveyard DOES contain is an arrival the checkpoints see, so an
# unnamed one still misses, and an absent end list still misses too. OrderCardsTest's
# pinnedGraveyardOrderKeepsATransitOfferedCard (red on e45e911953c), pinnedGraveyardOrderStillMissesAnUnnamedCardTheCheckpointsSee
# and pinnedGraveyardOrderWithoutEndNamesStillMisses cover both halves; scripts/forge-oracle-test-graveyard-transit-order.sh
# (make forge-driver-tests) pins the 17-test count. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged
# (same as FORGE_REF ff076d4432). Fork branch driver-transit-pin.
# 2ac6012982e = oracle/fpin-1125-join11: a4380dd47f (the pending-declaration offered rule; above) merged with e45e911953c (the
# below-min target decline; above), no textual conflicts; driver-only, die-roll code unchanged (same as FORGE_REF ff076d4432).
# 6d6b2d485ab = oracle/fpin-1125-join12: 2ac6012982e (join11, above) merged with 8b89eabc655 (the pinned transit graveyard
# order, above), no textual conflicts (StepMachine.java auto-merged); driver-only, die-roll code unchanged (same as
# FORGE_REF ff076d4432). r0's 1656bcb2a73 was described here but never pushed to the fork; this is the pushed merge.
# ad4cd3ba0f8 = oracle/fpin-1125-join14: join12 (6d6b2d485ab, above) merged with the PayLife<X> announcement 892f8bd63b6 (below) and the
# mandatory ChangeTargets regression test 61f44264a28 (ChangeTargetsMandatoryTest; the join the NOTE (merge) above asks for), no textual
# conflicts. It supersedes the earlier join13 586a3850332 (join11 + both), which lacked the transit order. Xproof cluster xpt-b1209a2d.
# Phyrexian Processor's "pay any amount of life" is AB$ StoreSVar | Cost$ Mandatory PayLife<X>: Forge announces the amount as the cost's X (PlaySpellAbility.announceValuesLikeX), while gorge logs it as a
# choose_n with pick kind paylife and an "N life" label, 0 included (CR 119.4, 118.3). ScriptedController.announceRequirements took the X
# only from an x decision, so the pick was a leftover and the AI answered ("Missing x answer at step 6: X for"). It now binds a paylife
# pick ("N life") to the X announce of an ability whose cost has a CostPayLife with amount X, after the x decision is tried and only
# for that cost shape. PhyrexianProcessorPayLifeTest (0 life: no harness row, no leftover, life 20; 3 life: life 17; fixture fork
# fixtures/phyrexian/processor-zero-life.jsonl, the exported xs:XP:phyrexian-processor:cast:s1 request) fails without the change with
# the exact "Missing x answer at step 6: X for" miss; scripts/forge-oracle-test-phyrexian-processor-paylife.sh (make forge-driver-tests)
# runs it. Driver-only; cardsfolder/tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-paylife-x.
# 6095b085edc = ad4cd3ba0f8 (join14; above) plus the face-down cast modes (StepMachine.spellForMode only): gorge's cast_mode
# "morphed"/"megamorphed"/"disguised" (CR 702.37c, 702.168a, 702.169a) hit the spellForMode default, so the driver cast the
# card's printed spell and Forge refused it where the printed cost exceeds the mana gorge paid (Belltoll Dragon megamorphed for
# {3} over three Islands: "step 3 (cast) p0:Belltoll Dragon was not put on the stack", xproof cluster xpt-716677db, 2 cycles /
# 4 items gorge~forge) and cast face up where it does not (Sleuth Instructor {1}{W}). spellForMode now maps all three modes to
# the one face-down spell Forge builds per printed keyword (CardFactoryUtil.abilityCastFaceDown, a Spell with
# setCastFaceDown(true) and Cost {3}; Megamorph builds it with the same Morph key). FaceDownCastModeTest (fixtures/morph/,
# Belltoll Dragon megamorphed, Weaver of Lies morphed and Sleuth Instructor disguised, each cut after the cast) fails on
# ad4cd3ba0f8 with the exact "was not put on the stack" harness row (megamorphed, morphed) and the printed-spell note
# (disguised), passes here; scripts/forge-oracle-test-facedown-cast-mode.sh (make forge-driver-tests) runs it. Driver-only;
# die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-facedown-cast.
# dcbac6a957 = 2ac6012982e plus the altaddcost tap ability choice (xproof cluster xpt-bf618006, Disruption Protocol,
# gorge~forge): p0 casts Disruption Protocol choosing gorge's altaddcost pick "Tap 1 artifact" (Memnite), and Forge offers
# two SAs (tap an artifact / pay {1}). gorge is right (CR 601.2b: either additional cost). getAbilityToPlay bound the pick
# by substring of the cost text, then by "cost" + verb against the CostPart class name; "tap 1 artifact" is not in Forge's
# cost text (CostTapType prints "Tap 1 untapped Artifact you control") and the part class of tapXType<1/Artifact> is
# CostTapType (costtaptype != costtap), so neither rule bound and the strict run missed ("Missing ability choice answer at
# step 7"). The verb fallback now goes through costPartClassFor, an explicit map from gorge's CostPhrase label to the Forge
# CostPart class: "tap this permanent" ({T}) is CostTap, any other tap label is CostTapType (same split for untap), "pay N
# life"/"pay N energy"/plain mana is CostPayLife/CostPayEnergy/CostPartMana, "take N damage" CostDamage, a gain-life label
# CostGainLife, "add N <kind> counters" CostPutCounter, "put ... into your library" CostPutCardToLib, "put ... from exile"
# CostExiledMoveToGrave, "return ... to its owner's hand" CostReturn, dice/coins CostRollDice/CostFlipCoin, a chosen reveal
# CostRevealChosen; unlisted verbs keep the old "cost" + verb naming, and the discard/sacrifice/forage/blight rules are
# unchanged. TapTypeAbilityChoiceTest (fork fixtures altaddcost/disruption-protocol-tap-s1.jsonl, the exported find) fails
# on 2ac6012982e with the exact miss and passes here; scripts/forge-oracle-test-tap-type-ability-choice.sh (make
# forge-driver-tests) runs it. Driver-only; cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork
# branch fix/xp-forge-driver-tap-altaddcost (tip 5bc8d1f9b0 adds the ChangeTargetsMandatoryTest join; merged into join15 below).
# 9124c696744 = oracle/fpin-1125-join15: join14 (ad4cd3ba0f8, above) merged with 5bc8d1f9b0 (dcbac6a957, the altaddcost tap ability
# choice above, plus the ChangeTargets test join14 already carries), no textual conflicts (ScriptedController.java auto-merged); the
# merge tree is 173000f9b6f. TapTypeAbilityChoiceTest and the join14 driver tests run together at this pin. Driver-only; die-roll code,
# cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# 686e3f82713 = oracle/fpin-1125-join17: join15 (9124c696744, below) merged with 6095b085edc (the face-down cast modes above), no
# textual conflicts (ScriptedController.java and StepMachine.java touch different areas; merge tree 83e433b219b). FaceDownCastModeTest,
# TapTypeAbilityChoiceTest and ForcedDieRollTest run together at this pin (11 tests, 0 failures). Driver-only; die-roll code,
# cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# 9b233511947 = 686e3f82713 (join17, above) plus the forced Hand->Library put-back (xproof cluster xpt-83d468df, gorge~forge,
# Sawtooth Loon): with exactly ChangeNum$ cards eligible gorge poses only the CR 401.4 placement order (an "order" decision of
# pick kind library_placement, first pick nearest the top), while Forge asks the forced selection one card at a time ("2 options,
# 1..1 to pick", twice) and places the picks in order; the driver never consumed the order and missed ("Missing zone change answer
# at step 8"). StepMachine.seedHandLibraryPlacement seeds the per-step carry from the seat's NEXT decision when it is a
# library_placement order naming exactly the offered cards, so pickObjects answers each ask from it (the export already reverses a
# top placement for Forge's successive puts). scripts/forge-oracle-test-hand-library-placement.sh (make forge-driver-tests)
# replays the find and fails on 686e3f82713 with the exact miss. Driver-only; die-roll code, cardsfolder and tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-hand-library-placement.
# 0ca7a220bd0 = oracle/fpin-1125-join18 tip: 3405b168dc3 (join17 above merged with join16 852c41880dd, which is 0aee1572747's
# Speedball optional ChangeTargets confirm regression test: ChangeTargetsConfirmTest runs six tests at this pin, the sixth
# speedballTriggerChangeTargetsConfirmConsumesTheRetargetPick) merged with a2954de67aa (no legal new target keeps the targets,
# ChangeTargetsNoCandidateTest) and 0ca7a220bd0 (the retarget count excludes the current target; review r1, xpt-5b82851f), no
# textual conflicts (ScriptedController.java: the join16/join17 confirm rule and the no-candidate guard touch different areas).
# Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# 47dd7358b32 = oracle/fpin-1125-join19: c72762cfadea (join18 tip: 0ca7a220bd0 above plus 53635f189c, the unbound optional-untap
# election answer, xpt-4782660c) merged with 9b233511947 (the forced Hand->Library put-back, above), no textual conflicts
# (StepMachine/DecisionQueue vs ScriptedController ChangeTargets touch different areas; merge tree 5d6202a0c7cb). ForcedDieRollTest
# re-run green on the pin. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# a2954de67aa = ad4cd3ba0f8 (join14) + the no-legal-new-target answer (xproof cluster xpt-5b82851f, Deflection, Shunt and Swerve): Forge's
# mandatory ChangeTargets effect asks chooseNewTargetsFor even when no new target is legal, and gorge poses no decision then (the target
# stays, CR 115.7a), so the ask was a strict miss ("Missing new targets answer at step 15") once Aerial Volley's only creature with flying
# left the battlefield. ScriptedController.chooseNewTargetsFor now counts the candidates Forge's TargetSelection would offer (the ability's
# target restrictions plus the effect's filter, targets cleared and restored as Forge does) and, with fewer than the declaration's targets,
# keeps them with a note; a legal candidate and no gorge decision is still the miss. ChangeTargetsNoCandidateTest (fixtures/changetargets/
# deflection-aerial-volley-no-candidate.jsonl, the exported xs:XP:deflection:cast:s1 request) fails without the change with the exact miss;
# scripts/forge-oracle-test-change-targets-no-candidate.sh (make forge-driver-tests) runs it. Driver-only; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-change-targets-no-candidate.
# 3405b168dc = oracle/fpin-1125-join18: join17 (686e3f82713, join15 + the face-down cast modes 6095b085edc) merged with
# join16 (852c41880dd, join15 + the Speedball optional ChangeTargets confirm 0aee1572747, review of cli-20261125T132000Z-b46ca2c9),
# no textual conflicts (ScriptedController.java auto-merged).
# 5c66bb14fa = 3405b168dc merged with a2954de67aa (the no-legal-new-target answer above), no textual conflicts
# (ScriptedController.java auto-merged). The ChangeTargets tests, TapTypeAbilityChoiceTest and the join14/15 driver tests run
# together at this pin. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# 0ca7a220bd = 5c66bb14fa + the r1 review follow-up: the candidate count excludes the declaration's current targets (Forge's
# own selection clears them first, PlayerControllerHuman.chooseNewTargetsFor, so it does not; but re-picking the old target
# leaves the game unchanged), and a second fixture, deflection-volley-second-flyer-still-miss.jsonl, pins that a legal new
# target with no gorge decision is still a miss (ChangeTargetsNoCandidateTest.legalNewTargetWithNoGorgeDecisionIsStillAMiss,
# 2 tests now). Driver-only; cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# 495ed200e9f = 0ca7a220bd0 (join18, above) + setup player_counters (xproof cluster xpt-a32b5388, Demon of Dark Schemes): SetupBuilder
# read life, speed, the commander fields and the zones but never a seat's player_counters, so a setup ENERGY=4 never reached the
# player and the {2}{B}, Pay {E}{E}{E}{E} ability was refused ("was not put on the stack (cost unpaid)") while gorge held the energy.
# SetupBuilder.placePlayerCounters now sets each entry as the COMPLETE count of its kind (ENERGY/POISON/EXPERIENCE/RAD, also 0) as
# recorded state with no event or trigger, and refuses an unknown, keyword or negative entry. PlayerCountersSetupTest (5 tests, red
# before) is run by scripts/forge-oracle-test-player-counters.sh (make forge-driver-tests). Driver-only; cardsfolder/tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-setup-player-counters.
# 19c49ba7117 = 0ca7a220bd0 + a "pN:ability:Name[#k]" target ref (xproof cluster xpt-1d873918, Azorius Guildmage's "{2}{U}: Counter
# target activated ability" aimed at Dark Sphere's ability, CR 113.3b / 602.2): StepMachine.targetObject resolved a target ref only to a
# player or a card, so the stack ability never bound to the TargetType$ Activated slot and the activation was refused ("was not put on
# the stack"). It now binds the k-th pending non-spell stack instance, oldest first (RefTable.nthPending), activated or triggered by that
# seat whose source card is the named card, wherever the source is now (Dark Sphere sacrificed itself). AbilityTargetRefTest replays the
# exported request (fixtures/abilitytarget; red without the branch: "was not put on the stack"), scripts/forge-oracle-test-ability-target-ref.sh
# (make forge-driver-tests) runs it. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432),
# ForcedDieRollTest, RefTableTest, DriverContractTest and ChangeTargetsConfirmTest re-run green. Fork branch fix/xp-forge-driver-ability-target-ref.
# d38946a0fd9 = oracle/fpin-1125-join-abilitytarget-playercounters: 19c49ba7117 (above) merged with 495ed200e9f (above), each a
# driver-only commit on 0ca7a220bd0 (RefTable/StepMachine/AbilityTargetRefTest vs SetupBuilder/PlayerCountersSetupTest), no textual
# conflicts (git merge-tree). AbilityTargetRefTest and PlayerCountersSetupTest run together at this pin; die-roll code, cardsfolder and
# tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch oracle/fpin-1125-join-abilitytarget-playercounters.
# f3065a4da86 = oracle/fpin-1125-join20: 495ed200e9f (above) merged with 53635f189cc (the unbound optional-untap election answered at
# the ask, CR 502.3/117.3d, xproof clusters xpt-4782660c/xpt-5133dae0, Whip Vine; ScriptedController/StepMachine/SnapshotWriter only), no
# textual conflicts (all three files auto-merged). UntapElectionTest runs at this pin via scripts/forge-oracle-test-untap-election.sh
# (make forge-driver-tests). Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# NOTE (merge): f3065a4da86 is the untap-election branch's pin but does not contain the ability-target ref (19c49ba7117); the pin stays at
# d38946a0fd9 until a fork join of 19c49ba7117 + 53635f189cc exists. UntapElectionTest (scripts/forge-oracle-test-untap-election.sh) needs that join.
# af167fe93c3 = 495ed200e9f (above) + the setup life is a state (xproof cluster xpt-57a0a28c, Angel of Vitality): SetupBuilder wrote a
# seat's life with Player.setLife after the battlefield was placed, and Forge turns a raise into gainLife and a drop into loseLife
# (CR 119.5), so Angel of Vitality's "if you would gain life, you gain that much life plus 1 instead" (CR 614) made a setup life of 25
# read 26 while gorge and XMage held 25. SetupBuilder.placeLife now writes every seat's life before the first permanent is placed,
# while no replacement effect or trigger exists. SetupLifeReplacementTest (4 tests, 3 red before) is run by
# scripts/forge-oracle-test-setup-life-replacement.sh (make forge-driver-tests). Driver-only; die-roll code, cardsfolder and
# tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-setup-life-state.
# 18e28a7af59 = oracle/fpin-1125-join-setup-life: d38946a0fd9 (above) merged with af167fe93c3 (the setup life is a state, xproof cluster
# xpt-57a0a28c, Angel of Vitality; SetupBuilder.placeLife writes every seat's life before the first permanent exists), no textual conflicts
# (the two sides touch disjoint files except SetupBuilder, which auto-merged). SetupLifeReplacementTest runs at this pin via
# scripts/forge-oracle-test-setup-life-replacement.sh (make forge-driver-tests). Driver-only; die-roll code, cardsfolder and tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch oracle/fpin-1125-join-setup-life.
# 64be1e6a7fe = oracle/fpin-1125-join-setup-life-untap: 18e28a7af59 (above) merged with f3065a4da86 (the unbound optional-untap election answered at
# the ask, 53635f189cc), closing the NOTE above: one pin now carries the ability-target ref, setup player_counters, the setup-life fix and the
# untap election, so every scripts/forge-oracle-test-*.sh (UntapElectionTest included) runs green in make forge-driver-tests. No textual
# conflicts beyond an auto-merge of StepMachine; driver-only, die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# 8765c682b2f = oracle/fpin-1125-join22: d38946a0fd9 (above) merged with a8da5c69719 (oracle/fpin-1125-join21 = join20 f3065a4da86,
# i.e. 495ed200e9f + the optional-untap election answer 53635f189c, merged with 9b233511947, the forced Hand->Library put-back),
# no textual conflicts (StepMachine/DecisionQueue/ScriptedController Hand->Library vs RefTable/SetupBuilder/StepMachine target ref;
# merge tree 027db51c983). ForcedDieRollTest, PlayerCountersSetupTest, AbilityTargetRefTest and the hand-library-placement replay run
# together at this pin. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# 30cbb3854d2 = oracle/fpin-1125-join-setup-life-handlib: 8765c682b2f (above, join22 incl. the untap election and the Hand->Library put-back)
# merged with af167fe93c3 (the setup life is a state, SetupBuilder.placeLife; xproof cluster xpt-57a0a28c, Angel of Vitality), no textual conflicts
# (SetupBuilder.java and SetupLifeReplacementTest.java only; 102 insertions). SetupLifeReplacementTest runs at this pin via
# scripts/forge-oracle-test-setup-life-replacement.sh next to the hand-library-placement and untap-election replays (make forge-driver-tests).
# Driver-only; die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432).
# 52ddaba3075 = 30cbb3854d2 + the keyword-expanded ability identity (xproof cluster xpt-1ea6e65f, Elvish Dreadlord and Rakshasa Debaser):
# gorge's keyword expansion of Encore is an `AB$ Encore | Keyword$ Encore` line, Forge derives the same graveyard ability from K:Encore as a
# CopyPermanent SpellAbility with the same keyword and cost (CR 702.141a), and StepMachine.abilityFor's keyword tier needed an equal API, so
# it fell to api+cost and stopped "ability identity at step N: api+cost 0". The keyword tier now falls back to the SAs derived from the
# keyword with the same cost when none shares the API. scripts/forge-oracle-test-encore-keyword-identity.sh (make forge-driver-tests)
# replays the four finds (cmd/oraclediff/testdata/forge/encore-keyword-identity.jsonl) and requires `identity: keyword` with no harness row.
# Driver-only (StepMachine); die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch
# fix/xp-forge-driver-encore-keyword-identity.
# efd75d00b34 = 52ddaba3075 + gorge's monarch trigger labels (xproof cluster xpt-457c5f66, Grave Venerations and Faramir, Steward of Gondor): at the
# monarch's end step gorge orders its own end-step trigger beside "The monarch: draw a card" (CR 603.3b, 725.2), fixed synthetic text that no Forge
# trigger text starts with (Forge hosts the monarch triggers on the "The Monarch" effect card, worded "At the beginning of your end step, draw a
# card."), so StepMachine.triggerScore scored 0 and the order answer missed. triggerScore now binds "The monarch: draw a card" to that host's Phase
# trigger and "The monarch: that creature's controller becomes the monarch" to its DamageDone trigger by role (beside the Evoke label binding).
# TriggerOrderMonarchLabelTest (run-unit-tests.sh) fails on 52ddaba3075 and passes here. Driver-only; die-roll code, cardsfolder and tokenscripts
# unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-monarch-trigger-label.
# 71fd77c15c0 = efd75d00b34 plus StepMachine.pickObjects/pickFrom checking a gorge object pick against min(min, options), as Forge's
# InputSelectEntitiesFromList clamps both bounds to the option count (xproof cluster xpt-8184f8d0, Foreboding Steamboat's "each player
# chooses two nontoken, non-Vehicle creatures they control" over one creature: gorge records the one pick it can make, CR 609.3, and the
# driver strict-missed "gorge picked 1, Forge wants 2..2"). ChooseFewerThanMinTest (run-unit-tests.sh) fails on efd75d00b34 and passes here;
# one pick over two candidates where two are required still misses. Driver-only; die-roll code, cardsfolder and tokenscripts unchanged
# (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-choose-fewer-than-min.
# bdad3bd38b1 = 71fd77c15c0 plus the values-list chooseNumber (RollDiceEffect ChosenSVar$, CR 706.2) answered from gorge's numeric choose_n
# when the pick is an offered value (StepMachine.chooseNumberFrom); it missed unconditionally (xproof clusters xpt-8a6bce20, xpt-c9c0e640,
# Valiant Endeavor "strict_miss: Missing number answer"). UnmappedAskTest's three Valiant Endeavor cases (forced rolls 3 and 6: pick 3 six
# Knights, pick 6 three, unoffered 4 still misses) fail on 71fd77c15c0 and pass here. Driver-only; die-roll forcing, cardsfolder and
# tokenscripts unchanged (same as FORGE_REF ff076d4432). Fork branch fix/xp-forge-driver-choose-number-values.
# 4385467a9eb = 483fcd27c71 (bdad3bd38b1 above, merged into fix/xp-forge-driver-choose-fewer-than-min) merged with gorge's FORGE_REF
# 5bc5851a54 (gorge-cond-fixes-20261125: Laboratory Drudge, Barrowin of Clan Undurr, Park Heights Pegasus check their conditions on
# resolution). gorge main moved FORGE_REF there on 11-25 and the pin invariant failed every xproof cycle; the merge was clean, and the
# cardsfolder/tokenscripts trees now equal FORGE_REF 5bc5851a54's. Driver code unchanged from bdad3bd38b1. Fork branch
# oracle/forge-pinfix-5bc5851a54. scripts/check-forge-ref-in-driver.sh now holds this in CI.
# 7b6ff5656c8 = 4385467a9eb plus the cost-shape fallback in StepMachine.abilityFor (xproof cluster xpt-d77c0bf0, Yuriko, the Tiger's Shadow):
# gorge's commander ninjutsu line costs `U B Return<1/Creature.YouCtrl+attacking+unblocked>`, Forge's two Ninjutsu SAs (hand and
# command zone, CR 702.49d, 903.8) cost `U B Return<1/Creature.attacking+unblocked/unblocked attacker>`; Cost.toSimpleString renders the
# Return part from its description text, so the keyword tier's cost filter dropped every hit and the api+cost tier stopped "ability
# identity at step 9: api+cost 0". The tiers now fall back, when no SA's rendering is equal, to the mana part plus the cost-part
# classes, and identity()'s param scoring (Origin$/ActivationZone$ Hand) picks the hand SA over the command-zone one.
# scripts/forge-oracle-test-ninjutsu-commander-identity.sh (make forge-driver-tests) replays the find
# (cmd/oraclediff/testdata/forge/ninjutsu-commander-identity.jsonl) and requires `identity: keyword` with no harness row; it fails on
# 4385467a9eb. Driver-only (StepMachine); die-roll code, cardsfolder and tokenscripts unchanged (same as FORGE_REF 5bc5851a54).
# Fork branch fix/xp-forge-driver-ninjutsu-commander-identity.
# 1dea7b45ce0 = 4385467a9eb plus the unpinned order-to-Exile ask kept (StepMachine.UNOBSERVABLE_ORDER_ZONES gains Exile, beside
# Graveyard/Battlefield/Hand; both snapshots sort an exile's names, so forge_order.go can never derive a pin and Flickerform's
# exile-the-bearer-and-itself resolution strict-missed, xproof cluster xpt-1d42dce4 xs:XP:flickerform:f0a0:s1). The library and
# every other zone still strict-miss. OrderCardsTest.unpinnedExileOrderKeepsForgeOrder (run-unit-tests.sh) is red on 4385467a9eb
# and green here; unpinnedOtherZoneStillLooseMisses now asserts Library instead of Exile. Driver-only; die-roll forcing,
# cardsfolder and tokenscripts unchanged. Fork branch oracle-driver.
# 3526a90f719 = 1dea7b45ce0 plus the unpinned order-to-Command ask kept (UNOBSERVABLE_ORDER_ZONES gains Command; both snapshots
# sort a command zone's names -- Forge's SnapshotWriter.commandZone Collections.sort, gorge's digest "sorted; Commander games" --
# so forge_order.go can never derive a pin there either, and ScriptedController builds the "order to " + destinationZone ask for
# every destination zone). No scripted card is known to order to Command yet (corpus check: no xproof cluster or scenario names
# it), so this is latent; OrderCardsTest.unpinnedCommandOrderKeepsForgeOrder (run-unit-tests.sh) is red on 1dea7b45ce0 and green
# here, and the library and every other zone still strict-miss. Driver-only; die-roll forcing, cardsfolder and tokenscripts
# unchanged. Fork branch oracle-driver.
# 5ceed32d687 = the merge of 7b6ff5656c8 and 1dea7b45ce0 above (siblings on 4385467a9eb, textually clean, no conflicts): carries both the
# commander-ninjutsu cost-shape fallback and the unpinned order-to-Exile ask. Fork branch fix/xp-forge-driver-ninjutsu-commander-identity.
# edf8473c4d6 = join23 on the fork (oracle/fpin-1126-join23): 5ceed32d687 merged with 3526a90f719, so the pin carries the Command-order
# gate, the ninjutsu cost-shape fallback and the unpinned order-to-Exile ask together. None of the three is on the die-roll path;
# OrderCardsTest (incl. unpinnedCommandOrderKeepsForgeOrder) and ForcedDieRollTest were re-run green on the merged pin
# (run-unit-tests.sh with FORGE_ORACLE_DIR=forgeoracle-1120 -- the default env's forge-game jar (10-29) predates the record
# GameEventRollDie and does not compile any 11-2x pin, this one included).
FORGE_ORACLE_REF ?= edf8473c4d64d76ccd35280b666aaa2257b5eff5
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

# The gorge pin (docs/PINNING.md): move GORGE_PIN and go.mod to REV (default gorge main).
# .github/workflows/gorge-pin-follow.yml runs the same script every 15 minutes.
.PHONY: gorge-pin-bump
gorge-pin-bump:
	scripts/gorge-pin.sh bump $(or $(REV),main)

.PHONY: help
help:
	@echo "gorge-compliance targets:"
	@echo "  make cards [GORGE_ROOT=../gorge] — link a gorge checkout's .cards corpus here (run gorge's make fetch-cards compile-cards first)"
	@echo "  make test           — go test ./... (capped like gorge's)"
	@echo "  make test-one PKG=./p RUN=TestX — one capped, corpus-checked test run with one verdict line"
	@echo "  make test-targeted [BASE=develop] — the packages the diff touches (including uncommitted work); the inner loop"
	@echo "  make test-smart [BASE=develop] — the precheck on a committed tree: pins, licence guards, smokes, affected tests"
	@echo "  make precheck       — an alias for test-smart"
	@echo "  make test-select BASE=ref — print the Go test packages the touched-package gate selects for BASE..HEAD"
	@echo "  make pr [PR_BASE=main] [DRY=1] — open/refresh a GitHub PR with a generated change overview"
	@echo "  make gorge-pin-bump [REV=main] — move GORGE_PIN and go.mod to a pushed gorge commit (docs/PINNING.md)"
	@echo "  make boundary       — the licence guards (no Forge content is committed)"
	@echo "  make carddb-test    — the carddb tool's Python tests (cmd/carddb/py/tests; git + python3 only, no network)"
	@echo "  make oracle-audit   — the hand Oracle-text audit + XMage ports (audit/; ORACLE_RUN=<Card> to filter)"
	@echo "  make padseats       — replay both scenario corpora at four seats and check scenarios/padseats.tsv"
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

# The test targets (scripts/test-targets.sh) replace a raw `go test`: capped,
# corpus-checked, one verdict line `test-<mode>: PASS|FAIL|ENV|EMPTY|BUDGET
# <sha> <pkgs> <seconds>`. BASE is the ref the change merges into here: tickets
# land on local develop, not by PR to it.
#   test-one       PKG=./p RUN=TestX [SKIP= V=1 TIMEOUT=2m TAGS=a,b]: debugging
#   test-targeted  the packages the diff vs BASE touches (working tree and
#                  untracked files included), via cmd/testselect: the inner loop.
#                  A pin diff (every package) is ENV, not the full suite; the one
#                  slow package runs a named guard only.
#   test-smart     the precheck on a committed tree, fail-fast: gofmt/vet, shell
#                  syntax, fork pins, the licence guards, the script smokes, then
#                  the affected packages; precheck is an alias; same pin/slow rules
BASE ?= develop
export BASE
.PHONY: test-one test-targeted test-smart precheck test-list
test-one: export PKG := $(PKG)
# RUN/SKIP go to the recipe through $(value ...), verbatim: a command-line RUN re-expands on export and make eats
# `$|` / `$)` in it (gorge 8117b8ec09; '^TestA$|^TestB$' became '^TestA^TestB$' and matched nothing).
test-one: export V := $(V)
test-one: export TIMEOUT := $(TIMEOUT)
test-one: export TAGS := $(TAGS)
test-one:
	@RUN='$(value RUN)' SKIP='$(value SKIP)' bash scripts/test-targets.sh one

test-targeted:
	@bash scripts/test-targets.sh targeted

test-smart:
	@bash scripts/test-targets.sh smart

# test-list prints test-targeted's selection; useful when a diff seems to select
# the wrong packages.
test-list:
	@bash scripts/test-targets.sh list

precheck: test-smart

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
	systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% env -C followers/phase CARGO_HOME=$(FOLLOWERS_CACHE)/cargo-home RUSTC=$(dir $(CARGO))rustc RUSTDOC=$(dir $(CARGO))rustdoc CARGO_TARGET_DIR=$(PHASE_ORACLE_DIR)/target PHASE_CARD_DATA=$(PHASE_ORACLE_DIR)/card-data-$(shell cut -d' ' -f2 followers/phase/PHASE_PIN).json RUST_MIN_STACK=16777216 $(CARGO) test --locked --profile follower $(RUN)

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

# precheck is the seat/merge precheck and an alias for test-smart: the fork
# pins (every pin a kit build fetches from adams-shaun/forge or adams-shaun/mage
# must already be pushed there), the licence guards, the script smokes, and the
# affected packages, fail-fast on a committed tree. CI's lint job calls
# scripts/check-fork-pins.sh directly because it has no .cards corpus; the test
# job is what runs the full `go test` gate.
#
# boundary is the licence guard on its own (the same step test-smart runs).
# It runs through scripts/test-targets.sh whole — the same helper test-one
# uses — so it works inside a seat: the seat go shim blocks a raw `go test`
# whose process ancestry lacks that script, and a recipe cannot self-declare
# the marker the shim accepts (only the script sets it legitimately). The
# whole mode carries the recipe's -count=1 cache bypass, so the guard still
# re-runs rather than replaying the test cache.
boundary:
	@PKG=./internal/boundary/ bash scripts/test-targets.sh whole

# carddb-test runs the Python carddb tool's tests (put, publish, merge driver, build, fetch);
# CARDDB_UPDATE_GOLDEN=1 rewrites cmd/carddb/py/tests/golden after a deliberate schema change.
.PHONY: carddb-test
carddb-test:
	python3 -I -m unittest discover -s cmd/carddb/py/tests -t cmd/carddb/py

# oracle-audit runs both scenario corpora through gorge's runner: the hand
# Oracle-text audit (scenarios/oracle plus gorge's residual
# engine/rules/testdata/oracle) and the XMage ports (scenarios/xmageport).
# ORACLE_RUN filters the audit per card, e.g. ORACLE_RUN='Fatal_Push'. It runs
# through scripts/test-targets.sh whole — the only seat-permitted `go test`
# path — which carries the -count=1 cache bypass and caps the run itself; V=1
# keeps the verbose stream, saved under .oracle-build/test/whole-*.json.
ORACLE_RUN ?=
oracle-audit:
	@PKG=./audit RUN='TestOracleScenarioFilesWellFormed|TestOracleAudit/$(ORACLE_RUN)|TestXMagePortScenarios' V=1 bash scripts/test-targets.sh whole

# padseats replays both scenario corpora with two inert seats added (gorge only,
# no JVM) and checks scenarios/padseats.tsv: the full TSV goes under
# .oracle-build/, the summary and any mismatched row to stderr. A command, not
# part of the test targets.
.PHONY: padseats
padseats:
	@go run ./cmd/padseats -out .oracle-build/padseats.tsv

smoke:
	bash ops/scripts/tests/make_cards_smoke.sh
	bash ops/scripts/tests/forge_pass_smoke.sh
	bash ops/scripts/tests/forge_oracle_pin_smoke.sh
	bash ops/scripts/tests/check_fork_pins_smoke.sh
	bash ops/scripts/tests/check_forge_ref_in_driver_smoke.sh
	bash ops/scripts/tests/gorge_pin_check_smoke.sh
	bash ops/scripts/tests/forge_driver_follow_smoke.sh
	bash ops/scripts/tests/forge_sbv2_smoke.sh
	bash ops/scripts/tests/xmage_sbv2_smoke.sh
	bash ops/scripts/tests/phase_oracle_pin_smoke.sh
	bash ops/scripts/tests/phase_sbv2_smoke.sh
	bash ops/scripts/tests/manabrew_oracle_pin_smoke.sh
	bash ops/scripts/tests/manabrew_oracle_run_smoke.sh
	bash ops/scripts/tests/oracle_warm_smoke.sh
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
	@PKG=./oracle/mtgish RUN=TestMtgishLedger V=1 bash scripts/test-targets.sh whole

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
# ARGS='--set FDN --setup-only --pool FILE [--require-mana] --limit N --min-agree PCT'.
# --pool FILE filters; a pool starting '#!require-mana' (oracle/xengine/pools/
# mana-abilities.txt) or --require-mana also activates and requires every named mana source.
.PHONY: follower-check
follower-check:
	scripts/follower-check.sh $(ENGINE) $(ARGS)

# follower-sweep-smoke runs one smoke-sized xengine chunk with one or several followers (ENGINE='phase manabrew'), the
# pool narrowed by the follower's preflight (scripts/follower-sweep-smoke.sh):
# ENGINE=phase [GAMES=2] [ARGS='--pool F --max-class harness=50'].
.PHONY: follower-sweep-smoke
follower-sweep-smoke:
	scripts/follower-sweep-smoke.sh "$(ENGINE)" --games $(or $(GAMES),2) $(ARGS)
