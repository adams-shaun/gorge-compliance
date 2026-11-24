package main

// The Forge oracle's gorge side.
//
//	oraclediff forge-export [-cards .cards] [-runner PATH] -scenarios scen.jsonl
//	                       -out forge-req.jsonl (refusals to <dir>/forge-prep.jsonl)
//	oraclediff forge-diff   [-cards .cards] -req forge-req.jsonl [-forge forge.jsonl]
//	                        (-cache DIR | -oracle-ref REF -driver SHA) -out forge-diff.jsonl
//	oraclediff follower-diff -engine NAME [forge-diff's flags]
//	oraclediff follower-export -engine NAME [-cards .cards] [-runner PATH]
//	                        -scenarios items.jsonl -out req.jsonl
//
// forge-export replays each gen Item in gorge once and writes the sidecar
// request the Forge driver reads: the Item exactly as gen wrote it, the
// decisions gorge's runner made (the answer source) and, per ability_index
// step, the raw script line of the ability that index names. forge-diff
// compares gorge with the Forge driver's rows through the unchanged
// oraclediff.CompareOpts and caches those rows by request sha. Neither
// command touches gen's output or oracle/compliance/verdicts, and both refuse an
// output path inside this repository: the request carries script text and
// Forge's rows are Forge output, neither of which may be committed.
//
// follower-diff is forge-diff for any registered follower whose items are
// Forge requests (follower.PrepForgeExport or follower.PrepFollowerExport);
// forge-diff is follower-diff -engine forge. forge-export asks
// forge-oracle-run.sh's --preflight which cards the pinned Forge cannot
// resolve and refuses the items naming one to <dir of out>/forge-prep.jsonl
// (xengine diff reads those as class not_sent). follower-export
// (follower_export.go) is forge-export plus a follower's own preflight
// refusals and the engine-neutral identities.

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/rules/pay"
)

// defaultForgeOracleDir is the gitignored directory holding the Forge build
// and caches, relative to the repository root, used when FORGE_ORACLE_DIR is
// unset (scripts/forge-oracle-setup.sh writes it).
const defaultForgeOracleDir = ".oracle-build/forge"

// forgeRequest is one line of forge-req.jsonl. The Item is marshalled
// exactly as gen wrote it, so its bytes (and gate.ItemSHA) are unchanged.
//
// The contract the Forge driver reads:
//
//   - A divided allocation (Twin Bolt, Forked Bolt, distribute counters) is
//     not a field: it is its own decision AFTER the target ask, a
//     "choose_n" with resume "damage_split" (for a spell gorge announces the
//     division at cast, CR 601.2d, and oraclegen.CastDivisions restores the
//     decision at the step the spell resolves in), whose options are the chosen
//     targets in order (pick_refs) and whose pick_idx repeats a target's
//     index once per point it receives. min == max == the total. The target
//     ask's own "divided" is the literal total only for a triggered ability
//     and is 0 for a spell cast from hand, so the driver sums the split.
//   - A card ref's ordinal ("p0:Wastes#27") counts gorge's object order,
//     which the deal shuffles. Name and owner are the identity; the driver
//     resolves within the candidates Forge offers, which are one zone.
//   - An untap election (resume "untap") is exported with the exact offered
//     object of each pick (pick_idx into option_refs), so a same-named copy
//     is named, not just its name. A pick of a permanent that is already
//     untapped at the decision's step is a legal no-op in gorge (CR 701.26b:
//     only a tapped permanent can be untapped), but Forge's UntapEffect
//     filters its candidates to canUntap, so the object is not offered and
//     the driver's name fallback would untap a different same-named
//     permanent instead. The no-op pick is dropped; when none remain and the
//     ask is "up to" (min 0), the decision becomes an empty choose_n, which
//     the driver consumes as "untap nothing", exactly as gorge did.
//   - A multi-card hand -> library batch (resume "hand_move", every pick kind
//     "hand_move") that lands on top is exported in reverse: Forge's
//     ChangeZoneEffect asks for one card at a time and puts each successive
//     pick on top, so the last pick ends topmost and gorge's top-first order
//     needs reversing. The top is re-derived from the checkpoint after the
//     decision's step (oraclegen.LibraryTopPlacement), so a plain top-only
//     put-back with no "changezone_alternative" (Stunted Growth, Brainstorm)
//     is covered too; a following "top" alternative is the fallback when the
//     checkpoint has no library top. A "bottom" placement needs no reversal
//     (successive puts on the bottom append, leaving the first pick above the
//     second). Only the parallel pick arrays reverse; the label fields stay.
//   - A "library_placement" arrange that lands on top (Harmonic Convergence's
//     "put all enchantments on top") is exported in reverse for the same
//     reason: its picks are top-first and the driver places successive picks
//     on top. A bottom arrangement keeps gorge's order. Like the hand_move
//     batch, the direction is the after-step library top, not a card list.
//   - A surveil/scry arrange whose looked-at cards are all tokens (Vanish from
//     Sight puts a token on top, then surveils 1) is exported with picks naming
//     the cards now on top of the seat's library, so Forge keeps what it looks
//     at (see forgeTokenLooks).
//   - A Soulbond pairing answer (resume "soulbond", the one pick kind "pair")
//     names the creature Forge's BondEffect asks for by trigger half: the
//     "another unpaired creature enters" half asks which SOURCE creature to
//     pair with (ValidCards$ Creature.Self+!Paired) while gorge answered the
//     entering partner it offered, so the pick is rewritten to the source;
//     the "this creature enters" half already asks the partner and is left
//     alone (see forgeSoulbondPairSource).
//   - Except for the untap, top hand_move, token-look and Soulbond pair
//     rewrites above, decisions are exported verbatim.
//     pay_<C>, pay_generic and pay_life are
//     pick kinds of gorge's hybrid/Phyrexian pip ask, and a mana-ability
//     "activate" pick is a payment-window choice; Forge asks neither, so the
//     driver does not count them as leftover. A "Choose a mana ability of
//     <name>" wheel on an activate step is dropped for the same reason: the
//     step's ability_index already names the ability, so the driver poses no
//     wheel. A colour pick of an any-colour ability stays (it is a real
//     choice).
//   - arrange: the picks are the cards kept on top in pick order; the
//     pick_kinds name where the unpicked go (bottom, graveyard, exile, hand),
//     in offered order. hideaway_bottom and dig_bottom leave none unpicked:
//     every offered card goes to the bottom and the picks give its order.
//   - trigger_cost_pay and trigger_cost_decline answer the payment of a
//     triggered ability's cost; gift_decline declines a gift (gift_promise
//     names the opponent in its label).
//   - A cast step's cast_mode names a non-default cast option the driver
//     must select: the plot or suspend special action, an Adventure or Omen half, a
//     modal back face, a warp cast, an optional cost paid at cast time
//     ("optionalcost", kicked, buyback, entwined, offspring, replicated);
//     empty is the ordinary cast. Modes outside xengine/classify's
//     forgeElectedCastModes are cast the ordinary way. gen's Step still
//     carries no kicked: a kick is paid only through a logged optional-cost
//     decision.
//   - The Item's payment array is parallel to its steps: entry i is the exact
//     pool mana gorge spent on step i's cast or activate, as WUBRGC letters
//     (one per unit): a multiset, not an order. A driver that can name pool
//     mana spends exactly that; empty means auto-pay. It is an Item field,
//     not a Step one, so Raw() never sends it to gorge's runner.
//   - item.die_rolls lists every die roll a step made (step, seat, sides,
//     natural, result), in log order. A driver must force the NATURAL roll
//     before running that step and let its own Modifier$ apply, or its
//     follower rolls its own die and a card like Ebony Fly animates to a
//     different P/T. It is an Item field, not a Step one, so Raw() never
//     sends it to gorge's runner, and it is not a gorge decision: Forge poses
//     no decision for a die, so exporting it in gorge_decisions would be an
//     unconsumed leftover.
//   - item.coin_flips lists every coin flip a step made (step, seat, win,
//     forced, draws), in log order. Forge's FlipCoinEffect takes its result
//     from MyRandom's nextBoolean, so a driver must force each result before
//     running that step or it flips from its own stream and a card like Mana
//     Clash (1 damage per tails, repeat until both heads) loops a different
//     number of times. A forced flip (a FlipCoinMod static) takes no result
//     from the generator and a doubled flip takes two. Like die_rolls it is an
//     Item field, never sent to gorge's runner, and not a gorge decision.
//   - abilities is keyed by the decimal step index.
//   - library_top is not compared when a seat's setup names library cards
//     (see forgeIgnore).
//   - A Forge row may echo request_sha, the sha256 of this line's bytes
//     without the newline; forge-diff drops a row whose echo differs.
type forgeRequest struct {
	ID          string         `json:"id"`
	ScenarioSHA string         `json:"scenario_sha"`
	Item        oraclegen.Item `json:"item"`
	// GorgeDecisions is the answer source: what gorge's deterministic
	// runner answered, so both engines answer alike.
	GorgeDecisions []rules.OracleDecision `json:"gorge_decisions"`
	// Abilities maps a step index to the raw script line of the ability
	// that step's ability_index names, the key the driver matches on.
	Abilities map[string]string `json:"abilities"`
}

// ForgeRow is one line of forge-diff's output.
type ForgeRow struct {
	ID          string             `json:"id"`
	Card        string             `json:"card"`
	Template    string             `json:"template"`
	Verdict     oraclediff.Verdict `json:"verdict"`
	ForgeMS     int                `json:"forge_ms"`
	RequestSHA  string             `json:"request_sha"`
	ScenarioSHA string             `json:"scenario_sha"`
}

func runForgeExport(args []string) error {
	fs := flag.NewFlagSet("forge-export", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	scen := fs.String("scenarios", "", "scenario JSONL (from gen or xengine play)")
	out := fs.String("out", "", "sidecar request JSONL (off-repo); refusals go to <dir>/forge-prep.jsonl")
	runner := fs.String("runner", "", "preflight runner (default: the registry's runner for forge)")
	fs.Parse(args)
	if *scen == "" || *out == "" {
		return fmt.Errorf("forge-export needs -scenarios and -out")
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	fl, ok := follower.Lookup("forge")
	if !ok {
		return fmt.Errorf("no forge follower in the registry")
	}
	if *runner == "" {
		*runner = fl.Runner
	}
	st, err := forgeExportRefusing(reg, fl, *runner, *scen, *out)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d requests to %s; %d of %d items refused (%d of %d cards lacking) to %s\n",
		st.Sent, *out, st.Refused, st.Items, st.Lacking, st.Asked, followerPrepPath(fl, *out))
	return nil
}

// forgeExportRefusing writes Forge's requests for the items in scen to out
// and its refusals beside it, asking runner's --preflight which cards the
// pinned Forge cannot resolve (the protocol in the file comment). An item
// naming a refused card is not sent: its refusal row, reason
// forge_lacks_card, goes beside the requests to <dir of out>/forge-prep.jsonl
// for xengine diff to read as class not_sent. The Forge pin's cardsfolder
// lags the corpus, so without this an item naming a newer card dies in
// Forge's harness with "Couldn't find a card" while gorge's play already
// counted it as compared.
func forgeExportRefusing(reg *cards.Registry, fl follower.Follower, runner, scen, out string) (followerExportStats, error) {
	st := followerExportStats{Census: census{}}
	prepPath := followerPrepPath(fl, out)
	for _, p := range []string{out, prepPath} {
		if err := refuseRepoPath(p); err != nil {
			return st, err
		}
	}
	items, itemNames, lacks, asked, err := preflightNames(reg, fl, runner, scen, filepath.Dir(out))
	if err != nil {
		return st, err
	}
	st.Items, st.Asked, st.Lacking = asked.Items, asked.Asked, asked.Lacking
	st.Sent, st.Refused, err = exportItems(items, itemNames, lacks, fl.Name, out, prepPath, func(it oraclegen.Item) ([]byte, error) {
		rq, _ := forgeExportItem(reg, it, true)
		return json.Marshal(rq)
	})
	return st, err
}

// forgeExport writes one request line per scenario Item, in input order.
func forgeExport(reg *cards.Registry, scen, out string) (int, error) {
	if err := refuseRepoPath(out); err != nil {
		return 0, err
	}
	of, err := os.Create(out)
	if err != nil {
		return 0, err
	}
	defer of.Close()
	w := bufio.NewWriter(of)
	n := 0
	if err := readLines(scen, func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		rq, _ := forgeExportItem(reg, it, true)
		line, err := json.Marshal(rq)
		if err != nil {
			return err
		}
		n++
		w.Write(line)
		return w.WriteByte('\n')
	}); err != nil {
		return n, err
	}
	if err := w.Flush(); err != nil {
		return n, err
	}
	return n, of.Close()
}

// forgeExportItem is the request forge-export writes for one item, and
// gorge's result for it: the item is replayed once in gorge and the request
// built from the decisions gorge's runner made. forgeDriver opts into the
// rewrites and roles only the Forge driver consumes ("graveyard_order",
// reversing a top hand -> library batch); a caller whose engine queue has no
// consumer for them (follower-export, see follower_export.go) must pass false,
// or an unasked role becomes an unconsumed leftover. A failed gorge run has no
// checkpoints to derive the role from, so it is never pinned.
func forgeExportItem(reg *cards.Registry, it oraclegen.Item, forgeDriver bool) (forgeRequest, rules.OracleResult) {
	g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
	// A divided spell's shares reach the driver as a "damage_split"
	// decision (the contract above), which gorge now announces at cast.
	decisions := oraclegen.CastDivisions(reg, it.Scenario, g)
	if gerr != nil || decisions == nil {
		decisions = []rules.OracleDecision{}
	}
	return forgeBuildRequest(reg, it, g, decisions, forgeDriver && gerr == nil), g
}

// forgeBuildRequest assembles the request for one item from gorge's decisions
// for it: the scenario SHA names the item as given, the decisions lose those
// for steps the driver will not pose, and the steps are then rewritten.
// forgeDriver is explicit at every call site: only the Forge driver consumes
// the Forge-only rewrites and role, and a new caller has to say so instead of
// inheriting them.
func forgeBuildRequest(reg *cards.Registry, it oraclegen.Item, g rules.OracleResult, decisions []rules.OracleDecision, forgeDriver bool) forgeRequest {
	sha := gate.ItemSHA(it)
	decisions = forgeDropNamedManaAbilityDecisions(reg, it, g, forgeDropDeclarationDecisions(it, decisions))
	decisions = forgeExploreAsYesNo(decisions)
	decisions = forgeUntapNoOps(g, decisions)
	decisions = oraclegen.RetimeEntryTargets(it.Steps, g.Snapshots, decisions)
	if forgeDriver {
		decisions = forgeLibraryTopOrder(decisions, g.Snapshots)
		decisions = forgeTokenLooks(g, decisions)
		decisions = forgeSoulbondPairSource(reg, g, decisions)
		decisions = forgeGraveyardOrderDecisions(it, g, decisions)
	} else {
		decisions = forgeDropGraveyardOrderDecisions(decisions)
	}
	it = forgeDeclarations(it)
	return forgeRequest{
		ID: it.ID, ScenarioSHA: sha, Item: it,
		GorgeDecisions: decisions, Abilities: forgeAbilityLines(reg, it, g),
	}
}

// forgeDeclarations adapts, in an xengine item, the steps around each attack
// and block to how the Forge driver reaches the declaration. Steps keep their
// index, so the per-step side tables (abilities, decisions) stay aligned.
// Other items pass through unchanged, and the caller hashes the item before
// this rewrite, so the scenario SHA still names the lowered item.
//
//   - A declaration with nothing declared (no attackers, no blocks) becomes a
//     pass_to decision "priority". Forge poses a declaration only to a player
//     with a creature able to make it, and an empty combat has no priority in
//     that step, so the driver cannot follow the step (it reports "passed
//     declare-attackers without being asked"). The driver completes a pass_to
//     "priority" at once: it snapshots the current state and consumes no
//     priority, which is all gorge's empty declaration does, and the passes
//     after it walk on from where they were. (A pass_to the declaration step
//     would need a further priority and desync the following pass.)
//   - The pass just before a declaration that declares something becomes a
//     pass_to decision "attackers" or "blockers". gorge's pass leaves the
//     decision pending, but Forge asks for the declaration before the priority
//     where a pass would complete, so the driver's callback finds that pass as
//     the current step and declares nothing. A pass_to of that decision
//     completes inside the callback, which then declares the next step. The
//     pass it replaces passes the same priority first.
func forgeDeclarations(it oraclegen.Item) oraclegen.Item {
	if !lower.IsRecorded(it.Template) {
		return it
	}
	var steps []oraclegen.Step
	set := func(i int, st oraclegen.Step) {
		if steps == nil {
			steps = append([]oraclegen.Step(nil), it.Steps...)
		}
		steps[i] = st
	}
	for i, st := range it.Steps {
		decision, empty, ok := forgeDeclaration(st)
		if !ok {
			continue
		}
		if empty {
			set(i, oraclegen.Step{Op: "pass_to", Decision: "priority", Expect: st.Expect, Answers: st.Answers, Targets: st.Targets})
		} else if i > 0 && it.Steps[i-1].Op == "pass" {
			p := it.Steps[i-1]
			set(i-1, oraclegen.Step{Op: "pass_to", Decision: decision, Expect: p.Expect, Answers: p.Answers, Targets: p.Targets})
		}
	}
	if steps != nil {
		it.Steps = steps
	}
	return it
}

// forgeDeclaration reports, for an attack or block step, gorge's decision
// kind for it ("attackers"/"blockers") and whether nothing is declared. ok is
// false for any other step.
func forgeDeclaration(st oraclegen.Step) (decision string, empty, ok bool) {
	switch st.Op {
	case "attack":
		return "attackers", len(st.Attackers) == 0, true
	case "block":
		return "blockers", len(st.Blocks) == 0, true
	}
	return "", false, false
}

// forgeDropDeclarationDecisions removes gorge's attackers/blockers decision
// for every attack and block step of an xengine item. The driver declares
// from the step's own attackers/blocks list (its declareAttackers and
// declareBlockers callbacks never take a queued decision), so it would never
// consume the decision and would report it as an unconsumed gorge decision.
// That holds for an empty declaration (forgeDeclarations rewrites it to a
// pass_to "priority", which the driver never poses) and for a stated one. Call
// it on the item before the rewrite, since decisions carry the unrewritten
// step indexes. The input slice is not modified; a list with nothing to drop
// is returned as is.
func forgeDropDeclarationDecisions(it oraclegen.Item, decisions []rules.OracleDecision) []rules.OracleDecision {
	if !lower.IsRecorded(it.Template) {
		return decisions
	}
	declared := map[int]string{}
	for i, st := range it.Steps {
		if decision, _, ok := forgeDeclaration(st); ok {
			declared[i] = decision
		}
	}
	if len(declared) == 0 {
		return decisions
	}
	out := make([]rules.OracleDecision, 0, len(decisions))
	for _, d := range decisions {
		if kind, ok := declared[d.Step]; ok && d.Kind == kind {
			continue
		}
		out = append(out, d)
	}
	return out
}

// forgeExploreAsYesNo rewrites gorge's explore pick (CR 701.44a: a nonland
// card revealed by explore goes back on top or into the graveyard) into the
// yes/no decision Forge poses for it, "[Explore] Put <card> in your
// graveyard?". gorge logs it as a choose_n with resume "explore" and one pick
// of kind "graveyard" (option 0) or "top" (option 1, "keep" in older
// snapshots); the driver's DecisionQueue only matches a yes/no ask to a
// choose_n whose single pick has kind yes or no (isYesNo), so left as is the
// ask is a strict miss and the pick an unconsumed gorge decision. "yes" puts
// the card into the graveyard. The pick loses its object refs: the card is
// the one revealed, not a choice among objects. Step, seat and order are
// kept, so the decision stays on the step it was logged at. The input slice
// is not modified; a list with no explore pick is returned as is.
func forgeExploreAsYesNo(decisions []rules.OracleDecision) []rules.OracleDecision {
	var out []rules.OracleDecision
	for i, d := range decisions {
		pick, ok := forgeExplorePick(d)
		if !ok {
			continue
		}
		if out == nil {
			out = append([]rules.OracleDecision(nil), decisions...)
		}
		label := "No"
		if pick == "yes" {
			label = "Yes"
		}
		d.Picks = []string{label}
		d.PickKinds = []string{pick}
		d.PickIdx = nil
		d.PickRefs = nil
		d.PickRefsInexact = nil
		d.ObjectPicks = nil
		d.OptionRefs = nil
		d.First = "Yes"
		out[i] = d
	}
	if out == nil {
		return decisions
	}
	return out
}

// forgeExplorePick reports gorge's explore pick as "yes" (graveyard) or "no"
// (back on top).
func forgeExplorePick(d rules.OracleDecision) (string, bool) {
	if d.Kind != "choose_n" || d.Resume != "explore" || len(d.PickKinds) != 1 || len(d.Picks) != 1 {
		return "", false
	}
	switch d.PickKinds[0] {
	case "graveyard":
		return "yes", true
	case "top", "keep":
		return "no", true
	}
	return "", false
}

// forgeUntapNoOps rewrites every untap election for the Forge driver: a pick
// is renamed to the exact object its option offered, and a pick that names a
// permanent already untapped in the checkpoint entering the decision's step
// is dropped as a no-op. If every pick is a no-op and the ask is "up to"
// (min 0), the decision becomes an empty object choice, which the driver
// consumes and answers with no card -- the same result as gorge's no-op.
//
// Gorge's untap ask (UntapUpTo$/UntapExactly$) offers every candidate,
// tapped or not (engine/effects/untap.go), so an untapped pick is legal but
// does nothing (CR 701.26b: only a tapped permanent can be untapped). Forge's
// UntapEffect filters its list to canUntap, so that object is not among the
// candidates the driver offers. The driver's card-ref resolution then falls
// back to "another <name> (gorge's ordinal names a different copy)" and
// untaps a different same-named permanent (xe:RAND:0:7:w14, Frantic Search:
// the base Island is untapped and a different Island was untapped). The
// request cannot carry Forge's filtered candidate list, so the no-op is
// encoded as the empty answer the ask's min 0 already allows.
//
// The option identity is read from pick_idx/option_refs, the same candidate
// list Forge sees: a bare pick ref ("p0:Island") may name the wrong copy when
// several are offered, while the option's ref names the exact one. A pick the
// checkpoint does not list, or an exact ask (min > 0) whose every pick is a
// no-op, is left as it is: dropping a real or required choice is worse than
// the fallback. The input slice is not modified; a list with no untap
// election is returned as is.
func forgeUntapNoOps(g rules.OracleResult, ds []rules.OracleDecision) []rules.OracleDecision {
	var out []rules.OracleDecision
	for i, d := range ds {
		if !isUntapElection(d) {
			continue
		}
		next, changed := forgeDropUntappedPicks(d, g)
		if !changed {
			continue
		}
		if out == nil {
			out = append([]rules.OracleDecision(nil), ds...)
		}
		out[i] = next
	}
	if out == nil {
		return ds
	}
	return out
}

// isUntapElection reports whether d is gorge's "untap up to/exactly N"
// election: a choose_n with resume "untap" whose picks all chose the engine's
// "untap" option. The untap-step "keep it tapped?" election has no resume
// kind, and a TapOrUntap target election resumes "taporuntap", so neither is
// rewritten.
func isUntapElection(d rules.OracleDecision) bool {
	if d.Kind != "choose_n" || d.Resume != "untap" || len(d.Picks) == 0 || len(d.Picks) != len(d.PickRefs) {
		return false
	}
	for _, k := range d.PickKinds {
		if k != "untap" {
			return false
		}
	}
	return true
}

// forgeDropUntappedPicks returns an untap election with its already-untapped
// picks removed and each kept pick ref replaced by the exact offered object,
// and whether anything changed. A pick the checkpoint does not list keeps its
// recorded ref: the state is not known, and dropping a real choice is worse
// than an inexact one. An exact ask (min > 0) whose every pick is a no-op is
// left unchanged, because an empty answer would be illegal.
func forgeDropUntappedPicks(d rules.OracleDecision, g rules.OracleResult) (rules.OracleDecision, bool) {
	if d.Step < 0 || d.Step >= len(g.Snapshots) {
		return d, false
	}
	tapped := make(map[string]bool, len(g.Snapshots[d.Step].Permanents))
	for _, p := range g.Snapshots[d.Step].Permanents {
		tapped[p.Ref] = p.Tapped
	}
	refs := make([]string, len(d.PickRefs))
	keep := make([]bool, len(d.PickRefs))
	dropped := 0
	for k := range d.PickRefs {
		ref := untapPickRef(d, k)
		refs[k] = ref
		if t, known := tapped[ref]; known && !t {
			dropped++
			continue
		}
		keep[k] = true
	}
	if dropped == 0 {
		changed := false
		for k := range refs {
			if refs[k] != d.PickRefs[k] {
				changed = true
				break
			}
		}
		if !changed {
			return d, false
		}
	}
	if dropped == len(d.PickRefs) && d.Min > 0 {
		return d, false
	}
	return filterUntapPicks(d, keep, refs), true
}

// untapPickRef is pick k's exact offered object: the option its pick_idx
// names when option_refs is parallel to the whole option list (it is for an
// untap ask, whose every option is an object), else the recorded ref.
func untapPickRef(d rules.OracleDecision, k int) string {
	if k < len(d.PickIdx) && d.Options > 0 && d.Options == len(d.OptionRefs) {
		if i := d.PickIdx[k]; i >= 0 && i < len(d.OptionRefs) && d.OptionRefs[i] != "" {
			return d.OptionRefs[i]
		}
	}
	if k < len(d.PickRefs) {
		return d.PickRefs[k]
	}
	return ""
}

// filterUntapPicks returns d with the parallel pick slices filtered by keep;
// refs replaces each kept pick's ref with its exact offered object. A slice
// that is not parallel to PickRefs is left alone, and an emptied pick list is
// emptied in every slice so the driver's isEmptyChoice sees a declined ask
// (kind choose_n, no picks, no pick kinds).
func filterUntapPicks(d rules.OracleDecision, keep []bool, refs []string) rules.OracleDecision {
	n := len(d.PickRefs)
	filterStrings := func(s []string) []string {
		if len(s) == 0 || len(s) != n {
			return s
		}
		out := make([]string, 0, len(s))
		for k, v := range s {
			if keep[k] {
				out = append(out, v)
			}
		}
		return out
	}
	d.Picks = filterStrings(d.Picks)
	d.PickKinds = filterStrings(d.PickKinds)
	d.PickRefs = filterStrings(append([]string(nil), refs...))
	if len(d.ObjectPicks) == n {
		// The driver's DecisionQueue.refs() prefers pick_refs only when it
		// contains every object_picks entry; keep the two parallel and exact.
		out := make([]string, 0, n)
		for k := range d.ObjectPicks {
			if keep[k] {
				out = append(out, refs[k])
			}
		}
		d.ObjectPicks = out
	} else {
		d.ObjectPicks = filterStrings(d.ObjectPicks)
	}
	if len(d.PickRefsInexact) == n {
		out := make([]bool, 0, n)
		for k, v := range d.PickRefsInexact {
			if keep[k] {
				out = append(out, v)
			}
		}
		d.PickRefsInexact = out
	}
	if len(d.PickIdx) == n {
		out := make([]int, 0, n)
		for k, v := range d.PickIdx {
			if keep[k] {
				out = append(out, v)
			}
		}
		d.PickIdx = out
	}
	return d
}

// forgeDropNamedManaAbilityDecisions removes gorge's "Choose a mana ability of
// <name>" decision for an activate step that already names the chosen ability
// by ability_index. forgeAbilityLines sends the driver that exact ability's
// script line, so Forge activates it and poses no ability wheel: the decision
// would only be reported as an unconsumed gorge decision. A colour pick of an
// any-colour ability is also pick kind "mana" but is a real choice, so a
// decision is dropped only when its pick names the labelled ability itself.
// Call it on the item before forgeDeclarations rewrites the steps, since
// decisions carry the unrewritten step indexes. The input slice is not
// modified; a list with nothing to drop is returned as is.
func forgeDropNamedManaAbilityDecisions(reg *cards.Registry, it oraclegen.Item, g rules.OracleResult, decisions []rules.OracleDecision) []rules.OracleDecision {
	if !lower.IsRecorded(it.Template) {
		return decisions
	}
	named := map[int]namedManaAbility{}
	for i, st := range it.Steps {
		// A granted step's ability is resolved by its grant identity, not
		// the printed index's ability: dropping a wheel decision against
		// the printed face could only drop the WRONG wheel. Resolve first
		// so a pre-fix item (no Step.Ability label) is covered too. A
		// label-bearing step that does not resolve is still not a printed
		// mana activation, so it is skipped rather than anchored on the
		// aliased index.
		if st.Op != "activate" || st.AbilityIndex == nil || st.Card == "" {
			continue
		}
		if _, kind := oraclegen.ResolveActivatedAbility(reg, it, i, st, g); kind != oraclegen.ActivatedPrinted {
			continue
		}
		if st.Ability != "" {
			continue
		}
		if f := forgeAbilityFace(reg, it, st, g, i); f != nil {
			named[i] = namedManaAbility{face: f, index: *st.AbilityIndex}
		}
	}
	if len(named) == 0 {
		return decisions
	}
	out := make([]rules.OracleDecision, 0, len(decisions))
	for _, d := range decisions {
		if a, ok := named[d.Step]; ok && a.drops(d) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// namedManaAbility is an activate step's resolved ability, the anchor for
// dropping the "Choose a mana ability" wheel decision.
type namedManaAbility struct {
	face  *cards.Face
	index int
}

// drops reports whether d is the wheel ask that chose a.face.Abilities[index]:
// a one-pick choose_n whose only options are mana and whose pick is that
// ability's wheel label. An explicit "Combo <colours>" ability is flattened
// into one option per colour in the wheel (pay.ManaAbilityComboColours), so
// its per-colour labels count when the face offers a wheel at all; a
// single-ability source never poses the wheel, and its colour ask ("Add W" for
// an "Add any color" ability) must stay. A "Chosen" production is labelled
// with the source's recorded colour, so the wheel labels for every colour
// count too.
func (a namedManaAbility) drops(d rules.OracleDecision) bool {
	if d.Kind != "choose_n" || d.Options <= 1 || len(d.Picks) != 1 || len(d.PickKinds) != len(d.Picks) {
		return false
	}
	for _, kind := range d.PickKinds {
		if kind != "mana" {
			return false
		}
	}
	if a.index < 0 || a.index >= len(a.face.Abilities) || a.face.Abilities[a.index] == nil {
		return false
	}
	sa := a.face.Abilities[a.index]
	if !cards.IsManaAbilitySA(sa) {
		return false
	}
	labels := map[string]bool{pay.ManaAbilityLabel(sa, ""): true}
	if len(a.face.ManaAbilities()) > 1 {
		prefix := pay.ManaAbilityCostPrefix(sa)
		for _, chosen := range []string{"W", "U", "B", "R", "G"} {
			labels[pay.ManaAbilityLabel(sa, chosen)] = true
			cols, ok := pay.ManaAbilityComboColours(sa, chosen)
			if !ok {
				continue
			}
			for _, col := range cols {
				labels[prefix+"Add "+pay.ManaAmountPips(sa, col)] = true
			}
		}
	}
	pick := d.Picks[0]
	if pick == "" && len(d.PickRefs) == 1 {
		pick = d.PickRefs[0]
	}
	return labels[pick]
}

// forgeAbilityLines maps each step that names an ability by ability_index to
// the script line (cards.SA.Line) of Face.Abilities[index] on the step's card.
// It reads face 1 when that card is listed in the owning seat's back_face
// setup. A step the setup face does not resolve is retried against the
// replay's snapshot entering that step: a token is not in the printed-name
// registry (tokens live in Registry.Tokens), and a card that transforms
// during the window is only on its back face from that step on. The retry
// only fills steps left out today, so an already-emitted line never changes
// and the Forge result cache stays valid. A step that still does not resolve
// is left out: the driver then falls back to its own matching rather than
// trusting a wrong line.
//
// A label-bearing step names a granted/gained/keyword option; its aliased
// AbilityIndex must never be exported as the printed ability. If the grant
// does not resolve, the step is omitted (fail closed) instead of falling
// back to the printed index -- the silent wrong export this resolver exists
// to remove.
func forgeAbilityLines(reg *cards.Registry, it oraclegen.Item, g rules.OracleResult) map[string]string {
	lines := map[string]string{}
	for i, st := range it.Steps {
		// A granted/gained/keyword step's identity is its grant, not the
		// printed index (which aliases printed ability 0 for an SVar
		// grant); a pre-fix item with no label is resolved from gorge's
		// recorded pick. A printed pick keeps the index-only path
		// byte-identical; an unresolved granted pick is omitted even when
		// the pre-fix step carries no label.
		switch sa, kind := oraclegen.ResolveActivatedAbility(reg, it, i, st, g); kind {
		case oraclegen.ActivatedGranted:
			lines[strconv.Itoa(i)] = sa.Line
			continue
		case oraclegen.ActivatedGrantedUnresolved:
			continue
		}
		if st.Ability != "" {
			continue
		}
		if f := forgeAbilityFace(reg, it, st, g, i); f != nil {
			line, _ := abilityLine(f, *st.AbilityIndex)
			lines[strconv.Itoa(i)] = line
		}
	}
	return lines
}

// forgeAbilityFace is the face whose Abilities[ability_index] step i names,
// resolved as forgeAbilityLines documents, or nil when the step names no
// ability or none resolves.
//
// A multi-face card can change face during the window without a setup
// back_face entry: a modal DFC played as its land back face (CR 712.12) has
// its front spell at the same printed index as the back land's mana ability.
// The printed face would then resolve, but to the wrong ability, so for a
// multi-face card the snapshot entering the step is consulted first: it names
// the face that is actually up. A single-face card keeps the setup path, so an
// already-emitted line never changes and the Forge result cache stays valid.
func forgeAbilityFace(reg *cards.Registry, it oraclegen.Item, st oraclegen.Step, g rules.OracleResult, i int) *cards.Face {
	if st.AbilityIndex == nil || st.Card == "" {
		return nil
	}
	k := *st.AbilityIndex
	c, ok := reg.Lookup(refCardName(st.Card))
	if ok && len(c.Faces) > 1 {
		if f := forgeSnapshotAbilityFace(reg, it, st, g, i); f != nil {
			return f
		}
	}
	if ok && len(c.Faces) > 0 {
		face := 0
		if seat, _, hasSeat := strings.Cut(st.Card, ":"); hasSeat {
			for _, name := range it.Setup[seat].BackFace {
				if name == refCardName(st.Card) {
					face = 1
					break
				}
			}
		}
		if face < len(c.Faces) {
			if _, ok := abilityLine(c.Faces[face], k); ok {
				return c.Faces[face]
			}
		}
	}
	return forgeSnapshotAbilityFace(reg, it, st, g, i)
}

// forgeSnapshotAbilityFace resolves a step the setup face could not: it finds
// the permanent the step names in the snapshot entering that step and reads
// the ability off the face that is up there. Token refs resolve through the
// seat's setup stems, a printed card (a token copy), or the token scripts,
// narrowed by the snapshot's keywords and types when a name is shared.
func forgeSnapshotAbilityFace(reg *cards.Registry, it oraclegen.Item, st oraclegen.Step, g rules.OracleResult, i int) *cards.Face {
	var perm rules.OracleSnapPerm
	found := false
	if i < len(g.Snapshots) {
		for _, p := range g.Snapshots[i].Permanents {
			if p.Ref == st.Card {
				perm, found = p, true
				break
			}
		}
	}
	has := func(f *cards.Face) *cards.Face {
		if _, ok := abilityLine(f, *st.AbilityIndex); ok {
			return f
		}
		return nil
	}
	name := refCardName(st.Card)
	if tokenName, isToken := strings.CutPrefix(name, "token:"); isToken {
		c := forgeTokenCard(reg, it, st.Card, tokenName, perm, found)
		if c == nil || len(c.Faces) == 0 {
			return nil
		}
		face := c.Faces[0]
		for _, f := range c.Faces {
			if f.Name == tokenName {
				face = f
				break
			}
		}
		return has(face)
	}
	if !found {
		return nil
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return nil
	}
	for _, f := range c.Faces {
		if f.Name == perm.Name {
			return has(f)
		}
	}
	return nil
}

// forgeTokenCard finds the token script behind a "pN:token:Name" ref: the
// owning seat's setup stem first (exact), then the printed-name registry (a
// token copy of a real card), then the token scripts. Several scripts can
// share a printed name (Vehicle Token), so the snapshot's keywords, types and
// PT narrow them; a remaining tie is left unresolved rather than guessed.
func forgeTokenCard(reg *cards.Registry, it oraclegen.Item, ref, name string, perm rules.OracleSnapPerm, found bool) *cards.Card {
	if seat, _, hasSeat := strings.Cut(ref, ":"); hasSeat {
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
	cands = filterCards(cands, func(c *cards.Card) bool {
		return stringSetSubset(c.Faces[0].Keywords, perm.Keywords)
	})
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) == 0 {
		return nil
	}
	cands = filterCards(cands, func(c *cards.Card) bool {
		return stringSetEqual(c.Faces[0].Types, perm.Types)
	})
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) == 0 {
		return nil
	}
	cands = filterCards(cands, func(c *cards.Card) bool {
		return c.Faces[0].PT == perm.PT
	})
	if len(cands) == 1 {
		return cands[0]
	}
	return nil
}

// abilityLine is the script line of ability k on f, when k names one.
func abilityLine(f *cards.Face, k int) (string, bool) {
	if k >= 0 && k < len(f.Abilities) && f.Abilities[k] != nil {
		return f.Abilities[k].Line, true
	}
	return "", false
}

// stringSetSubset reports whether every member of a is a member of b.
func stringSetSubset(a, b []string) bool {
	in := make(map[string]bool, len(b))
	for _, s := range b {
		in[s] = true
	}
	for _, s := range a {
		if !in[s] {
			return false
		}
	}
	return true
}

// stringSetEqual reports whether a and b hold the same members.
func stringSetEqual(a, b []string) bool {
	return stringSetSubset(a, b) && stringSetSubset(b, a)
}

// filterCards returns the cards for which keep is true, in input order.
func filterCards(cs []*cards.Card, keep func(*cards.Card) bool) []*cards.Card {
	out := cs[:0]
	for _, c := range cs {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// refCardName is the printed name inside a card ref ("p0:Shock#2" is
// "Shock"). A bare name is returned as is.
func refCardName(ref string) string {
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		ref = ref[i+1:]
	}
	if j := strings.LastIndexByte(ref, '#'); j >= 0 {
		if _, err := strconv.Atoi(ref[j+1:]); err == nil {
			ref = ref[:j]
		}
	}
	return ref
}

// runForgeDiff is forge-diff, follower-diff -engine forge under its old name.
func runForgeDiff(args []string) error {
	return runFollowerDiff("forge-diff", "forge", args)
}

// runFollowerDiff runs the follower diff as command cmd. engine fixes the
// follower; "" reads it from -engine, which must name a registered follower
// that reads Forge requests.
func runFollowerDiff(cmd, engine string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	var eng *string
	if engine == "" {
		eng = fs.String("engine", "", "follower whose items are Forge requests (required)")
	}
	dir := fs.String("cards", ".cards", "corpus dir")
	req := fs.String("req", "", "sidecar request JSONL (from forge-export)")
	forge := fs.String("forge", "", "Forge driver output JSONL; rows missing from it are read from the cache")
	cache := fs.String("cache", "", "Forge result cache directory (default: $FORGE_ORACLE_DIR/cache/<oracle-ref12>-<driver12>)")
	ref := fs.String("oracle-ref", "", "FORGE_ORACLE_REF the rows came from (names the cache directory)")
	driver := fs.String("driver", "", "driver source sha (names the cache directory)")
	out := fs.String("out", "", "forge-diff JSONL (off-repo)")
	fs.Parse(args)
	if eng != nil {
		engine = *eng
		if f, ok := follower.Lookup(engine); !ok || !readsForgeRequests(f) {
			return fmt.Errorf("%s needs -engine naming a follower that reads Forge requests (%s)", cmd, strings.Join(forgeRequestFollowers(), "|"))
		}
	}
	if *req == "" || *out == "" {
		return fmt.Errorf("%s needs -req and -out", cmd)
	}
	cacheDir, err := forgeCacheDir(*cache, *ref, *driver)
	if err != nil {
		return err
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	counts, err := followerDiff(reg, engine, *req, *forge, cacheDir, *out)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-28s %d\n", k, counts[k])
	}
	return nil
}

// forgeRequestFollowers are the registered followers whose items are Forge
// requests, the only ones follower-diff can compare.
func forgeRequestFollowers() []string {
	var out []string
	for _, n := range follower.Names() {
		if f, _ := follower.Lookup(n); readsForgeRequests(f) {
			out = append(out, n)
		}
	}
	return out
}

// readsForgeRequests reports whether f's runner reads Forge requests: those
// forge-export writes, or follower-export's (a Forge request plus "neutral").
func readsForgeRequests(f follower.Follower) bool {
	return f.Prep == follower.PrepForgeExport || f.Prep == follower.PrepFollowerExport
}

// forgeCacheDir is the Forge result cache: an explicit -cache, else
// $FORGE_ORACLE_DIR/cache/<oracle ref[:12]>-<driver sha[:12]>. The name
// carries both pins because Forge's side of a request is a function of the
// request, FORGE_ORACLE_REF and the driver source.
func forgeCacheDir(explicit, ref, driver string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if ref == "" || driver == "" {
		return "", fmt.Errorf("forge-diff needs -cache, or -oracle-ref and -driver")
	}
	base := os.Getenv("FORGE_ORACLE_DIR")
	if base == "" {
		base = defaultForgeOracleDir
	}
	return filepath.Join(base, "cache", short12(ref)+"-"+short12(driver)), nil
}

func short12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// forgeDiff is followerDiff for Forge.
func forgeDiff(reg *cards.Registry, reqPath, forgePath, cacheDir, out string) (map[string]int, error) {
	return followerDiff(reg, "forge", reqPath, forgePath, cacheDir, out)
}

// followerDiff compares gorge with the follower engine for every request line
// and writes one ForgeRow per request. The follower's rows come from forgePath
// (stored in the cache under the request sha) or, when absent there, from the
// cache. The result is the status census, keyed like diff's.
func followerDiff(reg *cards.Registry, engine, reqPath, forgePath, cacheDir, out string) (map[string]int, error) {
	fl, ok := follower.Lookup(engine)
	if !ok {
		return nil, fmt.Errorf("unknown follower %q", engine)
	}
	if err := refuseRepoPath(out); err != nil {
		return nil, err
	}
	if forgePath == "" && cacheDir == "" {
		return nil, fmt.Errorf("forge-diff needs -forge or a cache")
	}
	cache := oraclediff.Cache{Dir: cacheDir}
	fres := map[string]oraclediff.XResult{}
	echoed := map[string]string{}
	if forgePath != "" {
		if err := readLines(forgePath, func(b []byte) error {
			var r struct {
				oraclediff.XResult
				Engine     string `json:"engine"`
				RequestSHA string `json:"request_sha"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return err
			}
			// Another follower's row fed in by mistake would compare as this
			// one's.
			if r.Engine != engine && (fl.StampsEngine || r.Engine != "") {
				return fmt.Errorf("row %q is not a %s row (engine %q)", r.ID, fl.Display, r.Engine)
			}
			fres[r.ID] = r.XResult
			echoed[r.ID] = r.RequestSHA
			return nil
		}); err != nil {
			return nil, err
		}
	}
	of, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer of.Close()
	w := bufio.NewWriter(of)
	counts := map[string]int{}
	if err := readLines(reqPath, func(b []byte) error {
		var rq forgeRequest
		if err := json.Unmarshal(b, &rq); err != nil {
			return err
		}
		it := rq.Item
		sha := gate.Hash(b)
		row := ForgeRow{ID: it.ID, Card: it.Card, Template: it.Template, RequestSHA: sha, ScenarioSHA: rq.ScenarioSHA}
		x, ok := fres[it.ID]
		missMsg := "no " + fl.Display + " result"
		if echo := echoed[it.ID]; ok && echo != "" && echo != sha {
			// The row answers another version of this request (an older
			// export): caching it under this sha would poison the cache.
			ok = false
			missMsg = fmt.Sprintf("stale %s row: it echoes request_sha %s, this request is %s", fl.Display, short12(echo), short12(sha))
		}
		if ok && cacheDir != "" {
			if err := cache.Put(sha, x); err != nil {
				return err
			}
		} else if !ok {
			x, ok = cache.Get(sha)
		}
		if !ok {
			row.Verdict = oraclediff.Verdict{Status: oraclediff.Harness, Engine: engine, Msg: missMsg}
		} else {
			g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
			row.Verdict = oraclediff.CompareOpts(g, gerr, x, it.Compare, forgeIgnore(it)...)
			if row.Verdict.Engine == "xmage" {
				// CompareOpts names the follower "xmage" whichever it is.
				row.Verdict.Engine = engine
			}
			row.ForgeMS = x.MS
		}
		key := string(row.Verdict.Status)
		if row.Verdict.Status == oraclediff.Diverge {
			key += ":" + row.Verdict.Field
		}
		counts[key]++
		rb, err := json.Marshal(row)
		if err != nil {
			return err
		}
		w.Write(rb)
		return w.WriteByte('\n')
	}); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	return counts, of.Close()
}

// forgeIgnore is the item's own ignore list plus library_top when a seat's
// setup names library cards. Gorge's deal shuffles the library the named
// cards were mixed into (measured: Library ["Forest","Plains","Shock"] leaves
// Plains fifth from the top and Forest outside the window), while the Forge
// driver puts them under the filler, so the order cannot agree. A seat that
// names only library_top is a deterministic top (named cards, then filler),
// so it stays compared.
func forgeIgnore(it oraclegen.Item) []string {
	out := append([]string(nil), it.Ignore...)
	for _, seat := range it.Setup {
		if len(seat.Library) > 0 {
			return append(out, "library_top")
		}
	}
	return out
}

// refuseRepoPath rejects an output path inside the repository (the nearest
// ancestor of the working directory holding go.mod), except under the gitignored
// .oracle-build/. Forge output and the request's script text are never committed;
// a Forge verdict row is written by forge-verdicts, never by this pass's output.
func refuseRepoPath(path string) error {
	root := repoRoot()
	if root == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	abs = resolveExisting(abs)
	root = resolveExisting(root)
	if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if strings.HasPrefix(rel, ".oracle-build"+string(filepath.Separator)) {
			return nil
		}
		return fmt.Errorf("refusing to write %s: it is inside the repository %s (Forge output and request files go outside it, or under the gitignored .oracle-build/)", path, root)
	}
	return nil
}

func repoRoot() string {
	d, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

// resolveExisting resolves symlinks in the longest existing prefix of p, so
// a not-yet-created output file is judged by where its directory really is.
func resolveExisting(p string) string {
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}
