#!/usr/bin/env bash
# mtgish-fetch.sh -- download the pinned mtgish data for the card-definition
# cross-check (docs/design/2026-10-20-mtgish-crosscheck.md) and verify it
# against oracle/mtgish/MTGISH.lock.
#
# The files land in .mtgish/<ref>/ (gitignored): mtgish.lines.json carries
# Oracle text (Wizards' copyright) and is never committed. Idempotent: a file
# already present with the locked sha256 is not downloaded again.
#
#   scripts/mtgish-fetch.sh            # MTGISH_REF from the Makefile
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
lock="$repo/oracle/mtgish/MTGISH.lock"
ref=${MTGISH_REF:-$(sed -n 's/^MTGISH_REF *?= *//p' "$repo/Makefile")}
lock_ref=$(awk '$1 == "ref" {print $2}' "$lock")
[ -n "$ref" ] || { echo "mtgish-fetch: MTGISH_REF unset" >&2; exit 2; }
[ "$ref" = "$lock_ref" ] || { echo "mtgish-fetch: MTGISH_REF $ref != $lock ref $lock_ref (bump both together)" >&2; exit 2; }
base=${MTGISH_BASE_URL:-https://raw.githubusercontent.com/i5jb/mtgish}
dest="$repo/.mtgish/$ref"
mkdir -p "$dest"

status=0
while read -r kind sum path; do
	[ "$kind" = sha256 ] || continue
	out="$dest/$(basename "$path")"
	if [ -f "$out" ] && [ "$(sha256sum "$out" | cut -d' ' -f1)" = "$sum" ]; then
		echo "mtgish-fetch: $out ok"
		continue
	fi
	tmp="$out.part"
	curl -sSfL --retry 3 -o "$tmp" "$base/$ref/$path"
	got=$(sha256sum "$tmp" | cut -d' ' -f1)
	if [ "$got" != "$sum" ]; then
		echo "mtgish-fetch: $path: sha256 $got, lock says $sum" >&2
		rm -f "$tmp"
		status=1
		continue
	fi
	mv "$tmp" "$out"
	echo "mtgish-fetch: $out fetched"
done <"$lock"
exit $status
