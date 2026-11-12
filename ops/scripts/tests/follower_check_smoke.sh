#!/usr/bin/env bash
# follower_check_smoke.sh — prove scripts/follower-check.sh wires gen, filter,
# prep, runner and follower-diff together, without Java, systemd, a corpus or
# the network. go (the registry) and oraclediff are stubs on the script's
# override hooks, the runner is a stub on the registry's runner_env.
#   - an unknown engine, and one whose prep is xprep, exit 2;
#   - an --out inside the repository but outside .oracle-build/ exits 2;
#   - --setup-only items have "steps":[] and no payment;
#   - --pool keeps only pool cards (comments and blank lines skipped), --limit the first N;
#   - a runner that drops a row makes the script exit 1;
#   - --min-agree below/above the measured rate exits 0/1;
#   - SUMMARY.txt has the status histogram; an empty request file never starts the runner.
#
#   ops/scripts/tests/follower_check_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
CHECK=$ROOT/scripts/follower-check.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/follower-check-smoke.XXXXXX")
trap 'rm -rf "$TMP" "$ROOT/.follower-check-smoke-out"' EXIT
fails=0
check() { if eval "$2"; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi; }
has() { [[ $1 == *"$2"* ]]; }

BIN=$TMP/bin
mkdir -p "$BIN"
export LOG=$TMP/calls.log
: >"$LOG"

# go stub: only `go run -trimpath ./cmd/xengine followers`, a canned registry.
cat >"$BIN/go" <<'STUB'
#!/usr/bin/env bash
[ "$1" = run ] && [ "$4" = followers ] || exit 0
printf '#name\trunner\tprep\tjvm\trunner_env\tmem_env\twait_env\n'
printf 'xmage\tscripts/xmage.sh\txprep\ttrue\tXENGINE_XMAGE_RUNNER\tXENGINE_XMAGE_MEM\tXENGINE_XMAGE_WAIT\n'
printf 'phase\tscripts/phase.sh\tfollower-export\tfalse\tXENGINE_PHASE_RUNNER\tXENGINE_PHASE_MEM\tXENGINE_PHASE_WAIT\n'
STUB
# oraclediff stub: gen writes 6 items (cards A..F, each with a step and a payment);
# follower-export sends all but F (F is a refusal); follower-diff marks the first
# SMOKE_AGREE (default 3) requests AGREE, the rest DIVERGE.
cat >"$BIN/oraclediff" <<'STUB'
#!/usr/bin/env bash
sub=$1; shift
declare -A o
while [ $# -gt 0 ]; do o[${1#-}]=$2; shift 2; done
echo "oraclediff $sub" >>"$LOG"
case "$sub" in
gen)
	for c in A B C D E F; do
		printf '{"id":"s:%s","card":"%s","setup":{},"steps":[{"op":"pass"}],"payment":{"x":1}}\n' "$c" "$c"
	done >"${o[out]}"
	;;
follower-export)
	echo "$sub engine=${o[engine]} runner=${o[runner]}" >>"$LOG"
	grep -v '"card":"F"' "${o[scenarios]}" >"${o[out]}" || true
	grep '"card":"F"' "${o[scenarios]}" >"$(dirname "${o[out]}")/${o[engine]}-prep.jsonl" || true
	;;
follower-diff)
	n=0
	while IFS= read -r l; do
		[ -n "$l" ] || continue
		n=$((n + 1))
		if [ "$n" -le "${SMOKE_AGREE:-3}" ]; then printf '{"id":%d,"verdict":{"status":"AGREE"}}\n' "$n"
		else printf '{"id":%d,"verdict":{"status":"DIVERGE","field":"life","checkpoint":"setup"}}\n' "$n"; fi
	done <"${o[req]}" >"${o[out]}"
	;;
esac
STUB
cat >"$BIN/runner" <<'STUB'
#!/usr/bin/env bash
echo "runner" >>"$LOG"
if [ -n "${SMOKE_DROP:-}" ]; then head -n -1 "$1" >"$2"; else cat "$1" >"$2"; fi
STUB
chmod +x "$BIN"/*

cc() { # args...: one check run
	XENGINE_GO=$BIN/go XENGINE_SCOPE=none XENGINE_ORACLEDIFF=$BIN/oraclediff XENGINE_PHASE_RUNNER=$BIN/runner \
		"$CHECK" "$@" 2>&1
}
runs() { /usr/bin/grep -c '^runner' "$LOG" || true; }

out=$(cc nosuch --out "$TMP/o0"); rc=$?
check "unknown engine exits 2" '[ "$rc" = 2 ] && has "$out" "unknown follower nosuch (want one of: xmage phase)"' "rc=$rc $out"
out=$(cc xmage --out "$TMP/o0"); rc=$?
check "xprep engine exits 2" '[ "$rc" = 2 ] && has "$out" "prep xprep"' "rc=$rc $out"
out=$(cc phase --out "$ROOT/.follower-check-smoke-out"); rc=$?
check "in-repo out dir exits 2" '[ "$rc" = 2 ] && has "$out" "not under .oracle-build/" && [ ! -e "$ROOT/.follower-check-smoke-out" ]' "rc=$rc $out"

# Default run: 6 items, 1 refused, 5 sent, 3 agree.
O=$TMP/o1; : >"$LOG"
out=$(cc phase --set XYZ --out "$O"); rc=$?
check "plain run exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "summary line" 'has "$out" "follower-check: phase set=XYZ items=6 refused=1 sent=5 rows=5"' "$out"
check "SUMMARY.txt has the status histogram" '/usr/bin/grep -q "AGREE 3" "$O/SUMMARY.txt" && /usr/bin/grep -q "DIVERGE 2" "$O/SUMMARY.txt" && /usr/bin/grep -q " 2 life" "$O/SUMMARY.txt"' "$(cat "$O/SUMMARY.txt" 2>/dev/null)"
check "plain: steps and payment kept" '[ "$(jq -c ".steps|length" "$O/items.jsonl" | sort -u)" = 1 ] && jq -e "has(\"payment\")" "$O/items.jsonl" >/dev/null'
check "runner got the env override and the prep the runner path" '/usr/bin/grep -q "follower-export engine=phase runner=$BIN/runner" "$LOG"' "$(cat "$LOG")"

# --setup-only, --pool (comments, blanks), --limit.
printf '# pool\nB\n\nD # inline\nZ\n' >"$TMP/pool.txt"
O=$TMP/o2
out=$(cc phase --setup-only --pool "$TMP/pool.txt" --out "$O"); rc=$?
check "setup-only+pool exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "setup-only: steps [] and no payment" '[ "$(jq -c ".steps" "$O/items.jsonl" | sort -u)" = "[]" ] && ! jq -e "has(\"payment\")" "$O/items.jsonl" >/dev/null'
check "pool keeps only pool cards" '[ "$(jq -r .card "$O/items.jsonl" | tr "\n" " ")" = "B D " ]' "$(jq -r .card "$O/items.jsonl")"
out=$(cc phase --limit 2 --out "$O"); rc=$?
check "limit keeps the first N" '[ "$(jq -r .card "$O/items.jsonl" | tr "\n" " ")" = "A B " ]'
: >"$LOG"
out=$(cc phase --limit 0 --out "$O"); rc=$?
check "limit 0: exits 0, items=0, runner not started" '[ "$rc" = 0 ] && has "$out" "items=0" && [ "$(runs)" = 0 ] && [ -e "$O/rows.jsonl" ] && [ ! -s "$O/rows.jsonl" ]' "rc=$rc $out"

# A runner that drops a row.
O=$TMP/o3
out=$(SMOKE_DROP=1 cc phase --out "$O"); rc=$?
check "dropped row exits 1" '[ "$rc" = 1 ] && has "$out" "rows=4" && has "$out" "sent=5"' "rc=$rc $out"

# --min-agree: measured 3/5 = 60%.
O=$TMP/o4
out=$(cc phase --min-agree 61 --out "$O"); rc=$?
check "min-agree above the rate exits 1" '[ "$rc" = 1 ]' "rc=$rc $out"
out=$(cc phase --min-agree 60 --out "$O"); rc=$?
check "min-agree at the rate exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
out=$(cc phase --min-agree 50 --out "$O"); rc=$?
check "min-agree below the rate exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"

if [ "$fails" != 0 ]; then echo "follower_check_smoke: $fails check(s) FAILED" >&2; exit 1; fi
echo "follower_check_smoke: ok"
