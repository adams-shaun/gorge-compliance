#!/usr/bin/env bash
# forge_oracle_pin_smoke.sh — prove the Forge oracle scripts refuse to run when
# the pin invariant fails, and that the javac-on-change stamp
# works, without Java, git history, systemd or a Forge checkout.
#
# git, java, javac and systemd-run are stubs on PATH. The invariant: FORGE_REF
# must be an ancestor of FORGE_ORACLE_REF and the cardsfolder and tokenscripts
# trees must be equal at both, otherwise forge-oracle-setup.sh and
# forge-oracle-run.sh exit 2 BEFORE any build, javac or JVM starts.
#
#   ops/scripts/tests/forge_oracle_pin_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SETUP=$ROOT/scripts/forge-oracle-setup.sh
RUN=$ROOT/scripts/forge-oracle-run.sh
TMP=$(mktemp -d "${TMPDIR:-/tmp}/forge-oracle-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

FREF=1111111111111111111111111111111111111111
OREF=2222222222222222222222222222222222222222
BIN=$TMP/bin
LOG=$TMP/calls.log
mkdir -p "$BIN" "$TMP/root" "$TMP/clone/.git" "$TMP/clone/forge-gui/res" \
	"$TMP/clone/forge-oracle/src/main/java/forge/oracle" "$TMP/clone/forge-core/src/main/java/forge/util" "$TMP/jdk/bin"
echo 'class A {}' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/A.java"
echo '/stub/forge-core.jar' > "$TMP/root/cp.txt"
# The run script refuses a ref without the step-scoped die force, so the stub
# trees carry the four source markers it checks. A negative case below drops
# ForcedDieRandom.java from a third tree to prove the refusal.
echo 'class ForcedDieRandom { int nextInt(int bound) { return 0; } void arm(int step) { req.coinFlips(step); } }' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/ForcedDieRandom.java"
echo 'class ForgeEngine { Object r = new ForcedDieRandom(); }' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/ForgeEngine.java"
echo 'class Request { String coin_flips; }' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/Request.java"
echo 'class ScriptedController { boolean call(StepMachine m) { return m.req.coinFlips(m.step()).isEmpty(); } }' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/ScriptedController.java"
echo 'class StepMachine { void f() { dice.arm(0); } }' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/StepMachine.java"
echo 'class MyRandom { int f() { return delegate.nextInt(bound); } boolean g() { return delegate.nextBoolean(); } }' > "$TMP/clone/forge-core/src/main/java/forge/util/MyRandom.java"

# git: answers only what the scripts ask, driven by STUB_* variables.
cat > "$BIN/git" <<'STUB'
#!/usr/bin/env bash
echo "git $*" >> "$STUB_LOG"
args=("$@"); [ "${args[0]}" = -C ] && args=("${args[@]:2}")
case "${args[0]}" in
merge-base) exit "${STUB_ANCESTOR:-0}" ;;
diff) exit "${STUB_DIFF:-0}" ;;
ls-files) printf '%s' "${STUB_UNTRACKED:-}" ;;
status) exit 0 ;;
fetch | checkout) exit 0 ;;
rev-parse)
	last=${args[${#args[@]}-1]}
	case "$last" in
	--show-toplevel) echo "$STUB_TOP" ;;
	HEAD) echo "$STUB_HEAD" ;;
	*'^{commit}') last=${last%^\{commit\}}; echo "$last" ;;
	*:*) ref=${last%%:*}; p=${last#*:}
		if [ "$ref" = "$FORGE_REF" ] || [ -n "${STUB_TREE_PATH:-}" -a "$p" != "${STUB_TREE_PATH:-}" ]; then
			echo "tree-$p"
		else
			echo "tree-$p${STUB_TREE_SUFFIX:-}"
		fi ;;
	*) exit 2 ;;
	esac ;;
*) exit 2 ;;
esac
STUB
# java, javac: record the call; a refusal test fails if either is ever reached.
for t in java javac; do
	printf '#!/usr/bin/env bash\necho "%s $*" >> "$STUB_LOG"\n' "$t" > "$TMP/jdk/bin/$t"
done
printf 'echo "runtime res=$FORGE_RES ref=$FORGE_ORACLE_REF cwd=$PWD tmp=$*" >> "$STUB_LOG"\nprintf "stub output\\n" > "${@: -1}"\n' >> "$TMP/jdk/bin/java"
# systemd-run: drop its own flags up to `--`, run the rest.
cat > "$BIN/systemd-run" <<'STUB'
#!/usr/bin/env bash
echo "systemd-run $*" >> "$STUB_LOG"
while [ "$1" != -- ]; do shift; done
shift
exec "$@"
STUB
chmod +x "$BIN"/* "$TMP"/jdk/bin/*

export PATH="$BIN:$PATH" STUB_LOG=$LOG STUB_TOP=$TMP/clone STUB_HEAD=$OREF
export FORGE_REF=$FREF FORGE_ORACLE_REF=$OREF FORGE_ORACLE_DIR=$TMP/root FORGE_ORACLE_CLONE=$TMP/clone FORGE_ORACLE_JDK=$TMP/jdk
export FORGE_REPO=file:///nonexistent XMAGE_ORACLE_DIR=$TMP/none
unset FORGE_ORACLE_SRC FORGE_ORACLE_CLASSES FORGE_ORACLE_TMP FORGE_ORACLE_MAIN
unset STUB_ANCESTOR STUB_DIFF STUB_UNTRACKED STUB_TREE_SUFFIX STUB_TREE_PATH

has() { [[ $1 == *"$2"* ]]; }
reached() { /usr/bin/grep -qE "^(java|javac|systemd-run) |^git (-C [^ ]+ )?checkout" "$LOG" 2>/dev/null; }
count() { /usr/bin/grep -c "^$1 " "$LOG" 2>/dev/null || true; }

# 1. setup: FORGE_REF not an ancestor of the driver ref.
: > "$LOG"
out=$(STUB_ANCESTOR=1 "$SETUP" 2>&1); rc=$?
check "setup refuses a non-ancestor ref (exit 2)" $((rc == 2 ? 0 : 1)) "rc=$rc"
check "  ...naming the ancestry half" $([[ $out == *"not an ancestor"* ]] && echo 0 || echo 1) "$out"
reached; check "  ...before any checkout, javac, java or scope" $((! $? ))
check "  ...and no cp.txt rewritten" $([ "$(cat "$TMP/root/cp.txt")" = /stub/forge-core.jar ] && echo 0 || echo 1)
check "  ...and no build stamp" $([ ! -e "$TMP/root/build.ref" ] && echo 0 || echo 1)
check "  ...the ancestry handler ran" $(/usr/bin/grep -q "merge-base --is-ancestor $FREF $OREF" "$LOG" && echo 0 || echo 1)

# 2. setup: script trees differ at the two refs.
: > "$LOG"
out=$(STUB_TREE_SUFFIX=-moved "$SETUP" 2>&1); rc=$?
check "setup refuses a moved script tree (exit 2)" $((rc == 2 ? 0 : 1)) "rc=$rc"
check "  ...naming the tree half" $([[ $out == *"cardsfolder differs"* ]] && echo 0 || echo 1) "$out"
reached; check "  ...before any checkout, javac, java or scope" $((! $? ))

# Equal cardsfolder is not enough: tokenscripts must independently match.
for script in "$SETUP" "$RUN"; do
	: > "$LOG"
	if [ "$script" = "$SETUP" ]; then
		out=$(STUB_TREE_PATH=forge-gui/res/tokenscripts STUB_TREE_SUFFIX=-moved "$script" 2>&1); rc=$?
	else
		out=$(STUB_TREE_PATH=forge-gui/res/tokenscripts STUB_TREE_SUFFIX=-moved "$script" --compile 2>&1); rc=$?
	fi
	check "$(basename "$script") refuses a tokens-only mismatch" $([ "$rc" = 2 ] && has "$out" "tokenscripts differs" && echo 0 || echo 1) "rc=$rc $out"
	reached; check "  ...before javac, java or scope" $((! $? ))
done

# 3. setup --check-pin passes on a good pin (so the refusals above are the pin's doing).
: > "$LOG"
out=$("$SETUP" --check-pin "$OREF" "$TMP/clone" 2>&1); rc=$?
check "setup --check-pin accepts a good pin" $((rc == 0 ? 0 : 1)) "rc=$rc $out"

# 4. run: both refusals fire before javac/java, and a good pin proceeds past them.
: > "$LOG"; printf 'one input row\n' > "$TMP/in.jsonl"
out=$(STUB_ANCESTOR=1 "$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1); rc=$?
check "run refuses a non-ancestor ref (exit 2, names the half)" $([ "$rc" = 2 ] && has "$out" "not an ancestor" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
out=$(STUB_TREE_SUFFIX=-moved "$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1); rc=$?
check "run refuses a moved script tree (exit 2, names the half)" $([ "$rc" = 2 ] && has "$out" "cardsfolder differs" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
out=$(STUB_HEAD=3333333333333333333333333333333333333333 "$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1); rc=$?
check "run refuses a clone that is not at FORGE_ORACLE_REF" $([ "$rc" = 2 ] && has "$out" "is not at FORGE_ORACLE_REF" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
out=$(STUB_DIFF=1 "$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1); rc=$?
check "run refuses edited scripts under forge-gui/res (exit 2)" $([ "$rc" = 2 ] && has "$out" "uncommitted changes" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
# The preflight's verdict is read off the editions too, so an edited or
# untracked edition must fail the same guard as a card script.
check "  ...the dirty guard covers forge-gui/res/editions" \
	$(/usr/bin/grep -q 'diff --quiet HEAD -- .*forge-gui/res/editions' "$LOG" && echo 0 || echo 1) "$(/usr/bin/grep 'diff --quiet' "$LOG")"

: > "$LOG"
out=$(STUB_UNTRACKED=forge-gui/res/tokenscripts/extra.txt "$RUN" --compile 2>&1); rc=$?
check "run refuses untracked runtime scripts" $([ "$rc" = 2 ] && has "$out" "untracked scripts" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
check "  ...the untracked guard covers forge-gui/res/editions" \
	$(/usr/bin/grep -q 'ls-files --others -- .*forge-gui/res/editions' "$LOG" && echo 0 || echo 1) "$(/usr/bin/grep 'ls-files' "$LOG")"

# 4b. --preflight: on a good pin, the names the pinned cardsfolder cannot
# resolve, one grep pass, before any javac, java or scope. The three names
# the xengine farm caught as Forge harness "Couldn't find a card" stay
# refused here; Grizzly Bears is resolved; so are the split names "Fire //
# Ice" (a split card) and "Bonecrusher Giant // Stomp" (an adventure), whose
# card files hold one Name: line per face — the joined name is never a Name
# line, and a split with a missing face ("Fire // Nonesuch") is refused.
mkdir -p "$TMP/clone/forge-gui/res/cardsfolder/g" "$TMP/clone/forge-gui/res/cardsfolder/f" "$TMP/clone/forge-gui/res/cardsfolder/b" "$TMP/clone/forge-gui/res/cardsfolder/k" "$TMP/clone/forge-gui/res/cardsfolder/c"
printf 'Name:Grizzly Bears\nOracle:Grizzly Bears is a bear.\n' > "$TMP/clone/forge-gui/res/cardsfolder/g/grizzly_bears.txt"
printf 'Name:Fire\nName:Ice\nOracle:Fire deals 2 damage.\n' > "$TMP/clone/forge-gui/res/cardsfolder/f/fire_ice.txt"
printf 'Name:Bonecrusher Giant\nName:Stomp\nOracle:Bonecrusher Giant deals 4 damage.\n' > "$TMP/clone/forge-gui/res/cardsfolder/b/bonecrusher_giant.txt"
# A Funny edition names a split card by its joined name and an adventure by its
# main face (Forge keys a COMBINE card as "main // other" and an
# adventure/MDFC by its main face). Both have card files with a Name: line per
# face, so the old cardsfolder-only check passed them and the driver then threw
# "Couldn't find a card".
printf 'AlternateMode:Split\nName:Knowing\nOracle:Draw three cards.\n\nALTERNATE\n\nName:Half the Battle\nOracle:Search.\n' > "$TMP/clone/forge-gui/res/cardsfolder/k/knowing_half_the_battle.txt"
printf 'AlternateMode:Adventure\nName:Curious Pair\nOracle:Create a Food token.\n\nALTERNATE\n\nName:Treats to Share\nOracle:Create a Food token.\n' > "$TMP/clone/forge-gui/res/cardsfolder/c/curious_pair.txt"
# The cardsfolder is not the card DB: Forge's StaticData drops every card named
# in a Funny or silver-border edition outside its [eternal] section, and the
# driver then fails "Couldn't find a card" though the card file exists. Gobland
# and Mox Poison have a card file and only a Mystery Booster 2 playtest row;
# Indicate is in a silver-border set; Mardu Outrider has a file and an MB2
# [eternal] row (a legal reprint, kept); Plains is a basic land in a Funny set
# (kept). Lines carry the real file's shapes: collector number, rarity, @artist.
mkdir -p "$TMP/clone/forge-gui/res/editions" "$TMP/clone/forge-gui/res/cardsfolder/m" "$TMP/clone/forge-gui/res/cardsfolder/i" "$TMP/clone/forge-gui/res/cardsfolder/p"
printf 'Name:Gobland\nOracle:Gobland.\n' > "$TMP/clone/forge-gui/res/cardsfolder/g/gobland.txt"
printf 'Name:Mox Poison\nOracle:Mox.\n' > "$TMP/clone/forge-gui/res/cardsfolder/m/mox_poison.txt"
printf 'Name:Indicate\nOracle:Indicate.\n' > "$TMP/clone/forge-gui/res/cardsfolder/i/indicate.txt"
printf 'Name:Mardu Outrider\nOracle:Outrider.\n' > "$TMP/clone/forge-gui/res/cardsfolder/m/mardu_outrider.txt"
printf 'Name:Plains\nOracle:Plains.\n' > "$TMP/clone/forge-gui/res/cardsfolder/p/plains.txt"
printf '[metadata]\nCode=MB2\nName=Mystery Booster 2\nType=Funny\n\n[eternal]\n1 R Mardu Outrider @Evan Shipard\n\n[cards]\nF563 R Gobland @Anik Sikder\nF608 R Mox Poison @Dan Frazier\n300 L Plains @Someone\nRZ15 R Knowing // Half the Battle @\n1 C Curious Pair @Artist\n\n[tokens]\n1 Goblin\n' > "$TMP/clone/forge-gui/res/editions/Mystery Booster 2.txt"
printf '[metadata]\nCode=UNH\nName=Unhinged\nType=Expansion\nBorder=Silver\n\n[cards]\n1 C Indicate @Artist\n' > "$TMP/clone/forge-gui/res/editions/Unhinged.txt"
printf '[metadata]\nCode=M10\nName=Magic 2010\nType=Core\n\n[cards]\n1 C Grizzly Bears @Artist\n' > "$TMP/clone/forge-gui/res/editions/Magic 2010.txt"
: > "$LOG"
printf 'Grizzly Bears\nGobland\nIndicate\nMox Poison\nFire // Ice\nBonecrusher Giant // Stomp\nKnowing // Half the Battle\nKnowing\nCurious Pair // Treats to Share\nFire // Nonesuch\nMardu Outrider\nPlains\nNo Such Card\n' > "$TMP/names.txt"
out=$("$RUN" --preflight "$TMP/names.txt" 2>"$TMP/pf.err"); rc=$?
tab=$'\t'
check "preflight exits 0 on a good pin" $((rc == 0 ? 0 : 1)) "rc=$rc $(cat "$TMP/pf.err")"
filt="in the pinned cardsfolder but filtered from Forge's card DB (Funny/Silver-border edition card)"
check "  ...refusing Gobland, Indicate and Mox Poison (a card file the DB filters)" \
	$(printf '%s\n' "$out" | /usr/bin/grep -qxF "Gobland${tab}${filt}" &&
		printf '%s\n' "$out" | /usr/bin/grep -qxF "Indicate${tab}${filt}" &&
		printf '%s\n' "$out" | /usr/bin/grep -qxF "Mox Poison${tab}${filt}" && echo 0 || echo 1) "$out"
check "  ...refusing a name with no card file" \
	$(printf '%s\n' "$out" | /usr/bin/grep -qxF "No Such Card${tab}not in the pinned cardsfolder" && echo 0 || echo 1) "$out"
check "  ...keeping an [eternal] reprint and a basic land of a Funny set" \
	$(printf '%s\n' "$out" | /usr/bin/grep -q -e "Mardu Outrider" -e "^Plains" && echo 1 || echo 0) "$out"
check "  ...refusing a split whose face the pin lacks (Fire // Nonesuch)" \
	$(printf '%s\n' "$out" | /usr/bin/grep -qxF "Fire // Nonesuch${tab}not in the pinned cardsfolder" && echo 0 || echo 1) "$out"
check "  ...and keeping the card the pin resolves" $(printf '%s\n' "$out" | /usr/bin/grep -q "Grizzly Bears" && echo 1 || echo 0) "$out"
check "  ...and the split names the pin's per-face Name lines resolve" \
	$(printf '%s\n' "$out" | /usr/bin/grep -q "Fire // Ice" && echo 1 || echo 0) "$out"
check "     ...including the adventure" \
	$(printf '%s\n' "$out" | /usr/bin/grep -q "Bonecrusher Giant // Stomp" && echo 1 || echo 0) "$out"
check "  ...refusing a split a Funny edition names joined (Knowing // Half the Battle)" \
	$(printf '%s\n' "$out" | /usr/bin/grep -qxF "Knowing // Half the Battle${tab}${filt}" && echo 0 || echo 1) "$out"
check "  ...refusing a split named by its bare first face (Knowing)" \
	$(printf '%s\n' "$out" | /usr/bin/grep -qxF "Knowing${tab}${filt}" && echo 0 || echo 1) "$out"
check "  ...refusing an adventure whose main face a Funny edition names (Curious Pair)" \
	$(printf '%s\n' "$out" | /usr/bin/grep -qxF "Curious Pair // Treats to Share${tab}${filt}" && echo 0 || echo 1) "$out"
reached; check "  ...before javac, java or scope" $((! $? ))

# The StaticData filter is the reason the preflight reads the editions: a tree
# without them cannot answer, so the preflight refuses instead of falling back
# to the old cardsfolder-only answer. A symlinked editions dir must still be
# read (find -H).
mv "$TMP/clone/forge-gui/res/editions" "$TMP/clone/forge-gui/res/editions.real"
ln -s editions.real "$TMP/clone/forge-gui/res/editions"
out=$("$RUN" --preflight "$TMP/names.txt" 2>&1); rc=$?
check "preflight follows a symlinked editions dir" \
	$([ "$rc" = 0 ] && printf '%s\n' "$out" | /usr/bin/grep -qxF "Gobland${tab}${filt}" && echo 0 || echo 1) "rc=$rc $out"
rm "$TMP/clone/forge-gui/res/editions"
mv "$TMP/clone/forge-gui/res/editions.real" "$TMP/clone/forge-gui/res/editions"
mv "$TMP/clone/forge-gui/res/editions" "$TMP/clone/forge-gui/res/editions.away"
: > "$LOG"
out=$("$RUN" --preflight "$TMP/names.txt" 2>&1); rc=$?
check "preflight refuses a tree without editions (exit 2, not a silent skip)" \
	$([ "$rc" = 2 ] && has "$out" "needs the pinned editions" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
mv "$TMP/clone/forge-gui/res/editions.away" "$TMP/clone/forge-gui/res/editions"

# 5. run with a good pin: javac once per source sha, java with the pinned env.
: > "$LOG"
out=$("$RUN" --compile 2>&1); rc=$?
check "run --compile on a good pin succeeds" $((rc == 0 ? 0 : 1)) "rc=$rc $out"
check "  ...and compiled once" $([ "$(count javac)" = 1 ] && echo 0 || echo 1) "javac calls: $(count javac)"
"$RUN" --compile >/dev/null 2>&1
check "unchanged source reuses the stamp (no second javac)" $([ "$(count javac)" = 1 ] && echo 0 || echo 1) "javac calls: $(count javac)"
echo 'class B {}' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/B.java"
"$RUN" --compile >/dev/null 2>&1
check "a changed source recompiles" $([ "$(count javac)" = 2 ] && echo 0 || echo 1) "javac calls: $(count javac)"
"$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1
check "run starts the JVM on the driver main class" \
	$(/usr/bin/grep -qE "^java .*forge.oracle.ScenarioReplay .*/input\\.00000000 .*/output\\.00000001" "$LOG" && echo 0 || echo 1) "$(/usr/bin/grep '^java' "$LOG")"
check "  ...with pinned runtime res and ref" \
	$(/usr/bin/grep -qF "runtime res=$TMP/clone/forge-gui/res ref=$OREF" "$LOG" && echo 0 || echo 1)

# 6. FORGE_ORACLE_SRC replays a candidate worktree under its own HEAD, in its own class dir.
mkdir -p "$TMP/cand/forge-gui/res" "$TMP/cand/forge-oracle/src/main/java/forge/oracle" "$TMP/cand/forge-core/src/main/java/forge/util"
echo 'class C {}' > "$TMP/cand/forge-oracle/src/main/java/forge/oracle/C.java"
echo 'class ForcedDieRandom { int nextInt(int bound) { return 0; } void arm(int step) { req.coinFlips(step); } }' > "$TMP/cand/forge-oracle/src/main/java/forge/oracle/ForcedDieRandom.java"
echo 'class ForgeEngine { Object r = new ForcedDieRandom(); }' > "$TMP/cand/forge-oracle/src/main/java/forge/oracle/ForgeEngine.java"
echo 'class StepMachine { void f() { dice.arm(0); } }' > "$TMP/cand/forge-oracle/src/main/java/forge/oracle/StepMachine.java"
echo 'class Request { String coin_flips; }' > "$TMP/cand/forge-oracle/src/main/java/forge/oracle/Request.java"
echo 'class ScriptedController { boolean call(StepMachine m) { return m.req.coinFlips(m.step()).isEmpty(); } }' > "$TMP/cand/forge-oracle/src/main/java/forge/oracle/ScriptedController.java"
echo 'class MyRandom { int f() { return delegate.nextInt(bound); } boolean g() { return delegate.nextBoolean(); } }' > "$TMP/cand/forge-core/src/main/java/forge/util/MyRandom.java"
: > "$LOG"
CREF=3333333333333333333333333333333333333333
check "candidate HEAD differs from the pinned ref (precondition)" $([ "$CREF" != "$OREF" ] && echo 0 || echo 1)
rel=$(realpath --relative-to="$PWD" "$TMP")
out=$(FORGE_ORACLE_SRC=$rel/cand/forge-oracle FORGE_ORACLE_CLASSES=$rel/cand-classes FORGE_ORACLE_TMP=$rel/java-tmp STUB_TOP=$TMP/cand STUB_HEAD=$CREF "$RUN" "$TMP/in.jsonl" "$TMP/cand-out.jsonl" 2>&1); rc=$?
check "relative FORGE_ORACLE_SRC compiles and runs the candidate" $([ "$rc" = 0 ] && [ -n "$(ls "$TMP/cand-classes" 2>/dev/null)" ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...using candidate res, HEAD and absolute tmpdir" \
	$(/usr/bin/grep -qF "runtime res=$TMP/cand/forge-gui/res ref=$CREF cwd=$TMP/java-tmp" "$LOG" && /usr/bin/grep -qF -- "-Djava.io.tmpdir=$TMP/java-tmp" "$LOG" && echo 0 || echo 1)
out=$(FORGE_ORACLE_SRC=$TMP/cand/forge-oracle STUB_TOP=$TMP/cand STUB_HEAD=$CREF STUB_ANCESTOR=1 "$RUN" --compile 2>&1); rc=$?
check "FORGE_ORACLE_SRC is still pin-checked (exit 2)" $((rc == 2 ? 0 : 1)) "rc=$rc $out"

# 7. a driver ref without the recorded-die force is refused before any build,
# so a pin bump cannot silently roll Forge's own die.
NOD=$TMP/nofdr
mkdir -p "$NOD/forge-gui/res" "$NOD/forge-oracle/src/main/java/forge/oracle" "$NOD/forge-core/src/main/java/forge/util"
echo 'class D {}' > "$NOD/forge-oracle/src/main/java/forge/oracle/D.java"
echo 'class ForgeEngine { Object r = new ForcedDieRandom(); }' > "$NOD/forge-oracle/src/main/java/forge/oracle/ForgeEngine.java"
echo 'class StepMachine { void f() { dice.arm(0); } }' > "$NOD/forge-oracle/src/main/java/forge/oracle/StepMachine.java"
echo 'class MyRandom { int f() { return delegate.nextInt(bound); } boolean g() { return delegate.nextBoolean(); } }' > "$NOD/forge-core/src/main/java/forge/util/MyRandom.java"
: > "$LOG"
out=$(FORGE_ORACLE_SRC=$NOD/forge-oracle FORGE_ORACLE_CLASSES=$TMP/nofdr-classes FORGE_ORACLE_TMP=$TMP/nofdr-tmp STUB_TOP=$NOD STUB_HEAD=$CREF "$RUN" --compile 2>&1); rc=$?
check "run refuses a driver without the step-scoped die force (exit 2)" \
	$([ "$rc" = 2 ] && has "$out" "no step-scoped die force" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))

# 8. a driver ref without the recorded-coin force is refused before any build,
# so a pin bump cannot silently flip Forge's own coin.
NOC=$TMP/nocoin
mkdir -p "$NOC/forge-gui/res" "$NOC/forge-oracle/src/main/java/forge/oracle" "$NOC/forge-core/src/main/java/forge/util"
echo 'class E {}' > "$NOC/forge-oracle/src/main/java/forge/oracle/E.java"
echo 'class ForcedDieRandom { int nextInt(int bound) { return 0; } }' > "$NOC/forge-oracle/src/main/java/forge/oracle/ForcedDieRandom.java"
echo 'class ForgeEngine { Object r = new ForcedDieRandom(); }' > "$NOC/forge-oracle/src/main/java/forge/oracle/ForgeEngine.java"
echo 'class StepMachine { void f() { dice.arm(0); } }' > "$NOC/forge-oracle/src/main/java/forge/oracle/StepMachine.java"
echo 'class MyRandom { int f() { return delegate.nextInt(bound); } }' > "$NOC/forge-core/src/main/java/forge/util/MyRandom.java"
: > "$LOG"
out=$(FORGE_ORACLE_SRC=$NOC/forge-oracle FORGE_ORACLE_CLASSES=$TMP/nocoin-classes FORGE_ORACLE_TMP=$TMP/nocoin-tmp STUB_TOP=$NOC STUB_HEAD=$CREF "$RUN" --compile 2>&1); rc=$?
check "run refuses a driver that does not read item.coin_flips (exit 2)" \
	$([ "$rc" = 2 ] && has "$out" "does not read item.coin_flips" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
# Request with the reader but a ForcedDieRandom that never arms it is a
# different refusal, naming the wiring half.
echo 'class Request { String coin_flips; }' > "$NOC/forge-oracle/src/main/java/forge/oracle/Request.java"
echo 'class ScriptedController { boolean call(StepMachine m) { return m.req.coinFlips(m.step()).isEmpty(); } }' > "$NOC/forge-oracle/src/main/java/forge/oracle/ScriptedController.java"
: > "$LOG"
out=$(FORGE_ORACLE_SRC=$NOC/forge-oracle FORGE_ORACLE_CLASSES=$TMP/nocoin-classes FORGE_ORACLE_TMP=$TMP/nocoin-tmp STUB_TOP=$NOC STUB_HEAD=$CREF "$RUN" --compile 2>&1); rc=$?
check "run refuses a driver that never arms the coin force (exit 2)" \
	$([ "$rc" = 2 ] && has "$out" "does not wire the coin force" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))

[ "$fails" = 0 ] && echo "forge_oracle_pin_smoke: all passed" || echo "forge_oracle_pin_smoke: $fails FAILED"
exit $((fails != 0))
