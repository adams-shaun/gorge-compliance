// Package boundary holds gorge-compliance's licence guards and nothing else.
//
// Forge card scripts, token scripts and every Forge artifact (its Java
// source, the forge-oracle driver tree, Forge-produced result rows) are
// GPL-3.0. gorge-compliance, like gorge, never commits any of them. The tests
// in this package scan every file git would commit and fail on an offender:
//
//   - boundary_test.go: the card-script corpus, by content, by path
//     (cardsfolder/, tokenscripts/) and by byte identity with the live
//     corpus when one is present;
//   - boundary_forge_test.go: four Forge-artifact shapes: Java source in a
//     forge package, a forge-oracle path, the compliance/forge* result surface
//     and the Forge GPL header line;
//   - boundary_mtgish_test.go: mtgish's card data (it carries Oracle text),
//     fetched into the gitignored .mtgish/ for the definition cross-check.
//
// They are ports of gorge's engine/cards/boundary_test.go and
// engine/cards/boundary_forge_test.go. The package has no non-test code.
package boundary
