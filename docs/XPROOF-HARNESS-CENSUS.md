# xproof `harness` reason census

The keyproof `reason` of an owed key that no engine agreed on (and that is not RNG) is now
`harness:<signature>`, from `classify.HarnessSignature`:

    harness:x=<class>[~<slug>]+f=<class>[~<slug>]

`x` is the XMage follower's finding and `f` the Forge follower's. The slug is the first six words of
the finding's message with digits, quoted names and the engine prefix dropped, so every item that dies
at the same driver limit, unanswered decision or missing scripted answer shares a signature. A
follower with no finding is class `none`. The next cycle's owed table splits by this column.

The committed table is `oracle/xengine/scen/testdata/harness-census.tsv` (signature, keys, cards,
card names); `TestHarnessCensus` in `oracle/xengine/scen/scen_test.go` checks that the keys sum to the
harness total and that the card counts match the names.

## Which data

The brief's cycle, 11260225 (207 keys, 120 cards), is stored only under `/mnt/sata`, which this task
may not read, so it was NOT censused. The table is built from the one harness-class data reachable
here, cycle 11241101 batches b00 + b01 (`xengine keyproof` re-run with this code on each batch's
play, `diff-xmage`, `diff-forge` and `adjudicate` dirs): 25 keys, 12 cards. Re-run the same on 11260225
to produce its table; every signature of 5 or more keys gets a ticket `xpg-h-<slug>`.

## Rows (cycle 11241101 b00+b01)

| signature | keys | cards |
|---|---|---|
| x=decision_shape~assertionerror+f=decision_shape~missing-cost-costblight-answer-at-step | 9 | 4 |
| x=decision_shape~assertionerror+f=decision_shape~missing-yesno-answer-at-step | 6 | 3 |
| x=agree+f=decision_shape~missing-discard-unless-type-answer-at | 2 | 1 |
| x=agree+f=decision_shape~missing-entity-chooser | 2 | 1 |
| x=agree+f=decision_shape~missing-yesno-answer-at-step | 2 | 1 |
| x=decision_shape~assertionerror+f=decision_shape~missing-cost-costforage-answer-at-step | 2 | 1 |
| x=decision_shape~leftover+f=decision_shape~leftover | 2 | 1 |
| **total** | **25** | **12** |

Every row is a `decision_shape` finding: a follower was asked a decision the scripted tape holds no
answer for (blight and forage costs, yes/no prompts, a discard-unless-type choice, an entity chooser).
That is a lowering/tape gap (an answer the tape should script), not twelve unrelated bugs.
