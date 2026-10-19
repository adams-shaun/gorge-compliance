SHELL := /bin/bash

# gorge-compliance builds against gorge: go.mod's replace points at ../gorge,
# and a local (gitignored) go.work may point at a gorge worktree instead.
# GORGE_ROOT is that checkout, for the corpus pin in gorge's Makefile.
# The gorge revision the coverage targets are for is GORGE_PIN (docs/PINNING.md).
GORGE_ROOT ?= $(shell go list -m -f '{{.Dir}}' github.com/adams-shaun/gorge)

export GOMEMLIMIT ?= 5GiB
export GOFLAGS ?= -p=2 -trimpath
GO_TEST_FLAGS ?= -p=2

# The XMage commit the compliance oracle (manifests, out-of-tree driver)
# is pinned to. XMage is MIT; nothing from it is a build dependency.
# docs/design/2026-10-02-xmage-compliance-oracle-design.md
XMAGE_REF  ?= 6b602a1c85e8ed738a4b40b1ef44c54845b57f68
XMAGE_ORACLE_DIR ?= /mnt/sata/gorge-training/xmageoracle
# gorge's corpus pin and Forge fork, read from gorge's Makefile.
FORGE_REPO ?= $(shell awk '/^FORGE_REPO/ {print $$3}' $(GORGE_ROOT)/Makefile)
FORGE_REF  ?= $(shell awk '/^FORGE_REF/ {print $$3}' $(GORGE_ROOT)/Makefile)
# Forge oracle driver commit (fork, branch oracle-harness): a descendant of FORGE_REF with identical script trees.
# da1c712c3 = e27d2bdb2 + fork gorge 098b05531 (restores the pin invariant, 2026-10-13).
FORGE_ORACLE_REF ?= da1c712c31abec3063b0dbf3b517365f18951f89
FORGE_ORACLE_DIR ?= /mnt/sata/gorge-training/forgeoracle

.PHONY: help
help:
	@echo "gorge-compliance targets:"
	@echo "  make cards          — link gorge's .cards corpus here (run gorge's make fetch-cards compile-cards first)"
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
	@echo "  make xmage-driver-tests — the Java driver's unit tests (needs xmage-oracle-setup)"

.PHONY: cards test boundary oracle-audit smoke
cards:
	@test -d "$(GORGE_ROOT)/.cards" || { echo "no corpus at $(GORGE_ROOT)/.cards: run gorge's make fetch-cards compile-cards" >&2; exit 1; }
	ln -sfn "$(GORGE_ROOT)/.cards" .cards

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
	bash ops/scripts/tests/forge_pass_smoke.sh
	bash ops/scripts/tests/forge_oracle_pin_smoke.sh

# xmage-driver-tests compiles the Java replay driver and runs its unit tests
# (oracle/xmageoracle/test) against the xmage-oracle-setup build. Needs Java
# from that build; no game, no H2.
XMAGE_DRIVER_OUT ?= $(CURDIR)/.coverage/xmage-driver
.PHONY: xmage-driver-tests
xmage-driver-tests:
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
# over SETS. Needs the xmage-oracle-setup build. HEAVY_LOCK, when set, names
# the box's heavy-job lock (scripts/heavy-lock.sh).
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
