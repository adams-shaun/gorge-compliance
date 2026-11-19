package oraclegen

// Resolving a granted or gained activated ability for the follower exporters.
//
// A lowered step that activates a granted (AddAbilities$/AddAbility$ SVar),
// gained (GainsAbilitiesOf$) or keyword-granted ability carries the engine's
// offered option label in Step.Ability beside its AbilityIndex: the pick's
// Ability index is not the card's printed index there (it aliases printed
// index 0 for an SVar grant and is -1 for a keyword grant), so resolving the
// index against the printed Face.Abilities exports the WRONG line -- Dalakos,
// Crafter of Wonders's {T} mana ability for Dragon Mantle's granted {R} pump,
// the farm's xe:RAND:1:4:w14/w22 divergence.
//
// The label is exact engine text: "<recipient face name>: <SpellDescription>"
// (the granted-ability offer loops in gorge's legal walk). ResolveActivatedAbility
// finds the SA the label names among the abilities the board can actually
// grant to the step's recipient: the recipient's own and every attached
// card's SVar bodies, the SVar bodies and AddKeyword$ lines named by a static
// whose Affected$ filter reaches the recipient, and the printed abilities of
// a card a GainsAbilitiesOf$ static on the board names. The label alone is not
// enough -- an unrelated card may print the same SpellDescription (Vengeful
// Firebrand prints "CARDNAME gets +1/+0 until end of turn.") -- so a
// candidate's own card and grant relationship are checked, never just the
// text. Matching the grant relationship, not a list of grantor card names,
// covers the next granted ability without a new selector.
//
// This file lives in oraclegen, not in a follower: forge-export, the neutral
// exporter and xprep all need the same answer, and a copy in one of them
// would be the next place to forget a grant kind.

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// GrantedAbilityLabel is the label the engine offers sa under on a recipient
// whose face is named recipient: the granted-ability offer loops label a
// granted, gained or keyword-granted ability
// "<face name>: <SpellDescription>".
func GrantedAbilityLabel(recipient string, sa *cards.SA) string {
	return recipient + ": " + sa.ParamStr(cards.PKSpellDescription)
}

// ResolveActivatedAbility returns the granted/gained/keyword-granted ability
// activate step i names, or false when the step is a printed ability (or names
// no ability). A step lowered before the grant identity existed carries no
// label; its recorded activate pick in g.Transcript still names the offered
// option, so the same resolution covers a pre-fix item without a re-lowering.
//
// Two candidates with the same label but different script lines are
// ambiguous and count as none, so an export never depends on scan order.
func ResolveActivatedAbility(reg *cards.Registry, it Item, i int, st Step, g rules.OracleResult) (*cards.SA, bool) {
	if st.Op != "activate" || st.Card == "" {
		return nil, false
	}
	label := st.Ability
	if label == "" {
		// The pre-fix recovery reads a recorded pick for an index-named
		// step; a step with neither anchor names no ability.
		if st.AbilityIndex == nil {
			return nil, false
		}
		label = recordedActivateLabel(g, i, st.Card)
		if label == "" {
			return nil, false
		}
		// A pre-fix printed step's recorded pick is the printed ability's own
		// label. Treat it as printed, not as a grant that happens to carry the
		// same text, so neutral identities keep their ordinal.
		if sa, ok := printedStepAbility(reg, it, i, st, g); ok &&
			GrantedAbilityLabel(stepLabelName(reg, it, i, st, g), sa) == label {
			return nil, false
		}
	}
	recipient := stepLabelName(reg, it, i, st, g)
	// Every granted offer's label starts with the recipient's live face name
	// (the face-down form is "Face-down creature"). A label that does not is a
	// special action or another option kind with no grant identity, and the
	// full candidate scan can be skipped.
	prefix := recipient + ": "
	if !strings.HasPrefix(label, prefix) {
		return nil, false
	}
	found := grantedCandidates(reg, it, i, st, g, recipient, label)
	if len(found) != 1 {
		return nil, false
	}
	return found[0], true
}

// grantedCandidates returns every distinct activated ability the board can
// offer on the step's recipient under the exact engine label. Candidates are
// scoped by their grant, not by their text:
//
//   - the recipient's own face and every face attached to it may carry the
//     SVar body of a static or of a resolving Animate/effect grant;
//   - any card's printed Continuous static whose Affected$ filter reaches the
//     recipient names the SVar bodies and AddKeyword$ lines it grants;
//   - a GainsAbilitiesOf$/GainsAbilitiesOfDefined$ static that reaches the
//     recipient lends the printed activated abilities of the cards its zone
//     and filter name.
//
// An unrelated card's printed ability or SVar is never a candidate, so a
// same-text card elsewhere in the setup cannot make the answer ambiguous.
func grantedCandidates(reg *cards.Registry, it Item, i int, st Step, g rules.OracleResult, recipient, label string) []*cards.SA {
	rec := recipientBoard(reg, it, i, st.Card, recipient, g)
	var out []*cards.SA
	seen := map[string]bool{}
	add := func(sa *cards.SA) {
		if sa == nil || !sa.IsActivated() || seen[sa.Line] {
			return
		}
		if GrantedAbilityLabel(recipient, sa) != label {
			return
		}
		seen[sa.Line] = true
		out = append(out, sa)
	}
	pool := grantedPool(reg, it, i, st.Card, g)
	var gains []gainsGrant
	for _, pc := range pool {
		for _, f := range pc.card.Faces {
			if f == nil {
				continue
			}
			// The recipient and the cards attached to it may hold the body a
			// non-static grant (a Saga chapter's Animate, an effect that
			// resolves an SVar off its own source) mints. The text match plus
			// the same-card/attachment relation is still exact: no other card
			// reaches this branch.
			if pc.card == rec.card || rec.attached[pc.card] {
				for _, name := range sortedKeys(f.SVars) {
					add(cards.ResolveSVar(f.SVars, name))
				}
			}
			for _, s := range f.Statics {
				if !staticActiveInZone(s, pc.zone) || !staticAffectsRecipient(s, pc, rec) {
					continue
				}
				for _, name := range staticAddAbilityNames(s) {
					add(cards.ResolveSVar(f.SVars, name))
				}
				for _, line := range staticAddKeywordLines(s) {
					add(cards.GrantedKeywordAbility(line))
				}
				valid := s.ParamStr(cards.PKGainsValidAbilities)
				if spec := strings.TrimSpace(s.ParamStr(cards.PKGainsAbilitiesOf)); spec != "" {
					gains = append(gains, gainsGrant{src: pc, spec: spec,
						zones: s.ParamStr(cards.PKGainsAbilitiesOfZones), valid: valid})
				}
				if spec := strings.TrimSpace(s.ParamStr(cards.PKGainsAbilitiesOfDefined)); spec != "" {
					gains = append(gains, gainsGrant{src: pc, spec: spec, valid: valid, defined: true})
				}
			}
		}
	}
	for _, gg := range gains {
		for _, pc := range pool {
			if !gg.defined && !zoneAllows(gg.zones, pc.zone) {
				continue
			}
			for _, f := range pc.card.Faces {
				if f == nil || !donorMatches(gg, pc, f, rec) {
					continue
				}
				for _, ab := range f.Abilities {
					if gainsValidAdmits(gg.valid, ab) {
						add(ab)
					}
				}
			}
		}
	}
	return out
}

// gainsGrant is one GainsAbilitiesOf$ or GainsAbilitiesOfDefined$ static that
// reaches the recipient, with its donor zone/filter.
type gainsGrant struct {
	src     *grantedPoolCard
	spec    string
	zones   string
	valid   string
	defined bool
}

// donorMatches evaluates a GainsAbilitiesOf$ filter against a card in the
// granting static's zones. The predicate words the exporter cannot read are
// accepted; the type and controller words that separate the common donors are
// evaluated exactly.
func donorMatches(gg gainsGrant, pc *grantedPoolCard, f *cards.Face, rec grantRecipient) bool {
	facts := pc.facts()
	if len(facts.types) == 0 {
		facts.types = f.Types
		facts.colors = f.Colors
	}
	return filtersCard(gg.spec, facts, gg.src.card, rec.attached, gg.src.ctrl())
}

// recordedActivateLabel is the label of the option gorge's runner picked for
// activate step i, or "" when the result records none. A priority action's
// pick is not an OracleDecision (the runner filters KPriority), so this reads
// the run's own transcript line ("step activate ..." then
// "[activate] pN priority -> [\"label\"]"), which is the runner's exact pick.
// A transcript is absent only in a synthetic OracleResult; the entering
// snapshot's Offered list is then the fallback, and a single offered label
// for the source is still exact.
func recordedActivateLabel(g rules.OracleResult, i int, ref string) string {
	if label, ok := transcriptActivateLabel(g.Transcript, i); ok {
		return label
	}
	if i >= 0 && i < len(g.Snapshots) {
		var labels []string
		for _, o := range g.Snapshots[i].Offered {
			if o.Source == ref && o.Label != "" {
				labels = append(labels, o.Label)
			}
		}
		if len(labels) == 1 {
			return labels[0]
		}
	}
	return ""
}

// transcriptActivateLabel extracts step i's activate pick from the runner's
// transcript. Each scenario step opens one "step <op> <card>" line; the
// priority submit for the op follows it as
// "  [activate] p0 priority -> [\"label\"]". The label list is Go's %q of a
// []string, which is JSON for the strings it carries.
func transcriptActivateLabel(lines []string, step int) (string, bool) {
	idx := -1
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "step ") {
			idx++
			continue
		}
		if idx != step || !strings.Contains(line, "[activate]") ||
			!strings.Contains(line, "priority -> [") {
			continue
		}
		start := strings.Index(line, " -> [")
		if start < 0 {
			continue
		}
		var labels []string
		if err := json.Unmarshal([]byte(line[start+4:]), &labels); err != nil || len(labels) != 1 {
			continue
		}
		return labels[0], true
	}
	return "", false
}

// stepLabelName is the face name the engine prefixes an offered ability label
// with: the live snapshot permanent's name (or "Face-down creature", CR 708.8)
// when the step names one, else the ref's card / token face name.
func stepLabelName(reg *cards.Registry, it Item, i int, st Step, g rules.OracleResult) string {
	if perm, ok := stepSnapshotPerm(st.Card, i, g); ok {
		if perm.FaceDown {
			return "Face-down creature"
		}
		if perm.Name != "" {
			return perm.Name
		}
	}
	return oraclediffRefName(st.Card)
}

// printedStepAbility is the ability Face.Abilities[*st.AbilityIndex] the step
// names when the setup or the entering snapshot resolves the recipient face.
// It is nil for a token whose face has no such index or a step with no index.
func printedStepAbility(reg *cards.Registry, it Item, i int, st Step, g rules.OracleResult) (*cards.SA, bool) {
	if st.AbilityIndex == nil {
		return nil, false
	}
	k := *st.AbilityIndex
	if k < 0 {
		return nil, false
	}
	perm, found := stepSnapshotPerm(st.Card, i, g)
	c := refCard(reg, it, st.Card, perm, found)
	if c == nil {
		return nil, false
	}
	face := c.Faces[0]
	if found && perm.Name != "" {
		for _, f := range c.Faces {
			if f.Name == perm.Name {
				face = f
				break
			}
		}
	} else if seat, _, has := strings.Cut(st.Card, ":"); has {
		for _, name := range it.Setup[seat].BackFace {
			if name == oraclediffRefName(st.Card) && len(c.Faces) > 1 {
				face = c.Faces[1]
				break
			}
		}
	}
	if k >= len(face.Abilities) || face.Abilities[k] == nil {
		return nil, false
	}
	return face.Abilities[k], true
}

func stepSnapshotPerm(ref string, i int, g rules.OracleResult) (rules.OracleSnapPerm, bool) {
	if i < 0 || i >= len(g.Snapshots) {
		return rules.OracleSnapPerm{}, false
	}
	for _, p := range g.Snapshots[i].Permanents {
		if p.Ref == ref {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

// cardFacts is the subset of an object's live characteristics a grant
// filter's Affected$/GainsAbilitiesOf$ spec can name.
type cardFacts struct {
	card         *cards.Card
	types        []string
	colors       string
	counters     map[string]int32
	ctrl         int
	owner        int
	token        bool
	faceDown     bool
	hasEquipment bool
	hasAura      bool
}

// recipientBoard is the step recipient's live identity plus the facts and
// attachments a grant static's filter is evaluated against.
type grantRecipient struct {
	ref      string
	name     string
	card     *cards.Card
	facts    cardFacts
	attached map[*cards.Card]bool
}

// grantedPoolCard is one card in the item's setup or the entering snapshot,
// with the zone it is in there and the live permanent facts when it is on the
// battlefield. The same printed card can occur in several zones; each entry
// is judged in its own zone.
type grantedPoolCard struct {
	card    *cards.Card
	zone    state.Zone
	seat    int
	perm    rules.OracleSnapPerm
	hasPerm bool
}

func (p *grantedPoolCard) ctrl() int {
	if p.hasPerm {
		return p.perm.Controller
	}
	return p.seat
}

func (p *grantedPoolCard) facts() cardFacts {
	if p.hasPerm {
		return permFacts(p.perm, p.card)
	}
	f := cardFacts{card: p.card, ctrl: p.seat, owner: p.seat}
	if len(p.card.Faces) > 0 && p.card.Faces[0] != nil {
		f.types = p.card.Faces[0].Types
		f.colors = p.card.Faces[0].Colors
	}
	return f
}

// permFacts projects a live permanent snapshot onto the filter facts.
func permFacts(perm rules.OracleSnapPerm, c *cards.Card) cardFacts {
	return cardFacts{
		card: c, types: perm.Types, colors: perm.Colors, counters: perm.Counters,
		ctrl: perm.Controller, owner: perm.Owner, token: perm.Token, faceDown: perm.FaceDown,
	}
}

// recipientBoard resolves the step's recipient and the cards attached to it.
// The entering snapshot is authoritative; the item's setup "attached" list
// covers a synthetic result with no snapshot.
func recipientBoard(reg *cards.Registry, it Item, i int, ref, name string, g rules.OracleResult) grantRecipient {
	perm, found := stepSnapshotPerm(ref, i, g)
	rec := grantRecipient{ref: ref, name: name, attached: map[*cards.Card]bool{}}
	rec.card = refCard(reg, it, ref, perm, found)
	if found {
		rec.facts = permFacts(perm, rec.card)
	} else {
		seat, _ := refSeat(ref)
		rec.facts = cardFacts{card: rec.card, ctrl: seat, owner: seat}
		if rec.card != nil && len(rec.card.Faces) > 0 && rec.card.Faces[0] != nil {
			rec.facts.types = rec.card.Faces[0].Types
			rec.facts.colors = rec.card.Faces[0].Colors
		}
	}
	if i >= 0 && i < len(g.Snapshots) {
		for _, p := range g.Snapshots[i].Permanents {
			if p.AttachedTo != ref {
				continue
			}
			if c := refCard(reg, it, p.Ref, p, true); c != nil {
				rec.attached[c] = true
				if hasFaceType(c, "Equipment") {
					rec.facts.hasEquipment = true
				}
				if hasFaceType(c, "Aura") {
					rec.facts.hasAura = true
				}
			}
		}
	}
	seats := make([]string, 0, len(it.Setup))
	for seat := range it.Setup {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	for _, seat := range seats {
		for _, a := range it.Setup[seat].Attached {
			if a.To != ref {
				continue
			}
			if c, ok := reg.Lookup(oraclediffRefName(a.Card)); ok {
				rec.attached[c] = true
				if hasFaceType(c, "Equipment") {
					rec.facts.hasEquipment = true
				}
				if hasFaceType(c, "Aura") {
					rec.facts.hasAura = true
				}
			}
		}
	}
	return rec
}

func hasFaceType(c *cards.Card, typ string) bool {
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, t := range f.Types {
			if strings.EqualFold(t, typ) {
				return true
			}
		}
	}
	return false
}

// grantedPool is every card the item's setup or the entering snapshot places
// in a zone. The candidates are then scoped by the grant relationship, so a
// card in hand or a graveyard is only a donor when a GainsAbilitiesOf$ static
// actually names its zone.
func grantedPool(reg *cards.Registry, it Item, i int, ref string, g rules.OracleResult) []*grantedPoolCard {
	var out []*grantedPoolCard
	seen := map[string]bool{}
	add := func(c *cards.Card, zone state.Zone, seat int, perm rules.OracleSnapPerm, hasPerm bool) {
		if c == nil || len(c.Faces) == 0 {
			return
		}
		key := strconv.Itoa(int(zone)) + "\x00" + strconv.Itoa(seat) + "\x00" + perm.Ref + "\x00" + cardKey(c)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, &grantedPoolCard{card: c, zone: zone, seat: seat, perm: perm, hasPerm: hasPerm})
	}
	seats := make([]string, 0, len(it.Setup))
	for seat := range it.Setup {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	// The entering snapshot is the live board. When it exists, setup's
	// battlefield entries are only a pre-dispatch picture: a grantor that
	// left or a token that was not minted yet must not become a candidate.
	// Setup stays the fallback for a synthetic result with no snapshot.
	hasSnap := i >= 0 && i < len(g.Snapshots)
	if !hasSnap {
		for _, seat := range seats {
			s := it.Setup[seat]
			n, _ := refSeat(seat)
			for _, name := range s.Battlefield {
				if c, ok := reg.Lookup(name); ok {
					add(c, state.ZBattlefield, n, rules.OracleSnapPerm{}, false)
				}
			}
			for _, name := range s.BackFace {
				if c, ok := reg.Lookup(name); ok {
					add(c, state.ZBattlefield, n, rules.OracleSnapPerm{}, false)
				}
			}
			for _, zone := range []struct {
				names []string
				zone  state.Zone
			}{
				{s.Hand, state.ZHand}, {s.Graveyard, state.ZGraveyard}, {s.Exile, state.ZExile},
				{s.Command, state.ZCommand}, {s.Library, state.ZLibrary}, {s.LibraryTop, state.ZLibrary},
			} {
				for _, name := range zone.names {
					if c, ok := reg.Lookup(name); ok {
						add(c, zone.zone, n, rules.OracleSnapPerm{}, false)
					}
				}
			}
			for _, tok := range s.Tokens {
				if c := reg.Tokens[tok.Script]; c != nil {
					add(c, state.ZBattlefield, n, rules.OracleSnapPerm{}, false)
				}
			}
		}
	}
	if i >= 0 && i < len(g.Snapshots) {
		snap := g.Snapshots[i]
		for _, p := range snap.Permanents {
			add(refCard(reg, it, p.Ref, p, true), state.ZBattlefield, p.Controller, p, true)
		}
		for _, s := range snap.Players {
			for _, zone := range []struct {
				names []string
				zone  state.Zone
			}{
				{s.Hand, state.ZHand}, {s.Graveyard, state.ZGraveyard}, {s.Exile, state.ZExile},
				{s.Command, state.ZCommand}, {s.LibraryTop, state.ZLibrary},
			} {
				for _, name := range zone.names {
					if c, ok := reg.Lookup(name); ok {
						add(c, zone.zone, s.Seat, rules.OracleSnapPerm{}, false)
					}
				}
			}
		}
		for _, s := range snap.Stack {
			if p, ok := stepSnapshotPerm(s.Source, i, g); ok {
				add(refCard(reg, it, s.Source, p, true), state.ZStack, p.Controller, p, true)
			}
		}
	}
	// The recipient itself must be a grant source even when a synthetic
	// result carries no snapshot (its own max-speed/self AddAbility$ static).
	perm, found := stepSnapshotPerm(ref, i, g)
	add(refCard(reg, it, ref, perm, found), state.ZBattlefield, perm.Controller, perm, found)
	return out
}

// cardKey names a card by its front-face name for pool de-duplication.
func cardKey(c *cards.Card) string {
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return ""
	}
	return c.Faces[0].Name
}

// staticActiveInZone reports whether st functions from zone. A printed
// Continuous static without EffectZone$ works only from the battlefield
// (CR 113.6); EffectZone$ names the zones it functions from instead.
func staticActiveInZone(st cards.Static, zone state.Zone) bool {
	raw := strings.TrimSpace(st.ParamStr(cards.PKEffectZone))
	if raw == "" {
		return zone == state.ZBattlefield
	}
	zones, all, ok := effects.ParseZones(raw)
	if !ok {
		return false
	}
	if all {
		return true
	}
	for _, z := range zones {
		if z == zone {
			return true
		}
	}
	return false
}

// zoneAllows is staticActiveInZone for a GainsAbilitiesOfZones$ value, whose
// engine default is the battlefield (Forge's StaticAbilityContinuous default).
func zoneAllows(raw string, zone state.Zone) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return zone == state.ZBattlefield
	}
	zones, all, ok := effects.ParseZones(raw)
	if !ok {
		return false
	}
	if all {
		return true
	}
	for _, z := range zones {
		if z == zone {
			return true
		}
	}
	return false
}

// staticAffectsRecipient reports whether a Continuous static's filter reaches
// the step recipient. The parser has already rewritten AffectedDefined$ into
// the legacy Affected$ property form (cards.NormalizeAffectedDefined), so the
// Self/EnchantedBy/EquippedBy/AttachedBy properties are evaluated here.
func staticAffectsRecipient(st cards.Static, src *grantedPoolCard, rec grantRecipient) bool {
	spec := strings.TrimSpace(st.ParamStr(cards.PKAffected))
	if spec == "" {
		return true
	}
	return filtersCard(spec, rec.facts, src.card, rec.attached, src.ctrl())
}

// filtersCard evaluates a Forge filter spec against one card's facts. Filters
// the exporter cannot read are treated as satisfied, so a legitimate grant is
// never dropped; the type and controller words that decide the common
// "Creatures you control" grants are evaluated exactly.
func filtersCard(spec string, facts cardFacts, self *cards.Card, attached map[*cards.Card]bool, you int) bool {
	for _, alt := range splitFilterAlternatives(spec) {
		if alt == "" {
			continue
		}
		if filterAlternativeMatches(alt, facts, self, attached, you) {
			return true
		}
	}
	return false
}

// splitFilterAlternatives splits a filter's comma alternatives. Forge treats
// a comma in Affected$ as OR; each alternative is a conjunction.
func splitFilterAlternatives(spec string) []string {
	var out []string
	for _, part := range strings.Split(spec, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// filterAlternativeMatches evaluates one "+"-joined conjunction.
func filterAlternativeMatches(alt string, facts cardFacts, self *cards.Card, attached map[*cards.Card]bool, you int) bool {
	for _, part := range strings.Split(alt, "+") {
		for _, tok := range strings.Split(part, ".") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			if neg, ok := strings.CutPrefix(tok, "!"); ok {
				if filterTokenMatches(neg, facts, self, attached, you) {
					return false
				}
				continue
			}
			if !filterTokenMatches(tok, facts, self, attached, you) {
				return false
			}
		}
	}
	return true
}

// filterTokenMatches evaluates one filter word. Characteristic tests the
// exporter can answer are exact; a predicate it cannot read is accepted so a
// legitimate grant is not withheld.
func filterTokenMatches(tok string, facts cardFacts, self *cards.Card, attached map[*cards.Card]bool, you int) bool {
	switch strings.ToLower(tok) {
	case "card", "permanent", "any":
		return true
	case "self":
		return self != nil && facts.card == self
	case "other":
		return self == nil || facts.card != self
	case "enchantedby", "equippedby", "attachedby":
		return self != nil && attached[self]
	case "youctrl":
		return facts.ctrl == you
	case "youown":
		return facts.owner == you
	case "youdontown":
		return facts.owner != you
	case "equipped":
		return facts.hasEquipment
	case "enchanted":
		return facts.hasAura
	case "token":
		return facts.token
	case "nontoken":
		return !facts.token
	case "facedown", "isfacedown":
		return facts.faceDown
	case "multicolor":
		return len(facts.colors) > 1
	case "monocolor":
		return len(facts.colors) == 1
	case "kicked", "iscommander", "isremembered", "chosencolor", "chosentype", "hasyect":
		return true
	}
	if n, kind, ok := parseCounterSpec(tok); ok {
		return int(facts.counters[kind]) >= n
	}
	if isColorWord(tok) {
		if strings.EqualFold(tok, "colorless") {
			return facts.colors == ""
		}
		return strings.Contains(strings.ToUpper(facts.colors), strings.ToUpper(tok[:1]))
	}
	if isFilterTypeWord(tok) {
		return containsFold(facts.types, tok)
	}
	return true
}

// isColorWord reports whether tok names a colour a filter can test. The
// snapshot renders colours as WUBRG letters, so the test reads the first
// letter rather than a type word.
func isColorWord(tok string) bool {
	switch strings.ToLower(tok) {
	case "white", "blue", "black", "red", "green", "colorless":
		return true
	}
	return false
}

// parseCounterSpec reads Forge's counters_<op><n>_<kind> qualifier
// ("counters_GE8_LEVEL").
func parseCounterSpec(tok string) (int, string, bool) {
	rest, ok := strings.CutPrefix(tok, "counters_")
	if !ok {
		return 0, "", false
	}
	op, tail := "", rest
	for _, cand := range []string{"GE", "LE", "GT", "LT", "EQ"} {
		if strings.HasPrefix(rest, cand) {
			op, tail = cand, rest[len(cand):]
			break
		}
	}
	if op == "" {
		return 0, "", false
	}
	i := 0
	for i < len(tail) && tail[i] >= '0' && tail[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(tail) || tail[i] != '_' {
		return 0, "", false
	}
	n, err := strconv.Atoi(tail[:i])
	if err != nil {
		return 0, "", false
	}
	return n, tail[i+1:], true
}

// isFilterTypeWord is a Forge card type, supertype or colour word: it answers
// as a type test, anything else as a predicate the exporter does not evaluate.
// A subtype is any single capitalised word (engine/view's subtypeWord rule);
// predicates are camel case or lower case.
func isFilterTypeWord(tok string) bool {
	switch strings.ToLower(tok) {
	case "artifact", "battle", "creature", "enchantment", "instant", "land",
		"planeswalker", "sorcery", "tribal", "kindred",
		"basic", "legendary", "snow", "world":
		return true
	}
	return subtypeWord(tok)
}

func subtypeWord(tok string) bool {
	for _, word := range strings.Split(tok, " ") {
		if len(word) < 2 || word[0] < 'A' || word[0] > 'Z' {
			return false
		}
		for i := 1; i < len(word); i++ {
			if word[i] < 'a' || word[i] > 'z' {
				return false
			}
		}
	}
	return true
}

func containsFold(hay []string, needle string) bool {
	for _, s := range hay {
		if strings.EqualFold(s, needle) {
			return true
		}
	}
	return false
}

// staticAddAbilityNames lists the SVar names a static's AddAbility$/
// AddAbilities$ parameters grant.
func staticAddAbilityNames(st cards.Static) []string {
	var out []string
	for _, key := range []cards.ParamKey{cards.PKAddAbility, cards.PKAddAbilities} {
		out = append(out, splitParamList(st.ParamStr(key))...)
	}
	return out
}

// staticAddKeywordLines lists the AddKeyword$/AddKeywords$ lines a static
// grants.
func staticAddKeywordLines(st cards.Static) []string {
	var out []string
	out = append(out, splitParamList(st.ParamStr(cards.PKAddKeyword))...)
	out = append(out, splitParamList(st.ParamStr(cards.PKAddKeywords))...)
	return out
}

// splitParamList splits Forge's comma or " & " separated name lists.
func splitParamList(raw string) []string {
	var out []string
	for _, comma := range strings.Split(raw, ",") {
		for _, part := range strings.Split(comma, " & ") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// gainsValidAdmits applies the easy GainsValidAbilities$ filters: the
// activated half is already required, and !ManaAbility excludes the mana
// abilities. Anything else is admitted rather than guessed away.
func gainsValidAdmits(raw string, sa *cards.SA) bool {
	if sa == nil || !sa.IsActivated() {
		return false
	}
	if strings.Contains(strings.ToLower(raw), "!manaability") && cards.IsManaAbilitySA(sa) {
		return false
	}
	return true
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// refCard resolves a scenario ref to its card: a token script by seat and face
// name (narrowed by the snapshot when several scripts share the name), else
// the printed-name registry. Deterministic: setup tokens are scanned in slice
// order and token stems in sorted order; a remaining tie is no match.
func refCard(reg *cards.Registry, it Item, ref string, perm rules.OracleSnapPerm, found bool) *cards.Card {
	name := oraclediffRefName(ref)
	if _, isToken := strings.CutPrefix(oraclediffRefNameUnmarked(ref), "token:"); isToken {
		return grantedTokenCard(reg, it, ref, name, perm, found)
	}
	if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 {
		return c
	}
	return nil
}

// oraclediffRefNameUnmarked is oraclediffRefName without stripping the
// "token:" marker, so refCard can tell a token ref from a printed card whose
// name happens to be a token's.
func oraclediffRefNameUnmarked(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

func grantedTokenCard(reg *cards.Registry, it Item, ref, name string, perm rules.OracleSnapPerm, found bool) *cards.Card {
	if seat, _, has := strings.Cut(ref, ":"); has {
		for _, t := range it.Setup[seat].Tokens {
			if c := reg.Tokens[t.Script]; c != nil && len(c.Faces) > 0 && c.Faces[0].Name == name {
				return c
			}
		}
	}
	if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 {
		return c
	}
	stems := make([]string, 0, len(reg.Tokens))
	for stem := range reg.Tokens {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	var cands []*cards.Card
	for _, stem := range stems {
		c := reg.Tokens[stem]
		if c != nil && len(c.Faces) > 0 && c.Faces[0].Name == name {
			cands = append(cands, c)
		}
	}
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) == 0 || !found {
		return nil
	}
	cands = filterGrantedCards(cands, func(c *cards.Card) bool {
		return stringSetSubset(c.Faces[0].Keywords, perm.Keywords)
	})
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) == 0 {
		return nil
	}
	cands = filterGrantedCards(cands, func(c *cards.Card) bool {
		return stringSetEqual(c.Faces[0].Types, perm.Types)
	})
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) == 0 {
		return nil
	}
	cands = filterGrantedCards(cands, func(c *cards.Card) bool {
		return c.Faces[0].PT == perm.PT
	})
	if len(cands) == 1 {
		return cands[0]
	}
	return nil
}

func filterGrantedCards(in []*cards.Card, keep func(*cards.Card) bool) []*cards.Card {
	out := make([]*cards.Card, 0, len(in))
	for _, c := range in {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

func stringSetSubset(a, b []string) bool {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	for _, s := range a {
		if !set[s] {
			return false
		}
	}
	return true
}

func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return stringSetSubset(a, b)
}
