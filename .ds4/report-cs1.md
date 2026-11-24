# Report: xp-xmage-drawn-land-alias

## What I built
`oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java`:

1. **Bind the played land's ref to the permanent the play created** (`bindEnteredLand`, `noteBattlefield`,
   `playUnaliasedLand`). Every `play` step now snapshots the battlefield ids just before the play
   and, right after, finds the new permanent the play entered. If the ref's alias names no live
   battlefield permanent, it is moved onto the new permanent. A ref already bound to a live
   permanent is left alone, so setup-hand lands behave as before.
   - Root cause was slightly wider than the brief's "no alias": `registerAliases` also aliases the
     setup `library_top` cards. Island#8 therefore *had* an alias, but to library card #8, and the
     by-name play entered a different Island. The brief's sketch (bind only when `refAlias` lacks the
     ref) did not fix the example: I measured that bind-only-on-missing-alias still fails step 20.
     Binding on "alias does not name a live permanent" does fix it.
2. **Name fallback in `namedManaSources` skips a land a later step names** (`laterNamedOrdinals`).
   The fallback "any untapped one of the name" no longer takes the Nth same-named permanent when a
   later `activate` step of the same seat and name names ordinal N explicitly. It still falls back to
   any untapped one if every unreserved land is tapped (the old behaviour).

Structural choice: one shared helper (`bindEnteredLand`) serves both the aliased and unaliased play
paths, and the reserve rule derives from the scenario's own later steps rather than a list, so another
mid-window land (drawn, returned or searched) takes the same path.

## Test
New 6th row `manascope-drawn-island-played` in `oracle/xmageoracle/test/fixtures/mana-scope.jsonl`
(the example find's scenario, library trimmed) and assertions in
`ScenarioReplayManaAbilityScopedToCardTest`: after step 7 there are 7 Islands, 4 tapped; after step 20
all 7 are tapped, pool `UUU`. The brief suggested a fresh synthetic scenario; there is no draw op in
the scenario schema, so the real Coil and Catch window is the draw.

## Commands and output
No in-tree XMage build exists in this worktree, and the shared builds under `/mnt/sata` are read-only
and at other refs (none is the pinned `33704ee4`). I used a scratch root of symlinks
(`/tmp/dla/xroot2` -> `/mnt/sata/gorge-training/xmageoracle-next`, ref `fe075157`) with
`XMAGE_ORACLE_PRIVATE_DB=1`. `make xmage-driver-tests` itself was not run (it needs
`.oracle-build/xmage`); I ran its per-test scripts directly.

Baseline (HEAD driver) on the example find (`scenario.json` copied to a JSONL):
```
old: harness: activate step 20 on Island has no mana ability for "{T}: Add {U}." at ability_index 0   (21 snapshots)
new: harness: None                                                                                      (24 snapshots)
```
Bind alone (reserve rule disabled): replays clean. Reserve rule alone (bind not yet fixed): also
replays clean. Each part independently fixes the example.

`bash scripts/xmage-oracle-test-mana-scope.sh`:
```
PASS mana ability is scoped to the named source (Hidden Grotto not Island, pool C; Swamp#2 not Swamp#1, pool B; a played Plains#2 is still the named permanent; Simian Spirit Guide is exiled from hand for R, even with a same-named battlefield copy; a drawn, played Island#8 is tapped, not a setup Island)
```

Regression run of every `scripts/xmage-oracle-test-*.sh` (35 scripts, scratch root above): all rc=0
except `convoke-pip`, which failed once with `Couldn't load deck, deck size=0` (a transient card-DB
load, not the play path) and passed on rerun:
```
PASS convoke-pip-calamity.jsonl leaves R after the recorded convoke tap pays the coloured pip
PASS convoke-pip-scatter.jsonl leaves G after the recorded convoke tap pays the coloured pip
```

## Issues
- `make xmage-driver-tests` and the third "Done means" item (next xproof cycle) are not verifiable
  here; the operator must rerun against the pinned build.
- `make boundary` is not runnable in this seat (raw `go test` is blocked); no Go file changed.
