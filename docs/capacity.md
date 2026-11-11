# capacity: the ledger of dirac's table stalls

dirac (gorge's fixed-size kernel) holds a game in bounded tables: the stack, pending triggers, delayed
triggers, continuous effects, last-known information and so on. When a game needs more rows than a
table has, the kernel does not grow it or play on wrong: it reports a **capacity stall** naming the
table, and the lockstep run (`kfuzz`, gorge against dirac, every decision compared) counts the game as
`capacity` instead of clean or divergent. Each stall is a question: is this realistic play that the
table should hold (raise it), or a degenerate or exponential board (declare the limit and leave it)?

`scripts/capacity-ledger.py` keeps the numbers behind those calls: per measure, how many games stalled on
which table, so a raise can be checked against the next measure and a declared limit's rate watched.

## Feeding it

A measure is a directory of kfuzz outputs, one per pool, surface and mulligan setting, named
`<pool>.<surface>.m<mulligans>.out`:

```sh
GORGE_POOLS=<pools> kfuzz -synth std-fra -pairs 1500 -workers 2 -seed 1 -shrink 0 -mulligans 1 \
  -surface autopay -keep-going -implicates-all > measure/std-fra.autopay.m1.out 2>&1
python3 -I scripts/capacity-ledger.py measure <gorge sha> random capacity.tsv
```

The ledger reads the last summary line of each file,

```
std-fra: 1500 games (0 refused-card, 0 refused-pair, 0 error), 1496 clean, 0 divergent, 0 unmodelled [], 4 capacity [dynmana=4], 0 hang (lead gorge, surface autopay)
```

and appends one row per table in the `capacity [...]` brackets. A file with no stall writes no row, so a
clean measure leaves the ledger as it was; the measure's own summary is the record of the games played.
The ledger path is the fourth argument or `$CAPACITY_LEDGER`; a new file gets a header row. Measures and
ledgers stay outside the repository.

## Columns

| Column | What |
|---|---|
| `sha` | the gorge commit measured (as given) |
| `policy` | the bot policy driving both engines (as given; `random` for the lockstep sweeps) |
| `pool` | the pool, from the file name (`std-fra`) |
| `surface` | `full` or `autopay`, from the file name |
| `mull` | the mulligan setting, from the file name |
| `games` | games in the measure for that pool and surface |
| `table` | the dirac table that stalled (`pending`, `stack`, `dynmana`, ...) |
| `stalls` | games that stalled on it |

Rows for one `sha` against an earlier one show whether a raise cleared its stall; a declared limit (an
exponential token board, say) shows as a steady rate across shas. The decisions themselves (what was
raised, what was declared and why) are kept in the operator's limits log beside the ledger, not here.
