//! Which `WaitingFor` the seat can pose as a v2 decision, and which it cannot.
//!
//! [`classify`] is one `match` arm per `WaitingFor` variant (147 at the pin) and has no `_` arm: a
//! variant phase adds at a pin bump fails to compile here until it is classified, instead of falling
//! into a default. An [`Pose::Unposed`] variant is never answered by a heuristic. The game halts with
//! `engine_contract_failure:unmapped_decision`, the variant name goes to stderr and into the
//! per-variant census (`game::Census`), and the run counts it as an `engine_error`.
//!
//! A posed pool prompt must not carry a whole-set rule the pool machinery cannot enforce: phase
//! validates the completed `SelectCards` set, so an incremental picker could submit a set phase
//! rejects (`submit_rejected`). [`unenforceable_pool_rule`] is the one gate for that class; a
//! constrained prompt is `Unposed` (a named halt) rather than passed to `game::build`.

use engine::types::ability::SearchSelectionConstraint;
use engine::types::game_state::WaitingFor;

/// The whole-set rules `game::select_group` cannot enforce today. A prompt carrying one is
/// `Unposed`, never posed with a candidate phase would reject. Every constraint-bearing pool
/// variant belongs here: `SearchSelectionConstraint` and `ChooseFromZoneConstraint` are checked on
/// the completed set, not on each incremental pick. `game::pool_spec` re-checks this gate, so a
/// caller that bypasses `classify` still cannot submit a rejected set.
pub(crate) fn unenforceable_pool_rule(w: &WaitingFor) -> Option<&'static str> {
    match w {
        WaitingFor::SearchChoice { constraint, .. } if !matches!(constraint, SearchSelectionConstraint::None) => Some("SearchChoice"),
        WaitingFor::ChooseFromZoneChoice { constraint: Some(_), .. } => Some("ChooseFromZoneChoice"),
        _ => None,
    }
}

/// How a posed prompt becomes candidates (one family per `game` mapper).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Family {
    /// `Priority`: pass, play a land, cast a spell, activate an ability.
    Priority,
    /// `MulliganDecision`: keep or mulligan.
    Mulligan,
    /// `DeclareAttackers`: one substep per creature that may attack.
    DeclareAttackers,
    /// `DeclareBlockers`: one substep per creature that may block.
    DeclareBlockers,
    /// `TargetSelection`, `TriggerTargetSelection`: a target per slot.
    Targets,
    /// `OptionalEffectChoice`, `OpponentMayChoice`: accept or decline a "may".
    OptionalEffect,
    /// `OptionalCostChoice`: pay an optional cost or not.
    OptionalCost,
    /// `ChooseXValue`: the value of X.
    ChooseX,
    /// A pick of `min..=max` cards from a pool, one `select_object` per pick: `DiscardToHandSize`,
    /// `DiscardChoice`, `RevealChoice`, `SearchChoice`, `EffectZoneChoice`, `DigChoice`, the simple
    /// `PayCost` kinds, `ChooseLegend`, `ChooseFromZoneChoice`, `MultiTargetSelection`, the one-pick
    /// `BeholdChoice`, `EmpowerJaceChoice` and `SpellCopyOrderChoice`, and the ward/bounce choices.
    Pool,
    /// `ModeChoice`: `choose_spell_mode` picks.
    Modes,
    /// `OrderTriggers`: `order_pick` triggers.
    OrderTriggers,
    /// `ScryChoice`, `SurveilChoice`: `arrange_card` per looked-at card, then `order_pick` arrangement.
    Arrange,
    /// `TopOrBottomChoice`: `choose_option` top or bottom.
    TopOrBottom,
    /// `EquipTarget`: `select_object` `attach` per legal creature.
    Attach,
    /// `ChooseManaColor`: `choose_color` (`mana`) per colour, `choose_option` for a combination.
    ManaColor,
    /// `UnlessPayment`: `optional_cost` `unless_payment` pay or decline.
    Unless,
    /// `ChooseOneOfBranch`: `choose_option` per resolution-time branch.
    Branch,
    /// `ReplacementChoice`: `choose_replacement` per candidate replacement effect.
    Replacement,
    /// `AssignCombatDamage`: the host divides it (`combat_damage_assignment: "engine_order"`): lethal to
    /// each blocker in phase's order, the rest to the last blocker (or to the defender with trample). No
    /// decision is posed.
    CombatDamage,
    /// `ManaPayment`: finish a payment the pool covers, else `activate_mana_ability` per source or cancel.
    ManaPayment,
    /// `ManaSourceSelection`: `activate_mana_ability` per source, or back to the payment.
    ManaSource,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Pose {
    /// A seat decision of this family.
    Posed(Family),
    /// The game is over.
    Terminal,
    /// No mapping yet: an `engine_error`.
    Unposed,
}

/// The variant's name and how it is posed.
pub fn classify(w: &WaitingFor) -> (&'static str, Pose) {
    if let Some(name) = unenforceable_pool_rule(w) {
        return (name, Pose::Unposed);
    }
    match w {
        WaitingFor::Priority { .. } => ("Priority", Pose::Posed(Family::Priority)),
        WaitingFor::ResolveAllConsent { .. } => ("ResolveAllConsent", Pose::Unposed),
        WaitingFor::ResolveAllReady { .. } => ("ResolveAllReady", Pose::Unposed),
        WaitingFor::MeldPairChoice { .. } => ("MeldPairChoice", Pose::Unposed),
        WaitingFor::MeldAttackTargetChoice { .. } => ("MeldAttackTargetChoice", Pose::Unposed),
        WaitingFor::EntryAttackTargetChoice { .. } => ("EntryAttackTargetChoice", Pose::Unposed),
        WaitingFor::MulliganDecision { .. } => ("MulliganDecision", Pose::Posed(Family::Mulligan)),
        // Tiny Leaders' forced first mulligan only (TL:R 906.6, phase types/game_state.rs at the pin).
        // The London bottom of a two-player game arrives as `MulliganDecision` with phase `BottomCards`.
        WaitingFor::OpeningHandBottomCards { .. } => ("OpeningHandBottomCards", Pose::Unposed),
        WaitingFor::ManaPayment { .. } => ("ManaPayment", Pose::Posed(Family::ManaPayment)),
        WaitingFor::ManaSourceSelection { .. } => ("ManaSourceSelection", Pose::Posed(Family::ManaSource)),
        WaitingFor::AssistChoosePlayer { .. } => ("AssistChoosePlayer", Pose::Unposed),
        WaitingFor::AssistPayment { .. } => ("AssistPayment", Pose::Unposed),
        WaitingFor::ChooseXValue { .. } => ("ChooseXValue", Pose::Posed(Family::ChooseX)),
        WaitingFor::TargetSelection { .. } => ("TargetSelection", Pose::Posed(Family::Targets)),
        WaitingFor::DeclareAttackers { .. } => ("DeclareAttackers", Pose::Posed(Family::DeclareAttackers)),
        WaitingFor::DeclareBlockers { .. } => ("DeclareBlockers", Pose::Posed(Family::DeclareBlockers)),
        WaitingFor::UntapChoice { .. } => ("UntapChoice", Pose::Unposed),
        WaitingFor::ChooseUntapSubset { .. } => ("ChooseUntapSubset", Pose::Unposed),
        WaitingFor::ExertChoice { .. } => ("ExertChoice", Pose::Unposed),
        WaitingFor::EnlistChoice { .. } => ("EnlistChoice", Pose::Unposed),
        WaitingFor::GameOver { .. } => ("GameOver", Pose::Terminal),
        WaitingFor::ReplacementChoice { .. } => ("ReplacementChoice", Pose::Posed(Family::Replacement)),
        WaitingFor::EntryControllerChoice { .. } => ("EntryControllerChoice", Pose::Unposed),
        WaitingFor::OrderTriggers { .. } => ("OrderTriggers", Pose::Posed(Family::OrderTriggers)),
        WaitingFor::CopyTargetChoice { .. } => ("CopyTargetChoice", Pose::Unposed),
        WaitingFor::ExploreChoice { .. } => ("ExploreChoice", Pose::Unposed),
        WaitingFor::ReturnAsAuraTarget { .. } => ("ReturnAsAuraTarget", Pose::Unposed),
        WaitingFor::ChooseTokenHost { .. } => ("ChooseTokenHost", Pose::Unposed),
        WaitingFor::EquipTarget { .. } => ("EquipTarget", Pose::Posed(Family::Attach)),
        WaitingFor::CrewVehicle { .. } => ("CrewVehicle", Pose::Unposed),
        WaitingFor::StationTarget { .. } => ("StationTarget", Pose::Unposed),
        WaitingFor::SaddleMount { .. } => ("SaddleMount", Pose::Unposed),
        WaitingFor::ScryChoice { .. } => ("ScryChoice", Pose::Posed(Family::Arrange)),
        WaitingFor::RippleRevealChoice { .. } => ("RippleRevealChoice", Pose::Unposed),
        WaitingFor::RippleBottomOrder { .. } => ("RippleBottomOrder", Pose::Unposed),
        WaitingFor::RevealUntilBottomOrder { .. } => ("RevealUntilBottomOrder", Pose::Unposed),
        WaitingFor::DigBottomOrder { .. } => ("DigBottomOrder", Pose::Unposed),
        WaitingFor::ArrangePlanarDeckTopChoice { .. } => ("ArrangePlanarDeckTopChoice", Pose::Unposed),
        WaitingFor::RedistributeLifeTotals { .. } => ("RedistributeLifeTotals", Pose::Unposed),
        WaitingFor::CoinFlipKeepChoice { .. } => ("CoinFlipKeepChoice", Pose::Unposed),
        WaitingFor::DieKeepChoice { .. } => ("DieKeepChoice", Pose::Unposed),
        WaitingFor::DigChoice { .. } => ("DigChoice", Pose::Posed(Family::Pool)),
        WaitingFor::DigRestSplitChoice { .. } => ("DigRestSplitChoice", Pose::Unposed),
        WaitingFor::SurveilChoice { .. } => ("SurveilChoice", Pose::Posed(Family::Arrange)),
        WaitingFor::RevealChoice { .. } => ("RevealChoice", Pose::Posed(Family::Pool)),
        WaitingFor::SearchChoice { .. } => ("SearchChoice", Pose::Posed(Family::Pool)),
        WaitingFor::SearchPartitionChoice { .. } => ("SearchPartitionChoice", Pose::Unposed),
        WaitingFor::OutsideGameChoice { .. } => ("OutsideGameChoice", Pose::Unposed),
        WaitingFor::ChooseFromZoneChoice { .. } => ("ChooseFromZoneChoice", Pose::Posed(Family::Pool)),
        WaitingFor::BeholdChoice { .. } => ("BeholdChoice", Pose::Posed(Family::Pool)),
        WaitingFor::EmpowerJaceChoice { .. } => ("EmpowerJaceChoice", Pose::Posed(Family::Pool)),
        WaitingFor::SpellCopyOrderChoice { .. } => ("SpellCopyOrderChoice", Pose::Posed(Family::Pool)),
        WaitingFor::ChooseOneOfBranch { .. } => ("ChooseOneOfBranch", Pose::Posed(Family::Branch)),
        WaitingFor::ConniveDiscard { .. } => ("ConniveDiscard", Pose::Unposed),
        WaitingFor::DiscardChoice { .. } => ("DiscardChoice", Pose::Posed(Family::Pool)),
        WaitingFor::HeroEventChoice { .. } => ("HeroEventChoice", Pose::Unposed),
        WaitingFor::EffectZoneChoice { .. } => ("EffectZoneChoice", Pose::Posed(Family::Pool)),
        WaitingFor::DrawnThisTurnTopdeckChoice { .. } => ("DrawnThisTurnTopdeckChoice", Pose::Unposed),
        WaitingFor::LearnChoice { .. } => ("LearnChoice", Pose::Unposed),
        WaitingFor::ManifestDreadChoice { .. } => ("ManifestDreadChoice", Pose::Unposed),
        WaitingFor::TriggerTargetSelection { .. } => ("TriggerTargetSelection", Pose::Posed(Family::Targets)),
        WaitingFor::BetweenGamesSideboard { .. } => ("BetweenGamesSideboard", Pose::Unposed),
        WaitingFor::BetweenGamesChoosePlayDraw { .. } => ("BetweenGamesChoosePlayDraw", Pose::Unposed),
        WaitingFor::NamedChoice { .. } => ("NamedChoice", Pose::Unposed),
        WaitingFor::OpponentGuess { .. } => ("OpponentGuess", Pose::Unposed),
        WaitingFor::SpellbookDraft { .. } => ("SpellbookDraft", Pose::Unposed),
        WaitingFor::DamageSourceChoice { .. } => ("DamageSourceChoice", Pose::Unposed),
        WaitingFor::ModeChoice { .. } => ("ModeChoice", Pose::Posed(Family::Modes)),
        WaitingFor::DiscardToHandSize { .. } => ("DiscardToHandSize", Pose::Posed(Family::Pool)),
        WaitingFor::OptionalCostChoice { .. } => ("OptionalCostChoice", Pose::Posed(Family::OptionalCost)),
        WaitingFor::ChooseGiftRecipient { .. } => ("ChooseGiftRecipient", Pose::Unposed),
        WaitingFor::SpliceOffer { .. } => ("SpliceOffer", Pose::Unposed),
        WaitingFor::DefilerPayment { .. } => ("DefilerPayment", Pose::Unposed),
        WaitingFor::OrderCostReductions { .. } => ("OrderCostReductions", Pose::Unposed),
        WaitingFor::CastOffer { .. } => ("CastOffer", Pose::Unposed),
        WaitingFor::ModalFaceChoice { .. } => ("ModalFaceChoice", Pose::Unposed),
        WaitingFor::AlternativeCastChoice { .. } => ("AlternativeCastChoice", Pose::Unposed),
        WaitingFor::MutateMergeChoice { .. } => ("MutateMergeChoice", Pose::Unposed),
        WaitingFor::CipherEncodeChoice { .. } => ("CipherEncodeChoice", Pose::Unposed),
        WaitingFor::CastingVariantChoice { .. } => ("CastingVariantChoice", Pose::Unposed),
        WaitingFor::ChoosePermanentTypeSlot { .. } => ("ChoosePermanentTypeSlot", Pose::Unposed),
        WaitingFor::MultiTargetSelection { .. } => ("MultiTargetSelection", Pose::Posed(Family::Pool)),
        WaitingFor::AbilityModeChoice { .. } => ("AbilityModeChoice", Pose::Posed(Family::Modes)),
        WaitingFor::OptionalEffectChoice { .. } => ("OptionalEffectChoice", Pose::Posed(Family::OptionalEffect)),
        WaitingFor::ResolutionOptionalPaymentChoice { .. } => ("ResolutionOptionalPaymentChoice", Pose::Unposed),
        WaitingFor::PairChoice { .. } => ("PairChoice", Pose::Unposed),
        WaitingFor::TributeChoice { .. } => ("TributeChoice", Pose::Unposed),
        WaitingFor::MiracleReveal { .. } => ("MiracleReveal", Pose::Unposed),
        WaitingFor::OpponentMayChoice { .. } => ("OpponentMayChoice", Pose::Posed(Family::OptionalEffect)),
        WaitingFor::LoopShortcut { .. } => ("LoopShortcut", Pose::Unposed),
        WaitingFor::RespondToShortcut { .. } => ("RespondToShortcut", Pose::Unposed),
        WaitingFor::PrecastCopyShortcutOffer { .. } => ("PrecastCopyShortcutOffer", Pose::Unposed),
        WaitingFor::RespondToPrecastCopyShortcut { .. } => ("RespondToPrecastCopyShortcut", Pose::Unposed),
        WaitingFor::UnlessPayment { .. } => ("UnlessPayment", Pose::Posed(Family::Unless)),
        WaitingFor::UnlessPaymentChooseCost { .. } => ("UnlessPaymentChooseCost", Pose::Unposed),
        WaitingFor::WardDiscardChoice { .. } => ("WardDiscardChoice", Pose::Posed(Family::Pool)),
        WaitingFor::WardSacrificeChoice { .. } => ("WardSacrificeChoice", Pose::Posed(Family::Pool)),
        WaitingFor::UnlessBounceChoice { .. } => ("UnlessBounceChoice", Pose::Posed(Family::Pool)),
        WaitingFor::ChooseRingBearer { .. } => ("ChooseRingBearer", Pose::Unposed),
        WaitingFor::ChooseRoomDoor { .. } => ("ChooseRoomDoor", Pose::Unposed),
        WaitingFor::ChooseDungeon { .. } => ("ChooseDungeon", Pose::Unposed),
        WaitingFor::ChooseDungeonRoom { .. } => ("ChooseDungeonRoom", Pose::Unposed),
        WaitingFor::SpecializeColor { .. } => ("SpecializeColor", Pose::Unposed),
        WaitingFor::PayCost { .. } => ("PayCost", Pose::Posed(Family::Pool)),
        WaitingFor::ActivationCostOneOfChoice { .. } => ("ActivationCostOneOfChoice", Pose::Unposed),
        WaitingFor::CostTypeChoice { .. } => ("CostTypeChoice", Pose::Unposed),
        WaitingFor::BlightChoice { .. } => ("BlightChoice", Pose::Unposed),
        WaitingFor::PayManaAbilityMana { .. } => ("PayManaAbilityMana", Pose::Unposed),
        WaitingFor::ManaAbilityManaPayment { .. } => ("ManaAbilityManaPayment", Pose::Unposed),
        WaitingFor::ChooseManaColor { .. } => ("ChooseManaColor", Pose::Posed(Family::ManaColor)),
        WaitingFor::CollectEvidenceChoice { .. } => ("CollectEvidenceChoice", Pose::Unposed),
        WaitingFor::HarmonizeTapChoice { .. } => ("HarmonizeTapChoice", Pose::Unposed),
        WaitingFor::RevealUntilKeptChoice { .. } => ("RevealUntilKeptChoice", Pose::Unposed),
        WaitingFor::RepeatDecision { .. } => ("RepeatDecision", Pose::Unposed),
        WaitingFor::TopOrBottomChoice { .. } => ("TopOrBottomChoice", Pose::Posed(Family::TopOrBottom)),
        WaitingFor::PopulateChoice { .. } => ("PopulateChoice", Pose::Unposed),
        WaitingFor::ClashChooseOpponent { .. } => ("ClashChooseOpponent", Pose::Unposed),
        WaitingFor::ChooseFromZoneOpponentChooser { .. } => ("ChooseFromZoneOpponentChooser", Pose::Unposed),
        WaitingFor::ChooseAnnouncingOpponent { .. } => ("ChooseAnnouncingOpponent", Pose::Unposed),
        WaitingFor::ClashCardPlacement { .. } => ("ClashCardPlacement", Pose::Unposed),
        WaitingFor::PhysicalContest { .. } => ("PhysicalContest", Pose::Unposed),
        WaitingFor::VoteChoice { .. } => ("VoteChoice", Pose::Unposed),
        WaitingFor::SeparatePilesChooseOpponent { .. } => ("SeparatePilesChooseOpponent", Pose::Unposed),
        WaitingFor::SeparatePilesPartition { .. } => ("SeparatePilesPartition", Pose::Unposed),
        WaitingFor::SeparatePilesChoice { .. } => ("SeparatePilesChoice", Pose::Unposed),
        WaitingFor::SecretAgendaChoice { .. } => ("SecretAgendaChoice", Pose::Unposed),
        WaitingFor::CompanionReveal { .. } => ("CompanionReveal", Pose::Unposed),
        WaitingFor::ChooseLegend { .. } => ("ChooseLegend", Pose::Posed(Family::Pool)),
        WaitingFor::CommanderZoneChoice { .. } => ("CommanderZoneChoice", Pose::Unposed),
        WaitingFor::BattleProtectorChoice { .. } => ("BattleProtectorChoice", Pose::Unposed),
        WaitingFor::ProliferateChoice { .. } => ("ProliferateChoice", Pose::Unposed),
        WaitingFor::TimeTravelChoice { .. } => ("TimeTravelChoice", Pose::Unposed),
        WaitingFor::ChooseObjectsSelection { .. } => ("ChooseObjectsSelection", Pose::Unposed),
        WaitingFor::CategoryChoice { .. } => ("CategoryChoice", Pose::Unposed),
        WaitingFor::EachPlayerCopyChosenSelection { .. } => ("EachPlayerCopyChosenSelection", Pose::Unposed),
        WaitingFor::KeepWithinTotalPowerChoice { .. } => ("KeepWithinTotalPowerChoice", Pose::Unposed),
        WaitingFor::KeepExactPermanentsChoice { .. } => ("KeepExactPermanentsChoice", Pose::Unposed),
        WaitingFor::CopyRetarget { .. } => ("CopyRetarget", Pose::Unposed),
        WaitingFor::AssignCombatDamage { .. } => ("AssignCombatDamage", Pose::Posed(Family::CombatDamage)),
        WaitingFor::AssignBlockerDamage { .. } => ("AssignBlockerDamage", Pose::Unposed),
        WaitingFor::DistributeAmong { .. } => ("DistributeAmong", Pose::Unposed),
        WaitingFor::MoveCountersDistribution { .. } => ("MoveCountersDistribution", Pose::Unposed),
        WaitingFor::RemoveCountersChoice { .. } => ("RemoveCountersChoice", Pose::Unposed),
        WaitingFor::PayAmountChoice { .. } => ("PayAmountChoice", Pose::Unposed),
        WaitingFor::RetargetChoice { .. } => ("RetargetChoice", Pose::Unposed),
        WaitingFor::CombatTaxPayment { .. } => ("CombatTaxPayment", Pose::Unposed),
        WaitingFor::PhyrexianPayment { .. } => ("PhyrexianPayment", Pose::Unposed),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use engine::types::ability::{ChooseFromZoneConstraint, Comparator, Effect, ResolvedAbility};
    use engine::types::card_type::CoreType;
    use engine::types::identifiers::ObjectId;
    use engine::types::player::PlayerId;

    /// The arms above are the whole enum: the compiler holds the match exhaustive, and this holds the
    /// count and the no-`_` rule.
    #[test]
    fn one_arm_per_variant_and_no_wildcard() {
        let src = include_str!("waiting.rs");
        let body = src.split("pub fn classify").nth(1).unwrap().split("#[cfg(test)]").next().unwrap();
        let arms = body.lines().filter(|l| l.trim_start().starts_with("WaitingFor::")).count();
        assert_eq!(arms, 147, "WaitingFor has 147 variants at the pin");
        assert!(!body.lines().any(|l| l.trim_start().starts_with("_ =>")), "no wildcard arm");
    }

    #[test]
    fn the_posed_set_is_the_documented_one() {
        let src = include_str!("waiting.rs");
        let body = src.split("pub fn classify").nth(1).unwrap().split("#[cfg(test)]").next().unwrap();
        let posed = body.lines().filter(|l| l.trim_start().starts_with("WaitingFor::") && l.contains("Pose::Posed(")).count();
        assert_eq!(posed, 40);
        let terminal = body.lines().filter(|l| l.trim_start().starts_with("WaitingFor::") && l.contains("Pose::Terminal")).count();
        assert_eq!(terminal, 1);
    }

    /// Every variant `classify` poses is named by an actual seat arm: `game::build` for a family arm,
    /// `game::pool_spec` for a pool. Without this, a deleted arm would fall into `build`'s catch-all
    /// and pose a `followup_mismatch` instead of failing at test time.
    #[test]
    fn every_posed_variant_has_a_seat_arm() {
        let waiting = include_str!("waiting.rs");
        let game = include_str!("game.rs");
        let classify = waiting.split("pub fn classify").nth(1).unwrap().split("#[cfg(test)]").next().unwrap();
        let build = game.split("fn build(").nth(1).unwrap().split("/// What a pool selection asks").next().unwrap();
        let pool = game.split("fn pool_spec(").nth(1).unwrap().split("/// A ref the observation lists").next().unwrap();
        let mut posed = 0;
        for line in classify.lines().filter(|l| l.trim_start().starts_with("WaitingFor::") && l.contains("Pose::Posed(")) {
            for part in line.split("WaitingFor::").skip(1) {
                let variant: String = part.chars().take_while(|c| c.is_alphanumeric() || *c == '_').collect();
                if variant.is_empty() {
                    continue;
                }
                posed += 1;
                let arm = format!("WaitingFor::{variant}");
                assert!(build.contains(&arm) || pool.contains(&arm), "{variant} is posed but has no build/pool_spec arm");
            }
            let family: String = line.split("Family::").nth(1).unwrap_or("").chars().take_while(|c| c.is_alphanumeric()).collect();
            if !family.is_empty() && family != "Pool" {
                assert!(build.contains(&format!("Family::{family}")), "posed family {family} has no build arm");
            }
        }
        assert_eq!(posed, 40, "the scan must see every posed variant");
    }

    /// A whole-set constraint is validated on the completed `SelectCards`, which the incremental
    /// pool machinery cannot enforce. `classify` must fail closed (Unposed) instead of offering a
    /// set phase rejects. Ward's aggregate form is excepted: `game::pool_spec` substitutes the
    /// engine's threshold witness, so every offered pick belongs to an accepted set.
    #[test]
    fn whole_set_constraints_are_posed_only_where_enforced() {
        let search = |constraint| WaitingFor::SearchChoice {
            player: PlayerId(0),
            library_owner: None,
            cards: vec![ObjectId(1)],
            count: 1,
            reveal: false,
            up_to: false,
            allows_partial_find: false,
            constraint,
            ordering_hint: Default::default(),
            split: None,
        };
        assert_eq!(
            classify(&search(SearchSelectionConstraint::None)),
            ("SearchChoice", Pose::Posed(Family::Pool)),
        );
        assert_eq!(
            classify(&search(SearchSelectionConstraint::TotalManaValue { comparator: Comparator::LE, value: 3 })),
            ("SearchChoice", Pose::Unposed),
            "a search constraint the incremental picker cannot enforce must fail closed",
        );

        let zone = |constraint| WaitingFor::ChooseFromZoneChoice {
            player: PlayerId(0),
            cards: vec![ObjectId(1)],
            count: 1,
            up_to: false,
            constraint,
            source_id: ObjectId(1),
            reciprocal_role: None,
        };
        assert_eq!(classify(&zone(None)), ("ChooseFromZoneChoice", Pose::Posed(Family::Pool)));
        assert_eq!(
            classify(&zone(Some(ChooseFromZoneConstraint::DistinctCardTypes { categories: vec![CoreType::Creature] }))),
            ("ChooseFromZoneChoice", Pose::Unposed),
            "a zone-choice constraint the incremental picker cannot enforce must fail closed",
        );

        let ward = |min_total_power| WaitingFor::WardSacrificeChoice {
            player: PlayerId(0),
            permanents: vec![ObjectId(1)],
            pending_effect: Box::new(ResolvedAbility::new(Effect::NoOp, vec![], ObjectId(1), PlayerId(0))),
            remaining: 1,
            min_total_power,
        };
        assert_eq!(classify(&ward(None)), ("WardSacrificeChoice", Pose::Posed(Family::Pool)));
        assert_eq!(
            classify(&ward(Some(2))),
            ("WardSacrificeChoice", Pose::Posed(Family::Pool)),
            "the aggregate form is posed through the threshold witness",
        );
    }
}
