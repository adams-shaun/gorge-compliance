# Report (round 2): xp-forge-driver-unless-energy-confirm

## What changed
Round 1 pinned `c12c06bcef`, which lacked develop's `1eef2a952a` (zero-candidate decline). Fixed on the fork and here:
- Fork (adams-shaun/forge, branch fix/xp-forge-driver-unless-energy-confirm): merged `1eef2a952a` into `c12c06bcef` (clean, no conflicts) -> `3494ec7d2bdc0ac257d891a230443c2b9b730ed2`, pushed to the fork only. Both `c12c06bcef` and `1eef2a952a` are ancestors.
- Driver suite on the merge (`forge-oracle/run-unit-tests.sh` under systemd-run 2G/200%): `Total tests run: 162, Passes: 162, Failures: 0, Skips: 0` (includes UnlessEnergyConfirmTest x2, ZeroCandidateConfirmTest x4, ForcedDieRollTest; `emptyHandAndGraveyardAurasAnswersNo` passes).
- `scripts/forge-oracle-setup.sh --check-pin 3494ec7d2b...` -> pin invariant holds.
- compliance: merged develop into the branch, resolved the two pin conflicts (Makefile comment block keeps 1eef, c12c and the merge; `forgeOracleDieRollRef` and its comment now record `3494ec7d2b` with both parents, noting c12c does NOT contain 1eef). `FORGE_ORACLE_REF` = `forgeOracleDieRollRef` = 3494ec7d2b.
- Replay of the Die Young find on the merged driver (`scripts/forge-oracle-run.sh`): `{'harness': None, 'strict_miss': None, 'leftover': None}`.
- `make test-one PKG=./cmd/oraclediff RUN='Forge'` -> PASS; `make smoke` -> all passed. `git merge-tree --write-tree develop HEAD` is clean.

## Concerns
- `make boundary` is blocked in seats (raw go test); not a brief gate. The "next xproof cycle" item is the operator's.
- The previous round's note "1eef2a952a still an open fork branch" was stale; develop already had it.

## Contract changes
None beyond the Makefile pin and the die-roll pin constant.

## Issues
None unfixed.
