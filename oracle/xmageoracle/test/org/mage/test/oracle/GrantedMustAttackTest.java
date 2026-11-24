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

/** A creature forced to attack by ANOTHER permanent's static: Goblin
 * Rabblemaster's "Other Goblin creatures you control attack each combat if
 * able" (XMage's AttacksIfAllEffect). XMage's Combat.selectAttackers runs
 * checkAttackRequirements first (CR 508.1d), so Goblin Spy and the Goblin
 * token are declared before TestPlayer reads the queued attack commands; the
 * name-based must-attack check only knew a card's own printed static, so
 * their commands were left over ("must have 0 actions but found 2"). Two
 * cases: Rabblemaster attacks beside them (selectAttackers runs), and only
 * the granted-forced Goblins attack (getAvailableAttackers is empty, so
 * selectAttackers never runs). Run scripts/xmage-oracle-test-granted-must-attack.sh;
 * it needs the out-of-tree build's card database, like a full replay. */
public final class GrantedMustAttackTest {
    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    private static JsonObject replay(Path fixture) throws Exception {
        String line = new String(Files.readAllBytes(fixture), StandardCharsets.UTF_8).trim();
        StringWriter out = new StringWriter();
        int n;
        try (BufferedReader in = new BufferedReader(new StringReader(line));
             PrintWriter writer = new PrintWriter(out)) {
            n = ScenarioReplay.replayLines(new ScenarioReplay(), in, writer);
        }
        equal(1, n, fixture.getFileName() + " scenario rows");
        return JsonParser.parseString(out.toString().trim()).getAsJsonObject();
    }

    private static void clean(JsonObject res, String what) {
        for (String key : new String[]{"harness", "error", "strict_miss"}) {
            if (res.has(key) && !res.get(key).isJsonNull()) {
                throw new AssertionError(what + " " + key + ": " + res.get(key).getAsString());
            }
        }
        equal(true, res.get("strict").getAsBoolean(), what + " strict");
    }

    /** The defender's life and the attacking Goblins at the combat-damage
     * checkpoint. */
    private static void combat(JsonObject res, String what, int defenderLife, String... attackers) {
        JsonObject at = null;
        for (JsonElement e : res.getAsJsonArray("snapshots")) {
            JsonObject s = e.getAsJsonObject();
            if ("COMBAT_DAMAGE".equals(s.get("step").getAsString())) {
                at = s;
                break;
            }
        }
        if (at == null) {
            throw new AssertionError(what + ": no combat-damage checkpoint");
        }
        equal(defenderLife, at.getAsJsonArray("players").get(1).getAsJsonObject().get("life").getAsInt(),
                what + " defender life");
        JsonArray perms = at.getAsJsonArray("permanents");
        StringBuilder attacking = new StringBuilder();
        for (JsonElement e : perms) {
            JsonObject p = e.getAsJsonObject();
            if (p.get("attacking").getAsBoolean()) {
                attacking.append(attacking.length() == 0 ? "" : ",").append(p.get("name").getAsString());
            }
        }
        equal(String.join(",", attackers), attacking.toString(), what + " attackers");
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");

        // Rabblemaster (2/2, +1/+0 per other attacking Goblin: 4) and two 1/1s.
        JsonObject all = replay(dir.resolve("granted-must-attack-rabblemaster.jsonl"));
        clean(all, "Rabblemaster with Spy and token");
        combat(all, "Rabblemaster with Spy and token", 14, "Goblin Rabblemaster", "Goblin Spy", "Goblin Token");
        System.out.println("PASS Rabblemaster, Goblin Spy and its token attack; no leftover attack command");

        // Only the granted-forced Goblins: selectAttackers never runs.
        JsonObject forced = replay(dir.resolve("granted-must-attack-only-forced.jsonl"));
        clean(forced, "granted-forced only");
        combat(forced, "granted-forced only", 18, "Goblin Spy", "Goblin Token");
        System.out.println("PASS only the granted-forced Goblin Spy and token attack; no leftover attack command");
    }
}
