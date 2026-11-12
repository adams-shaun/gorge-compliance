//! The answerer: what the step machine does at a prompt that is not a priority window.
//!
//! One arm per `WaitingFor` variant (147 at the pinned phase commit) and NO `_` arm, so a variant
//! phase adds later breaks the build here instead of falling into a default. `Priority` belongs to
//! the step machine. Every other variant is answered only when phase's own
//! [`AiDecisionContract`] offers exactly one candidate (a forced move, noted in the row); two or
//! more candidates, or none, is a strict miss. This skeleton never guesses; the decision answers
//! (targets, modes, yes/no, ...) are the next ticket's.
//!
//! phase-only conveniences (resolve-all, loop shortcuts, precast-copy shortcuts) are never
//! initiated by the follower; meeting one is a harness row.

use engine::ai_support::AiDecisionContract;
use engine::types::actions::GameAction;
use engine::types::game_state::WaitingFor;
use follower_proto::batch::Stop;
use follower_proto::decisions::DecisionQueue;
use follower_proto::row::strict_miss_msg;

/// What an answer step needs: the gorge decisions, the step index and the row notes.
pub struct Cursor<'a> {
    /// Unread in this skeleton; the decision answers of the next ticket take from it.
    #[allow(dead_code)]
    pub queue: &'a mut DecisionQueue,
    /// Index of the step being replayed.
    pub step: usize,
    pub notes: Vec<String>,
}

/// The action to apply to answer a prompt.
#[derive(Debug, Clone)]
pub struct Answer {
    pub action: GameAction,
}

/// The variant name of `wf` (its `Debug` text up to the first non-identifier character).
pub fn variant_name(wf: &WaitingFor) -> String {
    format!("{wf:?}").split(|c: char| !c.is_ascii_alphanumeric()).next().unwrap_or("").to_string()
}

fn harness(msg: impl Into<String>) -> Stop {
    Stop::Harness(format!("phase follower: {}", msg.into()))
}

/// Answer the one forced candidate of `wf`, or strict-miss.
fn forced(state: &engine::types::game_state::GameState, wf: &WaitingFor, cur: &mut Cursor) -> Result<Answer, Stop> {
    let name = variant_name(wf);
    let Some(owner) = wf.acting_player() else {
        return Err(Stop::StrictMiss(strict_miss_msg(&name, cur.step as i64, "")));
    };
    let mut candidates = AiDecisionContract::issue(state, owner).candidates;
    if candidates.len() == 1 {
        cur.notes.push(format!("forced {name} at step {}", cur.step));
        Ok(Answer { action: candidates.remove(0).action })
    } else {
        Err(Stop::StrictMiss(strict_miss_msg(&name, cur.step as i64, "")))
    }
}

/// The answer to the prompt `wf` (always `state.waiting_for`).
pub fn answer(state: &engine::types::game_state::GameState, wf: &WaitingFor, cur: &mut Cursor) -> Result<Answer, Stop> {
    match wf {
        // The step machine's: it never asks the answerer about priority.
        WaitingFor::Priority { .. } => Err(harness(format!("answerer asked about Priority at step {}", cur.step))),
        // The driver stops at game over before asking.
        WaitingFor::GameOver { .. } => Err(harness(format!("answerer asked about GameOver at step {}", cur.step))),
        // phase-only conveniences the follower never initiates.
        WaitingFor::ResolveAllConsent { .. }
        | WaitingFor::ResolveAllReady { .. }
        | WaitingFor::LoopShortcut { .. }
        | WaitingFor::RespondToShortcut { .. }
        | WaitingFor::PrecastCopyShortcutOffer { .. }
        | WaitingFor::RespondToPrecastCopyShortcut { .. } => {
            Err(harness(format!("phase-only prompt {} at step {}", variant_name(wf), cur.step)))
        }
        WaitingFor::MeldPairChoice { .. }
        | WaitingFor::MeldAttackTargetChoice { .. }
        | WaitingFor::EntryAttackTargetChoice { .. }
        | WaitingFor::MulliganDecision { .. }
        | WaitingFor::OpeningHandBottomCards { .. }
        | WaitingFor::ManaPayment { .. }
        | WaitingFor::ManaSourceSelection { .. }
        | WaitingFor::AssistChoosePlayer { .. }
        | WaitingFor::AssistPayment { .. }
        | WaitingFor::ChooseXValue { .. }
        | WaitingFor::TargetSelection { .. }
        | WaitingFor::DeclareAttackers { .. }
        | WaitingFor::DeclareBlockers { .. }
        | WaitingFor::UntapChoice { .. }
        | WaitingFor::ChooseUntapSubset { .. }
        | WaitingFor::ExertChoice { .. }
        | WaitingFor::EnlistChoice { .. }
        | WaitingFor::ReplacementChoice { .. }
        | WaitingFor::EntryControllerChoice { .. }
        | WaitingFor::OrderTriggers { .. }
        | WaitingFor::CopyTargetChoice { .. }
        | WaitingFor::ExploreChoice { .. }
        | WaitingFor::ReturnAsAuraTarget { .. }
        | WaitingFor::ChooseTokenHost { .. }
        | WaitingFor::EquipTarget { .. }
        | WaitingFor::CrewVehicle { .. }
        | WaitingFor::StationTarget { .. }
        | WaitingFor::SaddleMount { .. }
        | WaitingFor::ScryChoice { .. }
        | WaitingFor::RippleRevealChoice { .. }
        | WaitingFor::RippleBottomOrder { .. }
        | WaitingFor::RevealUntilBottomOrder { .. }
        | WaitingFor::DigBottomOrder { .. }
        | WaitingFor::ArrangePlanarDeckTopChoice { .. }
        | WaitingFor::RedistributeLifeTotals { .. }
        | WaitingFor::CoinFlipKeepChoice { .. }
        | WaitingFor::DieKeepChoice { .. }
        | WaitingFor::DigChoice { .. }
        | WaitingFor::DigRestSplitChoice { .. }
        | WaitingFor::SurveilChoice { .. }
        | WaitingFor::RevealChoice { .. }
        | WaitingFor::SearchChoice { .. }
        | WaitingFor::SearchPartitionChoice { .. }
        | WaitingFor::OutsideGameChoice { .. }
        | WaitingFor::ChooseFromZoneChoice { .. }
        | WaitingFor::BeholdChoice { .. }
        | WaitingFor::EmpowerJaceChoice { .. }
        | WaitingFor::SpellCopyOrderChoice { .. }
        | WaitingFor::ChooseOneOfBranch { .. }
        | WaitingFor::ConniveDiscard { .. }
        | WaitingFor::DiscardChoice { .. }
        | WaitingFor::HeroEventChoice { .. }
        | WaitingFor::EffectZoneChoice { .. }
        | WaitingFor::DrawnThisTurnTopdeckChoice { .. }
        | WaitingFor::LearnChoice { .. }
        | WaitingFor::ManifestDreadChoice { .. }
        | WaitingFor::TriggerTargetSelection { .. }
        | WaitingFor::BetweenGamesSideboard { .. }
        | WaitingFor::BetweenGamesChoosePlayDraw { .. }
        | WaitingFor::NamedChoice { .. }
        | WaitingFor::OpponentGuess { .. }
        | WaitingFor::SpellbookDraft { .. }
        | WaitingFor::DamageSourceChoice { .. }
        | WaitingFor::ModeChoice { .. }
        | WaitingFor::DiscardToHandSize { .. }
        | WaitingFor::OptionalCostChoice { .. }
        | WaitingFor::ChooseGiftRecipient { .. }
        | WaitingFor::SpliceOffer { .. }
        | WaitingFor::DefilerPayment { .. }
        | WaitingFor::OrderCostReductions { .. }
        | WaitingFor::CastOffer { .. }
        | WaitingFor::ModalFaceChoice { .. }
        | WaitingFor::AlternativeCastChoice { .. }
        | WaitingFor::MutateMergeChoice { .. }
        | WaitingFor::CipherEncodeChoice { .. }
        | WaitingFor::CastingVariantChoice { .. }
        | WaitingFor::ChoosePermanentTypeSlot { .. }
        | WaitingFor::MultiTargetSelection { .. }
        | WaitingFor::AbilityModeChoice { .. }
        | WaitingFor::OptionalEffectChoice { .. }
        | WaitingFor::ResolutionOptionalPaymentChoice { .. }
        | WaitingFor::PairChoice { .. }
        | WaitingFor::TributeChoice { .. }
        | WaitingFor::MiracleReveal { .. }
        | WaitingFor::OpponentMayChoice { .. }
        | WaitingFor::UnlessPayment { .. }
        | WaitingFor::UnlessPaymentChooseCost { .. }
        | WaitingFor::WardDiscardChoice { .. }
        | WaitingFor::WardSacrificeChoice { .. }
        | WaitingFor::UnlessBounceChoice { .. }
        | WaitingFor::ChooseRingBearer { .. }
        | WaitingFor::ChooseRoomDoor { .. }
        | WaitingFor::ChooseDungeon { .. }
        | WaitingFor::ChooseDungeonRoom { .. }
        | WaitingFor::SpecializeColor { .. }
        | WaitingFor::PayCost { .. }
        | WaitingFor::ActivationCostOneOfChoice { .. }
        | WaitingFor::CostTypeChoice { .. }
        | WaitingFor::BlightChoice { .. }
        | WaitingFor::PayManaAbilityMana { .. }
        | WaitingFor::ManaAbilityManaPayment { .. }
        | WaitingFor::ChooseManaColor { .. }
        | WaitingFor::CollectEvidenceChoice { .. }
        | WaitingFor::HarmonizeTapChoice { .. }
        | WaitingFor::RevealUntilKeptChoice { .. }
        | WaitingFor::RepeatDecision { .. }
        | WaitingFor::TopOrBottomChoice { .. }
        | WaitingFor::PopulateChoice { .. }
        | WaitingFor::ClashChooseOpponent { .. }
        | WaitingFor::ChooseFromZoneOpponentChooser { .. }
        | WaitingFor::ChooseAnnouncingOpponent { .. }
        | WaitingFor::ClashCardPlacement { .. }
        | WaitingFor::PhysicalContest { .. }
        | WaitingFor::VoteChoice { .. }
        | WaitingFor::SeparatePilesChooseOpponent { .. }
        | WaitingFor::SeparatePilesPartition { .. }
        | WaitingFor::SeparatePilesChoice { .. }
        | WaitingFor::SecretAgendaChoice { .. }
        | WaitingFor::CompanionReveal { .. }
        | WaitingFor::ChooseLegend { .. }
        | WaitingFor::CommanderZoneChoice { .. }
        | WaitingFor::BattleProtectorChoice { .. }
        | WaitingFor::ProliferateChoice { .. }
        | WaitingFor::TimeTravelChoice { .. }
        | WaitingFor::ChooseObjectsSelection { .. }
        | WaitingFor::CategoryChoice { .. }
        | WaitingFor::EachPlayerCopyChosenSelection { .. }
        | WaitingFor::KeepWithinTotalPowerChoice { .. }
        | WaitingFor::KeepExactPermanentsChoice { .. }
        | WaitingFor::CopyRetarget { .. }
        | WaitingFor::AssignCombatDamage { .. }
        | WaitingFor::AssignBlockerDamage { .. }
        | WaitingFor::DistributeAmong { .. }
        | WaitingFor::MoveCountersDistribution { .. }
        | WaitingFor::RemoveCountersChoice { .. }
        | WaitingFor::PayAmountChoice { .. }
        | WaitingFor::RetargetChoice { .. }
        | WaitingFor::CombatTaxPayment { .. }
        | WaitingFor::PhyrexianPayment { .. }
        => forced(state, wf, cur),
    }
}
