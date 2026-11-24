package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
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

/** Volatile Arsonist's attack trigger has three "up to one" targets, asked
 * in order: a TargetCreaturePermanent, a plain TargetPlayer and a
 * TargetPlaneswalkerPermanent. The replay must route the step's queued
 * answers (Shield Sphere, PlayerB, Grist) one to each slot, so each takes
 * 1 damage: Shield Sphere 1 damage, PlayerB 19 life, Grist loyalty 2.
 *
 * <p>XMage's card at the pin deals the damage to the first target only
 * (DamageTargetEffect's default first-target pointer; docs/upstream-bugs.md,
 * fork PR adams-shaun/mage#23), which hid the driver's routing behind a
 * card bug. scripts/xmage-oracle-test-three-slot-trigger.sh therefore puts
 * the fixed card (docs/upstream-patches/xmage-volatile-arsonist-each-target.patch)
 * ahead of the build on the classpath; at a pin that already carries the
 * fix the script uses the build's own class. Needs the out-of-tree build's
 * card database, like a full replay. */
public final class ScenarioReplayThreeSlotTriggerTest {
    private static final String FIXTURE = "three-slot-trigger-volatile-arsonist.jsonl";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    private static JsonObject permanent(JsonObject snap, String name) {
        for (JsonElement e : snap.getAsJsonArray("permanents")) {
            JsonObject p = e.getAsJsonObject();
            if (name.equals(p.get("name").getAsString())) {
                return p;
            }
        }
        throw new AssertionError("no " + name + " at " + snap.get("checkpoint").getAsString());
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");
        String line = new String(Files.readAllBytes(dir.resolve(FIXTURE)), StandardCharsets.UTF_8).trim();
        StringWriter out = new StringWriter();
        int n;
        try (BufferedReader in = new BufferedReader(new StringReader(line));
             PrintWriter writer = new PrintWriter(out)) {
            n = ScenarioReplay.replayLines(new ScenarioReplay(), in, writer);
        }
        equal(1, n, "scenario rows");
        JsonObject res = JsonParser.parseString(out.toString().trim()).getAsJsonObject();
        for (String key : new String[]{"harness", "error", "strict_miss"}) {
            if (res.has(key) && !res.get(key).isJsonNull()) {
                throw new AssertionError(FIXTURE + " " + key + ": " + res.get(key).getAsString());
            }
        }
        equal(true, res.get("strict").getAsBoolean(), "strict");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        equal("step 14 (pass)", last.get("checkpoint").getAsString(), "last checkpoint");
        equal(19, last.getAsJsonArray("players").get(1).getAsJsonObject().get("life").getAsInt(),
                "PlayerB life after the trigger");
        equal(1, permanent(last, "Shield Sphere").get("damage").getAsInt(), "Shield Sphere damage");
        equal(2, permanent(last, "Grist, the Hunger Tide").getAsJsonObject("counters").get("loyalty").getAsInt(),
                "Grist loyalty");
        System.out.println("PASS Volatile Arsonist's creature, player and planeswalker targets each took 1 (PlayerB 19)");
    }
}
