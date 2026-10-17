// Package boundary holds four-yay's licence guards and nothing else.
//
// Forge card scripts, token scripts and every Forge artifact (its Java
// source, the forge-oracle driver tree, Forge-produced result rows) are
// GPL-3.0. four-yay, like gorge, never commits any of them. The tests in
// this package scan every file git would commit and fail on an offender:
//
//   - boundary_test.go: the card-script corpus, by content, by path
//     (cardsfolder/, tokenscripts/) and by byte identity with the live
//     corpus when one is present;
//   - boundary_forge_test.go: the four Forge-artifact shapes from the Forge
//     oracle design (section 11, decision P1-7).
//
// They are ports of gorge's engine/cards/boundary_test.go and
// engine/cards/boundary_forge_test.go. The package has no non-test code.
package boundary
