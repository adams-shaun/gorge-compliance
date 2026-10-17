package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// LosesGameSub is a Mode$ LosesGame trigger ("when a player/an
// opponent/the chosen player loses the game"). It needs a game of three
// seats: in a two-player game a loss ends the game (CR 104.2a), so nothing
// after it can be observed, while with a second opponent the game goes on
// (CR 800.4a) and the trigger resolves.
const LosesGameSub = "trigger.loses-game"

// losesGamePlayers are the ValidPlayer$ filters the three-seat template's
// cause (p1, an opponent of p0, loses) satisfies. A filter that names a
// player by a relation the setup cannot build (EnchantedBy, IsRemembered, an
// attacked-this-turn condition) keeps the mode gap.
var losesGamePlayers = []string{"", "Player", "Player.Opponent", "Player.Other", "Player.Chosen"}

// classifyLosesGameTrigger serves a LosesGame trigger whose ValidPlayer$
// filter p1's loss satisfies.
func classifyLosesGameTrigger(t *cards.Trigger) (string, bool) {
	if !strings.EqualFold(t.Mode, "LosesGame") {
		return "", false
	}
	vp := strings.TrimSpace(t.ParamStr(cards.PKValidPlayer))
	for _, ok := range losesGamePlayers {
		if strings.EqualFold(vp, ok) {
			return LosesGameSub, true
		}
	}
	return "", false
}
