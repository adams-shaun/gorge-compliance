package fshadow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// libraryTopN is how many library cards a snapshot names, as gorge's oracle
// snapshot does (rules.oracleLibraryTopN).
const libraryTopN = 3

// Snapshot projects a running gorge game onto the oracle snapshot shape, the
// fields oraclediff compares. gorge exports its snapshot only for an oracle
// scenario run (rules.oracleRun.snapshot is unexported, and needs scenario
// refs), so this mirrors it from the engine's public accessors: life, zones
// by name, library count and top, pool, each permanent's characteristics and
// combat state, and the stack. Refs are the follower's own ("pN:Name#id");
// the comparator reads names, not refs.
func Snapshot(e *rules.Engine, checkpoint string) rules.OracleSnapshot {
	g := e.G
	s := rules.OracleSnapshot{
		Checkpoint: checkpoint, Turn: g.Turn, Step: g.Step.String(),
		Active: int(g.Active), Priority: int(g.Priority), Over: g.Over,
		Players: []rules.OracleSnapPlayer{}, Permanents: []rules.OracleSnapPerm{}, Stack: []rules.OracleSnapStack{},
	}
	if g.Over && !g.Draw {
		w := int(g.Winner)
		s.Winner = &w
	}
	for i := range g.Players {
		p := state.PlayerID(i)
		lib := g.Zone(state.ZLibrary, p)
		top := []string{}
		for j := 0; j < len(lib) && j < libraryTopN; j++ {
			top = append(top, objName(g.Obj(lib[j])))
		}
		s.Players = append(s.Players, rules.OracleSnapPlayer{
			Seat: i, Life: g.Players[i].Life, Counters: counters(g.Players[i].Counters),
			Hand: zoneNames(g, state.ZHand, p, true), Graveyard: zoneNames(g, state.ZGraveyard, p, false),
			Exile: zoneNames(g, state.ZExile, p, true), Command: zoneNames(g, state.ZCommand, p, true),
			LibraryCount: len(lib), LibraryTop: top, Pool: pool(g.Players[i].Pool),
			Lost: g.Players[i].Lost,
		})
	}
	var field []state.ObjID
	for i := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, state.PlayerID(i)) {
			if !g.Obj(id).PhasedOut {
				field = append(field, id)
			}
		}
	}
	sort.Slice(field, func(a, b int) bool { return field[a] < field[b] })
	blocking := map[state.ObjID]bool{}
	for _, id := range field {
		for _, b := range g.Obj(id).BlockedBy {
			blocking[b] = true
		}
	}
	for _, id := range field {
		o := g.Obj(id)
		d := e.Derived(id)
		types := append([]string(nil), d.Types...)
		sort.Strings(types)
		kws := append([]string(nil), e.Keywords(id)...)
		sort.Strings(kws)
		p := rules.OracleSnapPerm{
			Ref: ref(o), Name: shownName(e, o), Controller: int(o.Controller), Owner: int(o.Owner),
			Token: o.IsToken, Tapped: o.Tapped, FaceDown: o.FaceDown, Damage: o.Damage,
			Counters: counters(o.Counters), Types: types, AllCreatureTypes: d.AllCreatureTypes,
			Colors: e.Colors(id), Keywords: kws,
			Attacking: o.IsAttacking, Blocking: blocking[id] || o.IsBlocking,
		}
		for _, t := range types {
			if strings.EqualFold(t, "Creature") {
				p.PT = fmt.Sprintf("%d/%d", e.Power(id), e.Toughness(id))
				break
			}
		}
		switch {
		case o.AttachedTo != 0:
			p.AttachedTo = ref(g.Obj(o.AttachedTo))
		case o.HasAttachedPlayer:
			p.AttachedTo = fmt.Sprintf("p%d", o.AttachedPlayer)
		}
		s.Permanents = append(s.Permanents, p)
	}
	for i := len(g.Stack) - 1; i >= 0; i-- {
		o := g.Obj(g.Stack[i])
		if o == nil {
			continue
		}
		it := rules.OracleSnapStack{Kind: "spell", Controller: int(o.Controller), Source: ref(o)}
		if o.Ability != nil {
			it.Kind = "ability"
			it.Source = ref(g.Obj(o.Source))
		}
		s.Stack = append(s.Stack, it)
	}
	return s
}

// shownName is permanent o's name as a snapshot shows it: none while face down
// (CR 708.2a), else the name an effect set, else its face's.
func shownName(e *rules.Engine, o *state.Object) string {
	if o.FaceDown {
		return ""
	}
	if d := e.Derived(o.ID); d.NameSet {
		return d.Name
	}
	return objName(o)
}

func objName(o *state.Object) string {
	if o == nil || o.Face() == nil {
		return ""
	}
	return o.Face().Name
}

// ref is the follower's ref for an object: "pN:Name", the shape RefName
// strips back to the name.
func ref(o *state.Object) string {
	if o == nil {
		return ""
	}
	return fmt.Sprintf("p%d:%s", o.Owner, objName(o))
}

func zoneNames(g *state.Game, z state.Zone, p state.PlayerID, sorted bool) []string {
	ids := g.Zone(z, p)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, objName(g.Obj(id)))
	}
	if sorted {
		sort.Strings(out)
	}
	return out
}

func counters(cs []state.Counter) map[string]int32 {
	if len(cs) == 0 {
		return nil
	}
	m := map[string]int32{}
	for _, c := range cs {
		m[c.Kind] += c.N
	}
	return m
}

func pool(m state.Mana) string {
	var b strings.Builder
	for i, c := range "WUBRGC" {
		b.WriteString(strings.Repeat(string(c), int(m[i])))
	}
	return b.String()
}
