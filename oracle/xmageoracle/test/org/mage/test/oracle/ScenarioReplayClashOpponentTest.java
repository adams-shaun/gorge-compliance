package org.mage.test.oracle;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

/** A clash with the game's lone opponent (xp-xmage-choice-def-residual2).
 * ClashEffect asks the controller for "an opponent to clash with"
 * (TargetOpponent); with one opponent the candidate set equals what is needed,
 * so forcedChoose took the opponent without reading the scripted "PlayerB" at
 * the head of the choice queue (a player is no MageObject, so the head never
 * "named a candidate"). The leftover name then sat in front of the "Put ...
 * back on top?" chooseUse and strict mode stopped at "Missing CHOICE def".
 * The fixture is the Lash Out find: opponent p1, then no (bottom) for each
 * clashing player. Run scripts/xmage-oracle-test-clash-opponent.sh; it needs
 * the out-of-tree build's card database, like a full replay. */
public final class ScenarioReplayClashOpponentTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args.length > 0 ? args[0] : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");
        Path fixture = dir.resolve("clash-lone-opponent-lash-out.jsonl");
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
        System.out.println("PASS clash with the lone opponent: the scripted opponent name is consumed, "
                + "then each player's put-back chooseUse");
    }
}
