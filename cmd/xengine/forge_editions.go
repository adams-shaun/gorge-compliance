package main

import (
	"fmt"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
)

// useForgeEditions applies the pinned Forge edition filter to b. An explicit
// dir wins; "none" opts out (an engine that never reads Forge card files);
// otherwise FindForgeEditions looks where make forge-oracle-setup leaves the
// clone (FORGE_ORACLE_EDITIONS, _SRC, _CLONE, _DIR). With none found it fails:
// an unfiltered pool can pick a playtest enabler the Forge preflight refuses
// (forge_lacks_card), and a warning on stderr is how that went unnoticed.
func useForgeEditions(b *certdeck.Builder, dir string) error {
	if dir == "none" {
		return nil
	}
	if dir == "" {
		dir = certdeck.FindForgeEditions()
	}
	if dir == "" {
		return fmt.Errorf("no pinned Forge editions found: set -forge-editions DIR, FORGE_ORACLE_EDITIONS, FORGE_ORACLE_CLONE or FORGE_ORACLE_DIR, or pass -forge-editions none to build an unfiltered pool")
	}
	return b.UseForgeEditions(dir)
}
