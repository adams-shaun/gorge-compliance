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
XMAGE_REF  ?= b964ce92f0274faef1c3b2395b74a8f91c5bcf61
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
MTGISH_REF ?= 153451e44d88baabfeb9902092032c328f4d5416
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
# 19b0c2902c5 = c92f40d4f49 + the scripted controller pays a Phyrexian pip with 2 life (CR 107.4f, 118.3b) when gorge
# recorded a pay_life pick: applyManaToCost takes the step's pay_life picks first (payPhyrexian + payLife(2)), then the
# recorded spend and the pool; a pip with no pay_life pick stays a mana pip. Before, Vault Skirge's {B/P} stayed unpaid and the
# cast was refused. PhyrexianLifePaymentTest covers Vault Skirge and Immolating Souleater. Driver-only change; same trees as
# FORGE_REF ff076d4432. Fork branch fix/xp-forge-driver-phyrexian-life.
FORGE_ORACLE_REF ?= 19b0c2902c5deaf698f85b6bf59dc2084c736cc9
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
