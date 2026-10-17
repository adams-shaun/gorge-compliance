#!/usr/bin/env bash
# heavy-lock.sh [--wait S] [--mem M] [--name N] -- CMD...
#
# Runs CMD under an optional heavy-job lock. HEAVY_LOCK names a wrapper with
# the maintainers' heavy.sh interface (`$HEAVY_LOCK heavy --wait S --mem M
# --name N -- CMD...`, which sets GORGE_BROKER_CLASS for CMD); unset, CMD
# runs directly and the lock options are ignored. gorge-compliance carries
# no lock of its own: the passes are heavy (a JVM per XMage/Forge batch), so
# on a shared box point HEAVY_LOCK at the box's lock.
set -euo pipefail
if [ -n "${HEAVY_LOCK:-}" ]; then
	exec "$HEAVY_LOCK" heavy "$@"
fi
while [ $# -gt 0 ] && [ "$1" != -- ]; do shift; done
[ "${1:-}" = -- ] || { echo "heavy-lock: usage: heavy-lock.sh [opts] -- CMD..." >&2; exit 2; }
shift
exec "$@"
