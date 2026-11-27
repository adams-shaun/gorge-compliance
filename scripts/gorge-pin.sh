#!/usr/bin/env bash
# gorge-pin.sh bump [REV]: move the gorge pin (docs/PINNING.md) to REV (default main), which must already be
# pushed: resolve its full SHA with `git ls-remote`, write GORGE_PIN, require its pseudo-version in go.mod and
# tidy. `make gorge-pin-bump` and .github/workflows/gorge-pin-follow.yml run it. Refreshing the corpus and the
# targets (docs/PINNING.md steps 3 and 4) is the bump PR's CI and triage, not this script.
# gorge-pin.sh check: verify the pin invariant offline (docs/PINNING.md "How the pin is expressed"): GORGE_PIN
# holds a full SHA, go.mod requires the module at exactly that commit's pseudo-version
# v0.0.0-<14-digit commit timestamp>-<first 12 hex of the SHA>, carries no replace for the module (a replace
# resolves the import locally, not at the pinned commit), and go.sum holds both hashes for that version.
# No network, no writes.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
mod=github.com/adams-shaun/gorge
remote=${GORGE_REMOTE:-https://github.com/adams-shaun/gorge}

case ${1:-} in
bump)
	rev=${2:-main}
	if [[ $rev =~ ^[0-9a-f]{40}$ ]]; then
		sha=$rev
	else
		sha=$(git ls-remote "$remote" "refs/heads/$rev" "refs/tags/$rev" | awk 'NR==1 {print $1}')
		[ -n "$sha" ] || { echo "gorge-pin: $rev is not a branch or tag on $remote" >&2; exit 1; }
	fi
	v=$(GOWORK=off GOFLAGS= go list -m -f '{{.Version}}' "$mod@$sha")
	echo "$sha" >GORGE_PIN
	GOWORK=off GOFLAGS= go mod edit -dropreplace "$mod" -require "$mod@$v"
	GOWORK=off GOFLAGS=-mod=mod go mod tidy
	echo "gorge-pin: pinned $mod $v"
	;;
check)
	[ $# -eq 1 ] || { echo "usage: gorge-pin.sh check" >&2; exit 2; }
	sha=$(head -n1 GORGE_PIN | awk '{print $1}')
	[[ $sha =~ ^[0-9a-f]{40}$ ]] || { echo "gorge-pin: GORGE_PIN is not a full SHA: '$sha'" >&2; exit 1; }
	if grep -Eq "^[[:space:]]*(replace[[:space:]]+)?${mod//./\\.}[[:space:]]*(\(|=>)" go.mod; then
		echo "gorge-pin: go.mod replaces $mod; a replace resolves the import locally, not at the pinned commit" >&2
		exit 1
	fi
	v=$(awk '$1=="github.com/adams-shaun/gorge"{print $2; exit} $2=="github.com/adams-shaun/gorge"{print $3; exit}' go.mod)
	[ -n "$v" ] || { echo "gorge-pin: go.mod does not require $mod" >&2; exit 1; }
	[[ $v =~ ^v0\.0\.0-[0-9]{14}-([0-9a-f]{12})$ ]] || { echo "gorge-pin: go.mod requires $mod $v, not a v0.0.0-<14-digit timestamp>-<12-hex> pseudo-version" >&2; exit 1; }
	[[ ${BASH_REMATCH[1]} == "${sha:0:12}" ]] || { echo "gorge-pin: go.mod requires $mod $v, not the pseudo-version of GORGE_PIN $sha" >&2; exit 1; }
	grep -q "^${mod//./\\.} $v h1:" go.sum || { echo "gorge-pin: go.sum has no module hash for $mod $v" >&2; exit 1; }
	grep -q "^${mod//./\\.} $v/go\.mod h1:" go.sum || { echo "gorge-pin: go.sum has no go.mod hash for $mod $v" >&2; exit 1; }
	echo "gorge-pin: GORGE_PIN $sha matches go.mod $v"
	;;
*)
	echo "usage: gorge-pin.sh bump [REV] | check" >&2
	exit 2
	;;
esac
