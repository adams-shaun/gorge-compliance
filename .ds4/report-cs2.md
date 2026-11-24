# Round 2: Forge follower registry omitted suspend

## What changed (finding: MAJOR follower.go:104)
- `oracle/xengine/follower/follower.go`: `"suspend"` added to `forgeCastModes` (and its comment), so `Lookup("forge").FollowsCastMode("suspend")` is true and `classify` no longer files a divergence at or after a suspend cast as `harness`.
- `oracle/xengine/follower/follower_test.go`: `TestPreH1Behaviour` pins the full Forge list with `suspend`; `TestFollowsCastMode` pins `suspend: true`.
- `oracle/xengine/classify/classify_test.go`: `TestClassifyForgeCastMode` now includes `suspend` among the modes the driver follows (a difference after it stays `diverge`).
- Docs/comments naming the mode list: `docs/engines/protocol.md` (cast_mode list) and `cmd/oraclediff/forge.go` comment.
- phase.rs and manabrew lists intentionally unchanged (their drivers do not act on suspend; `TestPhaseEntry`/`TestManabrewEntry` still pin that).

## Sibling check
`/usr/bin/grep -rn 'forgeCastModes|FollowsCastMode|CastModes|castModeSupported'` over `oracle/xengine`, `cmd`, `docs`: the consumers of the Forge cast-mode list are `follower.go` (registry), `classify.go:520` (reads it via `FollowsCastMode`, no list of its own), and the tests/docs updated above. `classify` derives from the registry, so the single list edit covers it. `cmd/oraclediff/forge.go` mentions a stale `forgeElectedCastModes` name (no such symbol); left as is.

## Commands
- `make test-one PKG=./oracle/xengine/follower RUN='^(TestPreH1Behaviour|TestFollowsCastMode|TestEntriesWellFormed)$'` -> `test-one: PASS ./oracle/xengine/follower`
- `make test-one PKG=./oracle/xengine/follower RUN='^(TestFollowsCastMode|TestForgeEntry)'` -> PASS
- `make test-one PKG=./oracle/xengine/classify RUN='^TestClassifyForgeCastMode$'` -> `test-one: PASS ./oracle/xengine/classify`
The round-1 driver gates (`make xmage-driver-tests`, fork suite) were not re-run; this round touches no driver code.

## Gorge finding
None.

## Issues
None unfixed.
