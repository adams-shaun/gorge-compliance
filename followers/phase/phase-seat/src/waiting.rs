//! Which `WaitingFor` the seat can pose as a v2 decision, and which it cannot.
//!
//! [`classify`] is one `match` arm per `WaitingFor` variant (147 at the pin) and has no `_` arm: a
//! variant phase adds at a pin bump fails to compile here until it is classified, instead of falling
//! into a default. An [`Pose::Unposed`] variant is never answered by a heuristic. The game halts with
//! `engine_contract_failure:unmapped_decision`, the variant name goes to stderr and into the
//! per-variant census (`game::Census`), and the run counts it as an `engine_error`.

use engine::types::game_state::WaitingFor;

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
    /// `DiscardChoice`, `RevealChoice`, `SearchChoice`, `EffectZoneChoice`, the simple `PayCost` kinds
    /// and `ChooseLegend`.
    Pool,
    /// `ModeChoice`: `choose_spell_mode` picks.
    Modes,
    /// `OrderTriggers`: `order_pick` triggers.
    OrderTriggers,
    /// `ScryChoice`, `SurveilChoice`: `arrange_card` per looked-at card, then `order_pick` arrangement.
    Arrange,
    /// `TopOrBottomChoice`: `choose_option` top or bottom.
    TopOrBottom,
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
    match w {
        WaitingFor::Priority { .. } => ("Priority", Pose::Posed(Family::Priority)),
        WaitingFor::ResolveAllConsent { .. } => ("ResolveAllConsent", Pose::Unposed),
        WaitingFor::ResolveAllReady { .. } => ("ResolveAllReady", Pose::Unposed),
        WaitingFor::MeldPairChoice { .. } => ("MeldPairChoice", Pose::Unposed),
        WaitingFor::MeldAttackTargetChoice { .. } => ("MeldAttackTargetChoice", Pose::Unposed),
        WaitingFor::EntryAttackTargetChoice { .. } => ("EntryAttackTargetChoice", Pose::Unposed),
        WaitingFor::MulliganDecision { .. } => ("MulliganDecision", Pose::Posed(Family::Mulligan)),
        WaitingFor::OpeningHandBottomCards { .. } => ("OpeningHandBottomCards", Pose::Unposed),
        WaitingFor::ManaPayment { .. } => ("ManaPayment", Pose::Unposed),
        WaitingFor::ManaSourceSelection { .. } => ("ManaSourceSelection", Pose::Unposed),
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
        WaitingFor::ReplacementChoice { .. } => ("ReplacementChoice", Pose::Unposed),
        WaitingFor::EntryControllerChoice { .. } => ("EntryControllerChoice", Pose::Unposed),
        WaitingFor::OrderTriggers { .. } => ("OrderTriggers", Pose::Posed(Family::OrderTriggers)),
        WaitingFor::CopyTargetChoice { .. } => ("CopyTargetChoice", Pose::Unposed),
        WaitingFor::ExploreChoice { .. } => ("ExploreChoice", Pose::Unposed),
        WaitingFor::ReturnAsAuraTarget { .. } => ("ReturnAsAuraTarget", Pose::Unposed),
        WaitingFor::ChooseTokenHost { .. } => ("ChooseTokenHost", Pose::Unposed),
        WaitingFor::EquipTarget { .. } => ("EquipTarget", Pose::Unposed),
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
        WaitingFor::DigChoice { .. } => ("DigChoice", Pose::Unposed),
        WaitingFor::DigRestSplitChoice { .. } => ("DigRestSplitChoice", Pose::Unposed),
        WaitingFor::SurveilChoice { .. } => ("SurveilChoice", Pose::Posed(Family::Arrange)),
        WaitingFor::RevealChoice { .. } => ("RevealChoice", Pose::Posed(Family::Pool)),
        WaitingFor::SearchChoice { .. } => ("SearchChoice", Pose::Posed(Family::Pool)),
        WaitingFor::SearchPartitionChoice { .. } => ("SearchPartitionChoice", Pose::Unposed),
        WaitingFor::OutsideGameChoice { .. } => ("OutsideGameChoice", Pose::Unposed),
        WaitingFor::ChooseFromZoneChoice { .. } => ("ChooseFromZoneChoice", Pose::Unposed),
        WaitingFor::BeholdChoice { .. } => ("BeholdChoice", Pose::Unposed),
        WaitingFor::EmpowerJaceChoice { .. } => ("EmpowerJaceChoice", Pose::Unposed),
        WaitingFor::SpellCopyOrderChoice { .. } => ("SpellCopyOrderChoice", Pose::Unposed),
        WaitingFor::ChooseOneOfBranch { .. } => ("ChooseOneOfBranch", Pose::Unposed),
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
        WaitingFor::MultiTargetSelection { .. } => ("MultiTargetSelection", Pose::Unposed),
        WaitingFor::AbilityModeChoice { .. } => ("AbilityModeChoice", Pose::Unposed),
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
        WaitingFor::UnlessPayment { .. } => ("UnlessPayment", Pose::Unposed),
        WaitingFor::UnlessPaymentChooseCost { .. } => ("UnlessPaymentChooseCost", Pose::Unposed),
        WaitingFor::WardDiscardChoice { .. } => ("WardDiscardChoice", Pose::Unposed),
        WaitingFor::WardSacrificeChoice { .. } => ("WardSacrificeChoice", Pose::Unposed),
        WaitingFor::UnlessBounceChoice { .. } => ("UnlessBounceChoice", Pose::Unposed),
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
        WaitingFor::ChooseManaColor { .. } => ("ChooseManaColor", Pose::Unposed),
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
        WaitingFor::AssignCombatDamage { .. } => ("AssignCombatDamage", Pose::Unposed),
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
        assert_eq!(posed, 22);
        let terminal = body.lines().filter(|l| l.trim_start().starts_with("WaitingFor::") && l.contains("Pose::Terminal")).count();
        assert_eq!(terminal, 1);
    }
}
