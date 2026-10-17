module github.com/adams-shaun/gorge-compliance

go 1.25.8

// The gorge revision the coverage targets are measured against is GORGE_PIN
// (docs/PINNING.md). Until that revision is a pushed commit the require is a
// placeholder and the replace points at the sibling checkout; a pin bump
// replaces both with `require github.com/adams-shaun/gorge <pseudo-version>`.
require github.com/adams-shaun/gorge v0.0.0-00010101000000-000000000000

replace github.com/adams-shaun/gorge => ../gorge
