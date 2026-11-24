package main

import (
	"fmt"
	"os"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
)

// useForgeEditions applies the pinned Forge edition filter to b. An explicit
// dir wins; otherwise FindForgeEditions looks where make forge-oracle-setup
// leaves the clone. With neither, it warns and leaves the pool unfiltered:
// xengine still builds decks for engines that do not read Forge card files,
// but picking a playtest enabler will get the item refused by the Forge
// preflight, so the missing setup is worth saying out loud.
func useForgeEditions(b *certdeck.Builder, dir string) error {
	if dir == "" {
		dir = certdeck.FindForgeEditions()
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "xengine: warning: no pinned Forge editions found (set -forge-editions, FORGE_ORACLE_EDITIONS or FORGE_ORACLE_DIR); certdeck may pick cards Forge's card DB filters")
		return nil
	}
	return b.UseForgeEditions(dir)
}
