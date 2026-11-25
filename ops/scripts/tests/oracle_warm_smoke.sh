#!/usr/bin/env bash
# oracle_warm_smoke.sh — prove scripts/oracle-warm.py's client behaviour without a JDK: a stub
# `java` speaks the warm worker protocol (forge.oracle.WarmServer, the XMage ScenarioReplay.serve):
# --serve SOCK IDLE_S MAX_ITEMS, a client line IN<TAB>OUT, "start", then "ok N" or "err ...".
#
#   reuse      two files go to one worker (one pid), each written with every row
#   key        a different launch command gets its own worker
#   tmpdir     a per-batch -Djava.io.tmpdir is not part of the key; the worker gets its slot's own
#   busy       a worker still busy with a file whose caller was killed is replaced, and the next file
#              is answered
#   err        a worker answering err exits 3 (the runner then runs one-shot) and is killed
#   dead       a worker that dies while starting exits 3
#   max/idle   a worker past MAX_ITEMS rows or IDLE_S idle seconds exits; the next file starts another
#
# What this does NOT show: that the real drivers' serve mode replays a file exactly as the one-shot
# run does. That was measured on metal2 (11-25: 232 Forge and 232 XMage rows, warm equal to cold
# apart from ms, in both orders).
#
#   ops/scripts/tests/oracle_warm_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
WARM=$ROOT/scripts/oracle-warm.py
mkdir -p "$ROOT/.oracle-build"
TMP=$(mktemp -d "$ROOT/.oracle-build/oracle-warm-smoke.XXXXXX")
WD=$(mktemp -d "${TMPDIR:-/tmp}/owarm.XXXXXX")   # the workers' sockets: an AF_UNIX path stops at 108 bytes
cleanup() {
	for p in "$WD"/*/s*/pid; do [ -f "$p" ] && kill -9 "$(cat "$p")" 2>/dev/null; done
	rm -rf "$TMP" "$WD"
}
trap cleanup EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

mkdir -p "$TMP/bin"
# The stub worker: each row is {"id", "pid", "tmp"}; IN naming "hang" never answers, "err" answers err.
cat >"$TMP/bin/java" <<'EOF'
#!/usr/bin/env python3
import json, os, socket, sys
a = sys.argv[1:]
if os.environ.get("STUB_DIE"):
    sys.exit(1)
tmp = next((x.split("=", 1)[1] for x in a if x.startswith("-Djava.io.tmpdir=")), "")
i = a.index("--serve")
sock, idle, mx = a[i + 1], int(a[i + 2]), int(a[i + 3])
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.bind(sock)
s.listen(4)
s.settimeout(idle)
served = 0
try:
    while served < mx:
        try:
            c, _ = s.accept()
        except socket.timeout:
            break
        f = c.makefile("rwb")
        inp, out = f.readline().decode().rstrip("\n").split("\t")
        f.write(b"start\n"); f.flush()
        if "hang" in inp:
            import time
            time.sleep(3600)
        if "err" in os.path.basename(inp):
            f.write(b"err boom\n"); f.flush(); c.close(); continue
        n = 0
        with open(inp) as fi, open(out, "w") as fo:
            for ln in fi:
                if ln.strip():
                    fo.write(json.dumps({"id": json.loads(ln)["id"], "pid": os.getpid(), "tmp": tmp}) + "\n")
                    n += 1
        served += n
        f.write(("ok %d\n" % n).encode()); f.flush(); c.close()
finally:
    os.unlink(sock)
EOF
chmod +x "$TMP/bin/java"
printf '{"id":"a"}\n{"id":"b"}\n' >"$TMP/in1.jsonl"
printf '{"id":"c"}\n' >"$TMP/in2.jsonl"
cp "$TMP/in1.jsonl" "$TMP/hang.jsonl"
cp "$TMP/in1.jsonl" "$TMP/err.jsonl"
export ORACLE_WARM_DIR=$WD ORACLE_WARM_SLOTS=1 ORACLE_WARM_START_S=1 ORACLE_WARM_BOOT_S=10
L=("$TMP/bin/java" -Xmx1g -Djava.io.tmpdir=/batch/b00 -cp x Main)
run() { python3 "$WARM" --engine stub --in "$1" --out "$2" -- "${@:3}"; }
pid() { python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["pid"])' "$1"; }

run "$TMP/in1.jsonl" "$TMP/o1.jsonl" "${L[@]}"; rc=$?
run "$TMP/in2.jsonl" "$TMP/o2.jsonl" "${L[@]/b00/b01}"; rc2=$?
check "reuse: both files answered" "$((rc + rc2))" "rc=$rc rc2=$rc2"
[ "$(wc -l <"$TMP/o1.jsonl")" = 2 ] && [ "$(wc -l <"$TMP/o2.jsonl")" = 1 ]
check "reuse: every row written" $?
[ "$(pid "$TMP/o1.jsonl")" = "$(pid "$TMP/o2.jsonl")" ]
check "tmpdir: one worker across two batch tmpdirs" $?
case "$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["tmp"])' "$TMP/o1.jsonl")" in
"$WD"/stub-*/s0/tmp) check "tmpdir: the slot's own" 0 ;;
*) check "tmpdir: the slot's own" 1 ;;
esac

run "$TMP/in2.jsonl" "$TMP/o3.jsonl" "$TMP/bin/java" -Xmx2g -cp x Main
[ "$(pid "$TMP/o3.jsonl")" != "$(pid "$TMP/o1.jsonl")" ] && [ "$(ls -d "$WD"/stub-* | wc -l)" = 2 ]
check "key: another launch command, another worker" $?

old=$(pid "$TMP/o1.jsonl")
timeout 3 python3 "$WARM" --engine stub --in "$TMP/hang.jsonl" --out "$TMP/oh.jsonl" -- "${L[@]}"
run "$TMP/in2.jsonl" "$TMP/o4.jsonl" "${L[@]}"; rc=$?
[ "$rc" = 0 ] && [ "$(pid "$TMP/o4.jsonl")" != "$old" ] && ! kill -0 "$old" 2>/dev/null
check "busy: an abandoned worker is replaced" $? "rc=$rc"

before=$(pid "$TMP/o4.jsonl")
run "$TMP/err.jsonl" "$TMP/oe.jsonl" "${L[@]}"; rc=$?
[ "$rc" = 3 ] && ! kill -0 "$before" 2>/dev/null
check "err: exit 3 and the worker is killed" $? "rc=$rc"

STUB_DIE=1 run "$TMP/in1.jsonl" "$TMP/od.jsonl" "$TMP/bin/java" -Xmx3g -cp x Main; rc=$?
check "dead: a worker that never listens exits 3" "$([ "$rc" = 3 ]; echo $?)" "rc=$rc"

ORACLE_WARM_MAX_ITEMS=2 run "$TMP/in1.jsonl" "$TMP/m1.jsonl" "$TMP/bin/java" -Xmx4g -cp x Main
sleep 0.5
ORACLE_WARM_MAX_ITEMS=2 run "$TMP/in1.jsonl" "$TMP/m2.jsonl" "$TMP/bin/java" -Xmx4g -cp x Main; rc=$?
[ "$rc" = 0 ] && [ "$(pid "$TMP/m1.jsonl")" != "$(pid "$TMP/m2.jsonl")" ]
check "max: a worker past MAX_ITEMS is replaced" $? "rc=$rc"

ORACLE_WARM_IDLE_S=1 run "$TMP/in1.jsonl" "$TMP/i1.jsonl" "$TMP/bin/java" -Xmx5g -cp x Main
sleep 2
ORACLE_WARM_IDLE_S=1 run "$TMP/in1.jsonl" "$TMP/i2.jsonl" "$TMP/bin/java" -Xmx5g -cp x Main; rc=$?
[ "$rc" = 0 ] && [ "$(pid "$TMP/i1.jsonl")" != "$(pid "$TMP/i2.jsonl")" ]
check "idle: an idle worker exits and the next file starts another" $? "rc=$rc"

ORACLE_WARM_DIR=$TMP/$(printf 'x%.0s' {1..80}) run "$TMP/in1.jsonl" "$TMP/ol.jsonl" "${L[@]}" 2>/dev/null; rc=$?
check "long: a socket path past the AF_UNIX limit exits 3" "$([ "$rc" = 3 ]; echo $?)" "rc=$rc"

[ "$fails" = 0 ] && echo "oracle_warm_smoke: PASS" || { echo "oracle_warm_smoke: $fails FAIL"; exit 1; }
