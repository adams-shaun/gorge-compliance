// Package audit_test holds the corpus drivers of gorge-compliance's two
// scenario corpora, which gorge's oracle runner plays through its exported
// API (rules.RunOracleAuditScenarioJSON, rules.CheckOracleScenarioJSON):
//
//   - scenarios/oracle/<family>/<card>.json: the hand-authored Oracle-text
//     audit (docs/design/2026-09-27-oracle-text-card-audit.md in gorge),
//     read together with the few scenario files gorge keeps for its own
//     card tests (engine/rules/testdata/oracle, located through gorgedir).
//     A scenario file lives in exactly one of the two directories.
//   - scenarios/xmageport/<family>/<Class>.json: XMage Mage.Tests methods
//     translated by cmd/xmageport (docs/design/2026-10-10-xmage-scenario-port-design.md).
//
// Every scenario names cards only; the corpus (.cards, GPL-3.0) is read at
// run time and never committed.
package audit_test
