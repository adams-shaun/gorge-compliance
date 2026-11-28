#!/usr/bin/env bash
# follower_check_smoke.sh — prove scripts/follower-check.sh wires gen, filter,
# prep, runner and follower-diff together, without Java, systemd, a corpus or
# the network. go (the registry) and oraclediff are stubs on the script's
# override hooks, the runner is a stub on the registry's runner_env.
#   - an unknown engine, and one whose prep is xprep, exit 2;
#   - an --out inside the repository but outside .oracle-build/ exits 2;
#   - --setup-only items have "steps":[] and no payment;
#   - --pool keeps only pool cards (comments and blank lines skipped), --limit the first N;
#   - a plain --pool is a generic filter: gen gets no -mana-abilities and a non-mana name is fine;
#   - --require-mana (or a '#!require-mana' first line) gives gen -mana-abilities and exits 2,
#     naming the card, for a pool name with no generated activation item, one the follower's
#     preflight refuses, or one --limit cuts;
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
printf 'forge\tscripts/forge-oracle-run.sh\tforge-export\tfalse\tXENGINE_FORGE_RUNNER\tXENGINE_FORGE_MEM\tXENGINE_FORGE_WAIT\n'
STUB
# oraclediff stub: gen writes 6 items (cards A..F, each with a step and a payment);
# follower-export sends all but F (F is a refusal); follower-diff marks the first
# SMOKE_AGREE (default 3) requests AGREE, the rest DIVERGE.
cat >"$BIN/oraclediff" <<'STUB'
#!/usr/bin/env bash
sub=$1; shift
argv="$*"
declare -A o
while [ $# -gt 0 ]; do
	case "$1" in
	-mana-abilities) o[mana-abilities]=true; shift ;;
	*) o[${1#-}]=$2; shift 2 ;;
	esac
done
echo "oraclediff $sub $argv" >>"$LOG"
case "$sub" in
gen)
	for c in A B C D E F; do
		printf '{"id":"s:%s","card":"%s","setup":{},"steps":[{"op":"pass"}],"payment":{"x":1}}\n' "$c" "$c"
	done >"${o[out]}"
	# The cards B, D and F get an activation item when the script passes
	# -mana-abilities (F's is then refused by the stub follower-export).
	if [ "${o[mana-abilities]:-}" = true ]; then
		for c in B D F; do
			printf '{"id":"%s/activate-mana/v1","card":"%s","template":"activate-mana","setup":{},"steps":[{"op":"activate"}],"payment":{"x":1}}\n' "$c" "$c"
		done >>"${o[out]}"
	fi
	;;
follower-export | forge-export)
	eng=${o[engine]:-forge}
	echo "$sub engine=${o[engine]} runner=${o[runner]}" >>"$LOG"
	# The request lines wrap the item like the real follower-export; the
	# refusal rows carry id/reason/detail only.
	grep -v '"card":"F"' "${o[scenarios]}" | jq -c '{id, item: .}' >"${o[out]}" || true
	grep '"card":"F"' "${o[scenarios]}" | jq -c '{id, reason: "phase_lacks_card", detail: "F: stub refusal"}' >"$(dirname "${o[out]}")/$eng-prep.jsonl" || true
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
		XENGINE_FORGE_RUNNER=$BIN/runner \
		"$CHECK" "$@" 2>&1
}
runs() { /usr/bin/grep -c '^runner' "$LOG" || true; }

out=$(cc nosuch --out "$TMP/o0"); rc=$?
check "unknown engine exits 2" '[ "$rc" = 2 ] && has "$out" "unknown follower nosuch (want one of: xmage phase forge)"' "rc=$rc $out"
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
check "plain run: gen omits -mana-abilities" '! /usr/bin/grep -q "oraclediff gen .*-mana-abilities" "$LOG"' "$(cat "$LOG")"

# Forge (forge-export): the same runner is passed to the preflight with
# -runner, and the refusals land in forge-prep.jsonl.
O=$TMP/o1f; : >"$LOG"
out=$(cc forge --set XYZ --out "$O"); rc=$?
check "forge run exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "forge summary line" 'has "$out" "follower-check: forge set=XYZ items=6 refused=1 sent=5 rows=5"' "$out"
check "forge: -runner passed to the forge-export preflight" '/usr/bin/grep -q "forge-export engine= runner=$BIN/runner" "$LOG"' "$(cat "$LOG")"
check "forge: refusal rows in forge-prep.jsonl" '[ "$(wc -l <"$O/forge-prep.jsonl")" = 1 ]' "$(cat "$O/forge-prep.jsonl" 2>/dev/null)"

# --setup-only, --pool (comments, blanks), --limit. A plain --pool is the
# generic card filter: no activation items, a non-mana name (Z, in no gen
# output) is not an error.
printf '# pool\nB\n\nD # inline\nZ\n' >"$TMP/pool.txt"
O=$TMP/o2; : >"$LOG"
out=$(cc phase --setup-only --pool "$TMP/pool.txt" --out "$O"); rc=$?
check "setup-only+pool exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "setup-only: steps [] and no payment" '[ "$(jq -c ".steps" "$O/items.jsonl" | sort -u)" = "[]" ] && ! jq -e "has(\"payment\")" "$O/items.jsonl" >/dev/null'
check "plain pool: gen omits -mana-abilities" '! /usr/bin/grep -q "oraclediff gen .*-mana-abilities" "$LOG"' "$(cat "$LOG")"
check "plain pool keeps only pool cards, no activation items" '[ "$(jq -r .card "$O/items.jsonl" | tr "\n" " ")" = "B D " ]' "$(jq -r .card "$O/items.jsonl")"
out=$(cc phase --set XYZ --pool "$TMP/pool.txt" --out "$O"); rc=$?
check "plain --set --pool exits 0 with a name gen never produced" '[ "$rc" = 0 ] && [ "$(jq -r .card "$O/items.jsonl" | tr "\n" " ")" = "B D " ]' "rc=$rc $out"

# --require-mana: B and D have an activation item that reaches the runner.
printf 'B\nD\n' >"$TMP/mana.txt"
O=$TMP/o2m; : >"$LOG"
out=$(cc phase --pool "$TMP/mana.txt" --require-mana --out "$O"); rc=$?
check "require-mana pool exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
check "require-mana: gen got -mana-abilities" '/usr/bin/grep -q "oraclediff gen .*-mana-abilities" "$LOG"' "$(cat "$LOG")"
check "require-mana: every pool card has an activation request" '[ "$(jq -r "select(.template == \"activate-mana\") | .card" "$O/items.jsonl" | tr "\n" " ")" = "B D " ] && [ "$(jq -r "select(.item.template == \"activate-mana\") | .item.card" "$O/req.jsonl" | tr "\n" " ")" = "B D " ]' "$(cat "$O/req.jsonl")"
# The directive on the pool file's first non-blank line is the same mode.
printf '\n#!require-mana\nB\nD\n' >"$TMP/mana-dir.txt"
: >"$LOG"
out=$(cc phase --pool "$TMP/mana-dir.txt" --out "$TMP/o2d"); rc=$?
check "directive pool: exits 0, gen got -mana-abilities" '[ "$rc" = 0 ] && /usr/bin/grep -q "oraclediff gen .*-mana-abilities" "$LOG"' "rc=$rc $out"
# A directive anywhere but first is just a comment.
printf 'B\n#!require-mana\n' >"$TMP/mana-late.txt"
: >"$LOG"
out=$(cc phase --pool "$TMP/mana-late.txt" --out "$TMP/o2e"); rc=$?
check "late directive is a comment: plain filter" '[ "$rc" = 0 ] && ! /usr/bin/grep -q "oraclediff gen .*-mana-abilities" "$LOG"' "rc=$rc $out"
out=$(cc phase --require-mana --out "$TMP/o2f"); rc=$?
check "require-mana without a pool exits 2" '[ "$rc" = 2 ] && has "$out" "--require-mana needs --pool"' "rc=$rc $out"

# A name with no generated activation item is an input error naming it.
printf 'B\nSol Talismann\n' >"$TMP/pool-missing.txt"
out=$(cc phase --pool "$TMP/pool-missing.txt" --require-mana --out "$TMP/o2g"); rc=$?
check "require-mana: name with no activation item exits 2" '[ "$rc" = 2 ] && has "$out" "no level-A mana activation item: Sol Talismann"' "rc=$rc $out"
# The activation gen made is refused by the follower's preflight: not sent, exit 2, reason shown.
printf 'B\nF\n' >"$TMP/pool-refused.txt"
: >"$LOG"
out=$(cc phase --pool "$TMP/pool-refused.txt" --require-mana --out "$TMP/o2h"); rc=$?
check "require-mana: refused activation exits 2 naming the card and reason" '[ "$rc" = 2 ] && has "$out" "mana activation item was not sent: F" && has "$out" "F/activate-mana/v1: phase_lacks_card F: stub refusal"' "rc=$rc $out"
check "require-mana: refused activation never starts the runner" '[ "$(runs)" = 0 ]'
# --limit cuts the activation items (pooled order: both base items, then the activations).
out=$(cc phase --pool "$TMP/mana.txt" --require-mana --limit 1 --out "$TMP/o2i"); rc=$?
check "require-mana: --limit that cuts an activation exits 2" '[ "$rc" = 2 ] && has "$out" "mana activation item was not sent: B,D"' "rc=$rc $out"
out=$(cc phase --pool "$TMP/mana.txt" --require-mana --limit 4 --out "$TMP/o2j"); rc=$?
check "require-mana: --limit that keeps them exits 0" '[ "$rc" = 0 ]' "rc=$rc $out"
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
