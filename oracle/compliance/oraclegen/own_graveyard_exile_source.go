package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// xmageOwnGraveyardExileFaces is the proven census of faces whose XMage
// implementation poses "exile N cards from your graveyard" through
// ExileCardFromOwnGraveyardControllerEffect. That effect builds a
// TargetCardInYourGraveyard(Math.min(N, graveyard size)) with
// withNotTarget(true) and calls player.chooseTarget, so TestPlayer serves the
// picks from the TARGET queue as one target definition, one "^" segment per
// pick. Gorge records the same choice as one mandatory hidden_pick of N picks,
// which the generic choose_n arm joins into one makeChoose choice
// ("Swamp^Swamp") no target ask reads: strict XMage then stops at
// "Missing TARGET def ... TargetCardInYourGraveyard"
// (xproof xs:XP:bloodcurdler:f0t0:s1, xs:XP:false-memories:cast:s1).
//
// Membership is proven against the XMage source at XMAGE_REF 464e1a6b23 (the
// pin the two finds were measured at), where `git grep -l
// 'new ExileCardFromOwnGraveyardControllerEffect' -- Mage.Sets` names exactly
// these two cards, and against their Forge scripts, which both carry the one
// own-graveyard hidden ChangeZone shape:
//
//	Bloodcurdler    SVar:BloodExile:DB$ ChangeZone | Hidden$ True | Mandatory$ True | ChangeType$ Card.YouCtrl | ChangeNum$ 2 | DefinedPlayer$ You | Origin$ Graveyard | Destination$ Exile   (granted by the threshold AddTrigger$ EndScream)
//	False Memories  SVar:TrigExile:DB$ ChangeZone | Origin$ Graveyard | Destination$ Exile | ChangeType$ Card.YouCtrl | ChangeNum$ 7 | Hidden$ True | Mandatory$ True   (executed by the delayed trigger DelTrig)
//
// Both effects live on an SVar (a granted trigger and a delayed trigger), so
// the stack snapshot names only the source card. TestOwnGraveyardExileFacesCensus
// proves the map is exactly the corpus's carriers of that shape and that the
// shape names exactly the XMage census, so a future carrier fails loudly
// instead of silently taking the choice route.
var xmageOwnGraveyardExileFaces = map[string]bool{
	"bloodcurdler":   true,
	"false memories": true,
}

// ownGraveyardExileSA reports the Forge script shape XMage implements with
// ExileCardFromOwnGraveyardControllerEffect: a hidden, mandatory ChangeZone
// from the controller's own graveyard to exile, of a fixed positive count,
// with no target or chooser picks of its own (a ValidTgts/Chooser/optional
// variant is another effect class and stays off this route). The shape is
// matched on the ability/effect role, not on a card name; the proven face set
// above is the XMage-side gate.
func ownGraveyardExileSA(sa *cards.SA) bool {
	return sa != nil && ownGraveyardExileParams(sa.API, sa.Params)
}

// ownGraveyardExileParams is ownGraveyardExileSA over a parsed parameter map,
// so it also matches an SVar body (whose ability keyword is DB$, not API).
func ownGraveyardExileParams(api string, p map[string]string) bool {
	if !strings.EqualFold(strings.TrimSpace(api), "ChangeZone") {
		return false
	}
	eq := func(k, v string) bool {
		return strings.EqualFold(strings.TrimSpace(p[k]), v)
	}
	if !eq("Origin", "Graveyard") || !eq("Destination", "Exile") ||
		!eq("Hidden", "True") || !eq("Mandatory", "True") {
		return false
	}
	// ChangeType$ Card.YouCtrl is the controller's own graveyard; a filtered
	// (Card.YouCtrl+Land) or non-controller type is a different effect.
	if !eq("ChangeType", "Card.YouCtrl") {
		return false
	}
	// Own-graveyard is the controller's; an explicit other player (or the
	// target-selected player) would not be this XMage effect.
	if d := strings.TrimSpace(p["DefinedPlayer"]); d != "" && !strings.EqualFold(d, "You") {
		return false
	}
	for _, k := range []string{"ValidTgts", "Chooser", "Optional", "UnlessCost",
		"UnlessPayer", "ChooseFromDefined", "TargetsWithDefinedPlayer",
		"OriginAlternative", "DestinationAlternative"} {
		if strings.TrimSpace(p[k]) != "" {
			return false
		}
	}
	n, err := strconv.Atoi(strings.TrimSpace(p["ChangeNum"]))
	return err == nil && n > 0
}

// ownGraveyardExileInChain reports whether an ability chain carries the shape.
func ownGraveyardExileInChain(sa *cards.SA) bool {
	seen := map[*cards.SA]bool{}
	for ; sa != nil && !seen[sa]; sa = sa.Sub {
		seen[sa] = true
		if ownGraveyardExileSA(sa) {
			return true
		}
	}
	return false
}

// ownGraveyardExileOnFace reports whether any effect a face can resolve has
// the own-graveyard exile shape. An SVar can be an effect body (the shape
// itself, False Memories' TrigExile), a trigger body that names its effect
// (Bloodcurdler's EndScream Execute$ BloodExile) or a delayed trigger's body
// (DelTrig Execute$ TrigExile), so the walk follows Execute$ and SubAbility$
// names transitively. The shape is also looked for on the printed ability and
// trigger chains, so a future card that carries it directly is covered the
// same way.
func ownGraveyardExileOnFace(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for _, sa := range f.Abilities {
		if ownGraveyardExileInChain(sa) {
			return true
		}
	}
	for _, tr := range f.Triggers {
		if ownGraveyardExileInChain(tr.Effect) {
			return true
		}
	}
	seen := map[string]bool{}
	var walk func(name string) bool
	walk = func(name string) bool {
		if name == "" || seen[name] {
			return false
		}
		seen[name] = true
		body, ok := f.SVars[name]
		if !ok {
			return false
		}
		p := svarParams(body)
		if ownGraveyardExileParams(p["DB"], p) {
			return true
		}
		if walk(p["Execute"]) || walk(p["SubAbility"]) {
			return true
		}
		// A trigger body names its effect only through Execute$; an effect
		// body names a rider through SubAbility$. Follow both, and the
		// delayed-trigger rider the same way.
		return false
	}
	for _, name := range sortedKeys(f.SVars) {
		if walk(name) {
			return true
		}
	}
	return false
}

// ownGraveyardExileStep reports whether the effect resolving at step is a
// proven own-graveyard exile face. The step's before-checkpoint stack is the
// authority, top first; both proven carriers pose the effect from an SVar, so
// the stack object names only the source card. The step's own cast/activate
// card is the fallback for a step whose snapshot shows no stack.
func ownGraveyardExileStep(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, step int) bool {
	if reg == nil || step < 0 {
		return false
	}
	if step < len(snaps) {
		for _, o := range snaps[step].Stack {
			if ownGraveyardExileSource(reg, o.Source) {
				return true
			}
		}
	}
	if step < len(sc.Steps) && sc.Steps[step].Card != "" {
		return ownGraveyardExileSource(reg, sc.Steps[step].Card)
	}
	return false
}

// ownGraveyardExileSource resolves a source ref to its face(s) and reports
// whether one is a proven own-graveyard exile face carrying the shape.
func ownGraveyardExileSource(reg *cards.Registry, ref string) bool {
	if ref == "" {
		return false
	}
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f == nil || !xmageOwnGraveyardExileFaces[strings.ToLower(f.Name)] {
			continue
		}
		if ownGraveyardExileOnFace(f) {
			return true
		}
	}
	return false
}

// ownGraveyardExileShape is the decision shape of the ask: one mandatory
// hidden pick of N >= 1 cards, all recorded as hidden_pick options. XMage's
// TargetCardInYourGraveyard reads the whole pick as one target definition on
// the TARGET queue, one "^" segment per card.
func ownGraveyardExileShape(d rules.OracleDecision) bool {
	if d.Kind != "choose_n" || d.Resume != "hidden_pick" || d.Min < 1 || d.Min != d.Max ||
		len(d.Picks) != d.Min || len(d.PickKinds) != len(d.Picks) {
		return false
	}
	for k := range d.Picks {
		if pickKind(d, k) != "hidden_pick" {
			return false
		}
	}
	return true
}

// ownGraveyardExileTargetAnswers is the TARGET-queue answer for a proven
// own-graveyard exile decision, or nil when decision i is not one.
//
// The effect calls player.chooseTarget ONCE for its min(amount, graveyard)
// target, so the replay's TestPlayer consumes ONE queue definition per ask and
// adds one target per "^" segment of it (its graveyard branch splits the
// definition and adds every name, exactly as the Dig graveyard take's
// "Swamp^Island" does). Emitting one definition per pick instead makes the
// single chooseTarget call take only the first: measured on the local build,
// False Memories left six target answers unconsumed (strict "Targets of
// PlayerA ... expected:<0> but was:<6>") and Bloodcurdler exiled one card
// instead of two. So every pick is one "^" segment of the one definition, each
// spelled by targetPickValue (the owner-qualified ref when the owner separates
// same-named candidates, the exact-ref alias when only identity does).
func (r *answerRouting) ownGraveyardExileTargetAnswers(i int) []XAnswer {
	d := r.ds[i]
	if !ownGraveyardExileShape(d) || !r.ownGraveyardExile[d.Step] {
		return nil
	}
	labels := make([]string, 0, len(d.Picks))
	for k, label := range d.Picks {
		if label == "" && k < len(d.PickRefs) {
			label = oraclediffRefName(d.PickRefs[k])
		}
		labels = append(labels, targetPickValue(d, k, label))
	}
	return []XAnswer{{d.Seat, "target", strings.Join(labels, "^")}}
}
