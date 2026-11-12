#!/bin/sh
# A test follower runner implementing only the preflight protocol
# (cmd/oraclediff/follower_export.go): RUNNER --preflight NAMES.txt.
#   FAKE_NAMES_COPY  copy the names file there
#   FAKE_EXIT        fail with this exit status
#   FAKE_RAW         print this verbatim first (protocol violations)
#   FAKE_REFUSE      names to refuse, separated by '|'
[ "$1" = "--preflight" ] || { echo "only --preflight is implemented" >&2; exit 2; }
[ -n "$FAKE_NAMES_COPY" ] && cp "$2" "$FAKE_NAMES_COPY"
[ -n "$FAKE_EXIT" ] && { echo "fake preflight failing" >&2; exit "$FAKE_EXIT"; }
[ -n "$FAKE_RAW" ] && printf '%s\n' "$FAKE_RAW"
while IFS= read -r n; do
	case "|$FAKE_REFUSE|" in
	*"|$n|"*) printf '%s\t%s\n' "$n" "not in the fake database" ;;
	esac
done < "$2"
exit 0
