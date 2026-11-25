#!/usr/bin/env bash
# check-fork-pins.sh — refuse a fork pin whose commit was never pushed.
#
# A kit build fetches FORGE_ORACLE_REF from adams-shaun/forge and XMAGE_REF from
# adams-shaun/mage. A seat that pins a commit made only in its local clone gets
# "upload-pack: not our ref <sha>" on every build and takes the build boxes down,
# so the pin is checked against the fork before it merges.
#
# Each pin comes from the Makefile, but an environment value wins, so a caller
# can check a candidate before bumping:
#   FORGE_ORACLE_REF=<sha> bash scripts/check-fork-pins.sh
#
# Exit: 0 every pin is on its fork; 1 a pin is missing (or the API returned a
# different commit); 2 usage/environment (no gh, no pin, not a full SHA).
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
cd "$here"

die() {
	echo "check-fork-pins: $*" >&2
	exit 2
}

command -v gh >/dev/null 2>&1 || die "gh is needed to check fork pins"
[ -f Makefile ] || die "no Makefile at $here"

# Every pin whose commit a build fetches from one of our forks belongs here;
# the loop below is the single check they all go through, so covering the next
# fork pin is one row, not a new branch. FORGE_REF, XMAGE_SBV2_REF and the
# phase/mtgish pins are upstream commits and deliberately absent.
pins=(
	"FORGE_ORACLE_REF adams-shaun/forge"
	"XMAGE_REF adams-shaun/mage"
)

pin_from_makefile() {
	local name=$1 value
	value=$(awk -v name="$name" \
		'$1 == name && $2 == "?=" { v = $3; sub(/#.*/, "", v); print v; exit }' Makefile)
	[ -n "$value" ] || die "Makefile defines no $name"
	printf '%s\n' "$value"
}

failed=0
for entry in "${pins[@]}"; do
	read -r name fork <<<"$entry"
	sha=${!name:-$(pin_from_makefile "$name")}
	[[ $sha =~ ^[0-9a-f]{40}$ ]] || die "pin $name is not a full commit sha: '$sha'"

	if got=$(gh api "repos/$fork/commits/$sha" --jq .sha) && [ "$got" = "$sha" ]; then
		echo "fork pin $name $sha is on $fork"
	else
		echo "fork pin $name $sha is not on $fork: push it first" >&2
		failed=1
	fi
done

exit "$failed"
