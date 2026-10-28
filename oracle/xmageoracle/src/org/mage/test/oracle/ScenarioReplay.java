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
import mage.abilities.common.AttacksEachCombatStaticAbility;
import mage.abilities.common.SimpleStaticAbility;
import mage.abilities.mana.ActivatedManaAbilityImpl;
import mage.abilities.costs.AlternativeSourceCosts;
import mage.abilities.costs.OptionalAdditionalSourceCosts;
import mage.abilities.costs.OrCost;
import mage.abilities.keyword.LeylineAbility;
import mage.abilities.keyword.SpreeAbility;
import mage.abilities.costs.mana.ManaCost;
import mage.cards.repository.CardInfo;
import mage.cards.repository.CardRepository;
import mage.abilities.effects.common.EndTurnEffect;
import mage.abilities.effects.common.InfoEffect;
import mage.cards.Card;
import mage.cards.Cards;
import mage.cards.CardsImpl;
import mage.cards.DoubleFacedCard;
import mage.constants.CardType;
import mage.constants.ManaType;
import mage.constants.Outcome;
import mage.constants.PhaseStep;
import mage.constants.Zone;
import mage.counters.Counter;
import mage.counters.CounterType;
import mage.counters.Counters;
import mage.game.FreeForAll;
import mage.game.Game;
import mage.game.GameException;
import mage.game.PutToBattlefieldInfo;
import mage.game.events.GameEvent;
import mage.game.permanent.Permanent;
import mage.game.stack.Spell;
import mage.game.stack.StackAbility;
import mage.game.stack.StackObject;
import mage.players.ManaPool;
import mage.util.RandomUtil;
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
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.UUID;
import java.util.function.BiPredicate;

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
    private static final PhaseStep MAIN = PhaseStep.PRECOMBAT_MAIN;

    private final List<JsonObject> snaps = new ArrayList<>();
    // The step a cast/resolve/checkpoint is registered at. MAIN until an
    // attack or block op moves the scenario into combat; gorge plays the
    // cast in the declare-attackers (attacking-only) or declare-blockers
    // (blocking) step, so the driver must too.
    private PhaseStep phase = MAIN;

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
        SETUP_TOKENS.put("b_2_2_zombie", mage.game.permanent.token.ZombieToken::new);
        SETUP_TOKENS.put("b_2_2_zombie_druid", mage.game.permanent.token.ZombieDruidToken::new);
        SETUP_TOKENS.put("c_0_0_a_thopter_flying", mage.game.permanent.token.Thopter00ColorlessToken::new);
        SETUP_TOKENS.put("c_1_1_a_drone_flying_blockflying", mage.game.permanent.token.DroneFlyingToken::new);
        SETUP_TOKENS.put("c_1_1_a_servo", mage.game.permanent.token.ServoToken::new);
        SETUP_TOKENS.put("c_1_1_a_thopter_flying", mage.game.permanent.token.ThopterColorlessToken::new);
        SETUP_TOKENS.put("c_1_1_pilot_crewbuff", mage.game.permanent.token.PilotCrewToken::new);
        SETUP_TOKENS.put("c_1_1_pilot_crewsaddlebuff", mage.game.permanent.token.PilotSaddleCrewToken::new);
        SETUP_TOKENS.put("c_1_1_sliver", mage.game.permanent.token.SliverToken::new);
        SETUP_TOKENS.put("c_2_2_a_robot", mage.game.permanent.token.RobotToken::new);
        SETUP_TOKENS.put("c_3_2_a_vehicle_crew_1", mage.game.permanent.token.VehicleToken::new);
        SETUP_TOKENS.put("c_a_clue_draw", mage.game.permanent.token.ClueArtifactToken::new);
        SETUP_TOKENS.put("c_a_food_sac", mage.game.permanent.token.FoodToken::new);
        SETUP_TOKENS.put("c_a_lander_sac_search", mage.game.permanent.token.LanderToken::new);
        SETUP_TOKENS.put("c_a_treasure_sac", mage.game.permanent.token.TreasureToken::new);
        SETUP_TOKENS.put("c_e_shard_draw", mage.game.permanent.token.ShardToken::new);
        SETUP_TOKENS.put("g_1_1_elf_warrior", mage.game.permanent.token.ElfWarriorToken::new);
        SETUP_TOKENS.put("g_2_2_spider_reach", mage.game.permanent.token.Spider22Token::new);
        SETUP_TOKENS.put("g_3_3_elephant", mage.game.permanent.token.ElephantToken::new);
        SETUP_TOKENS.put("r_1_1_goblin", mage.game.permanent.token.GoblinToken::new);
        SETUP_TOKENS.put("r_1_1_gremlin", mage.game.permanent.token.Gremlin11Token::new);
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
                    placeSetupTokens(this);
                    placeSetupAttached(this);
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
                placeSetupTokens(this);
                placeSetupAttached(this);
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
    // A cast step with cast_mode "alternative": gorge elected the first
    // alternative cost (Leyline of Mutation's grant). XMage poses that as its
    // alternative-cost key menu; the generator's leading "Yes" picks key 1.
    private boolean alternativeCostCast = false;
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
                String next = nextPaymentColour(unpaid);
                if (next != null) {
                    // TestPlayer.playMana consumes choices.get(0). This step's
                    // own answers (a resolution Yes/No, [choice_skip], a target
                    // pick) are already queued there and belong after the
                    // payment, so put the payment answer IN FRONT of them.
                    // Appending made playMana eat the step's answer and assert
                    // "Can't use choice in play mana: ...".
                    getChoices().add(0, next);
                } else {
                    fallBackToAutoPayment();
                }
            }
            return super.playMana(ability, unpaid, promptText, game);
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

        private String nextPaymentColour(ManaCost unpaid) {
            Mana due = unpaid.getMana();
            // A specific pip can only be paid by its own type. Prefer the
            // recorded unit of that type, but the plan is a preference, not a
            // restriction: a dual land's chosen colour or a plan written before
            // a restriction lifted can leave the pool holding the type while
            // the plan names another, and refusing it would strand the cast.
            for (char c : "WUBRGC".toCharArray()) {
                ManaType type = manaType(c);
                if (due.get(type) > 0 && getManaPool().get(type) > 0) {
                    return payment.indexOf(c) >= 0 ? takePayment(c) : dropPaymentLetterAndName(c);
                }
            }
            // A coloured pip the pool does not hold is not payable with any
            // other colour: leave it to auto-payment (which may float a new
            // source) instead of spending a unit the cost cannot take.
            if (hasColouredPip(due)) {
                return null;
            }
            // Only the generic part remains, and every unit pays it. Gorge's
            // recorded multiset names the units it spent, but the replayed pool
            // can hold the same COUNT in other colours, so the plan's remaining
            // letters need not name a unit at all. Take any pool unit, keeping
            // the plan's total in step.
            for (char c : "WUBRGC".toCharArray()) {
                if (getManaPool().get(manaType(c)) > 0) {
                    return payment.indexOf(c) >= 0 ? takePayment(c) : dropPaymentLetterAndName(c);
                }
            }
            return null;
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
         * aligned with the units still to spend. */
        private String dropPaymentLetterAndName(char c) {
            if (payment != null && !payment.isEmpty()) {
                payment = payment.substring(1);
            }
            return colourName(c);
        }

        private String takePayment(char c) {
            int i = payment.indexOf(c);
            payment = payment.substring(0, i) + payment.substring(i + 1);
            return colourName(c);
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

        private boolean scriptedYesNoNext() {
            return !getChoices().isEmpty() && (getChoices().get(0).equals("Yes") || getChoices().get(0).equals("No"));
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
         * Every other ask stays with the scripted queue.
         */
        @Override
        public boolean chooseUse(Outcome outcome, String message, String secondMessage, String trueText, String falseText, Ability source, Game game) {
            if (source instanceof LeylineAbility) {
                return false;
            }
            if (owner.copyCostCast != null && askedBy(owner.copyCostCast, "addOptionalAdditionalCosts")) {
                // The step elected this copy cost: pay it (the ask is the
                // keyword's own, so no other optional cost is answered here).
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
            return super.chooseUse(outcome, message, secondMessage, trueText, falseText, source, game);
        }

        /** An "alternative" cast's menu: AbilityImpl's alternative-cost key
         * choice ("You may choose an alternative cost", keys "1".. per
         * alternative, then "Cast with no alternative cost"). */
        @Override
        public boolean choose(Outcome outcome, mage.choices.Choice choice, Game game) {
            if (owner.alternativeCostCast && choice.isKeyChoice()) {
                alternativeCostKey(choice.getMessage(), getChoices());
            }
            return super.choose(outcome, choice, game);
        }

        /** Rewrites the generator's leading "Yes" into key "1", the first
         * alternative cost (gorge's AltCostIndex-1 election), when the menu is
         * XMage's alternative-cost choice. Returns whether it did. */
        static boolean alternativeCostKey(String message, List<String> choices) {
            if (message == null || !message.endsWith("choose an alternative cost")
                    && !message.equals("Choose an alternative cost")
                    || choices.isEmpty() || !"Yes".equals(choices.get(0))) {
                return false;
            }
            choices.set(0, "1");
            return true;
        }

        @Override
        public ScriptedChoicePlayer copy() {
            return new ScriptedChoicePlayer(this);
        }

        // Tokens may be created after scripted() queues an answer. Resolve a
        // token ref against the LIVE choice game, not a prematurely bound or
        // stale alias. Other choice values and the target path stay unchanged.
        private Game choiceGame;

        /** Also: the paid additional cost's sacrifice is posed as a target-group
         * `choose`, which reads XMage's CHOICE queue, not the target queue
         * (TargetSacrifice.pay -> TargetImpl.choose -> Player.choose ->
         * makeChoose over getChoices()). Push this cast's recorded pick so
         * makeChoose consumes it and strict mode records the decision, rather
         * than letting XMage's AI auto-choose a legal object. */
        @Override
        public boolean choose(Outcome outcome, mage.target.Target target, Ability source, Game game) {
            Game previous = choiceGame;
            choiceGame = game;
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
                return super.choose(outcome, target, source, game);
            } finally {
                choiceGame = previous;
            }
        }

        @Override
        public boolean choose(Outcome outcome, Cards cards, mage.target.TargetCard target, Ability source, Game game) {
            Game previous = choiceGame;
            choiceGame = game;
            boolean declined = declinePairToChoiceSkip(target.getMinNumberOfTargets(), getChoices(), getTargets());
            try {
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
                Permanent chosen = tokenChoice(ref, owner.seat(refSeat(ref)).getId(),
                        choiceGame.getBattlefield().getAllPermanents());
                return object != null && chosen != null && chosen.getId().equals(object.getId());
            }
            return super.hasObjectTargetNameOrAlias(object, value);
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
                if (selected == null) {
                    chooseTarget(Outcome.Benefit, cards, target, source, game);
                    selected = new CardsImpl(target.getTargets());
                }
                putCardsOnBottomOfLibrary(selected, game, source, true);
                if (!selected.isEmpty()) {
                    game.fireEvent(GameEvent.getEvent(GameEvent.EventType.SCRY_TO_BOTTOM, getId(), source, getId(), selected.size()));
                }
                cards.removeAll(selected);
                putCardsOnTopOfLibrary(cards, game, source, true);
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
                if (selected == null) {
                    chooseTarget(Outcome.Benefit, cards, target, source, game);
                    selected = new CardsImpl(target.getTargets());
                }
                if (!selected.isEmpty()) {
                    graveyard.addAllCards(moveCardsToGraveyardWithInfo(selected.getCards(game), source, game, Zone.LIBRARY));
                }
                cards.removeAll(selected);
                putCardsOnTopOfLibrary(cards, game, source, true);
                top.addAll(cards);
            }
            game.fireEvent(new GameEvent(GameEvent.EventType.SURVEILED, getId(), source, getId(), event.getAmount(), true));
            return Player.SurveilResult.surveil(graveyard, top);
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

            // The remaining labels are the chosen top-card ordering. The
            // generator repeats labels from the arrange decision here; a label
            // may therefore name a card already selected for the graveyard.
            // Match against the original look set to distinguish an answer for
            // this order prompt from an answer belonging to a later choice,
            // while only adding cards still available to the top of the library.
            Set<Card> lookedAt = new java.util.LinkedHashSet<>(lookedAtOrder);
            List<Card> ordered = new ArrayList<>();
            // A partial arrange emits each pick once for selection, then a
            // skip and exactly those same picks for order. The look set can
            // be larger than the pick set; counting it steals later answers.
            // A forced bottom order emits picks only once (no skip), so it
            // has no separate order answers to consume.
            int orderAnswersRemaining = selectionEnded ? selected.size() : 0;
            while (orderAnswersRemaining > 0 && !queue.isEmpty()) {
                String answer = queue.get(0);
                Card match = findByName(available, answer);
                if (match != null) {
                    queue.remove(0);
                    available.remove(match);
                    ordered.add(match);
                    orderAnswersRemaining--;
                    continue;
                }
                if (findByName(lookedAt, answer) == null) {
                    break;
                }
                // This order label names a card consumed during selection.
                // Consume it even though that card cannot be put back on top.
                queue.remove(0);
                orderAnswersRemaining--;
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
            // XMage poses every optional target ask even when no candidate is
            // legal (Targets.canChooseFromPossibleTargets returns true for a
            // min-0 object whatever its candidate set), while gorge's engine
            // never poses an ask without a legal option -- so such an ask is
            // unscriptable by construction and its only legal answer is none.
            // Decline it without consulting the queue: a later ask's scripted
            // answer (Does Machines' combat trigger, queued behind an earlier
            // level-gain trigger's empty graveyard ask) must stay untouched
            // for its own ask. A queued skip still belongs to THIS ask and is
            // honoured by the ordinary path below.
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
            List<String> laterPlayers = hideLaterPlayerTargets(target, getTargets());
            try {
                return chooseTargetInSegment(outcome, target, source, game);
            } finally {
                getTargets().addAll(laterPlayers);
                getTargets().addAll(later);
            }
        }

        /** Whether a target ask is unscriptable and must be declined without
         * consulting the queue: optional (min 0) with no legal candidate, and
         * no skip of its own at the queue front. Mirrors TestPlayer's own
         * empty-skip return, so an ability whose other objects are chosen
         * still proceeds. */
        static boolean unscriptableEmptyAsk(int minTargets, int possibleCount, boolean queueFrontIsSkip) {
            return minTargets == 0 && possibleCount == 0 && !queueFrontIsSkip;
        }

        /** Detaches and returns the queue's "targetPlayer=" entries that
         * belong to a later target object of the same activation or cast, so
         * a TargetPermanentOrPlayer ask sees only its own slot. A player
         * entry at the queue front is this ask's own answer and stays; only
         * entries after the first non-player answer are withheld. */
        static List<String> hideLaterPlayerTargets(mage.target.Target target, List<String> queue) {
            if (!(target.getOriginalTarget() instanceof mage.target.common.TargetPermanentOrPlayer)) {
                return new ArrayList<>();
            }
            int first = -1;
            for (int i = 0; i < queue.size(); i++) {
                if (!queue.get(i).startsWith("targetPlayer=")) {
                    first = i;
                    break;
                }
            }
            if (first < 0) {
                return new ArrayList<>();
            }
            List<String> hidden = new ArrayList<>();
            for (int i = queue.size() - 1; i > first; i--) {
                if (queue.get(i).startsWith("targetPlayer=")) {
                    hidden.add(0, queue.remove(i));
                }
            }
            return hidden;
        }

        /** XMage represents attach prompts either with a "to attach" hint or,
         * as One Last Job does, an unhinted non-targeting TargetPermanent ask. */
        static boolean isAttachmentChoice(mage.target.Target target) {
            return target.getChooseHint() != null && target.getChooseHint().startsWith("to attach ")
                    || target instanceof mage.target.TargetPermanent && target.isNotTarget()
                    && target.getTargetName().contains("can be attached to");
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
            // An adjusted spell's open slot is closed only when XMage asks
            // again after the scenario's answers: a slot whose candidates are
            // exhausted never asks, so a skip queued up front would be left
            // unused and fail assertAllCommandsUsed.
            closeAskedAgain(getTargets(), owner.isAdjustedSpellAsk(source, game));
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

    JsonObject replayOnce(JsonObject sc, boolean strict) {
        JsonObject res = new JsonObject();
        res.addProperty("strict", strict);
        res.addProperty("name", str(sc, "name"));
        if (sc.has("id")) {
            res.add("id", sc.get("id"));
        }
        snaps.clear();
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
            alternativeCostCast = false;
            copyCostCast = null;
            giftPromisedCast = false;
            gorgeName = str(sc, "card");
            xmageName = str(sc, "xmage_name");
            endTurnScenario = hasEndTurnEffect(xmageName.isEmpty() ? gorgeName : xmageName);
            cast.clear();
            adjustedCasts.clear();
            faceDownRefs.clear();
            refAlias.clear();
            phase = MAIN;
            TURN = scenarioTurn(sc);
            turn = TURN;
            activeSeat = (TURN - 1) % 2;
            attackAdvancedTurn = false;
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
                if (i < xans.size() && xans.get(i).isJsonArray()) {
                    // Queue answers by their recorded seat before registering
                    // actions. XMage consumes these queues while executing the
                    // cast; the action registration itself is deferred.
                    scripted(xans.get(i).getAsJsonArray());
                }
                int beforeA = playerA.getActions().size();
                int beforeB = playerB.getActions().size();
                queuePassCommands(i);
                String payment = paymentAt(sc, i);
                stepPayment = payment;
                step(st, op, i);
                String cp = "step " + i + " (" + op + ")";
                final PhaseStep reportAs = checkpointStepAs;
                checkpointStepAs = null;
                runCode(cp, turn, phase, checkpointSeat(op, st, i), (info, p, g) -> {
                    JsonObject snap = snapshot(info, g);
                    if (reportAs != null) {
                        snap.addProperty("step", reportAs.name());
                    }
                    snaps.add(snap);
                });
                if (!payment.isEmpty()) {
                    TestPlayer payer = seat(st.has("seat") ? st.get("seat").getAsInt() : 0);
                    queuePaymentEnd(payer);
                }
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
            setStopAt(endTurnScenario ? turn + 1 : turn,
                    endTurnScenario ? PhaseStep.UPKEEP : PhaseStep.END_TURN);
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
     *
     * Every other op keeps seat 0, which acts last on the turn it is on.
     */
    private TestPlayer checkpointSeat(String op, JsonObject st, int stepIdx) {
        if (op.equals("pass") && isPassRun(steps(sc0), stepIdx)) {
            // The active seat acts first at the step the pass reached, and
            // XMage runs its whole queue there before the other seat acts.
            return seat(activeSeat);
        }
        if (op.equals("pass") && isActiveP1Pair(steps(sc0), stepIdx, activeSeat)
                && passAction(steps(sc0), stepIdx) == PASS_PAIR_SECOND
                && !pairEndsMidResolution(steps(sc0), stepIdx)) {
            // The active seat 1's pair resolves on seat 1's queue, so its
            // second checkpoint must follow that wait there.
            return seat(activeSeat);
        }
        if (op.equals("pass_to")) {
            return seat(activeSeat);
        }
        if (op.equals("resolve")) {
            return seat(st.has("seat") ? st.get("seat").getAsInt() : 0);
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
        setupAttachedPlaced = false;
        setupPermanentIds.clear();
        setupNames.clear();
        placedNames.clear();
        abilityAliasSeq = 0;
        backFaceNames.clear();
        enterWithCountersApplied.clear();
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
            if (s.has("life")) {
                setLife(p, s.get("life").getAsInt());
            }
        }
    }

    /** The setup's per-seat "counters" and "speed" (gorge's runner emits the same
     * CounterChange / SpeedChange events when it places the cards). Runs inside
     * the "setup" checkpoint, before the first snapshot, because a counter or a
     * speed needs a live game. "counters" is card name -> counter kind (the
     * CounterType enum name: CHARGE, P1P1, M1M1, ...) -> amount, applied to every
     * battlefield placement of the name, like "tapped"; a name or kind XMage does
     * not know fails the scenario rather than placing nothing. */
    private void applySetupState(Game g) {
        JsonObject setup = sc0.has("setup") ? sc0.getAsJsonObject("setup") : new JsonObject();
        boolean changed = false;
        for (int i = 0; i < seats; i++) {
            if (!setup.has("p" + i)) {
                continue;
            }
            JsonObject s = setup.getAsJsonObject("p" + i);
            TestPlayer pl = seat(i);
            if (s.has("counters")) {
                for (Map.Entry<String, JsonElement> byCard : s.getAsJsonObject("counters").entrySet()) {
                    String name = xmageSpelling(byCard.getKey());
                    boolean placed = false;
                    for (Permanent perm : g.getBattlefield().getAllPermanents()) {
                        // A back-face-staged permanent is still named by its front (setupNames).
                        if (!pl.getId().equals(perm.getControllerId()) || !setupName(perm).equals(name)) {
                            continue;
                        }
                        if (enterWithCountersApplied.contains(perm.getId())) {
                            // Applied with the placement (build()), which is
                            // what keeps a would-die placement alive to here;
                            // a second application would double the counters.
                            placed = true;
                            continue;
                        }
                        for (Map.Entry<String, JsonElement> byKind : byCard.getValue().getAsJsonObject().entrySet()) {
                            CounterType kind = xmageCounter(byKind.getKey());
                            perm.addCounters(kind.createInstance(byKind.getValue().getAsInt()), pl.getId(), null, g);
                        }
                        placed = true;
                        changed = true;
                    }
                    if (!placed) {
                        throw new IllegalArgumentException("counters name " + byCard.getKey() + ", which is not on p" + i + "'s battlefield");
                    }
                }
            }
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
                changed = true;
            }
        }
        if (changed) {
            // A counter-gated static -- a Spacecraft's StationLevelAbility, "has
            // indestructible as long as it has a divinity counter on it" -- is a
            // ContinuousEffect whose condition reads the source's counters when
            // XMage last applied effects, which was before these setup adds.
            // addCounters does not itself recompute, and the snapshot below reads
            // the stale set, so XMage would show the card still uncounted (no P/T,
            // no keyword) where gorge shows it live. Apply once with the counters
            // in place, exactly as the game loop does at the start of a step.
            g.applyEffects();
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
            for (Map.Entry<String, JsonElement> byKind : counters.entrySet()) {
                permanent.getCounters(game).addCounter(
                        xmageCounter(byKind.getKey()).createInstance(byKind.getValue().getAsInt()));
            }
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
                // gorge's runner emits the setup counters WITH the placement,
                // before any state-based action, so a placement that would die
                // as an SBA (a printed 0/0, an X-cost creature's X=0 board)
                // survives with them. Apply through XMage's enter-with-counters
                // map, keyed by the id the placement resolution produces.
                // CardUtil.getDefaultCardSideForBattlefield is the exact card
                // the cheat() placement turns into a PermanentCard (a stored
                // half maps to itself, a stored parent to its left half, a
                // plain card to itself), and PermanentCard shares the card's
                // id, so applyEnterWithCounters' getEnterWithCounters lookup
                // hits for plain cards AND staged back faces alike. Keying on
                // getMainCard() instead holds the parent's id for a staged
                // half, which the resolution never looks up. The counters are
                // on the permanent when the first SBA check runs, exactly the
                // runner's order; applySetupState no longer re-adds them.
                JsonObject counters = s.has("counters") ? s.getAsJsonObject("counters") : null;
                JsonElement kinds = counters == null ? null : counters.get(n);
                if (kinds != null && kinds.isJsonObject()) {
                    List<PutToBattlefieldInfo> placements = getBattlefieldCards(p);
                    Card placedCard = CardUtil.getDefaultCardSideForBattlefield(
                            currentGame, placements.get(placements.size() - 1).getCard());
                    Counters enter = new Counters();
                    for (Map.Entry<String, JsonElement> byKind : kinds.getAsJsonObject().entrySet()) {
                        enter.addCounter(xmageCounter(byKind.getKey()).createInstance(byKind.getValue().getAsInt()));
                    }
                    currentGame.setEnterWithCounters(placedCard.getId(), enter);
                    enterWithCountersApplied.add(placedCard.getId());
                }
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
            List<mage.MageObject> objs = new ArrayList<>();
            for (Permanent perm : g.getBattlefield().getAllPermanents()) {
                if (pl.getId().equals(perm.getControllerId())) {
                    objs.add(perm);
                }
            }
            for (Card c : pl.getHand().getCards(g)) objs.add(c);
            for (Card c : pl.getGraveyard().getCards(g)) objs.add(c);
            for (Card c : g.getExile().getAllCards(g)) {
                if (pl.getId().equals(c.getOwnerId())) objs.add(c);
            }
            for (Card c : pl.getLibrary().getCards(g)) objs.add(c);
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
        String name = refName(ref).toLowerCase(java.util.Locale.ROOT);
        int wanted = 1;
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            wanted = Integer.parseInt(ref.substring(hash + 1));
        }
        List<Permanent> candidates = new ArrayList<>();
        for (Permanent permanent : battlefield) {
            if (permanent.isToken() && controller.equals(permanent.getControllerId())
                    && permanent.getName().toLowerCase(java.util.Locale.ROOT).contains(name)) {
                candidates.add(permanent);
            }
        }
        candidates.sort(java.util.Comparator.comparingInt(Permanent::getCreateOrder));
        return wanted > 0 && wanted <= candidates.size() ? candidates.get(wanted - 1) : null;
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

    private void bindOneAlias(String ref) {
        if (refAlias.containsKey(ref) || currentGame == null) {
            return;
        }
        int seatIndex = refSeat(ref);
        String name = refName(ref);
        boolean token = ref.contains(":token:");
        int wanted = 1;
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            wanted = Integer.parseInt(ref.substring(hash + 1));
        }
        mage.MageObject match = null;
        int seen = 0;
        if (token) {
            for (Permanent perm : currentGame.getBattlefield().getAllPermanents()) {
                if (!perm.isToken() || !seat(seatIndex).getId().equals(perm.getControllerId())
                        || !perm.getName().contains(name)) {
                    continue;
                }
                if (++seen == wanted) {
                    match = perm;
                    break;
                }
            }
        } else {
            List<mage.MageObject> objs = new ArrayList<>();
            Player pl = seat(seatIndex);
            for (Permanent perm : currentGame.getBattlefield().getAllPermanents()) {
                if (pl.getId().equals(perm.getControllerId())) {
                    objs.add(perm);
                }
            }
            for (Card c : pl.getHand().getCards(currentGame)) objs.add(c);
            for (Card c : pl.getGraveyard().getCards(currentGame)) objs.add(c);
            for (Card c : currentGame.getExile().getAllCards(currentGame)) {
                if (pl.getId().equals(c.getOwnerId())) objs.add(c);
            }
            for (Card c : pl.getLibrary().getCards(currentGame)) objs.add(c);
            for (mage.MageObject o : objs) {
                if (!setupName(o).equals(name)) {
                    continue;
                }
                if (++seen == wanted) {
                    match = o;
                    break;
                }
            }
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

    /** An ask for an adjusted spell's target with no scenario answer left skips
     * the slot (an "up to" slot with candidates remaining). */
    static void closeAskedAgain(List<String> queue, boolean adjustedSpellAsk) {
        if (adjustedSpellAsk && queue.isEmpty()) {
            queue.add(TestPlayer.TARGET_SKIP);
        }
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
     * Whether a spell ability is *targetless until its adjuster runs*: it has a
     * target adjuster AND no base target. Only this shape breaks the inline
     * {@code $target=} form -- the up-front check reads the unadjusted ability
     * ({@code handleNonPlayerTargetTarget}'s empty {@code selectedMode.getTargets()})
     * and throws "Ability has no targets" (The Eagles Are Coming!: no base
     * target, {@code ConditionalTargetAdjuster} supplies it during casting).
     * An adjuster card that already declares a base target (Dominate,
     * Distorting Wake) validates inline and keeps its old path, so this change
     * cannot reroute it.
     */
    static boolean needsQueuedCastTargets(Ability ability) {
        return hasTargetAdjuster(ability) && ability.getAllSelectedTargets().isEmpty();
    }

    /** The card's spell ability, or null when the name does not resolve. */
    private static Ability spellAbility(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
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

    /** Whether the card's spell ability has exactly one target object and
     * n answers already reach its maximum, so no slot is left to skip. */
    private static boolean singleTargetFilled(String name, int n) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        if (c == null) {
            return false;
        }
        return !targetSlotNeedsSkip(c.getSpellAbility().getAllSelectedTargets(), n);
    }

    /** An unfilled or open target slot needs an explicit skip to finish casting. */
    static boolean targetSlotNeedsSkip(List<mage.target.Target> targets, int supplied) {
        return targets.size() != 1 || supplied < targets.get(0).getMaxNumberOfTargets();
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
     * "optionalcost", the face-down Disguise/Morph/Megamorph casts and the "plot" special
     * action and a graveyard "flashback" cast are wired ("plot_cast", the later cast of a plotted card, is not), and an absent or empty mode is the ordinary
     * cast. Any other mode is a scenario the driver cannot replay and must
     * reject loudly rather than cast at face value. */
    static boolean castModeSupported(String mode) {
        return mode == null || mode.isEmpty()
                || mode.equals("bargained") || mode.equals("optionalcost")
                || mode.equals("disguised") || mode.equals("morphed") || mode.equals("megamorphed")
                || mode.equals("plot") || mode.equals("flashback")
                // "alternative": an alternative cost another permanent grants
                // (Leyline of Mutation's {W}{U}{B}{R}{G}). XMage poses it as
                // its own yes/no while casting, which the generator scripts
                // from gorge's election, so the step is a plain cast here.
                || mode.equals("alternative")
                || mode.equals("prepared_copy")
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
     * setup bound one, else the stripped, XMage-spelled card name. */
    private String targetName(String ref) {
        String a = refAlias.get(ref);
        return a != null ? a : xmageSpelling(refName(ref));
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
    // Setup counters already applied WITH the placement (build(), through the
    // enter-with-counters map): applySetupState skips these permanents, since
    // a second application would double them.
    private final Set<UUID> enterWithCountersApplied = new java.util.HashSet<>();
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
                ActivatedManaAbilityImpl ability = findManaAbility(source.card, source.zone, pl, g,
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
        UUID boundId = scenarioObjectId(g, p, ref);
        if (boundId != null) {
            Permanent bound = g.getPermanent(boundId);
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
            if (boundCard != null && boundZone != null) {
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

    /** The only source in list, or its wanted-th (1-based); null when the
     * list is empty or shorter than wanted. */
    private static ManaSource nthSource(List<ManaSource> list, int wanted) {
        if (list.size() == 1) {
            return list.get(0);
        }
        return wanted >= 1 && wanted <= list.size() ? list.get(wanted - 1) : null;
    }

    /** Whether a live object is one of the same-named objects a scenario ref
     * names, by its setup spelling, its staged back face or its live name. */
    private boolean matchesRef(Card object, String front, String live) {
        String setup = setupName(object);
        String actual = xmageSpelling(object.getName());
        return setup.equals(front) || setup.equals(live) || actual.equals(front) || actual.equals(live);
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
     * More than one text match without a usable index is a harness error,
     * never a guess. */
    private static ActivatedManaAbilityImpl findManaAbility(Card source, Zone zone, Player p, Game g,
            String text, Integer abilityIndex, String card, int stepIdx) {
        List<ActivatedManaAbilityImpl> available = new ArrayList<>(
                source.getAbilities(g).getAvailableActivatedManaAbilities(zone, p.getId(), g));
        List<ActivatedManaAbilityImpl> matching = new ArrayList<>();
        for (ActivatedManaAbilityImpl ability : available) {
            if (text.isEmpty() || ability.toString().startsWith(text)) {
                matching.add(ability);
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

    /** Whether step {@code index} is a pass of a recorded game: a pass that
     * carries the "xpos" position gorge reached after it. The hand-authored
     * and ported scenarios never carry one, so they keep the shapes above. */
    static boolean isPassRun(JsonArray steps, int index) {
        return isOp(steps, index, "pass") && steps.get(index).getAsJsonObject().has("xpos");
    }

    private static boolean posFlag(JsonObject pos, String key) {
        return pos.has(key) && pos.get(key).getAsBoolean();
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
        if (!isPassRun(steps, index)) {
            return null;
        }
        JsonObject pos = steps.get(index).getAsJsonObject().getAsJsonObject("xpos");
        if (!posFlag(pos, "moved") || !pos.has("step")) {
            return null;
        }
        String at = pos.get("step").getAsString();
        if (at.equals("declare-attackers") && declares(steps, index + 1, "attack", "attackers")) {
            return PhaseStep.DECLARE_ATTACKERS;
        }
        if (at.equals("declare-blockers") && declares(steps, index + 1, "block", "blocks")) {
            return PhaseStep.DECLARE_BLOCKERS;
        }
        return null;
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
     * priority on or moves the step queues nothing. The step after a pass the
     * active seat made in the same step, if it is an action of the other seat,
     * starts by yielding priority to that seat (the lone-handoff recipe). */
    static List<String> passRunCommands(JsonArray steps, int index, int activeSeat) {
        List<String> cmds = new ArrayList<>();
        if (isPassRun(steps, index - 1) && !isOp(steps, index, "pass")) {
            JsonObject prev = steps.get(index - 1).getAsJsonObject();
            JsonObject pos = prev.getAsJsonObject("xpos");
            String op = str(steps.get(index).getAsJsonObject(), "op");
            boolean acts = op.equals("cast") || op.equals("activate") || op.equals("play");
            if (acts && !posFlag(pos, "moved") && !posFlag(pos, "resolves")
                    && seatOfStep(steps, index - 1) == activeSeat && seatOfStep(steps, index) != activeSeat) {
                cmds.add(CMD_YIELD_PRIORITY);
            }
        }
        if (isPassRun(steps, index) && posFlag(steps.get(index).getAsJsonObject().getAsJsonObject("xpos"), "resolves")) {
            cmds.add(CMD_REQUIRE_STACK);
            cmds.add(CMD_WAIT_RESOLVE_ONE);
        }
        return cmds;
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
            case "combat-damage": return PhaseStep.COMBAT_DAMAGE;
            case "end-combat": return PhaseStep.END_COMBAT;
            case "main2": return PhaseStep.POSTCOMBAT_MAIN;
            case "end": return PhaseStep.END_TURN;
            default: throw new IllegalArgumentException("pass position in unknown step " + step);
        }
    }

    /** Move the replay's turn, step and active seat to where gorge stood after
     * a pass of a recorded game: the steps that follow are queued there. */
    private void followPassPosition(JsonObject pos) {
        turn = pos.get("turn").getAsInt();
        phase = phaseOfGorgeStep(pos.get("step").getAsString());
        activeSeat = pos.get("active").getAsInt();
        attackAdvancedTurn = false;
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
        List<String> cmds = new ArrayList<>();
        if (isPassRun(steps, index) || isPassRun(steps, index - 1)) {
            return passRunCommands(steps, index, activeSeat);
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

    private void queuePassCommands(int stepIdx) {
        // Every pass command runs on the active seat: seat 0 for every shape
        // but the active seat 1's own p1-first pair (isActiveP1Pair).
        TestPlayer active = seat(activeSeat);
        for (String cmd : passCommands(steps(sc0), stepIdx, activeSeat)) {
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
                attackNames = withoutMustAttack(attackNames, ScenarioReplay::isMustAttackName);
                for (String a : attackCommandOrder(attackNames)) {
                    attack(turn, p, a, seat(seatOf(str(st, "defender"))));
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
                    waitStackResolved(turn, phase, isOp(steps(sc0), stepIdx - 1, "pass") ? seat(activeSeat) : p, true);
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
                // An empty step is a checkpoint wherever the scenario already
                // is: phase is left as the preceding ops set it.
                return;
            }
            case "block": {
                // gorge's blocks are [blocker, attacker] pairs. XMage's
                // block() queues a declareBlockers command at DECLARE_BLOCKERS.
                // A recorded game's empty declaration has no "blocks": there
                // is nothing to queue, the step only reaches declare-blockers.
                JsonArray blocks = st.has("blocks") ? st.getAsJsonArray("blocks") : new JsonArray();
                for (JsonElement e : blocks) {
                    JsonArray pair = e.getAsJsonArray();
                    block(turn, p, combatName(pair.get(0).getAsString()), combatName(pair.get(1).getAsString()));
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
                alternativeCostCast = "alternative".equals(castMode);
                copyCostCast = copyCostKeyword(castMode);
                // "plot" is XMage's Plot special action (PlotAbility is a
                // SpecialAction: no stack, not a cast), activated from the
                // hand by its rule text "Plot {cost}" like XMage's own
                // PlotTest does. The step's mana was added above; a
                // reduction (Doc Aurlock) is applied by XMage itself.
                if ("plot".equals(castMode)) {
                    String plotText = xabilityAt(stepIdx);
                    if (plotText.isEmpty()) {
                        throw new IllegalArgumentException("plot step " + stepIdx + " has no xmage_ability");
                    }
                    activateAbility(turn, phase, p, plotText);
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
                // the hand holds the whole "A // B" card (castSpelling).
                String cardName = castSpelling(refName(str(st, "card")));
                // A face-down Morph/Megamorph/Disguise cast names the
                // SpellAbility by its XMage cast spelling; every plain cast
                // names the card itself. Morph and Megamorph share "using
                // Morph".
                String castSuffix = "morphed".equals(mode) || "megamorphed".equals(mode)
                        ? " using Morph"
                        : "disguised".equals(mode) ? " using Disguise" : "";
                String card = cardName + castSuffix;
                if (!castSuffix.isEmpty()) {
                    faceDownRefs.add(str(st, "card"));
                }
                List<String> tg = targets(st);
                if (hasAlternativeSourceCost(cardName)) {
                    // A plain cast step: gorge paid the mana cost, so decline
                    // the alternative cost (evoke, impending, dash, ...)
                    // XMage offers through its "Cast with no alternative
                    // cost" choice, rather than leave it to the AI.
                    setChoice(p, "Cast with no alternative cost");
                }
                List<Integer> skips = castTargetSkipsAt(stepIdx, cardName, tg.size());
                if (splitScripted && spellTargetsDivided(cardName) && skips.isEmpty()) {
                    // The scripted "<ref>^X=<share>" answers name the targets
                    // and gorge's split; a target string here would be a
                    // second, unconsumed set.
                    castSpell(turn, phase, p, card);
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
                    castSpell(turn, phase, p, card);
                } else if (!tg.isEmpty() && castQueuedTargets(turn, phase, p, cardName, tg, castAbility)) {
                    // Cast with its targets queued: see castQueuedTargets.
                } else if (tg.size() == 1 && isSeatRef(tg.get(0)) && !hasGift(cardName)) {
                    castSpell(turn, phase, p, card, seat(seatOf(tg.get(0))));
                } else if (tg.size() == 1 && isSeatRef(tg.get(0))) {
                    // A Gift spell (Mind Spiral, Sazacap's Brew): the castSpell
                    // player form binds the wrong ask, so queue the spell's
                    // own player target and close the rest.
                    addTarget(p, seat(seatOf(tg.get(0))));
                    addTarget(p, TestPlayer.TARGET_SKIP);
                    castSpell(turn, phase, p, card);
                } else if (tg.isEmpty()) {
                    castSpell(turn, phase, p, card);
                    cast.add(cardName);
                    return;
                } else if (tg.size() == 1 && cast.contains(castSpelling(refName(tg.get(0))))) {
                    // Targeting a spell cast by an earlier step: wait for it
                    // on the stack. cast holds cast-command names and the
                    // target is a scenario ref; the setup alias names the
                    // card in hand, not the spell, so target by name.
                    String spell = castSpelling(refName(tg.get(0)));
                    castSpell(turn, phase, p, card, spell, spell);
                } else if (tg.size() == 1) {
                    // A single target goes through XMage's own string form. A
                    // divided target whose inline form XMage cannot read
                    // (Twin Bolt) was queued by castQueuedTargets above.
                    castSpell(turn, phase, p, card, targetName(tg.get(0)));
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
                        List<String> names = new ArrayList<>();
                        for (String t : tg) {
                            names.add(xmageSpelling(refName(t)));
                        }
                        castSpell(turn, phase, p, card, String.join("^", names));
                        cast.add(cardName);
                        return;
                    }
                    for (String t : tg) {
                        queueCastTarget(p, t);
                    }
                    if (!singleTargetFilled(cardName, tg.size())) {
                        // Close an "up to N" slot the scenario left short,
                        // or a later slot (Rhino's Rampage's reflexive
                        // trigger). A skip after the one multi-target slot
                        // is filled is rejected (Pull Through the Weft).
                        addTarget(p, TestPlayer.TARGET_SKIP);
                    }
                    castSpell(turn, phase, p, card);
                }
                cast.add(cardName);
                return;
            }
            case "activate": {
                String text = xabilityAt(stepIdx);
                if (text.isEmpty()) {
                    throw new IllegalArgumentException("activate step " + stepIdx + " has no xmage_ability");
                }
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
                if (isManaAbilityText(card, text)) {
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
            case "play":
                playLand(turn, phase, p, xmageSpelling(refName(str(st, "card"))));
                return;
            case "pass":
                // Validated here; its XMage commands are queued around the
                // checkpoint by queuePassCommands, which the replay loop runs before this step.
                if (passAction(steps(sc0), stepIdx, activeSeat) == PASS_RUN) {
                    followPassPosition(st.getAsJsonObject("xpos"));
                    PhaseStep declaration = declarationAhead(steps(sc0), stepIdx);
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
    private void scripted(JsonArray as) {
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
                        if (isScenarioRef(v)) {
                            // A same-name target the generator names by its
                            // exact ref: bind its @alias if it entered after
                            // setup, so targetName resolves it.
                            bindAnswerAlias(v);
                        }
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
                        queueChoice(p, aliasChoiceValue(v));
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
            castSpell(turn, phase, p, card);
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
        castSpell(turn, phase, p, card);
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
     * setup alias (a token, a permanent that entered later) keeps the plain
     * text.
     */
    private String boundActivationText(TestPlayer p, String ref, String text, int stepIdx) {
        String alias = refAlias.get(ref);
        if (alias == null || text.startsWith("@")) {
            return text;
        }
        String key = "act" + stepIdx;
        String name = refName(ref);
        runCode("bind " + ref, turn, phase, p, (info, pl, g) -> {
            UUID setupId = p.getAliasByName(alias);
            UUID id = activationSourceId(p, g, name, text, setupId);
            if (id == null) {
                id = setupId;
            }
            if (id == null) {
                throw new IllegalArgumentException("activate " + ref + ": no setup object bound to " + alias);
            }
            for (int j = 0; j < seats; j++) {
                seat(j).addAlias(key, id);
            }
        });
        return "@" + key + " " + text;
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
     * and a split/Room object's front half). */
    private boolean sourceNameIsNamed(String live, String name) {
        return live != null
                && (live.equals(xmageSpelling(name)) || gorgeSpellingRule(gorgeName, xmageName, live).equals(name));
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
        lastCheckpointStack = stackNames(g);
        JsonObject s = new JsonObject();
        s.addProperty("checkpoint", checkpoint);
        s.addProperty("turn", g.getTurnNum());
        s.addProperty("step", g.getTurnStepType() == null ? "" : g.getTurnStepType().name());
        s.addProperty("active", seatIndex(g, g.getActivePlayerId()));
        s.addProperty("priority", seatIndex(g, g.getPriorityPlayerId()));
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
        for (Permanent perm : g.getBattlefield().getAllPermanents()) {
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
                src = mo == null ? "?" : mo.getName();
            }
            o.addProperty("source", src);
            o.addProperty("controller", seatIndex(g, so.getControllerId()));
            stack.add(o);
        }
        s.add("stack", stack);
        if (wantsOffered()) {
            s.add("offered", offered(g));
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
     * What the player holding priority may play, in gorge's snapshot
     * vocabulary (rules.OracleSnapOffered): the source's scenario ref, the
     * kind ("cast", "play" for a land, otherwise "activate" -- mana abilities
     * included, CR 605.3a: a mana ability is activatable whenever its
     * controller has priority), and the ability's rule text. The comparer
     * strips the display prefixes. No priority player (the game ended, or the
     * checkpoint is a skipped-step label recorded after the turn) offers
     * nothing.
     */
    private JsonArray offered(Game g) {
        JsonArray out = new JsonArray();
        UUID holder = g.getPriorityPlayerId();
        if (holder == null || g.hasEnded()) {
            return out;
        }
        Player pr = g.getPlayer(holder);
        if (pr == null) {
            return out;
        }
        for (Ability a : pr.getPlayable(g, true)) {
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
