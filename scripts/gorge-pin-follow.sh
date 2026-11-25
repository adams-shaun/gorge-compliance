#!/usr/bin/env bash
# gorge-pin-follow.sh plan | commit CUR MAIN: keep GORGE_PIN on gorge main with no person or orchestrator in
# the loop. .github/workflows/gorge-pin-follow.yml runs it every 15 minutes (and on a gorge-main
# repository_dispatch); the same file lives in four-yay and gorge-compliance.
#
#   plan              compare BASE's pin with gorge main's head (git ls-remote) and print key=value lines for
#                     $GITHUB_OUTPUT:
#                       action=bump      the pin is behind main: build a fresh bump
#                       action=proposed  BRANCH already pins main on top of BASE: leave it, keep its PR open
#                       action=current   BASE already pins main (a still-open bot PR is stale)
#                       action=skip      BASE pins a commit past main, or one main does not hold yet (a gorge
#                                        PR branch: it follows once that commit is merged to main)
#                     plus cur=, main=, msg= (the commit subject) and why=.
#   commit CUR MAIN   reset the current branch (LOCAL_BRANCH, default auto/gorge-pin) to BASE, run the repo's own
#                     bump (BUMP MAIN, default scripts/gorge-pin.sh bump) and commit every tracked change as
#                     "gorge pin: CUR..MAIN (gorge origin/main)" (10-character SHAs).
#
# A red bump never blocks the next: every bump is rebuilt from BASE on the one bot branch, so a newer gorge main
# supersedes it there (the workflow force-pushes that branch, and only that branch).
#
# Env: GORGE_REMOTE (https://github.com/adams-shaun/gorge), BASE (origin/develop), BRANCH (origin/auto/gorge-pin),
# BUMP, LOCAL_BRANCH, GORGE_MAIN (use this head instead of ls-remote), GORGE_GIT_DIR (a gorge clone to judge
# ancestry in; default GitHub's compare API through gh, which reads the public gorge with the job token).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
remote=${GORGE_REMOTE:-https://github.com/adams-shaun/gorge}
base=${BASE:-origin/develop}
branch=${BRANCH:-origin/auto/gorge-pin}

pin_at() { git show "$1:GORGE_PIN" 2>/dev/null | head -n1 | awk '{print $1}'; }

# relation CUR MAIN: where gorge main stands against CUR: ahead (CUR is an ancestor of main), identical, behind
# or diverged.
relation() {
	if [ -n "${GORGE_GIT_DIR:-}" ]; then
		if [ "$1" = "$2" ]; then echo identical
		elif git -C "$GORGE_GIT_DIR" merge-base --is-ancestor "$1" "$2" 2>/dev/null; then echo ahead
		elif git -C "$GORGE_GIT_DIR" merge-base --is-ancestor "$2" "$1" 2>/dev/null; then echo behind
		else echo diverged; fi
	else
		local slug=${remote#https://github.com/}
		gh api "repos/${slug%.git}/compare/$1...$2" -q .status
	fi
}

plan() {
	local main cur msg act why rel
	main=${GORGE_MAIN:-$(git ls-remote "$remote" refs/heads/main | awk 'NR==1 {print $1}' || true)}
	[[ $main =~ ^[0-9a-f]{40}$ ]] || { echo "gorge-pin-follow: no gorge main head from $remote: '$main'" >&2; exit 1; }
	cur=$(pin_at "$base")
	[[ $cur =~ ^[0-9a-f]{40}$ ]] || { echo "gorge-pin-follow: $base:GORGE_PIN is not a full SHA: '$cur'" >&2; exit 1; }
	msg="gorge pin: ${cur:0:10}..${main:0:10} (gorge origin/main)"
	if [ "$cur" = "$main" ]; then
		act=current why="$base already pins gorge main ${main:0:10}"
	else
		rel=$(relation "$cur" "$main")
		case $rel in
		ahead)
			if [ "$(pin_at "$branch")" = "$main" ] && git merge-base --is-ancestor "$base" "$branch" 2>/dev/null; then
				act=proposed why="$branch already pins gorge main ${main:0:10} on top of $base"
			else
				act=bump why="$base pins ${cur:0:10}, gorge main is ${main:0:10}"
			fi ;;
		identical) act=current why="$base already pins gorge main ${main:0:10}" ;;
		behind) act=skip why="$base pins ${cur:0:10}, past gorge main ${main:0:10}" ;;
		*) act=skip why="$base pins ${cur:0:10}, which gorge main ${main:0:10} does not hold ($rel); it follows once that commit is merged" ;;
		esac
	fi
	printf 'action=%s\ncur=%s\nmain=%s\nmsg=%s\nwhy=%s\n' "$act" "$cur" "$main" "$msg" "$why"
}

commit() {
	local cur=${1:?commit CUR MAIN} main=${2:?commit CUR MAIN} bump
	[[ $main =~ ^[0-9a-f]{40}$ ]] || { echo "gorge-pin-follow: MAIN is not a full SHA: '$main'" >&2; exit 1; }
	read -ra bump <<<"${BUMP:-scripts/gorge-pin.sh bump}"
	git checkout -q -B "${LOCAL_BRANCH:-auto/gorge-pin}" "$base"
	"${bump[@]}" "$main"
	! git diff --quiet || { echo "gorge-pin-follow: the bump changed nothing" >&2; exit 1; }
	[ "$(head -n1 GORGE_PIN | awk '{print $1}')" = "$main" ] || { echo "gorge-pin-follow: the bump did not write GORGE_PIN $main" >&2; exit 1; }
	git commit -q -a -m "gorge pin: ${cur:0:10}..${main:0:10} (gorge origin/main)" \
		-m "Moves GORGE_PIN and the go.mod require(s) to gorge main $main. Made by .github/workflows/gorge-pin-follow.yml (scripts/gorge-pin-follow.sh); a newer gorge main supersedes this bump on the same branch."
	git log -1 --format='gorge-pin-follow: committed %h %s'
}

case ${1:-} in
plan) plan ;;
commit) shift; commit "$@" ;;
*) echo "usage: gorge-pin-follow.sh plan | commit CUR MAIN" >&2; exit 2 ;;
esac
