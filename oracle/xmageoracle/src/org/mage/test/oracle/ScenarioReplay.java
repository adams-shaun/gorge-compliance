package org.mage.test.oracle;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.google.gson.JsonPrimitive;
import mage.ConditionalMana;
import mage.Mana;
import mage.abilities.Ability;
import mage.abilities.Mode;
import mage.abilities.PlayLandAbility;
import mage.abilities.StaticAbility;
import mage.abilities.common.AttacksEachCombatStaticAbility;
import mage.abilities.common.SimpleStaticAbility;
import mage.abilities.effects.AsThoughEffect;
import mage.abilities.effects.ContinuousEffect;
import mage.abilities.effects.ContinuousEffects;
import mage.abilities.mana.ActivatedManaAbilityImpl;
import mage.abilities.costs.AlternativeSourceCosts;
import mage.abilities.costs.Cost;
import mage.abilities.costs.OptionalAdditionalSourceCosts;
import mage.abilities.costs.OrCost;
import mage.abilities.keyword.KickerAbility;
import mage.abilities.keyword.LeylineAbility;
import mage.abilities.keyword.SpreeAbility;
import mage.abilities.costs.mana.HybridManaCost;
import mage.abilities.costs.mana.ManaCost;
import mage.abilities.costs.mana.ManaCosts;
import mage.cards.repository.CardInfo;
import mage.cards.repository.CardRepository;
import mage.abilities.effects.common.EndTurnEffect;
import mage.abilities.effects.common.InfoEffect;
import mage.abilities.effects.common.TapSourceUnlessPaysEffect;
import mage.cards.Card;
import mage.cards.CardWithSpellOption;
import mage.cards.Cards;
import mage.cards.CardsImpl;
import mage.cards.DoubleFacedCard;
import mage.cards.ModalDoubleFacedCard;
import mage.constants.AsThoughEffectType;
import mage.constants.CardType;
import mage.constants.ColoredManaSymbol;
import mage.constants.ManaType;
import mage.constants.Outcome;
import mage.constants.PhaseStep;
import mage.constants.SubType;
import mage.constants.Zone;
import mage.counters.Counter;
import mage.counters.CounterType;
import mage.counters.Counters;
import mage.game.FreeForAll;
import mage.game.Game;
import mage.game.GameException;
import mage.game.turn.Step;
import mage.game.PutToBattlefieldInfo;
import mage.game.combat.CombatGroup;
import mage.game.events.GameEvent;
import mage.game.permanent.Permanent;
import mage.game.stack.Spell;
import mage.game.stack.StackAbility;
import mage.game.stack.StackObject;
import mage.players.ManaPool;
import mage.players.ManaPoolItem;
import mage.util.RandomUtil;
import mage.utils.SystemUtil;
import mage.players.Player;
import mage.filter.FilterCard;
import mage.target.TargetCard;
import mage.target.common.TargetCardInGraveyardBattlefieldOrStack;
import mage.util.CardUtil;
import mage.watchers.common.PermanentsEnteredBattlefieldWatcher;
import org.mage.test.player.PlayerAction;
import org.mage.test.player.TestPlayer;
import org.mage.test.serverside.base.CardTestPlayerBase;

import java.io.BufferedReader;
import java.io.FileReader;
import java.io.PrintWriter;
import java.io.FileWriter;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.EnumMap;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.UUID;
import java.util.function.BiPredicate;
import java.util.function.Predicate;

/**
 * Replays gorge oracle scenarios (the oracle-scenario schema of gorge's runner, scenarios/oracle) in XMage and
 * writes one result line per scenario: the canonical snapshot after setup and
 * after every step. Run with main(in.jsonl, out.jsonl) from Mage.Tests, so the
 * card database and deck files resolve. One JVM serves a whole batch; each
 * scenario starts from reset().
 *
 * Gorge-side vocabulary (step names, counter names, colours) is normalized by
 * the Go comparator, not here: this class reports what XMage sees.
 */
public class ScenarioReplay extends CardTestPlayerBase {

    private static final Gson GSON = new GsonBuilder().disableHtmlEscaping().serializeNulls().create();
    private int TURN = 1;
    private int turn = 1;
    private int activeSeat = 0;
    // The scenario's seat count ("players"; absent = 2). Read before reset(),
    // whose createNewGameAndPlayers seats a FreeForAll of three for it.
    private int seats = 2;
    private boolean attackAdvancedTurn = false;
    // The step name the next checkpoint reports instead of the one it is taken
    // in, set by a pass that stops before a declaration (declarationAhead).
    private PhaseStep checkpointStepAs = null;
    // Whether a forced waitStackResolved ran since the last checkpoint. The
    // replay loop captures it per step and the checkpoint runs the game's
    // state-based check before its snapshot (waitStackResolved override).
    private boolean settleCheckpoint = false;
    private static final PhaseStep MAIN = PhaseStep.PRECOMBAT_MAIN;

    private final List<JsonObject> snaps = new ArrayList<>();
    // The cards the unscripted search auto-pick took, in ask order. Test hook:
    // the pick only answers a trailing ask, which no checkpoint compares, so
    // the stored regression must assert the funnel itself and the card it
    // gathered rather than a board snapshot.
    private final List<String> searchPicks = new ArrayList<>();
    // The step a cast/resolve/checkpoint is registered at. MAIN until an
    // attack or block op moves the scenario into combat; gorge plays the
    // cast in the declare-attackers (attacking-only) or declare-blockers
    // (blocking) step, so the driver must too.
    private PhaseStep phase = MAIN;

    // Phase occurrences: an additional phase (CR 500.8) runs a new Step with
    // the same PhaseStep name in the same turn, so (turn, PhaseStep) alone
    // cannot key the actions queued at it. actionOccurrences carries the
    // occurrence each queued command belongs to; the player hides the other
    // occurrences at every priority, attack and block declaration.
    // queuedOccurrences is built from xmage_pos while the scenario is queued;
    // runtimeStepOccurrence counts the Step instances XMage actually starts.
    private final Map<Integer, EnumMap<PhaseStep, Integer>> queuedOccurrences = new HashMap<>();
    private int queuedOccTurn;
    private PhaseStep queuedOccStep;
    // The occurrence each queued PlayerAction belongs to. Identity-keyed so
    // tagging never replaces the action objects: the exact attack/block
    // batches hold the same instances and remove them by identity.
    private final Map<PlayerAction, Integer> actionOccurrences = new IdentityHashMap<>();
    private int runtimeOccTurn;
    private final Map<PhaseStep, Integer> runtimeStepCounts = new EnumMap<>(PhaseStep.class);
    private final Map<Step, Integer> runtimeStepOccurrence = new IdentityHashMap<>();

    // Where the scenario's LAST step queued its checkpoint: the pass cut's
    // position. The untap step grants no priority (CR 500.4), so a cut whose
    // position is (turn, UNTAP) takes its checkpoint at the pending
    // optional-untap ask there (CR 502.3) -- chooseUse's untap-election
    // branch flushes the queued runCode actions and pauses the game, the
    // row complete. Both stay null/0 when the last step is not an untap cut.
    private PhaseStep cutCheckpointPhase;
    private int cutCheckpointTurn;

    // The stack object names seen at the last checkpoint, for the empty-stack
    // diagnostic: what a pass pair expected to resolve and no longer can.
    private List<String> lastCheckpointStack = new ArrayList<>();

    // Whether the scenario scripts a divided-damage split ("<ref>^X=<n>"
    // target answers, oraclegen's damageSplitAnswers). Such a cast queues no
    // target names of its own: the split answers are the targets.
    private boolean splitScripted;

    /** The setup "tokens" this driver can place, keyed by the Forge token-script
     * stem gorge's setup names and valued by the XMage token of the same P/T,
     * colours, types and abilities (checked against the script). A stem outside
     * the table fails the scenario; the Go side keeps the same set
     * (xmageSetupTokens, held equal by TestXMageSetupTokenTableMatchesDriver). */
    static final Map<String, java.util.function.Supplier<mage.game.permanent.token.Token>> SETUP_TOKENS = new TreeMap<>();
    static {
        SETUP_TOKENS.put("b_1_1_bat_flying", mage.game.permanent.token.BatToken::new);
        SETUP_TOKENS.put("b_2_1_skeleton", mage.game.permanent.token.Skeleton21Token::new);
        SETUP_TOKENS.put("b_2_2_romulan", mage.game.permanent.token.RomulanToken::new);
        SETUP_TOKENS.put("b_2_2_zombie", mage.game.permanent.token.ZombieToken::new);
        SETUP_TOKENS.put("b_2_2_zombie_druid", mage.game.permanent.token.ZombieDruidToken::new);
        SETUP_TOKENS.put("c_0_0_a_thopter_flying", mage.game.permanent.token.Thopter00ColorlessToken::new);
        SETUP_TOKENS.put("c_1_1_a_borg", mage.game.permanent.token.BorgToken::new);
        SETUP_TOKENS.put("c_1_1_a_drone_flying_blockflying", mage.game.permanent.token.DroneFlyingToken::new);
        SETUP_TOKENS.put("c_1_1_a_servo", mage.game.permanent.token.ServoToken::new);
        SETUP_TOKENS.put("c_1_1_a_thopter_flying", mage.game.permanent.token.ThopterColorlessToken::new);
        SETUP_TOKENS.put("c_1_1_pilot_crewbuff", mage.game.permanent.token.PilotCrewToken::new);
        SETUP_TOKENS.put("c_1_1_pilot_crewsaddlebuff", mage.game.permanent.token.PilotSaddleCrewToken::new);
        SETUP_TOKENS.put("c_1_1_sliver", mage.game.permanent.token.SliverToken::new);
        SETUP_TOKENS.put("c_2_2_a_robot", mage.game.permanent.token.RobotToken::new);
        SETUP_TOKENS.put("c_3_2_a_vehicle_crew_1", mage.game.permanent.token.VehicleToken::new);
        SETUP_TOKENS.put("c_3_3_a_illusion", mage.game.permanent.token.IllusionArtifactToken::new);
        SETUP_TOKENS.put("c_a_clue_draw", mage.game.permanent.token.ClueArtifactToken::new);
        SETUP_TOKENS.put("c_a_food_sac", mage.game.permanent.token.FoodToken::new);
        SETUP_TOKENS.put("c_a_lander_sac_search", mage.game.permanent.token.LanderToken::new);
        SETUP_TOKENS.put("c_a_treasure_sac", mage.game.permanent.token.TreasureToken::new);
        SETUP_TOKENS.put("c_e_shard_draw", mage.game.permanent.token.ShardToken::new);
        SETUP_TOKENS.put("g_1_1_elf_warrior", mage.game.permanent.token.ElfWarriorToken::new);
        SETUP_TOKENS.put("g_2_2_spider_reach", mage.game.permanent.token.Spider22Token::new);
        SETUP_TOKENS.put("g_3_3_elephant", mage.game.permanent.token.ElephantToken::new);
        SETUP_TOKENS.put("murf", mage.game.permanent.token.MurfToken::new);
        SETUP_TOKENS.put("porthos", mage.game.permanent.token.PorthosToken::new);
        SETUP_TOKENS.put("r_1_1_goblin", mage.game.permanent.token.GoblinToken::new);
        SETUP_TOKENS.put("r_1_1_gremlin", mage.game.permanent.token.Gremlin11Token::new);
        SETUP_TOKENS.put("r_1_1_officer", mage.game.permanent.token.OfficerToken::new);
        SETUP_TOKENS.put("r_2_1_klingon_warrior_haste", mage.game.permanent.token.KlingonWarriorToken::new);
        SETUP_TOKENS.put("r_4_4_dinosaur_dragon_flying", mage.game.permanent.token.DinDragonToken::new);
        SETUP_TOKENS.put("u_1_1_faerie_flying", mage.game.permanent.token.FaerieToken::new);
        SETUP_TOKENS.put("u_1_1_fish", mage.game.permanent.token.FishNoAbilityToken::new);
        SETUP_TOKENS.put("u_1_1_merfolk", mage.game.permanent.token.MerfolkToken::new);
        SETUP_TOKENS.put("ur_1_1_otter_prowess", mage.game.permanent.token.OtterProwessToken::new);
        SETUP_TOKENS.put("w_0_4_wall_defender", mage.game.permanent.token.WallWhiteToken::new);
        SETUP_TOKENS.put("w_1_1_bird_flying", mage.game.permanent.token.BirdToken::new);
        SETUP_TOKENS.put("w_1_1_cat_lifelink", mage.game.permanent.token.Cat11LifelinkToken::new);
        SETUP_TOKENS.put("w_1_1_dog", mage.game.permanent.token.WhiteDogToken::new);
        SETUP_TOKENS.put("w_1_1_e_glimmer", mage.game.permanent.token.GlimmerToken::new);
        SETUP_TOKENS.put("w_1_1_human", mage.game.permanent.token.HumanToken::new);
        SETUP_TOKENS.put("w_1_1_human_soldier", mage.game.permanent.token.HumanSoldierToken::new);
        SETUP_TOKENS.put("w_1_1_monk_prowess", mage.game.permanent.token.MonasteryMentorToken::new);
        SETUP_TOKENS.put("w_1_1_rabbit", mage.game.permanent.token.RabbitToken::new);
        SETUP_TOKENS.put("w_1_1_soldier", mage.game.permanent.token.SoldierToken::new);
        SETUP_TOKENS.put("w_2_1_insect_flying", mage.game.permanent.token.InsectWhiteToken::new);
        SETUP_TOKENS.put("w_2_2_soldier", mage.game.permanent.token.Soldier22Token::new);
        SETUP_TOKENS.put("w_3_1_spirit_flying", mage.game.permanent.token.Spirit31Token::new);
        SETUP_TOKENS.put("w_4_4_beast_lonely", mage.game.permanent.token.BeastieToken::new);
        SETUP_TOKENS.put("wu_2_2_detective", mage.game.permanent.token.DetectiveToken::new);
    }

    /** A fresh XMage token for a setup stem; an unmapped stem fails the scenario. */
    static mage.game.permanent.token.Token setupToken(String stem) {
        java.util.function.Supplier<mage.game.permanent.token.Token> make = SETUP_TOKENS.get(stem);
        if (make == null) {
            throw new IllegalArgumentException("setup token " + stem + " has no XMage token");
        }
        return make.get();
    }

    /** One seat's setup "tokens" entries, in creation order. */
    static List<JsonObject> setupTokenEntries(JsonObject seatSetup) {
        List<JsonObject> out = new ArrayList<>();
        if (seatSetup.has("tokens")) {
            for (JsonElement e : seatSetup.getAsJsonArray("tokens")) {
                out.add(e.getAsJsonObject());
            }
        }
        return out;
    }

    // Override the factory the base class calls BEFORE it adds the player to
    // the game. Wrapping createPlayer(Game, ...)'s result instead copies a
    // player the game already holds, so scripted actions go to a player the
    // game never runs and every scenario ends with no snapshots.
    /** A three-seat scenario (gorge's oracleScenario.Players) is a FreeForAll
     * of p0 = PlayerA, p1 = PlayerB, p2 = PlayerC; every other scenario keeps
     * the base class's TwoPlayerDuel. XMage's PlayerList inserts each new
     * player AHEAD of the previous one (CircularList.add at index 0), so turn
     * and priority order run from the first player through the others in
     * reverse creation order: creating A, C, B makes it A -> B -> C, gorge's
     * p0 -> p1 -> p2. */
    @Override
    protected Game createNewGameAndPlayers() throws GameException, java.io.FileNotFoundException {
        if (seats != 3) {
            // A previous three-seat scenario left playerC set; the base
            // class's per-player helpers treat a non-null playerC as seated.
            playerC = null;
            // The base class's TwoPlayerDuel, with the pre-start hook.
            Game duel = new mage.game.TwoPlayerDuel(mage.constants.MultiplayerAttackOption.LEFT,
                    mage.constants.RangeOfInfluence.ONE, mage.game.mulligan.MulliganType.GAME_DEFAULT.getMulligan(0),
                    60, 20, 7) {
                @Override
                public void start(UUID choosingPlayerId) {
                    placeSetupCounters(this);
                    placeSetupTokens(this);
                    placeSetupAttached(this);
                    applySetupLife();
                    super.start(choosingPlayerId);
                }
            };
            playerA = createPlayer(duel, "PlayerA", deckNameA);
            playerB = createPlayer(duel, "PlayerB", deckNameB);
            return duel;
        }
        Game game = new FreeForAll(mage.constants.MultiplayerAttackOption.LEFT, mage.constants.RangeOfInfluence.ALL,
                mage.game.mulligan.MulliganType.GAME_DEFAULT.getMulligan(0), 20, 7) {
            @Override
            public void start(UUID choosingPlayerId) {
                placeSetupCounters(this);
                placeSetupTokens(this);
                placeSetupAttached(this);
                applySetupLife();
                super.start(choosingPlayerId);
            }
        };
        playerA = createPlayer(game, "PlayerA", deckNameA);
        playerC = createPlayer(game, "PlayerC", deckNameA);
        playerB = createPlayer(game, "PlayerB", deckNameB);
        return game;
    }

    /** The scenario's "players": absent (or 2) is the two-seat game, 3 the
     * FreeForAll; anything else fails the scenario, as gorge's runner does. */
    static int scenarioPlayers(JsonObject sc) {
        JsonElement v = sc.get("players");
        if (v == null || v.isJsonNull()) {
            return 2;
        }
        int n = v.getAsInt();
        if (n != 2 && n != 3) {
            throw new IllegalArgumentException("scenario players " + v + " (want 2 or 3)");
        }
        return n;
    }

    @Override
    protected TestPlayer createPlayer(String name, mage.constants.RangeOfInfluence rangeOfInfluence) {
        return new ScriptedChoicePlayer(new org.mage.test.player.TestComputerPlayer(name, rangeOfInfluence), this);
    }

    // The gorge-side "choose" picks of the cast step being queued (its
    // recorded answers), so an either-or cost ask XMage poses can be answered
    // the way gorge answered it. Empty when the step recorded none.
    private List<String> castCostPicks = new ArrayList<>();

    // Whether the cast step being replayed elected a mode whose cost is a
    // cast-time optional additional cost (Bargain). XMage asks that cost
    // through OptionalAdditionalSourceCosts; the ask is answered Yes so the
    // mode's reduction is in force, and the sacrifice it adds is paid from
    // this cast's own recorded choose picks.
    private boolean bargainedCast = false;
    // A cast step with cast_mode "optionalcost": gorge elected the cast's
    // optional additional cost (collect evidence, blight, waterbend, behold),
    // so the generator's leading scripted "Yes" is that cost's ask. On any
    // other cast a leading Yes belongs to a later ask and is left queued.
    private boolean optionalCostCast = false;
    // A cast step with cast_mode "kicked": gorge paid the spell's kicker cost
    // (CR 702.33a: an optional additional cost chosen at announcement, CR
    // 601.2b). XMage poses it as KickerAbility's "Pay <cost> kicker cost?"
    // chooseUse (the OptionalAdditionalSourceCosts hook), which is answered
    // Yes whether or not the generator scripted anything; every other optional
    // cost on the same cast is still declined as on a plain cast.
    private boolean kickedCast = false;
    // Kicker asks answered on the current kicked cast. The shape check at the
    // cast arm admits only one non-repeatable kicker cost, so a second ask is
    // an unmodelled kicker the driver must not keep paying.
    private int kickerAsks = 0;
    // A cast step with cast_mode "alternative": gorge elected the first
    // alternative cost (Leyline of Mutation's grant, Land Grant's reveal).
    // XMage poses that as its alternative-cost key menu. The mode itself is
    // the election, so the menu is answered with key 1 even when the
    // generator queued no answer (the cost is on the card, not a gorge
    // decision); an older "Yes" shape is rewritten to the same key.
    private boolean alternativeCostCast = false;
    /** The step's cast_mode is "evoked": the cast elects EvokeAbility's
     * alternative cost (CR 702.74a) from XMage's alternative-cost key menu. */
    private boolean evokedCast = false;
    // A cast step with cast_mode "conspired" or "casualty": gorge elected the
    // spell's (often granted) copy cost -- Raiding Schemes' conspire,
    // Silverquill's casualty 1. XMage poses each as its own
    // OptionalAdditionalSourceCosts yes/no (ConspireAbility, CasualtyAbility);
    // that ask is paid, and the tap/sacrifice it adds takes the generator's
    // scripted pick (or the step's choose pick, as Bargain's sacrifice does).
    // Null on every other cast.
    private Class<?> copyCostCast = null;
    // A cast step whose recorded gorge answer promised a Gift (pick
    // "gift_promise", CR 702.174a): XMage's GiftAbility poses the promise as
    // an optional additional cost, so the generator's leading "Yes" pays it,
    // and its PromiseGiftCost then asks which opponent (giftOpponentChoice).
    private boolean giftPromisedCast = false;
    /** Whether the replay followed a recorded pass position (turn/step/active seat). */
    private boolean followedPassPosition = false;

    /** Index of the one rule a trigger-order key picks by wording, or -1.
     * Returns -1 when some rule already prefix-matches the key verbatim (then
     * TestPlayer's own match is the answer) and when zero or several rules
     * match after normalisation. */
    static int triggerKeyMatch(String key, List<String> rules, String sourceName) {
        for (String rule : rules) {
            if (rule.startsWith(key)) {
                return -1;
            }
        }
        String want = triggerKeyForm(key, sourceName);
        if (want.endsWith(".")) {
            want = want.substring(0, want.length() - 1).trim();
        }
        if (want.isEmpty()) {
            return -1;
        }
        int found = -1;
        for (int i = 0; i < rules.size(); i++) {
            if (triggerKeyForm(rules.get(i), sourceName).startsWith(want)) {
                if (found >= 0) {
                    return -1;
                }
                found = i;
            }
        }
        return found;
    }

    /** Rule text folded for a wording-tolerant prefix match: markup dropped,
     * lower case, the source's name as "{this}", whitespace collapsed, and a
     * leading "when " / "whenever " as one token. */
    static String triggerKeyForm(String text, String sourceName) {
        String t = text.replaceAll("<[^<>]*>", "").toLowerCase(java.util.Locale.ROOT);
        if (sourceName != null && !sourceName.isEmpty()) {
            t = t.replace(sourceName.toLowerCase(java.util.Locale.ROOT), "{this}");
        }
        t = t.replaceAll("\\s+", " ").trim();
        if (t.startsWith("whenever ")) {
            t = "when " + t.substring("whenever ".length());
        }
        return t;
    }

    /** TestPlayer normally delegates these library decisions directly to its AI,
     * bypassing the scripted target/choice queues. Route them through this player. */
    private static final class ScriptedChoicePlayer extends TestPlayer {
        // The replay that built this player: it holds the cast step's recorded
        // cost picks. Carried through copy(), as XMage copies players freely.
        private final ScenarioReplay owner;

        // payment is the pool-mana multiset a cast or activate step must
        // spend, as WUBRGC letters, while its manual payment is active; null
        // outside that window. autoPaymentBefore is the pool mode to restore.
        private String payment;
        private Boolean autoPaymentBefore;

        ScriptedChoicePlayer(org.mage.test.player.TestComputerPlayer computerPlayer, ScenarioReplay owner) {
            super(computerPlayer);
            this.owner = owner;
        }

        ScriptedChoicePlayer(final ScriptedChoicePlayer player) {
            super(player);
            this.owner = player.owner;
            this.payment = player.payment;
            this.autoPaymentBefore = player.autoPaymentBefore;
        }

        /** Begin spending payment (gorge's logged pool spend) at this step.
         * Manual mode makes TestPlayer.playMana consume one unlocked colour
         * per unit; playMana below chooses that colour from what the unpaid
         * cost still needs, so {1}{G} with R and G spends R for generic and G
         * for the pip, never the other legal split. */
        void beginPayment(String letters) {
            payment = letters == null ? "" : letters.toUpperCase();
            autoPaymentBefore = getManaPool().isAutoPayment();
            getManaPool().setAutoPayment(false);
        }

        void endPayment() {
            payment = null;
            if (autoPaymentBefore != null) {
                getManaPool().setAutoPayment(autoPaymentBefore);
                autoPaymentBefore = null;
            }
        }

        /** A same-source trigger-order key spelled in Forge's wording.
         * TestPlayer matches the head choice with
         * ability.toString().startsWith(choice) only; the generator's key is
         * Forge's TriggerDescription$, which words the same trigger "When ..."
         * where XMage prints "Whenever ..." (Hero of Oxid Ridge) and ends the
         * key at a period where XMage's text continues (Nefarox's "sacrifices
         * a creature of their choice."). An unmatched head is a strict
         * "Missing CHOICE def" there. When the head prefix-matches no ability,
         * take the one ability whose normalised rule starts with the
         * normalised key (CR 603.3b: the controller's pick); none or several
         * matching keeps TestPlayer's own handling. */
        @Override
        public mage.abilities.TriggeredAbility chooseTriggeredAbility(
                List<mage.abilities.TriggeredAbility> abilities, Game game) {
            if (!getChoices().isEmpty()) {
                String choice = getChoices().get(0);
                List<String> rules = new ArrayList<>();
                for (mage.abilities.TriggeredAbility ability : abilities) {
                    rules.add(ability.toString());
                }
                String name = abilities.isEmpty() ? "" : abilities.get(0).getSourceObject(game).getName();
                int at = triggerKeyMatch(choice, rules, name);
                if (at >= 0) {
                    getChoices().remove(0);
                    return abilities.get(at);
                }
            }
            return super.chooseTriggeredAbility(abilities, game);
        }

        /** Run only the queued commands of the current phase occurrence.
         * TestPlayer matches an action by (turn, PhaseStep), which an
         * additional phase reuses; the hidden actions are put back after the
         * call so their own occurrence can still reach them. */
        @Override
        public boolean priority(Game game) {
            int occurrence = currentOccurrence(game);
            List<PlayerAction> hidden = hideOtherOccurrences(occurrence);
            try {
                if (game.getStep() != null && game.getStep().getType() == PhaseStep.DECLARE_ATTACKERS) {
                    // Every attacker was forced, so selectAttackers never
                    // ran: the declaration satisfied the queued commands.
                    dropAutoDeclaredAttacks(game);
                }
                return super.priority(game);
            } finally {
                getActions().addAll(hidden);
            }
        }

        /** The occurrence to keep visible, or 0 when this player holds no
         * positioned action (a helper test double). Asking the game for its
         * Step is only needed when some action can actually be filtered. */
        private int currentOccurrence(Game game) {
            if (owner.actionOccurrences == null) {
                return 0;
            }
            for (PlayerAction action : getActions()) {
                if (owner.actionOccurrences.containsKey(action)) {
                    return owner.runtimeOccurrence(game);
                }
            }
            return 0;
        }

        /** Remove the actions of every other occurrence of the current step,
         * in queue order, so TestPlayer cannot run them here. */
        private List<PlayerAction> hideOtherOccurrences(int occurrence) {
            List<PlayerAction> hidden = new ArrayList<>();
            if (owner.actionOccurrences == null) {
                return hidden;
            }
            for (Iterator<PlayerAction> it = getActions().iterator(); it.hasNext(); ) {
                PlayerAction action = it.next();
                Integer at = owner.actionOccurrences.get(action);
                if (at != null && at != occurrence) {
                    it.remove();
                    hidden.add(action);
                }
            }
            return hidden;
        }

        /** Spend one unit of the step's recorded payment. Pool.pay consumes
         * only the unlocked type in manual mode, so this unlocks the colour
         * the still-unpaid cost names; only when no specific pip remains may
         * any remaining unit pay the generic part. The first matching unit is
         * removed from the multiset, so the count, not the order, is kept. */
        @Override
        public boolean playMana(Ability ability, ManaCost unpaid, String promptText, Game game) {
            // payment != null is the manual window, not a nonempty multiset:
            // once takePayment exhausts it, payment is "" until endPayment.
            // Test the window, and let nextPaymentColour's null (exhausted or
            // unusable units) fall back, so no later ask in the step reaches
            // TestPlayer in manual mode with no choice.
            if (payment != null) {
                // A recorded coloured tap can be the unit a still-unpaid
                // forced pip needs while the plan has no letter of that
                // colour: gorge tapped the creature for that pip, and the
                // plan's other letters paid everything else. Take the tap
                // before nextPaymentColour's drop-a-letter fallback would
                // spend a pool unit the recorded cast kept.
                if (takeRecordedTapForForcedPip(unpaid, game)) {
                    return playQueuedTapPayment(ability, unpaid, promptText, game);
                }
                String next = nextPaymentColour(ability, unpaid, game);
                if (next != null) {
                    // TestPlayer.playMana consumes choices.get(0). This step's
                    // own answers (a resolution Yes/No, [choice_skip], a target
                    // pick) are already queued there and belong after the
                    // payment, so put the payment answer IN FRONT of them.
                    // Appending made playMana eat the step's answer and assert
                    // "Can't use choice in play mana: ...".
                    ManaType nextType = manaTypeOfName(next);
                    if (getManaPool().get(nextType) <= 0) {
                        // Only a conditional (restricted) unit of this type
                        // is in the pool. TestPlayer's colour choice asserts
                        // an unconditional unit ("pool must have red mana"),
                        // so unlock the type here: that is all the choice
                        // does before it reports the ask answered, and
                        // ManaPool.pay then spends the conditional unit when
                        // its restriction admits the spell.
                        getManaPool().unlockManaType(nextType);
                        return true;
                    }
                    getChoices().add(0, next);
                } else if (queueTapPaymentAction(game)) {
                    // The recorded plan is spent and the step's next answer is
                    // gorge's improvise/convoke tap. Stay in manual payment so
                    // TestPlayer takes the special action it now names.
                    return playQueuedTapPayment(ability, unpaid, promptText, game);
                } else {
                    fallBackToAutoPayment();
                }
            }
            return super.playMana(ability, unpaid, promptText, game);
        }

        /** Take the recorded tap special action the queue now names, clearing
         * the one-shot convoke colour answer on either return path. */
        private boolean playQueuedTapPayment(Ability ability, ManaCost unpaid,
                String promptText, Game game) {
            try {
                return super.playMana(ability, unpaid, promptText, game);
            } finally {
                convokeColour = null;
            }
        }

        /** Whether the still-unpaid cost forces a WUBRG pip the recorded plan
         * has no letter for, and the recorded payment tap at the front of the
         * choice queue pays exactly that colour. Such a tap is the recorded
         * unit for the pip: queue its special action and let playMana take it,
         * so assignPayment consumes the added unit and the pool unit the plan
         * intended to keep survives. A "for 1" tap does not qualify (it is the
         * generic contribution, and the plan-exhausted path below takes it);
         * neither does a tap for a colour no unpaid pip forces. */
        private boolean takeRecordedTapForForcedPip(ManaCost unpaid, Game game) {
            if (getChoices().isEmpty()) {
                return false;
            }
            String[] pick = tapPaymentPick(getChoices().get(0));
            if (pick == null || "1".equals(pick[1])) {
                return false;
            }
            char colour = pick[1].charAt(0);
            if (payment.indexOf(colour) >= 0) {
                return false;
            }
            Mana forced = new Mana();
            collectDue(unpaid, forced, new ArrayList<HybridManaCost>());
            if (forced.get(manaType(colour)) <= 0) {
                return false;
            }
            return queueTapPaymentAction(game);
        }

        /** The colour XMage's convoke effect is to be answered with when it
         * asks which mana the tapped creature pays (set only while a convoke
         * special action runs inside playMana). The effect asks only when the
         * creature can pay more than one kind, so the answer is queued lazily
         * in choose(Choice), never up front where an unasked answer would be
         * left unconsumed. */
        private String convokeColour;

        /** Translate gorge's recorded payment tap ("Tap <name> for 1" or "for
         * <colour>", pay.ConvokeAsk's option label) at the front of the choice
         * queue into XMage's special mana action: the choice becomes the
         * action's rule prefix ("Improvise" / "Convoke", the key TestPlayer
         * matches in its manual branch) and the permanent goes to the front of
         * the target queue, which the action's own target ask reads. The
         * action is the one whose legal set holds a permanent of that name, so
         * an artifact improvises and a creature convokes. The ask's trailing
         * [choice_skip] is gorge's end of the tap menu and is dropped with the
         * last pick. False (nothing queued) when no action names the pick: the
         * caller then falls back to auto-payment as before. */
        private boolean queueTapPaymentAction(Game game) {
            if (getChoices().isEmpty()) {
                return false;
            }
            String[] pick = tapPaymentPick(getChoices().get(0));
            Map<UUID, mage.abilities.SpecialAction> actions =
                    game.getState().getSpecialActions().getControlledBy(getId(), true);
            if (pick == null) {
                // Not a tap: the bare card name of a recorded Delve exile
                // pick (CR 702.66a), or nothing this hook translates.
                return queueDelveAction(game, actions.values());
            }
            for (mage.abilities.SpecialAction action : actions.values()) {
                String rule = action.getRule(true);
                String key = rule.startsWith("Improvise") ? "Improvise"
                        : rule.startsWith("Convoke") ? "Convoke" : null;
                if (key == null || action.getTargets().isEmpty()
                        || !action.canActivate(getId(), game).canActivate()) {
                    continue;
                }
                mage.target.Target target = action.getTargets().get(0);
                Set<UUID> legal = target.possibleTargets(getId(), action, game);
                boolean named = false;
                for (UUID id : legal) {
                    if (hasObjectTargetNameOrAlias(game.getPermanent(id), pick[0])) {
                        named = true;
                        break;
                    }
                }
                if (!named) {
                    continue;
                }
                getChoices().remove(0);
                if (!getChoices().isEmpty() && TestPlayer.CHOICE_SKIP.equals(getChoices().get(0))) {
                    getChoices().remove(0);
                }
                getChoices().add(0, key);
                getTargets().add(0, pick[0]);
                if (target.getMaxNumberOfTargets() > 1 && legal.size() > 1) {
                    // Improvise takes up to one artifact per generic mana and
                    // asks again while more can be chosen: close the ask.
                    getTargets().add(1, TestPlayer.TARGET_SKIP);
                }
                convokeColour = "Convoke".equals(key) ? tapPaymentColourName(pick[1]) : null;
                return true;
            }
            return false;
        }

        /** Expand the front choice when oraclegen joined a multi-pick choose
         * with "^" (JoinedChoice): each segment is one pick, resolved the way
         * the single-answer path resolves a value (an "@ref" alias is bound
         * and kept, a bare ref becomes its alias, a plain label is unchanged).
         * Null when the front choice is not joined, so callers can use the
         * queue as the pick list unchanged. */
        private List<String> joinedChoiceSegments() {
            if (getChoices().isEmpty()) {
                return null;
            }
            String front = getChoices().get(0);
            if (front == null || front.indexOf('^') < 0) {
                return null;
            }
            List<String> segments = new ArrayList<>();
            for (String segment : front.split("\\^")) {
                segments.add(resolveDelvePick(segment));
            }
            return segments;
        }

        /** One "^" segment of a joined answer as the choice queue would hold
         * it on its own: strip the "@" alias marker to find the ref, then let
         * the answer path bind it and map it to its target spelling (an
         * already-aliased ref comes back as its alias). A non-ref label is
         * unchanged. */
        private String resolveDelvePick(String segment) {
            String ref = segment.startsWith("@") ? segment.substring(1) : segment;
            if (owner != null && isScenarioRef(ref)) {
                return owner.aliasChoiceValue(ref);
            }
            return segment;
        }

        /** Translate gorge's recorded Delve exile picks (CR 702.66a: one
         * choose_n pick per exiled card, the bare card name; the ask's
         * trailing [choice_skip] is the Done option) at the front of the
         * choice queue into XMage's Delve special mana action: the choice
         * becomes that action's own rule text (TestPlayer's manual branch
         * takes the action whose rule starts with the choice, and the Delve
         * rule leads with its cost, "Exile a card from your graveyard: Delve
         * (...)", so no short "Delve" key matches) and the exiled graveyard
         * cards follow it on the choice queue: the exile cost's target is a
         * Cost target, which TestPlayer asks through its choice queue (the
         * target queue only feeds an ability's own targets). The action takes at most
         * the cost target's maximum cards per activation (one in manual
         * payment); further recorded picks stay queued for the next payment
         * cycle, where XMage re-adds the action, as gorge re-poses its ask
         * per pick. Taken only when the Delve special action lists a card of
         * the front answer's name among its legal (graveyard) targets; false
         * (nothing queued) otherwise.
         * <p>
         * oraclegen scripts a multi-pick choose as ONE makeChoose answer, with
         * its picks joined by "^" (see JoinedChoice in gen.go). The front
         * answer is expanded into its individual picks before matching, and
         * the picks this activation cannot take stay on the queue as separate
         * entries for the next payment cycle. */
        private boolean queueDelveAction(Game game,
                java.util.Collection<mage.abilities.SpecialAction> actions) {
            // A joined answer is still one queue entry until a Delve action
            // accepts a pick: only commit the split once something matched, so
            // a non-Delve front answer is left exactly as it was.
            List<String> joined = joinedChoiceSegments();
            for (mage.abilities.SpecialAction action : actions) {
                // The class is the stable identity: the rule text leads with
                // the exile cost, so it never starts with "Delve".
                if (!"DelveSpecialAction".equals(action.getClass().getSimpleName())
                        || !action.canActivate(getId(), game).canActivate()) {
                    continue;
                }
                // The exile target sits on the action or on its exile cost.
                mage.target.Targets targets = action.getTargets().isEmpty()
                        ? action.getCosts().getTargets() : action.getTargets();
                if (targets.isEmpty()) {
                    continue;
                }
                mage.target.Target target = targets.get(0);
                Set<UUID> legal = target.possibleTargets(getId(), action, game);
                List<String> picks = joined != null ? joined : getChoices();
                List<String> picked = new ArrayList<>();
                Set<UUID> used = new java.util.HashSet<>();
                while (picked.size() < picks.size() && used.size() < legal.size()
                        && picked.size() < target.getMaxNumberOfTargets()) {
                    String name = picks.get(picked.size());
                    UUID found = null;
                    for (UUID id : legal) {
                        if (!used.contains(id)
                                && hasObjectTargetNameOrAlias(game.getCard(id), name)) {
                            found = id;
                            break;
                        }
                    }
                    if (found == null) {
                        break;
                    }
                    used.add(found);
                    picked.add(name);
                }
                if (picked.isEmpty()) {
                    continue;
                }
                if (joined != null) {
                    // Replace the one joined answer with its picks now that the
                    // action has taken some: the leading picks are removed
                    // below, so the rest remain queued one per entry for the
                    // next payment cycle.
                    getChoices().remove(0);
                    for (int i = joined.size() - 1; i >= 0; i--) {
                        getChoices().add(0, joined.get(i));
                    }
                }
                for (int i = 0; i < picked.size(); i++) {
                    getChoices().remove(0);
                }
                if (!getChoices().isEmpty() && TestPlayer.CHOICE_SKIP.equals(getChoices().get(0))) {
                    getChoices().remove(0);
                }
                getChoices().add(0, action.getRule(true));
                for (int i = 0; i < picked.size(); i++) {
                    getChoices().add(1 + i, picked.get(i));
                }
                if (target.getMaxNumberOfTargets() > picked.size() && legal.size() > picked.size()) {
                    // The cost target takes up to one card per generic mana
                    // and asks again while more can be chosen: close the ask.
                    getChoices().add(1 + picked.size(), TestPlayer.CHOICE_SKIP);
                }
                convokeColour = null;
                return true;
            }
            return false;
        }

        /** Splits gorge's waterbend-helper label "Tap <name> to waterbend for N"
         * into the permanent's name, or null for any other label. XMage poses
         * the waterbend ask as a TargetControlledPermanent choice, which reads
         * the CHOICE queue by name, not the recorded mana-payment plan. */
        static String waterbendTapName(String label) {
            if (label == null || !label.startsWith("Tap ")) {
                return null;
            }
            int at = label.indexOf(" to waterbend for ");
            if (at < 4) {
                return null;
            }
            String name = label.substring(4, at);
            return name.isEmpty() ? null : name;
        }

        /** Splits a payment-tap label into {permanent name, contribution}
         * ("1" or a WUBRG letter), or null for any other label: a mana wheel's
         * "Tap <name> for mana", a waterbend, a Harmonize "(reduce by N)". */
        static String[] tapPaymentPick(String label) {
            if (label == null || !label.startsWith("Tap ")) {
                return null;
            }
            int at = label.lastIndexOf(" for ");
            if (at < 4) {
                return null;
            }
            String what = label.substring(at + 5);
            if (!what.matches("[1WUBRG]")) {
                return null;
            }
            String name = label.substring(4, at);
            if (name.isEmpty() || name.endsWith(" to waterbend")) {
                return null;
            }
            return new String[] {name, what};
        }

        /** XMage's colour-choice text for a tap's contribution. */
        static String tapPaymentColourName(String what) {
            switch (what) {
                case "W": return "White";
                case "U": return "Blue";
                case "B": return "Black";
                case "R": return "Red";
                case "G": return "Green";
                default: return "Colorless";
            }
        }

        /** gorge's recorded multiset cannot pay the rest of this ask: XMage's
         * cost is larger (a cost-reduction difference or an additional cost
         * paid in the same step), a restricted unit is not in its pool, or the
         * units are exhausted. Auto-payment is the old behaviour and keeps the
         * ordinary snapshot divergence instead of TestPlayer failing the whole
         * harness. */
        void fallBackToAutoPayment() {
            payment = null;
            getManaPool().setAutoPayment(true);
        }

        private String nextPaymentColour(Ability ability, ManaCost unpaid, Game game) {
            Mana due = unpaid.getMana();
            // An exhausted plan (the manual window is still open, so payment is
            // "" not null) cannot say which unit pays the rest. A cost larger
            // than gorge's recorded spend (a reduction difference or an
            // additional cost paid in the same step) must go back to
            // auto-payment instead of spending an arbitrary pool unit.
            if (payment.isEmpty()) {
                return null;
            }
            // A specific coloured pip can only be paid by its own type. Prefer
            // the recorded unit of that type, but the plan is a preference, not
            // a restriction: a dual land's chosen colour or a plan written
            // before a restriction lifted can leave the pool holding the type
            // while the plan names another, and refusing it would strand the
            // cast. COLORLESS is XMage's generic bucket, so it belongs to the
            // plan-preferring generic phase below, not here: taking a {C} unit
            // here would bypass the plan whenever the pool also holds a colour
            // gorge actually spent.
            // A hybrid pip is NOT a specific pip: XMage's HybridManaCost keeps
            // BOTH halves in its cost Mana, so walking `due` would spend a
            // floating W for every {R/W} even when the plan holds no W left
            // (CR 107.4e lets either half pay). Split the hybrids out: the
            // forced pips go first, then each hybrid takes the plan letter
            // that is one of its halves before any WUBRG order.
            Mana forced = new Mana();
            List<HybridManaCost> hybrids = new ArrayList<>();
            collectDue(unpaid, forced, hybrids);
            for (char c : "WUBRG".toCharArray()) {
                ManaType type = manaType(c);
                if (forced.get(type) > 0 && poolSpendable(type, ability, game)) {
                    return payment.indexOf(c) >= 0
                            ? takePayment(c) : dropPaymentLetterAndName(c, ability, game);
                }
            }
            for (HybridManaCost hybrid : hybrids) {
                String halves = hybridHalves(hybrid);
                for (int i = 0; i < payment.length(); i++) {
                    char c = payment.charAt(i);
                    if (halves.indexOf(c) >= 0 && poolSpendable(manaType(c), ability, game)) {
                        return takePayment(c);
                    }
                }
            }
            for (HybridManaCost hybrid : hybrids) {
                String halves = hybridHalves(hybrid);
                for (char c : "WUBRG".toCharArray()) {
                    if (halves.indexOf(c) >= 0 && poolSpendable(manaType(c), ability, game)) {
                        return dropPaymentLetterAndName(c, ability, game);
                    }
                }
            }
            // An any-type asThough effect (Vizier of the Menagerie) lets a pool
            // unit of ANOTHER type pay a pip the pool holds none of. XMage's
            // ManaPool.pay then wants the pip's own type unlocked and picks
            // the unit itself, so name the pip's colour and spend the plan
            // letter of the unit that converts; auto-payment here would tap a
            // land for the pip and leave the recorded activation stranded.
            for (char c : "WUBRG".toCharArray()) {
                ManaType type = manaType(c);
                if (due.get(type) <= 0) {
                    continue;
                }
                char unit = convertedUnit(type, ability, game);
                if (unit != 0) {
                    if (payment.indexOf(unit) >= 0) {
                        takePayment(unit);
                    } else {
                        dropPaymentLetterAndName(unit, ability, game);
                    }
                    return colourName(c);
                }
            }
            // A coloured pip the pool does not hold is not payable with any
            // other colour: leave it to auto-payment (which may float a new
            // source) instead of spending a unit the cost cannot take.
            if (hasColouredPip(due)) {
                return null;
            }
            // Only the generic part remains, and every unit pays it. Gorge's
            // recorded multiset names the units it spent, so take a plan unit
            // first: a pool unit gorge did NOT spend must not be paid for the
            // generic part while a recorded unit is still available, or the
            // post-cast pool holds a different colour than gorge's.
            for (char c : "WUBRGC".toCharArray()) {
                if (payment.indexOf(c) >= 0 && poolSpendable(manaType(c), ability, game)) {
                    return takePayment(c);
                }
            }
            // No plan letter names a pool unit. The replayed pool can still
            // hold the same COUNT in other colours (a dual land's chosen colour,
            // a restriction change), so pay one of those rather than strand the
            // cast, keeping the plan's total in step.
            for (char c : "WUBRGC".toCharArray()) {
                if (poolSpendable(manaType(c), ability, game)) {
                    return dropPaymentLetterAndName(c, ability, game);
                }
            }
            return null;
        }

        /** Split an unpaid cost into the mana every pip forces (`forced`) and
         * its hybrid pips, whose halves are alternatives, not both due. */
        private static void collectDue(ManaCost cost, Mana forced, List<HybridManaCost> hybrids) {
            if (cost instanceof HybridManaCost) {
                hybrids.add((HybridManaCost) cost);
            } else if (cost instanceof ManaCosts) {
                for (Object part : (ManaCosts<?>) cost) {
                    collectDue((ManaCost) part, forced, hybrids);
                }
            } else {
                forced.add(cost.getMana());
            }
        }

        /** The plan letters of a hybrid pip's two halves ("RW" for {R/W}); a
         * generic half contributes none. */
        private static String hybridHalves(HybridManaCost hybrid) {
            StringBuilder halves = new StringBuilder();
            for (ColoredManaSymbol half : new ColoredManaSymbol[]{hybrid.getMana1(), hybrid.getMana2()}) {
                if (half != null) {
                    halves.append(half.toString().charAt(0));
                }
            }
            return halves.toString();
        }

        /** Whether the unpaid cost still names a WUBRG pip. XMage folds the
         * generic part into COLORLESS, so this is the "generic only" test. */
        private static boolean hasColouredPip(Mana due) {
            for (char c : "WUBRG".toCharArray()) {
                if (due.get(manaType(c)) > 0) {
                    return true;
                }
            }
            return false;
        }

        /** Spend a pool unit the recorded plan did not name: the generic part
         * accepts any colour, so drop one plan unit to keep the plan's count
         * aligned with the units still to spend. Drop a letter whose unit the
         * pool no longer holds when there is one: it can never be honoured, so
         * it is the least-needed remaining letter. */
        private String dropPaymentLetterAndName(char c, Ability ability, Game game) {
            if (payment != null && !payment.isEmpty()) {
                int i = leastNeededPaymentLetter(ability, game);
                payment = payment.substring(0, i) + payment.substring(i + 1);
            }
            return colourName(c);
        }

        /** Index of the plan letter with no pool unit behind it, or 0 when
         * every remaining letter still has one (the specific-pip branch can
         * reach the fallback while the pool holds each plan type). */
        private int leastNeededPaymentLetter(Ability ability, Game game) {
            for (int i = 0; i < payment.length(); i++) {
                if (!poolSpendable(manaType(payment.charAt(i)), ability, game)) {
                    return i;
                }
            }
            return 0;
        }

        private String takePayment(char c) {
            int i = payment.indexOf(c);
            payment = payment.substring(0, i) + payment.substring(i + 1);
            return colourName(c);
        }

        /** Whether this ask can actually spend a pool unit of `type`: an
         * unconditional unit, or a conditional (restricted) unit whose own
         * restriction admits the ability and cost being paid. The plain
         * ManaPool.get excludes conditional units, so a cast paid from them
         * alone looked unpayable and fell back to auto-payment. But mere
         * presence is not enough either: ManaPool.pay spends a conditional
         * unit only when ConditionalMana.apply(ability, game, producer,
         * costToPay) accepts the ask. Reporting a rejected unit as spendable
         * would consume the plan letter and unlock a type pay then refuses,
         * leaving auto-payment too late to spend already-floating mana. */
        private boolean poolSpendable(ManaType type, Ability ability, Game game) {
            if (getManaPool().get(type) > 0) {
                return true;
            }
            Cost costToPay = ability == null ? null : ability.getManaCostsToPay();
            for (ConditionalMana mana : getManaPool().getConditionalMana()) {
                if (mana.get(type) > 0
                        && mana.apply(ability, game, mana.getManaProducerId(), costToPay)) {
                    return true;
                }
            }
            return false;
        }

        /** The pool unit (a plan letter first, else WUBRGC order) that an
         * asThough effect lets pay a `pip` the pool holds no unit of, or 0.
         * Asks the same ContinuousEffects.asThoughMana that ManaPool.pay asks,
         * with a one-unit item per type as pay's own restricted-payment check
         * does, so a conditional or unapplicable effect is never assumed. */
        private char convertedUnit(ManaType pip, Ability ability, Game game) {
            if (ability == null || game == null) {
                return 0;
            }
            String order = payment + "WUBRGC";
            for (int i = 0; i < order.length(); i++) {
                ManaType unit = manaType(order.charAt(i));
                if (unit == pip || getManaPool().get(unit) <= 0) {
                    continue;
                }
                ManaPoolItem item = new ManaPoolItem();
                item.add(unit, 1);
                ManaType usable = game.getContinuousEffects().asThoughMana(
                        pip, item, ability.getSourceId(), ability, ability.getControllerId(), game);
                if (usable == unit) {
                    return order.charAt(i);
                }
            }
            return 0;
        }

        private static ManaType manaTypeOfName(String name) {
            for (char c : "WUBRGC".toCharArray()) {
                if (colourName(c).equals(name)) {
                    return manaType(c);
                }
            }
            throw new IllegalArgumentException("mana colour " + name);
        }

        private static String colourName(char c) {
            switch (c) {
                case 'W': return "White";
                case 'U': return "Blue";
                case 'B': return "Black";
                case 'R': return "Red";
                case 'G': return "Green";
                case 'C': return "Colorless";
                default: throw new IllegalArgumentException("mana letter " + c);
            }
        }

        private static ManaType manaType(char c) {
            switch (c) {
                case 'W': return ManaType.WHITE;
                case 'U': return ManaType.BLUE;
                case 'B': return ManaType.BLACK;
                case 'R': return ManaType.RED;
                case 'G': return ManaType.GREEN;
                case 'C': return ManaType.COLORLESS;
                default: throw new IllegalArgumentException("mana letter " + c);
            }
        }

        /** Whether the current ask is posed from inside the named method of a
         * class of the given type (a keyword's cast-time cost hook). */
        private static boolean askedBy(Class<?> type, String method) {
            return StackWalker.getInstance(StackWalker.Option.RETAIN_CLASS_REFERENCE).walk(frames ->
                    frames.anyMatch(f -> f.getMethodName().equals(method) && type.isAssignableFrom(f.getDeclaringClass())));
        }

        /** Like askedBy, for a class the driver cannot name (a package-private
         * cost class): matched by simple name. */
        private static boolean askedByName(String simpleName, String method) {
            return StackWalker.getInstance(StackWalker.Option.RETAIN_CLASS_REFERENCE).walk(frames ->
                    frames.anyMatch(f -> f.getMethodName().equals(method)
                            && f.getDeclaringClass().getSimpleName().equals(simpleName)));
        }

        /**
         * The answer to a yes/no a trigger poses while it resolves, when the
         * step queued no Yes/No for it: gorge folded the may into the body's
         * pick or cost (CR 603.5: the choice is made as the trigger resolves,
         * and a may with one legal outcome changes nothing), so it logged the
         * pick alone -- or nothing at all when there was nothing to pick (an
         * empty hand for "put a Soldier card from your hand ...", X = 0 for
         * "tap X untapped Myr ..."). XMage asks the may regardless: the
         * trigger's own "Use ability?" (TriggeredAbilityImpl.resolve) and the
         * effect's own "Put ... from your hand?". A queued pick (a name on
         * the choice or target queue) means gorge went ahead: yes, the pick is
         * left for the body's own ask. No pick, or a skip gorge logged for the
         * declined pick (taken off the queue with the decline, as mayPrefix
         * does), means no. Null when the ask is not posed by a resolving
         * trigger, or a scripted Yes/No heads the choice queue and TestPlayer
         * consumes it.
         */
        static Boolean foldedOptionalAnswer(boolean triggerResolves, List<String> choices,
                List<String> targets) {
            if (!triggerResolves) {
                return null;
            }
            if (!choices.isEmpty() && (choices.get(0).equals("Yes") || choices.get(0).equals("No"))) {
                return null;
            }
            boolean choicePick = !choices.isEmpty() && !TestPlayer.CHOICE_SKIP.equals(choices.get(0));
            boolean targetPick = !targets.isEmpty() && !TestPlayer.TARGET_SKIP.equals(targets.get(0));
            if (choicePick || targetPick) {
                return Boolean.TRUE;
            }
            if (!choices.isEmpty()) {
                choices.remove(0);
            }
            if (!targets.isEmpty()) {
                targets.remove(0);
            }
            return Boolean.FALSE;
        }

        /** The opponent a promised Gift goes to: in this two-seat harness the
         * only opponent, named on the choice queue the way makeChoose reads a
         * player, unless the scenario scripted a pick of its own. Null when
         * the ask is not a lone-opponent gift promise. */
        static String giftOpponentChoice(boolean giftAsk, List<String> opponentNames, List<String> choices) {
            if (!giftAsk || opponentNames.size() != 1
                    || !choices.isEmpty() && opponentNames.contains(choices.get(0))) {
                return null;
            }
            return opponentNames.get(0);
        }

        /** The per-zone pick may an attach effect poses before every card it
         * takes from a zone ("Attach an Aura from the battlefield?", "... from
         * your hand?", "... from your graveyard?"): the zone is the message's
         * last words, so a new card with the same loop is covered by the role.
         * The "Attach an " prefix keeps other per-zone mays (a cast or put ask
         * with its own scripted boolean) out of it. */
        static boolean zonePickAsk(String message) {
            return zoneOfPickAsk(message) != null;
        }

        /** The zone a per-zone pick ask names: HAND, GRAVEYARD or BATTLEFIELD,
         * else null. */
        static Zone zoneOfPickAsk(String message) {
            if (message == null || !message.startsWith("Attach an ") || !message.endsWith("?")) {
                return null;
            }
            if (message.endsWith(" from your hand?")) {
                return Zone.HAND;
            }
            if (message.endsWith(" from your graveyard?")) {
                return Zone.GRAVEYARD;
            }
            if (message.endsWith(" from the battlefield?")) {
                return Zone.BATTLEFIELD;
            }
            return null;
        }

        private boolean zonePickQueued(String message, Game game) {
            Zone zone = zoneOfPickAsk(message);
            if (zone == null) {
                return false;
            }
            return zonePickQueued(zone, getChoices(), getTargets(), (askedZone, answer) -> {
                if (askedZone == Zone.BATTLEFIELD) {
                    for (mage.game.permanent.Permanent p : game.getBattlefield().getAllActivePermanents()) {
                        if (answerNamesCandidate(game, p.getId(), answer)) {
                            return true;
                        }
                    }
                    return false;
                }
                Cards cards = askedZone == Zone.HAND ? getHand() : getGraveyard();
                for (UUID id : cards) {
                    if (answerNamesCandidate(game, id, answer)) {
                        return true;
                    }
                }
                return false;
            });
        }

        /** Whether a per-zone pick may is scripted to go ahead: either queue's
         * head may name a candidate of the ask's zone. choose(Cards,
         * TargetCard) reads the CHOICE queue, while the generator may have
         * scripted the pick on the TARGET queue, which handOffCardPick moves
         * to the choice queue only when the Cards ask comes, so both are
         * consulted and neither queue is consumed. A head that is a skip or
         * a scripted Yes/No is no pick. */
        static boolean zonePickQueued(Zone zone, List<String> choices, List<String> targets,
                BiPredicate<Zone, String> namesCandidateInZone) {
            return zonePickHeadQueued(zone, choices, namesCandidateInZone)
                    || zonePickHeadQueued(zone, targets, namesCandidateInZone);
        }

        private static boolean zonePickHeadQueued(Zone zone, List<String> queue,
                BiPredicate<Zone, String> namesCandidateInZone) {
            if (queue.isEmpty()) {
                return false;
            }
            String head = queue.get(0);
            if (head.equals("Yes") || head.equals("No")
                    || TestPlayer.CHOICE_SKIP.equals(head) || TestPlayer.TARGET_SKIP.equals(head)) {
                return false;
            }
            return namesCandidateInZone.test(zone, head);
        }

        private boolean scriptedYesNoNext() {
            return !getChoices().isEmpty() && (getChoices().get(0).equals("Yes") || getChoices().get(0).equals("No"));
        }

        /** Whether the ask is UntapLandsEffect's up-to untap shortcut. XMage
         * offers it before the target ask when every tapped land fits under
         * the effect's cap: "Include your tapped lands to untap?". Its
         * chooseUse does not accept aliases, while the target.choose that
         * follows is makeChoose and does, so the shortcut must not eat
         * gorge's picks. */
        static boolean includeTappedLandsAsk(String message) {
            return "Include your tapped lands to untap?".equals(message);
        }

        /** Whether {@code answer} names a land the shortcut could untap:
         * {@code tappedOnly} restricts it to the effect's candidates (tapped
         * lands the source's controller controls), otherwise any land on the
         * battlefield, so an already-untapped or foreign pick is recognised
         * as the no-op it is. */
        private boolean untapIncludeNames(Game game, Ability source, String answer, boolean tappedOnly) {
            if (answer == null || game == null) {
                return false;
            }
            UUID controllerId = source != null ? source.getControllerId() : getId();
            for (Permanent permanent : game.getBattlefield().getAllActivePermanents()) {
                if (!permanent.isLand(game)) {
                    continue;
                }
                if (tappedOnly && (!permanent.isTapped()
                        || controllerId == null || !controllerId.equals(permanent.getControllerId()))) {
                    continue;
                }
                if (answerNamesCandidate(game, permanent.getId(), answer)) {
                    return true;
                }
            }
            return false;
        }

        /** XMage's Phyrexian cost prompts: AbilityImpl.handlePhyrexianCosts
         * ("... (phyrexian cost)?" at CR 601.2b), ManaCostsImpl
         * .handlePhyrexianManaCosts ("...?" while a cost is paid) and
         * AbilityImpl.handlePhyrexianLikeEffects (K'rrik's PayLifeInsteadOf
         * grant, "... (pay life cost)?"). All three ask whether to pay two
         * life instead of a pip, and only these three use this wording. */
        static boolean phyrexianAsk(String message) {
            return message != null && message.startsWith("Pay 2 life instead of ");
        }

        /** Whether a Phyrexian ask must be declined because the step queued
         * no boolean for it. The generator scripts "yes" only for a recorded
         * pay_life face, so a starved ask means gorge paid the pip's mana
         * face: answer no WITHOUT consuming the step's next answer.
         * TestPlayer would otherwise stop the replay at "Missing CHOICE def"
         * before the cost can be paid from the pool (Blinding Souleater's
         * {W/P} activation). A queued Yes/No is left for TestPlayer, which
         * consumes it as the recorded life-face answer. */
        static boolean phyrexianStarved(String message, List<String> choices) {
            if (!phyrexianAsk(message)) {
                return false;
            }
            if (choices.isEmpty()) {
                return true;
            }
            String head = choices.get(0);
            return !head.equals("Yes") && !head.equals("No");
        }

        /** An unscripted plain wish chooseUse ("Put a card you own from outside
         * the game ...") is declined when the sideboard holds no card.
         * WishEffect's filter form (Coax from the Blind Eternities,
         * alsoFromExile=true) instead says "Reveal ... from outside the game or
         * choose ... in exile"; exile can still hold a candidate, so that
         * wording is never declined here. A queued Yes/No is the generator's
         * own answer and stays for TestPlayer; a non-empty sideboard still asks
         * because the card may be chosen. */
        static boolean unanswerableWishAsk(String message, boolean sideboardEmpty, List<String> choices) {
            // The plain wish ("Put a card you own from outside the game ...")
            // and the filtered one (Burning, Cunning, Glittering, Golden and
            // Living Wish: "Reveal a <filter> card you own from outside the
            // game and put it into your hand?") are the same candidate-free
            // ask over an empty sideboard.
            if (message == null || !sideboardEmpty
                    || !(message.startsWith("Put a card you own from outside the game")
                    || message.startsWith("Reveal ")
                    && message.endsWith(" card you own from outside the game and put it into your hand?"))) {
                return false;
            }
            if (!choices.isEmpty()) {
                String head = choices.get(0);
                if (head.equals("Yes") || head.equals("No")) {
                    return false;
                }
            }
            return true;
        }

        /** An unscripted "Put a <kind> card from your hand onto the battlefield?"
         * chooseUse (Elvish Piper, Terrain Generator, Dragon Arch, Metathran
         * Aerostat) over an EMPTY hand has no candidate and gorge records no
         * decision for it, yet XMage asks the may before it looks at the hand.
         * Decline it rather than strict-miss. A queued Yes/No is the
         * generator's own answer and stays for TestPlayer; a non-empty hand
         * still asks because the filter may admit one of its cards. */
        static boolean unanswerableHandPutAsk(String message, boolean handEmpty, List<String> choices) {
            if (message == null || !handEmpty || !message.startsWith("Put ")
                    || !message.endsWith(" from your hand onto the battlefield?")) {
                return false;
            }
            if (!choices.isEmpty()) {
                String head = choices.get(0);
                if (head.equals("Yes") || head.equals("No")) {
                    return false;
                }
            }
            return true;
        }

        /** Sita Varma, Masked Racer's Exhaust may have "the base power and
         * toughness of each other creature you control become equal to Sita
         * Varma's power". With no other creature it changes nothing either
         * way (CR 608.2d), so gorge records no decision and XMage's
         * SitaVarmaMaskedRacerMayEffect.apply still calls chooseUse. Decline
         * an unscripted ask with no affected creature rather than
         * strict-miss. A queued Yes/No is the generator's own answer and
         * stays for TestPlayer, and another controlled creature keeps the
         * ask because the effect does change the board. */
        static final String EMPTY_ANIMATE_ASK =
                "Have the base power and toughness of each other creature you control";

        static boolean unanswerableEmptyAnimateAsk(String message, boolean otherCreature, List<String> choices) {
            if (message == null || otherCreature || !message.startsWith(EMPTY_ANIMATE_ASK)) {
                return false;
            }
            if (!choices.isEmpty()) {
                String head = choices.get(0);
                if (head.equals("Yes") || head.equals("No")) {
                    return false;
                }
            }
            return true;
        }

        /** Whether the controller of {@code source} controls a creature other
         * than the source permanent itself, which is the ask's "each other
         * creature you control". Walks the active battlefield, so a phased-out
         * permanent (CR 702.26b: treated as though it does not exist) never
         * keeps an empty ask. Unknown state keeps the ask rather than guessing
         * a decline. */
        static boolean hasOtherControlledCreature(Iterable<Permanent> permanents, Game game, Ability source) {
            if (permanents == null || game == null || source == null || source.getControllerId() == null) {
                return true;
            }
            UUID controllerId = source.getControllerId();
            UUID sourceId = source.getSourceId();
            for (Permanent permanent : permanents) {
                if (permanent.isCreature(game)
                        && controllerId.equals(permanent.getControllerId())
                        && (sourceId == null || !sourceId.equals(permanent.getId()))) {
                    return true;
                }
            }
            return false;
        }

        /**
         * The yes/no asks a plain cast step implies. gorge's cast-resolve
         * scenario casts for the mana cost only, so each of these is answered
         * as gorge played it, keyed on the ask's type, not the card:
         * the opening-hand Leyline ask (LeylineAbility) is No; every optional
         * additional cost (kicker, offspring, waterbend: the
         * OptionalAdditionalSourceCosts hook; XMage's mana costs always report
         * canPay, so it asks even when the pool is short) takes the
         * generator's own leading "No" when it scripted one, and on an
         * "optionalcost" cast its leading "Yes" -- gorge elected the cost, so
         * XMage pays it too; an either-or
         * additional cost (OrCost) takes the cost gorge recorded for the step.
         * A yes/no a resolving trigger poses with no Yes/No queued is
         * answered from gorge's folded pick (foldedOptionalAnswer).
         * Every other ask stays with the scripted queue.
         */
        @Override
        public boolean chooseUse(Outcome outcome, String message, String secondMessage, String trueText, String falseText, Ability source, Game game) {
            if (unanswerableWishAsk(message, getSideboard().isEmpty(), getChoices())) {
                // WishEffect asks before it checks the sideboard. With no card
                // outside the game (North Wind Avatar's "you may put a card you
                // own from outside the game into your hand") the prompt has no
                // candidate and gorge records no decision, so decline it rather
                // than let strict mode stop at "Missing CHOICE def".
                return false;
            }
            if (unanswerableHandPutAsk(message, getHand().isEmpty(), getChoices())) {
                return false;
            }
            if (message != null && message.startsWith(EMPTY_ANIMATE_ASK)
                    && unanswerableEmptyAnimateAsk(message,
                            hasOtherControlledCreature(game.getBattlefield().getAllActivePermanents(), game, source),
                            getChoices())) {
                return false;
            }
            if (source == null && isUntapElectionAsk(message)) {
                // The CR 502.3 optional-untap ask: it fires in the untap
                // step's beginStep, a phase with no priority (CR 500.4), so
                // the runCode actions queued at the cut position (turn,
                // UNTAP) -- the pass cut's checkpoint among them -- would
                // never run. Flush them here, before the answer: the
                // snapshot must see the choice still pending, the permanent
                // still tapped, as gorge's cut snapshot does. Then answer:
                // the scripted election when one is queued, else leave
                // tapped, the pending choice's state. A flushed cut
                // checkpoint pauses the game here, the row complete.
                boolean cutCheckpoint = owner.flushUntapAskRunCodes(game);
                String head = getChoices().isEmpty() ? null : getChoices().get(0);
                if ("Untap".equals(head) || "Keep tapped".equals(head)) {
                    // A bound election: gorge recorded the ask's answer as its
                    // own label (choose_n, pick kinds untap/keep_tapped), so
                    // it is the choice head; TestPlayer's Yes/No handling
                    // would never match it. Consume the label and map it.
                    getChoices().remove(0);
                    if (cutCheckpoint) {
                        game.pause();
                    }
                    return head.equals("Untap");
                }
                boolean scripted = scriptedYesNoNext();
                boolean answer = scripted
                        ? super.chooseUse(outcome, message, secondMessage, trueText, falseText, source, game)
                        : false;
                if (cutCheckpoint) {
                    game.pause();
                }
                return answer;
            }
            if (phyrexianStarved(message, getChoices())) {
                return false;
            }
            if (source instanceof LeylineAbility) {
                return false;
            }
            if (owner.copyCostCast != null && askedBy(owner.copyCostCast, "addOptionalAdditionalCosts")) {
                // The step elected this copy cost: pay it (the ask is the
                // keyword's own, so no other optional cost is answered here).
                return true;
            }
            if (owner.kickedCast && askedBy(KickerAbility.class, "addOptionalAdditionalCosts")) {
                // The step elected Kicker: pay it, once. A scripted leading
                // Yes is the same election, so it is consumed (strict mode
                // sees it used) rather than left to misanswer a later ask.
                if (++owner.kickerAsks > 1) {
                    throw new IllegalStateException("cast_mode kicked: a second kicker ask on one cast "
                            + "(multikicker or multi-cost kicker) unsupported");
                }
                if (!getChoices().isEmpty() && getChoices().get(0).equals("Yes")) {
                    return super.chooseUse(outcome, message, secondMessage, trueText, falseText, source, game);
                }
                return true;
            }
            if (askedBy(OptionalAdditionalSourceCosts.class, "addOptionalAdditionalCosts")) {
                switch (optionalAdditionalCostAnswer(owner.bargainedCast,
                        owner.optionalCostCast || owner.giftPromisedCast
                                && askedBy(mage.abilities.keyword.GiftAbility.class, "addOptionalAdditionalCosts"),
                        getChoices())) {
                    case PAY:
                        // The step elected Bargain: pay the cost. This is the
                        // SAME ask every optional additional cost poses
                        // (kicker, offspring, waterbend); only the mode differs.
                        return true;
                    case CONSUME:
                        return super.chooseUse(outcome, message, secondMessage, trueText, falseText, source, game);
                    default:
                        return false;
                }
            }
            if (askedBy(OrCost.class, "pay") && !scriptedYesNoNext()) {
                // The generator scripts the boolean itself when two costs were
                // payable for gorge; with one payable it scripts none, but
                // XMage still asks, so answer the cost gorge recorded.
                Boolean first = owner.recordedCostIsFirst(trueText, falseText);
                if (first != null) {
                    return first;
                }
            }
            if (game.getStep() == null && askedBy(TapSourceUnlessPaysEffect.class, "apply")
                    && !scriptedYesNoNext()) {
                // gorge's setup drive answers an unscripted unless-pay KModes
                // with its first option, which pays (oracleRun.answer; the
                // unless-pay decision has Min 1). The life is re-applied after
                // placement below, exactly as oracleRun.build's post-placement
                // LifeChange does.
                return true;
            }
            if (zonePickAsk(message) && !scriptedYesNoNext()) {
                // A per-zone "Attach an Aura from your hand?" (Bruna, Light of
                // Alabaster) asks a may per pick; gorge recorded ONE yes for the
                // whole effect and the pick itself. Yes when a queued pick lies
                // in the named zone -- the choice queue when the generator
                // scripted it there (choose(Cards, TargetCard) reads it), the
                // target queue when handOffCardPick has still to move it --
                // else no, without consuming either queue.
                return zonePickQueued(message, game);
            }
            if (includeTappedLandsAsk(message) && !scriptedYesNoNext()) {
                // UntapLandsEffect's "Include your tapped lands to untap?"
                // shortcut (CR 608.2d's up-to untap choice). Answering it here
                // keeps the alias off the ask's choice dialog, which refuses
                // them; the effect's own target.choose (makeChoose) accepts
                // them and consumes gorge's picks and skip.
                Boolean answer = untapIncludeAnswer(getChoices(),
                        pick -> untapIncludeNames(game, source, pick, true),
                        pick -> untapIncludeNames(game, source, pick, false));
                if (answer != null) {
                    return answer;
                }
            }
            Boolean folded = foldedOptionalAnswer(source instanceof mage.abilities.TriggeredAbility
                    && askedBy(mage.abilities.TriggeredAbilityImpl.class, "resolve"), getChoices(), getTargets());
            if (folded != null) {
                return folded;
            }
            return super.chooseUse(outcome, message, secondMessage, trueText, falseText, source, game);
        }

        /**
         * XMage asks the controller to order simultaneous replacement effects
         * (CR 616.1: Edgewall Inn's "enters tapped" and "as it enters, choose a
         * color"; Zhao's "nonbasic lands enter tapped" beside Deserted Beach's
         * own). gorge applies them in its own fixed order and poses no decision
         * for it, so the order is never scripted, and TestPlayer's strict mode
         * stops the replay at "Missing CHOICE def" -- or, with a scripted answer
         * for a LATER ask at the head of the queue (the setup colour), logs a
         * wrong choice and fails the same way. An unscripted ask takes the
         * base AI's order, exactly the loose rerun's fallback, WITHOUT touching
         * the queue; a head that names one of the effects is still consumed by
         * the base player.
         */
        @Override
        public int chooseReplacementEffect(Map<String, String> effectsMap,
                Map<String, mage.MageObject> objectsMap, Game game) {
            if (effectsMap.size() > 1 && !replacementScripted(getChoices(), effectsMap.values())) {
                return getRealPlayer().chooseReplacementEffect(effectsMap, objectsMap, game);
            }
            return super.chooseReplacementEffect(effectsMap, objectsMap, game);
        }

        /** Whether the head of the choice queue names one of the replacement
         * effects' texts, by TestPlayer.chooseReplacementEffect's own rule: a
         * text prefix, or "<part>*<part>" with both parts contained. */
        static boolean replacementScripted(List<String> choices, java.util.Collection<String> effectTexts) {
            if (choices.isEmpty()) {
                return false;
            }
            String head = choices.get(0);
            String[] parts = head.split("\\*");
            for (String text : effectTexts) {
                if (text.startsWith(head)
                        || parts.length > 1 && text.contains(parts[0]) && text.contains(parts[1])) {
                    return true;
                }
            }
            return false;
        }

        /** An "alternative" cast's menu: AbilityImpl's alternative-cost key
         * choice ("You may choose an alternative cost", keys "1".. per
         * alternative, then "Cast with no alternative cost"). */
        @Override
        public boolean choose(Outcome outcome, mage.choices.Choice choice, Game game) {
            if (choice.isKeyChoice() && PERMITTING_OBJECT_ASK.equals(choice.getMessage())) {
                java.util.Set<String> grantingObjects = new java.util.HashSet<>();
                for (Permanent permanent : game.getBattlefield().getAllActivePermanents()) {
                    grantingObjects.add(permanent.getIdName());
                }
                java.util.Set<String> regularCasts = new java.util.HashSet<>();
                for (Card card : getHand().getCards(game)) {
                    regularCasts.add(card.getIdName());
                }
                permittingObjectKey(choice, getChoices(), owner.alternativeCostCast,
                        grantingObjects, regularCasts);
            }
            if (owner.alternativeCostCast && choice.isKeyChoice()) {
                alternativeCostKey(choice.getMessage(), getChoices());
            }
            if (owner.evokedCast && choice.isKeyChoice()) {
                evokeCostKey(choice, getChoices());
            }
            if (convokeColour != null && choice.getMessage() != null
                    && choice.getMessage().startsWith("Choose mana color to reduce from ")) {
                getChoices().add(0, convokeColour);
                convokeColour = null;
            }
            numberChoiceAnswer(choice, getChoices());
            return super.choose(outcome, choice, game);
        }

        /** A "choose a number" pick is queued as "X=<n>", the answer XMage's
         * getAmount reads. Some cards ask the number through a Choice dialog
         * instead (Shapeshifter's ShapeshifterEffect: a ChoiceImpl keyed or
         * labelled "0".."7"), where "X=<n>" names no entry. Rewrite the head
         * answer to "<n>" only when the dialog offers exactly "<n>" (as a
         * key, a key's text, or a plain choice) and does not offer "X=<n>".
         * Returns whether it rewrote the answer. */
        static boolean numberChoiceAnswer(mage.choices.Choice choice, List<String> choices) {
            if (choices.isEmpty() || !choices.get(0).matches("X=-?[0-9]+")) {
                return false;
            }
            String head = choices.get(0);
            String number = head.substring(2);
            java.util.Collection<String> offered = new ArrayList<>();
            if (choice.isKeyChoice()) {
                offered.addAll(choice.getKeyChoices().keySet());
                offered.addAll(choice.getKeyChoices().values());
            } else if (choice.getChoices() != null) {
                offered.addAll(choice.getChoices());
            }
            if (offered.contains(head) || !offered.contains(number)) {
                return false;
            }
            choices.set(0, number);
            return true;
        }

        /** PlayerImpl.chooseApprovingObject's menu title. XMage asks it when a
         * spell in its owner's hand has both the regular-cast ApprovingObject
         * SpellAbility.canActivate adds and one or more AsThough play-from-hand
         * grants (Zaffai and the Tempests, One with the Multiverse, ...). */
        static final String PERMITTING_OBJECT_ASK = "Choose the permitting object";

        /** The suffix PlayerImpl.chooseApprovingObject uses for an approver
         * whose rule text is empty. It is only a fallback marker here: a plain
         * cast of a spell with rules text shows that text instead (the find's
         * menu offered "Reach Through Mists [d91]: draw a card."), so the entry
         * cannot be identified by this suffix alone. */
        static final String CAST_NORMALLY = "Cast normally";

        /**
         * Answers PlayerImpl.chooseApprovingObject's key menu. The entries are
         * a HashMap, so the key numbers are not stable; the driver picks by the
         * entry's role, not by key. Each value names its approving ability's
         * source: the regular-cast approver's source is the spell card (still
         * in the caster's hand at this ask), while a hand-zone grant's source
         * is the permanent granting it. The caller passes those two id-name
         * sets; an "alternative" cast (gorge elected the permission) takes the
         * granting entry, every other cast takes the regular one so a paid cast
         * is not silently made free. When only one side matches -- e.g. an
         * Adventure half whose source is not the hand's parent card -- the
         * other entry is the answer. The CAST_NORMALLY suffix is the last
         * resort for a menu whose sources are in neither zone. Returns whether
         * it queued a key.
         */
        static boolean permittingObjectKey(mage.choices.Choice choice, List<String> choices,
                boolean grantCast, java.util.Set<String> grantingObjects,
                java.util.Set<String> regularCasts) {
            if (!choice.isKeyChoice() || choice.getMessage() == null
                    || !PERMITTING_OBJECT_ASK.equals(choice.getMessage())) {
                return false;
            }
            String grantKey = null;
            String regularKey = null;
            int grantMatches = 0;
            int regularMatches = 0;
            for (java.util.Map.Entry<String, String> e : choice.getKeyChoices().entrySet()) {
                String text = e.getValue() == null ? ""
                        : e.getValue().replaceAll("<[^<>]*>", "");
                if (containsAny(text, grantingObjects)) {
                    grantKey = e.getKey();
                    grantMatches++;
                }
                if (containsAny(text, regularCasts)) {
                    regularKey = e.getKey();
                    regularMatches++;
                }
            }
            String key = null;
            if (grantCast && grantMatches == 1) {
                key = grantKey;
            } else if (!grantCast && regularMatches == 1) {
                key = regularKey;
            } else if (grantCast && regularMatches == 1) {
                key = keyOtherThan(choice, regularKey);
            } else if (!grantCast && grantMatches == 1) {
                key = keyOtherThan(choice, grantKey);
            }
            if (key == null) {
                for (java.util.Map.Entry<String, String> e : choice.getKeyChoices().entrySet()) {
                    String text = e.getValue() == null ? ""
                            : e.getValue().replaceAll("<[^<>]*>", "");
                    if (text.endsWith(CAST_NORMALLY) == !grantCast) {
                        key = e.getKey();
                        break;
                    }
                }
            }
            if (key == null) {
                return false;
            }
            queueKeyAhead(choices, key);
            return true;
        }

        private static boolean containsAny(String text, java.util.Set<String> markers) {
            for (String marker : markers) {
                if (marker != null && !marker.isEmpty() && text.contains(marker)) {
                    return true;
                }
            }
            return false;
        }

        /** The other key of a two-entry menu, or null for any other shape. */
        private static String keyOtherThan(mage.choices.Choice choice, String excluded) {
            if (choice.getKeyChoices().size() != 2) {
                return null;
            }
            for (String key : choice.getKeyChoices().keySet()) {
                if (!key.equals(excluded)) {
                    return key;
                }
            }
            return null;
        }

        /** Queues a key election at the head of the scripted answers without
         * overwriting a real answer already there (a later ask on the same
         * cast). A head that already names the key is left to TestPlayer. */
        private static void queueKeyAhead(List<String> choices, String key) {
            if (choices.isEmpty() || !choices.get(0).equals(key)) {
                choices.add(0, key);
            }
        }

        /** Elects the first alternative cost (gorge's AltCostIndex-1 choice)
         * when the menu is XMage's alternative-cost key choice. The item's
         * cast_mode is the election, so key "1" is queued even when the
         * generator queued no answer (a plain alternative cast on the card
         * itself, e.g. Land Grant); a generator that queued "Yes" gets the
         * same key. Returns whether the menu was this one. */
        static boolean alternativeCostKey(String message, List<String> choices) {
            return electAlternativeKey(message, choices, "1");
        }

        /** AlternativeSourceCostsImpl.getAlternativeCostText's prefix for
         * EvokeAbility: "Cast with " + name + " alternative cost: ...". */
        static final String EVOKE_COST_TEXT = "Cast with Evoke alternative cost";

        /** Elects the Evoke alternative cost (CR 702.74a) on XMage's
         * alternative-cost key menu: the key whose text begins
         * EVOKE_COST_TEXT (EvokeAbility's getAlternativeCostText), not key 1,
         * so a card that offers another alternative first still elects evoke. A menu with no
         * Evoke entry is left to the strict-choose failure. Returns whether it
         * queued the election. */
        static boolean evokeCostKey(mage.choices.Choice choice, List<String> choices) {
            if (!choice.isKeyChoice()) {
                return false;
            }
            for (java.util.Map.Entry<String, String> e : choice.getKeyChoices().entrySet()) {
                String text = e.getValue() == null ? "" : e.getValue().replaceAll("<[^<>]*>", "");
                if (text.startsWith(EVOKE_COST_TEXT)) {
                    return electAlternativeKey(choice.getMessage(), choices, e.getKey());
                }
            }
            return false;
        }

        private static boolean electAlternativeKey(String message, List<String> choices, String key) {
            if (message == null || !message.toLowerCase(java.util.Locale.ROOT)
                    .endsWith("choose an alternative cost")) {
                return false;
            }
            if (choices.isEmpty()) {
                choices.add(key);
            } else {
                // Replace only the election itself: a scripted "Yes", or the
                // plain cast's "Cast with no alternative cost" decline queued
                // above. Any other head is a real answer for a later ask on
                // the same cast (Mine Collapse's sacrifice pick); overwriting
                // it left that answer to be consumed by the cost's target ask.
                String head = choices.get(0);
                if (head.equals("Yes") || head.equals("Cast with no alternative cost")) {
                    choices.set(0, key);
                } else {
                    choices.add(0, key);
                }
            }
            return true;
        }

        /**
         * RevealAndSeparatePilesEffect (Steam Augury, Fact or Fiction) asks the
         * chooser Player.choosePile, not chooseUse, and TestPlayer.choosePile
         * hands that to the AI without reading the choice queue, so the
         * generator's scripted pile answer was left over. A leading Yes/No is
         * the answer: Yes puts pile1 (the separator's chosen cards) in hand.
         * With nothing scripted the AI still chooses.
         */
        @Override
        public boolean choosePile(Outcome outcome, String message, List<? extends Card> pile1,
                List<? extends Card> pile2, Game game) {
            Boolean scripted = scriptedPileAnswer(getChoices());
            if (scripted != null) {
                return scripted;
            }
            return super.choosePile(outcome, message, pile1, pile2, game);
        }

        /** Consumes a leading Yes/No from the choice queue as a pile pick;
         * null (queue untouched) when the head is anything else. */
        static Boolean scriptedPileAnswer(List<String> choices) {
            if (choices.isEmpty()) {
                return null;
            }
            String head = choices.get(0);
            if (!head.equals("Yes") && !head.equals("No")) {
                return null;
            }
            choices.remove(0);
            return head.equals("Yes");
        }

        @Override
        public ScriptedChoicePlayer copy() {
            return new ScriptedChoicePlayer(this);
        }

        // XMage declares attackers/blockers in beginStep, before priority, so
        // this is the only point where a recorded token ref can be resolved
        // from the live battlefield and declared exactly instead of by name.
        @Override
        public void selectAttackers(Game game, UUID attackingPlayerId) {
            int occurrence = currentOccurrence(game);
            List<PlayerAction> hidden = hideOtherOccurrences(occurrence);
            try {
                owner.applyExactAttackers(this, game);
                dropAutoDeclaredAttacks(game);
                super.selectAttackers(game, attackingPlayerId);
            } finally {
                getActions().addAll(hidden);
            }
        }

        @Override
        public void selectBlockers(Ability source, Game game, UUID defendingPlayerId) {
            int occurrence = currentOccurrence(game);
            List<PlayerAction> hidden = hideOtherOccurrences(occurrence);
            try {
                owner.applyExactBlockers(this, game);
                dropAutoDeclaredBlocks(game);
                super.selectBlockers(source, game, defendingPlayerId);
            } finally {
                getActions().addAll(hidden);
            }
        }

        /** The attacker twin of dropAutoDeclaredBlocks. XMage's
         * Combat.selectAttackers runs checkAttackRequirements before it asks
         * this player (CR 508.1d), so a creature forced to attack -- by its
         * own static or by another permanent's, such as Goblin Rabblemaster's
         * "Other Goblin creatures you control attack each combat if able" --
         * is already declared and tapped, and TestPlayer cannot consume its
         * queued attack command. gorge records the same attacker, so drop, per
         * name, as many of this turn's queued attack commands as this player
         * has attacking permanents of that name. When every attacker is
         * forced, getAvailableAttackers is empty and selectAttackers never
         * runs; priority in the declare-attackers step calls this then. A
         * command for a creature XMage did not declare stays queued. */
        private void dropAutoDeclaredAttacks(Game game) {
            if (!getId().equals(game.getCombat().getAttackingPlayerId())
                    || game.getCombat().getAttackers().isEmpty()) {
                return;
            }
            Map<String, Integer> attacking = new HashMap<>();
            for (UUID id : game.getCombat().getAttackers()) {
                Permanent p = game.getBattlefield().getPermanent(id);
                if (p != null && getId().equals(p.getControllerId())) {
                    attacking.merge(p.getName(), 1, Integer::sum);
                }
            }
            for (PlayerAction action : new ArrayList<>(getActions())) {
                if (action.getTurnNum() != game.getTurnNum() || !action.getAction().startsWith("attack:")) {
                    continue;
                }
                String command = action.getAction().substring("attack:".length());
                if (command.startsWith(TestPlayer.ATTACK_SKIP)) {
                    continue;
                }
                String name = combatBaseName(command.split("\\$")[0]);
                Integer left = attacking.get(name);
                if (left == null || left == 0) {
                    continue;
                }
                attacking.put(name, left - 1);
                getActions().remove(action);
            }
        }

        /** XMage's combat declares a "blocks this turn if able" blocker itself
         * before it asks this player, and TestPlayer.selectBlockers throws
         * "already blocking the maximum amount of creatures" for a queued block
         * command on top of such a block. gorge records the same pair (the
         * requirement forces it, CR 509.1c), so drop each queued block command
         * of this turn whose blocker already blocks that attacker. A pair XMage
         * did not declare stays queued and keeps TestPlayer's own error. */
        private void dropAutoDeclaredBlocks(Game game) {
            UUID attackingPlayer = game.getCombat().getAttackingPlayerId();
            if (attackingPlayer == null) {
                return;
            }
            for (PlayerAction action : new ArrayList<>(getActions())) {
                if (action.getTurnNum() != game.getTurnNum() || !action.getAction().startsWith("block:")) {
                    continue;
                }
                String command = action.getAction().substring("block:".length());
                if (command.startsWith(TestPlayer.BLOCK_SKIP)) {
                    continue;
                }
                String[] groups = command.split("\\$");
                if (groups.length != 2) {
                    continue;
                }
                Permanent blocker = namedCombatPermanent(groups[0], getId(), null, game);
                Permanent attacker = namedCombatPermanent(groups[1], attackingPlayer, game.getCombat().getAttackers(), game);
                if (blocker == null || attacker == null) {
                    continue;
                }
                mage.game.combat.CombatGroup group = game.getCombat().findGroup(attacker.getId());
                if (group != null && group.getBlockers().contains(blocker.getId())) {
                    getActions().remove(action);
                }
            }
        }

        /** TestPlayer's name command ("Name" or "Name:k", k the zero-based
         * index among the controller's matches in battlefield order) resolved
         * against the live battlefield; attackers only, when `attacking` is
         * given. Null when nothing matches. */
        private static Permanent namedCombatPermanent(String command, UUID controller,
                java.util.Collection<UUID> attacking, Game game) {
            String name = combatBaseName(command);
            int index = combatIndex(command);
            mage.filter.predicate.mageobject.NamePredicate matches =
                    new mage.filter.predicate.mageobject.NamePredicate(name, true);
            int seen = 0;
            for (Permanent permanent : game.getBattlefield().getAllPermanents()) {
                if (!controller.equals(permanent.getControllerId()) || !permanent.isPhasedIn()
                        || (attacking != null && !attacking.contains(permanent.getId()))
                        || !matches.apply(permanent, game)) {
                    continue;
                }
                if (seen++ == index) {
                    return permanent;
                }
            }
            return null;
        }

        // Tokens may be created after scripted() queues an answer. Resolve a
        // token ref against the LIVE choice game, not a prematurely bound or
        // stale alias. Other choice values and the target path stay unchanged.
        private Game choiceGame;
        // The target ask in progress (choose / chooseTarget), so the face-down
        // match can tell which face-up permanents are legal candidates of it.
        private mage.target.Target askTarget;
        private Ability askSource;

        /** Also: the paid additional cost's sacrifice is posed as a target-group
         * `choose`, which reads XMage's CHOICE queue, not the target queue
         * (TargetSacrifice.pay -> TargetImpl.choose -> Player.choose ->
         * makeChoose over getChoices()). Push this cast's recorded pick so
         * makeChoose consumes it and strict mode records the decision, rather
         * than letting XMage's AI auto-choose a legal object. */
        @Override
        public boolean choose(Outcome outcome, mage.target.Target target, Ability source, Game game) {
            Game previous = choiceGame;
            mage.target.Target previousTarget = askTarget;
            Ability previousSource = askSource;
            choiceGame = game;
            askTarget = target;
            askSource = source;
            try {
                if (owner.giftPromisedCast && target.getOriginalTarget() instanceof mage.target.common.TargetOpponent) {
                    List<String> opponents = new ArrayList<>();
                    for (UUID id : target.possibleTargets(target.getAffectedAbilityControllerId(this.getId()), source, game)) {
                        Player opp = game.getPlayer(id);
                        if (opp != null) {
                            opponents.add(opp.getName());
                        }
                    }
                    String pick = giftOpponentChoice(askedByName("PromiseGiftCost", "pay"), opponents, getChoices());
                    if (pick != null) {
                        getChoices().add(0, pick);
                    }
                }
                if (owner.copyCostCast == mage.abilities.keyword.TeamworkAbility.class
                        && askedByName("TeamworkCost", "pay") && getChoices().isEmpty()) {
                    // A "teamworked" cast's tap (TeamworkCost: any number of
                    // untapped creatures you control, an open-ended
                    // TargetPermanent(0, MAX)): gorge's forced one-option
                    // election is not scripted, so name the creatures the
                    // step's choose answer recorded, then close the ask when
                    // other candidates remain.
                    UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                    List<UUID> candidates = new ArrayList<>(target.possibleTargets(abilityControllerId, source, game));
                    getChoices().addAll(teamworkTapChoices(owner.castCostPicks, candidates,
                            (id, answer) -> hasObjectTargetNameOrAlias(game.getPermanent(id), answer)));
                }
                // A copy-cost cast's sacrifice (casualty) is scripted by the
                // generator from gorge's own ask ("@ref" on the choice queue);
                // pushing the step's pick too would leave one unconsumed.
                if (isSacrificeChoice(target) && owner.copyCostCast == null) {
                    UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                    List<UUID> candidates = new ArrayList<>(target.possibleTargets(abilityControllerId, source, game));
                    for (String pick : owner.castCostPicks) {
                        if (getChoices().contains(pick)) {
                            continue;
                        }
                        // Queue only a pick that names a legal sacrifice; an
                        // unmatched pick is left out, so makeChoose's strict
                        // "invalid target" failure names it rather than silently
                        // paying the cost with some other object.
                        if (costPickChoice(java.util.Collections.singletonList(pick), candidates,
                                (id, answer) -> hasObjectTargetNameOrAlias(game.getPermanent(id), answer)) != null) {
                            addChoice(pick);
                        }
                    }
                }
                if (target.getMinNumberOfTargets() == 0 && !getChoices().isEmpty()) {
                    UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                    List<UUID> candidates = new ArrayList<>(target.possibleTargets(abilityControllerId, source, game));
                    candidates.removeIf(target::contains);
                    Boolean may = mayPrefix(getChoices(), getTargets(), !candidates.isEmpty(), answer -> {
                        for (UUID id : candidates) {
                            mage.MageObject o = game.getObject(id);
                            if (o != null && hasObjectTargetNameOrAlias(o, answer)) {
                                return true;
                            }
                        }
                        return false;
                    });
                    if (Boolean.FALSE.equals(may)) {
                        return false;
                    }
                }
                if (target.getMinNumberOfTargets() > 0 && !getTargets().isEmpty()
                        && target.getOriginalTarget() instanceof mage.target.TargetPermanent
                        && (source == null || !source.getTargets().contains(target))
                        && (source == null || !source.getTargets().contains(target.getOriginalTarget()))) {
                    // A bespoke non-targeting pick (Wayward Guide-Beast's "return
                    // a land you control to its owner's hand") is posed through
                    // choose, which reads the CHOICE queue, while the generator
                    // scripted it for the chooseTarget twin on the TARGET queue.
                    UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                    List<UUID> candidates = new ArrayList<>(target.possibleTargets(abilityControllerId, source, game));
                    candidates.removeIf(target::contains);
                    movePickToChoices(getChoices(), getTargets(), !candidates.isEmpty(), answer -> {
                        for (UUID id : candidates) {
                            if (answerNamesCandidate(game, id, answer)) {
                                return true;
                            }
                        }
                        return false;
                    });
                }
                if (forcedChoose(target, source, game)) {
                    return true;
                }
                return super.choose(outcome, target, source, game);
            } finally {
                choiceGame = previous;
                askTarget = previousTarget;
                askSource = previousSource;
            }
        }

        /**
         * The answer to UntapLandsEffect's "Include your tapped lands to
         * untap?" shortcut, or null when a scripted Yes/No heads the choice
         * queue and TestPlayer must consume it itself. TRUE takes the shortcut
         * (untap every tapped land, no target ask); FALSE leaves the target
         * ask to read the choice queue, which is where gorge's up-to picks and
         * its closing [choice_skip] sit.
         * <p>
         * The shortcut is declined whenever the queue already holds this ask's
         * answer: a [choice_skip], a pick naming a tapped land of the
         * controller's (a real untap), or -- after pruning it -- a pick naming
         * a land that is not a candidate (already untapped, or not the
         * controller's). Such a pick is a no-op, never an invitation to untap
         * something else (the Peregrine Drake rule), so it is consumed here and
         * the ask is closed with a skip. With no pick queued for the ask the
         * shortcut is the rules' only outcome and is taken, leaving the queue
         * untouched for later asks.
         */
        static Boolean untapIncludeAnswer(List<String> choices,
                Predicate<String> namesTappedLand, Predicate<String> namesLand) {
            if (choices.isEmpty()) {
                return Boolean.TRUE;
            }
            String head = choices.get(0);
            if (head.equals("Yes") || head.equals("No")) {
                return null;
            }
            boolean prunedNoop = false;
            while (!choices.isEmpty()) {
                String pick = choices.get(0);
                if (TestPlayer.CHOICE_SKIP.equals(pick) || namesTappedLand.test(pick)) {
                    return Boolean.FALSE;
                }
                if (!namesLand.test(pick)) {
                    break;
                }
                // A land the effect cannot untap: gorge recorded the no-op
                // pick, so consume it instead of letting the target ask fail
                // on a non-candidate or the shortcut untap other lands.
                choices.remove(0);
                prunedNoop = true;
            }
            if (prunedNoop) {
                choices.add(0, TestPlayer.CHOICE_SKIP);
                return Boolean.FALSE;
            }
            return Boolean.TRUE;
        }

        /**
         * A "you may" that XMage poses as ONE up-to-one choose, with no
         * chooseUse before it (Ambrosia Whiteheart's "you may return another
         * permanent you control", Spelunking's "you may put a land card from
         * your hand onto the battlefield"), while gorge asks the may and then
         * the pick and the generator scripts the may as a leading "Yes"/"No"
         * on the choice queue. makeChoose would pop it as a target name. For
         * an ask with candidates whose queue head is a boolean: "No" is taken
         * (and the declined pair's "[target_skip]" with it) and the pick is
         * declined (FALSE); "Yes" is taken and the pick follows (TRUE) -- on
         * the choice queue already, or moved there from the target queue when
         * the choice head names no candidate. Null means the ask is no such
         * may and the queues are untouched.
         */
        static Boolean mayPrefix(List<String> choices, List<String> targets, boolean hasCandidates,
                java.util.function.Predicate<String> namesCandidate) {
            if (!hasCandidates || choices.isEmpty()) {
                return null;
            }
            String head = choices.get(0);
            if (head.equals("No")) {
                choices.remove(0);
                if (!targets.isEmpty() && TestPlayer.TARGET_SKIP.equals(targets.get(0))) {
                    targets.remove(0);
                }
                return Boolean.FALSE;
            }
            if (!head.equals("Yes")) {
                return null;
            }
            choices.remove(0);
            boolean pickOnChoices = !choices.isEmpty()
                    && (TestPlayer.CHOICE_SKIP.equals(choices.get(0)) || namesCandidate.test(choices.get(0)));
            if (!pickOnChoices && !targets.isEmpty() && !TestPlayer.TARGET_SKIP.equals(targets.get(0))) {
                choices.add(0, targets.remove(0));
            }
            return Boolean.TRUE;
        }

        /**
         * A mandatory choose ask whose answer sits on the TARGET queue: when the
         * choice queue's head names no candidate (and is no skip) and the target
         * queue's head names one, that answer moves to the front of the choice
         * queue, where makeChoose reads it. Returns whether it moved. Matched by
         * the candidates, never by card name.
         */
        static boolean movePickToChoices(List<String> choices, List<String> targets, boolean hasCandidates,
                java.util.function.Predicate<String> namesCandidate) {
            if (!hasCandidates || targets.isEmpty() || TestPlayer.TARGET_SKIP.equals(targets.get(0))
                    || !namesCandidate.test(targets.get(0))) {
                return false;
            }
            if (!choices.isEmpty() && (TestPlayer.CHOICE_SKIP.equals(choices.get(0)) || namesCandidate.test(choices.get(0)))) {
                return false;
            }
            choices.add(0, targets.remove(0));
            return true;
        }

        /** Takes every candidate of a choose ask that has no choice in it:
         * each legal object must be chosen (at most as many candidates as
         * picks still needed), so the outcome is the rules' alone. gorge
         * records no decision for such an ask (Abyssal Gorestalker's "each
         * player sacrifices two creatures" over its controller's lone
         * creature, Wick's "a Snail you control" with one Snail, the one
         * sacrificeable object behind an accepted "you may sacrifice"), so
         * nothing is scripted and strict mode stopped at "Missing CHOICE
         * def". A queue head that names a candidate is this ask's own
         * answer and is left to the base player. */
        private boolean forcedChoose(mage.target.Target target, Ability source, Game game) {
            UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
            List<UUID> candidates = new ArrayList<>();
            for (UUID id : target.possibleTargets(abilityControllerId, source, game)) {
                if (!target.contains(id)) {
                    candidates.add(id);
                }
            }
            String head = getChoices().isEmpty() ? null : getChoices().get(0);
            boolean headNamesCandidate = false;
            if (head != null) {
                for (UUID id : candidates) {
                    if (answerNamesCandidate(game, id, head)) {
                        headNamesCandidate = true;
                        break;
                    }
                    // A player candidate is no MageObject: a TargetOpponent's
                    // lone opponent (ClashEffect's "an opponent to clash
                    // with") is named by the player's own name, the answer
                    // the generator scripted for the controller's pick.
                    Player candidate = game.getPlayer(id);
                    if (candidate != null && candidate.getName().equals(head)) {
                        headNamesCandidate = true;
                        break;
                    }
                }
            }
            int needed = target.getMinNumberOfTargets() - target.getSize();
            // The whole replay's answers share one FIFO queue, built before
            // any step runs, so a head that names the lone candidate may be a
            // LATER step's pick (the discard cost of Borborygmos Enraged's
            // first activation, a one-card hand, taking the Mountain scripted
            // for its third). A lone-candidate discard cost is never scripted:
            // gorge records no decision for it.
            boolean headIsOurs = headNamesCandidate
                    && !loneDiscardCostCandidate(askedByName("DiscardTargetCost", "pay"), needed, candidates.size());
            if (!forcedPick(needed, candidates.size(), head, headIsOurs)) {
                return false;
            }
            for (UUID id : candidates) {
                target.add(id, game);
            }
            return true;
        }

        /** Whether a choose ask is a discard COST's one forced card: the cost
         * (DiscardTargetCost.pay) still needs a card and exactly one is
         * legal. gorge drops that ask as a forced one-option pick, so no
         * answer is scripted for it and a queue head naming the card belongs
         * to a later ask. */
        static boolean loneDiscardCostCandidate(boolean discardCost, int needed, int candidates) {
            return discardCost && needed > 0 && candidates == 1;
        }

        /** Whether a choose ask is forced: it still needs picks, every
         * candidate is needed to meet them, and the choice queue's head is not
         * an answer for it (a candidate's name, or a skip). */
        static boolean forcedPick(int needed, int candidates, String head, boolean headNamesCandidate) {
            return needed > 0 && candidates > 0 && candidates <= needed && !headNamesCandidate
                    && !TestPlayer.CHOICE_SKIP.equals(head);
        }

        /** Whether a queued answer names this candidate: the target queue
         * spells a player "targetPlayer=<name>", which is not an object name,
         * and every other candidate is matched by name or setup alias. Shared
         * by the choose and chooseTarget forced-pick paths so a player
         * candidate cannot be missed by one of them. */
        /** The picks of one queued answer: TestPlayer's "^" join splits into
         * its segments, in order; any other answer is its own one segment. */
        static List<String> answerSegments(String answer) {
            List<String> parts = new ArrayList<>();
            for (String part : answer.split("\\^")) {
                if (!part.isEmpty()) {
                    parts.add(part);
                }
            }
            return parts;
        }

        /** Whether any "^" segment of a multi-pick answer names {@code id} by
         * {@code namesSegment}. TestPlayer reads the choice queue as its
         * segments ("Plains^Plains", "@p1:Plains#1^@p1:Plains#2"); comparing
         * the whole string missed the ask's own scripted answer and left it
         * queued while the forced fall-through took the cards. */
        static boolean answerNamesCandidate(String answer, UUID id, BiPredicate<UUID, String> namesSegment) {
            if (answer == null) {
                return false;
            }
            for (String part : answerSegments(answer)) {
                if (namesSegment.test(id, part)) {
                    return true;
                }
            }
            return false;
        }

        private boolean answerNamesCandidate(Game game, UUID id, String answer) {
            if (answer == null) {
                return false;
            }
            if (answer.contains("^")) {
                // A multi-pick answer ("Ornithopter^Sol Ring", TestPlayer's
                // "^" join) names a candidate when any segment does. Missing
                // it made a forced ask take every candidate in XMage's
                // possibleTargets (HashSet) order, so Rottenmouth Viper's two
                // sacrificed permanents reached the graveyard in a random
                // order and the scripted pick was left unused.
                return answerNamesCandidate(answer, id,
                        (candidate, part) -> answerNamesCandidate(game, candidate, part));
            }
            if (answer.startsWith("targetPlayer=")) {
                Player player = game.getPlayer(id);
                return player != null && answer.equals("targetPlayer=" + player.getName());
            }
            return hasObjectTargetNameOrAlias(game.getObject(id), answer);
        }

        /** Whether a front skip can close this ask: only an "up to" ask whose
         * minimum is already met can stop early. A skip can never close a
         * mandatory ask (max == min); there it belongs to a later ask in the
         * same step and must stay queued (TestPlayer.tryToSkipSelection fails
         * "Wrong skip command found" on a max == min target). */
        static boolean skipCanCloseAsk(mage.target.Target target) {
            return target.getMaxNumberOfTargets() > target.getMinNumberOfTargets()
                    && target.getSize() >= target.getMinNumberOfTargets();
        }

        /** The candidate an unscripted target ask should take when the rules
         * leave no choice, or null when the base player must answer. gorge
         * settles a lone legal candidate without recording a decision, so no
         * answer is scripted and strict mode otherwise fails "Missing TARGET
         * def" (Black Cat, Cunning Thief's lone opponent; Tishana's Tidebinder
         * countering the only ability on the stack). This matches by role --
         * one legal candidate and no queued answer of this ask's own -- in the
         * three funnels {@link #unscriptedLoneTarget} names. A queued entry
         * naming the candidate, at any index (the base scans the whole
         * queue), is this ask's own answer and is left to the base. A front skip is this
         * ask's own only when {@link #skipCanCloseAsk} says it can close the
         * ask (an "up to" ask whose minimum is met); otherwise it is a later
         * ask's skip and stays queued while the lone candidate is taken. */
        static UUID loneTargetChoice(List<String> targetQueue, List<UUID> candidates,
                boolean skipCanCloseAsk, BiPredicate<UUID, String> matches) {
            // TestPlayer.chooseTarget scans the whole visible queue
            // (takeMaxTargetsPerChoose), not only its head: an entry naming
            // the lone candidate anywhere is this ask's own answer, so the
            // base consumes it rather than leaving it queued for a later ask.
            return candidates.size() == 1 && unscriptedChoice(targetQueue, candidates, skipCanCloseAsk, matches)
                    ? candidates.get(0) : null;
        }

        /** Whether an ask reads nothing from {@code targetQueue}: the ask has
         * a candidate, no queued entry names any candidate, and no front skip
         * can close the ask. The shared rule behind the lone-candidate and the
         * divided-amount inference; the caller passes the part of the queue
         * its base funnel reads. */
        static boolean unscriptedChoice(List<String> targetQueue, List<UUID> candidates,
                boolean skipCanCloseAsk, BiPredicate<UUID, String> matches) {
            if (candidates.isEmpty()) {
                return false;
            }
            if (!targetQueue.isEmpty()
                    && TestPlayer.TARGET_SKIP.equals(targetQueue.get(0)) && skipCanCloseAsk) {
                return false;
            }
            for (String answer : targetQueue) {
                for (UUID id : candidates) {
                    if (matches.test(id, answer)) {
                        return false;
                    }
                }
            }
            return true;
        }

        /** The candidates of a target ask that it has not already chosen. */
        static List<UUID> unchosenCandidates(mage.target.Target target, java.util.Collection<UUID> possible) {
            List<UUID> candidates = new ArrayList<>();
            for (UUID id : possible) {
                if (!target.contains(id)) {
                    candidates.add(id);
                }
            }
            return candidates;
        }

        /** The lone candidate an unscripted target ask should take, or null:
         * the one rule every target-ask entry point applies (the plain Target
         * funnel, the Cards/TargetCard funnel and TargetAmount). */
        UUID unscriptedLoneTarget(mage.target.Target target, java.util.Collection<UUID> possible, Game game) {
            return loneTargetChoice(getTargets(), unchosenCandidates(target, possible),
                    skipCanCloseAsk(target), (id, answer) -> answerNamesCandidate(game, id, answer));
        }

        /** As unscriptedLoneTarget, but for a divided target whose queue
         * spells an answer "name^X=n": match the name part. */
        UUID unscriptedLoneAmountTarget(mage.target.TargetAmount target,
                java.util.Collection<UUID> possible, Game game) {
            return loneTargetChoice(getTargets(), unchosenCandidates(target, possible),
                    skipCanCloseAsk(target), (id, answer) -> {
                        if (answer == null) {
                            return false;
                        }
                        int amount = answer.indexOf("^X=");
                        return answerNamesCandidate(game, id, amount >= 0 ? answer.substring(0, amount) : answer);
                    });
        }

        /** The shares an unscripted DIVIDED amount takes from {@code candidates}
         * (in the caller's, stable, order), or null when the ask must go to
         * the base. gorge records a free division (CR 601.2d) as a decision,
         * but a lower item can carry no decision log (Ureni, the Song
         * Unending's ETB after a pass), so the driver uses the same
         * arithmetic as the generator's fallback for a divided ask with no
         * recorded split: the total dealt as evenly as possible, the
         * remainder first. It applies only to a real division, more than one
         * recipient: a one-recipient ask with several candidates is a target
         * CHOICE the replay cannot make. Each recipient takes at least one
         * point and the ask's remaining slots cap the recipients (CR 601.2d,
         * "any number"). TestPlayer.chooseTargetAmount reads only the queue's
         * head, so only the head can be this ask's own "name^X=n" answer or
         * skip. */
        int[] unscriptedAmountShares(mage.target.TargetAmount target, List<UUID> candidates, Game game) {
            int slots = target.getMaxNumberOfTargets() - target.getSize();
            int recipients = Math.min(Math.min(target.getAmountRemaining(), candidates.size()), slots);
            if (recipients < 2) {
                return null;
            }
            List<String> head = getTargets().subList(0, Math.min(1, getTargets().size()));
            if (!unscriptedChoice(head, candidates, skipCanCloseAsk(target), (id, answer) -> {
                int at = answer == null ? -1 : answer.indexOf("^X=");
                return answerNamesCandidate(game, id, at >= 0 ? answer.substring(0, at) : answer);
            })) {
                return null;
            }
            return evenShares(target.getAmountRemaining(), recipients);
        }

        /** Orders the candidates of an inferred division so the remainder and
         * the recipients cannot vary between replays: XMage's TargetAmount
         * collects possibleTargets into a HashSet, so its iteration order is
         * not stable. Battlefield permanents order by the game's creation
         * order, players after them by name, and any other object by name.
         * Ids only break a tie between objects the replay cannot otherwise
         * tell apart. */
        static List<UUID> stableAmountCandidates(java.util.Collection<UUID> candidates, Game game) {
            List<UUID> ordered = new ArrayList<>(candidates);
            ordered.sort((a, b) -> {
                boolean permanentA = game.getPermanent(a) != null;
                boolean permanentB = game.getPermanent(b) != null;
                if (permanentA != permanentB) {
                    return permanentA ? -1 : 1;
                }
                if (permanentA) {
                    int byOrder = Integer.compare(game.getPermanent(a).getCreateOrder(),
                            game.getPermanent(b).getCreateOrder());
                    if (byOrder != 0) {
                        return byOrder;
                    }
                }
                int byName = amountCandidateName(game, a).compareTo(amountCandidateName(game, b));
                return byName != 0 ? byName : a.toString().compareTo(b.toString());
            });
            return ordered;
        }

        /** A division candidate's stable display name (a player's name, else
         * the object's). */
        private static String amountCandidateName(Game game, UUID id) {
            Player player = game.getPlayer(id);
            if (player != null) {
                return player.getName();
            }
            mage.MageObject object = game.getObject(id);
            return object == null ? "" : object.getName();
        }

        /** {@code remaining} points split over at most {@code recipients}:
         * the quotient to each and one more to the first remainder
         * recipients, so every share is positive and the total is exact. A
         * recipient costs one point, so an amount smaller than the candidate
         * set chooses only its first {@code remaining} candidates. */
        static int[] evenShares(int remaining, int recipients) {
            if (remaining <= 0 || recipients <= 0) {
                return new int[0];
            }
            recipients = Math.min(recipients, remaining);
            int[] shares = new int[recipients];
            int each = remaining / recipients;
            int extra = remaining % recipients;
            for (int i = 0; i < recipients; i++) {
                shares[i] = each + (i < extra ? 1 : 0);
            }
            return shares;
        }

        /** A bespoke non-targeting card pick (Bruna's per-zone Aura attach) is
         * posed through choose(Cards, TargetCard), which reads the CHOICE
         * queue, while the generator scripted it for the chooseTarget twin on
         * the TARGET queue. Move it before forcedChooseCards, which would
         * otherwise take a lone candidate and strand the scripted target. */
        private void handOffCardPick(Cards cards, mage.target.TargetCard target, Ability source, Game game) {
            if (target.getMinNumberOfTargets() > 0 && !getTargets().isEmpty()
                    && (source == null || !source.getTargets().contains(target))
                    && (source == null || !source.getTargets().contains(target.getOriginalTarget()))) {
                UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                List<UUID> candidates = new ArrayList<>();
                for (UUID id : target.possibleTargets(abilityControllerId, source, game, cards)) {
                    if (!target.contains(id)) {
                        candidates.add(id);
                    }
                }
                movePickToChoices(getChoices(), getTargets(), !candidates.isEmpty(), answer -> {
                    for (UUID id : candidates) {
                        if (answerNamesCandidate(game, id, answer)) {
                            return true;
                        }
                    }
                    return false;
                });
            }
        }

        @Override
        public boolean choose(Outcome outcome, Cards cards, mage.target.TargetCard target, Ability source, Game game) {
            Game previous = choiceGame;
            choiceGame = game;
            boolean declined = declinePairToChoiceSkip(target.getMinNumberOfTargets(), getChoices(), getTargets());
            try {
                handOffCardPick(cards, target, source, game);
                if (forcedChooseCards(cards, target, source, game)) {
                    return true;
                }
                return super.choose(outcome, cards, target, source, game);
            } finally {
                if (declined && !getChoices().isEmpty() && TestPlayer.CHOICE_SKIP.equals(getChoices().get(0))) {
                    // The dialog returned without reading its queue (no
                    // candidate): put the pair back for the ask it belongs to.
                    getChoices().set(0, "No");
                    getTargets().add(0, TestPlayer.TARGET_SKIP);
                }
                choiceGame = previous;
            }
        }

        /** The choose(Cards, TargetCard) twin of forcedChoose: a look-and-pick
         * whose every remaining card is needed (Moment of Truth's last card of
         * three, Skyclave Plunder's three of three, Expressive Iteration's
         * last exile) is no decision gorge records, yet XMage asks it, so
         * the cards are taken here unless the choice queue's head is a name
         * for one of them or a skip. */
        private boolean forcedChooseCards(Cards cards, mage.target.TargetCard target, Ability source, Game game) {
            UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
            List<UUID> candidates = new ArrayList<>();
            for (UUID id : target.possibleTargets(abilityControllerId, source, game, cards)) {
                if (!target.contains(id)) {
                    candidates.add(id);
                }
            }
            String head = getChoices().isEmpty() ? null : getChoices().get(0);
            boolean headNamesCandidate = false;
            if (head != null) {
                for (UUID id : candidates) {
                    // TestPlayer reads the choice queue as "^" segments, so a
                    // multi-pick answer ("Plains^Plains") is this ask's own
                    // even though the whole string names no object. Comparing
                    // the whole string left the scripted answer at the queue
                    // front while the fall-through below took the cards, and
                    // the replay ended with a leftover choice.
                    if (answerNamesCandidate(game, id, head)) {
                        headNamesCandidate = true;
                        break;
                    }
                }
            }
            int needed = target.getMinNumberOfTargets() - target.getSize();
            if (forcedPick(needed, candidates.size(), head, headNamesCandidate)) {
                for (UUID id : candidates) {
                    target.add(id, game);
                }
                return true;
            }
            if (headNamesCandidate || TestPlayer.CHOICE_SKIP.equals(head)) {
                // The queue head is this ask's own answer (a name for one of
                // the offered cards, or a skip); leave it to the base player.
                return false;
            }
            List<UUID> offered = offeredCandidates(cards, candidates, game);
            UUID order = unscriptedCardOrderPick(target, cards, candidates, game, false);
            if (order != null) {
                target.add(order, game);
                return true;
            }
            List<UUID> picks = interchangeableLibraryPick(owner.pastLastCheckpoint(),
                    target.isRequired(), target.getMinNumberOfTargets(),
                    target.getMaxNumberOfTargets(), target.getZone(), needed, offered, candidates,
                    id -> {
                        Card card = game.getCard(id);
                        return card == null ? null : card.getName();
                    });
            if (picks == null) {
                return false;
            }
            for (UUID id : picks) {
                target.add(id, game);
            }
            return true;
        }

        /** Whether a Cards/TargetCard ask is XMage's card-ORDER dialog rather
         * than a pick. Three families build it: the putCardsOn{Bottom,Top}
         * OfLibrary(anyOrder) paths and putCardOnTopXOfLibrary take one card
         * at a time through a TargetCard(Zone.ALL) named "card ORDER to put
         * ...", ZonesHandler.chooseOrder's simultaneous-zone-change sites
         * (graveyard, top/bottom of library) name it "order to put ...", and a
         * card's own order loop (Proteus Staff's "card to put on bottom ...
         * (last chosen will be on bottom)") uses a Zone.LIBRARY window. Every
         * one of them ends its target name with the "last (one) chosen will
         * be" clause, whichever zone and wording it uses, so a new ordering
         * call site is covered by that role rather than by a card, effect or
         * message list. A pick-one (Booster Tutor's pack, Psychotic Episode's
         * hand-plus-topdeck, Agonizing Memories' hand pick) carries no such
         * clause; the Zone.HAND window is a sequence of picks, never an order,
         * so it is excluded too. The fixed 1/1 count narrows the role further. */
        static boolean isCardOrderAsk(Zone zone, int minTargets, int maxTargets, String targetName) {
            if (minTargets != 1 || maxTargets != 1 || zone == Zone.HAND || targetName == null) {
                return false;
            }
            String lower = targetName.toLowerCase(java.util.Locale.ROOT);
            return lower.contains("last chosen will be") || lower.contains("last one chosen will be");
        }

        /** The card an order ask takes when every offered card has the same
         * name, or null when the order can change the compared state. XMage
         * asks an order one card at a time (n-1 asks), and gorge records no
         * decision for a pile of indistinguishable cards (Fathom Trawl's
         * revealed lands): every name the replay compares is unchanged by the
         * order. Unlike {@link #interchangeableLibraryPick} this runs inside
         * the compared window too -- there is no gorge decision here whose
         * strict miss could be hidden. The offer order is the window's
         * insertion order (CardsImpl, the looked-at library order), so the
         * first card is deterministic. Distinct names are a real order gorge
         * should have posed under CR 401.4 (the owner of cards put in one
         * position at the same time arranges them): null leaves the ask to the
         * base, whose strict miss the item reports as a gorge finding. */
        static UUID interchangeableCardOrderPick(boolean orderAsk, List<UUID> offered,
                java.util.function.Function<UUID, String> nameOf) {
            if (!orderAsk || offered.size() < 2) {
                return null;
            }
            String only = null;
            for (UUID id : offered) {
                String name = nameOf.apply(id);
                if (name == null || only != null && !only.equals(name)) {
                    return null;
                }
                only = name;
            }
            return offered.get(0);
        }

        /** The cards a Cards/TargetCard window offers the ask: the window's
         * own insertion order (CardsImpl, so the looked-at library order),
         * intersected with the target's legal candidates. Both Cards funnels
         * use this one list so an order ask and a fixed-count pick read the
         * same deterministic offer order. */
        static List<UUID> offeredCandidates(Cards cards, List<UUID> candidates, Game game) {
            java.util.Set<UUID> legal = new java.util.HashSet<>(candidates);
            List<UUID> offered = new ArrayList<>();
            for (Card card : cards.getCards(game)) {
                if (legal.contains(card.getId())) {
                    offered.add(card.getId());
                }
            }
            return offered;
        }

        /** The card an unscripted order ask takes from the funnel's window,
         * or null. Both Cards funnels call this one rule; a queue answer the
         * funnel considers the ask's own ({@code queueNamesCandidate}) stays
         * with the base, and the role gate is {@link #isCardOrderAsk}. */
        UUID unscriptedCardOrderPick(mage.target.TargetCard target, Cards cards,
                List<UUID> candidates, Game game, boolean queueNamesCandidate) {
            if (queueNamesCandidate || !isCardOrderAsk(target.getZone(),
                    target.getMinNumberOfTargets(), target.getMaxNumberOfTargets(),
                    target.getTargetName())) {
                return null;
            }
            return interchangeableCardOrderPick(true,
                    offeredCandidates(cards, candidates, game), id -> {
                        Card card = game.getCard(id);
                        return card == null ? null : card.getName();
                    });
        }

        /** The pick a fixed-count look-at-library ask takes when the offered
         * cards are interchangeable by name, or null when the ask is a real
         * choice. A Black Cat, Cunning Thief look at the top nine exiles two
         * face down: gorge's decision over same-named cards (all Plains) moves
         * two card objects whose name is all the compared state can see, so
         * any deterministic subset is the same game state and the trailing
         * "Missing CHOICE def" is not a divergence. The same shape reaches the
         * chooseTarget(Cards) funnel for a card whose effect asks that way
         * (Dimir Charm's "put one back"). The rule is deliberately SHAPE-safe,
         * not a default pick: it needs a required ask, a fixed count
         * (min == max), a LIBRARY window (cards are identified by name alone;
         * a same-named permanent or exiled card can differ by its attached
         * state), more candidates than picks (a forced pick owns the rest),
         * and ONE name across every candidate. Distinct names are gorge's real
         * choice and stay unscripted until the generator scripts it.
         *
         * Only an ask in the trailing play -- after every scenario checkpoint
         * was captured ({@link ScenarioReplay#pastLastCheckpoint}) -- is
         * answered, the same rule as {@link #searchPickChoice}. Inside the
         * compared window the pick is gorge's real decision: a same-name
         * substitute would compare equal but hide a cut/generator gap and
         * cost the strict-miss signal. {@code offerOrder} is the order the
         * ask offered the cards in (from the engine's Cards; a Set, so it
         * promises no library order -- every candidate here has the same
         * name, so which ids are taken never reaches the compared state);
         * {@code candidates} the legal ones among it. */
        static List<UUID> interchangeableLibraryPick(boolean trailingPlay, boolean required,
                int minTargets, int maxTargets,
                Zone zone, int needed, List<UUID> offerOrder, List<UUID> candidates,
                java.util.function.Function<UUID, String> nameOf) {
            if (!trailingPlay || !required || needed <= 0 || minTargets != maxTargets || zone != Zone.LIBRARY
                    || candidates.size() <= needed) {
                return null;
            }
            String only = null;
            for (UUID id : candidates) {
                String name = nameOf.apply(id);
                if (name == null || only != null && !only.equals(name)) {
                    return null;
                }
                only = name;
            }
            List<UUID> picks = new ArrayList<>();
            for (UUID id : offerOrder) {
                if (candidates.contains(id)) {
                    picks.add(id);
                    if (picks.size() == needed) {
                        return picks;
                    }
                }
            }
            return null;
        }

        /** The generator scripts a declined pick as a pair -- "No" on the
         * choice queue and "[target_skip]" on the target queue -- because
         * XMage may pose it as a yes/no or as an "up to" target. An optional
         * choose(Cards, TargetCard) dialog (Choco's "you may put one of them
         * into your hand", a look-at-the-top dig) reads only the CHOICE
         * queue, where the decline is its own skip token: rewrite the pair at
         * both queue fronts into one "[choice_skip]". Anything else -- a
         * required dialog, a named pick, a lone "No" -- is left alone.
         * Returns whether the pair was rewritten. */
        static boolean declinePairToChoiceSkip(int minTargets, List<String> choices, List<String> targets) {
            if (minTargets != 0 || choices.isEmpty() || targets.isEmpty()
                    || !"No".equals(choices.get(0)) || !TestPlayer.TARGET_SKIP.equals(targets.get(0))) {
                return false;
            }
            choices.set(0, TestPlayer.CHOICE_SKIP);
            targets.remove(0);
            return true;
        }

        @Override
        public boolean hasObjectTargetNameOrAlias(mage.MageObject object, String value) {
            if (choiceGame != null && value != null && value.startsWith("@")
                    && isScenarioRef(value.substring(1)) && value.contains(":token:")) {
                String ref = value.substring(1);
                UUID seatId = owner.seat(refSeat(ref)).getId();
                Iterable<Permanent> field = choiceGame.getBattlefield().getAllPermanents();
                Permanent chosen = tokenChoice(ref, seatId, field);
                if (chosen == null && !owner.xmageSpelling(refName(ref)).equals(refName(ref))) {
                    // A token copy of a renamed card carries XMage's spelling.
                    chosen = tokenChoice(ref, owner.xmageSpelling(refName(ref)), seatId, field);
                }
                return object != null && chosen != null && chosen.getId().equals(object.getId());
            }
            if (super.hasObjectTargetNameOrAlias(object, value)) {
                return true;
            }
            // A face-down permanent (CR 708.2a: a manifested or cloaked
            // creature has no name) never matches the card name the scenario
            // ref carries. The face-down state arises mid-window (manifest
            // dread, cloak), after the steps are queued, so it is matched
            // here against the LIVE game: the ref names the card that became
            // the face-down creature (equip target, sacrifice-cost pick).
            if (choiceGame != null && object instanceof Permanent
                    && ((Permanent) object).isFaceDown(choiceGame)) {
                mage.cards.Card underlying = choiceGame.getCard(object.getId());
                String underlyingName = underlying == null ? null : underlying.getName();
                // A face-up permanent of the same name keeps the ref only when
                // the ask in progress could legally pick it: a Mountain LAND
                // is no candidate of an equip or "sacrifice a creature" ask.
                // Outside an ask (no target known) only a face-up creature
                // counts, as the face-down permanent is itself a creature.
                java.util.Set<UUID> legal = null;
                if (askTarget != null && askSource != null) {
                    legal = askTarget.possibleTargets(askTarget.getAffectedAbilityControllerId(getId()), askSource, choiceGame);
                }
                final java.util.Set<UUID> candidates = legal;
                boolean faceUpTwin = faceUpTwinAmong(candidates, object.getId(), id -> {
                    Permanent other = choiceGame.getPermanent(id);
                    return other == null || other.isFaceDown(choiceGame)
                            || (candidates == null && !other.isCreature(choiceGame)) ? null : other.getName();
                }, choiceGame.getBattlefield().getAllPermanents(), value);
                return namesFaceDownCard(true, underlyingName, value, faceUpTwin);
            }
            return false;
        }

        /** Whether a face-up permanent other than {@code self} that the ask
         * may legally pick ({@code legal}; null = ask unknown, every
         * permanent in {@code field} is looked at) carries {@code value} as
         * its name. {@code faceUpName} yields a permanent's name, or null
         * when it is face-down or not a possible twin. */
        static boolean faceUpTwinAmong(java.util.Set<UUID> legal, UUID self,
                java.util.function.Function<UUID, String> faceUpName, Iterable<Permanent> field, String value) {
            java.util.List<UUID> ids = new ArrayList<>();
            if (legal != null) {
                ids.addAll(legal);
            } else {
                for (Permanent p : field) {
                    ids.add(p.getId());
                }
            }
            for (UUID id : ids) {
                if (!id.equals(self) && value != null && value.equals(faceUpName.apply(id))) {
                    return true;
                }
            }
            return false;
        }

        /** True when a face-down permanent is the one a scenario ref names:
         * the queued value is the card's plain XMage-spelled name, never an
         * "@" alias or a bracketed test command, and equals the name of the
         * card that sits face down. A face-up permanent is never matched
         * here (its own name already answered the base's comparison), and a
         * face-up permanent of the same name keeps the ref: the candidate
         * Set's order must not decide between a card and its face-down twin. */
        static boolean namesFaceDownCard(boolean faceDown, String underlyingName, String value, boolean faceUpTwin) {
            return faceDown && !faceUpTwin && underlyingName != null && value != null
                    && !value.startsWith("@") && !value.startsWith("[")
                    && underlyingName.equals(value);
        }

        @Override
        public boolean scry(int value, Ability source, Game game) {
            if (game.getTurnNum() == 1 && game.getStep() == null) {
                return false;
            }
            GameEvent event = new GameEvent(GameEvent.EventType.SCRY, getId(), source, getId(), value, true);
            if (game.replaceEvent(event)) {
                return false;
            }
            game.informPlayers(getLogName() + " scries " + event.getAmount() + CardUtil.getSourceLogName(game, source));
            Cards cards = new CardsImpl();
            cards.addAllCards(getLibrary().getTopCards(game, event.getAmount()));
            if (!cards.isEmpty()) {
                TargetCard target = new TargetCard(0, cards.size(), Zone.LIBRARY,
                        new FilterCard("card" + (cards.size() == 1 ? "" : "s") + " to PUT on the BOTTOM of your library (Scry)"));
                Cards selected = scriptedLibrarySelection(cards, game);
                boolean placed = selected != null;
                if (selected == null && nothingScripted(getChoices(), getTargets())) {
                    // Gorge recorded no arrange decision, so nothing was
                    // scripted: keep every looked-at card where it is.
                    selected = new CardsImpl();
                    placed = true;
                } else if (selected == null) {
                    chooseTarget(Outcome.Benefit, cards, target, source, game);
                    selected = new CardsImpl(target.getTargets());
                }
                cards.removeAll(selected);
                if (placed) {
                    placeArranged(selected, cards, game, source);
                } else {
                    putCardsOnBottomOfLibrary(selected, game, source, true);
                }
                if (!selected.isEmpty()) {
                    game.fireEvent(GameEvent.getEvent(GameEvent.EventType.SCRY_TO_BOTTOM, getId(), source, getId(), selected.size()));
                }
                if (!placed) {
                    putCardsOnTopOfLibrary(cards, game, source, true);
                }
            }
            game.fireEvent(new GameEvent(GameEvent.EventType.SCRIED, getId(), source, getId(), event.getAmount(), true));
            return true;
        }

        @Override
        public Player.SurveilResult doSurveil(int value, Ability source, Game game) {
            GameEvent event = new GameEvent(GameEvent.EventType.SURVEIL, getId(), source, getId(), value, true);
            if (game.replaceEvent(event) || event.getAmount() < 1) {
                return Player.SurveilResult.noSurveil();
            }
            game.informPlayers(getLogName() + " surveils " + event.getAmount() + CardUtil.getSourceLogName(game, source));
            Cards cards = new CardsImpl();
            cards.addAllCards(getLibrary().getTopCards(game, event.getAmount()));
            Cards graveyard = new CardsImpl();
            Cards top = new CardsImpl();
            if (!cards.isEmpty()) {
                TargetCard target = new TargetCard(0, cards.size(), Zone.LIBRARY,
                        new FilterCard("card" + (cards.size() == 1 ? "" : "s") + " to PUT into your GRAVEYARD (Surveil)"));
                Cards selected = scriptedLibrarySelection(cards, game);
                boolean placed = selected != null;
                if (selected == null && nothingScripted(getChoices(), getTargets())) {
                    // Gorge recorded no arrange decision, so nothing was
                    // scripted: keep every looked-at card where it is.
                    selected = new CardsImpl();
                    placed = true;
                } else if (selected == null) {
                    chooseTarget(Outcome.Benefit, cards, target, source, game);
                    selected = new CardsImpl(target.getTargets());
                }
                if (!selected.isEmpty()) {
                    graveyard.addAllCards(moveCardsToGraveyardWithInfo(selected.getCards(game), source, game, Zone.LIBRARY));
                }
                cards.removeAll(selected);
                if (placed) {
                    placeArranged(new CardsImpl(), cards, game, source);
                } else {
                    putCardsOnTopOfLibrary(cards, game, source, true);
                }
                top.addAll(cards);
            }
            game.fireEvent(new GameEvent(GameEvent.EventType.SURVEILED, getId(), source, getId(), event.getAmount(), true));
            return Player.SurveilResult.surveil(graveyard, top);
        }

        /** Puts a scripted arrange's piles back without XMage's ORDER asks.
         * The scripted selection already fixed both orders, exactly as
         * gorge's KArrange answer does (rules.arrangeAnswerRecord): pile B
         * goes to the bottom in the order given, its last card bottommost,
         * and pile A goes back on top in the order given, its first card
         * topmost. XMage's putCardsOn{Bottom,Top}OfLibrary with anyOrder
         * would ask "card ORDER to put on the ..." once per card but the
         * last, an ask gorge never poses because its one answer carried the
         * order, so strict mode stopped there (Candy Trail's scry 2 sending
         * both cards to the bottom). */
        private void placeArranged(Cards bottom, Cards top, Game game, Ability source) {
            for (UUID id : new ArrayList<>(bottom)) {
                Card card = game.getCard(id);
                if (card != null) {
                    putCardsOnBottomOfLibrary(card, game, source);
                }
            }
            for (UUID id : arrangedTopMoves(new ArrayList<>(top))) {
                Card card = game.getCard(id);
                if (card != null) {
                    putCardsOnTopOfLibrary(card, game, source, false);
                }
            }
        }

        /** The order to move pile A onto the library one card at a time so
         * its first card ends topmost: last first. */
        static <T> List<T> arrangedTopMoves(List<T> topFirst) {
            List<T> moves = new ArrayList<>(topFirst);
            java.util.Collections.reverse(moves);
            return moves;
        }

        /** Whether the scenario scripted nothing at all for the player: both
         * answer queues are empty. A scry/surveil ask in that state has no
         * answer to read -- gorge recorded no arrange decision for it (Carrot
         * Cake's "create a Rabbit and scry 1" off an OrTriggeredAbility) -- so
         * the cards stay where they are instead of failing "Missing TARGET
         * def". A queue holding anything keeps the base's strict reading. */
        static boolean nothingScripted(List<String> choices, List<String> targets) {
            return choices.isEmpty() && targets.isEmpty();
        }

        /** Consume the generator's choice-queue selection and kept-card order.
         * Null means this decision was scripted through the ordinary target queue. */
        private Cards scriptedLibrarySelection(Cards cards, Game game) {
            // Cards#getCards is a Set and does not promise library order.
            // Reconstruct the looked-at prefix from Library's ordered view.
            List<Card> lookedAtOrder = new ArrayList<>();
            for (Card card : getLibrary().getCards(game)) {
                if (cards.contains(card.getId())) {
                    lookedAtOrder.add(card);
                }
            }
            Set<Card> available = new java.util.LinkedHashSet<>(lookedAtOrder);
            List<String> queue = librarySelectionQueue(getChoices(), getTargets(),
                    answer -> findByName(available, answer) != null);
            if (queue == null) {
                return null;
            }
            Cards selected = new CardsImpl();
            boolean scripted = false;
            boolean selectionEnded = false;
            while (!queue.isEmpty()) {
                String answer = queue.get(0);
                if (isLibrarySelectionSkip(answer)) {
                    queue.remove(0);
                    scripted = true;
                    selectionEnded = true;
                    break;
                }
                Card match = findByName(available, answer);
                if (match == null) {
                    break;
                }
                queue.remove(0);
                available.remove(match);
                selected.add(match);
                scripted = true;
            }
            if (!scripted) {
                return null;
            }

            // The labels after the selection's skip are the kept cards' top
            // order (pile A, in the decision's pick order). The generator
            // emits every kept card exactly once, so the order phase is
            // bounded by the cards still available after the selection and
            // consumes only labels naming one of them: a later decision's
            // answer in the same step may name a looked-at card (a binned
            // duplicate, or a card already selected), and must stay queued.
            // An all-kept arrange is the skip alone (nothing selected, no
            // order labels follow), so it consumes none.
            List<Card> ordered = new ArrayList<>();
            if (selectionEnded && !selected.isEmpty()) {
                List<String> keptNames = new ArrayList<>();
                for (Card card : available) {
                    keptNames.add(card.getName());
                }
                for (String name : keptOrderLabels(queue, keptNames)) {
                    Card match = findByName(available, name);
                    available.remove(match);
                    ordered.add(match);
                }
            }
            cards.clear();
            for (Card card : ordered) {
                cards.add(card);
            }
            // Any kept card without a distinct ordering label retains its
            // original look order; never let Set iteration determine library order.
            for (Card card : lookedAtOrder) {
                if (available.remove(card)) {
                    cards.add(card);
                }
            }
            return selected;
        }

        /** Consume the kept-card order labels at the head of {@code queue}:
         * the longest leading run naming a not-yet-ordered kept card, never
         * more than there are kept cards. The first label that names no
         * remaining kept card (or the queue's end) stops the phase, so an
         * answer belonging to a later decision is left queued. Returns the
         * consumed names in order. */
        static List<String> keptOrderLabels(List<String> queue, List<String> keptNames) {
            List<String> remaining = new ArrayList<>(keptNames);
            List<String> consumed = new ArrayList<>();
            while (!remaining.isEmpty() && !queue.isEmpty()) {
                if (!remaining.remove(queue.get(0))) {
                    break;
                }
                consumed.add(queue.remove(0));
            }
            return consumed;
        }

        /**
         * Library selection is exposed by XMage's chooser as a target-shaped
         * generator answer, but surveil/scry are implemented here through the
         * choice queue. Move only a leading target skip into that queue when no
         * card/skip answer already addresses this library selection; ordinary
         * target skips remain on their target queue.
         */
        static List<String> librarySelectionQueue(List<String> choices, List<String> targets,
                java.util.function.Predicate<String> namesLookedAtCard) {
            if (!choices.isEmpty() && (TestPlayer.CHOICE_SKIP.equals(choices.get(0))
                    || namesLookedAtCard.test(choices.get(0)))) {
                return choices;
            }
            if (!targets.isEmpty() && TestPlayer.TARGET_SKIP.equals(targets.get(0))) {
                targets.remove(0);
                choices.add(0, TestPlayer.CHOICE_SKIP);
                return choices;
            }
            return choices.isEmpty() ? null : choices;
        }

        private static boolean isLibrarySelectionSkip(String answer) {
            return TestPlayer.CHOICE_SKIP.equals(answer) || TestPlayer.TARGET_SKIP.equals(answer);
        }

        private Card findByName(Set<Card> cards, String name) {
            for (Card card : cards) {
                if (card.getName().equals(name)) {
                    return card;
                }
            }
            return null;
        }

        /** The cards-list overload TargetCardInLibrary.choose (every library
         * search) reaches. The unscripted-ask rules of the plain Target funnel
         * apply here: an ask with no legal candidate is declined at any
         * minimum rather than failing "Missing TARGET def", and a lone legal
         * candidate is taken because gorge settles it without a decision. A
         * short search closes on its skip, and a single-pick search with many
         * legal cards gathers the first one only in the trailing play
         * ({@link #searchPickChoice}). A plain TargetCard look-at window
         * outside that family (Dimir Charm's "look at the top three, put one
         * back") is answered by {@link #interchangeableLibraryPick} when every
         * candidate is the same name. Every other ask is the base's. */
        @Override
        public boolean chooseTarget(Outcome outcome, Cards cards, mage.target.TargetCard target, Ability source, Game game) {
            if (target instanceof mage.target.common.TargetCardInLibrary) {
                java.util.Collection<UUID> possible = target.possibleTargets(
                        target.getAffectedAbilityControllerId(this.getId()), source, game, cards);
                if (unscriptableEmptyAsk(target.getMinNumberOfTargets() - target.getSize(), possible.size(),
                        !getTargets().isEmpty() && TestPlayer.TARGET_SKIP.equals(getTargets().get(0)))) {
                    return false;
                }
                // A skip that closes a short search is the ask's own answer,
                // even when the library happens to offer one legal card: run
                // it before the lone-target rule, or the driver would take
                // the card and leave the skip queued for a later ask (CR
                // 701.23b closes the search on the skip).
                if (shortSearchStop(getTargets(), target.isRequired(),
                        target.getMinNumberOfTargets(), target.getMaxNumberOfTargets())) {
                    // A search for cards with a stated quality need not find them
                    // all (CR 701.23b): XMage leaves the target not required, but
                    // TestPlayer rejects a skip on a fixed-count ask. Close the
                    // selection with what is chosen. isChosen stays false below
                    // the stated count, so the result is the pick count itself:
                    // TargetCardInLibrary stops on a true result with nothing new
                    // selected and the effect then takes the short selection.
                    getTargets().remove(0);
                    target.setSkipChoice(true);
                    return target.getSize() > 0;
                }
                if (target.getSize() < target.getMaxNumberOfTargets()) {
                    UUID lone = unscriptedLoneTarget(target, possible, game);
                    if (lone != null) {
                        target.addTarget(lone, source, game);
                        return true;
                    }
                }
                UUID searchPick = searchPickChoice(getTargets(),
                        libraryCandidates(cards, possible, target),
                        target.getMaxNumberOfTargets() == 1,
                        skipCanCloseAsk(target),
                        owner.pastLastCheckpoint(),
                        (id, answer) -> hasObjectTargetNameOrAlias(game.getObject(id), answer));
                if (searchPick != null) {
                    mage.MageObject picked = game.getObject(searchPick);
                    owner.searchPicks.add(picked == null ? "?" : picked.getName());
                    target.addTarget(searchPick, source, game);
                    return true;
                }
            }
            boolean orderAsk = isCardOrderAsk(target.getZone(), target.getMinNumberOfTargets(),
                    target.getMaxNumberOfTargets(), target.getTargetName());
            if (orderAsk || target.getZone() == Zone.LIBRARY
                    && !(target instanceof mage.target.common.TargetCardInLibrary)) {
                java.util.Collection<UUID> possible = target.possibleTargets(
                        target.getAffectedAbilityControllerId(this.getId()), source, game, cards);
                List<UUID> candidates = unchosenCandidates(target, possible);
                // This funnel consumes the TARGET queue, so a queued answer
                // that names one of these candidates is this ask's own and
                // goes to the base; a front skip is this ask's only when it
                // can close the ask (an "up to" ask whose minimum is met).
                boolean ownAnswer = !getTargets().isEmpty()
                        && TestPlayer.TARGET_SKIP.equals(getTargets().get(0)) && skipCanCloseAsk(target);
                for (String answer : getTargets()) {
                    for (UUID id : candidates) {
                        if (answerNamesCandidate(game, id, answer)) {
                            ownAnswer = true;
                            break;
                        }
                    }
                    if (ownAnswer) {
                        break;
                    }
                }
                if (!ownAnswer) {
                    UUID order = unscriptedCardOrderPick(target, cards, candidates, game, false);
                    if (order != null) {
                        target.addTarget(order, source, game);
                        return true;
                    }
                }
                if (!ownAnswer && target.getZone() == Zone.LIBRARY
                        && !(target instanceof mage.target.common.TargetCardInLibrary)) {
                    List<UUID> offered = offeredCandidates(cards, candidates, game);
                    List<UUID> picks = interchangeableLibraryPick(owner.pastLastCheckpoint(),
                            target.isRequired(), target.getMinNumberOfTargets(),
                            target.getMaxNumberOfTargets(), target.getZone(),
                            target.getMinNumberOfTargets() - target.getSize(),
                            offered, candidates, id -> {
                                Card card = game.getCard(id);
                                return card == null ? null : card.getName();
                            });
                    if (picks != null) {
                        for (UUID id : picks) {
                            target.addTarget(id, source, game);
                        }
                        return true;
                    }
                }
            }
            return super.chooseTarget(outcome, cards, target, source, game);
        }

        /** Whether the target queue's head is a skip that closes a library
         * search XMage allows to fall short (not required) although its count
         * is fixed (min == max), the one shape TestPlayer's skip assertion
         * refuses. */
        static boolean shortSearchStop(List<String> targets, boolean required, int min, int max) {
            return !targets.isEmpty() && TestPlayer.TARGET_SKIP.equals(targets.get(0))
                    && !required && min == max;
        }

        /** The card an unscripted library search gathers, or null to leave
         * the ask to the base player. A search is not a decision gorge
         * always records when the item's cut ends before it: the stored
         * Gladiolus Amicitia ETB (32 legal lands) scripts no card name, yet
         * XMage poses the mandatory TargetCardInLibrary ask and strict mode
         * stops with "Missing TARGET def". {@link #libraryCandidates}
         * supplies the legal cards in the order XMage presents them, so its
         * first entry is a deterministic pick rather than a set iteration
         * order.
         *
         * Only an ask in the trailing play -- after every scenario checkpoint
         * was captured ({@link #pastLastCheckpoint}) -- is answered here. A
         * search inside the compared window is a real gorge decision: the
         * driver cannot infer which card was taken, so inventing one would
         * compare a made-up board (and could hide a generator/cut gap). Such
         * an ask stays the base's strict miss and the caller reruns loose.
         *
         * Only a single-pick ask is answered: a multi-pick search's count is
         * a decision of its own and must stay scripted. A queued answer of
         * this ask's own -- a front skip that can close it, or any entry
         * naming a candidate (the base scans the whole visible queue) -- goes
         * to the base; an answer for a later ask does not stop the pick. This
         * is the role-based rule for the whole TargetCardInLibrary family,
         * not a list of search cards. */
        static UUID searchPickChoice(List<String> targetQueue, List<UUID> candidates,
                boolean singlePick, boolean skipClosesAsk, boolean trailingPlay,
                BiPredicate<UUID, String> namesCandidate) {
            if (!trailingPlay || !singlePick || candidates.isEmpty()) {
                return null;
            }
            if (!targetQueue.isEmpty() && TestPlayer.TARGET_SKIP.equals(targetQueue.get(0)) && skipClosesAsk) {
                return null;
            }
            for (String answer : targetQueue) {
                for (UUID id : candidates) {
                    if (namesCandidate.test(id, answer)) {
                        return null;
                    }
                }
            }
            return candidates.get(0);
        }

        /** The target's unchosen legal cards in the order the search ask
         * presents them. TargetCardInLibrary.choose sorts the searchable
         * library by card name into the Cards it passes here, and CardsImpl
         * is a LinkedHashSet, so this iteration is that presentation order. */
        static List<UUID> libraryCandidates(Cards cards, java.util.Collection<UUID> possible,
                mage.target.Target target) {
            List<UUID> out = new ArrayList<>();
            if (cards == null) {
                return out;
            }
            for (UUID id : cards) {
                if (possible.contains(id) && !target.contains(id)) {
                    out.add(id);
                }
            }
            return out;
        }

        /**
         * The base TestPlayer handles a TargetSpellOrPermanent's stack half
         * only: its "stack" branch in chooseTarget searches game.getStack()
         * and then asserts when the queue is still non-empty, so a
         * battlefield permanent answer can never be matched through the
         * addTarget queue (Aang, Swift Savior's airbend "up to one other
         * target creature or spell" is a resolve-step trigger; Jeskai
         * Revelation's multi-target cast is the same gap). The cast/activate
         * path (handleNonPlayerTargetTarget) already matches such an answer
         * by name or alias against the target's own possibleTargets, so
         * mirror that here for the battlefield half the base omits, and leave
         * every other class -- including a spell on the stack, which the base
         * does handle -- to super. This lives in the tracked driver, not in
         * the out-of-tree TestPlayer, so it survives an XMAGE_REF bump.
         */
        @Override
        public boolean chooseTarget(Outcome outcome, mage.target.Target target, Ability source, Game game) {
            // The engine also calls chooseTarget directly (a spell's own
            // target selection), not only through choose. Make the scripted
            // matcher see this ask as a choice so a token ref's live,
            // creation-order resolution applies here too; the outer finally
            // keeps the previous game for the nested ask.
            Game previous = choiceGame;
            mage.target.Target previousTarget = askTarget;
            Ability previousSource = askSource;
            choiceGame = game;
            askTarget = target;
            askSource = source;
            try {
                // XMage poses a target ask even when no candidate is legal
                // (Targets.canChooseFromPossibleTargets returns true for a min-0
                // object whatever its candidate set, and a mandatory ask is
                // posed so the effect can report a failed choice), while
                // gorge's engine never poses an ask without a legal option --
                // so such an ask is unscriptable by construction and its only
                // legal answer is none. Decline it without consulting the
                // queue: a later ask's scripted answer (Does Machines' combat
                // trigger, queued behind an earlier level-gain trigger's empty
                // graveyard ask) must stay untouched for its own ask. A queued
                // skip still belongs to THIS optional ask and is honoured by
                // the ordinary path below.
                if (unscriptableEmptyAsk(target.getMinNumberOfTargets(),
                        target.possibleTargets(target.getAffectedAbilityControllerId(this.getId()), source, game).size(),
                        !getTargets().isEmpty() && TestPlayer.TARGET_SKIP.equals(getTargets().get(0)))) {
                    return target.getSize() > 0 && target.isChosen(game);
                }
                // TestPlayer.chooseTarget runs its zone matcher over every queued
                // answer and rejects a "[target_skip]" that is not at the queue
                // front (checkTargetDefinitionMarksSupport), so a skip queued for
                // a LATER target object (Rise from the Wreck's empty Mount slot)
                // is hidden from this ask and restored, in order, afterwards.
                List<String> later = hideAfterNextSkip(getTargets());
                // TestPlayer's player branch scans the WHOLE queue for a
                // "targetPlayer=" answer whenever the ask is a TargetPlayer OR a
                // TargetPermanentOrPlayer, before its permanent branch runs, so
                // the any-target ask of a chain whose LATER object targets a
                // player would consume the later object's answer (Survey Mechan:
                // "deals 3 damage to any target" then "target player draws
                // three"). Withhold the later objects' player answers from this
                // ask and restore them, in order, afterwards.
                List<Integer> laterPlayerSlots = laterPlayerIndices(target, getTargets());
                List<String> laterPlayers = hideLaterPlayerTargets(target, getTargets());
                int visibleBefore = getTargets().size();
                try {
                    return chooseTargetInSegment(outcome, target, source, game);
                } finally {
                    // Each withheld answer goes back where it stood among the
                    // answers still queued, not behind them: appending put a
                    // later step's answer in front of the player answer a
                    // multi-target cast's next ask owns (Boulder Dash). The
                    // skip suffix was the queue's tail, so appending it is
                    // already its original position.
                    restoreLaterPlayerTargets(getTargets(), laterPlayers, laterPlayerSlots,
                            visibleBefore);
                    getTargets().addAll(later);
                }
            } finally {
                choiceGame = previous;
                askTarget = previousTarget;
                askSource = previousSource;
            }
        }

        /** A divided target is chosen by TestPlayer.chooseTargetAmount, outside
         * choose and chooseTarget, and reads the queue through the same name/
         * alias matcher. Make that a choice too, so a token ref's live,
         * creation-order resolution reaches the TargetAmount path. The same
         * unscripted-ask rules as the plain Target funnel apply: no candidate
         * at any minimum is declined, and a lone candidate takes the whole
         * remaining amount (the only distribution the rules leave). Several
         * candidates with no scripted "name^X=n" answer take the generator's
         * fallback split (see {@link #unscriptedAmountShares}). */
        @Override
        public boolean chooseTargetAmount(Outcome outcome, mage.target.TargetAmount target,
                Ability source, Game game) {
            Game previous = choiceGame;
            choiceGame = game;
            try {
                target.prepareAmount(source, game);
                if (target.getAmountRemaining() > 0
                        && (target.getMaxNumberOfTargets() != 0 || target.getMinNumberOfTargets() != 0)) {
                    java.util.Collection<UUID> possible = target.possibleTargets(
                            target.getAffectedAbilityControllerId(this.getId()), source, game);
                    if (unscriptableEmptyAsk(target.getMinNumberOfTargets() - target.getSize(), possible.size(),
                            !getTargets().isEmpty() && TestPlayer.TARGET_SKIP.equals(getTargets().get(0)))) {
                        return false;
                    }
                    if (target.getSize() < target.getMaxNumberOfTargets()) {
                        UUID lone = unscriptedLoneAmountTarget(target, possible, game);
                        if (lone != null) {
                            target.addTarget(lone, target.getAmountRemaining(), source, game);
                            return true;
                        }
                        List<UUID> candidates = stableAmountCandidates(unchosenCandidates(target, possible), game);
                        int[] shares = unscriptedAmountShares(target, candidates, game);
                        if (shares != null) {
                            for (int i = 0; i < shares.length; i++) {
                                target.addTarget(candidates.get(i), shares[i], source, game);
                            }
                            return true;
                        }
                    }
                }
                return super.chooseTargetAmount(outcome, target, source, game);
            } finally {
                choiceGame = previous;
            }
        }

        /** XMage's inline {@code $target=} form (castSpell/activateAbility
         * with a target string) sets its targets in TestPlayer.addTargets,
         * before any chooseTarget ask and outside choose. It reads the same
         * name/alias matcher, so make it a choice too: a token ref spelled
         * "@...:token:" resolves against the LIVE game through tokenChoice
         * here as well, instead of a stale setup alias or a bare name. */
        @Override
        public boolean addTargets(Ability ability, Game game) {
            Game previous = choiceGame;
            choiceGame = game;
            try {
                return super.addTargets(ability, game);
            } finally {
                choiceGame = previous;
            }
        }

        /** Whether a target ask is unscriptable and must be declined without
         * consulting the queue: no legal candidate, and no skip of its own at
         * the queue front. A skip closes only an "up to" ask, so it is this
         * ask's own only when the ask is optional (min 0); a mandatory ask
         * with no candidate is declined even when a later ask's skip is
         * queued. Mirrors TestPlayer's own empty-skip return, so an ability
         * whose other objects are chosen still proceeds. */
        static boolean unscriptableEmptyAsk(int minTargets, int possibleCount, boolean queueFrontIsSkip) {
            return possibleCount == 0 && (minTargets > 0 || !queueFrontIsSkip);
        }

        /** Detaches and returns the queue's "targetPlayer=" entries that
         * belong to a later target object of the same activation or cast, so
         * a TargetPermanentOrPlayer ask sees only its own slot. A player
         * entry at the queue front is this ask's own answer and stays; only
         * entries after the first non-player answer are withheld. */
        static List<String> hideLaterPlayerTargets(mage.target.Target target, List<String> queue) {
            List<Integer> slots = laterPlayerIndices(target, queue);
            List<String> hidden = new ArrayList<>();
            for (int i = slots.size() - 1; i >= 0; i--) {
                hidden.add(0, queue.remove((int) slots.get(i)));
            }
            return hidden;
        }

        /** The queue indices (ascending) hideLaterPlayerTargets withholds. */
        static List<Integer> laterPlayerIndices(mage.target.Target target, List<String> queue) {
            List<Integer> slots = new ArrayList<>();
            if (!(target.getOriginalTarget() instanceof mage.target.common.TargetPermanentOrPlayer)) {
                return slots;
            }
            int first = -1;
            for (int i = 0; i < queue.size(); i++) {
                if (!queue.get(i).startsWith("targetPlayer=")) {
                    first = i;
                    break;
                }
            }
            if (first < 0) {
                return slots;
            }
            for (int i = first + 1; i < queue.size(); i++) {
                if (queue.get(i).startsWith("targetPlayer=")) {
                    slots.add(i);
                }
            }
            return slots;
        }

        /** Puts the withheld player answers back into {@code queue} at their
         * original place relative to the answers that remain. {@code slots}
         * are their indices in the queue as it stood before hiding;
         * {@code visibleBefore} is the queue's
         * length once they were hidden, so the ask consumed
         * {@code visibleBefore - queue.size()} answers from the front. A
         * withheld answer that stood behind {@code k} visible answers now
         * stands behind {@code k - consumed} of them (none when the ask ate
         * past it). */
        static void restoreLaterPlayerTargets(List<String> queue, List<String> hidden, List<Integer> slots,
                int visibleBefore) {
            int consumed = Math.max(0, visibleBefore - queue.size());
            for (int j = 0; j < hidden.size(); j++) {
                int visibleAhead = slots.get(j) - j;
                int at = Math.min(queue.size(), Math.max(0, visibleAhead - consumed) + j);
                queue.add(at, hidden.get(j));
            }
        }

        /** XMage represents attach prompts either with a "to attach" hint or,
         * as One Last Job does, an unhinted non-targeting TargetPermanent ask. */
        static boolean isAttachmentChoice(mage.target.Target target) {
            return target.getChooseHint() != null && target.getChooseHint().startsWith("to attach ")
                    || target instanceof mage.target.TargetPermanent && target.isNotTarget()
                    && target.getTargetName().contains("can be attached to");
        }

        /** The hand card a TargetCardInHand ask reached through chooseTarget
         * should take when the TARGET queue has nothing for it, or null to
         * fall through to the base player. Two shapes: (1) the scripted pick
         * sits on the CHOICE queue -- gorge records a hand pick as a choose
         * ask, but an XMage effect that calls Target.chooseTarget (Ruthless
         * Negotiation's ExileFromZoneTargetEffect, Mindmelter, Unscrupulous
         * Agent) reads the target queue, so the pick is consumed from the
         * choice queue head when it names a candidate; (2) the pick was
         * forced -- the mandatory ask has no more candidates than picks it
         * must make -- which gorge never records as a decision (a one-card
         * hand), so a legal card is taken (the game re-asks for the rest). A
         * queued target answer naming a candidate is always the base's to
         * read, so a scripted target is never overridden
         * and a choice that names no candidate is left in place for the
         * base's strict unused-command check. {@code needed} is how many more
         * picks the ask must make (0 for an "up to" ask with its minimum met). */
        static UUID handCardChoice(List<String> targetQueue, List<String> choices, List<UUID> candidates,
                int needed, BiPredicate<UUID, String> matches) {
            if (!targetQueue.isEmpty()) {
                // A queued answer naming a candidate (or a skip) is this ask's
                // own and the base's to read. One that names no candidate
                // belongs to a later ask in the window, so it only stops the
                // choice-queue shortcut; a forced pick is still taken.
                if (TestPlayer.TARGET_SKIP.equals(targetQueue.get(0))) {
                    return null;
                }
                for (UUID id : candidates) {
                    if (matches.test(id, targetQueue.get(0))) {
                        return null;
                    }
                }
                return needed > 0 && candidates.size() <= needed ? candidates.get(0) : null;
            }
            if (!choices.isEmpty() && !TestPlayer.CHOICE_SKIP.equals(choices.get(0))) {
                for (UUID id : candidates) {
                    if (matches.test(id, choices.get(0))) {
                        choices.remove(0);
                        return id;
                    }
                }
            }
            if (needed > 0 && candidates.size() <= needed && !candidates.isEmpty()) {
                return candidates.get(0);
            }
            return null;
        }

        /** A cast-time sacrifice ask: the non-targeting TargetSacrifice a
         * paid additional cost poses (Bargain, or an OrCost's sacrifice half).
         * XMage hints it "to sacrifice", which is what the target carries
         * structurally, not a card name. */
        static boolean isSacrificeChoice(mage.target.Target target) {
            return target != null && target.getChooseHint() != null
                    && target.getChooseHint().startsWith("to sacrifice");
        }

        /** A craft cost's material ask: CraftCost.pay's target is a
         * TargetCardInGraveyardBattlefieldOrStack (CraftAbility.makeTarget)
         * with a minimum of two or more materials, and pay() calls
         * chooseTarget exactly ONCE -- unlike every normal multi-target ask,
         * which the game re-asks until its minimum is met. A census of the
         * XMage sets shows the only min>1 users of this target class are the
         * craft abilities, so this is the structural craft marker. */
        static boolean isCraftMaterialsAsk(mage.target.Target target) {
            return target != null
                    && target.getOriginalTarget() instanceof TargetCardInGraveyardBattlefieldOrStack
                    && target.getMinNumberOfTargets() > 1;
        }

        /** Whether the craft fill loop must call chooseTarget again: the ask
         * has fewer materials chosen than its minimum and the scripted queue
         * still holds an answer for it. An empty queue stops the loop: the
         * base player would otherwise fall into strict-mode AI auto-pick (or
         * fail on an unmatched definition), neither of which a scenario
         * authored. */
        static boolean craftMaterialsNeedFill(int chosen, int min, boolean queueNonEmpty) {
            return chosen < min && queueNonEmpty;
        }

        /** The decision a cast-time optional additional cost ask gets,
         * independent of any Game so the contract test can exercise all three
         * outcomes: a bargained cast PAYs it; otherwise the generator's own
         * leading "No" is CONSUMED by the caller so strict mode sees it used,
         * and so is a leading "Yes" on an "optionalcost" cast (gorge elected
         * the cost, so the consumed Yes pays it); every other ask is DECLINEd
         * (gorge casts for the mana cost only). */
        enum OptionalCostAnswer { PAY, CONSUME, DECLINE }

        static OptionalCostAnswer optionalAdditionalCostAnswer(boolean bargainedCast, boolean optionalCostCast, List<String> choices) {
            if (bargainedCast) {
                return OptionalCostAnswer.PAY;
            }
            if (!choices.isEmpty() && (choices.get(0).equals("No")
                    || (optionalCostCast && choices.get(0).equals("Yes")))) {
                return OptionalCostAnswer.CONSUME;
            }
            return OptionalCostAnswer.DECLINE;
        }

        /** The choice-queue entries that pay a teamwork tap with the
         * creatures a cast's recorded choose picks name: each matching pick
         * once, then a [choice_skip] when a candidate is left untapped (the
         * ask is open-ended, so XMage would otherwise ask for more). An
         * unmatched pick is left out, so strict mode names the miss. */
        static List<String> teamworkTapChoices(List<String> picks, List<UUID> candidates,
                BiPredicate<UUID, String> matches) {
            List<String> out = new ArrayList<>();
            List<UUID> left = new ArrayList<>(candidates);
            for (String pick : picks) {
                UUID id = costPickChoice(java.util.Collections.singletonList(pick), left, matches);
                if (id != null) {
                    out.add(pick);
                    left.remove(id);
                }
            }
            if (!out.isEmpty() && !left.isEmpty()) {
                out.add(TestPlayer.CHOICE_SKIP);
            }
            return out;
        }

        /** The candidate a cost's recorded choose picks name, or null when
         * none matches. Unlike attachmentChoice there is no automatic pick:
         * a sacrifice cost is paid only when the scenario named its object,
         * so a missing pick surfaces as the base's strict unused-command/
         * no-target error rather than a guess. */
        static UUID costPickChoice(List<String> picks, List<UUID> candidates,
                BiPredicate<UUID, String> matches) {
            for (String pick : picks) {
                for (UUID id : candidates) {
                    if (matches.test(id, pick)) {
                        return id;
                    }
                }
            }
            return null;
        }

        /** The candidate an XMage attach ask should take, or null when the ask
         * must fall through to the base player. A scripted answer at the queue
         * front is honored first and consumed, so an attachment ask never
         * silently overrides the scenario or leaves its answer queued for a
         * later decision. Automatic selection happens only for an unscripted
         * ask whose candidate set is uniquely determined. A scripted answer
         * that names no candidate is left in place, so the base's strict
         * unused-command check still reports the scenario error. */
        static UUID attachmentChoice(List<String> queue, List<UUID> candidates,
                BiPredicate<UUID, String> matches) {
            if (!queue.isEmpty() && !TestPlayer.TARGET_SKIP.equals(queue.get(0))) {
                String answer = queue.get(0);
                for (UUID id : candidates) {
                    if (matches.test(id, answer)) {
                        queue.remove(0);
                        return id;
                    }
                }
                return null;
            }
            if (queue.isEmpty() && candidates.size() == 1) {
                return candidates.get(0);
            }
            return null;
        }

        /** Detaches and returns the queue's suffix that starts at its next
         * "[target_skip]" so only the contiguous segment that precedes it is
         * visible. A skip at the front (consumed by this ask) or no skip at all
         * detaches nothing. */
        static List<String> hideAfterNextSkip(List<String> queue) {
            int next = queue.indexOf(TestPlayer.TARGET_SKIP);
            if (next <= 0) {
                return new ArrayList<>();
            }
            List<String> tail = queue.subList(next, queue.size());
            List<String> hidden = new ArrayList<>(tail);
            tail.clear();
            return hidden;
        }

        private boolean chooseTargetInSegment(Outcome outcome, mage.target.Target target, Ability source, Game game) {
            // An adjusted spell's target objects are rebuilt after cast setup,
            // so close an open slot before the ordinary automatic choices: a
            // slot whose candidates are exhausted never asks, so a skip
            // queued up front would be left unused and fail
            // assertAllCommandsUsed.
            boolean adjustedAsk = owner.isAdjustedSpellAsk(source, game);
            if (adjustedAsk) {
                closeCastAskedAgain(getTargets(), source,
                        target.getMinNumberOfTargets(), target.getMaxNumberOfTargets(), target.getSize());
            }
            mage.target.Target orig = target.getOriginalTarget();
            if (isAttachmentChoice(target)) {
                UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                List<UUID> candidates = new ArrayList<>(target.possibleTargets(abilityControllerId, source, game));
                UUID chosen = attachmentChoice(getTargets(), candidates,
                        (id, answer) -> hasObjectTargetNameOrAlias(game.getPermanent(id), answer));
                if (chosen != null) {
                    target.addTarget(chosen, source, game);
                    return true;
                }
            }
            if (target.getOriginalTarget() instanceof mage.target.common.TargetCardInHand) {
                UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                List<UUID> candidates = new ArrayList<>(target.possibleTargets(abilityControllerId, source, game));
                UUID chosen = handCardChoice(getTargets(), getChoices(), candidates,
                        target.getMinNumberOfTargets() - target.getSize(),
                        (id, answer) -> hasObjectTargetNameOrAlias(game.getCard(id), answer));
                if (chosen != null) {
                    target.addTarget(chosen, source, game);
                    return true;
                }
            }
            if (orig instanceof mage.target.common.TargetSpellOrPermanent
                    && !getTargets().isEmpty()
                    && !TestPlayer.TARGET_SKIP.equals(getTargets().get(0))) {
                // The base's own controller derivation (TestPlayer.chooseTarget):
                // the target's ability controller when set, else this choosing
                // player. A trigger made on another player's behalf must filter
                // its legal set by the ability's controller, not by the source
                // controller, or the permanent can fall outside possibleTargets.
                UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                Permanent match = findBattlefieldTarget(target, abilityControllerId, source, game, getTargets().get(0));
                if (match != null) {
                    target.addTarget(match.getId(), source, game);
                    getTargets().remove(0);
                    return true;
                }
            }
            if (isCraftMaterialsAsk(target)) {
                // CraftCost.pay calls chooseTarget exactly ONCE while
                // TestPlayer.chooseTarget consumes one queued definition per
                // call, so without this a craft with two or more materials
                // would exile only the first and leave the rest of the
                // scripted queue to fail assertAllCommandsUsed. Fill to the
                // ask's minimum, one definition per call; the chosen count is
                // the progress guard, since the base can return true having
                // only consumed a [target_skip].
                boolean chosen = super.chooseTarget(outcome, target, source, game);
                while (chosen && craftMaterialsNeedFill(target.getSize(),
                        target.getMinNumberOfTargets(), !getTargets().isEmpty())) {
                    int before = target.getSize();
                    chosen = super.chooseTarget(outcome, target, source, game);
                    if (target.getSize() == before) {
                        break;
                    }
                }
                return chosen;
            }
            if (!adjustedAsk) {
                // No automatic choice above claimed this ask, and the scenario
                // has no answer left: close an "up to" slot XMage actually
                // posed (max > min, minimum already selected), but only when
                // the ask is a cast ask. An unscripted ordinary resolution or
                // activation ask stays a strict miss, so a divergence is not
                // silently turned into a skip. This is the only skip the cast
                // step gets, since it queues just the recorded targets
                // (Hunters' Feast, Rabid Bite).
                closeCastAskedAgain(getTargets(), source,
                        target.getMinNumberOfTargets(), target.getMaxNumberOfTargets(), target.getSize());
            }
            if (target.getSize() < target.getMaxNumberOfTargets()) {
                // A lone legal candidate is not a choice: gorge settles it
                // without a decision, so no answer is scripted and strict mode
                // would stop at "Missing TARGET def" (see loneTargetChoice).
                UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                java.util.Collection<UUID> possible = target.possibleTargets(abilityControllerId, source, game);
                UUID lone = unscriptedLoneTarget(target, possible, game);
                if (lone != null) {
                    target.addTarget(lone, source, game);
                    return true;
                }
            }
            return super.chooseTarget(outcome, target, source, game);
        }

        /** The battlefield permanent in the target's own legal set that the
         * queued name or alias names, or null when the answer is not a
         * permanent (a spell on the stack, which the base handles). */
        private Permanent findBattlefieldTarget(mage.target.Target target, UUID abilityControllerId, Ability source, Game game, String name) {
            for (UUID id : target.possibleTargets(abilityControllerId, source, game)) {
                Permanent p = game.getPermanent(id);
                if (p == null || target.contains(id)) {
                    continue;
                }
                if (hasObjectTargetNameOrAlias(p, name)) {
                    return p;
                }
            }
            return null;
        }
    }

    /** Whether the scripted attach step is already satisfied: the bearer
     * already lists the attachment. An ETB attach trigger (Baseball Bat,
     * Coral Sword, Shredder's Armor, ...) attaches the same pair during
     * the resolve step, and Card.addAttachment refuses an attachment the
     * bearer already lists, while gorge's runner accepts the re-attach
     * as a no-op -- so the driver must too. */
    static boolean attachAlreadySatisfied(java.util.List<java.util.UUID> bearerAttachments,
            java.util.UUID attachmentId) {
        return bearerAttachments != null && bearerAttachments.contains(attachmentId);
    }

    public static void main(String[] args) throws Exception {
        if (args.length == 4 && "--serve".equals(args[0])) {
            ScenarioReplay r = new ScenarioReplay();
            serve(args[1], Long.parseLong(args[2]), Long.parseLong(args[3]), r);
            System.exit(0);
        }
        if (args.length != 2) {
            System.err.println("usage: ScenarioReplay <in.jsonl> <out.jsonl>");
            System.exit(2);
        }
        ScenarioReplay r = new ScenarioReplay();
        int n;
        try (BufferedReader in = new BufferedReader(new FileReader(args[0]));
             PrintWriter out = new PrintWriter(new FileWriter(args[1]))) {
            n = replayLines(r, in, out);
        }
        System.err.println("ScenarioReplay: " + n + " scenarios");
        System.exit(0);
    }

    /** A warm worker: ScenarioReplay --serve SOCK IDLE_S MAX_ITEMS. One JVM
     * (one card DB load, one class scan) replays many request files. It
     * listens on the Unix socket SOCK, one connection at a time: a client
     * sends IN<TAB>OUT, the worker answers "start" at once (a client tells a
     * live worker from one busy with an abandoned file by it), replays IN into
     * OUT through replayLines exactly as main(IN, OUT) does, with stdout and
     * stderr sent to OUT.log, and answers "ok N" or "err MESSAGE". Serving B
     * after A is main's run of A then B: every scenario starts from reset()
     * and reseeds RandomUtil. It exits after IDLE_S seconds without a client
     * or after MAX_ITEMS rows. scripts/oracle-warm.py is the client; the
     * same protocol as the Forge driver's forge.oracle.WarmServer. */
    static void serve(String sock, long idleS, long maxItems, ScenarioReplay r) throws Exception {
        java.nio.file.Path p = java.nio.file.Paths.get(sock);
        java.nio.file.Files.deleteIfExists(p);
        java.io.PrintStream out0 = System.out, err0 = System.err;
        long served = 0;
        try (java.nio.channels.ServerSocketChannel ss = java.nio.channels.ServerSocketChannel.open(java.net.StandardProtocolFamily.UNIX);
             java.nio.channels.Selector sel = java.nio.channels.Selector.open()) {
            ss.bind(java.net.UnixDomainSocketAddress.of(p));
            ss.configureBlocking(false);
            ss.register(sel, java.nio.channels.SelectionKey.OP_ACCEPT);
            err0.println("WarmServer: listening on " + sock);
            while (served < maxItems) {
                if (sel.select(Math.max(1, idleS) * 1000L) == 0) {
                    err0.println("WarmServer: idle " + idleS + "s; exiting");
                    break;
                }
                sel.selectedKeys().clear();
                java.nio.channels.SocketChannel c = ss.accept();
                if (c == null) {
                    continue;
                }
                try (c) {
                    c.configureBlocking(true);
                    BufferedReader rd = new BufferedReader(new java.io.InputStreamReader(
                            java.nio.channels.Channels.newInputStream(c), java.nio.charset.StandardCharsets.UTF_8));
                    java.io.Writer wr = new java.io.OutputStreamWriter(
                            java.nio.channels.Channels.newOutputStream(c), java.nio.charset.StandardCharsets.UTF_8);
                    String line = rd.readLine();
                    if (line == null) {
                        continue;
                    }
                    wr.write("start\n");
                    wr.flush();
                    String[] f = line.split("\t");
                    if (f.length != 2) {
                        wr.write("err bad request\n");
                        wr.flush();
                        continue;
                    }
                    String reply;
                    try (java.io.PrintStream log = new java.io.PrintStream(new java.io.FileOutputStream(f[1] + ".log"), true, "UTF-8")) {
                        System.setOut(log);
                        System.setErr(log);
                        int n;
                        try (BufferedReader in = new BufferedReader(new FileReader(f[0]));
                             PrintWriter out = new PrintWriter(new FileWriter(f[1]))) {
                            n = replayLines(r, in, out);
                        }
                        served += n;
                        log.println("ScenarioReplay: " + n + " scenarios warm served=" + served);
                        reply = "ok " + n;
                    } catch (Throwable t) {
                        String m = String.valueOf(t).replace('\n', ' ');
                        reply = "err " + (m.length() > 500 ? m.substring(0, 500) : m);
                    } finally {
                        System.setOut(out0);
                        System.setErr(err0);
                    }
                    wr.write(reply + "\n");
                    wr.flush();
                } catch (java.io.IOException e) {
                    err0.println("WarmServer: client: " + e);
                }
            }
        } finally {
            java.nio.file.Files.deleteIfExists(p);
        }
    }

    /** Replay every scenario line, writing one result row each. ANY throwable
     * while handling a scenario -- including in replay or in replayOnce's own
     * catch/alignment path -- becomes a harness row for that scenario id, and
     * the loop goes on to the next. Returns the row count. */
    static int replayLines(ScenarioReplay r, BufferedReader in, PrintWriter out) throws Exception {
        int n = 0;
        String line;
        while ((line = in.readLine()) != null) {
            if (line.trim().isEmpty()) {
                continue;
            }
            JsonObject sc = JsonParser.parseString(line).getAsJsonObject();
            long t0 = System.nanoTime();
            JsonObject res;
            // XMage shuffles and picks "at random" from one static Random.
            // Seed it from the scenario id so a rerun replays the same luck
            // and a verdict cannot flip between identical runs.
            RandomUtil.setSeed(scenarioSeed(sc));
            try {
                res = r.replay(sc);
            } catch (Throwable t) {
                res = harnessRow(sc, true, t);
            }
            if (res.has("harness") && res.get("harness").isJsonPrimitive()) {
                res.addProperty("harness", stableMessage(res.get("harness").getAsString()));
            }
            res.addProperty("ms", (System.nanoTime() - t0) / 1_000_000);
            out.println(GSON.toJson(res));
            out.flush();
            n++;
        }
        return n;
    }

    /** The per-scenario RNG seed: the id's (or, without one, the name's)
     * String.hashCode, which the JLS fixes, so it is the same on every JVM. */
    static long scenarioSeed(JsonObject sc) {
        String key = sc.has("id") && sc.get("id").isJsonPrimitive() ? sc.get("id").getAsString() : str(sc, "name");
        return key == null ? 0L : key.hashCode();
    }

    private static final java.util.regex.Pattern OBJECT_ID = java.util.regex.Pattern.compile(" object_id='[0-9a-f-]+'");
    private static final java.util.regex.Pattern SHORT_ID = java.util.regex.Pattern.compile(" \\[[0-9a-f]{3}\\]");

    /** A harness message with XMage's per-run object ids (UUIDs from
     * UUID.randomUUID, which no seed reaches) removed: the full object_id
     * attribute and the three-hex short id XMage prints after a name. The
     * message is otherwise unchanged, so a rerun writes the same verdict. */
    static String stableMessage(String msg) {
        return SHORT_ID.matcher(OBJECT_ID.matcher(msg).replaceAll("")).replaceAll("");
    }

    /** Null-safe read of a scenario's "steps". A missing key, an explicit
     * JSON null or any non-array value yields an empty array instead of the
     * ClassCastException that used to escape replayOnce and kill the JVM, so
     * a malformed scenario fails its own row and the batch goes on. */
    static JsonArray steps(JsonObject sc) {
        JsonElement e = sc.get("steps");
        return e != null && e.isJsonArray() ? e.getAsJsonArray() : new JsonArray();
    }

    /** The result row a scenario gets when handling it throws -- including a
     * throw inside replayOnce's own catch/alignment path. Same shape as an
     * ordinary harness error so the comparator reads it unchanged. */
    private static JsonObject harnessRow(JsonObject sc, boolean strict, Throwable t) {
        JsonObject res = new JsonObject();
        res.addProperty("strict", strict);
        res.addProperty("name", str(sc, "name"));
        if (sc.has("id")) {
            res.add("id", sc.get("id"));
        }
        String msg = t.getClass().getSimpleName() + ": " + t.getMessage();
        res.addProperty("harness", msg.length() > 800 ? msg.substring(0, 800) : msg);
        res.add("snapshots", new JsonArray());
        return res;
    }

    /**
     * Strict first: every XMage decision must be scripted. When XMage poses
     * a decision the script (derived from gorge's decisions) does not cover,
     * the scenario is replayed with strict mode off so XMage's own player
     * answers it, and the result says so: the outcome is still compared.
     * Leftover scripted answers alone (a decision gorge posed and XMage did
     * not) do not void a run that reached every checkpoint.
     */
    JsonObject replay(JsonObject sc) {
        JsonObject res;
        try {
            res = replayOnce(sc, true);
        } catch (Throwable t) {
            return harnessRow(sc, true, t);
        }
        String h = res.has("harness") ? res.get("harness").getAsString() : "";
        if (h.contains("Missing") && h.contains("def for turn")
                && trailingStrictMiss(res.getAsJsonArray("snapshots").size(), steps(sc).size())) {
            // Every checkpoint was captured under strict mode before XMage
            // posed the unscripted ask: the driver plays on past gorge's last
            // checkpoint (to the stop step), and an ask there -- a trigger
            // the window's last pass lets resolve -- is outside the item.
            // gorge stopped with that decision pending and unlogged, so
            // there is no answer to script and nothing compares it. The
            // strict snapshots are the row; the miss is kept for diagnosis.
            res.remove("harness");
            res.addProperty("trailing_miss", h.length() > 300 ? h.substring(0, 300) : h);
            return res;
        }
        if (h.contains("Missing") && h.contains("def for turn")) {
            JsonObject loose;
            try {
                loose = replayOnce(sc, false);
            } catch (Throwable t) {
                return harnessRow(sc, false, t);
            }
            loose.addProperty("strict_miss", h.length() > 300 ? h.substring(0, 300) : h);
            return loose;
        }
        return res;
    }

    /** Whether a strict run that stopped at an unscripted ask had already
     * captured every checkpoint: the setup snapshot plus one per step. */
    static boolean trailingStrictMiss(int snapshots, int steps) {
        return steps > 0 && snapshots == steps + 1;
    }

    /** Whether every scenario checkpoint has been captured, so the replay is
     * now in the trailing play past gorge's last checkpoint. An unscripted
     * decision taken here is outside the compared window; before it, the
     * decision is part of the item and the driver must not invent an answer.
     * Snapshots are the setup snapshot plus one per captured step. */
    boolean pastLastCheckpoint() {
        return snaps.size() >= steps(sc0).size() + 1;
    }

    /** The cards the search auto-pick took, in ask order (test hook). */
    List<String> searchPicks() {
        return searchPicks;
    }

    // Match encoding/json's *int field: absent/null defaults to 1; otherwise
    // require an integer JSON number. Gson's getAsInt truncates fractions and
    // wraps oversized numbers, potentially replaying a different turn.
    private static int scenarioTurn(JsonObject sc) {
        JsonElement value = sc.get("turn");
        if (value == null || value.isJsonNull()) {
            return 1;
        }
        try {
            if (value.isJsonPrimitive() && value.getAsJsonPrimitive().isNumber()
                    && value.getAsString().matches("-?[0-9]+")) {
                int turn = Integer.parseInt(value.getAsString());
                if (turn >= 1 && turn <= 100) {
                    return turn;
                }
            }
        } catch (NumberFormatException ignored) {
            // Overflow is invalid, not an invitation to wrap to another turn.
        }
        throw new IllegalArgumentException("invalid scenario turn " + value + " (want integer 1..100)");
    }

    /** Test hook: the count of counters of the given type on the named card
     * in exile in the game the last replayOnce ran (-1 when no such card).
     * The snapshot carries exile names only, so the Suspend test reads the
     * time counters here. */
    int exileCounters(String cardName, String counterType) {
        for (Card c : currentGame.getExile().getAllCards(currentGame)) {
            if (cardName.equals(c.getName())) {
                return c.getCounters(currentGame).getCount(counterType);
            }
        }
        return -1;
    }

    JsonObject replayOnce(JsonObject sc, boolean strict) {
        JsonObject res = new JsonObject();
        res.addProperty("strict", strict);
        res.addProperty("name", str(sc, "name"));
        if (sc.has("id")) {
            res.add("id", sc.get("id"));
        }
        snaps.clear();
        searchPicks.clear();
        boolean endTurnScenario = false;
        // Preserve the exact actions each step queues (including mana and
        // combat sub-actions), not just its final checkpoint.
        List<List<PlayerAction>> queuedA = new ArrayList<>();
        List<List<PlayerAction>> queuedB = new ArrayList<>();
        List<List<String>> choicesA = new ArrayList<>();
        List<List<String>> choicesB = new ArrayList<>();
        List<List<String>> targetsA = new ArrayList<>();
        List<List<String>> targetsB = new ArrayList<>();
        try {
            seats = scenarioPlayers(sc);
            reset();
            skipInitShuffling();
            setStrictChooseMode(strict);
            sc0 = sc;
            // A cast-mode election is scoped to one scenario: every step's
            // actions are queued before execute(), so the cast case below
            // records it during queueing and the asks read it during execute.
            // Reset here so an earlier scenario's Bargain cannot answer this
            // one's optional additional costs.
            bargainedCast = false;
            optionalCostCast = false;
            kickedCast = false;
            alternativeCostCast = false;
            evokedCast = false;
            copyCostCast = null;
            giftPromisedCast = false;
            followedPassPosition = false;
            settleCheckpoint = false;
            gorgeName = str(sc, "card");
            xmageName = str(sc, "xmage_name");
            endTurnScenario = hasEndTurnEffect(xmageName.isEmpty() ? gorgeName : xmageName);
            cast.clear();
            adjustedCasts.clear();
            faceDownRefs.clear();
            exactAttacks.clear();
            exactBlocks.clear();
            refAlias.clear();
            phase = MAIN;
            TURN = scenarioTurn(sc);
            turn = TURN;
            activeSeat = (TURN - 1) % 2;
            attackAdvancedTurn = false;
            resetOccurrences();
            seedNaturalStepsBeforeStart(TURN);
            enterQueuedStep(TURN, MAIN, false);
            queueSetupChoices(sc);
            build(sc);
            if (TURN == 1) {
                // Turn 1's first priority is in upkeep, before any gameplay
                // entry (Bitterblossom's token is still on the stack). Age
                // only the seeded setup permanents recorded by build().
                java.util.Map<String, Integer> seeded = new java.util.HashMap<>(setupBattlefield);
                runCode("setup entry history", TURN, PhaseStep.UPKEEP, playerA,
                        (info, p, g) -> clearSetupEntryHistory(g, seeded));
            }
            runCode("setup", TURN, MAIN, playerA, (info, p, g) -> {
                applySetupState(g);
                registerAliases(g);
                snaps.add(snapshot(info, g));
            });
            tagQueuedActions(0, 0);
            mergePassPositions(sc);
            JsonArray steps = steps(sc);
            JsonArray xans = sc.has("xmage_answers") && sc.get("xmage_answers").isJsonArray()
                    ? sc.getAsJsonArray("xmage_answers") : new JsonArray();
            splitScripted = xans.toString().contains("^X=");
            xabilities = sc.has("xmage_ability") && sc.get("xmage_ability").isJsonArray()
                    ? sc.getAsJsonArray("xmage_ability") : new JsonArray();
            xtargetSkips = readTargetSkips(sc, steps);
            for (int i = 0; i < steps.size(); i++) {
                JsonObject st = steps.get(i).getAsJsonObject();
                String op = str(st, "op");
                int beforeChoicesA = playerA.getChoices().size();
                int beforeChoicesB = playerB.getChoices().size();
                int beforeTargetsA = playerA.getTargets().size();
                int beforeTargetsB = playerB.getTargets().size();
                String payment = paymentAt(sc, i);
                boolean inferWaterbendX = "activate".equals(op)
                        && xabilityAt(i).startsWith("waterbend {X}");
                if (i < xans.size() && xans.get(i).isJsonArray()) {
                    // Queue answers by their recorded seat before registering
                    // actions. XMage consumes these queues while executing the
                    // cast; the action registration itself is deferred.
                    scripted(xans.get(i).getAsJsonArray(), payment, inferWaterbendX);
                }
                int beforeA = playerA.getActions().size();
                int beforeB = playerB.getActions().size();
                queuePassCommands(i);
                tagQueuedActions(beforeA, beforeB);
                stepPayment = payment;
                step(st, op, i);
                String cp = "step " + i + " (" + op + ")";
                final PhaseStep reportAs = checkpointStepAs;
                checkpointStepAs = null;
                // The pass into a declaration step is a non-priority pending
                // decision (CR 508.1, 509.1, 117.3a) only when the declaration
                // has a legal option. With none, gorge records the forced empty
                // declaration inside the step transition and its checkpoint is
                // the live priority that follows, which does offer the holder's
                // options. Covers both the checkpoint moved before the
                // declaration (reportAs: a recorded declaration proves at least
                // one option) and one taken in the step when the scenario never
                // declares (the mm-menon/icetill shape, decided from the game
                // state at that checkpoint).
                final PhaseStep declaration = reportAs != null
                        ? reportAs : declarationCheckpoint(steps, i, seats);
                // A forced resolution queued during this step (waitStackResolved
                // override) must settle before the snapshot. Capture the flag
                // now: the whole action queue is built before execute(), so the
                // field's later value cannot describe this checkpoint.
                final boolean settleHere = settleCheckpoint;
                settleCheckpoint = false;
                // A pass that only hands priority on runs its checkpoint before
                // XMage moves the priority (TestPlayer's pass returns first), so
                // the snapshot must name the seat that receives priority
                // (CR 117.3d), not the passer the game still reports. Captured
                // here: `i` and the step array are only final for this step.
                final int passSeat = seatOfStep(steps, i);
                final boolean handsPriorityOn = passHandsPriorityOn(steps, i, activeSeat, seats);
                if (i == steps.size() - 1) {
                    // The last step's checkpoint is the run's cut; remember
                    // where it was queued so an untap ask there (CR 502.3)
                    // can flush it and pause the row complete.
                    cutCheckpointPhase = phase;
                    cutCheckpointTurn = turn;
                }
                runCode(cp, turn, phase, checkpointSeat(op, st, i), (info, p, g) -> {
                    if (settleHere) {
                        g.checkStateAndTriggered();
                    }
                    applyDeclarationStepEnd(reportAs, g);
                    final boolean declarationPending = declaration != null
                            && (reportAs != null || declarationHadOptions(declaration, g));
                    JsonObject snap = snapshot(info, g,
                            handsPriorityOn ? nextPriorityPlayer(g, passSeat) : null, declarationPending);
                    if (reportAs != null) {
                        snap.addProperty("step", reportAs.name());
                    }
                    snaps.add(snap);
                });
                if (passHandsOffBeforeLaterAction(steps, i, activeSeat, seats)) {
                    // After the checkpoint, so the snapshot still sees the
                    // priority the seat holds; before the seat's later actions.
                    runCode("pass hands priority on before the seat's next action", turn, phase,
                            seat(seatOfStep(steps, i)), (info, pl, g) -> pl.pass(g));
                }
                if (!payment.isEmpty()) {
                    TestPlayer payer = seat(st.has("seat") ? st.get("seat").getAsInt() : 0);
                    queuePaymentEnd(payer);
                }
                tagQueuedActions(beforeA, beforeB);
                queuedA.add(new ArrayList<>(playerA.getActions().subList(beforeA, playerA.getActions().size())));
                queuedB.add(new ArrayList<>(playerB.getActions().subList(beforeB, playerB.getActions().size())));
                choicesA.add(new ArrayList<>(playerA.getChoices().subList(beforeChoicesA, playerA.getChoices().size())));
                choicesB.add(new ArrayList<>(playerB.getChoices().subList(beforeChoicesB, playerB.getChoices().size())));
                targetsA.add(new ArrayList<>(playerA.getTargets().subList(beforeTargetsA, playerA.getTargets().size())));
                targetsB.add(new ArrayList<>(playerB.getTargets().subList(beforeTargetsB, playerB.getTargets().size())));
            }
            // Only EndTurn scenarios need the extended boundary. Ordinary
            // replays retain the original turn-1 stop and cannot encounter
            // unrelated turn-2 upkeep triggers or decisions.
            // A recorded game's window ends at the step its last pass entered
            // (gorge's final checkpoint). Playing on to the turn's end step
            // lets XMage's own AI pass through later steps that gorge never
            // reached, and a trigger there (a beginning-of-combat "up to two
            // target creatures") asks a target nobody scripted: "Missing
            // TARGET def". So stop at that step instead.
            if (!endTurnScenario && followedPassPosition) {
                // setStopAt names a PhaseStep, which XMage matches in the
                // first occurrence of that step in the turn. A window that
                // ends in a later occurrence (the extra combat/main of CR
                // 500.8, an additional end step) would stop before the
                // checkpoints it was cut at, so stop from that occurrence's
                // own priority and keep the next turn's untap as the
                // fallback boundary.
                int stopOccurrence = queuedOccurrence(turn, phase);
                if (stopOccurrence > 1) {
                    runCode("stop at " + phase + " occurrence " + stopOccurrence, turn, phase, playerA,
                            (info, p, g) -> g.pause());
                    tagQueuedActions(0, 0);
                    setStopAt(turn + 1, PhaseStep.UNTAP);
                } else if (phase == PhaseStep.UNTAP) {
                    // A pass cut at the next turn's untap step: the untap
                    // step grants no priority (CR 500.4), so
                    // checkStopOnTurnOption would pause at the turn's very
                    // start and the CR 502.3 optional-untap ask that holds
                    // the cut checkpoint would never fire. The ask itself
                    // pauses the game (chooseUse's untap-election branch,
                    // after flushUntapAskRunCodes); the turn's upkeep is the
                    // fallback boundary, which no checkpoint queues actions
                    // for -- an engine gap that never reaches the ask shows
                    // as an unused action there.
                    setStopAt(turn, PhaseStep.UPKEEP);
                } else {
                    setStopAt(turn, phase);
                }
            } else {
                setStopAt(endTurnScenario ? turn + 1 : turn,
                        endTurnScenario ? PhaseStep.UPKEEP : PhaseStep.END_TURN);
            }
            execute();
        } catch (Throwable t) {
            String msg = t.getClass().getSimpleName() + ": " + t.getMessage();
            int stepCount = steps(sc).size();
            int completedSteps = snaps.size() - 1;
            if (endTurnScenario && unusedActionCount(msg) >= 0
                    && completedSteps >= 0 && completedSteps < stepCount
                    && currentGame != null && currentGame.getTurnNum() > turn
                    && skippedActionsMatch(completedSteps, queuedA, queuedB)
                    && skippedAnswersMatch(completedSteps, choicesA, choicesB, targetsA, targetsB)) {
                // Only the actions for skipped steps remain. An unconsumed
                // action from an earlier step is still a harness error. Record
                // every skipped label from the post-turn state for alignment.
                for (int skipped = completedSteps; skipped < stepCount; skipped++) {
                    String op = str(steps(sc).get(skipped).getAsJsonObject(), "op");
                    snaps.add(snapshot("step " + skipped + " (" + op + ")", currentGame));
                }
                msg = null;
            }
            if (msg != null && currentGame != null && currentGame.hasEnded() && isLeftoverAssertion(msg)
                    && completedSteps >= 0 && completedSteps < stepCount
                    && leftoverIsQueuedSuffix(completedSteps, queuedA, queuedB, choicesA, choicesB, targetsA, targetsB)) {
                // The game ended mid-scenario (a player lost: Marina
                // Vendrell's Grimoire's empty-hand loss, The Millennium
                // Calendar's 1,000 life). XMage stops there, so the steps
                // after it never ran; gorge's runner records every remaining
                // checkpoint at the ended state (over: true). Do the same:
                // snapshot the final state for each remaining label, and drop
                // only the leftovers those unreached steps queued.
                for (int skipped = completedSteps; skipped < stepCount; skipped++) {
                    String op = str(steps(sc).get(skipped).getAsJsonObject(), "op");
                    snaps.add(snapshot("step " + skipped + " (" + op + ")", currentGame));
                }
                msg = null;
            }
            if (msg != null) {
                if (!xmageName.isEmpty()) {
                    // Name the card the way the scenario (and gorge) does.
                    msg = msg.replace(xmageName, gorgeName);
                }
                // A command XMage could not resolve is only half a diagnosis:
                // name the battlefield it searched, so a replay row carries
                // what was actually placed under each seat (agent
                // 20261009T041408Z cluster C3: the staged back-face combat
                // rows XMage cannot find under their back name).
                if (msg.contains("No permanents found called") && currentGame != null) {
                    msg = msg + " (battlefield: " + battlefieldListing() + ")";
                }
                // assertAllCommandsUsed names only the count of leftover
                // actions; name the actions themselves, so a replay row says
                // which queued ask the game never reached (agent
                // 20261009T041408Z cluster C4).
                if (msg.contains("must have 0 actions but found") && currentGame != null) {
                    msg = msg + " (leftover: " + leftoverActionNames() + ")";
                }
                int want = stepCount + 1;
                if (snaps.size() == want && msg.contains("Count are not equal")) {
                    res.addProperty("leftover", msg.length() > 300 ? msg.substring(0, 300) : msg);
                } else {
                    res.addProperty("harness", msg.length() > 800 ? msg.substring(0, 800) : msg);
                }
            }
        }
        JsonArray arr = new JsonArray();
        for (JsonObject s : snaps) {
            arr.add(s);
        }
        res.add("snapshots", arr);
        return res;
    }

    /**
     * The seat whose code action carries a step's checkpoint. XMage runs one
     * player's queued actions at a priority before the other player acts, so
     * a checkpoint queued on the wrong seat sees the wrong side of the step:
     *
     *  - pass_to only moves the clock. Its checkpoint belongs to the seat that
     *    has priority first on the turn it reached, ahead of that step's
     *    actions; on seat 0 it would run after the opponent's NEXT cast or
     *    loyalty activation (Found Footage's hand, Ajani's loyalty).
     *  - resolve by seat 1 waits on seat 1's queue (waitStackResolved), so a
     *    checkpoint on seat 0 would run while the spell is still on the stack.
     *  - in a recorded run, an action (a cast, activation or land play) of the
     *    non-active seat checkpoints on that seat. XMage runs the whole queue
     *    of the seat that holds priority before the opponent acts, so a
     *    checkpoint on the active seat would run after every later action of
     *    that seat in the same turn and step (ECL Impolite Entrance: p1's
     *    three mana activations and flash cast were all visible at the
     *    step-10 checkpoint, though gorge cast the flash at step 13). The
     *    actor's own queue puts its checkpoint between its actions, holding
     *    the later one for its own step.
     *  - a recorded pass that resolves or moves the step checkpoints on the
     *    active seat, which has priority after the resolution or at the step
     *    it reached; any other recorded pass checkpoints on the seat that
     *    passed, which still holds priority.
     *
     *  - a declaration (attack, block) checkpoints on the ACTIVE seat. XMage
     *    makes the declaration in the step's beginStep, and the step's first
     *    priority then goes to the active seat: on seat 1's turn that is
     *    playerB, so a checkpoint left on playerA runs only after playerB's
     *    next pass has moved the game on, and the two snapshots cross (the
     *    pass lands at the declaration's index). On seat 0's turn this is
     *    playerA, unchanged.
     *
     * Every other op keeps the seat-0 fallback, which acts last on the turn it
     * is on; attach is likewise queued on playerA.
     */
    private TestPlayer checkpointSeat(String op, JsonObject st, int stepIdx) {
        if (op.equals("pass") && isPassRun(steps(sc0), stepIdx)) {
            JsonObject pos = st.getAsJsonObject("xpos");
            if (posFlag(pos, "moved") || posFlag(pos, "resolves")) {
                // The active seat acts first at the step the pass reached (or
                // gets priority back once the top object resolves), and XMage
                // runs its whole queue there before the other seat acts.
                return seat(activeSeat);
            }
            // Otherwise the recorded seat still holds priority: its checkpoint
            // is a no-op between its own queued actions and checkpoints.
            return seat(seatOfStep(steps(sc0), stepIdx));
        }
        if (op.equals("pass") && isActiveP1Pair(steps(sc0), stepIdx, activeSeat)
                && passAction(steps(sc0), stepIdx) == PASS_PAIR_SECOND
                && !pairEndsMidResolution(steps(sc0), stepIdx)) {
            // The active seat 1's pair resolves on seat 1's queue, so its
            // second checkpoint must follow that wait there.
            return seat(activeSeat);
        }
        if (op.equals("attack") || op.equals("block")) {
            // The declaration itself is beginStep work on the declaring seat,
            // not a queued action of that seat; the checkpoint after it must
            // ride the queue that holds the step's first priority, which is
            // the active seat's. Registering it there also keeps it ahead of
            // the active seat's following pass checkpoint, so the snapshots
            // stay in step order. Checked before the recorded-action rule:
            // attack and block are never actions of a seat's queue.
            return seat(activeSeat);
        }
        if (sc0 != null && opensOnOtherSeat(steps(sc0), stepIdx, activeSeat)) {
            // A recorded game's action of the seat that is not active runs on
            // that seat's queue, and so must its checkpoint: a checkpoint on
            // the active seat runs only after the whole run of that seat's
            // actions, so every one of them would show the last one's stack.
            return seat(seatOfStep(steps(sc0), stepIdx));
        }
        if (op.equals("pass_to")) {
            return seat(activeSeat);
        }
        if (op.equals("resolve")) {
            return seat(st.has("seat") ? st.get("seat").getAsInt() : 0);
        }
        if (sc0 != null && isRecordedAction(steps(sc0), stepIdx)) {
            // A recorded cast, activation or land play is queued on the seat
            // that takes it, and that seat's queue carries its checkpoint
            // between its other actions. attach is queued on playerA and
            // keeps the fallback below.
            return seat(seatOfStep(steps(sc0), stepIdx));
        }
        return playerA;
    }

    private boolean skippedActionsMatch(int first, List<List<PlayerAction>> queuedA,
                                        List<List<PlayerAction>> queuedB) {
        if (queuedA.size() != queuedB.size() || first >= queuedA.size()) {
            return false;
        }
        List<PlayerAction> remainingA = new ArrayList<>();
        List<PlayerAction> remainingB = new ArrayList<>();
        for (int i = first; i < queuedA.size(); i++) {
            remainingA.addAll(queuedA.get(i));
            remainingB.addAll(queuedB.get(i));
        }
        // XMage may copy TestPlayer, but its copy retains the same PlayerAction
        // objects. Match the entire queue on both seats, not just its length.
        return ((TestPlayer) currentGame.getPlayer(playerA.getId())).getActions().equals(remainingA)
                && ((TestPlayer) currentGame.getPlayer(playerB.getId())).getActions().equals(remainingB);
    }

    private boolean skippedAnswersMatch(int first, List<List<String>> choicesA, List<List<String>> choicesB,
                                        List<List<String>> targetsA, List<List<String>> targetsB) {
        TestPlayer actualA = (TestPlayer) currentGame.getPlayer(playerA.getId());
        TestPlayer actualB = (TestPlayer) currentGame.getPlayer(playerB.getId());
        // assertAllCommandsUsed checks actions first. Do not hide its failure
        // if a completed step also left an unused choice or target behind.
        return remainingIsSkipped(actualA.getChoices(), choicesA, first)
                && remainingIsSkipped(actualB.getChoices(), choicesB, first)
                && remainingIsSkipped(actualA.getTargets(), targetsA, first)
                && remainingIsSkipped(actualB.getTargets(), targetsB, first);
    }

    private static <T> boolean remainingIsSkipped(List<T> actual, List<List<T>> queued, int first) {
        if (first < 0 || first >= queued.size()) {
            return false;
        }
        List<T> expected = new ArrayList<>();
        for (int i = first; i < queued.size(); i++) {
            expected.addAll(queued.get(i));
        }
        return actual.equals(expected);
    }

    private static int unusedActionCount(String message) {
        String marker = "must have 0 actions but found ";
        if (message == null || !message.contains(marker)) {
            return -1;
        }
        try {
            return Integer.parseInt(message.substring(message.indexOf(marker) + marker.length()).trim());
        } catch (NumberFormatException ignored) {
            return -1;
        }
    }

    /** Whether a failure is assertAllCommandsUsed's: a queued action,
     * choice or target nothing consumed. */
    static boolean isLeftoverAssertion(String message) {
        return message != null && (message.contains("must have 0 actions but found")
                || message.contains("Count are not equal"));
    }

    /** At game over: every seat's leftover actions, choices and targets are
     * a tail of what steps {@code first}.. queued -- the step the game ended
     * in may have consumed its head -- so nothing an earlier, completed step
     * queued is hidden by the game-over row. */
    private boolean leftoverIsQueuedSuffix(int first, List<List<PlayerAction>> queuedA, List<List<PlayerAction>> queuedB,
                                           List<List<String>> choicesA, List<List<String>> choicesB,
                                           List<List<String>> targetsA, List<List<String>> targetsB) {
        TestPlayer actualA = (TestPlayer) currentGame.getPlayer(playerA.getId());
        TestPlayer actualB = (TestPlayer) currentGame.getPlayer(playerB.getId());
        return isQueuedSuffix(actualA.getActions(), queuedA, first) && isQueuedSuffix(actualB.getActions(), queuedB, first)
                && isQueuedSuffix(actualA.getChoices(), choicesA, first) && isQueuedSuffix(actualB.getChoices(), choicesB, first)
                && isQueuedSuffix(actualA.getTargets(), targetsA, first) && isQueuedSuffix(actualB.getTargets(), targetsB, first);
    }

    /** Whether {@code actual} is a tail of the concatenation of
     * {@code queued[first..]}. */
    static <T> boolean isQueuedSuffix(List<T> actual, List<List<T>> queued, int first) {
        if (first < 0 || first >= queued.size()) {
            return false;
        }
        List<T> expected = new ArrayList<>();
        for (int i = first; i < queued.size(); i++) {
            expected.addAll(queued.get(i));
        }
        return actual.size() <= expected.size()
                && expected.subList(expected.size() - actual.size(), expected.size()).equals(actual);
    }

    private static boolean hasEndTurnEffect(String name) {
        if (name == null || name.isEmpty()) {
            return false;
        }
        CardInfo info = CardRepository.instance.findCard(name);
        if (info == null) {
            return false;
        }
        Card card = info.createCard();
        return card.getSpellAbility() != null && abilityHasEndTurnEffect(card.getSpellAbility());
    }

    private static boolean abilityHasEndTurnEffect(Ability ability) {
        if (ability.getEffects().stream().anyMatch(EndTurnEffect.class::isInstance)) {
            return true;
        }
        for (Ability sub : ability.getSubAbilities()) {
            if (abilityHasEndTurnEffect(sub)) {
                return true;
            }
        }
        return false;
    }

    // ---- setup -----------------------------------------------------------

    /**
     * CardTestPlayerAPIImpl cheats setup permanents onto the battlefield
     * before the game starts, but XMage leaves their turnsOnBattlefield at
     * zero (so EnteredThisTurnPredicate matches them) and the ETB watcher
     * shifts them into its last-turn history. Run only at turn 1's first
     * priority, before any gameplay entry: age the seeded permanents and
     * drop their last-turn history. Both passes match the recorded
     * (controller id, XMage name) multiset, so a permanent that genuinely
     * entered this turn -- an upkeep token -- keeps its zero age and its
     * watcher entry. At a later requested turn the setup permanents age
     * naturally, so this is not called.
     */
    private static void clearSetupEntryHistory(Game game, java.util.Map<String, Integer> seeded) {
        try {
            // Remaining seeded (controller|name) counts, decremented as
            // permanents are matched so duplicate names each age exactly once.
            java.util.Map<String, Integer> remaining = new java.util.HashMap<>(seeded);
            java.lang.reflect.Field turns = mage.game.permanent.PermanentImpl.class
                    .getDeclaredField("turnsOnBattlefield");
            turns.setAccessible(true);
            for (Permanent permanent : game.getBattlefield().getAllPermanents()) {
                String key = permanent.getControllerId() + "|" + permanent.getName();
                Integer left = remaining.get(key);
                if (left == null || left <= 0) {
                    continue; // genuinely entered this turn, or not a setup card
                }
                remaining.put(key, left - 1);
                turns.setInt(permanent, Math.max(1, permanent.getTurnsOnBattlefield()));
            }
            PermanentsEnteredBattlefieldWatcher watcher = game.getState()
                    .getWatcher(PermanentsEnteredBattlefieldWatcher.class);
            if (watcher != null) {
                java.lang.reflect.Field last = PermanentsEnteredBattlefieldWatcher.class
                        .getDeclaredField("enteringBattlefieldLastTurn");
                last.setAccessible(true);
                @SuppressWarnings("unchecked")
                java.util.Map<java.util.UUID, java.util.List<Permanent>> lastTurn =
                        (java.util.Map<java.util.UUID, java.util.List<Permanent>>) last.get(watcher);
                // Drop only the seeded setup entries recorded pre-game; any
                // other last-turn entry is left for its owning card.
                java.util.Map<String, Integer> watcherRemaining = new java.util.HashMap<>(seeded);
                for (java.util.List<Permanent> list : lastTurn.values()) {
                    java.util.Iterator<Permanent> it = list.iterator();
                    while (it.hasNext()) {
                        Permanent entry = it.next();
                        String key = entry.getControllerId() + "|" + entry.getName();
                        Integer left = watcherRemaining.get(key);
                        if (left != null && left > 0) {
                            watcherRemaining.put(key, left - 1);
                            it.remove();
                        }
                    }
                }
            }
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException("cannot normalize setup entry history", e);
        }
    }

    private TestPlayer seat(int i) {
        if (i == 2 && seats == 3) {
            return playerC;
        }
        return i == 0 ? playerA : playerB;
    }

    /** The scenario seat index of one of this game's TestPlayers. */
    private int seatOfPlayer(TestPlayer p) {
        return p == playerA ? 0 : p == playerB ? 1 : 2;
    }

    private void build(JsonObject sc) {
        buildCounts.clear();
        frontCounts.clear();
        setupBattlefield.clear();
        setupTokensPlaced = false;
        setupCountersPlaced = false;
        setupAttachedPlaced = false;
        setupPermanentIds.clear();
        setupNames.clear();
        placedNames.clear();
        abilityAliasSeq = 0;
        backFaceNames.clear();
        String format = str(sc, "format");
        if (!format.isEmpty() && !format.equals("constructed")) {
            throw new IllegalArgumentException("unsupported format " + format);
        }
        JsonObject setup = sc.has("setup") ? sc.getAsJsonObject("setup") : new JsonObject();
        for (int i = 0; i < seats; i++) {
            TestPlayer p = seat(i);
            removeAllCardsFromLibrary(p);
            removeAllCardsFromHand(p);
            JsonObject s = setup.has("p" + i) ? setup.getAsJsonObject("p" + i) : new JsonObject();
            int named = 0;
            named += add(s, "battlefield", Zone.BATTLEFIELD, p);
            named += add(s, "hand", Zone.HAND, p);
            named += add(s, "graveyard", Zone.GRAVEYARD, p);
            named += add(s, "exile", Zone.EXILED, p);
            named += add(s, "library", Zone.LIBRARY, p);
            // Setup tokens are placed by the game's pre-start hook; fail an
            // unmapped stem now and record them so clearSetupEntryHistory ages
            // them with the seeded cards (it copies setupBattlefield below).
            for (JsonObject t : setupTokenEntries(s)) {
                setupBattlefield.merge(p.getId() + "|" + setupToken(t.get("script").getAsString()).getName(),
                        1, Integer::sum);
            }
            if (s.has("command")) {
                throw new IllegalArgumentException("command zone setup unsupported");
            }
            // library_top: first = top. addCard(LIBRARY) puts on top, so add
            // them last, bottom-most first, after the filler.
            List<String> top = names(s, "library_top");
            named += top.size();
            int filler = Math.max(0, 40 - named);
            // Same draw reserve as oracleRun.build: preserve turn 1 exactly,
            // otherwise allow TURN/2 draws plus one card at the checkpoint,
            // even when all named setup cards are outside the library.
            if (TURN > 1) {
                filler = Math.max(filler, TURN / 2 + 1);
            }
            if (filler > 0) {
                addCard(Zone.LIBRARY, p, "Wastes", filler);
            }
            for (int k = top.size() - 1; k >= 0; k--) {
                addCard(Zone.LIBRARY, p, top.get(k));
            }
        }
    }

    /** Re-applies each seat's requested setup life after the cheat placements.
     * The gorge runner places every setup entry first and only then emits the
     * seat's LifeChange (oracleRun.build); XMage runs the queued "life:" command
     * before the cards, so an as-enters pay (a shock land's 2 life) would
     * otherwise land after the requested value. The hook runs before super.start,
     * so no trigger can resolve between the payment and this write. */
    private void applySetupLife() {
        JsonObject setup = sc0.has("setup") ? sc0.getAsJsonObject("setup") : new JsonObject();
        for (int i = 0; i < seats; i++) {
            if (!setup.has("p" + i)) {
                continue;
            }
            JsonObject s = setup.getAsJsonObject("p" + i);
            if (s.has("life")) {
                seat(i).initLife(s.get("life").getAsInt());
            }
        }
    }

    /** The setup's per-seat "tapped_after_untap" and "speed" (gorge's runner emits
     * the same SpeedChange events when it places the cards), followed by one
     * continuous-effects pass over the finished setup board. Runs inside the
     * "setup" checkpoint, before the first snapshot, because a speed needs a live
     * game. The setup "counters" are not here: placeSetupCounters seeds them
     * before start(). */
    private void applySetupState(Game g) {
        JsonObject setup = sc0.has("setup") ? sc0.getAsJsonObject("setup") : new JsonObject();
        for (int i = 0; i < seats; i++) {
            if (!setup.has("p" + i)) {
                continue;
            }
            JsonObject s = setup.getAsJsonObject("p" + i);
            TestPlayer pl = seat(i);
            if (s.has("tapped_after_untap")) {
                // gorge taps these single permanents after the turn's untap
                // step and drops every trigger the taps queue (CR 502.3), so
                // flip the flag instead of tap(): tap() fires TAPPED and a
                // "whenever ~ becomes tapped" trigger would fire on XMage only.
                for (JsonElement e : s.getAsJsonArray("tapped_after_untap")) {
                    permanentRef(g, "p" + i + ":" + e.getAsString()).setTapped(true);
                }
            }
            if (s.has("speed") && s.get("speed").getAsInt() > 0) {
                // CR 702.179: speed starts at 1 and rises one step at a time to 4.
                pl.initSpeed(g);
                for (int k = 1; k < s.get("speed").getAsInt(); k++) {
                    pl.increaseSpeed(g);
                }
            }
            putSeatSetupPlayerCounters(pl, s);
        }
        // Setup state is written without the events the game loop reacts to:
        // addCard's tapped placement and setTapped here flip a tap flag,
        // placeSetupCounters writes counters straight on, and a speed is a
        // player write. A conditional static -- Scoria Cat's "+3/+3 as long as
        // you control no untapped lands" -- is a ContinuousEffect whose
        // condition is read when effects are applied, so without this pass the
        // first snapshot reports the pre-setup P/T where gorge reports the live
        // value (CR 611.3a). Apply unconditionally: one pass per scenario is
        // cheap, and a hand-maintained "something changed" flag missed the tap
        // arm (and would miss the next setup kind).
        g.applyEffects();
    }

    /** The pre-start hook that seeds every seat's setup "counters" (card name ->
     * counter kind -> amount, applied to every battlefield placement of the
     * name; a name or kind XMage does not know fails the scenario rather than
     * placing nothing). The counters are recorded board state, not an event:
     * they are written straight onto the permanent (putSetupCounters), so a
     * counter-replacement permanent (Dr. Joseph M'Benga, Hardened Scales) never
     * sees them, as in gorge and Forge. Runs after the cheat() placements and
     * before start() checks state-based actions, so a printed 0/0 or an X=0
     * placement survives with its counters, as gorge emits them with the
     * placement. */
    private void placeSetupCounters(Game g) {
        if (setupCountersPlaced) {
            return;
        }
        setupCountersPlaced = true;
        JsonObject setup = sc0.has("setup") ? sc0.getAsJsonObject("setup") : new JsonObject();
        boolean changed = false;
        for (int i = 0; i < seats; i++) {
            if (!setup.has("p" + i)) {
                continue;
            }
            JsonObject s = setup.getAsJsonObject("p" + i);
            if (!s.has("counters")) {
                continue;
            }
            TestPlayer pl = seat(i);
            for (Map.Entry<String, JsonElement> byCard : s.getAsJsonObject("counters").entrySet()) {
                String name = xmageSpelling(byCard.getKey());
                boolean placed = false;
                for (Permanent perm : g.getBattlefield().getAllPermanents()) {
                    // A back-face-staged permanent is still named by its front (setupNames).
                    if (!pl.getId().equals(perm.getControllerId()) || !setupName(perm).equals(name)) {
                        continue;
                    }
                    putSetupCounters(g, perm, byCard.getValue().getAsJsonObject());
                    placed = true;
                    changed = true;
                }
                if (!placed) {
                    throw new IllegalArgumentException("counters name " + byCard.getKey() + ", which is not on p" + i + "'s battlefield");
                }
            }
        }
        if (changed) {
            g.applyEffects();
        }
    }

    /** Writes a setup counter set onto a permanent as recorded state: straight
     * into its Counters, with no ADD_COUNTERS event (so no replacement such as
     * Hardened Scales or Dr. Joseph M'Benga applies) and no COUNTER_ADDED
     * trigger. Shared by setup cards and setup tokens so the two cannot drift.
     * Never use Permanent.addCounters here. */
    static void putSetupCounters(Game game, Permanent permanent, JsonObject kinds) {
        for (Map.Entry<String, JsonElement> byKind : kinds.entrySet()) {
            permanent.getCounters(game).addCounter(
                    xmageCounter(byKind.getKey()).createInstance(byKind.getValue().getAsInt()));
        }
    }

    /** Writes a seat's setup "player_counters" as recorded state (each entry is
     * the COMPLETE count for its kind, applied as a delta): straight into
     * the PlayerImpl counter set, with no ADD_COUNTERS event and no
     * COUNTER_ADDED trigger (CR 122.1; energy is a player counter, CR 107.14).
     * The Player interface exposes only addCounters, which fires both, so the
     * write goes through the delegate's field the same way
     * clearSetupEntryHistory reaches turnsOnBattlefield. */
    static void putSeatSetupPlayerCounters(TestPlayer pl, JsonObject seat) {
        if (seat.has("player_counters")) {
            putSetupPlayerCounters(pl, seat.getAsJsonArray("player_counters"));
        }
    }

    static void putSetupPlayerCounters(TestPlayer pl, JsonArray counters) {
        try {
            java.lang.reflect.Field delegate = TestPlayer.class.getDeclaredField("computerPlayer");
            delegate.setAccessible(true);
            Object impl = delegate.get(pl);
            java.lang.reflect.Field field = mage.players.PlayerImpl.class.getDeclaredField("counters");
            field.setAccessible(true);
            mage.counters.Counters cs = (mage.counters.Counters) field.get(impl);
            for (JsonElement e : counters) {
                JsonObject pc = e.getAsJsonObject();
                CounterType type = xmageCounter(pc.get("kind").getAsString());
                // "n" is the COMPLETE count at the setup checkpoint (also 0),
                // so move the seat to it: a counter the game already produced
                // before the cut's main must not be added twice.
                int delta = pc.get("n").getAsInt() - cs.getCount(type);
                if (delta > 0) {
                    cs.addCounter(type.createInstance(delta));
                } else if (delta < 0) {
                    cs.removeCounter(type, -delta);
                }
            }
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException("cannot place setup player counters", e);
        }
    }

    /** The pre-start hook: places every seat's setup "tokens" (gorge's runner mints
     * them after the seat's card placements, in list order) once, after
     * CardTestPlayerAPIImpl.execute's cheat() loop has put the cards on the
     * battlefield and before start() runs init and the first state-based-action
     * check. */
    private void placeSetupTokens(Game game) {
        if (setupTokensPlaced) {
            return;
        }
        setupTokensPlaced = true;
        JsonObject setup = sc0.has("setup") ? sc0.getAsJsonObject("setup") : new JsonObject();
        boolean placed = false;
        for (int i = 0; i < seats; i++) {
            if (!setup.has("p" + i)) {
                continue;
            }
            for (JsonObject t : setupTokenEntries(setup.getAsJsonObject("p" + i))) {
                placeSetupToken(game, seat(i), setupToken(t.get("script").getAsString()),
                        t.has("counters") ? t.getAsJsonObject("counters") : null);
                placed = true;
            }
        }
        if (placed) {
            game.applyEffects();
        }
    }

    /** Puts one token onto the battlefield silently, as gorge's setup does: the
     * cheat() placement of a card (CardUtil.putCardOntoBattlefieldWithEffects:
     * entersBattlefield and addPermanent, no ZONE_CHANGE event) with the token
     * registration of TokenImpl.putOntoBattlefieldHelper minus its events (no
     * CREATE_TOKEN, CREATED_TOKEN or ZONE_CHANGE, so Doubling Season and "whenever
     * you create a token" never see it). counters is the token's complete counter
     * set, put on directly: no ADD_COUNTERS replacement (Hardened Scales) and no
     * COUNTER_ADDED trigger, as gorge's setup drops every trigger it queues. */
    static Permanent placeSetupToken(Game game, Player controller, mage.game.permanent.token.Token token,
            JsonObject counters) {
        Ability source = new SimpleStaticAbility(Zone.OUTSIDE, new InfoEffect("fake ability"));
        source.setControllerId(controller.getId());
        mage.game.permanent.PermanentToken permanent =
                new mage.game.permanent.PermanentToken(token, controller.getId(), game);
        source.setSourceId(permanent.getId());
        game.getState().addCard(permanent);
        game.getPermanentsEntering().put(permanent.getId(), permanent);
        permanent.updateZoneChangeCounter(game, new mage.game.events.ZoneChangeEvent(permanent, source,
                controller.getId(), Zone.OUTSIDE, Zone.BATTLEFIELD));
        if (counters != null) {
            putSetupCounters(game, permanent, counters);
        }
        // fireEvent=false: no ENTERS_THE_BATTLEFIELD event is queued (the return
        // value is false on that path whether or not a replacement applied, as
        // for the card placement, which ignores it).
        permanent.entersBattlefield(source, game, Zone.OUTSIDE, false);
        game.addPermanent(permanent, game.getState().getNextPermanentOrderNumber());
        permanent.setZone(Zone.BATTLEFIELD, game);
        game.getPermanentsEntering().remove(permanent.getId());
        permanent.removeSummoningSickness();
        // Init the continuous effects of the token's static abilities, as the card
        // placement does (the game state holds copies).
        for (mage.abilities.effects.ContinuousEffect effect : game.getState().getContinuousEffects().getLayeredEffects(game)) {
            java.util.Optional<Ability> ability = game.getState().getContinuousEffects()
                    .getLayeredEffectAbilities(effect).stream().findFirst();
            if (ability.isPresent() && permanent.getId().equals(ability.get().getSourceId())) {
                effect.init(ability.get(), game, controller.getId());
            }
        }
        return permanent;
    }

    /** One seat's setup "attached" entries, in list order. */
    static List<JsonObject> setupAttachedEntries(JsonObject seatSetup) {
        List<JsonObject> out = new ArrayList<>();
        if (seatSetup.has("attached")) {
            for (JsonElement e : seatSetup.getAsJsonArray("attached")) {
                out.add(e.getAsJsonObject());
            }
        }
        return out;
    }

    /** Links one Aura to its bearer silently, as gorge's setup places an Aura
     * attached (CR 303.4f): PermanentImpl.attachTo fires no event for an
     * unattached Aura, and the bearer's attachment list is extended directly.
     * Permanent.addAttachment would fire ATTACH and ATTACHED, so a "whenever
     * an Aura becomes attached" trigger (Bramble Elemental) would fire on
     * XMage only. Both ends of the link are what checkStateBasedActions reads
     * to keep an Aura (the Aura's attachedTo and the bearer's attachments), so
     * the Aura is not put into the graveyard at the first priority. */
    static void linkSetupAttached(Game game, Permanent aura, Permanent bearer) {
        aura.attachTo(bearer.getId(), null, game);
        if (!bearer.getAttachments().contains(aura.getId())) {
            bearer.getAttachments().add(aura.getId());
        }
    }

    /** The pre-start hook, after placeSetupTokens: links every seat's setup
     * "attached" Auras, in seat then list order (gorge's runner places them
     * last, after every seat and its tokens). card is a seat-relative ref of
     * the seat's battlefield; to is a full ref of a battlefield card or a setup
     * token. A ref that names nothing fails the scenario. */
    private void placeSetupAttached(Game game) {
        if (setupAttachedPlaced) {
            return;
        }
        setupAttachedPlaced = true;
        JsonObject setup = sc0.has("setup") ? sc0.getAsJsonObject("setup") : new JsonObject();
        boolean linked = false;
        for (int i = 0; i < seats; i++) {
            if (!setup.has("p" + i)) {
                continue;
            }
            for (JsonObject a : setupAttachedEntries(setup.getAsJsonObject("p" + i))) {
                String cardRef = "p" + i + ":" + a.get("card").getAsString();
                String toRef = a.get("to").getAsString();
                linkSetupAttached(game, setupPermanent(game, cardRef), setupPermanent(game, toRef));
                linked = true;
            }
        }
        if (linked) {
            game.applyEffects();
        }
    }

    /** The battlefield permanent a setup ref names: a token through the live
     * tokens (tokenChoice, as every other token ref), a card through the id
     * its placement recorded. */
    private Permanent setupPermanent(Game game, String ref) {
        Permanent found;
        if (ref.contains(":token:")) {
            found = tokenChoice(ref, seat(refSeat(ref)).getId(), game.getBattlefield().getAllPermanents());
        } else {
            UUID id = setupPermanentIds.get(ref);
            found = id == null ? null : game.getPermanent(id);
        }
        if (found == null) {
            throw new IllegalArgumentException("setup attached names " + ref + ", which is not on the battlefield");
        }
        return found;
    }

    private int add(JsonObject s, String key, Zone zone, TestPlayer p) {
        List<String> ns = names(s, key);
        // "tapped" names battlefield cards that start tapped (gorge's runner
        // taps every placement whose name it lists, oraclegen's ".tapped"
        // target slots: Push // Pull, Keep Out, Radiant Strike).
        List<String> tapped = zone == Zone.BATTLEFIELD ? names(s, "tapped") : new ArrayList<>();
        List<String> backFace = zone == Zone.BATTLEFIELD ? names(s, "back_face") : new ArrayList<>();
        int i = seatOfPlayer(p);
        for (String n : ns) {
            int k = buildCounts.merge(i + "|" + n, 1, Integer::sum);
            String ref = "p" + i + ":" + n + (k > 1 ? "#" + k : "");
            refAlias.put(ref, "@" + ref);
            String xmageName = xmageSpelling(n);
            addCard(zone, p, xmageName, 1, tapped.contains(n));
            if (backFace.contains(n)) {
                stageBackFace(p, n);
                xmageName = backFaceNames.get("p" + i + ":" + xmageName);
            }
            if (zone == Zone.BATTLEFIELD) {
                List<PutToBattlefieldInfo> seeded = getBattlefieldCards(p);
                setupPermanentIds.put(ref, CardUtil.getDefaultCardSideForBattlefield(
                        currentGame, seeded.get(seeded.size() - 1).getCard()).getId());
                // Record the seeded permanents by (controller id, current XMage
                // name), including staged back faces, so the entry-history
                // normalizer ages exactly these, never a genuine turn-1 entry.
                setupBattlefield.merge(p.getId() + "|" + xmageName, 1, Integer::sum);
                recordPlacedName(placedNames, getBattlefieldCards(p), xmageSpelling(n));
            }
        }
        return ns.size();
    }

    /** Setup is not a transform action: place the right half without firing
     * TRANSFORMED triggers. XMage's transforming and modal DFCs both extend
     * DoubleFacedCard; its test framework accepts either half directly. */
    private void stageBackFace(TestPlayer p, String name) {
        List<PutToBattlefieldInfo> placements = getBattlefieldCards(p);
        int last = placements.size() - 1;
        PutToBattlefieldInfo placed = placements.get(last);
        Card card = placed.getMainCard();
        if (!(card instanceof DoubleFacedCard)) {
            throw new IllegalArgumentException("setup back_face: " + name + " is not double-faced");
        }
        Card back = ((DoubleFacedCard) card).getRightHalfCard();
        if (!back.isPermanent()) {
            throw new IllegalArgumentException("setup back_face: " + name + " has no permanent back face");
        }
        placements.set(last, new PutToBattlefieldInfo(back, placed.isTapped()));
        // Scenario refs/counters still name the front, even though snapshots
        // and XMage's attack/block commands must name the current back face.
        setupNames.put(back.getId(), xmageSpelling(name));
        backFaceNames.put("p" + seatOfPlayer(p) + ":" + xmageSpelling(name), back.getName());
    }

    /** Maps a gorge counter kind (P1P1, M1M1, LOYALTY, CHARGE, ...) to XMage's
     * type: the CounterType enum constant of that name, else the counter whose
     * display name it spells. */
    private static CounterType xmageCounter(String kind) {
        try {
            return CounterType.valueOf(kind);
        } catch (IllegalArgumentException notAConstant) {
            // fall through to the display-name lookup
        }
        String n;
        switch (kind) {
            case "P1P1":
                n = "+1/+1";
                break;
            case "M1M1":
                n = "-1/-1";
                break;
            default:
                n = kind.toLowerCase();
        }
        CounterType t = CounterType.findByName(n);
        if (t == null) {
            throw new IllegalArgumentException("setup counters: unknown counter kind " + kind);
        }
        return t;
    }

    /** Binds one XMage alias per setup ref (the string build() recorded) to
     * the setup object itself, so a target ref naming two same-name cards
     * ("p0:Grizzly Bears#2") reaches exactly one of them. Counts follow the
     * zone order build() added them in. */
    private void registerAliases(Game g) {
        for (int i = 0; i < seats; i++) {
            TestPlayer pl = seat(i);
            Map<String, Integer> counts = new HashMap<>();
            Map<String, Integer> scenarioCounts = new HashMap<>();
            List<mage.MageObject> objs = aliasObjectsInZoneOrder(g, pl);
            for (mage.MageObject o : objs) {
                bindSetupObject(i, setupName(o), gorgeName, xmageName, counts, scenarioCounts,
                        frontCounts, o.getId(), refAlias, (ref, objId) -> {
                            for (int j = 0; j < seats; j++) {
                                try {
                                    seat(j).addAlias(ref, objId);
                                } catch (IllegalArgumentException ignored) {
                                    // already bound on this player
                                }
                            }
                        });
            }
        }
    }

    /** True when s is a scenario object ref ("p0:Name", "p1:token:Name#2").
     * A seat target ("p1") and a skip token ("[target_skip]") are not refs. */
    static boolean isScenarioRef(String s) {
        int i = s.indexOf(':');
        return !s.startsWith("[") && i > 0 && s.substring(0, i).matches("p[0-9]+");
    }

    /**
     * Binds the "@&lt;ref&gt;" alias an answer names if it is not already bound.
     * registerAliases covers every setup object; an object that entered after
     * setup (a token copy) has no alias yet, so a same-name answer naming it
     * by exact ref could not be matched. Locate it by the same counting rule
     * gorange's objRef uses: a token ranks among its controller's battlefield
     * tokens whose name CONTAINS the ref name; a card ranks among its owner's
     * live objects of that name in registerAliases' zone order. Additive and
     * idempotent: it never rebinds a ref registerAliases already bound.
     */
    private void bindAnswerAlias(String value) {
        for (String ref : answerRefs(value)) {
            bindOneAlias(ref);
        }
    }

    /** Tokens' gorge refs rank the controller's live tokens in creation order.
     * Battlefield iteration is UUID/hash order in XMage, not entry order. */
    static Permanent tokenChoice(String ref, UUID controller, Iterable<Permanent> battlefield) {
        return tokenChoice(ref, refName(ref), controller, battlefield);
    }

    /** As tokenChoice, but names the token under the given spelling. A token
     * copy of the card under test carries XMage's spelling while the combat
     * ref carries the scenario's. */
    static Permanent tokenChoice(String ref, String name, UUID controller, Iterable<Permanent> battlefield) {
        Permanent chosen = tokenChoice(ref, name, controller, battlefield, false);
        if (chosen != null) {
            return chosen;
        }
        // Forge's token scripts name a token "<Name> Token" while XMage may
        // name the same token bare: Empower's planeswalker token is "Jace"
        // where u_empower_jace.txt says "Jace Token". Fall back to the bare
        // name only when no live token contains the full ref name, so a
        // literal "Treasure Token" is never displaced by a "Treasure" token.
        String bare = withoutTokenSuffix(name);
        return bare == null ? null : tokenChoice(ref, bare, controller, battlefield, true);
    }

    private static Permanent tokenChoice(String ref, String name, UUID controller,
            Iterable<Permanent> battlefield, boolean exactName) {
        String wantedName = name.toLowerCase(java.util.Locale.ROOT);
        int wanted = 1;
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            wanted = Integer.parseInt(ref.substring(hash + 1));
        }
        List<Permanent> candidates = new ArrayList<>();
        for (Permanent permanent : battlefield) {
            if (permanent.isToken() && controller.equals(permanent.getControllerId())
                    && (exactName ? tokenNamedExactly(permanent, wantedName)
                            : tokenNamed(permanent, wantedName))) {
                candidates.add(permanent);
            }
        }
        candidates.sort(java.util.Comparator.comparingInt(Permanent::getCreateOrder));
        return wanted > 0 && wanted <= candidates.size() ? candidates.get(wanted - 1) : null;
    }

    /** Whether a token answers to the lower-cased name: its live name or the
     * name it was created with. A ref names a token by its creation name, and
     * a layer-3 name change (Witness Protection: "named Legitimate
     * Businessperson", CR 613.1c) moves only the live name, so matching the
     * live name alone loses the token to every ref lookup. Every token ref
     * resolves through tokenChoice, so this one test covers targets, setup
     * attachments, combat and answer aliases alike. */
    private static boolean tokenNamed(Permanent token, String wantedName) {
        if (token.getName().toLowerCase(java.util.Locale.ROOT).contains(wantedName)) {
            return true;
        }
        return token instanceof mage.game.permanent.PermanentToken
                && ((mage.game.permanent.PermanentToken) token).getToken().getName()
                        .toLowerCase(java.util.Locale.ROOT).contains(wantedName);
    }

    /** Whether a token's live or creation name IS the wanted lower-cased name.
     * The " Token" suffix fallback must be exact: a ref "Jace Token" names
     * XMage's "Jace" token, not "Jace, Reality Sculptor" or "Jace's
     * Phantasm". */
    private static boolean tokenNamedExactly(Permanent token, String wantedName) {
        if (token.getName().toLowerCase(java.util.Locale.ROOT).equals(wantedName)) {
            return true;
        }
        return token instanceof mage.game.permanent.PermanentToken
                && ((mage.game.permanent.PermanentToken) token).getToken().getName()
                        .toLowerCase(java.util.Locale.ROOT).equals(wantedName);
    }

    /** A Forge token name without its " Token" suffix, or null when it has
     * none. The suffix is a Forge token-script convention, not part of the
     * token's name: XMage's planeswalker and other named tokens carry the
     * bare name (JacePlaneswalkerToken is "Jace"). Deriving the bare name
     * here covers every such token, not one card at a time. */
    private static String withoutTokenSuffix(String name) {
        String suffix = " Token";
        if (name.length() <= suffix.length()
                || !name.regionMatches(true, name.length() - suffix.length(), suffix, 0, suffix.length())) {
            return null;
        }
        return name.substring(0, name.length() - suffix.length());
    }

    /** The controller's battlefield permanents in setup/creation order.
     * A scenario's "#k" ranks same-name objects by the order they were dealt
     * or entered, not by whatever order the battlefield happens to iterate,
     * so binding by iteration can give a same-name ref the wrong copy.
     * Permanent.getCreateOrder is that order, the same key tokenChoice has
     * always used. */
    static List<Permanent> battlefieldInCreationOrder(Iterable<Permanent> battlefield, UUID controller) {
        List<Permanent> ordered = new ArrayList<>();
        for (Permanent permanent : battlefield) {
            if (controller.equals(permanent.getControllerId())) {
                ordered.add(permanent);
            }
        }
        ordered.sort(java.util.Comparator.comparingInt(Permanent::getCreateOrder));
        return ordered;
    }

    /** The objects a scenario ref is ranked among, in the order build() added
     * them: battlefield first in setup/creation order, then hand, graveyard,
     * exile and library in add()'s zone order. registerAliases and
     * bindOneAlias both bind through this one list so a setup ref and a
     * post-setup answer with the same "#k" can never disagree about which
     * same-name object the rank names. */
    static List<mage.MageObject> aliasObjectsInZoneOrder(Game g, Player pl) {
        List<mage.MageObject> objs = new ArrayList<>();
        objs.addAll(battlefieldInCreationOrder(g.getBattlefield().getAllPermanents(), pl.getId()));
        for (Card c : pl.getHand().getCards(g)) objs.add(c);
        for (Card c : pl.getGraveyard().getCards(g)) objs.add(c);
        for (Card c : g.getExile().getAllCards(g)) {
            if (pl.getId().equals(c.getOwnerId())) objs.add(c);
        }
        for (Card c : pl.getLibrary().getCards(g)) objs.add(c);
        return objs;
    }

    /** The seat index of a scenario ref: "p1:Forest#2" -> 1. */
    static int refSeat(String ref) {
        return Integer.parseInt(ref.substring(1, ref.indexOf(':')));
    }

    /** The scenario refs an answer value names. A multi-pick answer joins its
     * picks with "^" ("@p0:Wastes#27^@p0:Wastes#39"); each segment loses its
     * "@" alias marker, and a segment that is not a ref ("X=2", a label) is
     * dropped. */
    static List<String> answerRefs(String value) {
        List<String> refs = new ArrayList<>();
        for (String seg : value.split("\\^")) {
            String ref = seg.startsWith("@") ? seg.substring(1) : seg;
            if (isScenarioRef(ref)) {
                refs.add(ref);
            }
        }
        return refs;
    }

    /** The object a non-token scenario ref names among objs, the ref's seat's
     * own objects in the caller's zone order: the rank-th whose scenario name
     * matches. A rank-free ref is #1. Null when the ref names no such
     * object. */
    static mage.MageObject refRankChoice(String ref, List<mage.MageObject> objs,
            java.util.function.Function<mage.MageObject, String> setupName) {
        String name = refName(ref);
        int wanted = 1;
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            wanted = Integer.parseInt(ref.substring(hash + 1));
        }
        int seen = 0;
        for (mage.MageObject o : objs) {
            if (!setupName.apply(o).equals(name)) {
                continue;
            }
            if (++seen == wanted) {
                return o;
            }
        }
        return null;
    }

    private void bindOneAlias(String ref) {
        if (refAlias.containsKey(ref) || currentGame == null) {
            return;
        }
        int seatIndex = refSeat(ref);
        boolean token = ref.contains(":token:");
        mage.MageObject match = null;
        if (token) {
            // A token ref ranks its controller's live tokens in creation
            // order (tokenChoice), the same rule permanentRef and
            // setupPermanent use. Battlefield iteration is UUID/hash order,
            // so binding by iteration bound the wrong same-named token.
            match = tokenChoice(ref, seat(seatIndex).getId(),
                    currentGame.getBattlefield().getAllPermanents());
        } else {
            String name = refName(ref);
            int wanted = 1;
            int hash = ref.lastIndexOf('#');
            if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
                wanted = Integer.parseInt(ref.substring(hash + 1));
            }
            // The same creation-ordered list registerAliases binds setup refs
            // in, so a post-setup non-token ref's "#k" counts the copies alike.
            List<mage.MageObject> objs = aliasObjectsInZoneOrder(currentGame, seat(seatIndex));
            // objs is already scoped to the ref's seat, so "p1:<name>" never
            // binds a same-named card another seat owns.
            match = refRankChoice(ref, objs, this::setupName);
        }
        if (match == null) {
            return;
        }
        refAlias.put(ref, "@" + ref);
        for (int j = 0; j < seats; j++) {
            try {
                seat(j).addAlias(ref, match.getId());
            } catch (IllegalArgumentException ignored) {
                // already bound on this player
            }
        }
    }

    /**
     * The choice-queue value for an answer that may name a scenario ref. The
     * driver matches an object by its "@ref" alias; an answer the generator
     * already emits as "@ref" is passed through, and a bare ref is mapped to
     * its alias here (the choice queue has no targetName step). Any other
     * value (a label, a yes/no, a skip token) is unchanged.
     */
    private String aliasChoiceValue(String v) {
        if (v.startsWith("@")) {
            bindAnswerAlias(v);
            return v;
        }
        if (isScenarioRef(v)) {
            bindAnswerAlias(v);
            return targetName(v);
        }
        return v;
    }

    /** Whether the ask belongs to the spell ability of a card the scenario cast
     * through a target adjuster. */
    private boolean isAdjustedSpellAsk(Ability source, Game game) {
        if (source == null || source.getAbilityType() != mage.constants.AbilityType.SPELL) {
            return false;
        }
        mage.MageObject object = source.getSourceObject(game);
        return object != null && adjustedCasts.contains(object.getName());
    }

    /** Whether an ask belongs to a cast: the spell ability itself (including
     * an adjusted spell's rebuilt target objects) or the Zone.STACK triggered
     * ability XMage uses for "when you cast this spell" (Ulamog, the
     * Ceaseless Hunger). A resolution or activation ask is not a cast, so an
     * unscripted "up to" slot there must stay a strict miss rather than be
     * silently skipped. */
    static boolean isCastTargetAsk(Ability source) {
        if (source == null) {
            return false;
        }
        return source.getAbilityType() == mage.constants.AbilityType.SPELL
                || (source instanceof mage.abilities.TriggeredAbility && source.getZone() == Zone.STACK);
    }

    /** An ask XMage actually poses with no scenario answer left closes the
     * slot when it is a cast ask's "up to" target (max > min) whose minimum is
     * already selected. A required slot still short of its minimum is a
     * scenario error and gets no skip, and neither does a completed fixed
     * slot (max <= min): TestPlayer rejects a skip on one ("Wrong skip
     * command found - it can be used with up to targets"), and XMage does not
     * ask a completed slot anyway. A non-cast ask is left unanswered so the
     * strict run reports it. */
    static void closeCastAskedAgain(List<String> queue, Ability source,
            int minTargets, int maxTargets, int selected) {
        if (!isCastTargetAsk(source) || !queue.isEmpty() || selected < minTargets
                || maxTargets <= minTargets) {
            return;
        }
        queue.add(TestPlayer.TARGET_SKIP);
    }

    /** An adjusted spell must receive targets through the cast-time chooser. */
    static boolean queueAdjustedCastTargets(boolean hasAdjuster, boolean divided, int targetCount) {
        return hasAdjuster && !divided && targetCount > 0;
    }

    /** Whether an ability gets its targets from a target adjuster. */
    static boolean hasTargetAdjuster(Ability ability) {
        return ability != null && ability.getTargetAdjuster() != null;
    }

    /**
     * Whether a spell ability must take its targets through the queue: it has a
     * target adjuster AND either no base target or an adjuster that discards
     * pre-set targets ({@link #replacesCastTargets}). The first shape breaks the inline
     * {@code $target=} form -- the up-front check reads the unadjusted ability
     * ({@code handleNonPlayerTargetTarget}'s empty {@code selectedMode.getTargets()})
     * and throws "Ability has no targets" (The Eagles Are Coming!: no base
     * target, {@code ConditionalTargetAdjuster} supplies it during casting).
     * An adjuster card that already declares a base target (Dominate,
     * Distorting Wake) validates inline and keeps its old path, so this change
     * cannot reroute it.
     */
    static boolean needsQueuedCastTargets(Ability ability) {
        return hasTargetAdjuster(ability)
                && (ability.getAllSelectedTargets().isEmpty() || replacesCastTargets(ability));
    }

    /**
     * Whether the ability's adjuster discards whatever the cast command
     * pre-set and builds fresh target objects: a {@code ConditionalTargetAdjuster}
     * ({@code adjustTargets} starts with {@code getTargets().clear()}, then
     * re-adds the blueprint or the replacement). Such a card (Into the Flood
     * Maw, Long River's Pull: a gift swaps the target filter) declares a base
     * target, so the inline {@code $target=} form validates, is stored on the
     * base object, and is wiped when the adjuster runs mid-cast; the chooser
     * then asks again and the TestPlayer fails "Missing TARGET def". A queued
     * target is read by that later ask, so these casts take the queue route.
     * A {@code ForEachPlayerTargetsAdjuster} does the same ({@code adjustTargets}
     * clears, then adds one copy of the blueprint per player or opponent): Mass
     * Mutiny's inline target was wiped and its "up to one" per-opponent copy,
     * min 0, was closed with nothing picked, so the spell resolved untargeted.
     */
    static boolean replacesCastTargets(Ability ability) {
        if (ability == null) {
            return false;
        }
        mage.target.targetadjustment.TargetAdjuster adjuster = ability.getTargetAdjuster();
        return adjuster instanceof mage.target.targetadjustment.ConditionalTargetAdjuster
                || adjuster instanceof mage.target.targetadjustment.ForEachPlayerTargetsAdjuster;
    }

    /** The named face's spell ability, or null when the name does not
     * resolve. A modal double-faced card is one XMage card object registered
     * under its front face, so the repository resolves its back-face name too;
     * the object's first SpellAbility is the front half's, while a cast of
     * the back face (cast_mode "modal_spell") must be analysed through the
     * right half's own SpellAbility. */
    private static Ability spellAbility(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        if (c instanceof ModalDoubleFacedCard
                && name.equals(((ModalDoubleFacedCard) c).getRightHalfCard().getName())) {
            return ((ModalDoubleFacedCard) c).getRightHalfCard().getSpellAbility();
        }
        return c == null ? null : c.getSpellAbility();
    }

    /**
     * Whether the named spell is a Spree card. XMage models Spree as a
     * SpreeAbility on the card (it sets the spell's modes to 1..unbounded), so
     * this is exact and structural: every Spree card is covered, including one
     * printed after this driver. The old hardcoded name set listed 11 of the
     * 21 Spree cards the corpus carries (One Last Job among the missing), so
     * its cast never got the closing mode skip and XMage failed the scenario
     * with "Missing MODE def".
     */
    private static boolean isSpreeSpell(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return isSpreeCard(c);
    }

    /** Whether the card itself is a Spree card: XMage models Spree as a
     * SpreeAbility on it, so this is exact and structural. Card-based and
     * package-private so a contract test can pin it without the card
     * repository (which needs H2). */
    static boolean isSpreeCard(Card c) {
        return c != null && c.getAbilities().containsClass(SpreeAbility.class);
    }

    /** The XMage Zone a `move` step's lowercase destination names. XMage's
     * enum member is EXILED, not EXILE, so a bare valueOf(upper) throws on
     * gorge's "exile"; every other zone name upper-cases straight to its
     * member. Static and package-private for the driver contract test. */
    static Zone moveDestination(String to) {
        return to.equals("exile") ? Zone.EXILED : Zone.valueOf(to.toUpperCase(java.util.Locale.ROOT));
    }

    /** The Card-bearing zones a `move` step's source card may start in, in
     * search order. gorge's generator currently stages every moved card in
     * hand (candidates.go emits hand -> destination), but the op is not
     * hand-specific: a future shape that moves a card already in a graveyard,
     * library or exile must not fall into a hand-only search and throw. A
     * battlefield permanent is a Permanent, not a Card, so it is not a move
     * source (XMage moves those with Permanent.moveToZone). Package-private
     * and pure so the driver contract test can pin the coverage. */
    static List<Zone> moveSourceZones() {
        return Arrays.asList(Zone.HAND, Zone.GRAVEYARD, Zone.LIBRARY, Zone.EXILED);
    }

    /** The first card named {@code name} in any of {@link #moveSourceZones()},
     * or null. Hand is searched first, so today's generated hand -> zone move
     * is unchanged. */
    private static Card findMoveCard(Player player, Game game, String name) {
        for (Zone zone : moveSourceZones()) {
            if (zone == Zone.HAND) {
                for (Card c : player.getHand().getCards(game)) {
                    if (c.getName().equals(name)) {
                        return c;
                    }
                }
            } else if (zone == Zone.GRAVEYARD) {
                for (Card c : player.getGraveyard().getCards(game)) {
                    if (c.getName().equals(name)) {
                        return c;
                    }
                }
            } else if (zone == Zone.LIBRARY) {
                for (Card c : player.getLibrary().getCards(game)) {
                    if (c.getName().equals(name)) {
                        return c;
                    }
                }
            } else if (zone == Zone.EXILED) {
                for (Card c : game.getExile().getCardsOwned(game, player.getId())) {
                    if (c.getName().equals(name)) {
                        return c;
                    }
                }
            }
        }
        return null;
    }

    /** The object a `move` step moves: the card found in a
     * {@link #moveSourceZones()} zone, else the battlefield permanent the ref
     * names. gorge's runner moves the setup-bound object wherever it is, so a
     * battlefield ref (The Masamune's "a creature dies" probe moves p0's
     * Grizzly Bears to the graveyard) is a dies-equivalent zone change, which
     * XMage's moveCards performs for a Permanent too. The card zones stay
     * first so every existing hand -> zone move is unchanged. */
    static <T> T moveSource(T inCardZone, java.util.function.Supplier<? extends T> onBattlefield) {
        return inCardZone != null ? inCardZone : onBattlefield.get();
    }

    /** The battlefield permanent a move ref names, or null when none does. */
    private Permanent battlefieldMoveSource(Game g, String ref) {
        try {
            return permanentRef(g, ref);
        } catch (IllegalArgumentException | StringIndexOutOfBoundsException notThere) {
            return null;
        }
    }

    /**
     * Whether a modal spell's first target lives in a later mode: the card's
     * first mode (the one XMage's up-front {@code $target=} check reads, since
     * the scenario's chosen modes are not selected yet) has no target, but
     * another mode does. Inline {@code $target=} then throws "Ability has no
     * targets" (Cosmium Confluence: modes 1 and 2 are targetless, mode 3
     * destroys target enchantment). The target is queued instead and consumed
     * when the chosen mode's target is asked for. A card whose first mode has a
     * target already validates inline, and a targetless card has no later mode
     * target, so neither is rerouted.
     */
    static boolean firstTargetInLaterMode(Ability ability) {
        if (ability == null || ability.getModes().size() < 2 || ability.getModes().getMode() == null
                || !ability.getModes().getMode().getTargets().isEmpty()) {
            return false;
        }
        for (Mode m : ability.getModes().values()) {
            if (!m.getTargets().isEmpty()) {
                return true;
            }
        }
        return false;
    }

    /** Whether a lone cast target cannot take the inline "$target=" form: the
     * spell has two or more independent target objects and every one is an
     * "up to" object (minimum 0), so the one target may belong to any of them
     * and the string form (which sets it on the first object) would reject a
     * ref only a later object accepts. A spell with a required object, a
     * divided object or a target adjuster keeps its own routes. */
    static boolean loneTargetNeedsQueue(Ability ability) {
        if (ability == null || hasTargetAdjuster(ability) || targetsDivided(ability)) {
            return false;
        }
        List<mage.target.Target> objects = ability.getAllSelectedTargets();
        if (objects.size() < 2) {
            return false;
        }
        for (mage.target.Target t : objects) {
            if (t.getMinNumberOfTargets() != 0 || t.getMaxNumberOfTargets() < 1) {
                return false;
            }
        }
        return true;
    }

    /** Whether every target object of the ability is a card in a graveyard
     * (Reclaim, Regrowth): none of them can name a spell on the stack, so a
     * ref whose name an earlier step cast is that spell's card in the
     * graveyard (countered or fizzled), never a stack object. False for a
     * null or targetless ability. */
    static boolean targetsOnlyGraveyardCards(Ability ability) {
        if (ability == null) {
            return false;
        }
        List<mage.target.Target> objects = ability.getAllSelectedTargets();
        if (objects.isEmpty()) {
            return false;
        }
        for (mage.target.Target t : objects) {
            if (!(t instanceof TargetCard) || ((TargetCard) t).getZone() != Zone.GRAVEYARD) {
                return false;
            }
        }
        return true;
    }

    /** Whether an ability has a divided-amount target (TargetAmount). */
    static boolean targetsDivided(Ability ability) {
        if (ability == null) {
            return false;
        }
        for (mage.target.Target t : ability.getAllSelectedTargets()) {
            if (t instanceof mage.target.TargetAmount) {
                return true;
            }
        }
        return false;
    }

    /**
     * The whole amount a lone recipient of a divided spell takes, or null when
     * the inline {@code $target=} form already works or the amount is not a
     * fixed number. XMage's TestPlayer sets a TargetCreaturePermanentAmount's
     * entire amount on the one named target inline (Biogenic Upgrade), but for
     * any other TargetAmount (TargetAnyTargetAmount: Twin Bolt) the inline
     * form adds the target with no share and the chooser then asks for more.
     */
    static Integer soleRecipientAmount(Ability ability) {
        if (ability == null) {
            return null;
        }
        for (mage.target.Target t : ability.getAllSelectedTargets()) {
            if (t instanceof mage.target.TargetAmount
                    && !(t instanceof mage.target.common.TargetCreaturePermanentAmount)
                    && ((mage.target.TargetAmount) t).getAmount() instanceof mage.abilities.dynamicvalue.common.StaticValue) {
                return ((mage.target.TargetAmount) t).getAmount().calculate(null, null, null);
            }
        }
        return null;
    }

    /** The queue name for a target-amount recipient: a player by name, else
     * the setup alias or XMage-spelled card name. Shared by the scripted
     * "<ref>^X=<share>" answers and the lone-recipient cast. */
    private String amountTargetName(String ref) {
        return isSeatRef(ref) ? "targetPlayer=" + seat(seatOf(ref)).getName() : targetName(ref);
    }

    /** Whether the card's spell ability has a divided-amount target. */
    private static boolean spellTargetsDivided(String name) {
        return targetsDivided(spellAbility(name));
    }

    /** Whether a cast's recorded targets live on a cast trigger: the spell
     * ability declares no target, but a Zone.STACK triggered ability of the
     * card does (Ulamog, the Ceaseless Hunger's "When you cast this spell,
     * exile two target permanents"). Those targets belong to the trigger's
     * own chooser after the cast, not to the spell, so they are queued and
     * the spell has no slot of its own to fill or skip. */
    private static boolean spellTargetsOnCastTrigger(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return c != null && spellTargetsOnCastTrigger(c.getSpellAbility(), c.getAbilities());
    }

    /** As above, from the card's spell ability and abilities (so the test can
     * drive it without the card repository). An adjuster's targets are
     * handled before this predicate is consulted. */
    static boolean spellTargetsOnCastTrigger(Ability spell, List<? extends Ability> abilities) {
        if (spell == null || !spell.getAllSelectedTargets().isEmpty()) {
            return false;
        }
        for (Ability a : abilities) {
            if (a instanceof mage.abilities.TriggeredAbility
                    && a.getZone() == Zone.STACK
                    && !a.getAllSelectedTargets().isEmpty()) {
                return true;
            }
        }
        return false;
    }

    /** Normalises a cost label so gorge's pick ("Sacrifice artifact or
     * creature", "Pay 4") and XMage's button text ("Sacrifice an artifact or
     * creature", "{4}") compare equal. */
    private static String costKey(String label) {
        return label.toLowerCase().replaceAll("[{}]", "").replaceFirst("^pay ", "")
                .replaceAll("\\b(a|an|the)\\b", "").replaceAll("\\s+", " ").trim();
    }

    /** Which side of XMage's OrCost ask (true text = first cost) the cast
     * step's recorded gorge pick names; null when none of them matches. */
    private Boolean recordedCostIsFirst(String trueText, String falseText) {
        for (String pick : castCostPicks) {
            String key = costKey(pick);
            if (falseText != null && key.equals(costKey(falseText))) {
                return false;
            }
            if (trueText != null && key.equals(costKey(trueText))) {
                return true;
            }
        }
        return null;
    }

    /** Whether the card carries the Gift keyword (CR 702.174). */
    private static boolean hasGift(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return c != null && c.getAbilities().stream().anyMatch(a -> a.getClass().getSimpleName().startsWith("Gift"));
    }

    /** Why a card cannot take cast_mode "kicked", or null when it can. The mode
     * is gorge's single kernel "kicked" cast, which it also sends for
     * multikicked, replicated and squadded casts (oracle/gorge.go
     * repeatedCastMode), so only a card with exactly one plain, non-repeatable
     * Kicker cost is a faithful single Yes: a Multikicker or any-number-of-modes
     * kicker is re-asked while it is repeatable, a two-cost kicker would pay
     * both, and Replicate/Squad pose their own (non-Kicker) asks. Those stay
     * loud harness errors rather than hanging, over-paying or declining. */
    static String kickedCastShapeError(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        if (c == null) {
            return null;
        }
        List<KickerAbility> kickers = new ArrayList<>();
        for (mage.abilities.Ability a : c.getAbilities()) {
            if (a instanceof KickerAbility) {
                kickers.add((KickerAbility) a);
            }
        }
        if (kickers.isEmpty()) {
            return "cast_mode kicked on a card without a Kicker ability (replicate/squad) unsupported";
        }
        KickerAbility k = kickers.get(0);
        if (kickers.size() != 1 || k.getClass() != KickerAbility.class || k.getKickerCosts().size() != 1
                || k.getKickerCosts().get(0).isRepeatable()) {
            return "cast_mode kicked on a multikicker/multi-kicker card unsupported";
        }
        return null;
    }

    /** Whether the card carries an alternative cost XMage offers on a plain
     * cast (EvokeAbility, ImpendingAbility, DashAbility, ...). */
    private static boolean hasAlternativeSourceCost(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        if (info == null) {
            return false;
        }
        Card c = info.createCard();
        return c != null && c.getAbilities().stream().anyMatch(a -> a instanceof AlternativeSourceCosts);
    }

    /** Whether a cast step's cast_mode is one this driver elects: Bargain,
     * "optionalcost", "kicked" (the single Kicker ask; kicked1/kicked2/kickedboth/
     * multikicked pose per-kicker asks in order and stay rejected), the face-down Disguise/Morph/Megamorph casts, the "plot" and
     * "suspend" and "foretell" special actions, a graveyard "flashback" cast, and a "mayplay" cast of a card a
     * play-from-exile permission (Reckless Impulse) reaches are wired
     * ("plot_cast", the later cast of a plotted card, is not); so are the
     * "adventure_alt"/"omen_alt" casts of an Adventure or Omen half, the
     * "room_alt" cast of a Room's second door, and the "modal_spell" cast of
     * a modal double-faced card's back face (CR 712.12), whose parent card the
     * step names and the cast arm resolves to the half. An
     * absent or empty mode is the ordinary cast. Any other mode is a scenario
     * the driver cannot replay and must reject loudly rather than cast at face
     * value. */
    static boolean castModeSupported(String mode) {
        return mode == null || mode.isEmpty()
                || mode.equals("bargained") || mode.equals("optionalcost") || mode.equals("kicked")
                || mode.equals("disguised") || mode.equals("morphed") || mode.equals("megamorphed")
                || mode.equals("plot") || mode.equals("flashback")
                // "suspend": Suspend from hand (CR 702.62a) is a special action
                // (CR 116.2f), XMage's SuspendAbility, activated like Plot.
                || mode.equals("suspend")
                // "foretell": Foretell from hand (CR 702.143a) is a special
                // action (CR 116.2h), XMage's ForetellAbility, activated like Plot.
                || mode.equals("foretell")
                // "mayplay": the card is in exile (or wherever a play-from-exile
                // permission reaches) rather than hand, but the cast itself is
                // the card's ordinary SpellAbility, played by name. TestPlayer's
                // "Cast <name>" search runs over Zone.ALL, so it finds it.
                || mode.equals("mayplay")
                // "alternative": the first alternative cost, whether the card
                // carries it itself (Land Grant's reveal) or another permanent
                // grants it (Leyline of Mutation's {W}{U}{B}{R}{G}). XMage
                // poses its alternative-cost key menu during the cast; the
                // mode is the election, so ScriptedChoicePlayer queues key 1
                // there and the step is otherwise a plain cast.
                || mode.equals("alternative")
                // "evoked": Evoke (CR 702.74a) is an alternative cost, XMage's
                // EvokeAbility. The mode is the election: the cast picks the
                // Evoke entry of the alternative-cost key menu.
                || mode.equals("evoked")
                || mode.equals("prepared_copy")
                // "adventure_alt" / "omen_alt": the cast of an Adventure or
                // Omen half. The farm's step names the parent card (the
                // xengine ref binder keeps the dealt card's name); setup deals
                // the whole card and the cast arm resolves the inset spell
                // half's XMage name (spellOptionCastName), which is what
                // XMage's castSpell takes. A step that already names the half
                // also replays.
                || mode.equals("adventure_alt") || mode.equals("omen_alt")
                // "room_alt": a Room is a split card cast as one of its two
                // doors (CR 709.3, 709.3a); this is the second (back) door,
                // whose own name is the half of the whole "A // B" spelling
                // (CR 709.5). XMage casts a split half through the half's own
                // SpellAbility, so the cast arm resolves it like an Adventure
                // half but from the card object's name (altCastHalfName).
                || mode.equals("room_alt")
                // "modal_spell": a modal double-faced card cast with its back
                // face up (CR 712.12). The step names the parent card; XMage
                // casts the back face through that half's own SpellAbility,
                // named for the back face (altCastHalfName), like a split
                // half. A land back face is a "play", not this mode.
                || mode.equals("modal_spell")
                // "warped": Warp (CR 702.185) is its own SpellAbility on the
                // card (WarpAbility, "<card> with Warp"), picked by that name
                // like WarpTest does; the step's mana is the warp cost.
                || mode.equals("warped")
                || copyCostKeyword(mode) != null;
    }

    /** The XMage keyword class whose optional additional cost a copy-cost
     * cast mode elects: "conspired" is ConspireAbility (CR 702.78a, tap two
     * untapped creatures that share a colour with the spell), "casualty" is
     * CasualtyAbility (CR 702.153a, sacrifice a creature with power N or
     * greater), "teamworked" is TeamworkAbility (CR 702.194a, tap any
     * number of untapped creatures you control with total power N or
     * more). Null for every other mode. A plain cast of the spell
     * otherwise: "prepared_copy" (the copy of a prepared creature's prepare
     * spell, CR 722.3c) is cast by its spell name like any card. */
    static Class<?> copyCostKeyword(String mode) {
        if ("conspired".equals(mode)) {
            return mage.abilities.keyword.ConspireAbility.class;
        }
        if ("casualty".equals(mode)) {
            return mage.abilities.keyword.CasualtyAbility.class;
        }
        if ("teamworked".equals(mode)) {
            return mage.abilities.keyword.TeamworkAbility.class;
        }
        return null;
    }

    /** The activate command for a card's flashback: TestPlayer's
     * "@alias ability text" form when setup bound an alias to the card, so the
     * pick is scoped to that card, else the bare "Flashback" rule prefix.
     * TestPlayer splits that form at its first space, so the alias is the
     * space-free twin registerObjectAliases binds (abilityAliasRef). */
    static String flashbackCommand(String alias) {
        return alias == null || alias.isEmpty() ? "Flashback" : abilityAliasRef(alias) + " Flashback";
    }

    /** The space-free spelling of a scenario ref ("p0:Chatter of the
     * Squirrel" -> "p0:Chatter_of_the_Squirrel"), bound beside the ref
     * itself: TestPlayer's "@ref ability text" command ends the ref at its
     * first space, so a multi-word ref can only scope an ability pick
     * through this twin. No card name holds an underscore. */
    static String abilityAliasRef(String ref) {
        return ref.replace(' ', '_');
    }

    /** The XMage target string for one scenario target ref: its alias when
     * setup bound one, else the stripped, XMage-spelled card name. A token
     * ref keeps its "@" alias spelling even when setup bound none: the token
     * may not exist yet (a later step creates it, or setup places it at
     * start), so the steps are queued before any id can be bound.
     * ScriptedChoicePlayer resolves an "@...:token:" alias against the LIVE
     * game through tokenChoice, so the ref's creation-order #n reaches
     * exactly one of several same-named tokens. A scripted xmage_answers
     * target binds a post-setup ref (bindAnswerAlias) before it gets here.
     * An already-aliased ref is returned unchanged, so the funnel is
     * idempotent for a hand-authored "@..." answer. */
    private String targetName(String ref) {
        if (ref.startsWith("@")) {
            return ref;
        }
        String a = refAlias.get(ref);
        if (a != null) {
            return a;
        }
        return ref.contains(":token:") ? "@" + ref : xmageSpelling(refName(ref));
    }

    /** Resolve a named setup permanent ref, preserving duplicate suffixes. */
    private Permanent permanentRef(Game g, String ref) {
        int seatIndex = Integer.parseInt(ref.substring(1, ref.indexOf(':')));
        String name = xmageSpelling(refName(ref));
        int wanted = 1;
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            wanted = Integer.parseInt(ref.substring(hash + 1));
        }
        if (ref.contains(":token:")) {
            Permanent token = tokenChoice(ref, seat(seatIndex).getId(), g.getBattlefield().getAllPermanents());
            if (token == null) {
                throw new IllegalArgumentException("permanent ref " + ref + " is not on the battlefield");
            }
            return token;
        }
        int seen = 0;
        for (Permanent perm : g.getBattlefield().getAllPermanents()) {
            if (perm.getControllerId().equals(seat(seatIndex).getId())
                    && setupName(perm).equals(name)) {
                if (++seen == wanted) {
                    return perm;
                }
            }
        }
        throw new IllegalArgumentException("permanent ref " + ref + " is not on the battlefield");
    }

    private static List<String> names(JsonObject o, String key) {
        List<String> out = new ArrayList<>();
        if (o.has(key)) {
            for (JsonElement e : o.getAsJsonArray(key)) {
                out.add(e.getAsString());
            }
        }
        return out;
    }

    // ---- steps -----------------------------------------------------------

    private JsonObject sc0 = new JsonObject();
    private final List<String> cast = new ArrayList<>();

    /** Cards cast through a target adjuster: the cast-time chooser may ask for
     * another target after the scenario's answers run out (see
     * ScriptedChoicePlayer.chooseTarget). */
    private final Set<String> adjustedCasts = new java.util.HashSet<>();
    // Scenario refs a cast step put onto the battlefield face down (a
    // morphed, megamorphed or disguised cast) and no later step turned face
    // up: XMage's face-down permanent has no name, so a combat command names
    // it by TestPlayer's face-down test command instead (combatName).
    private final Set<String> faceDownRefs = new java.util.HashSet<>();
    // One batch per recorded attack/block step: the refs its commands were
    // lowered from and the PlayerAction each command added. XMage's
    // attack/block command takes only a name (plus a same-name ":k" index),
    // so it cannot distinguish a token from a card of the same name. A batch
    // is resolved against the LIVE battlefield inside selectAttackers/
    // selectBlockers, when a token created earlier in the replay exists; an
    // ambiguous command is replaced by an exact declaration.
    private final List<ExactAttackBatch> exactAttacks = new ArrayList<>();
    private final List<ExactBlockBatch> exactBlocks = new ArrayList<>();
    // Setup objects the scenario can name by ref ("p0:Grizzly Bears#2"): the
    // XMage alias each ref is registered under, so two same-name permanents
    // are told apart. Filled during setup, read by step targeting.
    private final java.util.Map<String, String> refAlias = new java.util.LinkedHashMap<>();
    // Per-seat per-name occurrence counter build() uses to spell setup refs
    // ("p0:Grizzly Bears", "p0:Grizzly Bears#2") in the same order XMage
    // adds the cards, so registerAliases can bind each to its object.
    private final java.util.Map<String, Integer> buildCounts = new java.util.HashMap<>();
    // Shared occurrence counter for split/Room front-half refs; register
    // FrontOccurrence keys it "seat|frontHalf" so each seat counts from 1,
    // the same rule buildCounts applies per seat to the whole name.
    private final java.util.Map<String, Integer> frontCounts = new java.util.HashMap<>();
    // Seeded setup permanents keyed "controllerId|xmageName" -> count (build()).
    private final java.util.Map<String, Integer> setupBattlefield = new java.util.HashMap<>();
    /** True once placeSetupTokens ran for this scenario's game (start() runs once;
     * build() resets it). */
    private boolean setupTokensPlaced;
    /** True once placeSetupCounters ran for this scenario's game (build() resets it). */
    private boolean setupCountersPlaced;
    // Whether the pre-start hook has linked the setup "attached" Auras.
    private boolean setupAttachedPlaced;
    // The permanent id of each battlefield placement build() seeded, by its
    // scenario ref ("p0:Grizzly Bears", "p0:Grizzly Bears#2"): the endpoints
    // placeSetupAttached links.
    private final Map<String, UUID> setupPermanentIds = new HashMap<>();
    private final Map<UUID, String> setupNames = new HashMap<>();
    // The name each setup battlefield placement was dealt under, by the id its
    // permanent will carry. Read only when XMage's live name is empty (see
    // setupObjectName); setupNames stays the staged back faces' map.
    private final Map<UUID, String> placedNames = new HashMap<>();
    private final Map<String, String> backFaceNames = new HashMap<>();
    // The card under test's two spellings: the scenario's (gorge/corpus) name
    // and XMage's card-database name when they differ (Forge prints "Dáin
    // Ironfoot", XMage stores "Dain Ironfoot"). xmageName is empty when equal.
    private String gorgeName = "";
    private String xmageName = "";

    /** The spelling to give XMage for a scenario card: the card under test's
     * XMage spelling, else the name unchanged. This is the spelling of the
     * card OBJECT (what addCard stores, what a target alias names), which for
     * a split/Room card is the whole "A // B" card. */
    private String xmageSpelling(String n) {
        return (!xmageName.isEmpty() && n.equals(gorgeName)) ? xmageName : n;
    }

    /** XMage's addCard treats a leading "SET-" as a set code (SystemUtil's
     * rule: 3-6 upper-case/digit characters then '-'), so a real card name
     * such as "DOT-7 Repair Squad" would be looked up as set DOT, card
     * "7 Repair Squad". When the parser would split the name, re-spell it
     * with the card's own set code so the first split leaves the whole name;
     * every name XMage does not split is passed through unchanged. This is an
     * override, not a call-site wrapper, so every addCard the driver makes --
     * setup, filler and library_top -- goes through the same rule. */
    @Override
    public void addCard(Zone gameZone, TestPlayer player, String cardName, int count, boolean tapped) {
        String setCode = "";
        if (!SystemUtil.parseSetAndCardNameCommand(cardName).get(0).isEmpty()) {
            CardInfo info = CardRepository.instance.findCard(cardName, true);
            if (info != null) {
                setCode = info.getSetCode();
            }
        }
        super.addCard(gameZone, player, setCode.isEmpty() ? cardName : setCode + "-" + cardName, count, tapped);
    }

    /** The front half of a split/Room card's "A // B" name, or the name
     * unchanged when it is not a split name. */
    static String frontHalf(String n) {
        int i = n.indexOf(" // ");
        return i < 0 ? n : n.substring(0, i);
    }

    /** The back half of a split/Room card's "A // B" name, or the name
     * unchanged when it is not a split name. */
    static String backHalf(String n) {
        int i = n.indexOf(" // ");
        return i < 0 ? n : n.substring(i + 4);
    }

    /** The scenario ref spelling a split/Room object by its front half
     * ("p0:Bottomless Pool"), or null when the XMage name is not a split
     * name. occurrence is the 1-based count of the front-half name among the
     * seat's objects, the same counting rule the whole-name refs use. Gorge
     * spells every card object by its front face (gorgeSpellingRule), so a
     * scenario that targets or sacrifices a Room fixture queues the ref
     * spelled by its front half, and the whole-name alias alone would leave
     * that queued ref unbound (XMage's "Targets list was setup by addTarget
     * ..., but not used" / "Found wrong choice command"). */
    static String frontHalfScenarioRef(int seat, String xmageCardName, int occurrence) {
        if (!xmageCardName.contains(" // ")) {
            return null;
        }
        return "p" + seat + ":" + frontHalf(xmageCardName) + (occurrence > 1 ? "#" + occurrence : "");
    }

    /** The name a setup object is spelled by in scenario refs. A staged back
     * face keeps its front (setupNames); an object XMage still names uses
     * that name; an object XMage names "" falls back to the name setup dealt
     * it under. A Room permanent with both doors locked has no name (CR
     * 709.5: it has neither half's name, and RoomCharacteristicsEffect
     * strips both), and a setup-placed Room is always both-locked, so its
     * live name alone would leave every ref to it unbound. */
    static String setupObjectName(Map<UUID, String> setupNames, Map<UUID, String> placedNames,
            UUID id, String liveName) {
        String staged = setupNames.get(id);
        if (staged != null) {
            return staged;
        }
        if (liveName.isEmpty()) {
            return placedNames.getOrDefault(id, liveName);
        }
        return liveName;
    }

    private String setupName(mage.MageObject o) {
        return setupObjectName(setupNames, placedNames, o.getId(), o.getName());
    }

    /** Records the card setup just dealt to the battlefield (the last
     * placement) under the id its permanent will carry. A DFC half staged by
     * stageBackFace is already in setupNames, which wins. */
    static void recordPlacedName(Map<UUID, String> placedNames, List<PutToBattlefieldInfo> placements,
            String name) {
        placedNames.put(placements.get(placements.size() - 1).getMainCard().getId(), name);
    }

    /** Binds one setup object's refs during registerAliases: counts the
     * whole name (xmageCardName) and its scenario spelling per seat, then
     * hands the refs to registerObjectAliases. Static so the driver tests
     * can drive the whole ref computation without a game. */
    static void bindSetupObject(int seat, String xmageCardName, String gorgeName, String xmageName,
            Map<String, Integer> counts, Map<String, Integer> scenarioCounts,
            Map<String, Integer> frontCounts, UUID id, Map<String, String> refAlias, AliasBinder bind) {
        int k = counts.merge(xmageCardName, 1, Integer::sum);
        String xmageRef = "p" + seat + ":" + xmageCardName + (k > 1 ? "#" + k : "");
        String scenarioCardName = !xmageName.isEmpty() && xmageCardName.equals(xmageName)
                ? gorgeName : xmageCardName;
        int scenarioOccurrence = scenarioCounts.merge(scenarioCardName, 1, Integer::sum);
        String scenarioRef = "p" + seat + ":" + scenarioCardName
                + (scenarioOccurrence > 1 ? "#" + scenarioOccurrence : "");
        // A split/Room object also gets its front-half ref bound (see
        // frontHalfScenarioRef); the occurrence counts follow the same rule
        // the whole name uses.
        int frontOccurrence = registerFrontOccurrence(frontCounts, seat, xmageCardName);
        registerObjectAliases(seat, xmageCardName, xmageRef, scenarioRef, frontOccurrence, id,
                refAlias, bind);
    }

    /** One alias binding shared by both seats, duplicates swallowed: the
     * choosing player resolves the target string, and it may be either seat. */
    interface AliasBinder {
        void bind(String ref, UUID id);
    }

    /** Counts one split/Room object's front-half occurrence into the shared
     * setup counter, keyed by seat (0 when the name is not a split name).
     * The seat is part of the KEY, not the caller's loop state: p0's and
     * p1's first copy of the same split/Room card each spell
     * "p<N>:<Front>" with no #k suffix, the same rule the whole name
     * follows, and a caller cannot reintroduce a seat-agnostic count by
     * sharing its counter. Extracted so the driver contract test can
     * exercise the per-seat counting rule without a game. */
    static int registerFrontOccurrence(java.util.Map<String, Integer> frontCounts, int seat, String xmageCardName) {
        if (!xmageCardName.contains(" // ")) {
            return 0;
        }
        return frontCounts.merge(seat + "|" + frontHalf(xmageCardName), 1, Integer::sum);
    }

    /** Binds one object's scenario-ref aliases during registerAliases: the
     * whole-name scenario ref (as before), its XMage-spelled ref, and --
     * new -- the front-half ref of a split/Room card
     * (frontHalfScenarioRef). frontOccurrence is the front-half occurrence
     * count, 0 when the object is not a split name. Extracted so the driver
     * contract test can exercise the front-half binding without a game
     * (registerAliases walks live zones, which need the card database). */
    static void registerObjectAliases(int seat, String xmageCardName, String xmageRef,
            String scenarioRef, int frontOccurrence, UUID id, java.util.Map<String, String> refAlias,
            AliasBinder bind) {
        refAlias.put(scenarioRef, "@" + scenarioRef);
        refAlias.putIfAbsent(xmageRef, "@" + scenarioRef);
        bind.bind(scenarioRef, id);
        if (!abilityAliasRef(scenarioRef).equals(scenarioRef)) {
            bind.bind(abilityAliasRef(scenarioRef), id);
        }
        if (!xmageRef.equals(scenarioRef)) {
            bind.bind(xmageRef, id);
        }
        if (frontOccurrence > 0) {
            String frontRef = frontHalfScenarioRef(seat, xmageCardName, frontOccurrence);
            if (frontRef != null && !frontRef.equals(scenarioRef) && !refAlias.containsKey(frontRef)) {
                refAlias.put(frontRef, "@" + frontRef);
                bind.bind(frontRef, id);
            }
        }
    }

    /** The spelling XMage's cast command matches: the name of the card's
     * SpellAbility, which differs from the card object's name for a split or
     * Room card. XMage names a half's ability "Cast <half>" (SplitCard splits
     * the set info name on " // "), while the physical card added to hand is
     * the whole "A // B"; casting the whole name finds no ability ("Can't
     * find ability to activate command: Cast Walk-In Closet"). So a cast
     * step names the scenario's face (gorgeName) and setup still deals the
     * whole card through xmageSpelling. A whole-name cast of a card that is
     * NOT the card under test -- a generator probe Room dealt by setup
     * (roomUnlockProbe) -- is the front half's ability the same way; the
     * under-test whole name maps to gorgeName so an alias spelling is kept.
     * Returns null when the name is not a split spelling and the caller
     * falls through to xmageSpelling. */
    static String castSpellingRule(String gorgeName, String xmageName, String n) {
        if (!xmageName.isEmpty() && n.equals(gorgeName) && xmageName.contains(" // ")) {
            return gorgeName;
        }
        if (n.contains(" // ")) {
            return n.equals(xmageName) ? gorgeName : frontHalf(n);
        }
        return null;
    }

    /** The XMage cast name of an Adventure or Omen card's spell half ("Stomp"
     * for Bonecrusher Giant, "Absorb Essence" for Purging Stormbrood), or
     * null when name is not such a parent card.
     *
     * The farm's cast step names the parent card, not the half: the xengine
     * lowering puts the setup-bound ref on the step (refBinder.refOf keeps the
     * dealt card's name, e.g. "p1:Grabby Giant"), while XMage casts the half
     * through its own spell name. Adventure and Omen share the role --
     * CardWithSpellOption carries the inset spell card XMage names -- so this
     * matches the type, not a hardcoded card or mode list. A step that already
     * names the half returns null and falls through to castSpelling, so both
     * shapes replay; so does every card without an inset spell. */
    static String spellOptionCastName(String name) {
        CardInfo info = CardRepository.instance.findCard(name, true);
        Card c = info == null ? null : info.createCard();
        if (!(c instanceof CardWithSpellOption)) {
            return null;
        }
        String half = ((CardWithSpellOption) c).getSpellCard().getName();
        return half == null || half.isEmpty() || half.equals(name) ? null : half;
    }

    /** The XMage cast name an "alt" cast mode resolves to, or null when the
     * mode does not name one of the card's alternate faces. Adventure and
     * Omen halves are inset spell cards (spellOptionCastName); a Room is a
     * split card and "room_alt" is its second door, whose own name is the
     * back half of the whole "A // B" spelling (CR 709.3, 709.5), which is
     * what XMage's split-half SpellAbility is called. "modal_spell" is a
     * modal double-faced card's back face (CR 712.12), whose XMage
     * SpellAbility is the right half card, named for that half
     * (modalBackFaceName). A name with no such back half falls through to
     * castSpelling (the step has to name the ability itself) instead of
     * guessing at a half. */
    static String altCastHalfName(String mode, String xmageName) {
        if ("adventure_alt".equals(mode) || "omen_alt".equals(mode)) {
            return spellOptionCastName(xmageName);
        }
        if ("room_alt".equals(mode)) {
            return xmageName.contains(" // ") ? backHalf(xmageName) : null;
        }
        if ("modal_spell".equals(mode)) {
            return modalBackFaceName(xmageName);
        }
        return null;
    }

    /** The XMage cast name of a modal double-faced card's back face (the
     * right half), or null when the name is not one. CR 712.12 lets a player
     * cast such a card with its back face up, and XMage's castSpell then
     * takes the right half's own SpellAbility ("Cast <back face>"), exactly
     * as a split card's second half is cast by its half name. Matched by card
     * type, not a card or mode list, so every modal DFC -- including one
     * printed after this driver -- resolves; a step that already names the
     * half falls through to castSpelling and still replays. The lookup is the
     * front half of a whole "A // B" spelling because XMage's card object is
     * registered under the front face. */
    static String modalBackFaceName(String xmageName) {
        String lookup = xmageName.contains(" // ") ? frontHalf(xmageName) : xmageName;
        CardInfo info = CardRepository.instance.findCard(lookup, true);
        Card c = info == null ? null : info.createCard();
        if (!(c instanceof ModalDoubleFacedCard)) {
            return null;
        }
        String back = ((ModalDoubleFacedCard) c).getRightHalfCard().getName();
        return back == null || back.isEmpty() || back.equals(xmageName) ? null : back;
    }

    private String castSpelling(String n) {
        String s = castSpellingRule(gorgeName, xmageName, n);
        return s != null ? s : xmageSpelling(n);
    }

    /** One snapshot value's spelling rewrite for a STACK SPELL's source, for
     * a card under test that is not known to be a Room (see the four-argument
     * form). gorge spells a non-Room split or MDFC spell by its setup ref, the
     * front face, so XMage's whole "A // B" object name reads as the front
     * half, and the spell of a face-1 cast -- which XMage names by the back
     * half's ability -- is rewritten to the front. The card under test's back
     * half is the only back half a scenario can name, so it is the only alias.
     * The back-half alias is valid ONLY for a spell: a Room permanent whose
     * back door is unlocked really is named for that back half (CR 709.5),
     * and gorge reports it so; use gorgeObjectSpellingRule for every other
     * snapshot string. */
    static String gorgeSpellingRule(String gorgeName, String xmageName, String s) {
        return gorgeSpellingRule(gorgeName, xmageName, false, s);
    }

    /** The stack-spell rewrite with the card under test's Room-ness known.
     * A Room spell has only the characteristics of the door it was cast as
     * (CR 709.3b), so its name is that door's name, whichever door it is
     * (CR 709.5); gorge names it so (engine/rules/oracle_snapshot.go
     * stackSourceRef), so a Room's back-door spell keeps its name and only a
     * non-Room split/MDFC back face takes the front-face alias. */
    static String gorgeSpellingRule(String gorgeName, String xmageName, boolean room, String s) {
        if (!room && !xmageName.isEmpty() && xmageName.contains(" // ") && s.equals(backHalf(xmageName))) {
            return gorgeName;
        }
        return gorgeObjectSpellingRule(gorgeName, xmageName, s);
    }

    /** The spelling rewrite for every snapshot string that is not a stack
     * spell's source or a permanent's name (hand, graveyard, exile, library,
     * ability sources, targets): the whole object name reads as the front
     * half, and a bare back-half name is left alone, because a Room's
     * unlocked back door is a real name. */
    static String gorgeObjectSpellingRule(String gorgeName, String xmageName, String s) {
        if (!xmageName.isEmpty() && s.equals(xmageName)) {
            return gorgeName;
        }
        return frontHalf(s);
    }

    /** The name a stack ability's source is reported under. An ability of a
     * double-faced card that has left the battlefield is still the ability its
     * own face printed (CR 113.7a, 712.8e), and gorge names it by that face
     * (rules.abilitySourceRef): Aclazotz's dies trigger is the front face's, so
     * gorge reads "Aclazotz, Deepest Betrayal" while XMage's source lookup can
     * resolve to the back half ("Temple of the Dead"). So for a non-Room
     * DoubleFacedCard (a Room is a SplitCard: its door names are real, CR
     * 709.5) that is not a battlefield permanent -- a permanent on its back face
     * is named by that face on both sides -- the name is the half whose own
     * abilities include the stack ability, matched by id, original id, or rule
     * text. An ability neither half owns (a granted one) keeps the object's own
     * name. Card-based, so a test pins it without a game. */
    static String abilitySourceName(mage.MageObject source, Ability ability) {
        if (!(source instanceof Card) || source instanceof Permanent || ability == null) {
            return source.getName();
        }
        Card main = ((Card) source).getMainCard();
        if (!(main instanceof DoubleFacedCard)) {
            return source.getName();
        }
        DoubleFacedCard df = (DoubleFacedCard) main;
        boolean left = ownsAbility(df.getLeftHalfCard(), ability);
        boolean right = ownsAbility(df.getRightHalfCard(), ability);
        if (left != right) {
            return (left ? df.getLeftHalfCard() : df.getRightHalfCard()).getName();
        }
        return source.getName();
    }

    private static boolean ownsAbility(Card half, Ability ability) {
        String rule = ability.getRule();
        for (Ability a : half.getAbilities()) {
            if (a.getId().equals(ability.getId()) || a.getOriginalId().equals(ability.getOriginalId())
                    || a.getRule().equals(rule)) {
                return true;
            }
        }
        return false;
    }

    /** The spelling rewrite for a battlefield permanent's name (a snapshot
     * permanent's "name" and "attached_to"). The only permanent XMage names
     * "A // B" is a Room with both doors unlocked, and that IS its name (CR
     * 709.5: it has both halves' names, the same "Left // Right" gorge binds,
     * engine/rules/oracle_run.go fieldName), so it is never collapsed to the front
     * half -- not even for the card under test, whose scenario spelling is
     * its front face. Any other name takes the card under test's spelling
     * alias as gorgeObjectSpellingRule does. */
    static String gorgePermanentSpellingRule(String gorgeName, String xmageName, String s) {
        if (s.contains(" // ")) {
            return s;
        }
        return gorgeObjectSpellingRule(gorgeName, xmageName, s);
    }

    /** Whether the XMage card is a Room (CR 709.3; XMage's RoomCard). Card-
     * based and package-private so a test can pin it without the card
     * repository (which needs H2). */
    static boolean isRoomCard(Card c) {
        return c instanceof mage.cards.RoomCard;
    }

    // Room-ness per XMage card name: a card's class never changes within a
    // run, so one repository lookup per name is enough.
    private static final Map<String, Boolean> ROOM_NAMES = new java.util.concurrent.ConcurrentHashMap<>();

    /** Whether the named XMage card ("A // B") is a Room. Package-private
     * and overridable so a no-H2 test double can answer without the card
     * repository. */
    boolean cardIsRoom(String xmageCardName) {
        if (!xmageCardName.contains(" // ")) {
            return false;
        }
        return ROOM_NAMES.computeIfAbsent(xmageCardName, n -> {
            CardInfo info = CardRepository.instance.findCard(n);
            return isRoomCard(info == null ? null : info.createCard());
        });
    }

    /** Rewrites XMage's spelling of the card under test back to the scenario's
     * (gorge) spelling throughout a value, so the comparator sees one name. */
    private JsonElement gorgeSpellings(JsonElement e) {
        return gorgeSpellings(e, Spelling.OBJECT);
    }

    /** Which rewrite a snapshot string takes: an ordinary object name, a
     * stack spell's source, or a battlefield permanent's name. PERMANENTS
     * marks the "permanents" array, whose objects' name fields are
     * PERMANENT. */
    private enum Spelling { OBJECT, SPELL, PERMANENT, PERMANENTS }

    private JsonElement gorgeSpellings(JsonElement e, Spelling how) {
        if (e.isJsonPrimitive() && e.getAsJsonPrimitive().isString()) {
            String s = e.getAsString();
            String r;
            switch (how) {
                case SPELL:
                    // Only the card under test's back half can take the alias,
                    // so the repository is asked only then.
                    boolean room = s.equals(backHalf(xmageName)) && cardIsRoom(xmageName);
                    r = gorgeSpellingRule(gorgeName, xmageName, room, s);
                    break;
                case PERMANENT:
                    r = gorgePermanentSpellingRule(gorgeName, xmageName, s);
                    break;
                default:
                    r = gorgeObjectSpellingRule(gorgeName, xmageName, s);
            }
            return r.equals(s) ? e : new JsonPrimitive(r);
        }
        if (e.isJsonArray()) {
            JsonArray a = new JsonArray();
            for (JsonElement x : e.getAsJsonArray()) {
                a.add(gorgeSpellings(x, how == Spelling.PERMANENTS ? Spelling.PERMANENTS : Spelling.OBJECT));
            }
            return a;
        }
        if (e.isJsonObject()) {
            JsonObject in = e.getAsJsonObject();
            // A stack entry {"kind":"spell","source":name}: only its source
            // is the spell's name and takes the face-1 back-half alias.
            JsonElement kind = in.get("kind");
            boolean spell = kind != null && kind.isJsonPrimitive() && "spell".equals(kind.getAsString());
            boolean permanent = how == Spelling.PERMANENTS;
            JsonObject o = new JsonObject();
            for (Map.Entry<String, JsonElement> en : in.entrySet()) {
                String k = en.getKey();
                Spelling child = Spelling.OBJECT;
                if (spell && k.equals("source")) {
                    child = Spelling.SPELL;
                } else if (permanent && (k.equals("name") || k.equals("attached_to"))) {
                    child = Spelling.PERMANENT;
                } else if (!permanent && k.equals("permanents")) {
                    child = Spelling.PERMANENTS;
                }
                o.add(k, gorgeSpellings(en.getValue(), child));
            }
            return o;
        }
        return e;
    }

    // The item's per-step XMage ability text (parallel to the steps; empty
    // except on activate steps). H1, confirmed against the XMage source:
    // TestPlayer.hasAbilityTargetNameOrAlias (Mage.Tests .../player/
    // TestPlayer.java) selects an activated ability with
    // `ability.toString().startsWith(nameOrAlias)`, and AbilityImpl.toString()
    // is getRule(), so the item's value is the printed rule text's prefix
    // ("{T}", "Equip {2}"). The activate: / manaActivate: handlers route it
    // through that match before activating.
    private JsonArray xabilities = new JsonArray();

    /**
     * The X of a Waterbend {X} activation as the transcript records it: every
     * waterbend tap label pays one generic (its "for N"), and the step's
     * recorded pool spend is the rest of the waterbend cost. An explicit
     * amount answer or "X=<n>" choice is XMage's own X and is left to
     * TestPlayer.announceX: nothing is inferred beside it, so the step never
     * queues two X answers.
     */
    static int waterbendXFromAnswers(JsonArray as, String payment) {
        int x = payment == null ? 0 : payment.length();
        for (JsonElement e : as) {
            JsonObject a = e.getAsJsonObject();
            String kind = str(a, "kind");
            if ("amount".equals(kind)) {
                return 0;
            }
            if (!"choice".equals(kind)) {
                continue;
            }
            String v = str(a, "value");
            if (v.startsWith("X=")) {
                return 0;
            }
            if (ScriptedChoicePlayer.waterbendTapName(v) == null) {
                continue;
            }
            int at = v.lastIndexOf(" for ");
            if (at >= 0) {
                try {
                    x += Integer.parseInt(v.substring(at + 5).trim());
                    continue;
                } catch (NumberFormatException ignored) {
                    // no contribution: count the tap anyway
                }
            }
            x += 1;
        }
        return x;
    }

    /** Step i's XMage ability text, or "" when the item carries none. */
    private String xabilityAt(int i) {
        if (i < xabilities.size() && xabilities.get(i).isJsonPrimitive()) {
            return activationCommandText(xabilities.get(i).getAsString());
        }
        return "";
    }

    /**
     * CrewAbility and SaddleAbility render their rule prefix followed by
     * HTML-wrapped reminder text. Keep the identifying ability-class prefix:
     * TestPlayer matches activated commands against ability.toString().startsWith
     * (command), and these reminders have differed between Forge and XMage.
     */
    static String activationCommandText(String text) {
        java.util.regex.Matcher keyword = java.util.regex.Pattern
                .compile("^(Crew|Saddle)\\s+\\d+\\b", java.util.regex.Pattern.CASE_INSENSITIVE)
                .matcher(text);
        if (keyword.find()) {
            return keyword.group();
        }
        return text;
    }

    // The item's xmage_target_skips: parallel to the steps, entry i lists the
    // empty optional target objects of step i's cast as {"at": n, "slot": k}.
    // "at" is the number of filled targets that precede the object, so the
    // skip is queued before target n (consecutive empty objects repeat n, a
    // trailing one has n == the target count). "slot" identifies the XMage
    // object too: both engines must have the same independent 0..1 shape.
    private JsonArray xtargetSkips = new JsonArray();

    /** The queue offsets of step i's explicit target skips, validated against
     * the cast's target count; empty when the item carries none. A malformed
     * plan fails the replay rather than queueing a speculative skip. */
    static JsonArray readTargetSkips(JsonObject sc, JsonArray steps) {
        if (!sc.has("xmage_target_skips")) {
            return new JsonArray();
        }
        JsonElement raw = sc.get("xmage_target_skips");
        if (!raw.isJsonArray() || raw.getAsJsonArray().size() != steps.size()) {
            throw new IllegalArgumentException("xmage_target_skips must be parallel to steps");
        }
        JsonArray plan = raw.getAsJsonArray();
        for (int i = 0; i < plan.size(); i++) {
            JsonElement entry = plan.get(i);
            if (!entry.isJsonNull() && (!entry.isJsonArray()
                    || (entry.getAsJsonArray().size() > 0 && !str(steps.get(i).getAsJsonObject(), "op").equals("cast")))) {
                throw new IllegalArgumentException("step " + i + " has invalid xmage_target_skips");
            }
        }
        return plan;
    }

    private List<Integer> castTargetSkipsAt(int i, String card, int targetCount) {
        if (i >= xtargetSkips.size() || xtargetSkips.get(i).isJsonNull()
                || xtargetSkips.get(i).getAsJsonArray().size() == 0) {
            return new ArrayList<>();
        }
        CardInfo info = CardRepository.instance.findCard(card);
        Card c = info == null ? null : info.createCard();
        if (c == null || hasTargetAdjuster(c.getSpellAbility())) {
            throw new IllegalArgumentException("cannot establish explicit target objects for " + card);
        }
        return validateTargetSkips(xtargetSkips.get(i).getAsJsonArray(),
                c.getSpellAbility().getAllSelectedTargets(), targetCount);
    }

    /** Check correspondence against XMage itself before closing an object.
     * Every target object takes a contiguous run of the queued targets. An
     * object that received fewer than its maximum stays open and must be
     * closed by a skip at its end; an object that received its maximum is
     * already closed, so it may not carry a skip; and an object with no skip
     * of its own must be filled to its maximum (a partly filled object with
     * no skip would have kept asking). */
    static List<Integer> validateTargetSkips(JsonArray plan, List<mage.target.Target> targets, int targetCount) {
        if (targetCount < 0) {
            throw new IllegalArgumentException("explicit target plan has a negative target count");
        }
        List<Integer> ats = new ArrayList<>();
        List<Integer> slots = new ArrayList<>();
        int prevAt = 0;
        int prevSlot = -1;
        for (JsonElement e : plan) {
            if (!e.isJsonObject()) {
                throw new IllegalArgumentException("invalid target skip: " + e);
            }
            int at = targetSkipIndex(e.getAsJsonObject(), "at");
            int slot = targetSkipIndex(e.getAsJsonObject(), "slot");
            if (at < prevAt || at > targetCount) {
                throw new IllegalArgumentException("target skip is out of order or beyond its objects: " + e);
            }
            if (slot <= prevSlot || slot >= targets.size()) {
                throw new IllegalArgumentException("target skip names an out-of-order or unknown object: " + e);
            }
            if (targets.get(slot) instanceof mage.target.TargetAmount) {
                throw new IllegalArgumentException("explicit target plan requires independent objects");
            }
            ats.add(at);
            slots.add(slot);
            prevAt = at;
            prevSlot = slot;
        }
        List<Integer> out = new ArrayList<>();
        int at = 0;
        int closed = -1;
        for (int j = 0; j < ats.size(); j++) {
            int end = ats.get(j);
            int slot = slots.get(j);
            int picks = end - at;
            for (int i = closed + 1; i < slot; i++) {
                picks -= maximumOf(targets.get(i), i);
            }
            if (picks < 0) {
                throw new IllegalArgumentException("target skip at " + end + " leaves an earlier object unfilled");
            }
            mage.target.Target t = targets.get(slot);
            if (picks < t.getMinNumberOfTargets() || picks >= t.getMaxNumberOfTargets()) {
                throw new IllegalArgumentException("object " + slot + " takes " + picks
                        + " picks, but a closing skip requires fewer than " + t.getMaxNumberOfTargets()
                        + " and at least " + t.getMinNumberOfTargets());
            }
            out.add(end);
            at = end;
            closed = slot;
        }
        int rest = targetCount - at;
        for (int i = closed + 1; i < targets.size(); i++) {
            rest -= maximumOf(targets.get(i), i);
        }
        if (rest != 0) {
            throw new IllegalArgumentException("the plan accounts for " + (targetCount - rest)
                    + " of " + targetCount + " targets");
        }
        return out;
    }

    /** An object's maximum target count, or a loud failure when it has none
     * to fill (a shape the explicit plan does not cover). */
    private static int maximumOf(mage.target.Target t, int index) {
        if (t.getMaxNumberOfTargets() < 1) {
            throw new IllegalArgumentException("target object " + index + " has no maximum to fill");
        }
        return t.getMaxNumberOfTargets();
    }

    private static int targetSkipIndex(JsonObject skip, String key) {
        JsonElement value = skip.get(key);
        if (value == null || !value.isJsonPrimitive() || !value.getAsJsonPrimitive().isNumber()) {
            throw new IllegalArgumentException("target skip requires integer " + key);
        }
        try {
            return value.getAsBigDecimal().intValueExact();
        } catch (ArithmeticException ex) {
            throw new IllegalArgumentException("target skip requires integer " + key, ex);
        }
    }

    /** Queue a cast step's recorded targets, then cast, and nothing else.
     * Both the ordinary two-or-more-target arm and the Zone.STACK
     * cast-trigger arm call this one method; neither appends a speculative
     * trailing skip. XMage stops asking once a slot's candidates are
     * exhausted (Hunters' Feast with every player chosen), so a skip
     * computed from the card's target maximums is left unused ("Targets ...
     * Count are not equal (found [[target_skip]])"). A slot XMage really
     * poses again with no scripted answer is closed at ask time by
     * closeCastAskedAgain. The cast is a callback so the unit test drives
     * the whole arm (queue and cast) without a game. */
    void castWithQueuedTargets(TestPlayer p, List<String> tg, Runnable cast) {
        for (String t : tg) {
            queueCastTarget(p, t);
        }
        cast.run();
    }

    /** Queue the cast's targets with one "[target_skip]" before each offset. */
    private void queueCastTargetsWithSkips(TestPlayer p, List<String> tg, List<Integer> skips) {
        int next = 0;
        for (int k = 0; k <= tg.size(); k++) {
            while (next < skips.size() && skips.get(next) == k) {
                addTarget(p, TestPlayer.TARGET_SKIP);
                next++;
            }
            if (k < tg.size()) {
                queueCastTarget(p, tg.get(k));
            }
        }
    }

    /** Whether the named card has an activated mana ability whose rule text
     * starts with text; the activate step then resolves the named permanent
     * and activates exactly that ability (activateManaAbilityOnCard). */
    private static boolean isManaAbilityText(String name, String text) {
        CardInfo info = CardRepository.instance.findCard(name, true);
        Card c = info == null ? null : info.createCard();
        if (c == null) {
            return false;
        }
        if (c instanceof DoubleFacedCard && ((DoubleFacedCard) c).getRightHalfCard().getName().equals(name)) {
            c = ((DoubleFacedCard) c).getRightHalfCard();
        }
        for (Ability a : c.getAbilities()) {
            if (a.toString().startsWith(text)) {
                return a instanceof ActivatedManaAbilityImpl;
            }
        }
        // No printed ability carries the text: a GAINED ability (Locus of
        // Enlightenment's craft materials, CR 613.1f). Its rule text is the
        // only thing known before the game runs, so a plain "{T}: Add {G}"
        // tap-for-mana text is activated as the mana ability it is (CR
        // 605.1a); TestPlayer's activate command never finds a mana ability.
        return isTapManaText(text);
    }

    /** The abilities of the XMage token the setup table maps to a token
     * name; empty for a name it does not map. A token is no card in the
     * CardRepository, so this is the only script-time view of its rules. */
    static List<Ability> tokenAbilities(String name) {
        List<Ability> out = new ArrayList<>();
        for (java.util.function.Supplier<mage.game.permanent.token.Token> make : SETUP_TOKENS.values()) {
            mage.game.permanent.token.Token token = make.get();
            if (token.getName().equals(name)) {
                out.addAll(token.getAbilities());
                return out;
            }
        }
        return out;
    }

    /** Whether an XMage rule starts with a scenario ability prefix, reading
     * the prefix's "{this}" as any self-reference XMage words a token's cost
     * with ("this artifact", "this token", "this creature") or "{this}". */
    static boolean tokenRuleStartsWith(String rule, String text) {
        String[] parts = text.split("\\{this\\}", -1);
        StringBuilder re = new StringBuilder();
        for (int i = 0; i < parts.length; i++) {
            if (i > 0) {
                re.append("(?:\\{this\\}|this [a-z]+)");
            }
            re.append(java.util.regex.Pattern.quote(parts[i]));
        }
        return java.util.regex.Pattern.compile(re.toString(), java.util.regex.Pattern.CASE_INSENSITIVE)
                .matcher(rule).lookingAt();
    }

    /** The XMage rule text a token activation selects by: the mapped token's
     * one ability whose rule tokenRuleStartsWith the prefix. The prefix
     * itself when the token is unmapped or no single rule matches. */
    static String tokenAbilityText(String name, String text) {
        String found = null;
        for (Ability a : tokenAbilities(name)) {
            String rule = a.toString();
            if (!tokenRuleStartsWith(rule, text)) {
                continue;
            }
            if (found != null && !found.equals(rule)) {
                return text;
            }
            found = rule;
        }
        return found == null ? text : found;
    }

    /** isManaAbilityText for a token ref: the mapped XMage token's ability
     * with that rule (a Treasure's "{T}, Sacrifice this artifact: Add one
     * mana of any color" is a mana ability, CR 605.1a). A token the table
     * does not map keeps the tap-for-mana text shape. */
    static boolean isTokenManaAbilityText(String name, String text) {
        for (Ability a : tokenAbilities(name)) {
            if (a.toString().startsWith(text)) {
                return a instanceof ActivatedManaAbilityImpl;
            }
        }
        return isTapManaText(text);
    }

    /** Queue a mana-ability activation on the step's own source.
     *
     * TestPlayer's manaActivate: command walks every available mana producer
     * and takes the first whose rule text starts with the command, so a
     * "{T}" prefix (Hidden Grotto, an untapped Island) lands on whichever
     * producer XMage lists first. Here the source is resolved by its scenario
     * ref first -- name plus the 1-based "#N" instance on the activating seat
     * -- and only its own available abilities are considered. The source need
     * not be a battlefield permanent: a mana ability printed for the hand,
     * graveyard or exile (Simian Spirit Guide, Elvish Spirit Guide) is
     * resolved as the card in that zone, exactly as TestPlayer's old
     * instanceof Card branch did. An object or ability that cannot be
     * resolved fails the window with a harness: reason; it never falls back
     * to the global scan. */
    private void activateManaAbilityOnCard(int stepIdx, TestPlayer p, String ref, String card,
            String text, Integer abilityIndex) {
        runCode("mana ability " + card + " \"" + text + "\"", turn, phase, p, (info, pl, g) -> {
            for (ManaSource source : namedManaSources(g, pl, ref, card, stepIdx)) {
                ActivatedManaAbilityImpl ability = findManaAbility(source.card, source.zone, p, g,
                        text, abilityIndex, card, stepIdx);
                if (ability == null) {
                    continue;
                }
                if (!pl.activateAbility(ability.copy(), g)) {
                    throw new IllegalStateException("harness: activate step " + stepIdx + " could not activate \""
                            + text + "\" on " + card + " for " + pl.getName());
                }
                return;
            }
            throw new IllegalStateException("harness: activate step " + stepIdx + " on " + card
                    + " has no mana ability for \"" + text + "\""
                    + (abilityIndex == null ? "" : " at ability_index " + abilityIndex));
        });
    }

    /** A scenario object that can carry a mana ability: a battlefield
     * Permanent, or the Card in hand, graveyard, exile or library, plus the
     * zone it currently occupies. */
    private static final class ManaSource {
        final Card card;
        final Zone zone;

        ManaSource(Card card, Zone zone) {
            this.card = card;
            this.zone = zone;
        }
    }

    /** The objects a scenario ref names, in the order the activating player
     * should try them; every one is a same-named object of the ref, never a
     * global producer scan.
     *
     * A live alias permanent is the ref's one object. Otherwise the alias
     * card comes first in the zone it currently occupies (hand, graveyard,
     * exile or library), which is where a card-zone mana ability lives, then
     * the same-named battlefield objects (the only one or the ref's Nth).
     * This ordering also keeps the pre-existing play-land identity rule: a
     * land played by name is XMage's pick among same-named cards, so
     * "p0:Plains#2" can still point at the hand copy while the only Plains
     * permanent is the one the ref means -- the hand Plains has no hand-zone
     * mana ability, so the battlefield candidate is the one that resolves.
     * With no alias at all, battlefield then non-battlefield name matches
     * follow registerAliases' zone order. */
    private List<ManaSource> namedManaSources(Game g, Player p, String ref, String card, int stepIdx) {
        if (ref.contains(":token:")) {
            // A token's ref ranks the controller's live tokens of the name in
            // creation order, as every other token ref (tokenChoice).
            List<ManaSource> one = new ArrayList<>();
            Permanent token = tokenChoice(ref, p.getId(), g.getBattlefield().getAllPermanents());
            if (token != null) {
                one.add(new ManaSource(token, Zone.BATTLEFIELD));
            }
            return one;
        }
        UUID boundId = scenarioObjectId(g, p, ref);
        if (boundId != null) {
            Permanent bound = boundPermanent(boundId, g.getBattlefield().getAllPermanents());
            if (bound != null) {
                if (!bound.getControllerId().equals(p.getId())) {
                    throw new IllegalStateException("harness: activate step " + stepIdx + " names " + card
                            + ", which " + p.getName() + " does not control");
                }
                List<ManaSource> one = new ArrayList<>();
                one.add(new ManaSource(bound, Zone.BATTLEFIELD));
                return one;
            }
        }
        String front = xmageSpelling(refName(ref));
        String live = battlefieldName(ref);
        int wanted = refOrdinal(ref);
        List<ManaSource> sources = new ArrayList<>();
        java.util.Set<UUID> seen = new java.util.HashSet<>();
        if (boundId != null) {
            Card boundCard = g.getCard(boundId);
            Zone boundZone = g.getState().getZone(boundId);
            // A transformed double-faced card's setup alias can name the OLD
            // half after the craft/transform returned a new card object to the
            // battlefield; that half is OUTSIDE and has no live mana ability.
            // Only a card in a real zone is a candidate.
            if (boundCard != null && boundZone != null && boundZone != Zone.OUTSIDE) {
                seen.add(boundCard.getId());
                sources.add(new ManaSource(boundCard, boundZone));
            }
        }
        List<ManaSource> battlefield = new ArrayList<>();
        for (Permanent perm : g.getBattlefield().getAllPermanents()) {
            if (perm.getControllerId().equals(p.getId()) && !seen.contains(perm.getId())
                    && matchesRef(perm, front, live)) {
                battlefield.add(new ManaSource(perm, Zone.BATTLEFIELD));
            }
        }
        ManaSource onBattlefield = nthSource(battlefield, wanted);
        if (onBattlefield == null) {
            // A land played from a hand of same-named cards keeps the ordinal it
            // had among them, which no permanent has, and its alias can still
            // name a copy left in hand: any untapped one of the name makes the
            // same mana.
            // Never one a later step names by its own ordinal: that step
            // taps it, and taking it here leaves the later tap nothing.
            java.util.Set<Integer> reserved = laterNamedOrdinals(ref, stepIdx);
            for (int i = 0; i < battlefield.size(); i++) {
                ManaSource s = battlefield.get(i);
                if (s.card instanceof Permanent && !((Permanent) s.card).isTapped()
                        && !reserved.contains(i + 1)) {
                    onBattlefield = s;
                    break;
                }
            }
            for (ManaSource s : battlefield) {
                if (onBattlefield == null && s.card instanceof Permanent && !((Permanent) s.card).isTapped()) {
                    onBattlefield = s;
                }
            }
        }
        if (onBattlefield != null) {
            seen.add(onBattlefield.card.getId());
            sources.add(onBattlefield);
        }
        List<ManaSource> offBattlefield = new ArrayList<>();
        for (Card c : p.getHand().getCards(g)) {
            if (matchesRef(c, front, live)) {
                offBattlefield.add(new ManaSource(c, Zone.HAND));
            }
        }
        for (Card c : p.getGraveyard().getCards(g)) {
            if (matchesRef(c, front, live)) {
                offBattlefield.add(new ManaSource(c, Zone.GRAVEYARD));
            }
        }
        for (Card c : g.getExile().getAllCards(g)) {
            if (p.getId().equals(c.getOwnerId()) && matchesRef(c, front, live)) {
                offBattlefield.add(new ManaSource(c, Zone.EXILED));
            }
        }
        for (Card c : p.getLibrary().getCards(g)) {
            if (matchesRef(c, front, live)) {
                offBattlefield.add(new ManaSource(c, Zone.LIBRARY));
            }
        }
        ManaSource inZone = nthSource(offBattlefield, wanted);
        if (inZone != null && seen.add(inZone.card.getId())) {
            sources.add(inZone);
        }
        if (sources.isEmpty()) {
            throw new IllegalStateException("harness: activate step " + stepIdx + " names " + card
                    + (wanted > 1 ? "#" + wanted : "")
                    + ", which is not on " + p.getName() + "'s battlefield or in that player's"
                    + " hand, graveyard, exile or library");
        }
        return sources;
    }

    /**
     * A land play whose ref has no setup alias: the card entered the hand
     * mid-window (drawn, returned, searched), so registerAliases never saw it.
     * Playing it by name still enters XMage's first same-named card.
     */
    private void playUnaliasedLand(TestPlayer p, String playRef, String landName) {
        java.util.Set<UUID> before = noteBattlefield(p, playRef);
        playLand(turn, phase, p, landName);
        refAlias.put(playRef, "@" + playRef);
        bindEnteredLand(p, playRef, before);
    }

    /** Queue a snapshot of the battlefield's permanent ids; the returned set
     * fills when the command runs, just ahead of the play it precedes. */
    private java.util.Set<UUID> noteBattlefield(TestPlayer p, String playRef) {
        java.util.Set<UUID> before = new java.util.HashSet<>();
        runCode("note battlefield " + playRef, turn, phase, p, (info, pl, g) -> {
            before.clear();
            for (Permanent perm : g.getBattlefield().getAllPermanents()) {
                before.add(perm.getId());
            }
        });
        return before;
    }

    /**
     * Queue, right after a land play, the binding of its ref to the permanent
     * the play created. A ref whose alias names no battlefield permanent --
     * none at all (a card that entered the hand after setup) or a setup
     * object that is not the card the by-name play entered (a library card
     * the ref ranked before it was drawn) -- is moved onto the new permanent,
     * so a later tap, target or pick of the ref resolves through
     * scenarioObjectId instead of the by-name fallback, which would take a
     * setup land of the same name (CR 305.1, 605.3a). A ref bound to the
     * permanent this play created is left alone.
     *
     * A ref whose alias names a permanent that was ALREADY on the battlefield
     * before the play is stale: a land cannot be played from the battlefield,
     * so the ref's card is the one the play entered. XMage answers an effect's
     * by-name pick ("put a land card from your hand onto the battlefield":
     * Embrace the Paradox; "return target land": Slinza's bounce) with any
     * same-named card, so it can move the sibling of the card gorge moved.
     * The two refs then trade objects: the played ref takes the entered
     * permanent and the ref that named it takes the permanent the played ref
     * stood on, so every same-named ref still names one distinct object and
     * a later tap of the played ref reaches the untapped land.
     */
    private void bindEnteredLand(TestPlayer p, String playRef, java.util.Set<UUID> before) {
        String front = xmageSpelling(refName(playRef));
        String live = battlefieldName(playRef);
        runCode("bind entered " + playRef, turn, phase, p, (info, pl, g) -> {
            UUID current = scenarioObjectId(g, p, playRef);
            if (current != null && g.getPermanent(current) != null && !before.contains(current)) {
                return;
            }
            Permanent entered = null;
            Permanent anyEntered = null;
            int newCount = 0;
            for (Permanent perm : g.getBattlefield().getAllPermanents()) {
                if (before.contains(perm.getId()) || !perm.getControllerId().equals(p.getId())) {
                    continue;
                }
                newCount++;
                anyEntered = perm;
                if (entered == null && matchesRef(perm, front, live)) {
                    entered = perm;
                }
            }
            if (entered == null && newCount == 1) {
                entered = anyEntered;
            }
            if (entered == null) {
                return;
            }
            // The refs that named the entered permanent (the sibling a
            // mis-picked effect moved) take the object the played ref leaves.
            List<String> siblings = new ArrayList<>();
            if (current != null && !current.equals(entered.getId())) {
                for (java.util.Map.Entry<String, UUID> a : p.getAliases().entrySet()) {
                    if (entered.getId().equals(a.getValue()) && !a.getKey().equals(playRef)
                            && isScenarioRef(a.getKey())) {
                        siblings.add(a.getKey());
                    }
                }
            }
            for (int j = 0; j < seats; j++) {
                seat(j).getAliases().put(playRef, entered.getId());
                for (String sibling : siblings) {
                    seat(j).getAliases().put(sibling, current);
                }
            }
        });
    }

    /** The 1-based ordinals of the ref's same-named permanents that a step
     * AFTER stepIdx activates explicitly (same seat, same name). A name
     * fallback that must pick some untapped land of the name leaves those
     * for the later step that names them. */
    private java.util.Set<Integer> laterNamedOrdinals(String ref, int stepIdx) {
        java.util.Set<Integer> out = new java.util.HashSet<>();
        JsonArray all = steps(sc0);
        String seatPrefix = ref.substring(0, ref.indexOf(':') + 1);
        String name = refName(ref);
        for (int i = stepIdx + 1; i < all.size(); i++) {
            JsonObject later = all.get(i).getAsJsonObject();
            if (!later.has("op") || !"activate".equals(str(later, "op")) || !later.has("card")
                    || !later.get("card").isJsonPrimitive()) {
                continue;
            }
            String other = str(later, "card");
            if (other.startsWith(seatPrefix) && !other.contains(":token:") && refName(other).equals(name)
                    && other.lastIndexOf('#') >= 0) {
                out.add(refOrdinal(other));
            }
        }
        return out;
    }

    /** The only source in list, or its wanted-th (1-based); null when the
     * list is empty or shorter than wanted. */
    private static ManaSource nthSource(List<ManaSource> list, int wanted) {
        if (list.size() == 1) {
            return list.get(0);
        }
        return wanted >= 1 && wanted <= list.size() ? list.get(wanted - 1) : null;
    }

    /** Whether a live object is one of the same-named objects a scenario ref
     * names, by its setup spelling, its staged back face or its live name.
     *
     * A transformed double-faced permanent is also matched by either face of
     * its main card: a front-face ref ("p0:Sunbird Standard") names the craft
     * source that has since become "Sunbird Effigy", and the live permanent
     * keeps the front name only on the main card. */
    private boolean matchesRef(Card object, String front, String live) {
        String setup = setupName(object);
        String actual = xmageSpelling(object.getName());
        if (setup.equals(front) || setup.equals(live) || actual.equals(front) || actual.equals(live)) {
            return true;
        }
        if (object instanceof Permanent) {
            Card main = ((Permanent) object).getMainCard();
            if (main != null) {
                String frontFace = xmageSpelling(main.getName());
                if (frontFace.equals(front) || frontFace.equals(live)) {
                    return true;
                }
                Card other = main.getSecondCardFace();
                if (other != null) {
                    String otherFace = xmageSpelling(other.getName());
                    if (otherFace.equals(front) || otherFace.equals(live)) {
                        return true;
                    }
                }
            }
        }
        return false;
    }

    /** The object id a scenario ref is bound to on the activating player,
     * from setup's "@ref" aliases (registerAliases); null for a ref that no
     * setup object bound. */
    private static UUID scenarioObjectId(Game g, Player p, String ref) {
        Player actual = g.getPlayer(p.getId());
        return actual instanceof TestPlayer ? ((TestPlayer) actual).getAliases().get(ref) : null;
    }

    /** The named source's one mana ability for an activate step, or null
     * when this candidate has none. Its step's ability_index selects the AB$
     * the item means (gorge's face abilities are its activated abilities, in
     * printed order); the rule text is the selector both sides already agree
     * on, so a differing XMage ordering still activates the right ability.
     *
     * XMage often splits one gorge ability into several: "{T}, Pay 1 life: Add
     * {G}, {U}, or {R}" is three Green/Blue/RedManaAbility instances, so the
     * gorge ability_index does not name one of them. When the text matches
     * several abilities, the scripted colour choice that gorge recorded for
     * the same activation picks the split member (and is consumed, since XMage
     * poses no colour dialog for it). Duplicate granted abilities (Locus of
     * Enlightenment keeps both Elvish Mystic's and Llanowar Elves' "{T}: Add
     * {G}") have identical rule text, so any one is the same activation. More
     * than one DISTINCT text match without a usable index or colour answer is
     * a harness error, never a guess. */
    private static ActivatedManaAbilityImpl findManaAbility(Card source, Zone zone, TestPlayer p, Game g,
            String text, Integer abilityIndex, String card, int stepIdx) {
        List<ActivatedManaAbilityImpl> available = new ArrayList<>(
                source.getAbilities(g).getAvailableActivatedManaAbilities(zone, p.getId(), g));
        List<ActivatedManaAbilityImpl> matching = new ArrayList<>();
        for (ActivatedManaAbilityImpl ability : available) {
            if (text.isEmpty() || ability.toString().startsWith(text)) {
                matching.add(ability);
            }
        }
        // XMage models a split or combo ability ("{T}: Add {R} or {G}") as one
        // mana ability per colour, so gorge's ability_index names whichever
        // member XMage listed first. The colour gorge recorded for this
        // activation decides among distinct text matches before the index is
        // honoured; the choice is consumed, as XMage poses no colour dialog.
        if (matching.size() > 1 && !sameRuleText(matching)) {
            char colour = queuedManaColour(p);
            ActivatedManaAbilityImpl chosen = colour == 0 ? null : soleColourMatch(matching, colour);
            if (chosen != null) {
                p.getChoices().remove(0);
                return chosen;
            }
        }
        ActivatedManaAbilityImpl indexed = abilityIndex == null ? null
                : activatedAbilityAt(source, g, abilityIndex);
        boolean indexMatches = indexed != null && available.contains(indexed)
                && (text.isEmpty() || indexed.toString().startsWith(text));
        if (indexMatches) {
            return indexed;
        }
        if (matching.size() == 1) {
            return matching.get(0);
        }
        if (matching.size() > 1) {
            if (sameRuleText(matching)) {
                return matching.get(0);
            }
            throw new IllegalStateException("harness: activate step " + stepIdx + " on " + card + " matches "
                    + matching.size() + " mana abilities for \"" + text + "\" and ability_index "
                    + abilityIndex + " does not identify one");
        }
        if (indexed != null && available.contains(indexed)) {
            // The text is a prefix XMage spells differently from this card
            // instance (a granted/blended ability); the index still names
            // exactly one of the source's own mana abilities.
            return indexed;
        }
        return null;
    }

    /** Whether every matching ability has the same rule text: granted
     * duplicates of one ability, where activating any is the same choice. */
    private static boolean sameRuleText(List<ActivatedManaAbilityImpl> abilities) {
        String first = abilities.get(0).toString();
        for (ActivatedManaAbilityImpl ability : abilities) {
            if (!first.equals(ability.toString())) {
                return false;
            }
        }
        return true;
    }

    /** The mana colour the player's next scripted choice names, or 0 when the
     * head of the choice queue is not a colour (an unrelated ask is next). */
    private static char queuedManaColour(TestPlayer p) {
        if (p.getChoices().isEmpty()) {
            return 0;
        }
        String choice = p.getChoices().get(0);
        for (char c : "WUBRGC".toCharArray()) {
            if (ScriptedChoicePlayer.colourName(c).equalsIgnoreCase(choice)) {
                return c;
            }
        }
        return 0;
    }

    /** The one text-matching ability whose rule adds the named colour, or
     * null when none or several do. */
    private static ActivatedManaAbilityImpl soleColourMatch(List<ActivatedManaAbilityImpl> matching, char colour) {
        String mark = "{" + colour + "}";
        ActivatedManaAbilityImpl chosen = null;
        for (ActivatedManaAbilityImpl ability : matching) {
            if (ability.toString().contains(mark)) {
                if (chosen != null) {
                    return null;
                }
                chosen = ability;
            }
        }
        return chosen;
    }

    /** The source's ability_index-th activated ability (gorge indexes its
     * face's activated AB$ list), or null when the index is out of range or
     * names a non-mana ability. */
    private static ActivatedManaAbilityImpl activatedAbilityAt(Card source, Game g, int abilityIndex) {
        if (abilityIndex < 0) {
            return null;
        }
        int at = 0;
        for (Ability ability : source.getAbilities(g)) {
            if (!ability.isActivatedAbility()) {
                continue;
            }
            if (at++ == abilityIndex) {
                return ability instanceof ActivatedManaAbilityImpl
                        ? (ActivatedManaAbilityImpl) ability : null;
            }
        }
        return null;
    }

    /** A "{T}: Add ..." rule text with no other cost: the mana-ability shape
     * a gained ability is recognised by when the source prints none. */
    static boolean isTapManaText(String text) {
        return text != null && text.matches("\\{T\\}: Add (\\{[WUBRGC]\\})+\\.?");
    }

    static final int PASS_PAIR_FIRST = 1;
    static final int PASS_HANDOFF = 2;
    static final int PASS_PAIR_SECOND = 3;
    /** The active player's own pass on its turn (seat 1 active). XMage's
     * idle TestPlayer passes without being told, so it queues nothing. */
    static final int PASS_ACTIVE_OWN = 4;
    /** The first pass of a p1-first pair: on the active player's turn (seat 0)
     * seat 1 casts at instant speed, keeps priority (CR 117.3c) and passes,
     * then seat 0 passes to resolve the top object (CR 117.4). Queues nothing
     * like PASS_PAIR_FIRST; the pair's second pass is PASS_PAIR_SECOND. */
    static final int PASS_P1_PAIR_FIRST = 5;
    /** A pass of a recorded whole game (an xengine tape or window): the step
     * carries the position gorge reached after it ("xpos", from the item's
     * xmage_pos). Any run of passes follows CR 117.4 from that position alone:
     * the pass that finds a non-empty stack resolves its top object, the one
     * that finds it empty moves the game to the next step. */
    static final int PASS_RUN = 6;

    /** The XMage commands a pass step queues on the active player, the seat
     * that carries every checkpoint. */
    static final String CMD_REQUIRE_STACK = "pass pair needs a stack object";
    static final String CMD_WAIT_RESOLVE_ONE = "waitStackResolved:1";
    static final String CMD_YIELD_PRIORITY = "pass handoff to the opponent";
    /** Queued on the passing seat itself (not the active one) by a resolving
     * pass of a seat that is not the active player: that seat passes for real,
     * so nothing it has queued later in the turn and step runs at this
     * priority. */
    static final String CMD_PASS_OWN_PRIORITY = "resolving pass of the non-active seat";
    static final String CMD_REQUIRE_EMPTY_STACK = "opening pass pair needs an empty stack";
    static final String CMD_ADVANCE_STEP = "opening pass pair advances the step";

    private static boolean isOp(JsonArray steps, int index, String op) {
        return index >= 0 && index < steps.size() && str(steps.get(index).getAsJsonObject(), "op").equals(op);
    }

    /** The passing seat is the active player and not seat 0, and the pass is
     * not half of a pair: gorge's pass answers that seat's own priority. */
    private static boolean isActiveOwnPass(JsonArray steps, int index, int activeSeat) {
        return activeSeat != 0 && isOp(steps, index, "pass") && seatOfStep(steps, index) == activeSeat
                && !isOp(steps, index - 1, "pass") && !isOp(steps, index + 1, "pass");
    }

    /** Whether the pass pair starting at step {@code first} is a p1-first pair:
     * seat 1 casts at instant speed on the active player's turn, then passes
     * (CR 117.3c holds priority after casting), and seat 0 passes to resolve
     * (CR 117.4). A seat-1 cast immediately before the pair is what makes it
     * this shape; a bare [{pass s1},{pass s0}] stays unsupported. */
    private static boolean isP1PairFirst(JsonArray steps, int first) {
        return seatOfStep(steps, first) == 1
                && isOp(steps, first + 1, "pass") && seatOfStep(steps, first + 1) == 0
                && !isOp(steps, first - 1, "pass") && !isOp(steps, first + 2, "pass")
                && isOp(steps, first - 1, "cast") && seatOfStep(steps, first - 1) == 1;
    }

    /** Whether the pass at {@code index} is half of a p1-first pair on seat
     * 1's OWN turn: seat 1 casts, holds priority and passes (CR 117.3c), then
     * seat 0 passes and the top object resolves (CR 117.4). That is the
     * active player's pair, the mirror of seat 0's p0-then-p1 pair on its
     * turn, so its commands and its second checkpoint go on the active seat
     * (Zidane, Wan Shi Tong: an opponent's sorcery on its own turn). */
    static boolean isActiveP1Pair(JsonArray steps, int index, int activeSeat) {
        if (activeSeat != 1 || !isOp(steps, index, "pass")) {
            return false;
        }
        return isP1PairFirst(steps, isOp(steps, index - 1, "pass") ? index - 1 : index);
    }

    private static int seatOfStep(JsonArray steps, int index) {
        JsonObject st = steps.get(index).getAsJsonObject();
        return st.has("seat") ? st.get("seat").getAsInt() : 0;
    }

    /** Classify only the pass shapes emitted by Level-B scenarios; anything
     * else fails loudly. Gorge's pass answers one priority decision of the
     * named seat. Two opposing passes (p0 then p1) resolve the stack's top
     * object, so p0's pass leaves the stack as it was and only p1's pass
     * resolves. A lone p0 pass hands priority to the p1 cast that follows. */
    static int passAction(JsonArray steps, int index, int activeSeat) {
        if (isPassRun(steps, index)) {
            return PASS_RUN;
        }
        if (isActiveOwnPass(steps, index, activeSeat)) {
            return PASS_ACTIVE_OWN;
        }
        return passAction(steps, index);
    }

    static int passAction(JsonArray steps, int index) {
        if (index < 0 || index >= steps.size()) {
            throw new IllegalArgumentException("pass step index out of range: " + index);
        }
        if (!isOp(steps, index, "pass")) {
            throw new IllegalArgumentException("pass action requested for non-pass step " + index);
        }
        if (isPassRun(steps, index)) {
            return PASS_RUN;
        }
        int seat = seatOfStep(steps, index);
        if (isOp(steps, index - 1, "pass")) {
            if (isP1PairFirst(steps, index - 1)) {
                return PASS_PAIR_SECOND;
            }
            if (seatOfStep(steps, index - 1) == 0 && seat == 1
                    && !isOp(steps, index - 2, "pass") && !isOp(steps, index + 1, "pass")) {
                return PASS_PAIR_SECOND;
            }
            throw new IllegalArgumentException("unsupported pass sequence at step " + index);
        }
        if (isOp(steps, index + 1, "pass")) {
            if (isP1PairFirst(steps, index)) {
                return PASS_P1_PAIR_FIRST;
            }
            if (seat == 0 && seatOfStep(steps, index + 1) == 1 && !isOp(steps, index + 2, "pass")) {
                return PASS_PAIR_FIRST;
            }
            throw new IllegalArgumentException("unsupported pass pair at step " + index);
        }
        if (seat == 0 && isOp(steps, index + 1, "cast") && seatOfStep(steps, index + 1) == 1) {
            return PASS_HANDOFF;
        }
        throw new IllegalArgumentException("unsupported pass pattern at step " + index);
    }

    /** Whether the checkpoint of the pass at {@code index} is taken before
     * XMage has handed the priority on (CR 117.3d). A pass that resolves the
     * top object hands priority back to the active player and a pass that
     * enters a step hands it to that step's first player; their checkpoints
     * already run at that priority. Every other pass only hands priority to
     * the next seat, which is the holder the post-pass snapshot must name. */
    static boolean passHandsPriorityOn(JsonArray steps, int index, int activeSeat, int seats) {
        if (!isOp(steps, index, "pass")) {
            return false;
        }
        if (isPassRun(steps, index)) {
            JsonObject pos = steps.get(index).getAsJsonObject().getAsJsonObject("xpos");
            return !posFlag(pos, "resolves") && !passEntersNewStep(steps, index, seats);
        }
        // A pair's second pass resolves the top object (or, as the scenario's
        // opening pair, advances the step); every other legacy pass is a
        // handoff to the other seat.
        return passAction(steps, index, activeSeat) != PASS_PAIR_SECOND;
    }

    /** Whether step {@code index} is a pass of a recorded game: a pass that
     * carries the "xpos" position gorge reached after it. The hand-authored
     * and ported scenarios never carry one, so they keep the shapes above. */
    static boolean isPassRun(JsonArray steps, int index) {
        return isOp(steps, index, "pass") && steps.get(index).getAsJsonObject().has("xpos");
    }

    /** Whether the scenario is a recorded whole game (an xengine tape or
     * window): at least one of its passes carries a position. */
    static boolean isRecordedRun(JsonArray steps) {
        for (int i = 0; i < steps.size(); i++) {
            if (isPassRun(steps, i)) {
                return true;
            }
        }
        return false;
    }

    /** Whether step {@code index} is an action -- a cast, activation or land
     * play -- of a recorded game (one that has passes with positions). These
     * are the ops whose commands, and with them the checkpoint, run on the
     * acting seat's own queue. A declaration (attack, block) is XMage's step
     * beginStep work on the declaring seat, not a queued action of that seat,
     * so it is deliberately not an action here. */
    static boolean isRecordedAction(JsonArray steps, int index) {
        return (isOp(steps, index, "cast") || isOp(steps, index, "activate") || isOp(steps, index, "play"))
                && isRecordedRun(steps);
    }

    /** Whether step {@code index} is a recorded action by a seat that is not
     * the active player. */
    static boolean opensOnOtherSeat(JsonArray steps, int index, int activeSeat) {
        return isRecordedAction(steps, index) && seatOfStep(steps, index) != activeSeat;
    }

    private static boolean posFlag(JsonObject pos, String key) {
        return pos.has(key) && pos.get(key).getAsBoolean();
    }

    /** The number of seats the scenario's steps name, at least two. */
    static int seatCount(JsonArray steps) {
        int seats = 2;
        for (int i = 0; i < steps.size(); i++) {
            seats = Math.max(seats, seatOfStep(steps, i) + 1);
        }
        return seats;
    }

    /** Whether the pass at {@code index} completes a round of passes since
     * the last action, step entry or resolution (CR 117.4): every player
     * passed with no action in between. The round restarts at a step entry (a
     * different step name, or a full round ending a same-named step such as an
     * additional end step) and at a resolution, which hands priority back to
     * the active seat. The round's last pass is not assumed to be the
     * non-active seat's: an action can leave it with the active seat. */
    static boolean passesInSuccession(JsonArray steps, int index, int seats) {
        if (index < 0 || !isOp(steps, index, "pass")) {
            return false;
        }
        int round = 0;
        int prev = -1;
        for (int i = 0; i <= index; i++) {
            if (!isOp(steps, i, "pass")) {
                // An action breaks the round; the last position still names
                // the step the action was taken in.
                round = 0;
                continue;
            }
            if (!isPassRun(steps, i)) {
                // A pass with no position cannot be placed; legacy passes have
                // no recorded run at all. Count it and keep the last position.
                round++;
                continue;
            }
            JsonObject pos = steps.get(i).getAsJsonObject().getAsJsonObject("xpos");
            boolean entry = prev < 0
                    ? posFlag(pos, "moved")
                    : !sameStep(steps.get(prev).getAsJsonObject().getAsJsonObject("xpos"), pos);
            if (entry) {
                // The pass entered a new step instance: it closed the previous
                // step's round and is not part of the new step's round.
                round = 0;
                prev = i;
                if (i == index) {
                    return false;
                }
                continue;
            }
            round++;
            boolean closes = !posFlag(pos, "resolves") && round >= Math.max(seats, 2);
            if (i == index) {
                return closes;
            }
            if (closes || posFlag(pos, "resolves")) {
                // The next pass is either in a fresh step instance (a closing
                // pass) or after a resolution handed priority back.
                round = 0;
            }
            prev = i;
        }
        return false;
    }

    /** passesInSuccession for a caller with no game seat count to hand. */
    static boolean passesInSuccession(JsonArray steps, int index) {
        return passesInSuccession(steps, index, seatCount(steps));
    }

    /** Whether two positions name the same turn and step. */
    private static boolean sameStep(JsonObject a, JsonObject b) {
        return a.has("turn") && b.has("turn")
                && a.get("turn").getAsInt() == b.get("turn").getAsInt()
                && a.has("step") && b.has("step")
                && a.get("step").getAsString().equals(b.get("step").getAsString());
    }

    /** The last positioned pass before {@code index}, or -1. */
    private static int previousPassPosition(JsonArray steps, int index) {
        for (int i = index - 1; i >= 0; i--) {
            if (isPassRun(steps, i)) {
                return i;
            }
        }
        return -1;
    }

    /** Whether the recorded pass at {@code index} enters a new step instance:
     * a different turn or step name, or a pass that completes a full round of
     * passes for a same-named step (an additional end step, CR 500.9). The
     * round is re-derived from the step sequence rather than from the recorded
     * {@code moved} bit: a pass that follows an action is not in the round
     * that ends a step (CR 117.4), even when a stale item records a move
     * there, and the closing pass of a round can be the active seat's, even
     * when a stale item does not record the move. */
    static boolean passEntersNewStep(JsonArray steps, int index, int seats) {
        if (!isPassRun(steps, index)) {
            return false;
        }
        JsonObject pos = steps.get(index).getAsJsonObject().getAsJsonObject("xpos");
        int prev = previousPassPosition(steps, index);
        if (prev < 0) {
            // The first position of the run: only an explicit move can enter a
            // second occurrence of the setup step (typically main 1).
            return posFlag(pos, "moved");
        }
        JsonObject before = steps.get(prev).getAsJsonObject().getAsJsonObject("xpos");
        if (!sameStep(before, pos)) {
            return true;
        }
        return passesInSuccession(steps, index, seats);
    }

    /** passEntersNewStep for a caller with no game seat count to hand. */
    static boolean passEntersNewStep(JsonArray steps, int index) {
        return passEntersNewStep(steps, index, seatCount(steps));
    }

    /** Copy the item's xmage_pos entries (parallel to the steps, null for a
     * step that is no pass) onto their steps as "xpos". They ride beside the
     * steps because gorge's runner rejects unknown per-step fields. */
    static void mergePassPositions(JsonObject sc) {
        if (!sc.has("xmage_pos") || !sc.get("xmage_pos").isJsonArray()) {
            return;
        }
        JsonArray pos = sc.getAsJsonArray("xmage_pos");
        JsonArray steps = steps(sc);
        for (int i = 0; i < pos.size() && i < steps.size(); i++) {
            if (pos.get(i).isJsonObject() && isOp(steps, i, "pass")) {
                steps.get(i).getAsJsonObject().add("xpos", pos.get(i));
            }
        }
    }

    /** The step whose declaration the pass at {@code index} stops before: the
     * pass of a recorded game that moved gorge into declare-attackers (or
     * declare-blockers) and is followed by the step that declares something
     * there. gorge leaves that declaration pending, so its checkpoint shows the
     * board as it was before it. XMage declares in the step's beginStep, ahead
     * of the step's first priority, so a checkpoint taken in that step already
     * shows the creatures attacking (blocking). Null for any other pass. */
    static PhaseStep declarationAhead(JsonArray steps, int index) {
        return declarationAhead(steps, index, seatCount(steps));
    }

    static PhaseStep declarationAhead(JsonArray steps, int index, int seats) {
        if (!passEntersNewStep(steps, index, seats)) {
            return null;
        }
        JsonObject pos = steps.get(index).getAsJsonObject().getAsJsonObject("xpos");
        PhaseStep at = declarationStep(pos);
        if (at == PhaseStep.DECLARE_ATTACKERS && declares(steps, index + 1, "attack", "attackers")) {
            return at;
        }
        if (at == PhaseStep.DECLARE_BLOCKERS && declares(steps, index + 1, "block", "blocks")) {
            return at;
        }
        return null;
    }

    /** The declaration step a recorded pass checkpoint stands at: the pass
     * entered declare-attackers (or declare-blockers), so the step's
     * declaration is the pending turn-based action and nobody holds priority
     * (CR 508.1, 509.1, 117.3a). This holds whether or not the scenario then
     * records the declaration: declarationAhead moves that checkpoint before
     * the step, while a scenario that never declares (the xproof mm-menon /
     * icetill shape) checkpoints in the step itself. Null for any other pass. */
    static PhaseStep declarationCheckpoint(JsonArray steps, int index) {
        return declarationCheckpoint(steps, index, seatCount(steps));
    }

    static PhaseStep declarationCheckpoint(JsonArray steps, int index, int seats) {
        if (!passEntersNewStep(steps, index, seats)) {
            return null;
        }
        return declarationStep(steps.get(index).getAsJsonObject().getAsJsonObject("xpos"));
    }

    /** Whether the declaration phase a pass checkpoint stands at had at least
     * one legal option, so gorge stopped there with a pending non-priority
     * declaration (CR 508.1, 509.1, 117.3a). The checkpoint is taken at the
     * step's first priority, after XMage has already declared or skipped: an
     * attacker or blocker in combat is evidence of a legal option, as is one
     * still available. No option means gorge recorded the forced empty
     * declaration inside the step transition and the checkpoint is the live
     * priority that follows, whose offered set is not empty by rule. */
    static boolean declarationHadOptions(PhaseStep declaration, Game g) {
        switch (declaration) {
            case DECLARE_ATTACKERS: {
                if (!g.getCombat().getAttackers().isEmpty()) {
                    return true;
                }
                Player active = g.getPlayer(g.getActivePlayerId());
                return active != null && !active.getAvailableAttackers(g).isEmpty();
            }
            case DECLARE_BLOCKERS: {
                if (!g.getCombat().getBlockers().isEmpty()) {
                    return true;
                }
                if (g.getCombat().getAttackers().isEmpty()) {
                    return false;
                }
                for (CombatGroup group : g.getCombat().getGroups()) {
                    Player defender = g.getPlayer(group.getDefendingPlayerId());
                    if (defender == null) {
                        continue;
                    }
                    for (Permanent blocker : defender.getAvailableBlockers(g)) {
                        if (group.canBlock(blocker, g)) {
                            return true;
                        }
                    }
                }
                return false;
            }
            default:
                return false;
        }
    }

    /** The declaration step a pass position names, or null for another step. */
    private static PhaseStep declarationStep(JsonObject pos) {
        if (!pos.has("step")) {
            return null;
        }
        switch (pos.get("step").getAsString()) {
            case "declare-attackers": return PhaseStep.DECLARE_ATTACKERS;
            case "declare-blockers": return PhaseStep.DECLARE_BLOCKERS;
            default: return null;
        }
    }

    /** Whether step {@code index} is the op with a non-empty declaration list. */
    private static boolean declares(JsonArray steps, int index, String op, String key) {
        return isOp(steps, index, op) && steps.get(index).getAsJsonObject().has(key)
                && steps.get(index).getAsJsonObject().getAsJsonArray(key).size() > 0;
    }

    /** The last step with a priority before a declaration step: where XMage
     * can still show the board as gorge's checkpoint before the declaration
     * does. A declaration step has no other predecessor in the turn. */
    static PhaseStep beforeDeclaration(PhaseStep declaration) {
        switch (declaration) {
            case DECLARE_ATTACKERS: return PhaseStep.BEGIN_COMBAT;
            case DECLARE_BLOCKERS: return PhaseStep.DECLARE_ATTACKERS;
            default: throw new IllegalArgumentException("no declaration in " + declaration);
        }
    }

    /** Apply the step end a declaration-ahead checkpoint stands after: the
     * checkpoint is taken at the previous step's last priority, and that step
     * ends immediately afterwards, emptying every mana pool (CR 500.5/106.4). Runs
     * XMage's own emptyManaPools, so an effect that keeps mana (an EndOfTurn
     * mana item, a do-not-empty type) still keeps it. */
    static void applyDeclarationStepEnd(PhaseStep reportAs, Game g) {
        if (reportAs == PhaseStep.DECLARE_ATTACKERS || reportAs == PhaseStep.DECLARE_BLOCKERS) {
            g.emptyManaPools(null);
        }
    }

    /** Whether CMD_REQUIRE_STACK is a hard requirement at step {@code index}.
     * It is for the legacy pairs, where the stack must hold the spell the pair
     * resolves. A pass of a recorded game only follows gorge's position: gorge
     * resolved an object XMage never put on its stack (an intervening-if or
     * delayed trigger XMage does not make), and the stack is then empty. That
     * is no harness failure: waitStackResolved passes at once on an empty
     * stack, and the checkpoint shows the stack difference as a diverge. */
    static boolean requireStackIsStrict(JsonArray steps, int index) {
        return !isPassRun(steps, index);
    }

    /** The commands of a pass of a recorded game, plus those of the step
     * after one. The pass that resolves an object (gorge's stack lost one)
     * queues the one-object resolution on the active seat, the seat that has
     * priority at the step the pass was made in; a pass that only hands
     * priority on or moves the step queues nothing. A recorded pass by the
     * OTHER seat that resolves the top object is queued on that seat as its
     * own real pass (the active seat's passes stay virtual checkpoints), so an
     * action that seat has queued later in the same turn and step -- a flash
     * spell held for its own step -- cannot fire at this priority instead
     * (ECL Impolite Entrance: p1's Glamermite fired at the p0/p1 pass pair, so
     * the top object never resolved). The step after a pass the active seat
     * made in the same step, if it is an action of the other seat, starts by
     * yielding priority to that seat (the lone-handoff recipe). */
    static List<String> passRunCommands(JsonArray steps, int index, int activeSeat) {
        return passRunCommands(steps, index, activeSeat, seatCount(steps));
    }

    static List<String> passRunCommands(JsonArray steps, int index, int activeSeat, int seats) {
        List<String> cmds = new ArrayList<>();
        if (isPassRun(steps, index - 1) && !isOp(steps, index, "pass")) {
            JsonObject prev = steps.get(index - 1).getAsJsonObject();
            JsonObject pos = prev.getAsJsonObject("xpos");
            String op = str(steps.get(index).getAsJsonObject(), "op");
            boolean acts = op.equals("cast") || op.equals("activate") || op.equals("play");
            if (acts && !passEntersNewStep(steps, index - 1, seats) && !posFlag(pos, "resolves")
                    && seatOfStep(steps, index - 1) == activeSeat && seatOfStep(steps, index) != activeSeat) {
                cmds.add(CMD_YIELD_PRIORITY);
            }
        }
        if (isPassRun(steps, index)) {
            JsonObject pos = steps.get(index).getAsJsonObject().getAsJsonObject("xpos");
            // A pass that resolves an object needs the recorded seat to
            // really pass: the active seat's wait is already a real pass,
            // but the other seat would otherwise run its next queued action
            // at this priority (a flash cast held for its own step). A pass
            // that only hands priority on is virtual; its checkpoint
            // consumes the seat's priority instead.
            if (seatOfStep(steps, index) != activeSeat && posFlag(pos, "resolves")) {
                cmds.add(CMD_PASS_OWN_PRIORITY);
            }
            if (posFlag(pos, "resolves")) {
                cmds.add(CMD_REQUIRE_STACK);
                cmds.add(CMD_WAIT_RESOLVE_ONE);
            }
        }
        return cmds;
    }

    /** Whether the recorded pass at {@code index} must be a real pass of its
     * seat, queued right after its checkpoint. A pass of the seat that is not
     * active, which only hands priority on, is virtual: its checkpoint rides
     * that seat's own queue and consumes nothing. XMage then drains the rest
     * of the queue at that one priority, so a later action of the same seat in
     * the same step instance fires before the other seat has had its say
     * (Helix Pinnacle, Saproling Burst: p0 activates twice, passes, p1
     * resolves and passes, p0 activates a third time with the first ability
     * still on the stack, and the checkpoints of p1's two passes ran after
     * p0's third activation; No More Lies: p1 casts Reach Through Mists and
     * passes, p0 answers with a counterspell, and p1's later Consider fired
     * before p0's response). The pass is real exactly when the step that
     * follows belongs to another seat -- its pass resolves the top object, or
     * its cast, activation or land play answers the pass (CR 117.3c/117.3d:
     * after the seat passes, the other seat holds priority and may act) --
     * and the seat has an action still queued in this step instance. */
    static boolean passHandsOffBeforeLaterAction(JsonArray steps, int index, int activeSeat, int seats) {
        if (!isPassRun(steps, index)) {
            return false;
        }
        int seat = seatOfStep(steps, index);
        JsonObject pos = steps.get(index).getAsJsonObject().getAsJsonObject("xpos");
        if (seat == activeSeat || posFlag(pos, "moved") || posFlag(pos, "resolves")
                || passEntersNewStep(steps, index, seats)) {
            return false;
        }
        if (index + 1 >= steps.size() || seatOfStep(steps, index + 1) == seat) {
            return false;
        }
        for (int j = index + 1; j < steps.size(); j++) {
            if (isPassRun(steps, j) && passEntersNewStep(steps, j, seats)) {
                return false;
            }
            if (isRecordedAction(steps, j) && seatOfStep(steps, j) == seat) {
                return true;
            }
        }
        return false;
    }

    /** gorge's step name as the XMage step its priority falls in. */
    static PhaseStep phaseOfGorgeStep(String step) {
        switch (step) {
            case "upkeep": return PhaseStep.UPKEEP;
            case "draw": return PhaseStep.DRAW;
            case "main1": return PhaseStep.PRECOMBAT_MAIN;
            case "begin-combat": return PhaseStep.BEGIN_COMBAT;
            case "declare-attackers": return PhaseStep.DECLARE_ATTACKERS;
            case "declare-blockers": return PhaseStep.DECLARE_BLOCKERS;
            case "first-strike-damage": return PhaseStep.FIRST_COMBAT_DAMAGE;
            case "combat-damage": return PhaseStep.COMBAT_DAMAGE;
            case "end-combat": return PhaseStep.END_COMBAT;
            case "main2": return PhaseStep.POSTCOMBAT_MAIN;
            case "end": return PhaseStep.END_TURN;
            // A trigger or state-based action in the cleanup step gives the
            // players priority there (CR 514.3a), so gorge records passes at it.
            case "cleanup": return PhaseStep.CLEANUP;
            // A pass that ends a turn sits at the next turn's untap step
            // while that turn's optional-untap choice (CR 502.3) is pending:
            // the untap step grants no priority (CR 500.4), so the position
            // has no priority of its own, only the ask the controller takes
            // the checkpoint at (see flushUntapAskRunCodes).
            case "untap": return PhaseStep.UNTAP;
            default: throw new IllegalArgumentException("pass position in unknown step " + step);
        }
    }

    /** Move the replay's turn, step and active seat to where gorge stood after
     * a pass of a recorded game: the steps that follow are queued there. The
     * occurrence advances only when the pass really entered a new step
     * instance (passEntersNewStep): counting a handoff pass as one hides its
     * checkpoint at an occurrence XMage never starts. */
    private void followPassPosition(JsonArray steps, int index) {
        JsonObject pos = steps.get(index).getAsJsonObject().getAsJsonObject("xpos");
        turn = pos.get("turn").getAsInt();
        phase = phaseOfGorgeStep(pos.get("step").getAsString());
        activeSeat = pos.get("active").getAsInt();
        enterQueuedStep(turn, phase, passEntersNewStep(steps, index, seats));
        attackAdvancedTurn = false;
        followedPassPosition = true;
    }

    /** Start a replay's occurrence bookkeeping: the queued side from the
     * scenario's positions, the runtime side from the Step instances XMage
     * starts. They are counted with the same rule, so occurrence N of a step
     * in xmage_pos is occurrence N in the game. */
    private void resetOccurrences() {
        queuedOccurrences.clear();
        queuedOccTurn = 0;
        queuedOccStep = null;
        actionOccurrences.clear();
        runtimeOccTurn = 0;
        runtimeStepCounts.clear();
        runtimeStepOccurrence.clear();
    }

    /** The replay starts in the scenario turn's main 1, but the game runs
     * that turn's natural upkeep and draw before it: the runtime side counts
     * them. Count them on the queued side too, so an additional upkeep or
     * draw step after the start (CR 500.8) is occurrence 2 in both. The draw
     * is counted only when it runs: a two-player game's starting player skips
     * the draw step of turn 1 (TwoPlayerDuel's one-shot TurnMod), so there
     * the runtime never sees a natural draw and an additional one is
     * occurrence 1. A three-seat FreeForAll has no such skip. */
    private void seedNaturalStepsBeforeStart(int turnNum) {
        EnumMap<PhaseStep, Integer> counts = queuedOccurrences.computeIfAbsent(
                turnNum, key -> new EnumMap<>(PhaseStep.class));
        counts.put(PhaseStep.UPKEEP, 1);
        if (!(seats == 2 && turnNum == 1)) {
            counts.put(PhaseStep.DRAW, 1);
        }
    }

    /** Record a position the queued replay enters. A pass that moved within
     * the same step name begins an additional step (CR 500.9), so Moved alone
     * makes it a new occurrence, not just a different turn or step. */
    private void enterQueuedStep(int turnNum, PhaseStep step, boolean moved) {
        EnumMap<PhaseStep, Integer> counts = queuedOccurrences.computeIfAbsent(
                turnNum, key -> new EnumMap<>(PhaseStep.class));
        if (turnNum != queuedOccTurn || step != queuedOccStep || moved) {
            counts.merge(step, 1, Integer::sum);
        }
        queuedOccTurn = turnNum;
        queuedOccStep = step;
    }

    /** The queued occurrence of a step; 1 for a step no position has entered
     * (hand-authored scenarios, and a step before its first pass). */
    private int queuedOccurrence(int turnNum, PhaseStep step) {
        EnumMap<PhaseStep, Integer> counts = queuedOccurrences.get(turnNum);
        Integer occurrence = counts == null ? null : counts.get(step);
        return occurrence == null ? 1 : occurrence;
    }

    /** The occurrence of the current step in the running game. XMage builds a
     * new Step for every occurrence (the regular phase's steps and an
     * additional phase's), so a distinct Step instance of the same (turn,
     * PhaseStep) is the next occurrence, including an additional same-named
     * step such as CR 500.9's extra end step. */
    private int runtimeOccurrence(Game game) {
        Step step = game.getStep();
        if (step == null) {
            return 1;
        }
        int turnNum = game.getTurnNum();
        if (turnNum != runtimeOccTurn) {
            runtimeOccTurn = turnNum;
            runtimeStepCounts.clear();
            runtimeStepOccurrence.clear();
        }
        Integer occurrence = runtimeStepOccurrence.get(step);
        if (occurrence != null) {
            return occurrence;
        }
        occurrence = runtimeStepCounts.merge(game.getTurnStepType(), 1, Integer::sum);
        runtimeStepOccurrence.put(step, occurrence);
        return occurrence;
    }

    /** Tag the actions a step queued (from the two indices) with the
     * occurrence of the step each one names. Called once before and once
     * after the step's own queueing, because queuePassCommands registers at
     * the pre-pass position and the checkpoint at the post-pass one. */
    private void tagQueuedActions(int fromA, int fromB) {
        tagQueuedActions(playerA, fromA);
        tagQueuedActions(playerB, fromB);
    }

    private void tagQueuedActions(TestPlayer player, int from) {
        List<PlayerAction> actions = player.getActions();
        for (int i = from; i < actions.size(); i++) {
            PlayerAction action = actions.get(i);
            if (actionOccurrences.containsKey(action)) {
                continue;
            }
            actionOccurrences.put(action, queuedOccurrence(action.getTurnNum(), action.getStep()));
        }
    }

    /** Whether a message is the CR 502.3 optional-untap ask ("Untap
     * Whip Vine?"): SkipUntapOptionalSourceEffect poses it from the untap
     * step's beginStep with no ability as source, and no other engine ask
     * shares the shape (MayTapOrUntapTargetEffect's "Untap that permanent?"
     * carries its ability's source). */
    static boolean isUntapElectionAsk(String message) {
        return message != null && message.startsWith("Untap ") && message.endsWith("?");
    }

    /** Run the runCode actions queued at the running game's current untap
     * occurrence, earliest queued first across both seats, and report
     * whether the cut checkpoint (the scenario's last step, at the same
     * position) was among them. The untap step grants no priority (CR
     * 500.4), so TestPlayer never runs them: the CR 502.3 optional-untap
     * ask is the only event there, and the pass cut at it holds the row's
     * last checkpoint. Only runCode actions run -- the flusher must never
     * act or pass. */
    boolean flushUntapAskRunCodes(Game game) {
        int turnNum = game.getTurnNum();
        boolean positioned = false;
        for (TestPlayer p : new TestPlayer[]{playerA, playerB}) {
            for (PlayerAction action : p.getActions()) {
                if (action.getTurnNum() == turnNum && action.getStep() == PhaseStep.UNTAP
                        && action.getAction().startsWith(RUN_PREFIX)
                        && action.getAction().endsWith(RUN_COMMAND_CODE)) {
                    positioned = true;
                    break;
                }
            }
            if (positioned) {
                break;
            }
        }
        if (!positioned) {
            return false;
        }
        // The occurrence of the Step instance the ask runs in: the ask is
        // inside its playStep, so the lazy count sees and registers it.
        int occurrence = runtimeOccurrence(game);
        int flushed = 0;
        while (true) {
            ScriptedChoicePlayer best = null;
            int bestIndex = -1;
            PlayerAction bestAction = null;
            for (TestPlayer tp : new TestPlayer[]{playerA, playerB}) {
                ScriptedChoicePlayer p = (ScriptedChoicePlayer) tp;
                List<PlayerAction> actions = p.getActions();
                for (int i = 0; i < actions.size(); i++) {
                    PlayerAction action = actions.get(i);
                    Integer at = actionOccurrences.get(action);
                    if (action.getTurnNum() == turnNum && action.getStep() == PhaseStep.UNTAP
                            && action.getAction().startsWith(RUN_PREFIX)
                            && action.getAction().endsWith(RUN_COMMAND_CODE)
                            && at != null && at == occurrence
                            && (best == null || i < bestIndex)) {
                        best = p;
                        bestIndex = i;
                        bestAction = action;
                    }
                }
            }
            if (best == null) {
                break;
            }
            best.getActions().remove(bestAction);
            bestAction.getCodePayload().run(bestAction.getActionName(), best.getComputerPlayer(), game);
            flushed++;
        }
        return flushed > 0 && cutCheckpointPhase == PhaseStep.UNTAP && cutCheckpointTurn == turnNum;
    }

    /** The commands step {@code index} queues before its own actions and
     * checkpoint, in queue order. XMage runs every queued action of the
     * active player at one priority in order and only then lets the opponent
     * act, so each pass effect has to sit between the right two checkpoints:
     * the first pass of a pair queues nothing (its checkpoint still sees the
     * spell, as gorge's does); the second queues the one-object resolution
     * ahead of its checkpoint; the step after a lone p0 pass starts by
     * yielding priority, so the opponent's cast is on the stack before that
     * step's checkpoint. Pairs and handoffs need the active player to be
     * seat 0; the active seat 1's own lone pass queues nothing. */
    static List<String> passCommands(JsonArray steps, int index, int activeSeat) {
        return passCommands(steps, index, activeSeat, seatCount(steps));
    }

    static List<String> passCommands(JsonArray steps, int index, int activeSeat, int seats) {
        List<String> cmds = new ArrayList<>();
        if (isPassRun(steps, index) || isPassRun(steps, index - 1)) {
            return passRunCommands(steps, index, activeSeat, seats);
        }
        if (isActiveOwnPass(steps, index, activeSeat)) {
            return cmds;
        }
        boolean passHere = isOp(steps, index, "pass");
        boolean afterHandoff = isOp(steps, index - 1, "pass")
                && passAction(steps, index - 1, activeSeat) == PASS_HANDOFF;
        if ((passHere || afterHandoff) && activeSeat != 0 && !isActiveP1Pair(steps, index, activeSeat)) {
            throw new IllegalArgumentException("pass at step " + index + " needs seat 0 to be the active player");
        }
        if (afterHandoff) {
            cmds.add(CMD_YIELD_PRIORITY);
        }
        if (passHere && passAction(steps, index) == PASS_PAIR_SECOND) {
            if (pairOpensScenario(steps, index)) {
                cmds.add(CMD_REQUIRE_EMPTY_STACK);
                cmds.add(CMD_ADVANCE_STEP);
            } else {
                cmds.add(CMD_REQUIRE_STACK);
                if (!pairEndsMidResolution(steps, index)) {
                    cmds.add(CMD_WAIT_RESOLVE_ONE);
                }
            }
        }
        return cmds;
    }

    /** Whether the pass pair whose second pass is step {@code second} stops
     * inside the top object's resolution for gorge: the next step is a
     * pass_to "priority" that answers a mid-resolution "choose" (Wan Shi
     * Tong: the opponent's Demonic Tutor resolves to its search). gorge's
     * pair checkpoint still sees the spell on the stack, so XMage, which
     * cannot stop inside a resolution, resolves it at that pass_to instead
     * (passToResolvesTop). */
    static boolean pairEndsMidResolution(JsonArray steps, int second) {
        int next = second + 1;
        if (!isOp(steps, second, "pass") || !isOp(steps, next, "pass_to")) {
            return false;
        }
        JsonObject st = steps.get(next).getAsJsonObject();
        if (!str(st, "decision").equals("priority") || !st.has("answers") || !st.get("answers").isJsonArray()) {
            return false;
        }
        for (JsonElement e : st.getAsJsonArray("answers")) {
            if (e.isJsonObject() && str(e.getAsJsonObject(), "kind").equals("choose")) {
                return true;
            }
        }
        return false;
    }

    /** Whether the pass pair whose second pass is step {@code second} is the
     * scenario's first two steps AND its only steps. Setup then puts nothing
     * on the stack: the CantBeCast/CantBeActivated opponent-priority probes
     * (static_cant_be.go) are exactly that pair, so both seats pass in
     * succession on an empty stack and the game moves to the next step (CR
     * 500.2), as gorge's does. A pair with any step after it is a prelude to
     * those steps -- a beginning-of-step trigger is still on the stack there
     * (Shadow of the Goblin's setupResolvePrelude emits the pair before a
     * cast) -- so it resolves the top object like any other pair, even though
     * it is also the scenario's first two steps. */
    static boolean pairOpensScenario(JsonArray steps, int second) {
        return second == 1 && steps.size() == 2;
    }

    /** The step an opening pass pair moves the game to: the step after the
     * one it was passed in. Only main1, where every scenario starts, is
     * mapped; anything else fails loudly rather than being guessed at. */
    static PhaseStep stepAfterEmptyPass(PhaseStep from) {
        if (from == PhaseStep.PRECOMBAT_MAIN) {
            return PhaseStep.BEGIN_COMBAT;
        }
        throw new IllegalArgumentException("opening pass pair in " + from + " is unsupported");
    }

    /** A pass_to for decision "priority" that directly follows a pass_to for
     * decision "choose": gorge posed a mid-resolution ask at the first and
     * answers it at the second, so XMage must resolve the stack's top object
     * before this step's checkpoint (planCounterCauses' proliferate recipe).
     * Likewise the pass_to after a pass pair that stopped mid-resolution
     * (pairEndsMidResolution). */
    static boolean passToResolvesTop(JsonArray steps, int index) {
        if (index < 1 || index >= steps.size()) {
            return false;
        }
        if (pairEndsMidResolution(steps, index - 1) && isOp(steps, index - 2, "pass")) {
            return true;
        }
        JsonObject cur = steps.get(index).getAsJsonObject();
        JsonObject prev = steps.get(index - 1).getAsJsonObject();
        return str(cur, "op").equals("pass_to") && str(cur, "decision").equals("priority")
                && str(prev, "op").equals("pass_to") && str(prev, "decision").equals("choose");
    }

    /** The stack's object names, top first. */
    private static List<String> stackNames(Game g) {
        List<String> names = new ArrayList<>();
        for (StackObject so : g.getStack()) {
            names.add(so.getName());
        }
        return names;
    }

    /** The CMD_REQUIRE_STACK failure text. A pass pair that should resolve the
     * stack's top object found the stack empty; name the turn, step and
     * priority holder, and the objects that were on the stack at the last
     * checkpoint and have since left it (the recent departures), so the
     * mechanism (the object resolved early, or the pair ran at the wrong step)
     * is visible without a re-run. */
    static String emptyStackDiagnostic(String cmd, int turn, String step, String priority,
            List<String> lastStack, List<String> nowStack) {
        List<String> departed = new ArrayList<>(lastStack);
        departed.removeAll(nowStack);
        return cmd + ": the stack is empty (turn " + turn + ", step " + step
                + ", priority " + priority + "; since the last checkpoint the stack lost " + departed + ")";
    }

    /** A forced resolution (waitStackResolved) happens inside a TestPlayer
     * action, before the game loop's next checkStateAndTriggered. A checkpoint
     * queued in the same priority would otherwise snapshot the pre-SBA board:
     * an Aura whose bearer an exile-instead replacement removed (Bleed Dry,
     * Gnashing of Teeth) is still on the battlefield, and its CR 704.5m move
     * waits for the next priority. Arm the next checkpoint instead of queueing
     * a separate action: the checkpoint runs the game's own check just before
     * its snapshot, on the checkpoint's seat, so a forced resolution cannot
     * leave its state-based actions behind and the pinned queue order is
     * unchanged. Overriding the single wait entry point covers every forced
     * resolution site, including a future one (CR 117.5, 704.3). */
    @Override
    public void waitStackResolved(int turnNum, PhaseStep step, TestPlayer player, boolean skipOneStackObjectOnly) {
        super.waitStackResolved(turnNum, step, player, skipOneStackObjectOnly);
        settleCheckpoint = true;
    }

    private void queuePassCommands(int stepIdx) {
        // Most pass commands run on the active seat: seat 0 for every shape
        // but the active seat 1's own p1-first pair (isActiveP1Pair).
        // CMD_PASS_OWN_PRIORITY is the exception: it runs on the passing
        // non-active seat, not the active seat that carries the checkpoints.
        TestPlayer active = seat(activeSeat);
        for (String cmd : passCommands(steps(sc0), stepIdx, activeSeat, seats)) {
            switch (cmd) {
                case CMD_REQUIRE_STACK:
                    if (!requireStackIsStrict(steps(sc0), stepIdx)) {
                        break;
                    }
                    runCode(cmd, turn, phase, active, (info, pl, g) -> {
                        if (g.getStack().isEmpty()) {
                            Player prio = g.getPriorityPlayerId() == null ? null
                                    : g.getPlayer(g.getPriorityPlayerId());
                            throw new IllegalStateException(emptyStackDiagnostic(cmd, g.getTurnNum(),
                                    g.getTurnStepType() == null ? "" : g.getTurnStepType().name(),
                                    prio == null ? "none" : prio.getName(), lastCheckpointStack,
                                    stackNames(g)));
                        }
                    });
                    break;
                case CMD_WAIT_RESOLVE_ONE:
                    waitStackResolved(turn, phase, active, true);
                    // The pair resolves the stack's top object, so a cast
                    // step's spell has left the stack: a later cast that
                    // names it targets its permanent, not a spell that will
                    // never be on the stack (see dropLastCast).
                    dropLastCast();
                    break;
                case CMD_YIELD_PRIORITY:
                    runCode(cmd, turn, phase, active, (info, pl, g) -> pl.pass(g));
                    break;
                case CMD_PASS_OWN_PRIORITY:
                    // The seat that actually passed in the recorded game, not
                    // the active seat that carries the checkpoints. Queueing a
                    // real pass here puts it ahead of that seat's later actions
                    // in the same turn and step, which must not fire at this
                    // priority (a flash cast held for its own step).
                    runCode(cmd, turn, phase, seat(seatOfStep(steps(sc0), stepIdx)),
                            (info, pl, g) -> pl.pass(g));
                    break;
                case CMD_REQUIRE_EMPTY_STACK:
                    runCode(cmd, turn, phase, playerA, (info, pl, g) -> {
                        if (!g.getStack().isEmpty()) {
                            throw new IllegalStateException(cmd + ": the stack holds " + g.getStack().size());
                        }
                    });
                    break;
                case CMD_ADVANCE_STEP:
                    // Both seats pass on the empty stack, so the pair's
                    // checkpoint is the next step's first priority.
                    phase = stepAfterEmptyPass(phase);
                    break;
                default:
                    throw new IllegalArgumentException("unknown pass command " + cmd);
            }
        }
    }

    /** The Item's driver-only payment entry for a step: the pool mana gorge
     * spent on the step's cast/activate, or "" when it recorded none. The
     * array is parallel to the steps and lives on the Item because gorge's
     * runner rejects an unknown per-step field. */
    static String paymentAt(JsonObject sc, int i) {
        if (!sc.has("payment") || !sc.get("payment").isJsonArray()) {
            return "";
        }
        JsonArray pays = sc.getAsJsonArray("payment");
        return i >= 0 && i < pays.size() && pays.get(i).isJsonPrimitive()
                ? pays.get(i).getAsString() : "";
    }

    /** The ScriptedChoicePlayer a runCode action was registered on. runCode
     * dispatches its payload with the player's INNER computer player
     * (TestPlayer.priority passes computerPlayer), not the TestPlayer wrapper
     * createPlayer returned, so the payload argument must never be cast here:
     * manual-payment state lives on the wrapper. Resolve it from the captured
     * TestPlayer instead. */
    private static ScriptedChoicePlayer scriptedPlayer(TestPlayer p) {
        if (!(p instanceof ScriptedChoicePlayer)) {
            throw new IllegalStateException("payment hook on a non-scripted player: " + p);
        }
        return (ScriptedChoicePlayer) p;
    }

    /** Queue the step's manual-pool-payment start before its cast/activate
     * action. The payment is gorge's actual spend, so XMage must not choose a
     * different legal split. The matching endPayment is queued after the
     * step's checkpoint, so it also restores auto payment on an early return. */
    private void queuePaymentStart(String payment, TestPlayer p) {
        if (payment.isEmpty()) {
            return;
        }
        ScriptedChoicePlayer player = scriptedPlayer(p);
        runCode("payment " + payment, turn, phase, p,
                (info, pl, g) -> player.beginPayment(payment));
    }

    /** Queue the end of the manual-payment window opened by queuePaymentStart. */
    private void queuePaymentEnd(TestPlayer payer) {
        ScriptedChoicePlayer player = scriptedPlayer(payer);
        runCode("payment end", turn, phase, payer,
                (info, pl, g) -> player.endPayment());
    }

    /** The step being replayed's recorded payment ("" when none); set by the
     * replay loop so step keeps its (st, op, stepIdx) shape. */
    private String stepPayment = "";

    private void step(JsonObject st, String op, int stepIdx) {
        String payment = stepPayment;
        int seatIdx = st.has("seat") ? st.get("seat").getAsInt() : 0;
        TestPlayer p = seat(seatIdx);
        switch (op) {
            case "mana": {
                String mana = str(st, "mana");
                runCode("mana " + mana, turn, phase, p, (info, pl, g) -> addPool(pl, g, mana));
                return;
            }
            case "attack": {
                // gorge's attack op passes the current player's turn when the
                // scripted attacker belongs to the other seat.
                if (seatIdx != activeSeat) {
                    turn++;
                    activeSeat = seatIdx;
                    attackAdvancedTurn = true;
                }
                // XMage's attack() queues a selectAttackers command at
                // DECLARE_ATTACKERS; the checkpoint stays on this turn.
                List<String> attackers = names(st, "attackers");
                if (!attackers.isEmpty() && allMustAttack(attackers)) {
                    // Every attacker carries an AttacksEachCombatStaticAbility:
                    // XMage's checkAttackRequirements declares and taps it
                    // before selectAttackers runs, so getAvailableAttackers is
                    // empty on the first pass and selectAttackers (which would
                    // consume the queued attack command) is skipped. Queue no
                    // attack(): XMage declares them itself. Fail loudly if the
                    // scenario names a defender XMage would not force -- a
                    // must-attack effect (not goad) attacks any legal defender,
                    // which in this two-seat harness is the opponent.
                    int defenderSeat = seatOf(str(st, "defender"));
                    if (defenderSeat == seatIdx) {
                        throw new IllegalArgumentException("attack step " + stepIdx
                                + " is a must-attack creature with its own controller as defender");
                    }
                    phase = PhaseStep.DECLARE_ATTACKERS;
                    return;
                }
                List<String> attackNames = new ArrayList<>();
                for (String a : attackers) {
                    attackNames.add(combatName(a));
                }
                // A mixed declaration (Ares, God of War beside a Grizzly
                // Bears): XMage declares the must-attack creature itself
                // (above), and selectAttackers consumes only the commands of
                // the others, so a command for the forced one is left over.
                List<String> keptNames = withoutMustAttack(attackNames, ScenarioReplay::isMustAttackName);
                List<String> keptRefs = new ArrayList<>();
                for (int i = 0, kept = 0; i < attackNames.size() && kept < keptNames.size(); i++) {
                    if (keptNames.get(kept).equals(attackNames.get(i))) {
                        keptRefs.add(attackers.get(i));
                        kept++;
                    }
                }
                int defenderSeat = seatOf(str(st, "defender"));
                ExactAttackBatch exact = lowerAttackBatch(turn, seatIdx, defenderSeat, keptNames, keptRefs,
                        name -> {
                            int before = p.getActions().size();
                            attack(turn, p, name, seat(defenderSeat));
                            return p.getActions().get(before);
                        });
                if (!exact.refs.isEmpty()) {
                    exactAttacks.add(exact);
                }
                phase = PhaseStep.DECLARE_ATTACKERS;
                return;
            }
            case "pass_to": {
                // Only the phase matters here; the scenario has already run
                // the ops that reach it. gorge's generated scenarios never
                // emit pass_to (a pre-block declare-blockers checkpoint is
                // not observable in XMage, whose engine selects blockers in
                // beginStep before any priority), so this is for completeness.
                String stepName = str(st, "step");
                String decision = str(st, "decision");
                if (passToResolvesTop(steps(sc0), stepIdx)) {
                    // gorge's two-step "choose" then "priority" reaches a
                    // mid-resolution ask and answers it; XMage cannot stop
                    // inside a resolution, so the spell or ability stays on
                    // its stack until the second step resolves its top object
                    // (the answers are consumed by that resolution).
                    // A pair's deferred resolution waits on the active seat,
                    // the seat that queued the pair (queuePassCommands).
                    TestPlayer waiter = isOp(steps(sc0), stepIdx - 1, "pass") ? seat(activeSeat) : p;
                    waitStackResolved(turn, phase, waiter, true);
                    dropLastCast();
                    return;
                }
                if (st.has("active")) {
                    String active = str(st, "active");
                    int nextActiveSeat = active.equals("p1") ? 1 : active.equals("p0") ? 0 : -1;
                    if (nextActiveSeat >= 0) {
                        // An off-turn attack has already advanced to its
                        // attacker's turn; its matching main2 checkpoint must
                        // not advance a second time.
                        if (attackAdvancedTurn && nextActiveSeat == activeSeat) {
                            attackAdvancedTurn = false;
                        } else {
                            // pass_to asks for the next turn on this seat, not merely
                            // the next distinct seat: p0 after p0 skips p1's turn too.
                            turn += nextActiveSeat == activeSeat ? 2 : 1;
                            activeSeat = nextActiveSeat;
                            attackAdvancedTurn = false;
                        }
                    }
                }
                PhaseStep beforeStep = phase;
                if (decision.equals("blockers") || stepName.equals("declare-blockers")) {
                    phase = PhaseStep.DECLARE_BLOCKERS;
                } else if (stepName.equals("declare-attackers")) {
                    phase = PhaseStep.DECLARE_ATTACKERS;
                } else if (stepName.equals("begin-combat")) {
                    phase = PhaseStep.BEGIN_COMBAT;
                } else if (stepName.equals("end-combat")) {
                    phase = PhaseStep.END_COMBAT;
                } else if (stepName.equals("main2")) {
                    phase = PhaseStep.POSTCOMBAT_MAIN;
                } else if (stepName.equals("upkeep")) {
                    phase = PhaseStep.UPKEEP;
                } else if (stepName.equals("draw")) {
                    phase = PhaseStep.DRAW;
                } else if (stepName.equals("end")) {
                    phase = PhaseStep.END_TURN;
                } else if (stepName.equals("main1")) {
                    phase = MAIN;
                }
                // A pass_to that moves the phase is the queued replay's own
                // step entry; an empty one stays where the scenario already
                // is (phase is left as the preceding ops set it).
                enterQueuedStep(turn, phase, phase != beforeStep);
                return;
            }
            case "block": {
                // gorge's blocks are [blocker, attacker] pairs. XMage's
                // block() queues a declareBlockers command at DECLARE_BLOCKERS.
                // A recorded game's empty declaration has no "blocks": there
                // is nothing to queue, the step only reaches declare-blockers.
                JsonArray blocks = st.has("blocks") ? st.getAsJsonArray("blocks") : new JsonArray();
                ExactBlockBatch exact = new ExactBlockBatch(turn, seatIdx);
                for (JsonElement e : blocks) {
                    JsonArray pair = e.getAsJsonArray();
                    String blocker = pair.get(0).getAsString();
                    String attacker = pair.get(1).getAsString();
                    int before = p.getActions().size();
                    block(turn, p, combatName(blocker), combatName(attacker));
                    exact.blockers.add(blocker);
                    exact.attackers.add(attacker);
                    exact.actions.add(p.getActions().get(before));
                }
                if (!exact.blockers.isEmpty()) {
                    exactBlocks.add(exact);
                }
                phase = PhaseStep.DECLARE_BLOCKERS;
                return;
            }
            case "move": {
                String ref = str(st, "card");
                String name = xmageSpelling(refName(ref));
                // gorge names the exile zone "exile"; XMage's enum member is EXILED.
                String to = str(st, "to");
                Zone destination = moveDestination(to);
                runCode("move " + ref + " to " + destination, turn, phase, p, (info, pl, g) -> {
                    Card moving = moveSource(findMoveCard(pl, g, name), () -> battlefieldMoveSource(g, ref));
                    if (moving == null) {
                        throw new IllegalArgumentException("move card " + ref + " is not in "
                                + pl.getName() + "'s " + moveSourceZones() + " or on the battlefield");
                    }
                    if (!pl.moveCards(moving, destination, null, g)) {
                        throw new IllegalStateException("move " + ref + " to " + destination + " refused");
                    }
                });
                return;
            }
            case "attach": {
                String card = str(st, "card");
                String bearer = str(st, "attached_to");
                // Queued on the active player: a runCode for the non-active seat would
                // run only when it next gets priority, after this step's snapshot.
                runCode("attach " + card + " to " + bearer, turn, phase, playerA, (info, pl, g) -> {
                    Permanent attachment = permanentRef(g, card);
                    Permanent target = permanentRef(g, bearer);
                    // gorge's runner accepts a re-attach as a no-op: an ETB
                    // attach trigger (Baseball Bat, Coral Sword, Shredder's
                    // Armor, ...) may already have attached this exact pair
                    // during the resolve step, and Card.addAttachment refuses
                    // an attachment the bearer already lists (H5 rows).
                    if (attachAlreadySatisfied(target.getAttachments(), attachment.getId())) {
                        return;
                    }
                    // Card.addAttachment links both sides (the bearer's
                    // attachment list and the attachment's attachedTo) and
                    // applies XMage's own legality checks.
                    if (!target.addAttachment(attachment.getId(), null, g)) {
                        throw new IllegalStateException("attach " + card + " to " + bearer + " refused");
                    }
                });
                return;
            }
            case "cast": {
                if (st.has("mana")) {
                    String mana = str(st, "mana");
                    runCode("mana " + mana, turn, phase, p, (info, pl, g) -> addPool(pl, g, mana));
                }
                queuePaymentStart(payment, p);
                if (st.has("kicked")) {
                    throw new IllegalArgumentException("kicked unsupported");
                }
                // cast_mode elects a mode whose cost is a cast-time optional
                // additional cost. Bargain's is wired here: its yes/no ask is
                // answered from bargainedCast and the sacrifice it adds is
                // matched against this step's recorded choose picks.
                // "optionalcost" is a plain cast on this side: XMage poses the
                // "pay the additional cost?" chooseUse itself, so its yes and
                // the cost's picks come from xmage_answers. Any other mode
                // fails loudly rather than being silently dropped (the legacy
                // whole-rejection this replaces).
                String castMode = st.has("cast_mode") ? str(st, "cast_mode") : "";
                if (!castModeSupported(castMode)) {
                    throw new IllegalArgumentException("cast_mode " + castMode + " unsupported");
                }
                bargainedCast = "bargained".equals(castMode);
                optionalCostCast = "optionalcost".equals(castMode);
                kickedCast = "kicked".equals(castMode);
                kickerAsks = 0;
                alternativeCostCast = "alternative".equals(castMode);
                evokedCast = "evoked".equals(castMode);
                copyCostCast = copyCostKeyword(castMode);
                // "plot" is XMage's Plot special action (PlotAbility is a
                // SpecialAction: no stack, not a cast), activated from the
                // hand by its rule text "Plot {cost}" like XMage's own
                // PlotTest does, scoped to the step's card. The step's mana was added above; a
                // reduction (Doc Aurlock) is applied by XMage itself.
                if ("plot".equals(castMode)) {
                    String plotText = xabilityAt(stepIdx);
                    if (plotText.isEmpty()) {
                        throw new IllegalArgumentException("plot step " + stepIdx + " has no xmage_ability");
                    }
                    // Scope the pick to this step's card: two hand cards
                    // sharing a Plot cost have identical rule text, and the
                    // bare text plots whichever XMage lists first (CR
                    // 702.170a). boundActivationText binds the card's id as
                    // an alias, like an ordinary activation.
                    activateAbility(turn, phase, p, boundActivationText(p, str(st, "card"), plotText, stepIdx));
                    return;
                }
                // "suspend" is XMage's Suspend special action from the hand
                // (SuspendAbility is a SpecialAction with usesStack false,
                // CR 116.2f): it pays the suspend cost and exiles the card
                // with its time counters, no cast and no stack. Activated by
                // its rule-text prefix "Suspend N" like XMage's SuspendTest,
                // scoped to the step's card so another suspend card in hand
                // cannot be picked.
                if ("suspend".equals(castMode)) {
                    String suspendText = xabilityAt(stepIdx);
                    if (suspendText.isEmpty()) {
                        throw new IllegalArgumentException("suspend step " + stepIdx + " has no xmage_ability");
                    }
                    activateAbility(turn, phase, p, boundActivationText(p, str(st, "card"), suspendText, stepIdx));
                    return;
                }
                // "foretell" is XMage's Foretell special action from the hand
                // (ForetellAbility is a SpecialAction with usesStack false,
                // CR 116.2h): it pays {2} and exiles the card face down, no
                // cast and no stack. Activated by its rule-text prefix
                // "Foretell {cost}" like XMage's own foretell tests, scoped to
                // the step's card so another foretell card in hand cannot be
                // picked.
                if ("foretell".equals(castMode)) {
                    String foretellText = xabilityAt(stepIdx);
                    if (foretellText.isEmpty()) {
                        throw new IllegalArgumentException("foretell step " + stepIdx + " has no xmage_ability");
                    }
                    activateAbility(turn, phase, p, boundActivationText(p, str(st, "card"), foretellText, stepIdx));
                    return;
                }
                // "flashback" is the cast of a graveyard card for its
                // flashback cost. XMage offers it as a separate SpellAbility
                // named "Flashback <cost>" (CastFromGraveyardAbility), picked
                // like any ability by its rule-text prefix; the card's alias
                // scopes the pick to this step's card so another flashback
                // card in the graveyard cannot be chosen. The step's mana was
                // added above; a reduction (Bilbo) is applied by XMage itself.
                if ("flashback".equals(castMode)) {
                    String fbText = flashbackCommand(refAlias.get(str(st, "card")));
                    if (!sc0.has("xmage_answers")) {
                        answers(st, p);
                    }
                    for (String t : targets(st)) {
                        queueCastTarget(p, t);
                    }
                    activateAbility(turn, phase, p, fbText);
                    return;
                }
                // "disguised" casts the card face down for {3}; XMage selects
                // that cast by suffixing the card name ("<card> using
                // Disguise", DisguiseTest/CovetedFalconTest). "morphed" and
                // "megamorphed" are the same face-down cast for the Morph and
                // Megamorph families; XMage defines no Megamorph spelling, as
                // both set SpellAbilityCastMode.MORPH, so both suffix " using
                // Morph" (MorphAbility.java:79-88; MegamorphTest.java:24).
                String mode = castMode;
                if ("mayplay".equals(mode)) {
                    // A card cast from exile under a permanent's static
                    // permission is only in TestPlayer's playable set if that
                    // permission applies. XMage registers static abilities
                    // before the permanent exists (zcc 0), so a permission
                    // that keys its exile zone on the source's zcc misses the
                    // zone the entering trigger used (Azula, Cunning
                    // Usurper). Re-derive those counters at this cast's own
                    // priority, before the activate command queues.
                    runCode("repair static permission source zcc", turn, phase, p,
                            (info, pl, g) -> repairStaticSourceZCC(g));
                }
                if (!sc0.has("xmage_answers")) {
                    answers(st, p);
                }
                // Spree permits choosing further modes after the first. Its
                // generated answer records the chosen mode, not the decision
                // to stop, so close XMage's repeated mode prompt explicitly.
                if (isSpreeSpell(xmageName.isEmpty() ? refName(str(st, "card")) : xmageName)) {
                    setModeChoice(p, TestPlayer.MODE_SKIP);
                }
                castCostPicks = new ArrayList<>();
                if (st.has("answers")) {
                    for (JsonElement e : st.getAsJsonArray("answers")) {
                        JsonObject a = e.getAsJsonObject();
                        if (str(a, "kind").equals("choose")) {
                            castCostPicks.addAll(names(a, "pick"));
                        }
                    }
                }
                giftPromisedCast = castCostPicks.contains("gift_promise");
                // The cast command names the SpellAbility, not the card
                // object: a split/Room half is cast by its half name while
                // the hand holds the whole "A // B" card (castSpelling). An
                // Adventure/Omen cast is the other way round: the farm's step
                // names the parent card (the xengine ref binder keeps the
                // dealt card's name), so resolve the inset spell half's XMage
                // name and cast that. A Room's "room_alt" is the same shape
                // for a split card: the step names the parent, and the second
                // door is the back half's own name (altCastHalfName). A step
                // that already names the half falls through unchanged.
                String refCard = refName(str(st, "card"));
                String halfName = altCastHalfName(mode, xmageSpelling(refCard));
                String cardName = halfName != null ? halfName : castSpelling(refCard);
                if (kickedCast) {
                    String kickedErr = kickedCastShapeError(refCard);
                    if (kickedErr != null) {
                        throw new IllegalArgumentException(kickedErr);
                    }
                }
                // A face-down Morph/Megamorph/Disguise cast names the
                // SpellAbility by its XMage cast spelling; every plain cast
                // names the card itself. Morph and Megamorph share "using
                // Morph".
                String castSuffix = "morphed".equals(mode) || "megamorphed".equals(mode)
                        ? " using Morph"
                        : "disguised".equals(mode) ? " using Disguise" : "";
                boolean warpedCast = "warped".equals(mode);
                String card = cardName + castSuffix + (warpedCast ? " with Warp" : "");
                // A mayplay cast names the object by where it is (CR 601.2a),
                // not by its name: TestPlayer's "Cast <name>" search runs
                // over Zone.ALL and takes the first same-name match, so a
                // same-name card in the caster's hand beat the exiled one.
                String castObject = mayplayCastObject(mode, refAlias.get(str(st, "card")), cardName,
                        xmageSpelling(refCard), card);
                if (!castSuffix.isEmpty()) {
                    faceDownRefs.add(str(st, "card"));
                }
                List<String> tg = targets(st);
                if (!warpedCast && !alternativeCostCast && !evokedCast && hasAlternativeSourceCost(cardName)) {
                    // A plain cast step: gorge paid the mana cost, so decline
                    // the alternative cost (evoke, impending, dash, ...)
                    // XMage offers through its "Cast with no alternative
                    // cost" choice, rather than leave it to the AI. An
                    // "alternative" step already elected the cost through the
                    // key menu (alternativeCostKey); queuing a decline there
                    // left it for the elected cost's own target ask.
                    setChoice(p, "Cast with no alternative cost");
                }
                List<Integer> skips = castTargetSkipsAt(stepIdx, cardName, tg.size());
                if (splitScripted && spellTargetsDivided(cardName) && skips.isEmpty()) {
                    // The scripted "<ref>^X=<share>" answers name the targets
                    // and gorge's split; a target string here would be a
                    // second, unconsumed set.
                    castObjectSpell(turn, phase, p, castObject);
                    cast.add(cardName);
                    return;
                }
                Ability castAbility = spellAbility(cardName);
                if (!skips.isEmpty()) {
                    // The generator named every empty optional target object
                    // and where it falls between the filled ones, so the queue
                    // is exactly the plan: no blind trailing skip.
                    boolean queued = castAbility != null
                            && (needsQueuedCastTargets(castAbility) || firstTargetInLaterMode(castAbility));
                    if (spellTargetsDivided(cardName) || queueAdjustedCastTargets(queued, false, tg.size())) {
                        throw new IllegalArgumentException("cast step " + stepIdx + " carries xmage_target_skips for " + cardName
                                + ", a divided, adjusted or later-mode-target spell the explicit skip plan does not cover");
                    }
                    queueCastTargetsWithSkips(p, tg, skips);
                    castObjectSpell(turn, phase, p, castObject);
                } else if (!tg.isEmpty() && castQueuedTargets(turn, phase, p, cardName, castObject, tg, castAbility)) {
                    // Cast with its targets queued: see castQueuedTargets.
                } else if (!tg.isEmpty() && spellTargetsOnCastTrigger(cardName)) {
                    // The spell declares no target: its recorded targets
                    // belong to a cast trigger (Zone.STACK), whose own chooser
                    // runs after the cast. Queue them for that ask; any "up
                    // to" slot it leaves open is closed at ask time. One
                    // target takes this path too, so it is never set inline
                    // on a spell ability that has no targets ("Ability has no
                    // targets").
                    castWithQueuedTargets(p, tg, () -> castObjectSpell(turn, phase, p, castObject));
                } else if (tg.size() == 1 && isSeatRef(tg.get(0)) && !hasGift(cardName)) {
                    castObjectSpell(turn, phase, p, castObject, seat(seatOf(tg.get(0))));
                } else if (tg.size() == 1 && isSeatRef(tg.get(0))) {
                    // A Gift spell (Mind Spiral, Sazacap's Brew): the castSpell
                    // player form binds the wrong ask, so queue the spell's
                    // own player target and close the rest.
                    // Only a promised gift opens the spell's second, gift-gated
                    // target (Sazacap's Brew, Mind Spiral); a declined gift has
                    // no such object, and a skip queued for it is left unused
                    // ("Targets ... Count are not equal (found [[target_skip]])").
                    addTarget(p, seat(seatOf(tg.get(0))));
                    if (giftPromisedCast) {
                        addTarget(p, TestPlayer.TARGET_SKIP);
                    }
                    castObjectSpell(turn, phase, p, castObject);
                } else if (tg.isEmpty()) {
                    castObjectSpell(turn, phase, p, castObject);
                    cast.add(cardName);
                    return;
                } else if (tg.size() == 1 && cast.contains(castSpelling(refName(tg.get(0))))) {
                    // Targeting a spell cast by an earlier step. cast holds
                    // cast-command names and the target is a scenario ref;
                    // the setup alias names the card in hand, not the spell,
                    // so a stack target is named, not aliased.
                    String spell = castSpelling(refName(tg.get(0)));
                    if (targetsOnlyGraveyardCards(castAbility)) {
                        // A spell that can only target a graveyard card
                        // (Reclaim) names the card of a spell that has left
                        // the stack (countered, say), which cast cannot see
                        // (dropLastCast forgets only the stack's top). The
                        // card is in the graveyard once the spell is gone, so
                        // hold the cast until then: at the priority before
                        // that XMage has no legal target and would fail the
                        // command ("Can't find ability to activate").
                        castSpell(turn, phase, p, castObject, targetName(tg.get(0)), spell,
                                StackClause.WHILE_NOT_ON_STACK);
                    } else {
                        // Wait for it on the stack.
                        castSpell(turn, phase, p, castObject, spell, spell);
                    }
                } else if (tg.size() == 1 && loneTargetNeedsQueue(castAbility)) {
                    // One card or permanent target (a lone player ref and a
                    // spell on the stack took their own routes above) for a
                    // spell with several independent "up to" objects (Relive
                    // the Past's artifact, land and non-Aura enchantment): the
                    // inline string form sets the ref on the FIRST object,
                    // which rejects a ref only a later object accepts ("Can't
                    // find ability to activate command").
                    // Queue it instead; an object with no candidate is
                    // declined at ask time (unscriptableEmptyAsk) and one that
                    // is posed again is closed by closeCastAskedAgain.
                    castWithQueuedTargets(p, tg, () -> castObjectSpell(turn, phase, p, castObject));
                } else if (tg.size() == 1) {
                    // A single target goes through XMage's own string form. A
                    // divided target whose inline form XMage cannot read
                    // (Twin Bolt) was queued by castQueuedTargets above.
                    castSpell(turn, phase, p, castObject, targetName(tg.get(0)));
                } else {
                    // Two or more targets: queue each through addTarget and
                    // cast with no $target, so an "up to N" slot stays open
                    // and same-name permanents are told apart by the alias
                    // each ref carries. A trailing skip closes any slot XMage
                    // offers that this scenario did not fill (a reflexive
                    // sub-ability with no legal target, say).
                    if (spellTargetsDivided(cardName)) {
                        // A divided-amount target (TargetAmount: Biogenic
                        // Upgrade, Synchronized Charge) takes the whole set
                        // as one castSpell string and lets XMage split it,
                        // as the single-target form does; addTarget's alias
                        // answers are rejected ("Must be target amount").
                        // Use the same target-amount name helper as the
                        // scripted "<ref>^X=<share>" answers, so a token ref
                        // keeps its "@...:token:" spelling and the addTargets
                        // choice context resolves it live (a bare name would
                        // lose the ref's creation-order #n).
                        List<String> names = new ArrayList<>();
                        for (String t : tg) {
                            names.add(amountTargetName(t));
                        }
                        castSpell(turn, phase, p, castObject, String.join("^", names));
                        cast.add(cardName);
                        return;
                    }
                    // No speculative trailing skip: XMage itself stops asking
                    // once a slot's candidates are exhausted (Hunters'
                    // Feast's "any number of target players" with every
                    // player chosen), so a skip queued from the card's
                    // target maximums is left unused ("Targets ... Count are
                    // not equal (found [[target_skip]])"). A slot XMage does
                    // pose again with no scripted answer is closed at ask
                    // time by closeCastAskedAgain.
                    castWithQueuedTargets(p, tg, () -> castObjectSpell(turn, phase, p, castObject));
                }
                cast.add(cardName);
                return;
            }
            case "activate": {
                String printed = xabilityAt(stepIdx);
                if (printed.isEmpty()) {
                    throw new IllegalArgumentException("activate step " + stepIdx + " has no xmage_ability");
                }
                // A token's self-reference is XMage's own wording ("Sacrifice
                // this artifact" on a Treasure), not the "{this}" a card's
                // rule text keeps: select by the mapped XMage token's rule.
                String text = str(st, "card").contains(":token:")
                        ? tokenAbilityText(battlefieldName(str(st, "card")), printed) : printed;
                if (st.has("mana")) {
                    String mana = str(st, "mana");
                    runCode("mana " + mana, turn, phase, p, (info, pl, g) -> addPool(pl, g, mana));
                }
                queuePaymentStart(payment, p);
                if (!sc0.has("xmage_answers")) {
                    answers(st, p);
                }
                String card = battlefieldName(str(st, "card"));
                if (turnsFaceUp(text)) {
                    faceDownRefs.remove(str(st, "card"));
                }
                if (str(st, "card").contains(":token:") ? isTokenManaAbilityText(card, text)
                        : isManaAbilityText(card, text)) {
                    Integer abilityIndex = st.has("ability_index") && st.get("ability_index").isJsonPrimitive()
                            ? st.get("ability_index").getAsInt() : null;
                    activateManaAbilityOnCard(stepIdx, p, str(st, "card"), card, text, abilityIndex);
                    return;
                }
                // A divided (TargetAmount) activation's recipients come from
                // the generator's scripted "<ref>^X=<share>" answers, already
                // queued for this step; queueing the bare step targets too
                // would hand chooseTargetAmount an alias it rejects ("Must be
                // target amount, but found unknown target", The Earth Crystal).
                if (!stepScriptsTargetAmount(sc0, stepIdx)) {
                    for (String t : targets(st)) {
                        queueCastTarget(p, t);
                    }
                }
                activateAbility(turn, phase, p, boundActivationText(p, str(st, "card"), text, stepIdx));
                return;
            }
            case "play": {
                String playRef = str(st, "card");
                // A land play of a modal DFC with a nonland front enters its
                // land back (CR 712.12, 305.1), and XMage's PlayLandAbility
                // reads "Play <back face>"; naming the front finds nothing.
                String landName = playLandName(xmageSpelling(refName(playRef)));
                String playAlias = refAlias.get(playRef);
                if (playAlias == null) {
                    playUnaliasedLand(p, playRef, landName);
                    return;
                }
                // playLand names the land: XMage enters the FIRST same-named
                // hand card, not the one the ref's setup alias binds, so a
                // later "p0:Swamp#4" tap or pick reaches a different land
                // than gorge's (CR 305.1, 605.3a). Bind a space-free alias to
                // the card the ref means (from whichever zone it plays: hand,
                // graveyard, exile, library; a double-faced land by its half)
                // just ahead of the play and prefix the command with it
                // ("@key Play <name>").
                String playKey = "play" + stepIdx;
                java.util.Set<UUID> before = noteBattlefield(p, playRef);
                runCode("bind " + playRef, turn, phase, p, (info, pl, g) -> {
                    UUID id = playableLandId(p, g, p.getAliasByName(playAlias), landName);
                    if (id == null) {
                        throw new IllegalArgumentException("play " + playRef + ": no " + landName
                                + " playable by " + p.getName());
                    }
                    for (int j = 0; j < seats; j++) {
                        seat(j).addAlias(playKey, id);
                    }
                });
                activateAbility(turn, phase, p, "@" + playKey + " Play " + landName);
                bindEnteredLand(p, playRef, before);
                return;
            }
            case "pass":
                // Validated here; its XMage commands are queued around the
                // checkpoint by queuePassCommands, which the replay loop runs before this step.
                if (passAction(steps(sc0), stepIdx, activeSeat) == PASS_RUN) {
                    followPassPosition(steps(sc0), stepIdx);
                    PhaseStep declaration = declarationAhead(steps(sc0), stepIdx, seats);
                    if (declaration != null) {
                        // The checkpoint is taken at the last priority before the
                        // declaration and reports the declaration step; the
                        // declaring step moves the clock on.
                        phase = beforeDeclaration(declaration);
                        checkpointStepAs = declaration;
                    }
                }
                return;
            case "resolve":
                // gorge's resolve op passes priority until the stack is empty.
                waitStackResolved(turn, phase, p);
                // waitStackResolved empties the stack, so every cast recorded
                // on it is gone. A later cast targeting one of those names is
                // targeting the permanent (the setup alias still names it),
                // not a spell that is no longer on the stack.
                cast.clear();
                return;
            default:
                throw new IllegalArgumentException("op " + op + " unsupported");
        }
    }

    /** Queue as-enters choices before addCard places setup permanents. */
    private void queueSetupChoices(JsonObject scenario) {
        if (!scenario.has("xmage_answers") || !scenario.get("xmage_answers").isJsonArray()) {
            return;
        }
        JsonArray answers = scenario.getAsJsonArray("xmage_answers");
        if (answers.size() == 0 || !answers.get(0).isJsonArray()) {
            return;
        }
        for (JsonElement e : answers.get(0).getAsJsonArray()) {
            JsonObject a = e.getAsJsonObject();
            String kind = str(a, "kind");
            if (kind.equals("setup_target")) {
                // An optional target ask posed while setup resolves a placed
                // Saga's chapter I (and queues the next chapter): script the
                // decline gorge's runner took, so TestPlayer does not
                // auto-pick a legal target. Only "[target_skip]" is read.
                addTarget(seat(a.get("seat").getAsInt()), TestPlayer.TARGET_SKIP);
                continue;
            }
            TestPlayer p = seat(a.get("seat").getAsInt());
            String value = str(a, "value");
            if (kind.equals("setup_flip")) {
                // A coin flip the setup drive walked into before the first
                // gameplay step (Mana Crypt's turn-1 upkeep trigger): XMage
                // poses the same flip during the drive, so the result must be
                // queued ahead of build() or TestPlayer's unscripted
                // flipCoinResult fails the run (oraclegen emits "win"/"lose").
                queueFlipResult(p, value);
                continue;
            }
            if (kind.equals("setup_mode")) {
                // A setup-drive modal pick (Zuko, Conflicted's turn-1 charm):
                // XMage poses chooseMode, or chooseUse for a yes/no
                // GenericChoice, exactly as the step-time "mode" kind does.
                if (value.equalsIgnoreCase("yes") || value.equalsIgnoreCase("no")) {
                    setChoice(p, value.equalsIgnoreCase("yes"));
                } else {
                    setModeChoice(p, value);
                }
                continue;
            }
            if (!kind.equals("setup_choice")) {
                continue;
            }
            if (value.equals("yes") || value.equals("no")) {
                // An optional boolean the setup drive posed (Gathering
                // Stone's mill/reveal pair): XMage asks chooseUse, not a
                // labelled choice.
                setChoice(p, value.equals("yes"));
                continue;
            }
            queueSetupChoices(p, value);
        }
    }

    static void queueSetupChoices(TestPlayer player, String value) {
        queueChoice(player, value);
    }

    /** Queue one scripted coin-flip result on the player's CHOICE queue:
     * "win" maps to TestPlayer.FLIPCOIN_RESULT_TRUE ("[flipcoin_true]"),
     * "lose" to FLIPCOIN_RESULT_FALSE — the tokens TestPlayer.flipCoinResult
     * consumes from the queue head, which unscripted fails the replay with
     * "Missing FLIP COIN RESULT def". */
    static void queueFlipResult(TestPlayer p, String value) {
        p.addChoice(value.equals("win") ? TestPlayer.FLIPCOIN_RESULT_TRUE : TestPlayer.FLIPCOIN_RESULT_FALSE);
    }

    /** Queue one scripted die-roll result on the player's CHOICE queue:
     * TestPlayer.DIE_ROLL ("[die_roll]: ") followed by the natural result, the
     * token TestPlayer.rollDieResult consumes from the queue head. Unscripted,
     * the replay fails with "Missing DIE ROLL RESULT def". Only step-time
     * rolls have a producer (oraclegen's kind "die"); a die the setup drive
     * rolls before the first gameplay step is not scripted, unlike a setup
     * coin flip, so it still strict-misses. */
    static void queueDieResult(TestPlayer p, String value) {
        p.addChoice(TestPlayer.DIE_ROLL + value);
    }

    /** Whether the generator scripted a divided-target share
     * ("<ref>^X=<share>", oraclegen dividedTargetAnswers) for step i: the
     * TargetAmount recipients of an activation whose own targets it then
     * answers in place of the step's bare target list. */
    static boolean stepScriptsTargetAmount(JsonObject sc, int i) {
        if (sc == null || !sc.has("xmage_answers") || !sc.get("xmage_answers").isJsonArray()) {
            return false;
        }
        JsonArray xans = sc.getAsJsonArray("xmage_answers");
        if (i < 0 || i >= xans.size() || !xans.get(i).isJsonArray()) {
            return false;
        }
        for (JsonElement e : xans.get(i).getAsJsonArray()) {
            JsonObject a = e.getAsJsonObject();
            if ("target".equals(str(a, "kind")) && str(a, "value").contains("^X=")) {
                return true;
            }
        }
        return false;
    }

    /** Applies the generator's scripted answers (oraclegen.XAnswer). */
    private void scripted(JsonArray as, String payment, boolean inferWaterbendX) {
        int waterbendX = inferWaterbendX ? waterbendXFromAnswers(as, payment) : 0;
        boolean waterbendXQueued = false;
        for (JsonElement e : as) {
            JsonObject a = e.getAsJsonObject();
            TestPlayer p = seat(a.get("seat").getAsInt());
            String kind = str(a, "kind");
            String v = str(a, "value");
            switch (kind) {
                case "target":
                    if (isSeatRef(v)) {
                        addTarget(p, seat(seatOf(v)));
                    } else if (v.contains("^X=")) {
                        // Divided damage: the ref carries its share.
                        String[] parts = v.split("\\^X=", 2);
                        bindAnswerAlias(parts[0]);
                        addTarget(p, amountTargetName(parts[0]) + "^X=" + parts[1]);
                    } else {
                        // Bind every ref the answer names before targetName:
                        // a bare ref, an "@ref" spelling, or a "^"-joined
                        // multi-part record (echo's single SacrificeTargetCost
                        // selects all its lands from one record). A target
                        // that entered after setup has no @alias yet, and
                        // targetName passes an "@..." value through unchanged,
                        // so binding only a bare isScenarioRef(v) value left
                        // those targets unmatched. bindAnswerAlias splits on
                        // "^" and strips "@", so this one call covers every
                        // shape and is a no-op for labels and skip tokens.
                        bindAnswerAlias(v);
                        addTarget(p, v.equals("[target_skip]") ? TestPlayer.TARGET_SKIP : targetName(v));
                    }
                    break;
                case "amount":
                    setChoiceAmount(p, Integer.parseInt(v));
                    break;
                case "mode":
                    // Some GenericChoice effects are recorded as a gorge
                    // mode decision but XMage asks the player through
                    // chooseUse. Route their boolean answer to the player's
                    // CHOICE queue, not the numeric mode queue.
                    if (v.equalsIgnoreCase("yes") || v.equalsIgnoreCase("no")) {
                        setChoice(p, v.equalsIgnoreCase("yes"));
                    } else {
                        setModeChoice(p, v);
                    }
                    break;
                case "flip":
                    // A coin flip the scenario's runner recorded (Mana
                    // Crypt's upkeep): XMage poses the same flip while this
                    // step executes, and TestPlayer's flipCoinResult consumes
                    // the token from the CHOICE queue head (oraclegen emits
                    // "win"/"lose"; the drive-window "setup_flip" kind is
                    // queued before build() and never reaches this switch).
                    queueFlipResult(p, v);
                    break;
                case "die":
                    // A die roll the scenario's runner recorded
                    // (item.die_rolls): XMage poses the same roll while this
                    // step executes, and TestPlayer.rollDieResult consumes the
                    // natural result from the CHOICE queue head. The die
                    // modifier is the engine's own, so the natural roll is the
                    // value to force.
                    queueDieResult(p, v);
                    break;
                case "setup_choice":
                case "setup_target":
                case "setup_mode":
                case "setup_flip":
                    // Queued before setup placement; never enqueue it again at
                    // the corresponding gameplay step.
                    break;
                case "choice":
                    if (v.equals("yes") || v.equals("no")) {
                        setChoice(p, v.equals("yes"));
                    } else if (isSeatRef(v)) {
                        setChoice(p, seat(seatOf(v)).getName());
                    } else {
                        String waterbendTap = ScriptedChoicePlayer.waterbendTapName(v);
                        if (waterbendTap != null && !waterbendXQueued && waterbendX > 0) {
                            // Waterbend {X}: xproof records the helper taps and pool spend, not a
                            // separate X amount. TestPlayer.announceX reads this before the tap ask.
                            queueChoice(p, "X=" + waterbendX);
                            waterbendXQueued = true;
                        }
                        queueChoice(p, aliasChoiceValue(waterbendTap != null ? waterbendTap : v));
                    }
                    break;
                default:
                    throw new IllegalArgumentException("xmage answer kind " + kind);
            }
        }
    }

    /** Queue one label answer on the player's choice queue (FIFO). A mana
     * ability that adds a colour of the controller's choice, and a permanent's
     * "as it enters, choose a color", both pose XMage's ChoiceColor dialog,
     * which TestPlayer answers from this queue by colour name ("White"); with
     * the queue empty it picks a colour at random. */
    static void queueChoice(TestPlayer p, String v) {
        p.addChoice(v.equals("[choice_skip]") ? TestPlayer.CHOICE_SKIP : v);
    }

    private void answers(JsonObject st, TestPlayer p) {
        if (!st.has("answers")) {
            return;
        }
        for (JsonElement e : st.getAsJsonArray("answers")) {
            JsonObject a = e.getAsJsonObject();
            String kind = str(a, "kind");
            List<String> pick = names(a, "pick");
            switch (kind) {
                case "target":
                    for (String t : pick) {
                        if (isSeatRef(t)) {
                            addTarget(p, seat(seatOf(t)));
                        } else {
                            addTarget(p, targetName(t));
                        }
                    }
                    break;
                case "yesno":
                case "trigger_optional":
                    for (String t : pick) {
                        setChoice(p, t.equalsIgnoreCase("yes") || t.equalsIgnoreCase("true"));
                    }
                    break;
                case "modes":
                case "choose":
                    // gorge-side answers; XMage's come from xmage_answers.
                    break;
                default:
                    throw new IllegalArgumentException("answer kind " + kind + " unsupported");
            }
        }
    }

    /**
     * What a cast command names for one cast step. A "mayplay" cast of a whole
     * card (not a split/Room half, which XMage names by half) whose ref setup
     * bound an alias is the cast "@ref": TestPlayer then matches the
     * ability's source object by id, so the exiled copy is cast and a
     * same-name card in the caster's hand stays there (CR 601.2a: the card
     * is picked by where it is). Every other cast, and a mayplay ref setup
     * never bound, keeps the name form (card).
     */
    static String mayplayCastObject(String mode, String alias, String castName, String refSpelling,
            String card) {
        if (!"mayplay".equals(mode) || alias == null || !castName.equals(refSpelling)) {
            return card;
        }
        return alias;
    }

    /** castSpell for what mayplayCastObject picked. The plain and the
     * player-target castSpell forms refuse an "@ref" alias, so an aliased cast
     * goes through activateAbility, whose "activate:Cast @ref" command is the
     * same one (ACTIVATE_CAST) and which TestPlayer matches by object id. */
    private void castObjectSpell(int turn, PhaseStep phase, TestPlayer p, String castObject) {
        if (castObject.startsWith("@")) {
            activateAbility(turn, phase, p, "Cast " + castObject);
        } else {
            castSpell(turn, phase, p, castObject);
        }
    }

    private void castObjectSpell(int turn, PhaseStep phase, TestPlayer p, String castObject, Player target) {
        if (castObject.startsWith("@")) {
            activateAbility(turn, phase, p, "Cast " + castObject, target);
        } else {
            castSpell(turn, phase, p, castObject, target);
        }
    }

    /**
     * Re-derive the source zone-change counter of every continuous effect's
     * static ability from the permanent that currently supplies it.
     *
     * XMage registers a card's static abilities with GameState at load time
     * (GameState.addAbility -> addEffect), before the card is a permanent, so
     * getStackMomentSourceZCC() stays 0 -- the warning on
     * CardUtil.getExileZoneId names exactly this. A static effect that keys
     * instance state on that counter, as the play-from-exile permissions do
     * (CardUtil.getExileZoneId(game, source)), then reads the zone of the
     * pre-game placeholder instead of the settled permanent. Azula, Cunning
     * Usurper is one: AzulaCunningUsurperExileEffect runs with the entering
     * trigger's zcc while AzulaCunningUsurperCastEffect asks for the zcc-0
     * zone, so TestPlayer.getPlayable never lists the exiled card's cast
     * ("Can't find ability to activate command: Cast @p1:Shield Sphere").
     *
     * The permanent's counter is the authority. Only a registered static
     * ability still at 0 is touched, and only when its source is a battlefield
     * permanent, so an ability the engine already initialized is left alone.
     * Sweeping every layered and as-thought effect covers the next static
     * permission that keys on its own zcc without naming the card.
     */
    static void repairStaticSourceZCC(Game game) {
        java.util.Map<java.util.UUID, Integer> permanentZCC = new java.util.HashMap<>();
        for (Permanent permanent : game.getBattlefield().getAllActivePermanents()) {
            permanentZCC.put(permanent.getId(), permanent.getZoneChangeCounter(game));
        }
        if (permanentZCC.isEmpty()) {
            return;
        }
        java.util.Set<java.util.UUID> repaired = new java.util.HashSet<>();
        java.util.function.Consumer<Ability> repair = ability -> {
            if (!(ability instanceof StaticAbility)
                    || ability.getStackMomentSourceZCC() != 0
                    || !repaired.add(ability.getId())) {
                return;
            }
            Integer zcc = permanentZCC.get(ability.getSourceId());
            if (zcc != null && zcc > 0) {
                ability.setSourceObjectZoneChangeCounter(zcc);
            }
        };
        ContinuousEffects effects = game.getState().getContinuousEffects();
        for (ContinuousEffect effect : effects.getLayeredEffects(game)) {
            for (Ability ability : effects.getLayeredEffectAbilities(effect)) {
                repair.accept(ability);
            }
        }
        for (AsThoughEffectType type : AsThoughEffectType.values()) {
            for (AsThoughEffect effect : effects.getApplicableAsThoughEffects(type, game)) {
                for (Ability ability : effects.getAsThoughEffectsAbility(effect)) {
                    repair.accept(ability);
                }
            }
        }
    }

    private List<String> targets(JsonObject st) {
        return new ArrayList<>(names(st, "targets"));
    }

    private static boolean isSeatRef(String s) {
        return s.matches("p[0-9]+");
    }

    private static int seatOf(String s) {
        return Integer.parseInt(s.substring(1));
    }

    /**
     * Cast through the target queue when the inline {@code $target=} form
     * cannot work: an adjuster may add the SpellAbility's target slots only
     * after cast setup, and a modal spell's first target may live in a mode
     * after the first. Inline $target is validated too early (against the
     * unadjusted ability's first mode, which has no target), so the scenario
     * targets are queued for the chooser that runs during casting and the
     * cast carries none. Returns false, doing nothing, for every other cast.
     */
    boolean castQueuedTargets(int turn, PhaseStep phase, TestPlayer p, String card, List<String> tg,
            Ability ability) {
        return castQueuedTargets(turn, phase, p, card, card, tg, ability);
    }

    /** As above; castObject is what the cast command names (the card's name,
     * or its "@ref" alias for a mayplay cast, see mayplayCastObject). */
    boolean castQueuedTargets(int turn, PhaseStep phase, TestPlayer p, String card, String castObject,
            List<String> tg, Ability ability) {
        // Derive every routing flag HERE, from the one ability, so a caller
        // cannot drop the modal-first-target-in-a-later-mode term: the test
        // drives this method with a real card's spell ability, and a routing
        // term removed anywhere in it makes that test fail.
        boolean adjusted = needsQueuedCastTargets(ability);
        boolean firstTargetInLaterMode = firstTargetInLaterMode(ability);
        boolean divided = targetsDivided(ability);
        Integer soleShare = tg.size() == 1 ? soleRecipientAmount(ability) : null;
        if (soleShare != null) {
            // A divided target XMage's inline $target= cannot fill (Twin
            // Bolt's TargetAnyTargetAmount: "selected 1 of 2"): the only
            // recipient takes the whole amount, which chooseTargetAmount
            // reads from the queue as "<name>^X=<amount>" while casting.
            addTarget(p, amountTargetName(tg.get(0)) + "^X=" + soleShare);
            castObjectSpell(turn, phase, p, castObject);
            return true;
        }
        if (!queueAdjustedCastTargets(adjusted || firstTargetInLaterMode, divided, tg.size())) {
            return false;
        }
        for (String t : tg) {
            queueCastTarget(p, t);
        }
        if (adjusted) {
            adjustedCasts.add(card);
        }
        castObjectSpell(turn, phase, p, castObject);
        return true;
    }

    /** Queue one cast target ref on the target queue. */
    private void queueCastTarget(TestPlayer p, String t) {
        if (isSeatRef(t)) {
            addTarget(p, seat(seatOf(t)));
        } else if (isAbilityRef(t)) {
            queueAbilityTarget(p, t);
        } else if (cast.contains(castSpelling(refName(t)))) {
            // A spell an earlier step cast: its setup alias names the card
            // in hand, not the spell. The target string is the cast-command
            // name (a split/Room half), matching what castSpell queued.
            addTarget(p, castSpelling(refName(t)));
        } else {
            addTarget(p, targetName(t));
        }
    }

    /** True for an "ability:" ref ("p0:ability:Elvish Visionary#2"): the
     * k-th pending ability object on the stack controlled by that seat whose
     * source is the named card, gorge's own grammar for a stack ability
     * (engine/rules/oracle_run.go splitRef). */
    static boolean isAbilityRef(String ref) {
        return isScenarioRef(ref) && ref.startsWith("ability:", ref.indexOf(':') + 1);
    }

    /** The source card name of an ability ref, "#k" and seat stripped. */
    static String abilitySourceName(String ref) {
        return refName(ref.substring(0, ref.indexOf(':') + 1) + ref.substring(ref.indexOf(':') + 1 + "ability:".length()));
    }

    /** The 1-based "#k" ordinal of a ref, 1 when it has none. */
    static int refOrdinal(String ref) {
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            return Integer.parseInt(ref.substring(hash + 1));
        }
        return 1;
    }

    /** The nth (1-based) element of a stack, counted bottom-up: gorge ranks
     * pending abilities in creation order, the oldest first, and XMage
     * iterates its stack top-first, so the same ability gets the same k. */
    static <T> T nthPending(List<T> topFirst, java.util.function.Predicate<T> matches, int nth) {
        int seen = 0;
        for (int i = topFirst.size() - 1; i >= 0; i--) {
            if (matches.test(topFirst.get(i)) && ++seen == nth) {
                return topFirst.get(i);
            }
        }
        return null;
    }

    /** Counter that makes each ability-ref alias unique: the same ref can
     * name a different ability object in a later step, and TestPlayer
     * refuses to rebind an alias. Reset by build(). */
    private int abilityAliasSeq;

    /** Queue a target that names a stack ability. XMage's stack branch
     * matches an ability by an alias or by a prefix of its rule text
     * (StackAbility.toString), never by "ability:<Source>", and the ability
     * does not exist yet when the step's answers are queued (every step is
     * registered before execute()). So bind an alias to the pending
     * ability's id with a code action registered just ahead of the
     * activation or cast, and queue that alias. */
    /**
     * XMage matches an activated command by a prefix of the ability's rule
     * text across every permanent the player controls, so two planeswalkers
     * that share "+1: ..." text activate whichever XMage lists first (Ajani
     * Unrelenting beside Ajani Goldmane). The scenario names the card: bind a
     * space-free alias to that setup object just ahead of the activation and
     * prefix the command with it ("@ref text from ref object"). A card with no
     * setup alias (a token, a permanent cast or entered mid-window) is bound
     * the same way, by the live playable ability whose source carries the
     * ref's name: left bare, a cost-only text like "{T}" matches any playable
     * ability that starts with it (a Swamp's "{T}: Add {B}" beside the Hulk's
     * "{T}: Mill a card."), so XMage would activate the wrong object.
     */
    private String boundActivationText(TestPlayer p, String ref, String text, int stepIdx) {
        String alias = refAlias.get(ref);
        if (text.startsWith("@")) {
            return text;
        }
        String key = "act" + stepIdx;
        String name = refName(ref);
        runCode("bind " + ref, turn, phase, p, (info, pl, g) -> {
            UUID setupId = alias == null ? null : p.getAliasByName(alias);
            if (setupId == null && ref.contains(":token:")) {
                // A token the window created has no setup alias: its ref's
                // "#k" ranks the controller's live tokens of that name in
                // creation order (tokenChoice), as every other token ref.
                // A mana ability (a Treasure's) is not in getPlayable's list
                // when nothing is being paid, so the live name scan below
                // cannot find it; the token itself is the source.
                Permanent token = tokenChoice(ref, p.getId(), g.getBattlefield().getAllPermanents());
                if (token != null) {
                    setupId = token.getId();
                }
            }
            UUID id = activationSourceId(p, g, name, text, setupId);
            if (id == null) {
                id = setupId;
            }
            if (id == null) {
                throw new IllegalArgumentException("activate " + ref + ": no live ability for it and no setup object bound to "
                        + alias);
            }
            for (int j = 0; j < seats; j++) {
                seat(j).addAlias(key, id);
            }
        });
        return "@" + key + " " + text;
    }

    /**
     * The name XMage's land-play command must carry for a card: a modal
     * double-faced card whose left half is not a land and whose right half
     * is enters that right half on a land play (CR 712.12, 305.1), and the
     * PlayLandAbility reads "Play <right half>". A land-front modal DFC (a
     * Pathway) and every plain land keep the name the ref spells.
     *
     * Card-based and package-private so a contract test can pin the rule
     * without the card repository (which needs H2); the name-based overload
     * resolves the ref's card through CardRepository at queue time.
     */
    static String playLandName(Card card, String fallback) {
        if (card instanceof ModalDoubleFacedCard) {
            ModalDoubleFacedCard mdfc = (ModalDoubleFacedCard) card;
            if (!mdfc.getLeftHalfCard().isLand() && mdfc.getRightHalfCard().isLand()) {
                return mdfc.getRightHalfCard().getName();
            }
        }
        return fallback;
    }

    /** The XMage land-play name for the named card, or the name unchanged
     * when the card repository cannot resolve it. */
    private static String playLandName(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card card = info == null ? null : info.createCard();
        return playLandName(card, name);
    }

    /**
     * The source id XMage must match for a land-play step: the live
     * PlayLandAbility (any zone the player can play from: hand, graveyard,
     * exile, library top) whose source is the ref's setup-bound object. A
     * double-faced land's ability sits on a HALF card whose id differs from
     * the main card the setup alias binds, so the main card id counts as the
     * bound object too. Null when no live ability matches.
     */
    private UUID playableLandId(TestPlayer p, Game g, UUID bound, String landName) {
        List<PlayLandCandidate> candidates = new ArrayList<>();
        for (Ability ability : p.getPlayable(g, true)) {
            if (!(ability instanceof PlayLandAbility)) {
                continue;
            }
            Card source = g.getCard(ability.getSourceId());
            candidates.add(new PlayLandCandidate(source == null ? null : source.getName(),
                    ability.getSourceId(), source == null ? null : source.getMainCard().getId()));
        }
        return choosePlayLandSource(candidates, landName, bound);
    }

    /** One live land-play ability: its source card's name, the source id the
     * alias must carry, and the id of the source's main card (the same id
     * for a plain card, the whole card for a double-faced half). */
    static final class PlayLandCandidate {
        final String sourceName;
        final UUID sourceId;
        final UUID mainId;

        PlayLandCandidate(String sourceName, UUID sourceId, UUID mainId) {
            this.sourceName = sourceName;
            this.sourceId = sourceId;
            this.mainId = mainId;
        }
    }

    /**
     * Picks the source id a land-play alias must carry: the ability of the
     * setup-bound object itself (by source id, or by main-card id for a
     * double-faced half) so a later tap or pick of the ref reaches the land
     * the play entered (CR 305.1, 605.3a); otherwise the first same-named
     * land, which is what a name-only play would enter. Null when none.
     */
    UUID choosePlayLandSource(List<PlayLandCandidate> candidates, String landName, UUID bound) {
        UUID nameMatch = null;
        for (PlayLandCandidate c : candidates) {
            if (bound != null && (c.sourceId.equals(bound) || bound.equals(c.mainId))) {
                return c.sourceId;
            }
            if (nameMatch == null && sourceNameIsNamed(c.sourceName, landName)) {
                nameMatch = c.sourceId;
            }
        }
        return nameMatch;
    }

    /**
     * The source id XMage must match for a named activation: the live
     * playable ability that carries the scenario's ability text belongs to
     * the named card. The setup alias binds the object as it was created,
     * but a card that moved zones (a graveyard activation, a permanent cast
     * from hand) has a NEW id for its ability's source, so binding the setup
     * id makes XMage find no ability ("Can't find ability to activate
     * command: @act0 {3}{B}{B}{R}{R}"). Prefer the setup id itself (a
     * same-named permanent whose source is the setup object, which keeps
     * duplicate-name cards exact); otherwise fall back to the first live
     * ability whose source name is the ref's card. Returns null when no live
     * ability matches, so the caller keeps the setup alias.
     */
    private UUID activationSourceId(TestPlayer p, Game g, String name, String text, UUID setupId) {
        List<ActivationCandidate> candidates = new ArrayList<>();
        for (Ability ability : p.getPlayable(g, true)) {
            mage.MageObject source = g.getObject(ability.getSourceId());
            candidates.add(new ActivationCandidate(source == null ? null : source.getName(),
                    ability.toString(), ability.getSourceId()));
        }
        return chooseActivationSource(candidates, name, text, setupId);
    }

    /** One live playable ability considered for a named activation: its
     * source object's name, the ability text XMage matches, and the source
     * object id the alias must carry. */
    static final class ActivationCandidate {
        final String sourceName;
        final String text;
        final UUID sourceId;

        ActivationCandidate(String sourceName, String text, UUID sourceId) {
            this.sourceName = sourceName;
            this.text = text;
            this.sourceId = sourceId;
        }
    }

    /**
     * Picks the source id a named activation's alias must carry. Prefer the
     * setup id itself when a live ability's source is that exact object (a
     * same-named permanent whose source is the setup object, which keeps
     * duplicate-name cards exact); otherwise the first live ability whose
     * text matches and whose source name is the ref's card. A card that
     * moved zones (a graveyard activation, a permanent cast from hand) has a
     * new source id, so the setup id no longer matches and the name picks it
     * out. Null when no live ability matches, so the caller keeps the setup
     * alias.
     */
    UUID chooseActivationSource(List<ActivationCandidate> candidates, String name, String text, UUID setupId) {
        UUID nameMatch = null;
        for (ActivationCandidate c : candidates) {
            if (!c.text.startsWith(text)) {
                continue;
            }
            if (setupId != null && c.sourceId.equals(setupId)) {
                return c.sourceId;
            }
            if (nameMatch == null && sourceNameIsNamed(c.sourceName, name)) {
                nameMatch = c.sourceId;
            }
        }
        return nameMatch;
    }

    private void queueAbilityTarget(TestPlayer p, String ref) {
        String key = ref + "~" + (++abilityAliasSeq);
        runCode("bind " + ref, turn, phase, p, (info, pl, g) -> bindPendingAbility(g, ref, key));
        addTarget(p, "@" + key);
    }

    private void bindPendingAbility(Game g, String ref, String key) {
        UUID controller = seat(refSeat(ref)).getId();
        String name = abilitySourceName(ref);
        List<StackObject> topFirst = new ArrayList<>();
        for (StackObject so : g.getStack()) {
            topFirst.add(so);
        }
        StackObject found = nthPending(topFirst, so -> so instanceof StackAbility
                && controller.equals(so.getControllerId())
                && abilitySourceIsNamed(((StackAbility) so).getSourceObject(g), name), refOrdinal(ref));
        if (found == null) {
            throw new IllegalArgumentException("ability ref " + ref + " is not on the stack");
        }
        for (int j = 0; j < seats; j++) {
            seat(j).addAlias(key, found.getId());
        }
    }

    private boolean abilitySourceIsNamed(mage.MageObject source, String name) {
        return source != null && sourceNameIsNamed(source.getName(), name);
    }

    /** Whether a live source object's name is the scenario ref's card, under
     * both spellings the driver carries (the card under test's XMage name,
     * and a split/Room object's front half), and under the bare name of a
     * Forge "<Name> Token" ref when the live source is XMage's bare-named
     * token (Empower's "Jace"). */
    private boolean sourceNameIsNamed(String live, String name) {
        if (live == null) {
            return false;
        }
        if (live.equals(xmageSpelling(name)) || gorgeSpellingRule(gorgeName, xmageName, live).equals(name)) {
            return true;
        }
        String bare = withoutTokenSuffix(name);
        return bare != null && live.equals(bare);
    }

    /** The name form XMage's attack/block command takes. Unlike a cast
     * target, the command does not accept the driver's "@" aliases, so it
     * must be the card name, with the legacy zero-based "<name>:<index>"
     * suffix for a duplicate ("p0:Grizzly Bears#2" -> "Grizzly Bears:1"). */
    private String combatName(String ref) {
        if (faceDownRefs.contains(ref)) {
            // Yarus's face-down Abzan Guide: named by the face-down test
            // command, which TestPlayer's attack/block search maps back to
            // the empty face-down name (EmptyNames). The scenario's only
            // face-down creature of that seat is the one it names.
            return faceDownCombatName();
        }
        String n = battlefieldName(ref);
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            int k = Integer.parseInt(ref.substring(hash + 1)) - 1;
            if (k > 0) {
                return n + ":" + k;
            }
        }
        return n;
    }

    /** TestPlayer's combat command for a face-down creature. */
    static String faceDownCombatName() {
        return mage.constants.EmptyNames.FACE_DOWN_CREATURE.getTestCommand();
    }

    /** One attack step's queued commands: the scenario refs they were
     * lowered from and the PlayerAction each command added. */
    static final class ExactAttackBatch {
        final int turn;
        final int seat;
        final int defenderSeat;
        final List<String> refs = new ArrayList<>();
        final List<PlayerAction> actions = new ArrayList<>();

        ExactAttackBatch(int turn, int seat, int defenderSeat) {
            this.turn = turn;
            this.seat = seat;
            this.defenderSeat = defenderSeat;
        }
    }

    /** One block step's queued [blocker, attacker] commands and actions. */
    private static final class ExactBlockBatch {
        final int turn;
        final int seat;
        final List<String> blockers = new ArrayList<>();
        final List<String> attackers = new ArrayList<>();
        final List<PlayerAction> actions = new ArrayList<>();

        ExactBlockBatch(int turn, int seat) {
            this.turn = turn;
            this.seat = seat;
        }
    }

    /**
     * Replace name-only attacker commands with exact declarations when a
     * command could name a different object than the recorded ref: the
     * controlling seat has a token and a card of one name, the step declares
     * a repeat of a name, a ref carries a later "#k" ordinal than its first
     * live copy, or the seat controls more than one live same-name permanent
     * (a tapped, summoning-sick or already-attacking duplicate is not in
     * XMage's "Name:k" rank, while the ref's ordinal counts every copy). It
     * also has to name the highest-index duplicate first, which reverses the
     * step's order; a band's leader and XMage's combat asks follow declaration
     * order.
     *
     * <p>Once a batch needs exact declarations, every ref of that batch that
     * resolves is declared exactly, in the step's own order, not just the
     * same-name group: declaring the group before a unique earlier attacker
     * would still put it first in XMage's declaration order and consume that
     * attacker's recorded answer. Only a ref that cannot be resolved (and is
     * not ambiguous) keeps its name command, for the superclass to consume.
     * This runs at the engine's declaration point, after a token created
     * earlier in the turn is on the battlefield.
     */
    private void applyExactAttackers(TestPlayer player, Game game) {
        for (java.util.Iterator<ExactAttackBatch> it = exactAttacks.iterator(); it.hasNext();) {
            ExactAttackBatch batch = it.next();
            if (batch.turn != game.getTurnNum() || !player.getId().equals(seat(batch.seat).getId())) {
                continue;
            }
            it.remove();
            Iterable<Permanent> battlefield = game.getBattlefield().getAllPermanents();
            Map<String, Permanent> exactByRef = new HashMap<>();
            for (String ref : batch.refs) {
                exactByRef.put(ref, combatPermanent(ref, battlefieldName(ref), player.getId(), game));
            }
            boolean exactNeeded = false;
            for (String ref : batch.refs) {
                String name = battlefieldName(ref);
                if (combatRefAmbiguous(name, player.getId(), battlefield)
                        || combatNameStale(ref, exactByRef.get(ref), name)
                        || (exactByRef.get(ref) != null
                                && combatNameUnreliable(ref, name, player.getId(), battlefield))) {
                    exactNeeded = true;
                    break;
                }
            }
            if (!exactNeeded) {
                // Every name command still names its recorded object: the
                // name commands keep the behaviour every other step has.
                continue;
            }
            for (int i = 0; i < batch.refs.size(); i++) {
                String ref = batch.refs.get(i);
                String name = battlefieldName(ref);
                Permanent exact = exactByRef.get(ref);
                if (exact != null && game.getCombat().getAttackers().contains(exact.getId())) {
                    // XMage's checkAttackRequirements declared this forced
                    // attacker itself; the recorded command is satisfied.
                    player.getActions().remove(batch.actions.get(i));
                    continue;
                }
                if (exact == null) {
                    if (combatRefAmbiguous(name, player.getId(), battlefield)) {
                        throw new IllegalArgumentException("attack ref " + ref + " is not on the battlefield");
                    }
                    // Genuinely unresolvable: leave its name command to the
                    // superclass rather than half-declaring the batch.
                    continue;
                }
                player.getActions().remove(batch.actions.get(i));
                player.declareAttacker(exact.getId(), seat(batch.defenderSeat).getId(), game, false);
                if (!game.getCombat().getAttackers().contains(exact.getId())) {
                    throw new IllegalArgumentException("attack ref " + ref + " was not declared (battlefield: "
                            + battlefieldListing() + ")");
                }
            }
        }
    }

    /**
     * The blocker counterpart of applyExactAttackers. Both sides of a
     * recorded [blocker, attacker] pair are resolved exactly and declared
     * directly instead of lowered to a name when either name command could
     * name the wrong object: the seat controls more than one same-name
     * permanent (a token beside a card, or a live duplicate whose rank the
     * command's filter shifts), or a ref carries a later "#k" ordinal than
     * its first live copy. The blocker command ranks every controlled
     * permanent of the name; the attacker command ranks only the currently
     * attacking ones, so a same-name non-attacker shifts its index too.
     */
    private void applyExactBlockers(TestPlayer player, Game game) {
        UUID attackerId = game.getCombat().getAttackingPlayerId();
        if (attackerId == null) {
            return;
        }
        for (java.util.Iterator<ExactBlockBatch> it = exactBlocks.iterator(); it.hasNext();) {
            ExactBlockBatch batch = it.next();
            if (batch.turn != game.getTurnNum() || !player.getId().equals(seat(batch.seat).getId())) {
                continue;
            }
            it.remove();
            for (int i = 0; i < batch.blockers.size(); i++) {
                String blockerRef = batch.blockers.get(i);
                String attackerRef = batch.attackers.get(i);
                String blockerName = battlefieldName(blockerRef);
                String attackerName = battlefieldName(attackerRef);
                Iterable<Permanent> battlefield = game.getBattlefield().getAllPermanents();
                Permanent blocker = combatPermanent(blockerRef, blockerName, player.getId(), game);
                Permanent attacker = combatPermanent(attackerRef, attackerName, attackerId, game);
                boolean exactNeeded = combatRefAmbiguous(blockerName, player.getId(), battlefield)
                        || combatRefAmbiguous(attackerName, attackerId, battlefield)
                        || combatNameStale(blockerRef, blocker, blockerName)
                        || combatNameStale(attackerRef, attacker, attackerName)
                        || (blocker != null
                                && combatNameUnreliable(blockerRef, blockerName, player.getId(), battlefield))
                        || (attacker != null
                                && combatNameUnreliable(attackerRef, attackerName, attackerId, battlefield));
                if (!exactNeeded) {
                    continue;
                }
                player.getActions().remove(batch.actions.get(i));
                if (blocker == null || attacker == null) {
                    throw new IllegalArgumentException("block " + blockerRef + " -> " + attackerRef
                            + " names no live permanent (battlefield: " + battlefieldListing() + ")");
                }
                mage.game.combat.CombatGroup group = game.getCombat().findGroup(attacker.getId());
                if (group != null && group.getBlockers().contains(blocker.getId())) {
                    // XMage already declared this pair from a block requirement.
                    continue;
                }
                player.declareBlocker(player.getId(), blocker.getId(), attacker.getId(), game);
                group = game.getCombat().findGroup(attacker.getId());
                if (group == null || !group.getBlockers().contains(blocker.getId())) {
                    throw new IllegalArgumentException("block " + blockerRef + " -> " + attackerRef
                            + " was not declared (battlefield: " + battlefieldListing() + ")");
                }
            }
        }
    }

    /** The exact permanent a recorded combat ref names, resolved from live
     * state. A token ref ranks its controller's live tokens by creation order
     * (tokenChoice). A non-token ref names the object the driver bound to the
     * ref: a card ref's `#k` is the setup/objRef ordinal ACROSS ZONES (the
     * second `X` in the deck is `p0:X#2` whether it sits in the graveyard or
     * the battlefield), not a rank among the controller's live battlefield
     * permanents, so only the bound object's identity can identify it -- its
     * own id, or the main card's id for a DFC whose permanent is a front half
     * (boundPermanent). `name` is the XMage spelling from battlefieldName;
     * `boundId` is the id bound to a non-token ref (null when the driver never
     * bound one). */
    static Permanent combatPermanent(String ref, String name, UUID controller, UUID boundId,
            Iterable<Permanent> battlefield) {
        if (ref.contains(":token:")) {
            Permanent token = tokenChoice(ref, controller, battlefield);
            if (token == null && !name.equals(refName(ref))) {
                // A token copy of a renamed card under test carries the
                // XMage spelling, not the scenario ref's spelling.
                token = tokenChoice(ref, name, controller, battlefield);
            }
            return token;
        }
        Permanent bound = boundPermanent(boundId, battlefield);
        return bound != null && controller.equals(bound.getControllerId()) ? bound : null;
    }

    /** The live permanent a setup-bound object id names, or null. The id is
     * usually the permanent's own, but a double-faced card's alias binds the
     * MAIN card while the permanent XMage makes is its front half: a DFC cast
     * from hand enters as a half whose id differs from the hand card's id, and
     * a transform keeps that half permanent (CR 712.8, 701.28). Matching the
     * permanent's main-card id as well is what makes a card cast during the
     * scenario resolve from the ref that named it in hand, and the next
     * zone-change or transform cannot break it: {@code getMainCard()} follows
     * the physical card, not the current face. */
    static Permanent boundPermanent(UUID boundId, Iterable<Permanent> battlefield) {
        if (boundId == null) {
            return null;
        }
        for (Permanent permanent : battlefield) {
            if (!permanent.isToken() && permanent.getId().equals(boundId)) {
                return permanent;
            }
        }
        for (Permanent permanent : battlefield) {
            if (permanent.isToken()) {
                continue;
            }
            Card main = permanent.getMainCard();
            if (main != null && main.getId().equals(boundId)) {
                return permanent;
            }
        }
        return null;
    }

    /** Whether the name command lowered for a ref no longer names its live
     * permanent: a double-faced permanent that transformed mid-scenario
     * (Ulvenwald Captive's "{5}{G}{G}: Transform" resolved before combat,
     * CR 712.8, 701.28) keeps its object, but now carries the back face's
     * name, and TestPlayer's name-only attack/block command, queued at
     * script time with the front name, finds nothing. The ref still names
     * the same object, so the batch declares it exactly. A face-down
     * creature is named by its own test command, and a token ref by
     * tokenChoice's own name rule; neither is stale here. */
    boolean combatNameStale(String ref, Permanent live, String name) {
        // faceDownRefs is null in a driver test that builds the replay
        // without its constructor (ScenarioReplaySplitRoomTest).
        return live != null && !ref.contains(":token:") && (faceDownRefs == null || !faceDownRefs.contains(ref))
                && !CardUtil.haveSameNames(xmageSpelling(live.getName()), name, true);
    }

    /** combatPermanent for a live game: resolve a non-token ref through the
     * object id registerAliases (or bindOneAlias) bound to it, so a `#k`
     * setup ordinal is never mistaken for a live battlefield rank. A token
     * ref keeps tokenChoice's live creation order, where `#k` IS a live
     * rank. */
    private Permanent combatPermanent(String ref, String name, UUID controller, Game game) {
        UUID boundId = null;
        if (!ref.contains(":token:")) {
            String alias = refAlias.get(ref);
            if (alias != null) {
                boundId = seat(refSeat(ref)).getAliasByName(alias);
            }
        }
        return combatPermanent(ref, name, controller, boundId, game.getBattlefield().getAllPermanents());
    }

    /** Whether XMage's name-only combat command cannot tell this name's
     * token(s) and card(s) apart: the seat controls both kinds under it. The
     * rule is by kind, not a list of cards, so it covers a token copy of a
     * card, a card created beside a same-name token, attackers and blockers. */
    static boolean combatRefAmbiguous(String name, UUID controller, Iterable<Permanent> battlefield) {
        boolean token = false;
        boolean card = false;
        for (Permanent permanent : battlefield) {
            if (!controller.equals(permanent.getControllerId())
                    || !CardUtil.haveSameNames(permanent.getName(), name, true)) {
                continue;
            }
            if (permanent.isToken()) {
                token = true;
            } else {
                card = true;
            }
        }
        return token && card;
    }

    /** Whether XMage's name-only combat command may name a different object
     * than the recorded ref. The command ranks the permanents that currently
     * match its own filter: the attack command ranks untapped, non-sick
     * creatures of the name, while a block's attacker side ranks only the
     * currently attacking ones. The driver's "#k" is instead the scenario
     * object's setup ordinal across zones. The two ranks agree only for the
     * first ordinal when the seat controls exactly one live permanent of the
     * name; a duplicate (tapped, already attacking, phased out, or on the
     * token/card side of the same name) shifts XMage's index, and a later
     * ordinal can outlive the earlier copies. Resolving the ref to its bound
     * object is the only exact answer, so the caller declares it directly. */
    static boolean combatNameUnreliable(String ref, String name, UUID controller,
            Iterable<Permanent> battlefield) {
        if (refOrdinal(ref) > 1) {
            return true;
        }
        int sameName = 0;
        for (Permanent permanent : battlefield) {
            if (controller.equals(permanent.getControllerId())
                    && CardUtil.haveSameNames(permanent.getName(), name, true)
                    && ++sameName > 1) {
                return true;
            }
        }
        return false;
    }

    /** Whether an activation's XMage text turns its permanent face up
     * (Morph/Megamorph/Disguise's special action, "Turn face up"). */
    static boolean turnsFaceUp(String abilityText) {
        return abilityText != null && abilityText.toLowerCase(java.util.Locale.ROOT).contains("face up");
    }

    /** The attack commands in the order XMage can consume them. TestPlayer
     * finds a "Name:k" attacker as the k-th UNTAPPED creature of that name
     * (FilterCreatureForCombat), and declaring an attacker taps it at once
     * (Combat.declareAttacker), so after "Goblin Token" is declared
     * "Goblin Token:1" names a creature that is no longer there and the
     * command is left over. Declaring same-name duplicates highest index
     * first never shifts a lower index, with or without vigilance. Each
     * name's commands keep the positions that name held; other names are
     * untouched. */
    static List<String> attackCommandOrder(List<String> combatNames) {
        Map<String, List<Integer>> positions = new java.util.LinkedHashMap<>();
        for (int i = 0; i < combatNames.size(); i++) {
            positions.computeIfAbsent(combatBaseName(combatNames.get(i)), k -> new ArrayList<>()).add(i);
        }
        List<String> out = new ArrayList<>(combatNames);
        for (List<Integer> at : positions.values()) {
            List<String> group = new ArrayList<>();
            for (int i : at) {
                group.add(combatNames.get(i));
            }
            group.sort(java.util.Comparator.comparingInt(ScenarioReplay::combatIndex).reversed());
            for (int k = 0; k < at.size(); k++) {
                out.set(at.get(k), group.get(k));
            }
        }
        return out;
    }

    /** Queue one attack step's name commands and build its exact batch.
     * The commands are queued in {@link #attackCommandOrder} because a
     * "Name:k" command ranks untapped creatures and declaring the first copy
     * removes it; the batch's refs stay in the step's own order, which is the
     * order a band's leader and XMage's combat asks follow. Static so the
     * driver test can pin both orders without a player. */
    static ExactAttackBatch lowerAttackBatch(int turn, int seat, int defenderSeat,
            List<String> keptNames, List<String> keptRefs,
            java.util.function.Function<String, PlayerAction> queue) {
        ExactAttackBatch exact = new ExactAttackBatch(turn, seat, defenderSeat);
        PlayerAction[] actions = new PlayerAction[keptRefs.size()];
        boolean[] queued = new boolean[keptNames.size()];
        for (String name : attackCommandOrder(keptNames)) {
            // The ordered names are a permutation of keptNames, so each name
            // still has one unqueued position; duplicates are interchangeable
            // here because the batch resolves the ref exactly.
            int at = -1;
            for (int i = 0; i < keptNames.size(); i++) {
                if (!queued[i] && keptNames.get(i).equals(name)) {
                    at = i;
                    break;
                }
            }
            if (at < 0) {
                throw new IllegalStateException("no attack ref lowered to command " + name);
            }
            queued[at] = true;
            actions[at] = queue.apply(name);
        }
        for (int i = 0; i < keptRefs.size(); i++) {
            exact.refs.add(keptRefs.get(i));
            exact.actions.add(actions[i]);
        }
        return exact;
    }

    /** "Goblin Token:1" -> "Goblin Token"; a name with no index unchanged. */
    static String combatBaseName(String combatName) {
        int c = combatName.lastIndexOf(':');
        return c >= 0 && combatName.substring(c + 1).matches("[0-9]+") ? combatName.substring(0, c) : combatName;
    }

    /** "Goblin Token:1" -> 1; a name with no index is index 0. */
    static int combatIndex(String combatName) {
        int c = combatName.lastIndexOf(':');
        return c >= 0 && combatName.substring(c + 1).matches("[0-9]+") ? Integer.parseInt(combatName.substring(c + 1)) : 0;
    }

    /** Whether every named attacker is a card XMage forces to attack (an
     * AttacksEachCombatStaticAbility), so checkAttackRequirements declares and
     * taps it before selectAttackers, which then skips the queued attack
     * command on its first pass. Anything that is not such a card means the
     * ordinary attack() path must run. */
    private boolean allMustAttack(List<String> attackers) {
        for (String ref : attackers) {
            if (!isMustAttackName(combatName(ref))) {
                return false;
            }
        }
        return true;
    }

    /** The attack command names left once every must-attack name is dropped
     * (XMage's checkAttackRequirements declares those itself). Order is kept;
     * a duplicate's ":k" index is read through its base name. */
    static List<String> withoutMustAttack(List<String> combatNames, java.util.function.Predicate<String> mustAttack) {
        List<String> out = new ArrayList<>();
        for (String n : combatNames) {
            if (!mustAttack.test(combatBaseName(n))) {
                out.add(n);
            }
        }
        return out;
    }

    /** Whether the named card carries XMage's AttacksEachCombatStaticAbility.
     * Structural and name-based, the way spellAbility and isSpreeCard resolve
     * a name through CardRepository, so a card printed after this driver is
     * covered by what it actually is, not a list of names. */
    private static boolean isMustAttackName(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return isMustAttackCard(c);
    }

    /** Card-based and package-private so a contract test can pin it without
     * the card repository (which needs H2). */
    static boolean isMustAttackCard(Card c) {
        return c != null && c.getAbilities().containsClass(AttacksEachCombatStaticAbility.class);
    }

    /** Forget a cast whose spell has left the stack, so a later cast that
     * names it falls through to the setup alias naming its permanent. */
    private void dropLastCast() {
        if (!cast.isEmpty()) {
            cast.remove(cast.size() - 1);
        }
    }

    /** Every battlefield permanent, "seat:name", for a failed command's
     * diagnosis. controllerSeat(g) resolves a controller id to the seat it
     * plays; a controller the two seats do not cover is named by its id. */
    private String battlefieldListing() {
        if (currentGame == null) {
            return "no game";
        }
        java.util.List<String> out = new ArrayList<>();
        for (Permanent perm : currentGame.getBattlefield().getAllPermanents()) {
            String seat = perm.getControllerId() == playerA.getId() ? "p0"
                    : perm.getControllerId() == playerB.getId() ? "p1"
                    : String.valueOf(perm.getControllerId());
            out.add(seat + ":" + perm.getName());
        }
        return out.isEmpty() ? "empty" : String.join(", ", out);
    }

    /** The actions still queued on either seat at a failed execute(): the
     * asks XMage never reached, named so a replay row says which of the
     * scenario's steps dangled. */
    private String leftoverActionNames() {
        java.util.List<String> out = new ArrayList<>();
        for (TestPlayer p : new TestPlayer[]{playerA, playerB}) {
            Player real = currentGame == null ? null : currentGame.getPlayer(p.getId());
            if (!(real instanceof TestPlayer)) {
                continue;
            }
            String seat = p == playerA ? "p0" : "p1";
            for (PlayerAction a : ((TestPlayer) real).getActions()) {
                out.add(seat + " " + a.getActionName() + " (" + a.getAction() + ")");
            }
        }
        return out.isEmpty() ? "none" : String.join("; ", out);
    }

    /** Front-name setup refs keep their identity; name-based battlefield
     * commands instead need the face setup actually placed on that seat. */
    private String battlefieldName(String ref) {
        String n = xmageSpelling(refName(ref));
        int colon = ref.indexOf(':');
        if (colon >= 0 && ref.substring(0, colon).matches("p[0-9]+")) {
            return backFaceNames.getOrDefault(ref.substring(0, colon + 1) + n, n);
        }
        return n;
    }

    /** "p1:token:Name#2" -> "Name". */
    static String refName(String ref) {
        // Only a leading seat ref ("p0:") is a prefix; a card name may carry
        // its own colon ("Summon: Bahamut").
        int i = ref.indexOf(':');
        String n = i >= 0 && ref.substring(0, i).matches("p[0-9]+") ? ref.substring(i + 1) : ref;
        if (n.startsWith("token:")) {
            n = n.substring("token:".length());
        }
        int j = n.lastIndexOf('#');
        if (j >= 0 && n.substring(j + 1).matches("[0-9]+")) {
            n = n.substring(0, j);
        }
        return n;
    }

    private static final Ability MANA_SOURCE = new SimpleStaticAbility(Zone.ALL, new InfoEffect("oracle scenario mana"));

    private static void addPool(Player pl, Game g, String letters) {
        Mana m = new Mana();
        for (char c : letters.toUpperCase().toCharArray()) {
            switch (c) {
                case 'W': m.increaseWhite(); break;
                case 'U': m.increaseBlue(); break;
                case 'B': m.increaseBlack(); break;
                case 'R': m.increaseRed(); break;
                case 'G': m.increaseGreen(); break;
                case 'C': m.increaseColorless(); break;
                default: throw new IllegalArgumentException("mana letter " + c);
            }
        }
        pl.getManaPool().addMana(m, g, MANA_SOURCE);
    }

    // ---- snapshot ----------------------------------------------------------

    private int seatIndex(Game g, UUID id) {
        if (id == null) {
            return -1;
        }
        if (seats == 3 && id.equals(playerC.getId())) {
            return 2;
        }
        return id.equals(playerA.getId()) ? 0 : id.equals(playerB.getId()) ? 1 : -1;
    }

    /** The player who receives priority after {@code seat} passes: the next
     * in-game seat in the game's own turn order (CR 117.3d), derived from
     * GameState's player list so a reversed turn order and a leaver are
     * handled the way the engine handles them. Null when nobody is left. */
    private UUID nextPriorityPlayer(Game g, int seat) {
        Player next = g.getState().getPlayerList(seat(seat).getId()).getNext(g, false);
        return next == null ? null : next.getId();
    }

    /** The permanent's keyword names, sorted and de-duplicated. Spec H5
     * (confirmed against the XMage source): a keyword ability's getRule() is
     * its lower-case name ("flying", "first strike"), which the comparator
     * case-folds onto its evergreen set (oracle/compliance/oraclediff/keywords.go).
     * Some rules carry HTML-wrapped reminder text ("menace <i>(This creature
     * can't be blocked ...)</i>", MenaceAbility.showAbilityHint), so tags are
     * stripped and the rule is cut at the first "(". getAbilities(g) includes
     * abilities gained from continuous effects, so a granted keyword shows
     * here. Only classes of mage.abilities.keyword count, so
     * triggered/activated/static rule text never reaches the list. */
    private static List<String> keywordNames(Permanent perm, Game g) {
        java.util.TreeSet<String> names = new java.util.TreeSet<>();
        for (Ability a : perm.getAbilities(g)) {
            if (!a.getClass().getName().startsWith("mage.abilities.keyword.")) {
                continue;
            }
            String rule = a.getRule().replaceAll("<[^>]*>", "");
            int paren = rule.indexOf("(");
            if (paren >= 0) {
                rule = rule.substring(0, paren);
            }
            rule = rule.trim();
            if (!rule.isEmpty()) {
                names.add(rule);
            }
        }
        return new ArrayList<>(names);
    }

    private JsonObject snapshot(String checkpoint, Game g) {
        return snapshot(checkpoint, g, null, false);
    }

    /** The checkpoint's snapshot. {@code holderAfterStep}, when a pass
     * checkpoint is captured before XMage moves the priority, names the seat
     * that receives it (CR 117.3d); every other snapshot reads the game's
     * current holder. Both fields that report the holder -- priority and
     * offered -- read this one value, so they cannot disagree unless the
     * checkpoint is a pending declaration: {@code declarationPending} marks
     * the pass into declare-attackers/declare-blockers, where the declaration
     * is a turn-based action before anyone has priority (CR 508.1, 509.1,
     * 117.3a). gorge's snapshot offers nothing for a non-priority pending
     * decision, so this checkpoint does too even though XMage still names the
     * active player. */
    private JsonObject snapshot(String checkpoint, Game g, UUID holderAfterStep, boolean declarationPending) {
        lastCheckpointStack = stackNames(g);
        UUID holder = holderAfterStep != null && !g.hasEnded() ? holderAfterStep : g.getPriorityPlayerId();
        JsonObject s = new JsonObject();
        s.addProperty("checkpoint", checkpoint);
        s.addProperty("turn", g.getTurnNum());
        s.addProperty("step", g.getTurnStepType() == null ? "" : g.getTurnStepType().name());
        s.addProperty("active", seatIndex(g, g.getActivePlayerId()));
        s.addProperty("priority", seatIndex(g, holder));
        s.addProperty("over", g.hasEnded());
        JsonArray players = new JsonArray();
        for (int i = 0; i < seats; i++) {
            Player p = g.getPlayer(seat(i).getId());
            JsonObject po = new JsonObject();
            po.addProperty("seat", i);
            po.addProperty("life", p.getLife());
            JsonObject pc = new JsonObject();
            for (Counter c : p.getCountersAsCopy().values()) {
                pc.addProperty(c.getName(), c.getCount());
            }
            if (pc.size() > 0) {
                po.add("counters", pc);
            }
            po.add("hand", sortedNames(p.getHand().getCards(g)));
            JsonArray gy = new JsonArray();
            for (Card c : p.getGraveyard().getCards(g)) {
                gy.add(c.getName());
            }
            po.add("graveyard", gy);
            List<String> ex = new ArrayList<>();
            for (Card c : g.getExile().getAllCards(g)) {
                if (p.getId().equals(c.getOwnerId())) {
                    ex.add(c.getName());
                }
            }
            Collections.sort(ex);
            po.add("exile", GSON.toJsonTree(ex));
            po.add("command", new JsonArray());
            po.addProperty("library_count", p.getLibrary().size());
            JsonArray top = new JsonArray();
            List<Card> lib = p.getLibrary().getCards(g);
            for (int k = 0; k < lib.size() && k < 5; k++) {
                top.add(lib.get(k).getName());
            }
            po.add("library_top", top);
            po.addProperty("pool", pool(p.getManaPool()));
            if (seats > 2 && p.hasLost()) {
                // CR 800.4a: only a multiplayer game goes on past a loss, so
                // only its snapshots carry the flag (gorge's OracleSnapPlayer.Lost).
                po.addProperty("lost", true);
            }
            players.add(po);
        }
        s.add("players", players);
        JsonArray perms = new JsonArray();
        // CR 702.26b: a phased-out permanent is treated as though it does not
        // exist. XMage keeps it on the battlefield so phasing can return it,
        // and getAllPermanents() lists it; the snapshot must walk the active
        // list, as gorge's oracle snapshot and Forge's battlefield do. Every
        // perms field -- counters, attached_to and the rest -- is written from
        // this one loop, so none of a phased-out permanent reaches the
        // snapshot.
        for (Permanent perm : g.getBattlefield().getAllActivePermanents()) {
            JsonObject o = new JsonObject();
            o.addProperty("name", perm.getName());
            o.addProperty("controller", seatIndex(g, perm.getControllerId()));
            o.addProperty("owner", seatIndex(g, perm.getOwnerId()));
            o.addProperty("token", perm.isToken());
            o.addProperty("tapped", perm.isTapped());
            o.addProperty("face_down", perm.isFaceDown(g));
            if (perm.isCreature(g)) {
                o.addProperty("pt", perm.getPower().getValue() + "/" + perm.getToughness().getValue());
            }
            o.addProperty("damage", perm.getDamage());
            JsonObject cs = new JsonObject();
            for (Counter c : perm.getCounters(g).values()) {
                cs.addProperty(c.getName(), c.getCount());
            }
            if (cs.size() > 0) {
                o.add("counters", cs);
            }
            List<String> types = new ArrayList<>();
            for (CardType t : perm.getCardType(g)) {
                types.add(t.toString());
            }
            perm.getSubtype(g).forEach(st -> types.add(st.toString()));
            if (perm.isAllNonbasicLandTypes(g)) {
                // "Is every nonbasic land type" (CR 205.3i) is a flag on the
                // permanent's subtype state, not a materialised subtype, so
                // getSubtype above never lists the granted types. Report the
                // whole enum set the flag grants, derived from the enum so a
                // new land type joins without a list to edit here.
                for (SubType st : SubType.getNonbasicLandTypes()) {
                    String name = st.toString();
                    if (!types.contains(name)) {
                        types.add(name);
                    }
                }
            }
            perm.getSuperType(g).forEach(st -> types.add(st.toString()));
            Collections.sort(types);
            o.add("types", GSON.toJsonTree(types));
            if (perm.isAllCreatureTypes(g)) {
                // Changeling / "is every creature type": the type list above
                // is the printed subtypes only, so the comparator needs this
                // marker to see the all-types state.
                o.addProperty("all_creature_types", true);
            }
            o.addProperty("colors", perm.getColor(g).toString());
            o.add("keywords", GSON.toJsonTree(keywordNames(perm, g)));
            if (perm.getAttachedTo() != null) {
                Permanent to = g.getPermanent(perm.getAttachedTo());
                if (to != null) {
                    o.addProperty("attached_to", to.getName());
                } else {
                    int si = seatIndex(g, perm.getAttachedTo());
                    o.addProperty("attached_to", si >= 0 ? "p" + si : "?");
                }
            }
            o.addProperty("attacking", perm.isAttacking());
            o.addProperty("blocking", perm.getBlocking() > 0);
            perms.add(o);
        }
        s.add("permanents", perms);
        JsonArray stack = new JsonArray();
        for (StackObject so : g.getStack()) { // top first
            JsonObject o = new JsonObject();
            o.addProperty("kind", so instanceof Spell ? "spell" : "ability");
            String src = so.getName();
            if (!(so instanceof Spell)) {
                mage.MageObject mo = g.getObject(so.getSourceId());
                src = mo == null ? "?" : abilitySourceName(mo, ((StackAbility) so).getStackAbility());
            }
            o.addProperty("source", src);
            o.addProperty("controller", seatIndex(g, so.getControllerId()));
            stack.add(o);
        }
        s.add("stack", stack);
        if (wantsOffered()) {
            s.add("offered", declarationPending ? new JsonArray() : offered(g, holder));
        }
        return (JsonObject) gorgeSpellings(s);
    }

    /** Whether the scenario opts the "offered" field into the comparison
     * (Item.Compare). Only then do the snapshots carry it, so every other
     * scenario's wire bytes stay what they were. */
    private boolean wantsOffered() {
        if (sc0 == null || !sc0.has("compare") || !sc0.get("compare").isJsonArray()) {
            return false;
        }
        for (JsonElement e : sc0.getAsJsonArray("compare")) {
            if (e.isJsonPrimitive() && e.getAsString().equals("offered")) {
                return true;
            }
        }
        return false;
    }

    /**
     * What the player holding priority -- or, at a pass checkpoint XMage has
     * not yet handed on, the seat that receives it -- may play, in gorge's
     * snapshot vocabulary (rules.OracleSnapOffered): the source's scenario
     * ref, the kind ("cast", "play" for a land, otherwise "activate" -- mana
     * abilities included, CR 605.3a: a mana ability is activatable whenever
     * its controller has priority), and the ability's rule text. The comparer
     * strips the display prefixes. {@code holder} is the snapshot's one
     * priority-holder value; null (the game ended, or the checkpoint is a
     * skipped-step label recorded after the turn) offers nothing.
     */
    private JsonArray offered(Game g, UUID holder) {
        JsonArray out = new JsonArray();
        if (holder == null || g.hasEnded()) {
            return out;
        }
        Player pr = g.getPlayer(holder);
        if (pr == null) {
            return out;
        }
        List<? extends Ability> playable = playableKeepingDuplicates(pr, g);
        for (Ability a : playable) {
            mage.MageObject src = g.getObject(a.getSourceId());
            if (src == null) {
                continue;
            }
            JsonObject o = new JsonObject();
            o.addProperty("source", offeredRef(g, a, src));
            String kind = "activate";
            if (a instanceof mage.abilities.PlayLandAbility) {
                kind = "play";
            } else if (a instanceof mage.abilities.SpellAbility) {
                kind = "cast";
            }
            o.addProperty("kind", kind);
            o.addProperty("label", a.toString());
            out.add(o);
        }
        return out;
    }

    /**
     * What {@code pr} may play, with identical abilities kept. The public
     * {@code getPlayable(g, true)} hides an ability whose rule text an earlier
     * object already listed ("first instance only", an AI convenience), so
     * identical mana abilities on different sources -- four Plains beside
     * Adagia's own "{T}: Add {W}" -- would list one source and drop the rest.
     * Each source's ability is its own offer (CR 605.3a), so go to the
     * PlayerImpl behind the TestPlayer wrapper and ask for duplicates kept.
     */
    static List<? extends Ability> playableKeepingDuplicates(Player pr, Game g) {
        Player impl = pr instanceof TestPlayer ? ((TestPlayer) pr).getComputerPlayer() : pr;
        if (impl instanceof mage.players.PlayerImpl) {
            return ((mage.players.PlayerImpl) impl).getPlayable(g, true, mage.constants.Zone.ALL, false);
        }
        return pr.getPlayable(g, true);
    }

    /** The scenario ref of an offered ability's source: the setup alias of
     * that object when one exists (duplicate names keep their "#n"), else
     * "pN:name" by its controller (owner, for a card outside the battlefield). */
    private String offeredRef(Game g, Ability a, mage.MageObject src) {
        UUID who = a.getControllerId();
        if (src instanceof Card && !(src instanceof Permanent) && ((Card) src).getOwnerId() != null) {
            who = ((Card) src).getOwnerId();
        }
        String name = setupName(src);
        String best = null;
        for (java.util.Map.Entry<String, UUID> en : seat(0).getAliases().entrySet()) {
            String key = en.getKey();
            if (en.getValue().equals(src.getId()) && isScenarioRef(key) && refName(key).equals(name)
                    && (best == null || key.compareTo(best) < 0)) {
                best = key;
            }
        }
        return best != null ? best : "p" + seatIndex(g, who) + ":" + name;
    }

    private static JsonArray sortedNames(java.util.Collection<Card> cs) {
        List<String> ns = new ArrayList<>();
        for (Card c : cs) {
            ns.add(c.getName());
        }
        Collections.sort(ns);
        JsonArray a = new JsonArray();
        ns.forEach(a::add);
        return a;
    }

    // ManaPool.getWhite() and the other colour getters sum only unconditional
    // pool items: a restricted add ("spend only on Dragon spells") is a
    // ConditionalMana whose ManaPoolItem keeps its plain counters at zero. Add
    // those in too, so the snapshot matches gorge's letters-only pool. The
    // restriction itself is not compared.
    static String pool(ManaPool mp) {
        int w = mp.getWhite();
        int u = mp.getBlue();
        int bl = mp.getBlack();
        int r = mp.getRed();
        int g = mp.getGreen();
        int c = mp.getColorless();
        for (ConditionalMana cm : mp.getConditionalMana()) {
            w += cm.getWhite();
            u += cm.getBlue();
            bl += cm.getBlack();
            r += cm.getRed();
            g += cm.getGreen();
            // ManaPool.addMana folds generic into colorless for plain mana.
            c += cm.getColorless() + cm.getGeneric();
        }
        StringBuilder b = new StringBuilder();
        rep(b, 'W', w);
        rep(b, 'U', u);
        rep(b, 'B', bl);
        rep(b, 'R', r);
        rep(b, 'G', g);
        rep(b, 'C', c);
        return b.toString();
    }

    private static void rep(StringBuilder b, char c, int n) {
        for (int i = 0; i < n; i++) {
            b.append(c);
        }
    }

    private static String str(JsonObject o, String k) {
        return o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsString() : "";
    }
}
