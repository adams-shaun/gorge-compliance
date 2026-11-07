#!/usr/bin/env bash
# forge-oracle-test-fresh-clone.sh -- regression test for forge-oracle-setup.sh's
# clean-tree guard on a fresh clone.
#
# A fresh setup clones with --no-checkout and sets a sparse checkout.  On git
# 2.53 sparse-checkout set leaves the index empty, so every tracked path looks
# staged-deleted and the guard rejected a directory that had never been checked
# out.  The setup script must still refuse an existing clone with a modified
# tracked file (it must not discard a developer's edits).
#
# No network, Maven or JDK: a synthetic local fixture repository stands in for
# Forge (no Forge file is copied; Forge is GPL-3.0), the refs are commits in
# that fixture, and the setup script is stopped at "no JDK at" -- after its
# clone, guard and checkout -- by pointing FORGE_ORACLE_JDK at a missing path.
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.coverage/forge-oracle-test-fresh-clone}
work=$(realpath -m "$out")
case "$work" in
  "$here"/.coverage/*) ;;
  *) echo "forge-oracle-test-fresh-clone: refusing to use $work (must be under $here/.coverage)" >&2; exit 1 ;;
esac
rm -rf "$work"
mkdir -p "$work"

fail() {
  echo "forge-oracle-test-fresh-clone: FAIL: $*" >&2
  if [ -n "${setup_out:-}" ]; then
    printf '%s\n' "$setup_out" >&2
  fi
  exit 1
}
say() { echo "forge-oracle-test-fresh-clone: $*"; }

fixture=$work/fixture
root=$work/forge-oracle
clone=$root/forge
missing_jdk=$work/no-such-jdk

# Synthetic Forge-shaped fixture: enough for the pin invariant and the sparse
# set, with a file outside the sparse set to prove the set is applied.
mkdir -p "$fixture/forge-core" "$fixture/forge-gui/res/cardsfolder" \
  "$fixture/forge-gui/res/tokenscripts" "$fixture/forge-gui/res/other"
printf '<project/>\n' > "$fixture/pom.xml"
printf 'core\n' > "$fixture/forge-core/a.txt"
printf 'card\n' > "$fixture/forge-gui/res/cardsfolder/a.txt"
printf 'token\n' > "$fixture/forge-gui/res/tokenscripts/t.txt"
printf 'outside the sparse set\n' > "$fixture/forge-gui/res/other/not-sparse.txt"
git -C "$fixture" init -q
git -C "$fixture" add -A
git -C "$fixture" -c user.name=forge-oracle-test -c user.email=forge-oracle-test@example.invalid \
  commit -qm 'fixture base'
base_ref=$(git -C "$fixture" rev-parse HEAD)

# A second commit that leaves cardsfolder and tokenscripts untouched, so it is
# still a valid FORGE_ORACLE_REF for the same FORGE_REF.
printf 'next\n' > "$fixture/forge-core/b.txt"
git -C "$fixture" add -A
git -C "$fixture" -c user.name=forge-oracle-test -c user.email=forge-oracle-test@example.invalid \
  commit -qm 'fixture next'
next_ref=$(git -C "$fixture" rev-parse HEAD)

# run_setup ORACLE_REF: run the setup script against the fixture and record its
# exit status and combined output in setup_rc/setup_out without tripping set -e.
setup_rc=0
setup_out=
run_setup() {
  local ref=$1
  set +e
  setup_out=$(FORGE_REPO=$fixture FORGE_REF=$base_ref FORGE_ORACLE_REF=$ref \
    FORGE_ORACLE_DIR=$root FORGE_ORACLE_JDK=$missing_jdk \
    bash "$here/scripts/forge-oracle-setup.sh" 2>&1)
  setup_rc=$?
  set -e
}

# 1. A fresh clone with nothing checked out must pass the guard, check out the
#    pinned ref and apply the sparse set.
run_setup "$base_ref"
[ "$setup_rc" -eq 2 ] || fail "fresh clone: expected exit 2 at the missing JDK, got $setup_rc"
case "$setup_out" in
  *"uncommitted changes"*) fail "fresh clone: guard fired on a directory with nothing checked out" ;;
esac
case "$setup_out" in
  *"no JDK at"*) ;;
  *) fail "fresh clone: setup did not reach the JDK check" ;;
esac
[ "$(git -C "$clone" rev-parse HEAD)" = "$base_ref" ] || fail "fresh clone: HEAD is not the pinned ref"
[ -z "$(git -C "$clone" status --porcelain --untracked-files=no)" ] || fail "fresh clone: tree is still dirty after setup"
[ -f "$clone/forge-gui/res/cardsfolder/a.txt" ] || fail "fresh clone: sparse cardsfolder not checked out"
[ -f "$clone/forge-gui/res/tokenscripts/t.txt" ] || fail "fresh clone: sparse tokenscripts not checked out"
[ -f "$clone/forge-core/a.txt" ] || fail "fresh clone: sparse forge-core not checked out"
[ ! -e "$clone/forge-gui/res/other/not-sparse.txt" ] || fail "fresh clone: file outside the sparse set was checked out"
say 'fresh clone: guard passed, pinned ref checked out, sparse set applied'

# 2. An existing clone with a modified tracked file must still be refused with
#    the same message and exit 2, and its HEAD must not move to a newer ref.
printf 'local edit\n' >> "$clone/forge-gui/res/cardsfolder/a.txt"
run_setup "$next_ref"
[ "$setup_rc" -eq 2 ] || fail "modified tree: expected exit 2, got $setup_rc"
case "$setup_out" in
  *"uncommitted changes"*) ;;
  *) fail "modified tree: guard did not fire" ;;
esac
[ "$(git -C "$clone" rev-parse HEAD)" = "$base_ref" ] || fail "modified tree: HEAD moved despite the guard"
case "$(cat "$clone/forge-gui/res/cardsfolder/a.txt")" in
  *"local edit"*) ;;
  *) fail "modified tree: local edit was discarded" ;;
esac
say 'modified tracked file: guard fired, HEAD did not move'

# 3. Restore the edit: an existing clean clone must fetch and move to the new
#    pinned ref.
git -C "$clone" checkout -q -- forge-gui/res/cardsfolder/a.txt
run_setup "$next_ref"
[ "$setup_rc" -eq 2 ] || fail "clean existing clone: expected exit 2 at the missing JDK, got $setup_rc"
case "$setup_out" in
  *"uncommitted changes"*) fail "clean existing clone: guard fired on a clean tree" ;;
esac
[ "$(git -C "$clone" rev-parse HEAD)" = "$next_ref" ] || fail "clean existing clone: HEAD did not move to the pinned ref"
[ -f "$clone/forge-core/b.txt" ] || fail "clean existing clone: new pinned ref was not checked out"
[ -z "$(git -C "$clone" status --porcelain --untracked-files=no)" ] || fail "clean existing clone: tree is dirty after setup"
say 'clean existing clone: moved to the new pinned ref'

# 4. Re-running at the same ref is idempotent: no checkout, no dirty tree.
before=$(wc -l < "$clone/.git/logs/HEAD")
run_setup "$next_ref"
[ "$setup_rc" -eq 2 ] || fail "re-run: expected exit 2 at the missing JDK, got $setup_rc"
case "$setup_out" in
  *"no JDK at"*) ;;
  *) fail "re-run: setup did not reach the JDK check" ;;
esac
[ "$(git -C "$clone" rev-parse HEAD)" = "$next_ref" ] || fail "re-run: HEAD changed"
[ "$(wc -l < "$clone/.git/logs/HEAD")" = "$before" ] || fail "re-run: HEAD moved even though it already matched the pinned ref"
[ -z "$(git -C "$clone" status --porcelain --untracked-files=no)" ] || fail "re-run: tree is dirty"
say 'same-ref re-run: idempotent, no checkout'

say 'PASS'
