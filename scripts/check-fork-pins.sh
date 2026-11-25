#!/usr/bin/env bash
# check-fork-pins.sh — refuse a fork pin whose commit was never pushed, or one
# that moves backward/diverges from the base branch's pin.
#
# A kit build fetches FORGE_ORACLE_REF from adams-shaun/forge and XMAGE_REF from
# adams-shaun/mage. A seat that pins a commit made only in its local clone gets
# "upload-pack: not our ref <sha>" on every build and takes the build boxes down,
# so the pin is checked against the fork before it merges.
#
# A pin bump must also move forward: a merge that resolves the Makefile back to
# an ancestor silently undoes a fix, so each pin is compared with the base
# branch's value using the fork compare API. The base is origin/develop, or the
# local develop branch when it has advanced past it (tickets land on local
# develop, and an unpushed pin bump must still set the floor); FORGE_PIN_BASE_REF
# overrides. `ahead` and `identical` are allowed; `behind` and `diverged` fail.
# The base Makefile is read with `git show`; CI's checkout only fetches the PR
# ref, so the GitHub contents API is the fallback.
#
# Each pin comes from the Makefile, but an environment value wins, so a caller
# can check a candidate before bumping:
#   FORGE_ORACLE_REF=<sha> bash scripts/check-fork-pins.sh
#
# The API answers for any commit in the fork *network*, which is exactly what a
# kit's `git fetch <fork-url> <sha>` needs. It does not prove the commit is on a
# branch of the named fork: scripts/forge-oracle-setup.sh and
# scripts/xmage-oracle-setup.sh fetch refs only, so a network-only commit would
# still fail those setups. The success line therefore says "fork network".
#
# Every API read goes through api(): `gh api` first; when gh fails for a reason
# other than "the commit/ref does not exist" (no gh auth, rate limit, network),
# the same read is retried on the unauthenticated REST endpoint with curl, which
# answers for the public forks. Only a 404/422 means "not on the fork"; any
# other failure on both routes is an environment problem (exit 2), never a FAIL.
#
# Exit: 0 every pin is in its fork network and moves forward; 1 a pin is
# missing, behind or diverged (or the API returned a different commit);
# 2 usage/environment (no gh, no pin, not a full SHA, no base Makefile, or the
# GitHub API could not answer at all).
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
cd "$here"

die() {
	echo "check-fork-pins: $*" >&2
	exit 2
}

command -v gh >/dev/null 2>&1 || die "gh is needed to check fork pins"
[ -f Makefile ] || die "no Makefile at $here"

# api <path> <jq-expr>: print the jq result of GET <path>. Returns 0 on success,
# 1 when GitHub says the object does not exist (HTTP 404/422), 2 when the API
# could not answer (auth, rate limit, network, no curl/jq to fall back on).
api() {
	local path=$1 jqx=$2 errf bodyf out code
	errf=$(mktemp "${TMPDIR:-/tmp}/check-fork-pins.err.XXXXXX")
	if out=$(gh api "$path" --jq "$jqx" 2>"$errf"); then
		rm -f "$errf"
		printf '%s\n' "$out"
		return 0
	fi
	if grep -Eq 'HTTP (404|422)|No commit found' "$errf"; then
		cat "$errf" >&2
		rm -f "$errf"
		return 1
	fi
	# gh could not answer (typically: not logged in). Retry unauthenticated.
	cat "$errf" >&2
	rm -f "$errf"
	command -v curl >/dev/null 2>&1 && command -v jq >/dev/null 2>&1 || return 2
	bodyf=$(mktemp "${TMPDIR:-/tmp}/check-fork-pins.body.XXXXXX")
	code=$(curl -sS -m 30 -o "$bodyf" -w '%{http_code}' \
		-H 'Accept: application/vnd.github+json' "https://api.github.com/$path") || code=000
	case $code in
	200)
		out=$(jq -r "$jqx" "$bodyf") || out=
		rm -f "$bodyf"
		[ -n "$out" ] || return 2
		printf '%s\n' "$out"
		return 0
		;;
	404 | 422)
		rm -f "$bodyf"
		return 1
		;;
	*)
		echo "check-fork-pins: unauthenticated GitHub API answered HTTP $code for $path" >&2
		rm -f "$bodyf"
		return 2
		;;
	esac
}

# Every pin whose commit a build fetches from one of our forks belongs here;
# the loop below is the single check they all go through, so covering the next
# fork pin is one row, not a new branch. FORGE_REF, XMAGE_SBV2_REF and the
# phase/mtgish pins are upstream commits and deliberately absent.
pins=(
	"FORGE_ORACLE_REF adams-shaun/forge"
	"XMAGE_REF adams-shaun/mage"
)

makefile_pin() {
	awk -v name="$1" \
		'$1 == name && $2 == "?=" { v = $3; sub(/#.*/, "", v); print v; exit }' "$2"
}

pin_from_makefile() {
	local name=$1 file=${2:-Makefile} value
	value=$(makefile_pin "$name" "$file")
	[ -n "$value" ] || die "Makefile defines no $name"
	printf '%s\n' "$value"
}

# The base branch's pins are the floor: a working-tree pin may be the same
# (identical) or newer (ahead), never older or on a different line (behind or
# diverged). Read the whole file once; the pin loop below is still the single
# path every fork pin goes through, so a new pin row is covered here too.
base_ref=${FORGE_PIN_BASE_REF:-}
if [ -z "$base_ref" ]; then
	base_ref=origin/develop
	if git rev-parse --verify -q develop >/dev/null; then
		if ! git rev-parse --verify -q origin/develop >/dev/null ||
			git merge-base --is-ancestor origin/develop develop; then
			base_ref=develop
		fi
	fi
fi
base_makefile=$(mktemp "${TMPDIR:-/tmp}/check-fork-pins.XXXXXX")
trap 'rm -f "$base_makefile"' EXIT

if ! git show "$base_ref:Makefile" >"$base_makefile" 2>/dev/null; then
	# actions/checkout fetches only the PR ref, so origin/develop can be absent
	# in CI. Ask the same repository for the file at the branch instead.
	base_branch=${base_ref#origin/}
	repo=${GITHUB_REPOSITORY:-}
	if [ -z "$repo" ] && url=$(git config --get remote.origin.url 2>/dev/null); then
		url=${url%.git}
		repo=${url#*github.com:}
		repo=${repo#*github.com/}
	fi
	[ -n "$repo" ] || die "cannot read $base_ref:Makefile and no GitHub repo to ask"
	content=$(api "repos/$repo/contents/Makefile?ref=$base_branch" .content) \
		|| die "cannot read $base_ref:Makefile (git and the GitHub contents API failed)"
	printf '%s' "$content" | tr -d '\n' | base64 -d >"$base_makefile" 2>/dev/null \
		|| die "cannot read $base_ref:Makefile (git and the GitHub contents API failed)"
fi

failed=0
for entry in "${pins[@]}"; do
	read -r name fork <<<"$entry"
	sha=${!name:-$(pin_from_makefile "$name")}
	[[ $sha =~ ^[0-9a-f]{40}$ ]] || die "pin $name is not a full commit sha: '$sha'"

	arc=0
	got=$(api "repos/$fork/commits/$sha" .sha) || arc=$?
	[ "$arc" != 2 ] || die "fork pin check could not run: GitHub API unreachable for $name on $fork (gh unauthenticated and the unauthenticated REST fallback failed)"
	if [ "$arc" = 0 ] && [ "$got" = "$sha" ]; then
		echo "fork pin $name $sha is in the $fork fork network (fetchable by SHA)"
	else
		echo "fork pin $name $sha is not on $fork: push it first" >&2
		failed=1
		continue
	fi

	# A pin the base branch does not define is a new row, not a regression.
	base_sha=$(makefile_pin "$name" "$base_makefile")
	[ -n "$base_sha" ] || continue
	[[ $base_sha =~ ^[0-9a-f]{40}$ ]] ||
		die "$base_ref Makefile pin $name is not a full commit sha: '$base_sha'"
	if [ "$base_sha" = "$sha" ]; then
		continue
	fi

	arc=0
	status=$(api "repos/$fork/compare/$base_sha...$sha" .status) || arc=$?
	[ "$arc" != 2 ] || die "fork pin check could not run: GitHub API unreachable comparing $name on $fork"
	if [ "$arc" != 0 ]; then
		echo "$name: cannot compare ${base_sha:0:10} -> ${sha:0:10} on $fork" >&2
		failed=1
		continue
	fi
	case $status in
	ahead | identical) ;;
	behind)
		echo "$name moves backward: ${base_sha:0:10} -> ${sha:0:10}" >&2
		failed=1
		;;
	diverged)
		echo "$name diverged from $base_ref: ${base_sha:0:10} -> ${sha:0:10}" >&2
		failed=1
		;;
	*)
		echo "$name: unexpected compare status '$status' for ${base_sha:0:10} -> ${sha:0:10} on $fork" >&2
		failed=1
		;;
	esac
done

exit "$failed"
