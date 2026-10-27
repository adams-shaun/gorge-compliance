package lower

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/events"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// Refusal reasons that are not a bare field:<Type>.<Name>.
const (
	reasonToken        = "token_on_battlefield"
	reasonContinuous   = "field:Engine.continuous"
	reasonPhaseTrig    = "trigger:upkeep_or_draw"
	reasonByName       = "setup_by_name:"       // + tapped | back_face | counters
	reasonCountersLow  = "counters_below_entry" // fewer counters than the card enters with
	reasonZone         = "zone:"                // + the zone name
	reasonCard         = "card:no_front_face"
	reasonActiveTap    = "tapped_active_permanent" // the setup drive's untap step untaps it
	reasonTapElsewhere = "tapped_name_elsewhere"   // setup taps every placement of the name
	reasonAura         = "aura_on_battlefield"     // an Aura placed unattached never enters
)

// cut is the live engine at one cut point, reduced to what a window needs: the
// setup it can be rebuilt from, or why it cannot be. Seats are the live game's.
type cut struct {
	Entry  int   // tape index of the cut's decision
	Turn   int32 // the live turn
	Active int
	// Setup is one seat's placed state; Attach the attach prelude steps.
	Setup  [2]oraclegen.Seat
	Attach []oraclegen.Step
	// Answers is, per seat, the as-enters asks the seat's placements pose
	// during the setup drive, in placement order.
	Answers [2][]oraclegen.Answer
	// Refs is the runner ref (live seat numbering) setup will bind each placed
	// card to; MaxID is the highest object id alive at the cut.
	Refs  map[state.ObjID]string
	MaxID state.ObjID
	// Reasons is why the window is refused, sorted and without duplicates; empty
	// when it is expressible.
	Reasons []string
}

// capture reads the live engine at a cut point. It only reads.
func capture(e *rules.Engine, entry int) *cut {
	g := e.G
	c := &cut{Entry: entry, Turn: g.Turn, Active: int(g.Active), Refs: map[state.ObjID]string{}, MaxID: state.ObjID(len(g.Objs))}
	w := &walker{g: g, why: map[string]bool{}}
	w.struct_(reflect.ValueOf(g).Elem(), "Game", gameTable, nil)
	for i := range g.Players {
		w.seat = i
		w.struct_(reflect.ValueOf(&g.Players[i]).Elem(), "Player", playerTable, nil)
	}
	for i := range g.Objs {
		o := &g.Objs[i]
		// A resolved ability is parked off the stack as an object that is no
		// card; one still on the stack is refused with the stack itself.
		if o.Zone == state.ZCeased || o.Ability != nil && o.Zone != state.ZStack {
			continue
		}
		switch o.Zone {
		case state.ZBattlefield, state.ZHand, state.ZLibrary, state.ZGraveyard, state.ZExile:
		default:
			w.why[reasonZone+o.Zone.String()] = true
		}
		w.struct_(reflect.ValueOf(o).Elem(), "Object", objectTable, o)
		if o.Zone == state.ZBattlefield && phaseTriggered(o) {
			w.why[reasonPhaseTrig] = true
		}
		if o.Zone == state.ZBattlefield && slices.Contains(e.Derived(o.ID).Types, "Aura") {
			w.why[reasonAura] = true
		}
	}
	for _, ce := range e.RegisteredContinuous() {
		// printed statics are not registered; a resolved effect whose duration
		// outlives the turn it was made in is still live at the cut (an
		// until-end-of-turn one made in an earlier turn has expired).
		if ce.Permanent || ce.UntilTurn != 0 || ce.UntilUntapOf != 0 || ce.Duration != "" || ce.FromEffect {
			w.why[reasonContinuous] = true
		}
	}
	if len(w.why) == 0 {
		w.place(c)
	}
	c.Reasons = sortedKeys(w.why)
	return c
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// phaseTriggered is true for a permanent with a beginning-of-upkeep or
// draw-step trigger: the setup's drive from genesis to the cut passes through
// the upkeep and would fire it a second time.
func phaseTriggered(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	for _, t := range f.Triggers {
		if !strings.EqualFold(t.Mode, "Phase") {
			continue
		}
		ph := strings.ToLower(t.Params["Phase"])
		if strings.Contains(ph, "upkeep") || strings.Contains(ph, "draw") {
			return true
		}
	}
	return false
}

// walker applies the field tables to the live state.
type walker struct {
	g    *state.Game
	why  map[string]bool
	seat int // the seat Player fields are being read for
}

// struct_ visits every exported field of v (a struct of the named type) and adds
// field:<Type>.<Name> for a refuse field that is live. An unclassified field is
// itself a refusal, so a gorge bump never silently widens a window.
func (w *walker) struct_(v reflect.Value, typ string, table map[string]entry, o *state.Object) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		en, ok := table[f.Name]
		if !ok {
			w.why[fmt.Sprintf("field:%s.%s", typ, f.Name)] = true
			continue
		}
		if en.Class != refuse || !w.live(typ, f.Name, v.Field(i), o) {
			continue
		}
		if typ == "Object" && f.Name == "IsToken" {
			w.why[reasonToken] = true
			continue
		}
		w.why[fmt.Sprintf("field:%s.%s", typ, f.Name)] = true
	}
}

// live is true when a refuse field carries state. The default is "not at its
// zero value"; a turn stamp is live only when it names the cut's turn, and the
// zone entries of the draw step (library to hand) are not history setup could
// have changed.
func (w *walker) live(typ, name string, v reflect.Value, o *state.Object) bool {
	switch {
	case typ == "Player" && name == "LandsPlayed":
		return v.Int() > 0 && w.seat == int(w.g.Active)
	case typ == "Object" && stackOnly[name]:
		return o.Zone == state.ZStack && v.Len() > 0
	case typ == "Object" && name == "ActivatedThisTurn":
		return v.Int() > 0 && hasActivationLimit(o)
	case typ == "Object" && turnStamps[name]:
		return v.Int() == int64(w.g.Turn)
	case typ == "Object" && name == "EnteredThisTurn":
		return v.Bool() && !(o.Zone == state.ZHand && o.EnteredFrom == state.ZLibrary)
	case typ == "Game" && name == "Entered":
		for _, z := range w.g.Entered {
			if z.To != state.ZHand || z.From != state.ZLibrary {
				return true
			}
		}
		return false
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Map:
		return v.Len() > 0
	}
	return !v.IsZero()
}

// frontName is the name setup lists a card under: its first face, the name the
// runner's dealt library card carries.
func frontName(o *state.Object) string {
	if o.Card == nil || len(o.Card.Faces) == 0 {
		return ""
	}
	return o.Card.Faces[0].Name
}

// place fills c.Setup, c.Refs and c.Attach from the live game, adding to w.why
// whatever setup cannot say.
func (w *walker) place(c *cut) {
	g := w.g
	var bf [2][]*state.Object
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone == state.ZBattlefield {
			if o.Owner != o.Controller {
				w.why["field:Object.Controller"] = true
				continue
			}
			bf[o.Owner] = append(bf[o.Owner], o)
		}
	}
	for s := range g.Players {
		p := state.PlayerID(s)
		sort.SliceStable(bf[s], func(a, b int) bool {
			if bf[s][a].Timestamp != bf[s][b].Timestamp {
				return bf[s][a].Timestamp < bf[s][b].Timestamp
			}
			return bf[s][a].ID < bf[s][b].ID
		})
		var seat oraclegen.Seat
		counts := map[string]int{}
		bind := func(o *state.Object) string {
			n := frontName(o)
			if n == "" {
				w.why[reasonCard] = true
			}
			counts[n]++
			ref := fmt.Sprintf("p%d:%s", s, n)
			if counts[n] > 1 {
				ref = fmt.Sprintf("%s#%d", ref, counts[n])
			}
			c.Refs[o.ID] = ref
			return n
		}
		names := func(ids []state.ObjID) []string {
			out := []string{}
			for _, id := range ids {
				o := g.Obj(id)
				if o == nil || o.Ability != nil {
					continue
				}
				if o.Owner != p {
					w.why["field:Object.Owner"] = true
					continue
				}
				out = append(out, bind(o))
			}
			return out
		}
		// The runner binds refs in setup order: battlefield, hand, graveyard,
		// exile, then the library top.
		tapped, back, counters := map[string]map[bool]bool{}, map[string]map[bool]bool{}, map[string]map[string]int32{}
		mixed := map[string]bool{}
		netOf := map[string]string{}
		for _, o := range bf[s] {
			n := bind(o)
			seat.Battlefield = append(seat.Battlefield, n)
			if o.ChosenColor != "" {
				c.Answers[s] = append(c.Answers[s], oraclegen.Answer{Kind: "choose", Pick: []string{o.ChosenColor}})
			}
			if tapped[n] == nil {
				tapped[n], back[n] = map[bool]bool{}, map[bool]bool{}
			}
			tapped[n][o.Tapped] = true
			if o.FaceIdx > 1 {
				w.why["field:Object.FaceIdx"] = true
			}
			back[n][o.FaceIdx == 1] = true
			net := setupCounters(o)
			key := ""
			for _, k := range sortedMapKeys(net) {
				if net[k] < 0 {
					w.why[reasonCountersLow] = true
				}
				if net[k] > 0 {
					key += fmt.Sprintf("%s=%d;", k, net[k])
				}
			}
			if prev, seen := netOf[n]; seen && prev != key {
				mixed["counters"] = true
			}
			netOf[n] = key
			if key != "" && counters[n] == nil {
				counters[n] = map[string]int32{}
				for k, v := range net {
					if v > 0 {
						counters[n][k] = v
					}
				}
			}
		}
		for _, n := range seat.Battlefield {
			if len(tapped[n]) > 1 {
				mixed["tapped"] = true
			}
			if len(back[n]) > 1 {
				mixed["back_face"] = true
			}
		}
		for _, k := range sortedKeys(mixed) {
			w.why[reasonByName+k] = true
		}
		seat.Hand = names(g.Zone(state.ZHand, p))
		seat.Graveyard = names(g.Zone(state.ZGraveyard, p))
		seat.Exile = names(g.Zone(state.ZExile, p))
		seat.LibraryTop = names(g.Zone(state.ZLibrary, p))
		elsewhere := map[string]bool{}
		for _, id := range append(append(append([]state.ObjID{}, g.Zone(state.ZHand, p)...), g.Zone(state.ZGraveyard, p)...), g.Zone(state.ZExile, p)...) {
			// A land cannot enter the battlefield on the opponent's turn but
			// by an effect the digest would show; any other card might (flash).
			if o := g.Obj(id); o != nil && o.Ability == nil && o.Face() != nil && !o.Face().IsLand() {
				elsewhere[frontName(o)] = true
			}
		}
		for _, n := range dedupe(seat.Battlefield) {
			if tapped[n][true] && !tapped[n][false] {
				// The runner taps every placement of the name, in any zone, and a
				// tapped card keeps it when it later enters the battlefield; a
				// card in the library is not in play before the window ends.
				switch {
				case s == int(g.Active):
					w.why[reasonActiveTap] = true
				case elsewhere[n]:
					w.why[reasonTapElsewhere] = true
				}
				seat.Tapped = append(seat.Tapped, n)
			}
			if back[n][true] && !back[n][false] {
				seat.BackFace = append(seat.BackFace, n)
			}
			if cs := counters[n]; len(cs) > 0 {
				if seat.Counters == nil {
					seat.Counters = map[string]map[string]int32{}
				}
				seat.Counters[n] = cs
			}
		}
		life := g.Players[s].Life
		seat.Life = &life
		seat.Speed = g.Players[s].Speed
		c.Setup[s] = seat
	}
	// An attachment names its bearer's ref, so both seats are bound first.
	for s := range g.Players {
		for _, o := range bf[s] {
			if o.AttachedTo == 0 {
				continue
			}
			b := g.Obj(o.AttachedTo)
			if b == nil || b.Zone != state.ZBattlefield || c.Refs[b.ID] == "" {
				w.why["field:Object.AttachedTo"] = true
				continue
			}
			c.Attach = append(c.Attach, oraclegen.Step{Op: "attach", Seat: s, Card: c.Refs[o.ID], AttachedTo: c.Refs[b.ID]})
		}
	}
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func sortedMapKeys(m map[string]int32) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// setupCounters is the counters setup must ADD to o's placement: what o carries
// less what a fresh copy of the card enters with (a planeswalker's printed
// loyalty), which the runner's counters field is added to. A kind the card
// carries fewer of than it enters with comes out negative, which setup cannot say.
func setupCounters(o *state.Object) map[string]int32 {
	net := map[string]int32{}
	for _, ct := range o.Counters {
		net[ct.Kind] += ct.N
	}
	for _, gr := range events.EntryCounterGrants(&state.Object{Card: o.Card, FaceIdx: o.FaceIdx}, false) {
		net[gr.Kind] -= gr.Amount
	}
	return net
}

// hasActivationLimit is true for a face with an ability gated by an
// ActivationLimit$ (the only reader of Object.ActivatedThisTurn).
func hasActivationLimit(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	for _, sa := range f.Abilities {
		for k := range sa.Params {
			if strings.Contains(k, "ActivationLimit") {
				return true
			}
		}
	}
	return false
}
