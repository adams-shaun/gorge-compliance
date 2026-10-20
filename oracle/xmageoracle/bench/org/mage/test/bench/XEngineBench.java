package org.mage.test.bench;

import java.io.File;
import java.lang.management.ManagementFactory;
import java.util.List;
import java.util.Set;
import java.util.UUID;

import mage.abilities.Ability;
import mage.abilities.ActivatedAbility;
import mage.abilities.PlayLandAbility;
import mage.abilities.common.PassAbility;
import mage.cards.decks.Deck;
import mage.cards.decks.DeckCardLists;
import mage.cards.decks.importer.DeckImporter;
import mage.cards.repository.CardScanner;
import mage.constants.MultiplayerAttackOption;
import mage.constants.RangeOfInfluence;
import mage.game.Game;
import mage.game.GameOptions;
import mage.game.TwoPlayerDuel;
import mage.game.TwoPlayerMatch;
import mage.game.combat.CombatGroup;
import mage.game.match.Match;
import mage.game.match.MatchOptions;
import mage.game.mulligan.MulliganType;
import mage.game.permanent.Permanent;
import mage.players.Player;
import mage.player.ai.ComputerPlayer;
import mage.player.ai.ComputerPlayer7;
import mage.server.game.GameFactory;
import mage.server.managers.ConfigSettings;
import mage.server.util.ConfigFactory;
import mage.server.util.ConfigWrapper;
import mage.server.util.PluginUtil;
import mage.server.util.config.GamePlugin;
import mage.util.Copier;
import mage.util.RandomUtil;

/**
 * XEngineBench: whole-game XMage throughput for the cross-engine comparison
 * on gorge's performance pages (gorge cmd/enginebench's random and bot rows,
 * in XMage).
 *
 * <pre>
 *   XEngineBench POLICY DECK_A.dck DECK_B.dck WARMUP_S MEASURE_S [SEED] [MAX_TURNS] [SKILL]
 * </pre>
 *
 * POLICY is one of:
 * <ul>
 *   <li>random: at each priority a uniformly random playable ability or pass
 *   (XMage's own MCTS rollout rule, SimulatedPlayerMCTS.getAction), random
 *   attackers and blockers; every other ask (targets, modes, mulligan) is
 *   answered by XMage's base ComputerPlayer heuristics.</li>
 *   <li>simple: XMage's base ComputerPlayer on both seats (no look-ahead).</li>
 *   <li>mad: ComputerPlayer7 ("Computer - mad", the client's default AI) at
 *   SKILL (default 1).</li>
 * </ul>
 *
 * Run it with Mage.Tests as the working directory (config/config.xml and the
 * prebuilt db/cards.h2 card DB live there). Plays whole games for WARMUP_S
 * CPU seconds first (JIT warm-up, not reported), then for at least MEASURE_S
 * CPU seconds (the game in flight finishes), alternating which deck starts, and
 * prints one JSON line per measured game plus a summary line. Times are
 * process CPU (every JVM thread: game, AI search, JIT, GC) and wall; pin the
 * JVM to one core (taskset -c N) so the two agree.
 */
public final class XEngineBench {

    /** Priority/attack/block asks answered on the game thread. */
    static long decisions;

    static boolean onGameThread() {
        return "main".equals(Thread.currentThread().getName());
    }

    /** Uniformly random priority, attack and block (SimulatedPlayerMCTS's rules). */
    public static final class RandomXPlayer extends ComputerPlayer {
        public RandomXPlayer(String name, RangeOfInfluence range) {
            super(name, range);
        }

        public RandomXPlayer(final RandomXPlayer p) {
            super(p);
        }

        @Override
        public RandomXPlayer copy() {
            return new RandomXPlayer(this);
        }

        @Override
        public boolean priority(Game game) {
            if (onGameThread()) {
                decisions++;
            }
            List<ActivatedAbility> playables = getPlayable(game, true);
            playables.add(new PassAbility());
            Ability ability = playables.get(RandomUtil.nextInt(playables.size()));
            List<Ability> options = getPlayableOptions(ability, game);
            if (!options.isEmpty()) {
                ability = options.get(RandomUtil.nextInt(options.size()));
            }
            if (ability instanceof PassAbility) {
                pass(game);
                return false;
            }
            activateAbility((ActivatedAbility) ability, game);
            return true;
        }

        @Override
        public void selectAttackers(Game game, UUID attackingPlayerId) {
            if (onGameThread()) {
                decisions++;
            }
            Set<UUID> opps = game.getOpponents(playerId, true);
            if (opps.isEmpty()) {
                return;
            }
            UUID defenderId = opps.iterator().next();
            for (Permanent attacker : getAvailableAttackers(defenderId, game)) {
                if (RandomUtil.nextBoolean()) {
                    setStoredBookmark(game.bookmarkState());
                    if (!game.getCombat().declareAttacker(attacker.getId(), defenderId, playerId, game)) {
                        game.undo(playerId);
                    }
                }
            }
        }

        @Override
        public void selectBlockers(Ability source, Game game, UUID defendingPlayerId) {
            if (onGameThread()) {
                decisions++;
            }
            int groups = game.getCombat().getGroups().size();
            if (groups == 0) {
                return;
            }
            for (Permanent blocker : getAvailableBlockers(game)) {
                int pick = RandomUtil.nextInt(groups + 1);
                if (pick < groups) {
                    CombatGroup g = game.getCombat().getGroups().get(pick);
                    if (!g.getAttackers().isEmpty()) {
                        declareBlocker(playerId, blocker.getId(), g.getAttackers().get(0), game);
                    }
                }
            }
        }
    }

    /** XMage's base heuristic player, counted. */
    public static final class SimpleXPlayer extends ComputerPlayer {
        public SimpleXPlayer(String name, RangeOfInfluence range) {
            super(name, range);
        }

        public SimpleXPlayer(final SimpleXPlayer p) {
            super(p);
        }

        @Override
        public SimpleXPlayer copy() {
            return new SimpleXPlayer(this);
        }

        @Override
        public boolean priority(Game game) {
            if (onGameThread()) {
                decisions++;
            }
            return super.priority(game);
        }
    }

    /** ComputerPlayer7 ("Computer - mad"), counted on the game thread only. */
    public static final class MadXPlayer extends ComputerPlayer7 {
        public MadXPlayer(String name, RangeOfInfluence range, int skill) {
            super(name, range, skill);
        }

        public MadXPlayer(final MadXPlayer p) {
            super(p);
        }

        @Override
        public MadXPlayer copy() {
            return new MadXPlayer(this);
        }

        @Override
        public boolean priority(Game game) {
            if (onGameThread()) {
                decisions++;
            }
            return super.priority(game);
        }
    }

    static Player newPlayer(String policy, String name, int skill) {
        switch (policy) {
            case "random":
                return new RandomXPlayer(name, RangeOfInfluence.ONE);
            case "simple":
                return new SimpleXPlayer(name, RangeOfInfluence.ONE);
            case "mad":
                return new MadXPlayer(name, RangeOfInfluence.ONE, skill);
            default:
                throw new IllegalArgumentException("unknown policy " + policy + " (random|simple|mad)");
        }
    }

    static void initPlugins() {
        if (GameFactory.instance.getGameTypes().isEmpty()) {
            ConfigSettings config = new ConfigWrapper(ConfigFactory.loadFromFile("config/config.xml"));
            for (GamePlugin plugin : config.getGameTypes()) {
                GameFactory.instance.addGameType(plugin.getName(), PluginUtil.loadGameType(plugin),
                        PluginUtil.loadPlugin(plugin, plugin.getClassName()));
            }
            Copier.setLoader(PluginUtil.classLoader);
        }
        new File("saved").mkdirs();
    }

    static long cpuNanos() {
        return ((com.sun.management.OperatingSystemMXBean) ManagementFactory.getOperatingSystemMXBean()).getProcessCpuTime();
    }

    /** One game; returns {turns, decisions, winnerSeat (-1 none)}. */
    static int[] play(String policy, DeckCardLists la, DeckCardLists lb, long seed, int maxTurns, int skill,
                      boolean swap) throws Exception {
        RandomUtil.setSeed(seed);
        Game game = new TwoPlayerDuel(MultiplayerAttackOption.LEFT, RangeOfInfluence.ONE,
                MulliganType.GAME_DEFAULT.getMulligan(0), 40, 20, 7);
        Match match = new TwoPlayerMatch(new MatchOptions("bench", "Two Player Duel", false));
        Player[] ps = new Player[2];
        DeckCardLists[] lists = {la, lb};
        for (int i = 0; i < 2; i++) {
            Player p = newPlayer(policy, "P" + i, skill);
            Deck d = Deck.load(lists[i], false, false);
            if (d.getMaindeckCards().size() < 40) {
                // An unresolved card name drops silently; never measure a short deck.
                throw new IllegalArgumentException("deck " + i + " loaded " + d.getMaindeckCards().size() + " cards");
            }
            game.loadCards(d.getCards(), p.getId());
            game.addPlayer(p, d);
            match.addPlayer(p, d);
            ps[i] = p;
        }
        GameOptions opts = new GameOptions();
        opts.stopOnTurn = maxTurns + 1;
        game.setGameOptions(opts);
        long d0 = decisions;
        game.start(ps[swap ? 1 : 0].getId());
        int winner = -1;
        for (int i = 0; i < 2; i++) {
            Player p = game.getPlayer(ps[i].getId());
            if (p != null && p.hasWon()) {
                winner = i;
            }
        }
        return new int[]{game.getTurnNum(), (int) (decisions - d0), winner};
    }

    public static void main(String[] args) throws Exception {
        if (args.length < 5) {
            System.err.println("usage: XEngineBench random|simple|mad DECK_A.dck DECK_B.dck WARMUP_S MEASURE_S [SEED] [MAX_TURNS] [SKILL]");
            System.exit(2);
        }
        String policy = args[0];
        double warmS = Double.parseDouble(args[3]);
        double measureS = Double.parseDouble(args[4]);
        long seed = args.length > 5 ? Long.parseLong(args[5]) : 1L;
        int maxTurns = args.length > 6 ? Integer.parseInt(args[6]) : 100;
        int skill = args.length > 7 ? Integer.parseInt(args[7]) : 1;

        long tBoot0 = System.nanoTime();
        CardScanner.scan();
        initPlugins();
        DeckCardLists la = DeckImporter.importDeckFromFile(args[1], false);
        DeckCardLists lb = DeckImporter.importDeckFromFile(args[2], false);
        long bootMs = (System.nanoTime() - tBoot0) / 1_000_000;
        long bootCpuMs = cpuNanos() / 1_000_000;

        long turns = 0, decs = 0, capped = 0;
        int measured = 0, warm = 0;
        long w0 = cpuNanos();
        while ((cpuNanos() - w0) / 1e9 < warmS) {
            play(policy, la, lb, seed + warm, maxTurns, skill, (warm & 1) == 1);
            warm++;
        }
        long cpu0 = cpuNanos(), wall0 = System.nanoTime();
        for (int g = warm; (cpuNanos() - cpu0) / 1e9 < measureS; g++) {
            long c = cpuNanos(), w = System.nanoTime();
            int[] r = play(policy, la, lb, seed + g, maxTurns, skill, (g & 1) == 1);
            measured++;
            turns += r[0];
            decs += r[1];
            if (r[2] < 0) {
                capped++;
            }
            System.out.printf("{\"engine\":\"xmage\",\"policy\":\"%s\",\"game\":%d,\"turns\":%d,\"decisions\":%d,"
                            + "\"winner\":%d,\"cpu_ms\":%.3f,\"wall_ms\":%.3f}%n",
                    policy, g, r[0], r[1], r[2], (cpuNanos() - c) / 1e6, (System.nanoTime() - w) / 1e6);
        }
        double cpuS = (cpuNanos() - cpu0) / 1e9, wallS = (System.nanoTime() - wall0) / 1e9;
        Runtime rt = Runtime.getRuntime();
        System.out.printf("{\"summary\":true,\"engine\":\"xmage\",\"policy\":\"%s\",\"skill\":%d,\"warmup_games\":%d,"
                        + "\"games\":%d,\"turns\":%d,\"decisions\":%d,\"no_winner\":%d,\"cpu_s\":%.3f,\"wall_s\":%.3f,"
                        + "\"games_per_cpu_s\":%.4f,\"turns_per_cpu_s\":%.3f,\"us_per_decision\":%.2f,\"boot_wall_ms\":%d,"
                        + "\"boot_cpu_ms\":%d,\"heap_used_mb\":%.1f,\"java\":\"%s\"}%n",
                policy, skill, warm, measured, turns, decs, capped, cpuS, wallS, measured / cpuS, turns / cpuS,
                decs == 0 ? 0 : cpuS * 1e6 / decs, bootMs, bootCpuMs,
                (rt.totalMemory() - rt.freeMemory()) / 1048576.0, System.getProperty("java.version"));
        System.exit(0);
    }
}
