# pf-decks-03: carddb need --by decks

## Built
- `query.py`: `NEED_DECKS_RANK_SQL` (verbatim from the brief) and the `--by decks` branch of `run_need`
  (LEFT JOIN of the rank SQL, columns `unlocked blocked weight` first, the brief's order, LIMIT last).
  The tier path is untouched. Missing `deck_sample`/`decks` (and `card_proven`) exits 1 with
  `need: --by decks needs the private build with the deck sample (no <table> table)`.
- `cli.py`: `need --by {tier,decks}` (default tier) and the usage line. The verb table lives in the
  `cli.py` usage text (carddb/README.md has no verb table), so that is where the row went.
- `tests/test_query.py`: `TestNeedByDecks` (order, heldout never read, ranking spelling,
  default/--by tier unchanged, missing private tables).

## Commands
- `make carddb-test` -> `Ran 218 tests in 21.727s  OK (skipped=5)` (existing need goldens unchanged).
- `python3 -I cmd/carddb/main.py need --by decks` -> `carddb need: need: --by decks needs the private build
  with the deck sample (no deck_sample table)`, exit=1 (the CLI adds its own `carddb need:` prefix).

## Contract changes
None.

## Issues
None.
