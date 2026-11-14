# First joint follower sweep: phase.rs and Manabrew together

One xengine chunk (set FDN, chunk 0, seed 1, 4 games) replayed through both followers at once
and classified against gorge, to see how the two followers relate item by item. Smoke-sized:
a plumbing record and a first look, not a rate estimate.

## Command

```
scripts/follower-sweep-smoke.sh 'phase manabrew' --games 4
```

(run under the script's own systemd-run cap, 4G / 400%; output in
`.oracle-build/xengine/smoke-phase-manabrew/`, gitignored; exit 0.)

## Pins (read at run time)

- `MANABREW_ORACLE_REF` (Makefile): `7bfc02e612cf4e7f4b8aaf3b00a5a8ce1875da47`
- `followers/phase/PHASE_PIN` (engine sha, hash16, card-data sha256):
  `b14f974787d86b6b6ade67aaaed67d32d68d7fd8 d0ffa159dcde97c7 d0ffa159dcde97c702e59ab34c801c3afc3d5507c0127910a955a6e66288ffba`
- `scripts/phase-oracle-setup.sh --check-pin` and `scripts/manabrew-oracle-setup.sh --check-pin` both exited 0 before the run.

## Pool

The pool narrowed by the UNION of both preflights: refused 12 of 196 pool cards.

## Lowering

```
lower_gap 1
lower_ok 77
refused 13
```

## Per-follower class histograms (SUMMARY.md)

phase:

```
agree 18
diverge 29
harness 5
lower_gap 1
phase_skips_attack_step 25
```

manabrew:

```
agree 58
decision_shape 1
diverge 6
harness 12
lower_gap 1
```

## Joint table: class tuple per item (phase manabrew)

Items present in both followers' verdicts; columns are the phase class, the manabrew class, then the count.

```
agree agree 12
agree decision_shape 1
agree diverge 2
agree harness 3
diverge agree 24
diverge diverge 2
diverge harness 3
harness agree 3
harness diverge 1
harness harness 1
lower_gap lower_gap 1
phase_skips_attack_step agree 19
phase_skips_attack_step diverge 1
phase_skips_attack_step harness 5
```

## Items where phase and manabrew disagree

Both followers sent the item and each gave `agree` or `diverge`, with different classes. Listed:
every item where phase agrees and manabrew diverges, plus every fourth item (byte order of id) where
phase diverges and manabrew agrees, at most 10. `field` is the verdict's differing field from
`diff-<f>/verdicts.jsonl`; no values or Oracle text are quoted here.

| item id | phase | manabrew |
|---|---|---|
| `xe:FDN:0:0:w12` | phase=diverge (field step) | manabrew=agree |
| `xe:FDN:0:0:w18` | phase=diverge (field step) | manabrew=agree |
| `xe:FDN:0:0:w22` | phase=diverge (field step) | manabrew=agree |
| `xe:FDN:0:0:w28` | phase=diverge (field step) | manabrew=agree |
| `xe:FDN:0:1:w14` | phase=agree | manabrew=diverge (field permanents) |
| `xe:FDN:0:1:w19` | phase=agree | manabrew=diverge (field stack) |
| `xe:FDN:0:2:w11` | phase=diverge (field step) | manabrew=agree |
| `xe:FDN:0:3:w10` | phase=diverge (field permanents) | manabrew=agree |

## Observations

- The two followers disagree mostly on windows where phase.rs diverges at field `step` while
  Manabrew agrees with gorge, and where phase.rs is classed `phase_skips_attack_step` (the 25 such
  items are 19 agree / 1 diverge / 5 harness on the manabrew side). Which side is wrong is not decided here;
  a follower bug is an observation, not fixed in this ticket.
- Manabrew's own non-agree rows are mostly `harness` (the driver could not carry the window out).
- No claim is made here that any row is a gorge bug; a triaged finding needs the three-way pass
  (N-way adjudication is design H5, not this ticket).
