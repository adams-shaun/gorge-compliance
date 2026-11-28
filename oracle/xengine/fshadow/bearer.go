package fshadow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// attachAsk is the permanent gorge's target decision d attaches where the
// remembered line does not say to what, else 0. d is then the one-bearer ask of
// the remembered activation, whose ability on top of gorge's stack attaches its
// own source (Equip, Fortify, Reconfigure's attach half), and no targeting part
// of the line holds a target.
func (f *follower) attachAsk(d *decision.Decision) state.ObjID {
	c := f.chain
	if !c.ability || c.fromLine() || d.ResumeKind != "" || d.Min != 1 || d.Max != 1 || !f.castPoses(d) ||
		slices.ContainsFunc(c.parts, func(p []Ref) bool { return len(p) > 0 }) ||
		f.e == nil || len(f.e.G.Stack) == 0 {
		return 0
	}
	o := f.e.G.Obj(f.e.G.Stack[len(f.e.G.Stack)-1])
	if o == nil || o.Source != c.obj || o.Ability == nil || o.Ability.API != "Attach" {
		return 0
	}
	if ap := effects.AttachOf(o.Ability); ap.Unattach || ap.Object != "" && ap.Object != "Self" {
		return 0
	}
	return c.obj
}

// answerAttach answers attachAsk's decision d, which attaches host, from the
// next snapshot line (bearerFromSnapshot) and spends the ask's targeting part,
// if the line counted one. Where the snapshot does not tell the bearer it is a
// divergence: a default answer could attach host elsewhere than Forge did.
func (f *follower) answerAttach(d *decision.Decision, host state.ObjID) (decision.Intent, bool, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	i, why := f.bearerFromSnapshot(d, host)
	if why != "" {
		return in, false, &Divergence{What: WhatAnswerMissing, Harness: HarnessUnmapped, Field: "cast_targets",
			Forge: "none", Gorge: strings.Join(f.gorgeOptionKeys(d), " "), Cards: []Implicated{{Name: objName(f.e.G.Obj(host))}},
			Detail: "the activation records no bearer and " + why}
	}
	if len(f.chain.parts) > 0 {
		f.chain.parts = f.chain.parts[1:]
	}
	f.res.BearerPicks++
	in.Choices = []int{i}
	return in, true, nil
}

// bearerFromSnapshot is the index of d's option that Forge attached host to: of
// the names host and its controller's namesakes are attached to, the one that
// is new at the next snapshot line, offered by exactly one option. why says why
// the snapshot does not tell: it shows no new bearer (the ability did nothing,
// or the new bearer is named like the old one) or several, not exactly one
// offered permanent bears the name, or a later line may have moved host or a
// namesake by then (movedAgain).
func (f *follower) bearerFromSnapshot(d *decision.Decision, host state.ObjID) (pick int, why string) {
	o := f.e.G.Obj(host)
	name := shownName(f.e, o)
	if f.movedAgain(host) {
		return 0, "a later line may move it again before the next snapshot"
	}
	snap := f.nextSnapshot()
	if snap == nil {
		return 0, "the log has no later snapshot"
	}
	now := Snapshot(f.e, "")
	seat := int(o.Controller)
	gained := surplus(bearerNames(snap, seat, name), bearerNames(&now, seat, name))
	if len(gained) != 1 {
		return 0, fmt.Sprintf("the next snapshot shows %d new bearers of %q", len(gained), name)
	}
	var offered []int
	for i, op := range d.Options {
		if b := f.e.G.Obj(op.Obj); op.Obj != 0 && b != nil && shownName(f.e, b) == gained[0] {
			offered = append(offered, i)
		}
	}
	if len(offered) != 1 {
		return 0, fmt.Sprintf("gorge offers %d permanents called %q, its new bearer", len(offered), gained[0])
	}
	return offered[0], ""
}

// movedAgain reports whether the next snapshot may show an attachment of host
// or of a namesake that a later line made: one before it that activates a card
// of host's name, or targets host.
func (f *follower) movedAgain(host state.ObjID) bool {
	fid := f.toF[host]
	for _, e := range f.log.Entries[f.pos:] {
		if e.Snap != nil || e.End != nil {
			break
		}
		a := e.Ask
		if a == nil {
			continue
		}
		if a.Answer.Action == "activate" && f.name[a.Answer.Card] == f.name[fid] ||
			slices.ContainsFunc(a.Answer.Targets, func(r Ref) bool { return r.Card == fid }) {
			return true
		}
	}
	return false
}

// bearerNames is the name of every permanent that seat's permanents called name
// are attached to in s. A snapshot names a bearer by its ref or by its name.
func bearerNames(s *rules.OracleSnapshot, seat int, name string) []string {
	byRef := map[string]string{}
	for _, p := range s.Permanents {
		byRef[p.Ref] = p.Name
	}
	var out []string
	for _, p := range s.Permanents {
		if p.Controller != seat || p.Name != name || p.AttachedTo == "" {
			continue
		}
		host, ok := byRef[p.AttachedTo]
		if !ok {
			host = oraclediff.RefName(p.AttachedTo)
		}
		out = append(out, host)
	}
	return out
}
