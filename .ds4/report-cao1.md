# Report — padseats record guard

## What I built
- `internal/testselect/testselect.go` (`Packages`): any `scenarios/` path now selects `./cmd/padseats` as well as `./audit`. It matches on the prefix, so `scenarios/oracle/**`, `scenarios/xmageport/**` and `scenarios/padseats.tsv` are all covered, and so is any corpus added later.
- `internal/testselect/testselect_test.go` (`TestScenariosMapToAudit`): requires `[./audit ./cmd/padseats]` for an oracle scenario, an xmageport scenario and `scenarios/padseats.tsv`.
- `cmd/padseats/padseats_test.go`: new `TestPadSeatsRecordedKeysMatchRecord` replays every scenario key named in `scenarios/padseats.tsv`, whatever shard it falls in. It then runs `compare(rows, record, shard{0,1})`. Every measured key is a recorded key, so this reports:
  - a changed verdict or first_diff,
  - a row that is now `same` or `base-fails` (stale),
  - a key that names no scenario (stale, "no such scenario").
  It replays 142 of 142 keys in about 0.3 s. `TestPadSeatsSampleMatchesRecord` is unchanged and still catches new differences inside shard 0/64.

## Why this fixes the whole class
The record test takes its keys from the record itself, not from a hash shard, so no recorded row can sit outside the test. The selector matches the directory prefix, not a list of files.

## Evidence
I checked that the test fails on a bad record: I changed one first_diff in a temporary copy of `scenarios/padseats.tsv`, then put the file back. The test failed:
```
padseats_test.go:82: changed row: oracle/activated-ability/bloom-tender#noncreature-colored-permanent-counts	recorded differs "snap 0 p1.graveyard: [] != [Wastes Wastes]X", measured differs "sna...
padseats_test.go:84: replayed 142 of 142 recorded keys
--- FAIL: TestPadSeatsRecordedKeysMatchRecord (0.29s)
```
Gates, both run on the restored record:
```
$ make test-one PKG=./internal/testselect RUN='^TestScenariosMapToAudit$'
test-one: PASS 016ec3f5d3+dirty ./internal/testselect 1s
$ make test-one PKG=./cmd/padseats RUN='^TestPadSeats'
test-one: PASS 016ec3f5d3+dirty ./cmd/padseats 1s
```

## Contract changes
None. `internal/testselect` is a shared helper; the change only adds packages to its output.

## Issues
None.
