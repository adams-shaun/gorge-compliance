#!/usr/bin/env bash
# gorge-pin.sh bump [REV]: move the gorge pin (docs/PINNING.md) to REV (default main), which must already be
# pushed: resolve its full SHA with `git ls-remote`, write GORGE_PIN, require its pseudo-version in go.mod and
# tidy. `make gorge-pin-bump` and .github/workflows/gorge-pin-follow.yml run it. Refreshing the corpus and the
# targets (docs/PINNING.md steps 3 and 4) is the bump PR's CI and triage, not this script.
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
*)
	echo "usage: gorge-pin.sh bump [REV]" >&2
	exit 2
	;;
esac
