# Sourced by oracle/validation/oracle/*.sh: repo root, XMAGE_REF and a Go temp dir
# off the RAM-backed /tmp. /mnt/sata is volatile scratch (operator,
# 2026-10-05): everything the oracle needs to run lives in this repo; only
# rebuildable outputs (the XMage build, caches, run dirs) live there.
repo=$(git rev-parse --show-toplevel)
ref=${XMAGE_REF:-$(sed -n 's/^XMAGE_REF *?= *//p' "$repo/Makefile")}
export GOMEMLIMIT=${GOMEMLIMIT:-16GiB} GOMAXPROCS=${GOMAXPROCS:-16}
# The maintainers' box keeps Go temp files on /mnt/sata; elsewhere the default.
if [ -z "${GOTMPDIR:-}" ] && [ -d /mnt/sata/gorge-training ]; then
	export GOTMPDIR=/mnt/sata/gorge-training/gotmp
fi
[ -z "${GOTMPDIR:-}" ] || mkdir -p "$GOTMPDIR"
