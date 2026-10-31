# Sourced by oracle/validation/oracle/*.sh: repo root, XMAGE_REF and memory caps.
# Set GOTMPDIR to keep Go's temporary files off a RAM-backed /tmp.
repo=$(git rev-parse --show-toplevel)
ref=${XMAGE_REF:-$(sed -n 's/^XMAGE_REF *?= *//p' "$repo/Makefile")}
export GOMEMLIMIT=${GOMEMLIMIT:-16GiB} GOMAXPROCS=${GOMAXPROCS:-16}
[ -z "${GOTMPDIR:-}" ] || mkdir -p "$GOTMPDIR"
