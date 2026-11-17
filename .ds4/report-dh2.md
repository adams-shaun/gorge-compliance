# Report dh2 — xmage driver: cast Adventure/Omen halves from a parent-card step

## What was wrong

The previous round only added `adventure_alt` / `omen_alt` to `castModeSupported`. The farm's
cast step names the **parent** card (`{"op":"cast","card":"p1:Grabby Giant","cast_mode":"adventure_alt"}`),
because the xengine ref binder keeps the dealt card's name. The cast arm therefore cast the
creature front instead of the inset spell half.

## What changed

- `oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java`
  - New `spellOptionCastName(String)` resolves an Adventure/Omen **parent** name to the inset spell
    card's XMage name (`CardWithSpellOption#getSpellCard().getName()`: "Stomp", "Absorb Essence",
    "That's Mine", "Desperate Parry"). It matches the XMage **role/type**, not a card list, so every
    Adventure and Omen card — including future printings — is covered by the same lookup.
  - The `case "cast":` arm now resolves that name for `adventure_alt`/`omen_alt` and casts the half.
    A step that already names the half falls through to `castSpelling` unchanged, so both shapes work.
    No other cast mode is touched.
  - `castModeSupported`'s comment no longer claims gorge's step names the half; it documents the
    parent-ref shape and the resolution.
- `oracle/xmageoracle/test/org/mage/test/oracle/ScenarioReplayCastModeTest.java`
  - `stackHas` now takes the **farm's shape** (`card:'p0:Bonecrusher Giant'` / `card:'p0:Purging
    Stormbrood'` with the cast_mode) and asserts the half is on the stack **and the parent is not**.
  - Keeps one half-named step (`p0:Stomp`) as the backwards-compatible shape.
  - Doc comment no longer says "without H2 or a game".
- `scripts/xmage-oracle-test-cast-mode.sh`
  - Doc comment now says the Adventure/Omen cases open H2 and replay a real one-step game.

## Verification

`XMAGE_ORACLE_DIR=/home/sadams/.cache/xmage-oracle-4142596` because the XMage build is out of tree;
`XMAGE_ORACLE_PRIVATE_DB=1` gives the JVM its own H2 copy (the shared-DB startup failure that stopped
the previous round).

### 1. Cast-mode driver test

```
$ XMAGE_ORACLE_DIR=/home/sadams/.cache/xmage-oracle-4142596 bash scripts/xmage-oracle-test-cast-mode.sh .coverage/xmage-driver/cast-mode
Note: ScenarioReplay.java uses or overrides a deprecated API.
PASS mayplay cast_mode is accepted (cast from exile under a play permission)
PASS plot_cast stays rejected (kicked too)
PASS adventure_alt and omen_alt are accepted
PASS adventure_alt casts Stomp by its own name from a parent-card step; it is on the stack
PASS omen_alt casts Absorb Essence by its own name from a parent-card step; it is on the stack
PASS adventure_alt casts Stomp by its own name from a half-named step; it is on the stack
PASS an absent cast_mode is the ordinary cast
EXIT=0
```

The two parent-card cases are the new regression: with the old cast arm they would cast the creature
front and fail both the "half is on the stack" and "parent is not" assertions.

### 2. Driver contract test

```
$ XMAGE_ORACLE_DIR=/home/sadams/.cache/xmage-oracle-4142596 bash scripts/xmage-oracle-test-driver-contract.sh .coverage/xmage-driver/contract
PASS attach-ask choice (scripted first/non-first, consume, successive, unique auto-select, ambiguous, unmatched, skip, idempotent step)
PASS move destination mapping (exile->EXILED) and multi-zone source search
PASS pass mapping and queue order (pair resolves after its second pass; lone p0 pass yields before the p1 cast checkpoint; p1-first pair resolves after its second pass; other shapes rejected; empty-stack failure self-diagnosing)
PASS recorded seat-1 resolving pass (its own pass before a later flash cast; the active seat only waits)
PASS stepless pass_to keeps the tracked phase
PASS Spree detection (SpreeAbility on the card, not a name list)
PASS must-attack detection (AttacksEachCombatStaticAbility on the card, not a name list)
PASS Bargain election (cast_mode acceptance, sacrifice recognition, pay/consume/decline, recorded sacrifice pick)
PASS craft material fill (structural recognition, fill decision)
PASS copy-cost ops (copy-cost modes, prepared copy, divided activation shares, teamwork tap, gained tap-mana text)
PASS split/Room spellings (probe whole-name cast, back-half spell alias, whole object name rewrite, pass-through)
PASS seat counts (absent/2 duel, 3 free-for-all, others refused)
PASS owner-qualified graveyard target (seat-scoped rank choice, exact owner card, joined @ref answers)
EXIT=0
```

This includes the existing positive checks `castModeSupported("adventure_alt") == "true"` and
`castModeSupported("omen_alt") == "true"` in the "Bargain election (cast_mode acceptance, ...)" block.

### 3. oraclegen cast-arm test

```
$ make test-one PKG=./oracle/compliance/oraclegen RUN='^TestDriverPlotCast$'
test-one: PASS 736a9f140e+dirty ./oracle/compliance/oraclegen 2s
EXIT=0
```

### 4. Repro sweep — 0 `cast_mode adventure_alt unsupported`

```
$ XMAGE_ORACLE_DIR=/home/sadams/.cache/xmage-oracle-4142596 XMAGE_ORACLE_PRIVATE_DB=1 \
    XENGINE_FOLLOWERS=xmage bash scripts/xengine-sweep.sh --run adv --sets WOE --chunks 1 --games 10 --seed 111 --force
== xengine sweep adv: root .../.oracle-build/xengine/adv, 1 chunk(s) to do
== WOE chunk 0: play
== WOE chunk 0: xmage
WOE/0: done
EXIT=0

$ /usr/bin/grep -rl --include=REPRO.md 'cast_mode adventure_alt unsupported' .oracle-build/xengine/adv | wc -l
0
$ /usr/bin/grep -rl --include=REPRO.md 'cast_mode omen_alt unsupported' .oracle-build/xengine/adv | wc -l
0
```

(The final `grep` exits 1 on no match, hence `wc` prints `0`; the sweep itself exited 0.)

The default sweep sends window items only (`XENGINE_XMAGE_TAPES=0`), and this sample's 3
`adventure_alt` steps are tape items. To exercise a real farm item end to end I also ran the same
sweep with tapes enabled:

```
$ XMAGE_ORACLE_DIR=... XMAGE_ORACLE_PRIVATE_DB=1 XENGINE_FOLLOWERS=xmage XENGINE_XMAGE_TAPES=1 \
    bash scripts/xengine-sweep.sh --run adv-tapes --sets WOE --chunks 1 --games 10 --seed 111
WOE/0: done
EXIT=0
$ /usr/bin/grep -rl --include=REPRO.md 'cast_mode adventure_alt unsupported' .oracle-build/xengine/adv-tapes | wc -l
0
$ /usr/bin/grep -rl --include=REPRO.md 'cast_mode omen_alt unsupported' .oracle-build/xengine/adv-tapes | wc -l
0
```

For farm item `xe:WOE:0:5:tape` (step 134: `{"op":"cast","seat":0,"card":"p0:Obyra's Attendants",
"targets":["p1:Warehouse Tabby"],"cast_mode":"adventure_alt"}`) the XMage snapshot stack is
`['Aquatic Alchemist', 'Desperate Parry']` at snapshots 135/136 and `['Desperate Parry']` at
137/138 — the half, never `Obyra's Attendants`. The classified divergence's own snapshots agree:
`gorge.json` has `"source":"p0:Desperate Parry"` and `xmage.json` has
`"source":"Desperate Parry"`. The row's later harness text is an unrelated `Freeze in Place` scry
choice (`Found wrong choice command ... TargetCard: Select a card ORDER ...`), not a cast-mode row.
The two `Grabby Giant` tape steps in this sample have no XMage row (xprep refused them before replay,
as it does for some tape items); the parent-card unit cases cover that card shape directly.

No `gorgeSpellingRule` change was needed: neither the parent name nor the half name contains
`" // "`, and the test/sweep show XMage's stack source ("Stomp", "Absorb Essence", "Desperate Parry")
comparing equal to gorge's.

## Contract changes

None. Only `oracle/xmageoracle/` and its test script changed; no follower, ledger, ratchet or
generated output was touched.

## Issues

None outstanding. All five findings from `findings-dh2.md` are addressed:
the cast arm now resolves the half for parent-card steps; the unit test uses the farm shape and
asserts the parent is not cast; Done-means item 4 is verified plus a real farm tape replay; both
stale doc comments are corrected; and the `castModeSupported` comment is accurate.
