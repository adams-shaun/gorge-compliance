# Report cs4 (round 4)
- MAJOR fixed: `funny_face` now starts with a prefilter `grep -qF -e "$1 // " <<<"$funny" || return 1`. The per-file cardsfolder grep runs only for names that appear before " // " in a filtered joined name (Knowing, Start, Bind, ...). Per-name cost is back to a grep over the precomputed string. Round-3 semantics are unchanged, because the prefilter is a superset test.
- MINOR fixed: the comment above the filter now points at funny_face's per-file rule.
- MINOR (Go case runs through the fake runner) is left as is, as the review said. The smoke is the real guard.
- Not re-measured: I did not re-time the 405-name run on the Forge clone.
## Gates
- `bash ops/scripts/tests/forge_oracle_pin_smoke.sh`: "forge_oracle_pin_smoke: all passed"
- `make test-one PKG=./cmd/oraclediff RUN='Preflight|TestForgeExportRefusesUnknownCard'`: PASS
## Issues
None.
