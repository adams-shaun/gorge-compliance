# Report cs0
Added "morphed", "megamorphed", "disguised" to `forgeCastModes` (oracle/xengine/follower/follower.go) and updated its comment to name the face-down casts. Also updated follower_test.go: `TestPreH1Behaviour` pins the exact forge mode list (it failed after the registry change), and the FollowsCastMode table now covers the new modes.

Commands (raw `go test` is blocked in seats, so `make test-one`):
- `make test-one PKG=./oracle/xengine/follower RUN='^TestFollowsCastMode$'` -> PASS
- `make test-one PKG=./oracle/xengine/follower RUN='.'` -> PASS
- Every other ./oracle/xengine/* package with tests, one by one (adjudicate, certdeck, classify, cov, decks, dside, fshadow, game, lower, pincap, scen, xprep) -> PASS. rowio has no tests (EMPTY).

## Issues
None.
