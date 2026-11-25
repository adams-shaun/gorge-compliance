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

/** Stifle ("Counter target activated or triggered ability") cast at Dark
 * Sphere's activated ability (xproof xs:XP:stifle:cast:s1; xpt-4eded72b). A
 * cast step's ability ref ("p1:ability:Dark Sphere") reached XMage as the raw
 * inline "Cast Stifle$target=ability:Dark Sphere" command, which names no card,
 * so the cast failed "Can't find ability to activate command". The cast must
 * queue the ref through the same pending-ability alias binding activations use:
 * Stifle resolves, counters Dark Sphere's ability, and the stack is empty after
 * it. Needs the out-of-tree build and its card database, like a full replay. */
public final class ScenarioReplayCastAbilityTargetTest {
    private static final String FIXTURE = "cast-ability-target-stifle.jsonl";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) throws Exception {
        // The routing predicate: any ability ref among the targets (not just a lone one).
        equal(true, ScenarioReplay.namesStackAbility(java.util.List.of("p1:ability:Dark Sphere")), "lone ability ref");
        equal(true, ScenarioReplay.namesStackAbility(java.util.List.of("p0", "p1:ability:Dark Sphere#2")), "ability ref among two");
        equal(false, ScenarioReplay.namesStackAbility(java.util.List.of("p1", "p1:Dark Sphere")), "seat and card refs");
        equal(false, ScenarioReplay.namesStackAbility(java.util.List.of()), "no targets");

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
                "stack after Stifle resolves (Dark Sphere's ability left means it was not countered)");
        equal("[\"Stifle\"]",
                last.getAsJsonArray("players").get(0).getAsJsonObject().getAsJsonArray("graveyard").toString(),
                "p0 graveyard (Stifle, resolved)");
        System.out.println("PASS Stifle countered Dark Sphere's activated ability");
    }
}
