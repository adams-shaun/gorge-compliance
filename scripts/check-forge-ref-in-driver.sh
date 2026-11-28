#!/usr/bin/env bash
# check-forge-ref-in-driver.sh — refuse a tree whose gorge FORGE_REF is not
# contained in FORGE_ORACLE_REF (the Forge oracle driver).
#
# scripts/forge-oracle-setup.sh enforces the same pin invariant on the build
# box, but only at kit build time: on 2026-11-25 gorge moved FORGE_REF to
# 5bc5851a54, gorge-pin-follow carried it into develop, and every xproof cycle
# then died in forge-oracle-setup for hours. This check runs in CI (the lint
# job) and in `make smoke` instead, so a GORGE_PIN bump that brings a FORGE_REF
# the driver lacks goes red on its PR. The fix is in the fork: merge the commit
# gorge pinned into the driver, bump FORGE_ORACLE_REF here, then the pin bump.
#
# FORGE_REF comes from the environment, else gorge's Makefile in $GORGE_DIR
# when set, else gorge's Makefile at GORGE_PIN read through the GitHub contents
# API (what develop ships, whatever gorge checkout CI or a seat has). FORGE_ORACLE_REF comes from the environment,
# else this repo's Makefile. The fork compare API answers
# compare/<driver>...<FORGE_REF>: `behind` or `identical` means FORGE_REF is an
# ancestor of the driver.
#
# Exit: 0 contained; 1 not contained (or unknown to the fork); 2 usage or
# environment (no gh, no pin, the API could not answer).
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
cd "$here"
fork=${FORGE_FORK:-adams-shaun/forge}

die() {
	echo "check-forge-ref-in-driver: $*" >&2
	exit 2
}

command -v gh >/dev/null 2>&1 || die "gh is needed"

mkpin() { awk -v n="$1" '!seen && $1 == n && ($2 == "?=" || $2 == "=") { v = $3; sub(/#.*/, "", v); print v; seen = 1 }'; }

oracle=${FORGE_ORACLE_REF:-}
[ -n "$oracle" ] || oracle=$(mkpin FORGE_ORACLE_REF <Makefile)
[[ $oracle =~ ^[0-9a-f]{40}$ ]] || die "FORGE_ORACLE_REF is not a full commit sha: '$oracle'"

forge_ref=${FORGE_REF:-}
src="FORGE_REF from the environment"
if [ -z "$forge_ref" ] && [ -n "${GORGE_DIR:-}" ]; then
	forge_ref=$(mkpin FORGE_REF <"$GORGE_DIR/Makefile")
	src="gorge Makefile in $GORGE_DIR"
fi
if [ -z "$forge_ref" ]; then
	[ -f GORGE_PIN ] || die "no FORGE_REF, no GORGE_DIR and no GORGE_PIN"
	pin=$(head -n1 GORGE_PIN | awk '{print $1}')
	content=$(gh api "repos/adams-shaun/gorge/contents/Makefile?ref=$pin" --jq .content) ||
		die "cannot read gorge's Makefile at GORGE_PIN $pin"
	forge_ref=$(printf '%s' "$content" | tr -d '\n' | base64 -d | mkpin FORGE_REF)
	src="gorge Makefile at GORGE_PIN ${pin:0:10}"
fi
[[ $forge_ref =~ ^[0-9a-f]{40}$ ]] || die "FORGE_REF ($src) is not a full commit sha: '$forge_ref'"

if [ "$forge_ref" = "$oracle" ]; then
	status=identical
else
	errf=$(mktemp "${TMPDIR:-/tmp}/check-forge-ref.XXXXXX")
	trap 'rm -f "$errf"' EXIT
	if ! status=$(gh api "repos/$fork/compare/$oracle...$forge_ref" --jq .status 2>"$errf"); then
		cat "$errf" >&2
		if /usr/bin/grep -Eq 'HTTP (404|422)|No commit found' "$errf"; then
			echo "FORGE_REF ${forge_ref:0:10} or FORGE_ORACLE_REF ${oracle:0:10} is not in the $fork network" >&2
			exit 1
		fi
		die "the GitHub compare API could not answer"
	fi
fi

case $status in
behind | identical)
	echo "FORGE_REF ${forge_ref:0:10} ($src) is contained in FORGE_ORACLE_REF ${oracle:0:10}"
	;;
*)
	echo "FORGE_REF ${forge_ref:0:10} ($src) is not contained in FORGE_ORACLE_REF ${oracle:0:10} (compare: $status)." >&2
	echo "Merge ${forge_ref:0:10} into the driver on $fork, bump FORGE_ORACLE_REF to the merge, then land this pin bump." >&2
	exit 1
	;;
esac
