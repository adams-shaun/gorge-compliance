package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;
import org.mage.test.player.TestPlayer;

/** An optional attack trigger whose "may" gorge folds into the body's pick or
 * cost: gorge logs the pick alone, or nothing when there is nothing to pick
 * (Preeminent Captain with no Soldier in hand; Myr Battlesphere with no other
 * Myr, X = 0), yet XMage asks the trigger's "Use ability?" (and Captain's own
 * "Put ... from your hand?") anyway, and strict mode stopped at "Missing CHOICE
 * def". The driver answers from the folded pick: no pick, no; a queued pick,
 * yes. The fixtures are the xproof finds' scenarios, plus Captain with a
 * Soldier in hand and the pick scripted on the choice queue. Run
 * scripts/xmage-oracle-test-folded-optional.sh; it needs the out-of-tree
 * build's card database, like a full replay. */
public final class ScenarioReplayFoldedOptionalTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static List<String> q(String... items) {
        return new ArrayList<>(List.of(items));
    }

    /** The folded answer, with the ask being a resolving trigger's own. */
    private static Boolean folded(boolean triggerResolves, List<String> choices, List<String> targets)
            throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method m = adapter.getDeclaredMethod("foldedOptionalAnswer", boolean.class, List.class, List.class);
        m.setAccessible(true);
        return (Boolean) m.invoke(null, triggerResolves, choices, targets);
    }

    private static JsonObject replay(Path fixture) throws Exception {
        String line = new String(Files.readAllBytes(fixture), StandardCharsets.UTF_8).trim();
        StringWriter out = new StringWriter();
        try (BufferedReader in = new BufferedReader(new StringReader(line));
             PrintWriter writer = new PrintWriter(out)) {
            check(ScenarioReplay.replayLines(new ScenarioReplay(), in, writer) == 1, fixture + ": one row");
        }
        JsonObject row = JsonParser.parseString(out.toString().trim()).getAsJsonObject();
        for (String key : new String[] {"strict_miss", "harness", "leftover", "trailing_miss"}) {
            check(!row.has(key), fixture.getFileName() + ": " + key + " = " + row.get(key));
        }
        check(row.get("strict").getAsBoolean(), fixture.getFileName() + ": strict replay");
        return row;
    }

    /** The scenario's permanent of that name at the checkpoint, or null. */
    private static JsonObject permanent(JsonObject row, String checkpoint, String name) {
        for (JsonElement s : row.getAsJsonArray("snapshots")) {
            JsonObject snap = s.getAsJsonObject();
            if (!snap.get("checkpoint").getAsString().equals(checkpoint)) {
                continue;
            }
            JsonArray perms = snap.getAsJsonArray("permanents");
            for (JsonElement p : perms) {
                if (p.getAsJsonObject().get("name").getAsString().equals(name)) {
                    return p.getAsJsonObject();
                }
            }
        }
        return null;
    }

    public static void main(String[] args) throws Exception {
        foldedOptionalAttackTrigger(Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures"));
        System.out.println("PASS folded optional attack trigger: Captain empty hand no, Myr Battlesphere X=0 no,"
                + " Captain with a picked Soldier yes");
    }

    private static void foldedOptionalAttackTrigger(Path dir) throws Exception {
        // The rule: nothing queued is no; a queued pick is yes and stays queued
        // for the body's own ask; a declined pick's skip goes with the no; a
        // scripted Yes/No and an ask no resolving trigger posed are not ours.
        List<String> c = q();
        List<String> t = q();
        check(Boolean.FALSE.equals(folded(true, c, t)), "empty queues: no");
        c = q("Midnight Duelist");
        check(Boolean.TRUE.equals(folded(true, c, q())), "a pick on the choice queue: yes");
        check(c.equals(q("Midnight Duelist")), "the pick stays queued");
        t = q("p0:Plains");
        check(Boolean.TRUE.equals(folded(true, q(), t)) && t.equals(q("p0:Plains")), "a pick on the target queue: yes");
        c = q(TestPlayer.CHOICE_SKIP);
        t = q(TestPlayer.TARGET_SKIP, "later");
        check(Boolean.FALSE.equals(folded(true, c, t)), "a declined pick: no");
        check(c.isEmpty() && t.equals(q("later")), "the declined pick's skips go with the no");
        c = q("Yes", "Midnight Duelist");
        check(folded(true, c, q()) == null && c.size() == 2, "a scripted Yes is TestPlayer's");
        c = q("No");
        check(folded(true, c, q()) == null && c.size() == 1, "a scripted No is TestPlayer's");
        c = q("Midnight Duelist");
        check(folded(false, c, q()) == null && c.size() == 1, "not a resolving trigger's ask: untouched");
        // Documented limit: the rule reads the head of the queue and cannot tell whose pick it
        // is. A pick queued for a LATER ask in the same step (here a second trigger's target)
        // is read as this trigger's yes when gorge folded this trigger's own may to nothing.
        // That ask strict-missed before this rule, so no replay that passed changes; the pick
        // stays queued for the ask it belongs to.
        c = q();
        t = q("p1:Phyrexian Walker");
        check(Boolean.TRUE.equals(folded(true, c, t)) && t.equals(q("p1:Phyrexian Walker")),
                "a later ask's pick at the head is read as yes and stays queued (documented limit)");

        // The replays, strict: a "Missing CHOICE def" would be a strict_miss.
        replay(dir.resolve("folded-optional-captain-empty-hand.jsonl"));
        JsonObject myr = replay(dir.resolve("folded-optional-myr-battlesphere.jsonl"));
        JsonObject battlesphere = permanent(myr, "step 6 (pass)", "Myr Battlesphere");
        check(battlesphere != null && battlesphere.get("pt").getAsString().equals("4/7"),
                "X = 0: Myr Battlesphere is not pumped, " + battlesphere);
        JsonObject soldier = replay(dir.resolve("folded-optional-captain-soldier.jsonl"));
        JsonObject duelist = permanent(soldier, "step 6 (pass)", "Midnight Duelist");
        check(duelist != null && duelist.get("tapped").getAsBoolean() && duelist.get("attacking").getAsBoolean(),
                "the picked Soldier entered tapped and attacking, " + duelist);
    }
}
