#!/usr/bin/env bash
# gorge-pin-follow-test.sh: scripts/gorge-pin-follow.sh (run by .github/workflows/gorge-pin-follow.yml) bumps a pin
# that is behind gorge main, leaves an up-to-date bot branch alone, rebuilds it when develop or main moves (a red
# bump is superseded, never blocking), and skips a pin that is current, past main, or off main.
set -u
here=$(cd "$(dirname "$0")" && pwd); S=$here/gorge-pin-follow.sh
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
fail=0; ok() { if eval "$2"; then echo "ok   $1"; else echo "FAIL $1"; fail=1; fi; }
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t GIT_CONFIG_GLOBAL=/dev/null
G=$tmp/gorge; git init -q -b main "$G"
gc() { git -C "$G" commit -q --allow-empty -m "$1"; git -C "$G" rev-parse HEAD; }
c1=$(gc one); c2=$(gc two)
git -C "$G" checkout -q -b side "$c1"; s1=$(gc side); git -C "$G" checkout -q main
R=$tmp/repo; git init -q -b develop "$R"
echo "$c1" >"$R/GORGE_PIN"; echo "module x" >"$R/go.mod"; git -C "$R" add -A; git -C "$R" commit -qm base
printf '#!/usr/bin/env bash\necho "$1" >GORGE_PIN; echo "// $1" >>go.mod\n' >"$tmp/bump"; chmod +x "$tmp/bump"
export GORGE_REMOTE=$G GORGE_GIT_DIR=$G BASE=develop BRANCH=auto/gorge-pin BUMP=$tmp/bump
plan() { (cd "$R" && bash "$S" plan) 2>&1; }
act() { plan | sed -n 's/^action=//p'; }
setpin() { git -C "$R" checkout -q develop; echo "$1" >"$R/GORGE_PIN"; git -C "$R" commit -qam "pin $1"; }

out=$(plan)
ok "a pin behind main is bumped" '[ "$(act)" = bump ] && [[ "$out" == *"cur=$c1"* ]] && [[ "$out" == *"main=$c2"* ]]'
ok "the subject names the range" '[[ "$out" == *"msg=gorge pin: ${c1:0:10}..${c2:0:10} (gorge origin/main)"* ]]'
out=$(cd "$R" && bash "$S" commit "$c1" "$c2" 2>&1); rc=$?
ok "commit runs the repo's bump on auto/gorge-pin off develop" '[ $rc -eq 0 ] && [ "$(git -C "$R" rev-parse --abbrev-ref HEAD)" = auto/gorge-pin ] && [ "$(git -C "$R" rev-parse HEAD^)" = "$(git -C "$R" rev-parse develop)" ]'
ok "the commit carries GORGE_PIN, go.mod and the subject" '[ "$(git -C "$R" show HEAD:GORGE_PIN)" = "$c2" ] && git -C "$R" show HEAD:go.mod | /usr/bin/grep -q "$c2" && [ "$(git -C "$R" log -1 --format=%s)" = "gorge pin: ${c1:0:10}..${c2:0:10} (gorge origin/main)" ]'
ok "a bot branch that already pins main on develop is left alone" '[ "$(act)" = proposed ]'
git -C "$R" checkout -q develop; echo x >"$R/other"; git -C "$R" add other; git -C "$R" commit -qm other
ok "develop moving rebuilds the bump" '[ "$(act)" = bump ]'
(cd "$R" && bash "$S" commit "$c1" "$c2" >/dev/null 2>&1)
ok "the rebuilt bump sits on the new develop" '[ "$(act)" = proposed ] && [ "$(git -C "$R" rev-parse auto/gorge-pin^)" = "$(git -C "$R" rev-parse develop)" ]'
c3=$(gc three)
ok "a newer gorge main supersedes a (red) bump" '[ "$(act)" = bump ]'
out=$(cd "$R" && bash "$S" commit "$c1" "$c3" 2>&1); rc=$?
ok "the superseding bump replaces the old one on the same branch" '[ $rc -eq 0 ] && [ "$(git -C "$R" show auto/gorge-pin:GORGE_PIN)" = "$c3" ] && [ "$(git -C "$R" rev-list --count develop..auto/gorge-pin)" = 1 ]'
setpin "$c3"
ok "develop at main is current" '[ "$(act)" = current ]'
setpin "$s1"
ok "a pin off main (a gorge PR branch) is skipped" '[ "$(act)" = skip ] && [[ "$(plan)" == *"does not hold"* ]]'
setpin "$c3"; git -C "$G" update-ref refs/heads/main "$c2"
ok "a pin past main is skipped" '[ "$(act)" = skip ] && [[ "$(plan)" == *"past gorge main"* ]]'
git -C "$G" update-ref refs/heads/main "$c3"; setpin "$c1"
out=$(cd "$R" && BUMP=true bash "$S" commit "$c1" "$c3" 2>&1); rc=$?
ok "a bump that changes nothing fails" '[ $rc -ne 0 ] && [[ "$out" == *"changed nothing"* ]]'
git -C "$R" checkout -q develop
out=$(cd "$R" && GORGE_REMOTE=$tmp/nope GORGE_GIT_DIR=$G bash "$S" plan 2>&1); rc=$?
ok "no gorge main head fails loudly" '[ $rc -ne 0 ] && [[ "$out" == *"no gorge main head"* ]]'
[ $fail -eq 0 ] && echo "gorge-pin-follow-test: all ok" || { echo "gorge-pin-follow-test: FAILED"; exit 1; }
