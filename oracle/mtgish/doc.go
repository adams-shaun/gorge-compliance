// Package mtgish cross-checks gorge's compiled card definitions against
// mtgish (https://github.com/i5jb/mtgish, MIT), an independent typed encoding
// of the card corpus parsed from Oracle text.
//
// Both sides are normalised to one Summary per card face, made of short
// canonical tokens in four tiers: T1 characteristics (mana cost, types, P/T,
// loyalty, keywords), T2 ability shape (abilities by kind, trigger event
// class), T3 targets (per ability: count and target kind class), T4 effects
// (effect verbs, with amounts where both sides give a literal). Diff turns
// two summaries into rows, one per differing field; anything a normaliser
// cannot map is an explicit "unmapped:<name>" token, and a row involving one
// is a normaliser gap rather than a disagreement.
//
// The mtgish data (.mtgish/<ref>/mtgish.lines.json, `make mtgish-fetch`) is
// decoded generically (node.go): mtgish's JSON is tagged unions, and only the
// variants the normalisers map are named in code. The FDN and BLB rows are
// held to a committed ledger (known/<SET>.tsv) by TestMtgishLedger.
//
// docs/design/2026-10-20-mtgish-crosscheck.md has the design: the tiers, the
// triage classes, the pin and the ratchet.
package mtgish
