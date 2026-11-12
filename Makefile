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
XMAGE_REF  ?= 4142596fb72b9dc03a0eae0ec34f4544cfd6d59e
# The out-of-tree oracle builds default to gitignored directories here. They run
# to several GB, so point these at a shared disk to reuse one build across clones.
XMAGE_ORACLE_DIR ?= .oracle-build/xmage
# The mtgish commit the card-definition cross-check reads its data at
# (oracle/mtgish/MTGISH.lock holds the same ref and the files' sha256).
# mtgish is MIT; its data carries Oracle text and is never committed.
MTGISH_REF ?= 153451e44d88baabfeb9902092032c328f4d5416
# gorge's corpus pin and Forge fork, read from gorge's Makefile.
FORGE_REPO ?= $(shell awk '/^FORGE_REPO/ {print $$3}' $(GORGE_MODULE_DIR)/Makefile 2>/dev/null)
FORGE_REF  ?= $(shell awk '/^FORGE_REF/ {print $$3}' $(GORGE_MODULE_DIR)/Makefile 2>/dev/null)
# Forge oracle driver commit (fork, branch oracle-harness-trek7): a descendant of FORGE_REF with identical script trees.
# 54a3777c23 = 0e200f1410 merged with FORGE_REF 418ba5215f (gorge-trek7); 0e200f1410 alone differed from it in 5 Trek scripts.
# 0e200f1410 = baa2c42320 + a second Game.onCleanupPhase pass so setup life lost (player life below 20) does not roll into lifeLostLastTurn, and every other last-turn mirror starts empty too (First Response, Paladin of Atonement, Feast on the Fallen). baa2c42320 made setup counters put and setup life gained not seed per-turn history (Lasting Tarfire, Vanguard Seraph). Keeping FORGE_REF's cardsfolder and tokenscripts trees.
FORGE_ORACLE_REF ?= 54a3777c237672e220425ef8f66955d0a017a71c
FORGE_ORACLE_DIR ?= .oracle-build/forge

# Out-of-tree follower builds (Manabrew, ...) live under one cache directory.
FOLLOWERS_CACHE ?= $(or $(XDG_CACHE_HOME),$(HOME)/.cache)/gorge-compliance
# Manabrew (AGPL-3.0, a Rust port of Forge's engine): upstream until the operator creates the fork
# https://github.com/adams-shaun/manabrew; driver commits sit on branch `oracle` of the local clone.
MANABREW_REPO ?= https://github.com/witchesofthehill/manabrew
# 3e13fbe42a53 = upstream dadee731 + oracle patches (place_for_setup, take_state_edit, diagnostics sink, profile.oracle) + the manabrew-oracle driver crate: CLI and preflight (local branch oracle; pushed to adams-shaun/manabrew by the operator)
MANABREW_ORACLE_REF ?= 3e13fbe42a53a087b9f30ae94fec494ca2863d45
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
	@echo "  make oracle-audit   — the hand Oracle-text audit + XMage ports (audit/; ORACLE_RUN=<Card> to filter)"
	@echo "  make compliance-status — per-set/per-format compliance status (oracle/compliance/status.md, never committed) + ratchet check"
	@echo "  make compliance-sets — both levels over every printed set -> .coverage/compliance-sets.md"
	@echo "  make compliance-manifests — regenerate oracle/compliance/manifests from XMage set classes at XMAGE_REF"
	@echo "  make xmage-oracle-setup — build XMage at XMAGE_REF out of tree (heavy; run alone)"
	@echo "  make forge-oracle-setup — build the Forge oracle driver at FORGE_ORACLE_REF out of tree (heavy)"
	@echo "  make manabrew-oracle-setup — build Manabrew at MANABREW_ORACLE_REF and its card archive from gorge's FORGE_REF scripts, out of tree (heavy)"
	@echo "  make manabrew-engine-test [RUN=filter] — run the Manabrew engine's lib tests in the MANABREW_ORACLE_REF build tree, capped (heavy)"
	@echo "  make manabrew-oracle-test RUN=filter — cargo test for the manabrew-oracle driver crate in the MANABREW_ORACLE_REF build tree, capped (heavy)"
	@echo "  make manabrew-preflight POOL=FILE — which pool cards Manabrew would silently improvise (builds the driver; prints NAME<TAB>REASON, then 'refused K of P')"
	@echo "  make compliance-pass SETS='FRA BLB' — XMage compliance pass over the sets; reruns only stale verdicts"
	@echo "  make follower-check ENGINE=phase [ARGS='--setup-only --pool F'] — level-A scenarios through one follower (scripts/follower-check.sh)"
	@echo "  make follower-sweep-smoke ENGINE=phase [GAMES=2] — one xengine chunk with one follower, pool narrowed by its preflight (scripts/follower-sweep-smoke.sh)"
	@echo "  make phase-oracle-setup — build the phase.rs follower and fetch its card data at followers/phase/PHASE_PIN, out of tree (heavy; one cold build per pin)"
	@echo "  make phase-follower-test [RUN=filter] — cargo test for the phase.rs follower (needs phase-oracle-setup; --profile follower, PHASE_CARD_DATA set)"
	@echo "  make phase-preflight POOL=FILE — which pool cards phase.rs lacks (rebuilds the follower; prints NAME<TAB>REASON, then 'refused K of P')"
	@echo "  make smoke          — the script smokes (stubs only; no Java, no network)"
	@echo "  make mtgish-fetch   — download the pinned mtgish data into .mtgish/<ref>/ (gitignored) and verify its sha256"
	@echo "  make mtgish-xc      — the mtgish card-definition cross-check: corpus report + the corpus ledger ratchet"
	@echo "  make follower-proto-test [RUN=name] — cargo test for the engine-neutral follower crate (followers/proto; output under FOLLOWERS_CACHE)"
	@echo "  make xmage-driver-tests — the Java driver's unit tests (needs xmage-oracle-setup)"

.PHONY: cards test boundary oracle-audit smoke follower-proto-test phase-oracle-setup phase-follower-test phase-preflight
cards:
	@GORGE_ROOT='$(GORGE_ROOT)' FORGE_REF='$(FORGE_REF)' scripts/link-cards.sh

test:
	go test $(GO_TEST_FLAGS) ./...

# test-one: PKG=./path RUN='^TestX$' [SKIP=re] [V=1] [TIMEOUT=2m] [TAGS=a,b]
# (scripts/test-targets.sh one; the last stdout line is the verdict).
.PHONY: test-one
test-one: export PKG := $(PKG)
test-one: export RUN := $(RUN)
test-one: export SKIP := $(SKIP)
test-one: export V := $(V)
test-one: export TIMEOUT := $(TIMEOUT)
test-one: export TAGS := $(TAGS)
test-one:
	@bash scripts/test-targets.sh one

follower-proto-test:
	systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env CARGO_TARGET_DIR=$(FOLLOWERS_CACHE)/target/proto $(CARGO) test --locked --manifest-path followers/proto/Cargo.toml $(RUN)

phase-oracle-setup:
	FOLLOWERS_CACHE='$(FOLLOWERS_CACHE)' PHASE_ORACLE_DIR='$(PHASE_ORACLE_DIR)' CARGO='$(CARGO)' scripts/phase-oracle-setup.sh

# The follower profile is used for tests too, so the engine is built once, not again in debug. Cargo
# runs inside followers/phase (env -C) so its rust-toolchain.toml selects the nightly.
phase-follower-test:
	systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% env -C followers/phase CARGO_HOME=$(FOLLOWERS_CACHE)/cargo-home RUSTC=$(dir $(CARGO))rustc CARGO_TARGET_DIR=$(PHASE_ORACLE_DIR)/target PHASE_CARD_DATA=$(PHASE_ORACLE_DIR)/card-data-$(shell cut -d' ' -f2 followers/phase/PHASE_PIN).json RUST_MIN_STACK=16777216 $(CARGO) test --locked --profile follower $(RUN)

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
	go test -count=1 ./internal/boundary/

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
	bash ops/scripts/tests/phase_oracle_pin_smoke.sh
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
# from that build; no game, no H2.
XMAGE_DRIVER_OUT ?= $(CURDIR)/.coverage/xmage-driver
.PHONY: xmage-driver-tests
xmage-driver-tests:
	@test -s "$(XMAGE_ORACLE_DIR)/tests.cp" || { echo "no XMage build at $(XMAGE_ORACLE_DIR): run make xmage-oracle-setup, or set XMAGE_ORACLE_DIR" >&2; exit 1; }
	for t in scripts/xmage-oracle-test-*.sh scripts/forge-oracle-test-*.sh; do echo "== $$t"; XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) bash "$$t" "$(XMAGE_DRIVER_OUT)/$$(basename $$t .sh)" || exit 1; done

.PHONY: compliance-manifests
compliance-manifests:
	go run ./cmd/compliance manifest -xmage $(XMAGE_ORACLE_DIR)/sets-only -ref $(XMAGE_REF) -out oracle/compliance/manifests

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

# follower-sweep-smoke runs one smoke-sized xengine chunk with one follower, the
# pool narrowed by the follower's preflight (scripts/follower-sweep-smoke.sh):
# ENGINE=phase [GAMES=2] [ARGS='--pool F --max-class harness=50'].
.PHONY: follower-sweep-smoke
follower-sweep-smoke:
	scripts/follower-sweep-smoke.sh $(ENGINE) --games $(or $(GAMES),2) $(ARGS)
