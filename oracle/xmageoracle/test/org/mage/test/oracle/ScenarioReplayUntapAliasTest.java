package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.FileReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.util.ArrayList;
import java.util.List;
import org.mage.test.player.TestPlayer;

/** Executable regression for Unwind's "Untap up to three lands": gorge logs
 * the pick on the choice queue as an alias ("@p0:Island") plus a closing
 * [choice_skip]. XMage's UntapLandsEffect first asks the chooseUse shortcut
 * "Include your tapped lands to untap?" when every tapped land fits under the
 * cap; TestPlayer's chooseUse rejects aliases. The driver must decline that
 * shortcut and let the effect's own target.choose (makeChoose) consume the
 * aliased pick and the skip, untapping exactly one of p0's three tapped
 * Islands. Also pins the no-op arm: a queued pick naming an already-untapped
 * land is consumed as a no-op and closes the ask, never an AI untap.
 *
 * Run scripts/xmage-oracle-test-untap-alias.sh. */
public final class ScenarioReplayUntapAliasTest {
    private static final String ID = "xs:XP:unwind:cast:s1";

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static void equal(Object want, Object got) {
        if (!java.util.Objects.equals(want, got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static List<String> q(String... items) {
        return new ArrayList<>(List.of(items));
    }

    /** Replay one scenario line through the real batch entry point. */
    private static JsonObject replay(ScenarioReplay driver, String line) throws Exception {
        List<JsonObject> rows = new ArrayList<>();
        try (BufferedReader in = new BufferedReader(new StringReader(line));
             StringWriter text = new StringWriter();
             PrintWriter out = new PrintWriter(text)) {
            equal(1, ScenarioReplay.replayLines(driver, in, out));
            out.flush();
            for (String rowLine : text.toString().split("\\R")) {
                if (!rowLine.trim().isEmpty()) {
                    rows.add(JsonParser.parseString(rowLine).getAsJsonObject());
                }
            }
        }
        equal(1, rows.size());
        return rows.get(0);
    }

    /** The shortcut's three answers on queues the replay shapes: empty (take
     * it), a real pick or skip (decline, leave for the target ask), and an
     * already-untapped pick (consume as a no-op, close the ask). */
    private static void shortcutAnswers() throws Exception {
        java.lang.reflect.Method answer = Class.forName(
                "org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer")
                .getDeclaredMethod("untapIncludeAnswer", List.class,
                        java.util.function.Predicate.class, java.util.function.Predicate.class);
        answer.setAccessible(true);
        java.util.function.Predicate<String> tapped = a -> a.equals("@p0:Island") || a.equals("Island");
        java.util.function.Predicate<String> anyLand = a -> a.equals("@p0:Island") || a.equals("Island");
        java.util.function.Predicate<String> none = a -> false;

        List<String> c = q();
        equal(Boolean.TRUE, answer.invoke(null, c, tapped, anyLand));

        c = q("@p0:Island", TestPlayer.CHOICE_SKIP);
        equal(Boolean.FALSE, answer.invoke(null, c, tapped, anyLand));
        equal(q("@p0:Island", TestPlayer.CHOICE_SKIP), c);

        c = q("@p0:Island");
        equal(Boolean.FALSE, answer.invoke(null, c, none, anyLand));
        equal(q(TestPlayer.CHOICE_SKIP), c);

        c = q("@p0:Island", "Forest");
        equal(Boolean.FALSE, answer.invoke(null, c, none, anyLand));
        equal(q(TestPlayer.CHOICE_SKIP, "Forest"), c);

        c = q("Forest");
        equal(Boolean.TRUE, answer.invoke(null, c, tapped, anyLand));
        equal(q("Forest"), c);

        c = q("Yes");
        equal(null, answer.invoke(null, c, tapped, anyLand));
        equal(q("Yes"), c);
    }

    public static void main(String[] args) throws Exception {
        shortcutAnswers();

        check(args.length == 1, "usage: ScenarioReplayUntapAliasTest <fixture.jsonl>");
        String line;
        JsonObject fixture;
        int steps;
        try (BufferedReader in = new BufferedReader(new FileReader(args[0]))) {
            line = in.readLine();
            fixture = JsonParser.parseString(line).getAsJsonObject();
            JsonArray array = fixture.getAsJsonArray("steps");
            check(array != null, "the fixture has no steps");
            steps = array.size();
        }
        equal(ID, fixture.get("id").getAsString());
        equal("Unwind", fixture.get("card").getAsString());
        String answers = fixture.getAsJsonArray("xmage_answers").toString();
        check(answers.contains("@p0:Island") && answers.contains("[choice_skip]"),
                "the fixture no longer scripts the aliased up-to untap pick: " + answers);

        ScenarioReplay driver = new ScenarioReplay();
        JsonObject row = replay(driver, line);
        equal(ID, row.get("id").getAsString());
        check(!row.has("harness"), "the unwind replay failed: " + row.get("harness"));
        check(!row.has("strict_miss"), "the unwind replay is a strict miss: " + row.get("strict_miss"));
        JsonArray snaps = row.getAsJsonArray("snapshots");
        equal(steps + 1, snaps.size());

        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        int islands = 0;
        int untapped = 0;
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject p = e.getAsJsonObject();
            if (!"Island".equals(p.get("name").getAsString()) || p.get("controller").getAsInt() != 0) {
                continue;
            }
            islands++;
            if (!p.get("tapped").getAsBoolean()) {
                untapped++;
            }
        }
        equal(3, islands);
        equal(1, untapped);
        String p0 = last.getAsJsonArray("players").get(0).getAsJsonObject().toString();
        check(p0.contains("\"Unwind\""), "Unwind did not reach p0's graveyard: " + p0);

        System.out.println("PASS an aliased up-to untap pick replays " + steps + " steps clean,"
                + " declining the include-tapped shortcut and untapping exactly one of three Islands");
    }
}
