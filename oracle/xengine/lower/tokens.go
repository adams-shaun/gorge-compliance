package lower

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/state"
)

// Refusal reasons of a battlefield token (the runner's setup "tokens" places
// one by its Forge token-script stem).
const (
	reasonTokenUnscripted = "token_unscripted" // a copy, face-down, or no stem names its card
	reasonTokenChoice     = "token_choice"     // an as-enters choice the token made
	reasonTokenUnmapped   = "token_unmapped:"  // + the stem the XMage driver cannot place
)

// xmageSetupTokens is the set of token-script stems ScenarioReplay.SETUP_TOKENS
// can place (TestXMageSetupTokenTableMatchesDriver holds the two equal). A
// stem outside it is refused rather than lowered into a window XMage cannot
// replay.
var xmageSetupTokens = map[string]bool{
	"b_1_1_bat_flying":                 true,
	"b_2_1_skeleton":                   true,
	"b_2_2_zombie":                     true,
	"b_2_2_zombie_druid":               true,
	"c_0_0_a_thopter_flying":           true,
	"c_1_1_a_drone_flying_blockflying": true,
	"c_1_1_a_servo":                    true,
	"c_1_1_a_thopter_flying":           true,
	"c_1_1_pilot_crewbuff":             true,
	"c_1_1_pilot_crewsaddlebuff":       true,
	"c_1_1_sliver":                     true,
	"c_2_2_a_robot":                    true,
	"c_3_2_a_vehicle_crew_1":           true,
	"c_a_clue_draw":                    true,
	"c_a_food_sac":                     true,
	"c_a_lander_sac_search":            true,
	"c_a_treasure_sac":                 true,
	"c_e_shard_draw":                   true,
	"g_1_1_elf_warrior":                true,
	"g_2_2_spider_reach":               true,
	"g_3_3_elephant":                   true,
	"r_1_1_goblin":                     true,
	"r_1_1_gremlin":                    true,
	"r_4_4_dinosaur_dragon_flying":     true,
	"u_1_1_faerie_flying":              true,
	"u_1_1_fish":                       true,
	"u_1_1_merfolk":                    true,
	"ur_1_1_otter_prowess":             true,
	"w_0_4_wall_defender":              true,
	"w_1_1_bird_flying":                true,
	"w_1_1_cat_lifelink":               true,
	"w_1_1_dog":                        true,
	"w_1_1_e_glimmer":                  true,
	"w_1_1_human":                      true,
	"w_1_1_human_soldier":              true,
	"w_1_1_monk_prowess":               true,
	"w_1_1_rabbit":                     true,
	"w_1_1_soldier":                    true,
	"w_2_1_insect_flying":              true,
	"w_2_2_soldier":                    true,
	"w_3_1_spirit_flying":              true,
	"w_4_4_beast_lonely":               true,
	"wu_2_2_detective":                 true,
}

// tokenStems is the reverse of g.Tokens: the stem of each token definition.
// Two stems sharing one definition resolve to the first in sorted order.
func tokenStems(g *state.Game) map[*cards.Card]string {
	keys := make([]string, 0, len(g.Tokens))
	for k := range g.Tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[*cards.Card]string, len(keys))
	for _, k := range keys {
		if _, seen := out[g.Tokens[k]]; !seen {
			out[g.Tokens[k]] = k
		}
	}
	return out
}

// tokenScript is the stem whose definition o was minted from, or "" when o
// carries a card no stem names (a modified or copied token).
func (w *walker) tokenScript(o *state.Object) string {
	if o.Card == nil {
		return ""
	}
	if w.stems == nil {
		w.stems = tokenStems(w.g)
	}
	return w.stems[o.Card]
}

// checkToken refuses a battlefield token setup cannot place.
func (w *walker) checkToken(o *state.Object) {
	stem := w.tokenScript(o)
	switch {
	case o.IsCopy || stem == "" || o.FaceDown || o.FaceIdx != 0:
		w.why[reasonTokenUnscripted] = true
	case o.ChosenColor != "":
		w.why[reasonTokenChoice] = true
	case !xmageSetupTokens[stem]:
		w.why[reasonTokenUnmapped+stem] = true
	}
}

// placeTokens lists each seat's battlefield tokens in id order as setup
// tokens and binds their refs as the runner's resolve does: "pN:token:<face
// name>", plus "#k" for k > 1, k being the rank among the seat's battlefield
// tokens (id order) whose face name contains this one's, case-insensitively.
func (w *walker) placeTokens(c *cut, toks [2][]*state.Object) {
	for s := range toks {
		ts := toks[s]
		sort.SliceStable(ts, func(a, b int) bool { return ts[a].ID < ts[b].ID })
		for i, o := range ts {
			f := o.Face()
			if f == nil {
				w.why[reasonTokenUnscripted] = true
				continue
			}
			if o.AttachedTo != 0 {
				// The runner mints tokens unattached.
				w.why["field:Object.AttachedTo"] = true
				continue
			}
			name := strings.ToLower(f.Name)
			k := 0
			for _, x := range ts[:i+1] {
				if xf := x.Face(); xf != nil && strings.Contains(strings.ToLower(xf.Name), name) {
					k++
				}
			}
			ref := "token:" + f.Name
			if k > 1 {
				ref = fmt.Sprintf("%s#%d", ref, k)
			}
			c.Refs[o.ID] = fmt.Sprintf("p%d:%s", s, ref)
			tok := oraclegen.SetupToken{Script: w.tokenScript(o)}
			for _, ct := range o.Counters {
				if ct.N <= 0 {
					continue
				}
				if tok.Counters == nil {
					tok.Counters = map[string]int32{}
				}
				tok.Counters[ct.Kind] += ct.N
			}
			c.Setup[s].Tokens = append(c.Setup[s].Tokens, tok)
			if o.Tapped {
				c.Setup[s].TappedAfterUntap = append(c.Setup[s].TappedAfterUntap, ref)
			}
		}
	}
}
