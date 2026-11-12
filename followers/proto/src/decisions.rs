//! The decision queue: step-scoped answers from a request's `gorge_decisions`.
//!
//! A follower answers every engine prompt from gorge's decision log so both engines answer
//! alike. The queue holds the decisions in log order (setup decisions have `step: -1`) and hands
//! them out per step. It only *reports* misses: a prompt no decision answers is a strict miss
//! (`row::strict_miss_msg`), and the driver decides what to do about it. Decisions never consumed
//! are reported by [`DecisionQueue::leftover`].
//!
//! Nothing here is engine-specific and nothing iterates an unordered map.

use crate::refs::Ref;
use crate::request::Decision;
use crate::row;

/// Pick kinds that make a decision a yes/no question.
const YES_NO_KINDS: [&str; 5] = ["yes", "no", "opening_yes", "trigger_cost_pay", "decline"];

/// The queue. See the module docs.
#[derive(Debug, Clone, Default)]
pub struct DecisionQueue {
    decisions: Vec<Decision>,
    /// Parallel to `decisions`: the decision was taken whole.
    done: Vec<bool>,
    /// Parallel to `decisions`: per `pick_refs` entry, taken by a target prompt.
    ref_done: Vec<Vec<bool>>,
    lookahead: bool,
}

impl DecisionQueue {
    pub fn new(decisions: Vec<Decision>) -> DecisionQueue {
        let done = vec![false; decisions.len()];
        let ref_done = decisions.iter().map(|d| vec![false; d.pick_refs.len()]).collect();
        DecisionQueue { decisions, done, ref_done, lookahead: false }
    }

    /// Set right after a `pass_to` step reached its named step: `take` then also considers
    /// decisions of `step + 1`. The step machine clears it again when it moves on.
    pub fn set_lookahead(&mut self, on: bool) {
        self.lookahead = on;
    }

    fn in_scope(&self, d: &Decision, step: i32) -> bool {
        d.step == step || (self.lookahead && d.step == step + 1)
    }

    /// The first unconsumed decision of `step` (or `step + 1` under lookahead) matching `pred`;
    /// it is marked consumed.
    pub fn take(&mut self, step: i32, pred: impl Fn(&Decision) -> bool) -> Option<Decision> {
        let i = (0..self.decisions.len()).find(|&i| {
            !self.done[i] && self.in_scope(&self.decisions[i], step) && pred(&self.decisions[i])
        })?;
        self.done[i] = true;
        Some(self.decisions[i].clone())
    }

    /// Targets are taken by target spec, not log order: from this step's unconsumed target
    /// decisions in log order, take the refs `can_target` accepts, up to `max`. Consumption is per
    /// ref; a decision is consumed once all its refs are. An explicit empty answer (a target
    /// decision with no picks) reached before any ref was taken means "no target": it is consumed
    /// and nothing is returned. Unparseable refs are never taken.
    pub fn take_target_refs(
        &mut self,
        step: i32,
        max: usize,
        can_target: impl Fn(&Ref) -> bool,
    ) -> Vec<Ref> {
        let mut out: Vec<Ref> = Vec::new();
        for i in 0..self.decisions.len() {
            if out.len() >= max {
                break;
            }
            let d = &self.decisions[i];
            if self.done[i] || d.kind != "target" || !self.in_scope(d, step) {
                continue;
            }
            if d.picks.is_empty() && d.pick_refs.is_empty() {
                if out.is_empty() {
                    self.done[i] = true;
                }
                break;
            }
            for j in 0..d.pick_refs.len() {
                if out.len() >= max {
                    break;
                }
                if self.ref_done[i][j] {
                    continue;
                }
                if let Ok(r) = Ref::parse(&d.pick_refs[j]) {
                    if can_target(&r) {
                        self.ref_done[i][j] = true;
                        out.push(r);
                    }
                }
            }
            if !d.pick_refs.is_empty() && self.ref_done[i].iter().all(|&b| b) {
                self.done[i] = true;
            }
        }
        out
    }

    /// Every decision never consumed, except payment-window asks and look-acks, as
    /// `unconsumed gorge decision(s): step S pN KIND[PK1,PK2] [PICK1, PICK2]; ...` (cut to 300
    /// chars). `None` when nothing is left over.
    pub fn leftover(&self) -> Option<String> {
        let parts: Vec<String> = self
            .decisions
            .iter()
            .enumerate()
            .filter(|(i, d)| !self.done[*i] && !is_payment_window(d))
            .map(|(_, d)| {
                format!(
                    "step {} p{} {}[{}] [{}]",
                    d.step,
                    d.seat,
                    d.kind,
                    d.pick_kinds.join(","),
                    d.picks.join(", ")
                )
            })
            .collect();
        if parts.is_empty() {
            None
        } else {
            Some(row::leftover_msg(&parts.join("; ")))
        }
    }
}

/// Payment-window asks (`pay_*`, `activate`) and `look_ack` are not answers to engine prompts.
fn is_payment_window(d: &Decision) -> bool {
    d.resume == "look_ack"
        || d.pick_kinds.iter().any(|k| k.starts_with("pay_") || k == "activate")
}

fn is_yes_no_kind(k: &str) -> bool {
    YES_NO_KINDS.contains(&k)
}

/// `kind == "yesno"`, or a `choose_n` with exactly one yes/no-shaped pick, or any pick kind in
/// `yes no opening_yes trigger_cost_pay decline`. (A pick without a kind is yes/no-shaped when its
/// label is `yes`/`no`.)
pub fn is_yes_no(d: &Decision) -> bool {
    if d.kind == "yesno" || d.pick_kinds.iter().any(|k| is_yes_no_kind(k)) {
        return true;
    }
    d.kind == "choose_n"
        && d.picks.len() == 1
        && d.pick_kinds.is_empty()
        && matches!(d.picks[0].to_ascii_lowercase().as_str(), "yes" | "no")
}

/// The answer of a yes/no decision. An empty answer, a `no`/`decline` pick kind or a `no`/
/// `decline` label declines.
pub fn yes(d: &Decision) -> bool {
    let Some(first) = d.picks.first() else {
        return false;
    };
    if matches!(d.pick_kinds.first().map(String::as_str), Some("no" | "decline")) {
        return false;
    }
    !matches!(first.trim().to_ascii_lowercase().as_str(), "no" | "decline" | "false")
}

/// Parse a label of the form `X = n` (spaces optional, `X` either case).
fn x_label(s: &str) -> Option<i64> {
    let rest = s.trim().strip_prefix(['X', 'x'])?;
    rest.trim_start().strip_prefix('=')?.trim().parse().ok()
}

/// The X value: a pick label `X = n`, or the numeric label of a `choose_n` pick of kind `x`.
pub fn x_value(d: &Decision) -> Option<i64> {
    if let Some(n) = d.picks.iter().find_map(|p| x_label(p)) {
        return Some(n);
    }
    if d.kind != "choose_n" {
        return None;
    }
    d.picks
        .iter()
        .zip(&d.pick_kinds)
        .find(|(_, k)| k.as_str() == "x")
        .and_then(|(p, _)| p.trim().parse().ok())
}

/// `kind == "mode"`; the picks are mode labels.
pub fn is_mode(d: &Decision) -> bool {
    d.kind == "mode"
}

/// `kind == "order"` with every pick kind `trigger`.
pub fn is_trigger_order(d: &Decision) -> bool {
    d.kind == "order" && !d.pick_kinds.is_empty() && d.pick_kinds.iter().all(|k| k == "trigger")
}

/// The trigger labels in the order they go on the stack: `picks[0]` FIRST, so it resolves LAST.
pub fn trigger_order_stack_first(d: &Decision) -> Vec<String> {
    d.picks.clone()
}

/// Scry, surveil, dig: the picks stay on top in pick order, the rest go to the pick kind's
/// destination.
pub fn is_arrange(d: &Decision) -> bool {
    d.gorge_kind == "arrange"
}

/// A divided-damage split: a `choose_n` with `resume == "damage_split"`, whose picks repeat a
/// target once per point.
pub fn is_damage_split(d: &Decision) -> bool {
    d.kind == "choose_n" && d.resume == "damage_split"
}

/// A `choose_n` whose `object_picks` or `pick_refs` name objects (search, discard, sacrifice,
/// choose a card). Damage splits are not object choices.
pub fn is_object_choice(d: &Decision) -> bool {
    d.kind == "choose_n"
        && !is_damage_split(d)
        && (!d.object_picks.is_empty() || !d.pick_refs.is_empty())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn dec(step: i32, kind: &str) -> Decision {
        Decision { step, kind: kind.into(), ..Decision::default() }
    }

    fn target(step: i32, refs: &[&str]) -> Decision {
        Decision {
            pick_refs: refs.iter().map(|s| s.to_string()).collect(),
            picks: refs.iter().map(|s| s.to_string()).collect(),
            ..dec(step, "target")
        }
    }

    fn pick(step: i32, kind: &str, pk: &str, label: &str) -> Decision {
        Decision {
            picks: vec![label.into()],
            pick_kinds: vec![pk.into()],
            ..dec(step, kind)
        }
    }

    #[test]
    fn decisions_take_is_step_scoped() {
        let mut q = DecisionQueue::new(vec![dec(-1, "yesno"), dec(1, "mode"), dec(0, "mode"), dec(0, "yesno")]);
        assert!(q.take(2, |_| true).is_none());
        assert_eq!(q.take(0, is_yes_no).unwrap().step, 0);
        assert!(q.take(0, is_yes_no).is_none(), "consumed");
        assert_eq!(q.take(-1, |_| true).unwrap().kind, "yesno");
        // the predicate decides, log order breaks ties
        assert_eq!(q.take(0, |d| d.kind == "mode").unwrap().step, 0);
        assert_eq!(q.take(1, |_| true).unwrap().step, 1);
    }

    #[test]
    fn decisions_lookahead_after_pass_to() {
        let mut q = DecisionQueue::new(vec![dec(3, "mode"), dec(2, "yesno")]);
        assert!(q.take(2, is_mode).is_none());
        q.set_lookahead(true);
        assert_eq!(q.take(2, is_mode).unwrap().step, 3);
        assert!(q.take(0, |_| true).is_none(), "only step + 1, never further");
        q.set_lookahead(false);
        assert_eq!(q.take(2, |_| true).unwrap().kind, "yesno");
    }

    #[test]
    fn decisions_targets_by_spec() {
        let mut q = DecisionQueue::new(vec![
            target(0, &["p1", "p0:Bear"]),
            target(0, &["p1:Wolf"]),
            target(1, &["p1:Elk"]),
        ]);
        // A creature-only prompt skips the player and takes the first legal ref.
        let creature = |r: &Ref| matches!(r, Ref::Card { .. });
        assert_eq!(q.take_target_refs(0, 1, creature), vec![Ref::parse("p0:Bear").unwrap()]);
        // The player is still unconsumed; an any-target prompt takes up to max in log order.
        let any = |_: &Ref| true;
        let got = q.take_target_refs(0, 5, any);
        assert_eq!(got, vec![Ref::parse("p1").unwrap(), Ref::parse("p1:Wolf").unwrap()]);
        assert!(q.take_target_refs(0, 5, any).is_empty());
        assert!(q.leftover().unwrap().contains("step 1 p0 target"));
        // A partially consumed decision is still left over.
        let mut q = DecisionQueue::new(vec![target(0, &["p1", "p0:Bear"])]);
        q.take_target_refs(0, 1, creature);
        assert!(q.leftover().unwrap().contains("step 0 p0 target"));
        q.take_target_refs(0, 1, any);
        assert_eq!(q.leftover(), None);
        // An explicit empty answer means no target.
        let mut q = DecisionQueue::new(vec![dec(0, "target"), target(0, &["p1"])]);
        assert!(q.take_target_refs(0, 1, any).is_empty());
        assert_eq!(q.take_target_refs(0, 1, any), vec![Ref::Player(1)]);
        assert_eq!(q.leftover(), None);
    }

    #[test]
    fn decisions_yes_no_shapes() {
        assert!(is_yes_no(&pick(0, "yesno", "", "yes")));
        assert!(is_yes_no(&pick(0, "choose_n", "yes", "Yes")));
        assert!(is_yes_no(&pick(0, "choose_n", "trigger_cost_pay", "Pay")));
        assert!(is_yes_no(&pick(0, "choose_n", "opening_yes", "Keep")));
        assert!(!is_yes_no(&pick(0, "choose_n", "card", "Bear")));
        assert!(!is_yes_no(&dec(0, "mode")));
        assert!(yes(&pick(0, "yesno", "yes", "yes")));
        assert!(!yes(&pick(0, "yesno", "no", "no")));
        assert!(!yes(&pick(0, "choose_n", "decline", "Decline")));
        assert!(!yes(&dec(0, "yesno")), "an empty answer declines");
    }

    #[test]
    fn decisions_x_value_from_label() {
        assert_eq!(x_value(&pick(0, "choose_n", "x", "X = 3")), Some(3));
        assert_eq!(x_value(&pick(0, "choose_n", "x", "5")), Some(5));
        assert_eq!(x_value(&pick(0, "mode", "mode", "x=2")), Some(2));
        assert_eq!(x_value(&pick(0, "choose_n", "card", "Bear")), None);
        assert_eq!(x_value(&pick(0, "mode", "x", "5")), None);
    }

    #[test]
    fn decisions_trigger_order_stack_first() {
        let d = Decision {
            picks: vec!["A".into(), "B".into()],
            pick_kinds: vec!["trigger".into(), "trigger".into()],
            ..dec(1, "order")
        };
        assert!(is_trigger_order(&d));
        assert_eq!(trigger_order_stack_first(&d), vec!["A", "B"]);
        let mixed = Decision { pick_kinds: vec!["trigger".into(), "card".into()], ..d };
        assert!(!is_trigger_order(&mixed));
        assert!(!is_trigger_order(&dec(1, "order")));
    }

    #[test]
    fn decisions_leftover_skips_payment_window() {
        let ack = Decision { resume: "look_ack".into(), ..dec(0, "choose_n") };
        let mut q = DecisionQueue::new(vec![
            pick(0, "choose_n", "pay_G", "G"),
            pick(0, "choose_n", "pay_generic", "1"),
            pick(0, "choose_n", "pay_life", "2"),
            pick(0, "choose_n", "activate", "Tap"),
            ack,
        ]);
        assert_eq!(q.leftover(), None);
        q.take(0, |_| true);
        assert_eq!(q.leftover(), None);
    }

    #[test]
    fn decisions_leftover_format() {
        let mut q = DecisionQueue::new(vec![
            Decision { seat: 1, ..pick(-1, "choose_n", "card", "Bear") },
            Decision {
                picks: vec!["A".into(), "B".into()],
                pick_kinds: vec!["trigger".into(), "trigger".into()],
                ..dec(2, "order")
            },
            dec(3, "yesno"),
        ]);
        q.take(3, |_| true);
        assert_eq!(
            q.leftover().unwrap(),
            "unconsumed gorge decision(s): step -1 p1 choose_n[card] [Bear]; step 2 p0 order[trigger,trigger] [A, B]"
        );
        let long: Vec<Decision> = (0..40).map(|i| pick(i, "choose_n", "card", "Some Long Card Name")).collect();
        assert_eq!(DecisionQueue::new(long).leftover().unwrap().chars().count(), 300);
    }

    #[test]
    fn decisions_shape_helpers() {
        let arrange = Decision { gorge_kind: "arrange".into(), ..dec(0, "choose_n") };
        assert!(is_arrange(&arrange));
        let split = Decision { resume: "damage_split".into(), picks: vec!["p1".into(), "p1".into()], pick_refs: vec!["p1".into(), "p1".into()], ..dec(0, "choose_n") };
        assert!(is_damage_split(&split) && !is_object_choice(&split));
        let obj = Decision { pick_refs: vec!["p0:Bear".into()], ..dec(0, "choose_n") };
        assert!(is_object_choice(&obj));
        assert!(is_object_choice(&Decision { object_picks: vec!["Bear".into()], ..dec(0, "choose_n") }));
        assert!(!is_object_choice(&dec(0, "choose_n")));
        assert!(is_mode(&dec(0, "mode")));
    }
}
