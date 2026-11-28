#!/usr/bin/env bash
# forge-driver-follow.sh driver [REF...] | pin: keep the Forge oracle driver on our fork holding gorge's FORGE_REF,
# with no person in the loop.
#
# forge-oracle-setup.sh refuses a FORGE_REF that is not an ancestor of FORGE_ORACLE_REF with identical
# cardsfolder/tokenscripts trees. On 2026-11-25 gorge moved FORGE_REF to 5bc5851a54, gorge-pin-follow carried it into
# develop, the driver lacked it, and every xproof cycle failed for 2 h. Two halves close that:
#
#   driver [REF...]  gorge CI (scripts/check-forge-ref-driver.sh there) refuses a FORGE_REF move until the fork's
#                    oracle-driver branch contains it. This merges every FORGE_REF that gorge main or an open gorge
#                    pull request names (or the REFs given) into oracle-driver and pushes the branch (a fast-forward,
#                    never a force). .github/workflows/forge-driver-follow.yml runs it every 15 minutes.
#   pin              run by gorge-pin-follow.yml on its bump commit: when the new GORGE_PIN's FORGE_REF is not in
#                    FORGE_ORACLE_REF, merge FORGE_REF into FORGE_ORACLE_REF, check the trees match FORGE_REF's, push
#                    the merge to the fork as oracle/pin-<FORGE_REF 12>, and amend the bump to set FORGE_ORACLE_REF.
#                    The pin and the driver then move in one commit, so the box never sees one without the other.
#
# A merge conflict, a tree mismatch or a refused push exits 1 and changes nothing here; the run goes red and the
# guard (scripts/check-forge-ref-in-driver.sh) keeps the bump PR red, so a person resolves it on the fork.
#
# Env: FORGE_PUSH_TOKEN (contents:write on the fork; required to push), FORGE_FORK (adams-shaun/forge),
# FORGE_FORK_URL (clone/push URL; tests point it at a local repository), GORGE_REPO (adams-shaun/gorge),
# FORGE_REF (skip reading gorge's Makefile), DRIVER_BRANCH (oracle-driver), WORK (scratch dir).
# Exit: 0 done or nothing to do; 1 a merge/push failed; 2 usage or environment.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
fork=${FORGE_FORK:-adams-shaun/forge}
gorge=${GORGE_REPO:-adams-shaun/gorge}
dbranch=${DRIVER_BRANCH:-oracle-driver}
url=${FORGE_FORK_URL:-https://github.com/$fork.git}

die() { echo "forge-driver-follow: $*" >&2; exit 2; }
fail() { echo "forge-driver-follow: $*" >&2; exit 1; }
mkpin() { awk -v n="$1" '!seen && $1 == n && ($2 == "?=" || $2 == "=") { v = $3; sub(/#.*/, "", v); print v; seen = 1 }'; }
sha40() { [[ $1 =~ ^[0-9a-f]{40}$ ]]; }

# gorge_forge_ref REF: FORGE_REF in gorge's Makefile at REF (a branch or commit), through the contents API.
gorge_forge_ref() {
	gh api "repos/$gorge/contents/Makefile?ref=$1" --jq .content | tr -d '\n' | base64 -d | mkpin FORGE_REF
}

# contains BASE REF: is REF an ancestor of BASE on the fork (compare API: BASE...REF behind or identical)?
contains() {
	local st
	[ "$1" = "$2" ] && return 0
	st=$(gh api "repos/$fork/compare/$1...$2" --jq .status) || die "compare $1...$2 failed on $fork"
	case $st in behind | identical) return 0 ;; *) return 1 ;; esac
}

push_url() {
	if [ -n "${FORGE_PUSH_TOKEN:-}" ] && [[ $url == https://github.com/* ]]; then
		printf 'https://x-access-token:%s@%s\n' "$FORGE_PUSH_TOKEN" "${url#https://}"
	elif [[ $url == https://github.com/* ]]; then
		die "FORGE_PUSH_TOKEN is not set: the job cannot push to $fork (run ~/op/forgepin.sh once)"
	else
		printf '%s\n' "$url"
	fi
}

# clone_at REF: a blob-less clone of the fork checked out (sparse: only pom.xml) at REF, in $clone.
clone_at() {
	local w=${WORK:-$(mktemp -d "${TMPDIR:-/tmp}/forge-driver-follow.XXXXXX")}
	clone=$w/forge
	rm -rf "$clone"
	git clone -q --filter=blob:none --no-checkout "$url" "$clone"
	git -C "$clone" sparse-checkout set --no-cone /pom.xml
	git -C "$clone" -c advice.detachedHead=false checkout -q --detach "$1"
}

# merge_in REF MSG: merge REF into $clone's HEAD (a no-op when HEAD already holds it); 1 on a conflict.
merge_in() {
	git -C "$clone" merge-base --is-ancestor "$1" HEAD && return 0
	if ! git -C "$clone" -c user.name="gorge forge-driver-follow" -c user.email=noreply@github.com \
		merge -q --no-ff --no-edit -m "$2" "$1"; then
		git -C "$clone" merge --abort 2>/dev/null || true
		fail "merging ${1:0:10} into the driver conflicts; resolve it on $fork by hand"
	fi
}

driver() {
	local refs=("$@") tip r want=() n pr
	tip=$(git ls-remote "$url" "refs/heads/$dbranch" | awk 'NR==1 {print $1}')
	sha40 "$tip" || die "no $dbranch branch on $fork"
	if [ ${#refs[@]} -eq 0 ]; then
		refs+=("$(gorge_forge_ref main)")
		for pr in $(gh api "repos/$gorge/pulls?state=open&per_page=100" --jq '.[].head.sha'); do
			r=$(gorge_forge_ref "$pr" 2>/dev/null) || continue
			refs+=("$r")
		done
	fi
	for r in $(printf '%s\n' "${refs[@]}" | awk 'NF && !seen[$0]++'); do
		sha40 "$r" || die "not a full FORGE_REF sha: '$r'"
		contains "$tip" "$r" || want+=("$r")
	done
	if [ ${#want[@]} -eq 0 ]; then
		echo "forge-driver-follow: $dbranch ${tip:0:10} holds every FORGE_REF named (${#refs[@]})"
		return 0
	fi
	clone_at "$tip"
	for r in "${want[@]}"; do
		merge_in "$r" "Merge FORGE_REF ${r:0:10} into $dbranch (forge-driver-follow)"
		echo "forge-driver-follow: merged ${r:0:10} into $dbranch"
	done
	git -C "$clone" push -q "$(push_url)" "HEAD:refs/heads/$dbranch" || fail "push to $dbranch refused"
	echo "forge-driver-follow: $dbranch ${tip:0:10} -> $(git -C "$clone" rev-parse --short=10 HEAD)"
}

pin() {
	local fref oracle merged p a b
	fref=${FORGE_REF:-$(gorge_forge_ref "$(head -n1 GORGE_PIN | awk '{print $1}')")}
	oracle=$(mkpin FORGE_ORACLE_REF <Makefile)
	sha40 "$fref" || die "FORGE_REF is not a full sha: '$fref'"
	sha40 "$oracle" || die "FORGE_ORACLE_REF is not a full sha: '$oracle'"
	if contains "$oracle" "$fref"; then
		echo "forge-driver-follow: FORGE_ORACLE_REF ${oracle:0:10} holds FORGE_REF ${fref:0:10}"
		return 0
	fi
	clone_at "$oracle"
	merge_in "$fref" "Merge gorge FORGE_REF ${fref:0:10} into the oracle driver ${oracle:0:10} (gorge-pin-follow)"
	merged=$(git -C "$clone" rev-parse HEAD)
	for p in forge-gui/res/cardsfolder forge-gui/res/tokenscripts; do
		a=$(git -C "$clone" rev-parse "$fref:$p" 2>/dev/null || echo none)
		b=$(git -C "$clone" rev-parse "$merged:$p" 2>/dev/null || echo none)
		[ "$a" = "$b" ] || fail "$p differs between FORGE_REF ${fref:0:10} and the merge ${merged:0:10}: the driver carries script changes FORGE_REF lacks"
	done
	# cmd/oraclediff pins the driver its ForcedDieRollTest result holds for (forgeOracleDieRollRef). A merge that adds
	# only card scripts leaves the driver and die-roll code byte-identical, so the pin follows; one that brings code
	# (an upstream merge in FORGE_REF) needs that test re-run by a person.
	if ! git -C "$clone" diff --quiet "$oracle" "$merged" -- . ':!forge-gui/res'; then
		fail "the merge of ${fref:0:10} changes Forge code beyond forge-gui/res: re-run the driver tests (make forge-driver-tests) and bump FORGE_ORACLE_REF by hand"
	fi
	git -C "$clone" push -q "$(push_url)" "$merged:refs/heads/oracle/pin-${fref:0:12}" || fail "push of oracle/pin-${fref:0:12} refused"
	dr=cmd/oraclediff/forge_dieroll_test.go
	if [ -f "$dr" ] && /usr/bin/grep -q "^const forgeOracleDieRollRef = \"$oracle\"" "$dr"; then
		awk -v m="$merged" -v o="${oracle:0:11}" -v f="${fref:0:10}" '
			/^const forgeOracleDieRollRef = / {
				print "// " substr(m, 1, 11) " = " o " merged with gorge FORGE_REF " f " by forge-driver-follow.sh pin: card scripts only, driver"
				print "// and die-roll code byte-identical to " o "."
				print "const forgeOracleDieRollRef = \"" m "\""; next }
			{ print }' "$dr" >"$dr.tmp" && mv "$dr.tmp" "$dr"
	fi
	awk -v m="$merged" -v f="${fref:0:10}" -v o="${oracle:0:10}" '
		$1 == "FORGE_ORACLE_REF" && $2 == "?=" {
			print "# " substr(m, 1, 11) " = " o " merged with gorge FORGE_REF " f " by gorge-pin-follow (scripts/forge-driver-follow.sh pin); fork branch oracle/pin-" f "."
			print "FORGE_ORACLE_REF ?= " m; next }
		{ print }' Makefile >Makefile.tmp && mv Makefile.tmp Makefile
	git -c user.name="gorge forge-driver-follow" -c user.email=noreply@github.com commit -q --amend --no-edit -a
	echo "forge-driver-follow: FORGE_ORACLE_REF ${oracle:0:10} -> ${merged:0:10} (holds FORGE_REF ${fref:0:10})"
}

command -v gh >/dev/null 2>&1 || die "gh is needed"
case ${1:-} in
driver) shift; driver "$@" ;;
pin) pin ;;
*) echo "usage: forge-driver-follow.sh driver [REF...] | pin" >&2; exit 2 ;;
esac
