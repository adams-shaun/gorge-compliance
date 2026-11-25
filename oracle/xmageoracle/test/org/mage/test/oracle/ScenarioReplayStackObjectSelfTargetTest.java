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

/** Katara's Reversal cast on an empty stack with only an untap target
 * (xproof xs:XP:katara-s-reversal:cast:s2). CR 115.5: a spell on the stack is
 * an illegal target for itself, so the "counter up to four target spells and/or
 * abilities" group has no legal target and only the untap group is asked.
 * XMage's TargetStackObject before the fork fix (adams-shaun/mage branch
 * fix/stackobject-self-target, docs/upstream-patches/xmage-stackobject-self-target.patch)
 * offered the Reversal as its own one candidate: the farm's driver then fails
 * "Targets list was setup by addTarget with [Memnite], but not used", and this
 * driver's lone-candidate rule takes the Reversal for the counter group, so it
 * counters itself. Multani's Presence ("Whenever a spell you've cast is
 * countered, draw a card") on p0's battlefield makes that visible: the unfixed
 * build leaves its trigger on the stack at the resolving pass, the fixed build
 * leaves the stack empty. Memnite starts tapped and the cast must untap it.
 * Needs the out-of-tree build at an XMAGE_REF that carries the fix, and its
 * card database, like a full replay. */
public final class ScenarioReplayStackObjectSelfTargetTest {
    private static final String FIXTURE = "stack-object-self-target-katara-s-reversal.jsonl";

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
        JsonObject cast = null;
        for (int i = 0; i < snaps.size(); i++) {
            JsonObject snap = snaps.get(i).getAsJsonObject();
            if ("step 6 (cast)".equals(snap.get("checkpoint").getAsString())) {
                cast = snap;
            }
        }
        if (cast == null) {
            throw new AssertionError("no snapshot at checkpoint \"step 6 (cast)\" in " + FIXTURE);
        }
        equal(true, permanent(cast, "Memnite").get("tapped").getAsBoolean(), "Memnite tapped before the resolve");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        equal("step 8 (pass)", last.get("checkpoint").getAsString(), "last checkpoint");
        equal(false, permanent(last, "Memnite").get("tapped").getAsBoolean(), "Memnite untapped by the resolve");
        equal("[\"Katara's Reversal\"]",
                last.getAsJsonArray("players").get(0).getAsJsonObject().getAsJsonArray("graveyard").toString(),
                "p0 graveyard");
        equal("[]", last.getAsJsonArray("stack").toString(),
                "stack after the resolve (a Multani's Presence trigger means the Reversal countered itself)");
        System.out.println("PASS Katara's Reversal on an empty stack resolved, untapped Memnite and did not counter itself");
    }
}
