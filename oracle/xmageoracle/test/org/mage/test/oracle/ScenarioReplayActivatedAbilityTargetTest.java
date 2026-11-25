package org.mage.test.oracle;

import com.google.gson.JsonArray;
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

/** Azorius Guildmage's "{2}{U}: Counter target activated ability" aimed at
 * Dark Sphere's activated ability (xproof xs:XP:azorius-guildmage:f0a1:s1;
 * xpt-7271607a). The target is TargetActivatedAbility, which extends
 * TargetObject rather than TargetStackObject, so the base TestPlayer's stack
 * branch never matches the queued "@p1:ability:Dark Sphere~1" alias and fails
 * "Targets list was setup by addTarget ... but not used". The driver's own
 * chooseTarget hook must bind the alias to the pending ability: the Guildmage's
 * ability resolves, counters Dark Sphere's, and the stack is empty after it
 * (Dark Sphere's ability left on the stack means the counter was not aimed at
 * it). Needs the out-of-tree build and its card database, like a full replay. */
public final class ScenarioReplayActivatedAbilityTargetTest {
    private static final String FIXTURE = "activated-ability-target-azorius-guildmage.jsonl";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) throws Exception {
        // The class predicate: TargetActivatedAbility is not a TargetStackObject.
        equal(true, ScenarioReplay.isActivatedAbilityTarget(new mage.target.common.TargetActivatedAbility()),
                "TargetActivatedAbility");
        equal(false, ScenarioReplay.isActivatedAbilityTarget(new mage.target.TargetStackObject()),
                "TargetStackObject");

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
        equal("step 8 (pass)", last.get("checkpoint").getAsString(), "last checkpoint");
        equal("[]", last.getAsJsonArray("stack").toString(),
                "stack after the Guildmage's ability resolves (Dark Sphere's ability left means it was not countered)");
        equal("[\"Dark Sphere\"]",
                last.getAsJsonArray("players").get(1).getAsJsonObject().getAsJsonArray("graveyard").toString(),
                "p1 graveyard (Dark Sphere, sacrificed to its own cost)");
        System.out.println("PASS Azorius Guildmage countered Dark Sphere's activated ability");
    }
}
