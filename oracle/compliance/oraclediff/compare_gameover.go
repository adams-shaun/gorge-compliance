package oraclediff

import "strings"

// gameOverField reports whether a field is still compared at a checkpoint
// where BOTH engines report the game over. In a two-seat game a player's
// loss ends the game (CR 104.2a), and the rules define nothing about the
// zones after that: CR 800.4a's "objects owned by that player leave the
// game" is a multiplayer rule for a game that continues. gorge's runner
// applies that sweep to the loser anyway and XMage clears some zones and
// not others (Marina Vendrell's Grimoire's empty-hand loss: gorge removes
// the Grimoire, XMage keeps it and empties the library), so an ended
// checkpoint compares only who ended it and how: the turn header, the over
// flag and each player's life total.
func gameOverField(name string) bool {
	switch name {
	case "turn", "step", "active", "over", "players":
		return true
	}
	return strings.HasSuffix(name, ".life")
}
