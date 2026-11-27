#!/usr/bin/env bash
# follower-check.sh ENGINE [--set FDN] [--manifest FILE] [--pool FILE]
#                          [--require-mana] [--limit N] [--setup-only] [--min-agree PCT] [--out DIR]
#
# Level-A scenarios through one registered follower, in one command:
#   oraclediff gen -level A  ->  filter (--pool, --limit, --setup-only)
#   ->  the follower's prep (follower-export / forge-export)  ->  the runner
#   ->  oraclediff follower-diff  ->  <out>/SUMMARY.txt (printed).
#
# ENGINE must be a row of `xengine followers` whose prep is follower-export or
# forge-export (else exit 2). The runner is the runner_env variable's value
# when set (e.g. XENGINE_PHASE_RUNNER), else the registry's runner relative to
# the repository root. With an empty request file the runner is not started.
#
#   --set S         the set manifest oracle/compliance/manifests/S.json (default FDN)
#   --manifest FILE read cards from an explicit manifest instead of --set. A
#                   follower-check pool outside every committed set uses this,
#                   and --pool without --set builds one from the pool names so
#                   a card in no committed manifest is selectable too.
#   --pool FILE     keep only the items whose card is named in FILE (one name per
#                   line, '#' starts a comment, blank lines skipped;
#                   oracle/xengine/pools/README.md). Without --set the pool names
#                   are the card source; with --set they filter within it.
#   --require-mana  mana-gate mode, for a pool of mana sources: gen adds each
#                   card's level-A activation item (-mana-abilities), and the
#                   script exits 2 unless every pool name has an activate-mana
#                   item both generated and sent (a misspelling, an activation
#                   the activate family cannot build, a follower preflight
#                   refusal and a --limit that cuts it are all errors). A pool
#                   file whose first non-blank line is '#!require-mana' turns it
#                   on, as oracle/xengine/pools/mana-abilities.txt does.
#   --limit N       then keep the first N items
#   --setup-only    every item becomes steps=[] with no payment: gorge and the
#                   follower both stop at the setup checkpoint, which is compared alone
#   --min-agree P   exit 1 when AGREE/sent*100 < P
#   --out DIR       default .oracle-build/follower-check/ENGINE; a path inside the
#                   repository but not under .oracle-build/ is refused (exit 2):
#                   follower output (Forge's is GPL-3.0) is never committed
#
# Exit: 0 ok; 1 rows.jsonl has a different line count from req.jsonl (a runner that
# dropped rows), the runner failed, or --min-agree was missed; 2 usage.
#
# Every go stage runs in a systemd scope (4G, 400% CPU, GOMAXPROCS=4,
# GOMEMLIMIT=3GiB); XENGINE_SCOPE=none disables it. Overrides for the smoke:
# XENGINE_GO (the go command), XENGINE_ORACLEDIFF (an oraclediff binary or stub).
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"

die() { echo "follower-check: $*" >&2; exit 2; }

engine="" set=FDN set_given=0 manifest="" pool="" require_mana=0 limit="" setup_only=0 min_agree="" out=""
while [ $# -gt 0 ]; do
	case "$1" in
	--set) set=${2:?--set needs a set code}; set_given=1; shift 2 ;;
	--manifest) manifest=${2:?--manifest needs a file}; shift 2 ;;
	--pool) pool=${2:?--pool needs a file}; shift 2 ;;
	--require-mana) require_mana=1; shift ;;
	--limit) limit=${2:?--limit needs a number}; shift 2 ;;
	--setup-only) setup_only=1; shift ;;
	--min-agree) min_agree=${2:?--min-agree needs a percentage}; shift 2 ;;
	--out) out=${2:?--out needs a directory}; shift 2 ;;
	-*) die "unknown argument $1" ;;
	*) [ -z "$engine" ] || die "unexpected argument $1"; engine=$1; shift ;;
	esac
done
[ -n "$engine" ] || die "usage: follower-check.sh ENGINE [--set FDN] [--manifest FILE] [--pool FILE] [--require-mana] [--limit N] [--setup-only] [--min-agree PCT] [--out DIR]"
case "$limit" in *[!0-9]*) die "--limit wants a non-negative integer" ;; esac
case "$min_agree" in *[!0-9.]* | .) die "--min-agree wants a number" ;; esac
case "$set" in "" | */* | .*) die "--set wants a set code" ;; esac
[ -z "$pool" ] || [ -f "$pool" ] || die "--pool $pool: no such file"
[ -z "$manifest" ] || [ -f "$manifest" ] || die "--manifest $manifest: no such file"
[ -z "$manifest" ] || [ "$set_given" = 0 ] || die "--manifest and --set name different card sources; pass one"
command -v jq >/dev/null || die "jq is needed"

GO=${XENGINE_GO:-go}

capped() {
	if [ "${XENGINE_SCOPE:-systemd}" = none ]; then
		env GOMAXPROCS=4 GOMEMLIMIT=3GiB "$@"
	else
		systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env GOMAXPROCS=4 GOMEMLIMIT=3GiB "$@"
	fi
}
xe() { capped "$GO" run -trimpath ./cmd/xengine "$@"; }

# The follower registry; see cmd/xengine/followers.go for the columns.
registry=$(xe followers) || die "xengine followers FAILED"
found=0 names=""
while IFS=$'\t' read -r name f_runner f_prep _ f_runner_env _; do
	case "$name" in "" | "#"*) continue ;; esac
	names+=" $name"
	if [ "$name" = "$engine" ]; then found=1; prep=$f_prep runner_rel=$f_runner runner_env=$f_runner_env; fi
done <<<"$registry"
[ "$found" = 1 ] || die "unknown follower $engine (want one of:$names)"
case "$prep" in
follower-export | forge-export) ;;
*) die "follower $engine: prep $prep is not follower-export or forge-export" ;;
esac
runner=${!runner_env:-$repo/$runner_rel}

# Follower output can never be one `git add` from a commit.
[ -n "$out" ] || out=$repo/.oracle-build/follower-check/$engine
absout=$(realpath -m "$out")
case "$absout/" in
"$repo"/.oracle-build/*) ;;
"$repo"/*) die "refusing --out $out: it is inside the repository $repo and not under .oracle-build/" ;;
esac
out=$absout
mkdir -p "$out"

# run_od: the oraclediff stub when XENGINE_ORACLEDIFF is set, else a binary built once into <out>/bin.
if [ -z "${XENGINE_ORACLEDIFF:-}" ]; then
	mkdir -p "$out/bin"
	capped "$GO" build -trimpath -o "$out/bin/oraclediff" ./cmd/oraclediff
fi
run_od() {
	if [ -n "${XENGINE_ORACLEDIFF:-}" ]; then "$XENGINE_ORACLEDIFF" "$@"; else capped "$out/bin/oraclediff" "$@"; fi
}

pool_names=$out/pool-names.txt
if [ -n "$pool" ]; then
	sed -e 's/#.*//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' "$pool" | sed -e '/^$/d' >"$pool_names"
	# The directive is the first non-blank line, so a name list cannot hold it by accident.
	if [ "$(sed -e '/^[[:space:]]*$/d' "$pool" | head -n 1 | tr -d '[:space:]')" = '#!require-mana' ]; then require_mana=1; fi
fi
[ "$require_mana" = 0 ] || [ -n "$pool" ] || die "--require-mana needs --pool"


# A pool without an explicit --set selects the cards themselves: followers test
# cards every committed manifest may omit (The Alright Henge), and a card in a
# set other than FDN is still reachable. --set keeps the set manifest as the
# card source and the pool as a filter within it.
if [ -z "$manifest" ]; then
	if [ "$set_given" = 0 ] && [ -n "$pool" ]; then
		manifest=$out/manifest.json
		jq -R -s 'split("\n") | map(select(length > 0)) | {code:"POOL", name:"follower-check pool", cards: map({name: .})}' "$pool_names" >"$manifest"
		set=POOL
	else
		manifest=oracle/compliance/manifests/$set.json
	fi
else
	manifest=$(realpath -m "$manifest")
	set=$(jq -r '.code // empty' "$manifest" 2>/dev/null || true)
	[ -n "$set" ] || set=MANIFEST
fi
rm -f "$out/gen.jsonl" "$out/items.jsonl" "$out/req.jsonl" "$out/rows.jsonl" "$out/diff.jsonl" "$out/$engine-prep.jsonl"

# 3. gen. -mana-abilities is the mana-gate mode (--require-mana): it appends the
# level-A activation item for every battlefield mana ability, the step a
# follower's mana-source mapping reads. A plain --pool or --set keeps the
# ordinary level-A scenario set, so its behaviour and committed bytes do not
# move.
if [ -n "$pool" ] && [ ! -s "$pool_names" ]; then
	: >"$out/gen.jsonl"
	: >"$out/gen.jsonl.skips.jsonl"
else
	gen_args=(-level A)
	if [ "$require_mana" = 1 ]; then gen_args+=(-mana-abilities); fi
	run_od gen "${gen_args[@]}" -manifest "$manifest" -out "$out/gen.jsonl"
fi

# 3b. (--require-mana) every pool name must have produced an activation item. A
# misspelling is a "not in corpus" skip; a real mana source the activate family
# cannot build is a mana-ability skip beside its base item. Either way,
# filtering it out would leave the gate reporting AGREE for a card whose mana
# ability it never exercised, so the names that produced none are an input error.
if [ "$require_mana" = 1 ] && [ -s "$pool_names" ]; then
	missing=$(jq -r -s --rawfile names "$pool_names" '
		($names | split("\n") | map(select(length > 0))) as $want
		| ([.[] | select((.template // "") | startswith("activate-mana")) | .card]) as $got
		| ($want - $got) | .[]
	' "$out/gen.jsonl")
	if [ -n "$missing" ]; then
		die "pool names with no level-A mana activation item: $(printf '%s\n' "$missing" | paste -sd, -) (see $out/gen.jsonl.skips.jsonl)"
	fi
fi

# 4. filter
filter='.'
if [ -n "$pool" ]; then
	jq -c --rawfile names "$pool_names" '($names | split("\n") | map(select(. != "")) | map({key: ., value: true}) | from_entries) as $p | select($p[.card] // false)' \
		"$out/gen.jsonl" >"$out/pooled.jsonl"
	src=$out/pooled.jsonl
else
	src=$out/gen.jsonl
fi
if [ "$setup_only" = 1 ]; then filter='.steps = [] | del(.payment)'; fi
if [ -n "$limit" ]; then
	jq -c "$filter" "$src" | head -n "$limit" >"$out/items.jsonl" || true
else
	jq -c "$filter" "$src" >"$out/items.jsonl"
fi
rm -f "$out/pooled.jsonl"

# 5. prep
case "$prep" in
follower-export) run_od follower-export -engine "$engine" -scenarios "$out/items.jsonl" -out "$out/req.jsonl" -runner "$runner" >"$out/export.log" 2>&1 || { cat "$out/export.log" >&2; echo "follower-check: follower-export failed" >&2; exit 1; } ;;
forge-export) run_od forge-export -scenarios "$out/items.jsonl" -out "$out/req.jsonl" -runner "$runner" >"$out/export.log" 2>&1 || { cat "$out/export.log" >&2; echo "follower-check: forge-export failed" >&2; exit 1; } ;;
esac
[ -e "$out/req.jsonl" ] || : >"$out/req.jsonl"

# 5b. (--require-mana) the activation item must have reached the runner. gen
# can make it and the follower's preflight refuse it (a mana source the driver
# does not map), or --limit can cut it; either leaves the named card
# unexercised under an AGREE, so each is an input error naming the card.
if [ "$require_mana" = 1 ] && [ -s "$pool_names" ]; then
	unsent=$(jq -r -s --rawfile names "$pool_names" '
		($names | split("\n") | map(select(length > 0))) as $want
		| ([.[] | (.item // .) | select((.template // "") | startswith("activate-mana")) | .card]) as $got
		| ($want - $got) | .[]
	' "$out/req.jsonl")
	if [ -n "$unsent" ]; then
		reasons=""
		if [ -s "$out/$engine-prep.jsonl" ]; then
			reasons=$(jq -r 'select(.id | contains("/activate-mana")) | "\(.id): \(.reason) \(.detail // "")"' "$out/$engine-prep.jsonl" | paste -sd';' -)
		fi
		die "pool names whose mana activation item was not sent: $(printf '%s\n' "$unsent" | paste -sd, -) (refused by $engine's preflight: ${reasons:-none}; or cut by --limit)"
	fi
fi
rm -f "$pool_names"

# 6. runner (not started for an empty request file)
rc_run=0
if [ -s "$out/req.jsonl" ]; then
	"$runner" "$out/req.jsonl" "$out/rows.jsonl" || rc_run=$?
else
	: >"$out/rows.jsonl"
fi
[ -e "$out/rows.jsonl" ] || : >"$out/rows.jsonl"

# 7. diff
run_od follower-diff -engine "$engine" -req "$out/req.jsonl" -forge "$out/rows.jsonl" -cache "$out/cache" -out "$out/diff.jsonl" >"$out/diff.log" 2>&1 \
	|| { cat "$out/diff.log" >&2; echo "follower-check: follower-diff failed" >&2; exit 1; }
[ -e "$out/diff.jsonl" ] || : >"$out/diff.jsonl"

# 8. summary
lines() { if [ -e "$1" ]; then wc -l <"$1" | tr -d ' '; else echo 0; fi; }
items=$(lines "$out/items.jsonl")
refused=$(lines "$out/$engine-prep.jsonl")
sent=$(lines "$out/req.jsonl")
rows=$(lines "$out/rows.jsonl")
agree=$(jq -r 'select(.verdict.status == "AGREE") | 1' "$out/diff.jsonl" | wc -l | tr -d ' ')
{
	echo "follower-check: $engine set=$set items=$items refused=$refused sent=$sent rows=$rows"
	echo "verdict status:"
	jq -r '.verdict.status // "?"' "$out/diff.jsonl" | sort | uniq -c | sort -k1,1nr -k2,2 | awk '{printf "  %s %s\n", $2, $1}'
	echo "top DIVERGE fields:"
	jq -r 'select(.verdict.status == "DIVERGE") | .verdict.field // "?"' "$out/diff.jsonl" | sort | uniq -c | sort -k1,1nr -k2,2 | head -n 10 | awk '{c=$1; $1=""; printf "  %s %s\n", c, substr($0,2)}'
	echo "top HARNESS msg prefixes:"
	jq -r 'select(.verdict.status == "HARNESS") | (.verdict.msg // "?") | gsub("[\n\t]"; " ") | .[0:60]' "$out/diff.jsonl" | sort | uniq -c | sort -k1,1nr -k2 | head -n 10 | sed -e 's/^ *\([0-9]*\) /  \1 /'
} >"$out/SUMMARY.txt"
cat "$out/SUMMARY.txt"

# 9. exit
rc=0
if [ "$rc_run" != 0 ]; then echo "follower-check: runner exited $rc_run" >&2; rc=1; fi
if [ "$rows" != "$sent" ]; then echo "follower-check: runner wrote $rows rows for $sent requests" >&2; rc=1; fi
if [ -n "$min_agree" ]; then
	if ! awk -v a="$agree" -v s="$sent" -v m="$min_agree" 'BEGIN { exit !(s > 0 && a * 100 / s >= m) }'; then
		echo "follower-check: AGREE $agree of $sent is below --min-agree $min_agree" >&2
		rc=1
	fi
fi
exit "$rc"
