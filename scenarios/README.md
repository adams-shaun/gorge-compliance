# Scenarios

The scenario corpora gorge's runner plays: hand scenarios in `oracle/` and translated XMage
tests in `xmageport/`. Read this to add one. [docs/oracles.md](../docs/oracles.md) says what
they can and can't show.

## Add a hand scenario

1. Read the card, not gorge's script for it:

   ```sh
   go run ./cmd/oraclepacket "Fatal Push"
   ```

   That prints the printed view: name, cost, type line, P/T, Oracle text and `oracle_sha`.
   Don't commit its output; Oracle text stays out of this repository. `.oracle-packets/` is gitignored.

2. Write `scenarios/oracle/<family>/<card-slug>.json`, in an existing family directory or a new one:

   ```json
   {
    "card": "Boltwave",
    "family": "spell-conditional",
    "author": "Oracle + CR only",
    "oracle_sha": "be953e7e16fa",
    "scenarios": [
     {
      "name": "three-to-each-opponent",
      "cr": ["120.3"],
      "why": "3 damage to each opponent; its caster and creatures are untouched.",
      "setup": {
       "p0": {"battlefield": ["Grizzly Bears"], "hand": ["Boltwave"]},
       "p1": {"battlefield": ["Grizzly Bears"]}
      },
      "steps": [
       {"op": "cast", "seat": 0, "card": "p0:Boltwave", "mana": "R"},
       {"op": "resolve"}
      ],
      "expect": [{"life": {"p0": 20, "p1": 17}}, {"card": "p1:Grizzly Bears", "damage": 0}]
     }
    ]
   }
   ```

   Derive the expectations from the printed card and the Comprehensive Rules alone. If gorge
   then disagrees, the scenario is doing its job: don't change it to match gorge. The schema is gorge's
   (`oracleScenario` in `engine/rules/oracle_run.go`). Its
   [authoring rules](https://github.com/adams-shaun/gorge/blob/main/docs/design/2026-09-27-oracle-text-card-audit.md)
   are in gorge.

3. Check it:

   ```sh
   go test ./audit -run TestOracleScenarioFilesWellFormed     # the schema; needs no corpus
   make oracle-audit ORACLE_RUN=Boltwave                      # plays it; the filter uses _ for spaces
   ```

A scenario file lives in one directory only: here or in gorge's
`engine/rules/testdata/oracle/`, which `audit` also reads.

A scenario that restates a worked example of the Comprehensive Rules goes in the `cr-examples/`
family and restates the example in its own words (never copy the rules text). `cr-examples/LEDGER.tsv`
has one row per example in the rules text; in the same change that adds the scenario, move that
example's row from `open` to `covered` and name the scenario as `<file stem>#<scenario name>`. One
example can have several scenarios (a control case, the opposite polarity, another facet), so a
covered row names each of them, separated by `;`. `TestCRExampleLedger` fails if a covered row names a
scenario that does not exist or does not cite the row's rule, if a scenario there is named by no row,
or if an `open` row's rule is already cited by a scenario there.
`scripts/cr_example_ledger.py` diffs the ledger against the rules text by hand when the pinned text
changes; the rules text itself is never committed here.

## When a scenario fails

A failure is either a wrong scenario or a gorge bug. Fix a wrong scenario. For a bug, fix gorge if
you can. Otherwise, once triage has confirmed gorge is wrong (a row isn't a way to make a new scenario
pass), record it in `<family>/known-divergent/<card-slug>.json`, in the same family, with the rule and
what was observed:

```json
{"card": "Basalt Monolith", "rows": {"<scenario name>": "CR 602.2b: ... Observed: ..."}}
```

The row must then keep failing: once gorge is fixed the audit fails until the row is deleted.
`known-unconsumed/` has the same shape and lists scenarios that leave a scripted answer unused. Its
rows are only ever deleted.

## Four seats

[`padseats.tsv`](padseats.tsv) records every scenario here that its two seats see differently
when two inert seats are added, or that doesn't seat two, with a `class` and rule (`cr`) once
triage has explained the row. `make padseats` re-runs every scenario and fails on a missing, stale
or changed row; `go run ./cmd/padseats -write` rewrites the record, keeping the triage of unchanged
rows. A new scenario that differs at four seats arrives with its row.

## XMage ports

`xmageport/<family>/<Class>.json` holds translations of XMage's Mage.Tests methods; each
scenario's `xmage` field names its `Class#method`, and `NOTICE` carries the licence.
`cmd/xmageport` writes them with `-commit scenarios/xmageport`. A method that fails on a gorge bug
carries `known_bug`, with the reason in `cmd/xmageport/known_bugs.tsv`, and the row must keep
failing until gorge is fixed.
