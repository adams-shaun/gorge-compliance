package game

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// refBinder names live objects the way gorge's oracle runner names them in a
// scenario (rules' objRef, which is unexported): a card dealt at the start is
// bound to "p<owner>:<name>[#n]", n counting that seat's cards of the name in
// the order the lowered scenario's setup lists them (hand in zone order, then
// the library top first); every other object is named by its rank among the
// objects the runner's resolve walks, as of the moment it is asked. A binder
// is built once, at the first decision, before anything has moved.
type refBinder struct {
	bound map[state.ObjID]string
}

func objName(o *state.Object) string {
	if o == nil || o.Face() == nil {
		return ""
	}
	return o.Face().Name
}

// newRefBinder binds every card in a seat's hand and library, in setup order.
func newRefBinder(e *rules.Engine) *refBinder {
	b := &refBinder{bound: map[state.ObjID]string{}}
	for i := range e.G.Players {
		p := state.PlayerID(i)
		counts := map[string]int{}
		place := func(ids []state.ObjID) {
			for _, id := range ids {
				n := objName(e.G.Obj(id))
				counts[n]++
				ref := fmt.Sprintf("p%d:%s", i, n)
				if counts[n] > 1 {
					ref = fmt.Sprintf("%s#%d", ref, counts[n])
				}
				b.bound[id] = ref
			}
		}
		place(e.G.Zone(state.ZHand, p))
		place(e.G.Zone(state.ZLibrary, p))
	}
	return b
}

// refOf is the runner ref of object id in the engine's current state, or ""
// for no object, a player reference, or an object the runner could not
// resolve again (a ceased token, a pending ability that left the stack).
func (b *refBinder) refOf(e *rules.Engine, id state.ObjID) string {
	if b == nil || id == 0 {
		return ""
	}
	if _, isPlayer := id.PlayerRef(); isPlayer {
		return ""
	}
	if r, ok := b.bound[id]; ok {
		return r
	}
	g := e.G
	o := g.Obj(id)
	if o == nil {
		return ""
	}
	if o.Ability != nil {
		name := objName(g.Obj(o.Source))
		norm := cards.NormalizeName(name)
		k := 0
		for i := range g.Objs {
			c := &g.Objs[i]
			if c.Zone != state.ZStack || c.Ability == nil || c.Controller != o.Controller ||
				cards.NormalizeName(objName(g.Obj(c.Source))) != norm {
				continue
			}
			k++
			if c.ID == o.ID {
				break
			}
		}
		base := fmt.Sprintf("p%d:ability:%s", o.Controller, name)
		if k > 1 {
			return fmt.Sprintf("%s#%d", base, k)
		}
		return base
	}
	if o.Face() == nil || o.Zone == state.ZCeased {
		return ""
	}
	name := objName(o)
	lower := strings.ToLower(name)
	seat, k := o.Owner, 0
	if o.IsToken {
		seat = o.Controller
	}
	for i := range g.Objs {
		c := &g.Objs[i]
		if c.Zone == state.ZCeased || c.Ability != nil || c.Face() == nil {
			continue
		}
		if o.IsToken {
			if !c.IsToken || c.Controller != seat || c.Zone != state.ZBattlefield ||
				!strings.Contains(strings.ToLower(c.Face().Name), lower) {
				continue
			}
		} else if c.Owner != seat || c.Face().Name != name {
			continue
		}
		k++
		if c.ID == o.ID {
			break
		}
	}
	if o.IsToken && o.Zone != state.ZBattlefield {
		return "" // resolve only finds a token on the battlefield
	}
	base := fmt.Sprintf("p%d:%s", seat, name)
	if o.IsToken {
		base = fmt.Sprintf("p%d:token:%s", seat, name)
	}
	ref := base
	if k > 1 {
		ref = fmt.Sprintf("%s#%d", base, k)
	}
	// A setup ref of the same spelling would shadow this object in resolve.
	for _, br := range b.bound {
		if br == ref {
			if k == 1 {
				return base + "#1"
			}
			return ""
		}
	}
	return ref
}
