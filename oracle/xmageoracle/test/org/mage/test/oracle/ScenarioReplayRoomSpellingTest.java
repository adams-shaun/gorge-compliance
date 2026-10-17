package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.UUID;
import mage.cards.CardSetInfo;
import mage.constants.Rarity;
import sun.misc.Unsafe;

/** The snapshot spelling rewrite for Rooms (CR 709) against split cards,
 * through the production gorgeSpellings walk, without H2 or a game. Run
 * scripts/xmage-oracle-test-split-room.sh.
 *
 * Two shapes gorge reports by the Room rules and the driver used to collapse:
 *  - a Room with both doors unlocked is named "Left // Right" on the
 *    battlefield (CR 709.5), gorge's bound name, so a permanent's whole name
 *    is never rewritten to its front half;
 *  - a Room spell has only the characteristics of the door it was cast as
 *    (CR 709.3b), so a back-door spell keeps the door's name, while a
 *    non-Room split card's face-1 spell still reads as its front face (gorge
 *    names that spell by its setup ref). */
public final class ScenarioReplayRoomSpellingTest {
    public static final class Driver extends ScenarioReplay {
        java.util.Set<String> rooms;

        @Override
        boolean cardIsRoom(String xmageCardName) {
            return rooms.contains(xmageCardName);
        }
    }

    private static Driver driver(String gorgeName, String xmageName, String... rooms) throws Exception {
        // The superclass constructor scans CardRepository (H2); skip it and
        // set only the two spelling fields the rewrite reads.
        Field f = Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        Driver d = (Driver) ((Unsafe) f.get(null)).allocateInstance(Driver.class);
        d.rooms = java.util.Set.of(rooms);
        set(d, "gorgeName", gorgeName);
        set(d, "xmageName", xmageName);
        return d;
    }

    private static void set(ScenarioReplay d, String name, Object value) throws Exception {
        Field f = ScenarioReplay.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(d, value);
    }

    private static JsonObject spell(Driver d, JsonObject snap) throws Exception {
        Method m = ScenarioReplay.class.getDeclaredMethod("gorgeSpellings", JsonElement.class);
        m.setAccessible(true);
        return (JsonObject) m.invoke(d, snap);
    }

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    /** A snapshot carrying name in every place one can appear: seat 0's hand
     * and graveyard, a permanent and an Aura attached to it, and a spell and
     * an ability on the stack. */
    private static JsonObject snapshot(String hand, String perm, String spell, String abilitySource) {
        JsonObject p0 = new JsonObject();
        JsonArray h = new JsonArray();
        h.add(hand);
        p0.add("hand", h);
        JsonArray gy = new JsonArray();
        gy.add(hand);
        p0.add("graveyard", gy);
        JsonArray players = new JsonArray();
        players.add(p0);
        JsonObject room = new JsonObject();
        room.addProperty("name", perm);
        JsonArray types = new JsonArray();
        types.add("Enchantment");
        types.add("Room");
        room.add("types", types);
        JsonObject aura = new JsonObject();
        aura.addProperty("name", "Pacifism");
        aura.addProperty("attached_to", perm);
        JsonArray perms = new JsonArray();
        perms.add(room);
        perms.add(aura);
        JsonObject sp = new JsonObject();
        sp.addProperty("kind", "spell");
        sp.addProperty("source", spell);
        JsonObject ab = new JsonObject();
        ab.addProperty("kind", "ability");
        ab.addProperty("source", abilitySource);
        JsonArray stack = new JsonArray();
        stack.add(sp);
        stack.add(ab);
        JsonObject s = new JsonObject();
        s.add("players", players);
        s.add("permanents", perms);
        s.add("stack", stack);
        return s;
    }

    private static String hand(JsonObject s) {
        return s.getAsJsonArray("players").get(0).getAsJsonObject().getAsJsonArray("hand").get(0).getAsString();
    }

    private static String graveyard(JsonObject s) {
        return s.getAsJsonArray("players").get(0).getAsJsonObject().getAsJsonArray("graveyard").get(0)
                .getAsString();
    }

    private static String perm(JsonObject s, int i, String key) {
        return s.getAsJsonArray("permanents").get(i).getAsJsonObject().get(key).getAsString();
    }

    private static String stack(JsonObject s, int i) {
        return s.getAsJsonArray("stack").get(i).getAsJsonObject().get("source").getAsString();
    }

    public static void main(String[] args) throws Exception {
        String theater = "Dazzling Theater // Prop Room";

        // A probe Room (not the card under test: Balemurk Leech's "whenever
        // you fully unlock a Room" scenario) with both doors unlocked keeps
        // its CR 709.5 whole name on the battlefield and as an Aura's host;
        // the same card in hand or graveyard still reads as its front face,
        // the way gorge names a card outside the battlefield.
        JsonObject probe = spell(driver("Balemurk Leech", "", theater),
                snapshot(theater, theater, "Prop Room", theater));
        equal(theater, perm(probe, 0, "name"), "fully unlocked probe Room permanent");
        equal(theater, perm(probe, 1, "attached_to"), "Aura host that is a fully unlocked Room");
        equal("Pacifism", perm(probe, 1, "name"), "ordinary permanent name");
        equal("Dazzling Theater", hand(probe), "probe Room in hand");
        equal("Dazzling Theater", graveyard(probe), "probe Room in graveyard");
        equal("Prop Room", stack(probe, 0), "probe Room cast as its back door");
        equal("Dazzling Theater", stack(probe, 1), "ability source keeps the object rule");
        System.out.println("PASS a fully unlocked probe Room keeps \"" + theater + "\" on the battlefield");

        // The card under test is a Room: fully unlocked on the battlefield it
        // is the whole name too (NOT its front-face scenario spelling), and a
        // spell cast as its back door keeps the door's name (CR 709.3b).
        String pool = "Bottomless Pool // Locker Room";
        Driver underTest = driver("Bottomless Pool", pool, pool);
        JsonObject room = spell(underTest, snapshot(pool, pool, "Locker Room", pool));
        equal(pool, perm(room, 0, "name"), "fully unlocked Room under test");
        equal(pool, perm(room, 1, "attached_to"), "Aura host that is the Room under test");
        equal("Bottomless Pool", hand(room), "Room under test in hand");
        equal("Locker Room", stack(room, 0), "Room under test cast as its back door");
        equal("Bottomless Pool", stack(room, 1), "ability source of the Room under test");
        // One door unlocked: each door's name is the permanent's name.
        JsonObject back = spell(underTest, snapshot(pool, "Locker Room", "Bottomless Pool", "Locker Room"));
        equal("Locker Room", perm(back, 0, "name"), "back-door-only Room permanent");
        equal("Bottomless Pool", stack(back, 0), "Room under test cast as its front door");
        JsonObject front = spell(underTest, snapshot(pool, "Bottomless Pool", "Bottomless Pool", pool));
        equal("Bottomless Pool", perm(front, 0, "name"), "front-door-only Room permanent");
        System.out.println("PASS the Room under test: whole name on the battlefield, door name on the stack");

        // A non-Room split card under test keeps the face-1 alias: gorge
        // names that spell by its setup ref (the front face), and the whole
        // object name reads as the front in every zone.
        String cease = "Cease // Desist";
        JsonObject split = spell(driver("Cease", cease), snapshot(cease, "Grizzly Bears", "Desist", cease));
        equal("Cease", stack(split, 0), "split card cast as its second half");
        equal("Cease", hand(split), "split card in hand");
        equal("Cease", graveyard(split), "split card in graveyard");
        equal("Grizzly Bears", perm(split, 0, "name"), "ordinary permanent");
        JsonObject splitWhole = spell(driver("Cease", cease), snapshot(cease, "Grizzly Bears", cease, cease));
        equal("Cease", stack(splitWhole, 0), "split spell named whole");
        System.out.println("PASS a non-Room split card's face-1 spell still aliases to the front face");

        // A plain spelling alias (Forge "Dain", XMage "Dain" without the
        // accent) still applies to a permanent's name.
        JsonObject dain = spell(driver("Dáin Ironfoot", "Dain Ironfoot"),
                snapshot("Dain Ironfoot", "Dain Ironfoot", "Dain Ironfoot", "Dain Ironfoot"));
        equal("Dáin Ironfoot", perm(dain, 0, "name"), "spelling alias on the battlefield");
        equal("Dáin Ironfoot", stack(dain, 0), "spelling alias on the stack");
        equal("Dáin Ironfoot", hand(dain), "spelling alias in hand");
        System.out.println("PASS the card under test's spelling alias holds in every field");

        // The static rules the walk dispatches to.
        equal("Bottomless Pool", ScenarioReplay.gorgeSpellingRule("Bottomless Pool", pool, false, "Locker Room"),
                "non-Room back-half spell alias");
        equal("Locker Room", ScenarioReplay.gorgeSpellingRule("Bottomless Pool", pool, true, "Locker Room"),
                "Room back-door spell");
        equal("Bottomless Pool", ScenarioReplay.gorgeSpellingRule("Bottomless Pool", pool, true, pool),
                "whole spell name of the card under test");
        equal(pool, ScenarioReplay.gorgePermanentSpellingRule("Bottomless Pool", pool, pool),
                "whole permanent name of the card under test");
        equal(theater, ScenarioReplay.gorgePermanentSpellingRule("", "", theater), "whole probe permanent name");
        equal("Grizzly Bears", ScenarioReplay.gorgePermanentSpellingRule("", "", "Grizzly Bears"),
                "ordinary permanent name");

        // Room-ness is the card's XMage class: a RoomCard, not a SplitCard.
        equal(true, ScenarioReplay.isRoomCard(new mage.cards.b.BottomlessPoolLockerRoom(UUID.randomUUID(),
                new CardSetInfo(pool, "DSK", "1", Rarity.UNCOMMON))), "Bottomless Pool is a Room");
        equal(true, ScenarioReplay.isRoomCard(new mage.cards.d.DazzlingTheaterPropRoom(UUID.randomUUID(),
                new CardSetInfo(theater, "DSK", "1", Rarity.RARE))), "Dazzling Theater is a Room");
        equal(false, ScenarioReplay.isRoomCard(new mage.cards.c.CeaseDesist(UUID.randomUUID(),
                new CardSetInfo(cease, "MKM", "1", Rarity.UNCOMMON))), "Cease // Desist is not a Room");
        equal(false, ScenarioReplay.isRoomCard(null), "an unresolved name is not a Room");
        System.out.println("PASS Room detection by XMage card class");
    }
}
