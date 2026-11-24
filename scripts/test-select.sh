#!/usr/bin/env bash
# test-select.sh BASE [HEAD] — print the Go test packages a pre-merge gate
# must run for the files this branch changed since BASE, one per line. BASE is
# any ref; the script diffs its merge-base with HEAD, so a moved BASE does not
# make another branch's commits look like this branch's changes.
#
# This is the selection the agentctl "go test (touched)" gate uses. It is a
# tracked helper so the rule is unit-tested (internal/testselect) instead of
# living inline in the operator's .agentctl/config.toml:
#
#   scripts/test-select.sh develop
#
# An oraclegen or XMage-driver change selects the packages that emit the
# merge gate's GORGE-CERT-RESULT ratchet verdict (oracle/compliance/adopt and
# oracle/compliance/gate today), because those are the tests that replay the
# committed verdicts the change re-hashes. See internal/testselect.
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
base=${1:-}
head=${2:-HEAD}
if [ -z "$base" ]; then
	echo "test-select: usage: scripts/test-select.sh BASE [HEAD]" >&2
	exit 2
fi

cd "$repo"
mb=$(git merge-base "$base" "$head") || exit 2
exec env GOWORK=off go run ./cmd/testselect -root . -base "$mb" -head "$head"
