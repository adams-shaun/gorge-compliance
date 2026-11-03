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
XMAGE_REF  ?= 6b602a1c85e8ed738a4b40b1ef44c54845b57f68
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
# Forge oracle driver commit (fork, branch oracle-harness): a descendant of FORGE_REF with identical script trees.
# f8682f9e = the driver merged with gorge's FORGE_REF 07d4f918d (Card-Forge master 2026-11-02).
FORGE_ORACLE_REF ?= f8682f9e54ea513bc30ba9a1fc5aeece8cf58837
FORGE_ORACLE_DIR ?= .oracle-build/forge

.PHONY: help
help:
	@echo "gorge-compliance targets:"
	@echo "  make cards [GORGE_ROOT=../gorge] — link a gorge checkout's .cards corpus here (run gorge's make fetch-cards compile-cards first)"
	@echo "  make test           — go test ./... (capped like gorge's)"
	@echo "  make pr [PR_BASE=main] [DRY=1] — open/refresh a GitHub PR with a generated change overview"
	@echo "  make boundary       — the licence guards (no Forge content is committed)"
	@echo "  make oracle-audit   — the hand Oracle-text audit + XMage ports (audit/; ORACLE_RUN=<Card> to filter)"
	@echo "  make compliance-status — per-set/per-format compliance status (oracle/compliance/status.md, never committed) + ratchet check"
	@echo "  make compliance-sets — both levels over every printed set -> .coverage/compliance-sets.md"
	@echo "  make compliance-manifests — regenerate oracle/compliance/manifests from XMage set classes at XMAGE_REF"
	@echo "  make xmage-oracle-setup — build XMage at XMAGE_REF out of tree (heavy; run alone)"
	@echo "  make forge-oracle-setup — build the Forge oracle driver at FORGE_ORACLE_REF out of tree (heavy)"
	@echo "  make compliance-pass SETS='FRA BLB' — XMage compliance pass over the sets; reruns only stale verdicts"
	@echo "  make smoke          — the script smokes (stubs only; no Java, no network)"
	@echo "  make mtgish-fetch   — download the pinned mtgish data into .mtgish/<ref>/ (gitignored) and verify its sha256"
	@echo "  make mtgish-xc      — the mtgish card-definition cross-check: corpus report + the corpus ledger ratchet"
	@echo "  make xmage-driver-tests — the Java driver's unit tests (needs xmage-oracle-setup)"

.PHONY: cards test boundary oracle-audit smoke
cards:
	@GORGE_ROOT='$(GORGE_ROOT)' FORGE_REF='$(FORGE_REF)' scripts/link-cards.sh

test:
	go test $(GO_TEST_FLAGS) ./...

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
	bash ops/scripts/tests/xengine_sweep_smoke.sh
	bash ops/scripts/tests/xengine_random_smoke.sh

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
	for t in scripts/xmage-oracle-test-*.sh; do echo "== $$t"; XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) bash "$$t" "$(XMAGE_DRIVER_OUT)/$$(basename $$t .sh)" || exit 1; done

.PHONY: compliance-manifests
compliance-manifests:
	go run ./cmd/compliance manifest -xmage $(XMAGE_ORACLE_DIR)/sets-only -ref $(XMAGE_REF) -out oracle/compliance/manifests

.PHONY: xmage-oracle-setup forge-oracle-setup
xmage-oracle-setup:
	XMAGE_REF=$(XMAGE_REF) XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) scripts/xmage-oracle-setup.sh
forge-oracle-setup:
	FORGE_REPO='$(FORGE_REPO)' FORGE_REF='$(FORGE_REF)' FORGE_ORACLE_REF='$(FORGE_ORACLE_REF)' FORGE_ORACLE_DIR='$(FORGE_ORACLE_DIR)' scripts/forge-oracle-setup.sh

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
