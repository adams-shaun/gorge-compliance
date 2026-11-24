package org.mage.test.oracle;

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
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import mage.choices.Choice;
import mage.choices.ChoiceImpl;

/** A "choose a number" pick goes out as "X=<n>" (getAmount's answer), but
 * Shapeshifter asks it through a Choice dialog whose entries are "0".."7"
 * (ShapeshifterEffect), so TestPlayer threw "Choice key [X=0] not found".
 * The scripted Choice path consumes "X=<n>" as "<n>" when the dialog offers
 * "<n>" and not "X=<n>". The unit half needs no game; the replay half casts
 * Shapeshifter with a queued X=3 and checks it entered as a 3/4. Run
 * scripts/xmage-oracle-test-number-choice.sh; the replay needs the
 * out-of-tree build's card database, like a full replay. */
public final class ScenarioReplayNumberChoiceTest {
    private static final String FIXTURE = "number-choice-shapeshifter-x3.jsonl";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    private static Method rewrite() throws Exception {
        Class<?> c = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method m = c.getDeclaredMethod("numberChoiceAnswer", Choice.class, List.class);
        m.setAccessible(true);
        return m;
    }

    private static Choice plain(String... entries) {
        Choice c = new ChoiceImpl(false);
        c.setChoices(new LinkedHashSet<>(Arrays.asList(entries)));
        return c;
    }

    private static Choice keyed(String... keyText) {
        Choice c = new ChoiceImpl(false);
        Map<String, String> keys = new LinkedHashMap<>();
        for (int i = 0; i < keyText.length; i += 2) {
            keys.put(keyText[i], keyText[i + 1]);
        }
        c.setKeyChoices(keys);
        return c;
    }

    private static List<String> after(Method m, Choice choice, String... queue) throws Exception {
        List<String> q = new ArrayList<>(Arrays.asList(queue));
        m.invoke(null, choice, q);
        return q;
    }

    private static void units() throws Exception {
        Method m = rewrite();
        Choice numbers = plain("0", "1", "2", "3", "4", "5", "6", "7");
        equal(Arrays.asList("3", "later"), after(m, numbers, "X=3", "later"), "plain numeric dialog");
        equal(Arrays.asList("0"), after(m, numbers, "X=0"), "plain numeric dialog, zero");
        // A number the dialog does not offer keeps TestPlayer's own error.
        equal(Arrays.asList("X=9"), after(m, numbers, "X=9"), "number outside the dialog");
        // A dialog that really offers "X=<n>" keeps the answer.
        equal(Arrays.asList("X=2"), after(m, plain("X=1", "X=2", "2"), "X=2"), "dialog offering X=<n>");
        // Keyed dialogs: by key or by key text.
        equal(Arrays.asList("4"), after(m, keyed("4", "Four", "5", "Five"), "X=4"), "keyed by key");
        equal(Arrays.asList("6"), after(m, keyed("a", "6", "b", "7"), "X=6"), "keyed by text");
        // Only the head answer, and only an X=<n> answer, is touched.
        equal(Arrays.asList("Red", "X=3"), after(m, numbers, "Red", "X=3"), "non-number head");
        equal(new ArrayList<String>(), after(m, numbers), "empty queue");
        System.out.println("PASS X=<n> maps to <n> only for a dialog that offers <n> and not X=<n>");
    }

    private static void shapeshifter(Path dir) throws Exception {
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
        JsonObject last = res.getAsJsonArray("snapshots")
                .get(res.getAsJsonArray("snapshots").size() - 1).getAsJsonObject();
        String pt = null;
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject p = e.getAsJsonObject();
            if ("Shapeshifter".equals(p.get("name").getAsString())) {
                pt = p.get("pt").getAsString();
            }
        }
        equal("3/4", pt, "Shapeshifter's P/T after choosing 3");
        System.out.println("PASS Shapeshifter cast with a queued X=3 chose 3 (3/4), no Choice key error");
    }

    public static void main(String[] args) throws Exception {
        units();
        shapeshifter(Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures"));
    }
}
