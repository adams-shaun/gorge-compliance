import gzip
import hashlib
import json
import os
import unittest

from .helpers import Base, GOLDEN, HERE, ROOT
from .test_build import write

FIXTURE = os.path.join(HERE, "fixtures", "mtgjson-allprintings.json")
GOLDEN_DIR = os.path.join(GOLDEN, "catalogue")


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def shards(root):
    """{rel: text} of every data shard under a clone (no .git/.github, no non-.tsv)."""
    out = {}
    for dp, dn, fns in os.walk(root):
        dn[:] = sorted(d for d in dn if d not in (".git", ".github"))
        for fn in sorted(fns):
            if fn.endswith(".tsv"):
                p = os.path.join(dp, fn)
                with open(p, encoding="utf-8", newline="") as f:
                    out[os.path.relpath(p, root).replace(os.sep, "/")] = f.read()
    return out


def fixture_texts():
    with open(FIXTURE, encoding="utf-8") as f:
        data = json.load(f)
    out = []

    def walk(value):
        if isinstance(value, dict):
            if isinstance(value.get("text"), str):
                out.append(value["text"])
            for v in value.values():
                walk(v)
        elif isinstance(value, list):
            for v in value:
                walk(v)

    walk(data)
    return out


class Catalogue(Base):
    def empty_manifests(self):
        d = os.path.join(self.tmp, "manifests-empty")
        os.makedirs(d, exist_ok=True)
        return d

    def ingest(self, home, fixture=FIXTURE, manifests=None):
        if self.cli(home, "init").returncode != 0:
            self.fail("carddb init failed")
        args = ["ingest", "catalogue", "--mtgjson", fixture, "--manifests", manifests or self.empty_manifests()]
        return self.cli(home, *args)

    def clone(self, home):
        return os.path.join(home, "public")

    def golden(self, got):
        if os.environ.get("CARDDB_UPDATE_GOLDEN") == "1":
            import shutil
            shutil.rmtree(GOLDEN_DIR, ignore_errors=True)
            os.makedirs(GOLDEN_DIR)
            for rel, text in sorted(got.items()):
                write(os.path.join(GOLDEN_DIR, rel), text)
            return
        want = {}
        for dp, dn, fns in os.walk(GOLDEN_DIR):
            dn.sort()
            for fn in sorted(fns):
                p = os.path.join(dp, fn)
                rel = os.path.relpath(p, GOLDEN_DIR).replace(os.sep, "/")
                with open(p, encoding="utf-8", newline="") as f:
                    want[rel] = f.read()
        self.assertEqual(sorted(got), sorted(want), "golden shard set differs (CARDDB_UPDATE_GOLDEN=1 rewrites it)")
        for rel in sorted(got):
            self.assertEqual(got[rel], want[rel], "golden %s differs (CARDDB_UPDATE_GOLDEN=1 rewrites it)" % rel)

    def test_golden_shards_and_no_text(self):
        home = os.path.join(self.tmp, "m1")
        r = self.ingest(home)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("9 cards, 12 printings, 8 sets, 78 legality rows, 6 faces", r.stdout)
        self.assertIn("cross-check 0 manifest set(s): 0 mismatch(es)", r.stdout)
        got = shards(self.clone(home))
        self.golden(got)
        joined = "\n".join(got.values())
        for text in fixture_texts():
            self.assertNotIn(text, joined)
        self.assertNotIn("oracle_text", joined)
        # full sha256 values computed independently of normalise_text: the single-face
        # card, a split card's face join, a same-name DFC's face join, and a CRLF text
        # with one trailing newline
        self.assertIn("\t" + hashlib.sha256(
            b"Lightning Bolt deals 3 damage to any target.").hexdigest() + "\t", joined)
        self.assertIn("\t" + hashlib.sha256(
            b"Fire deals 2 damage divided as you choose among one or two targets.\n//\n"
            b"Tap target permanent.\nDraw a card.").hexdigest() + "\t", joined)
        self.assertIn("\t" + hashlib.sha256(
            b"Whenever a player wins a coin flip, draw a card.\n//\n"
            b"Whenever a player loses a coin flip, scry 1.").hexdigest() + "\t", joined)
        self.assertIn("\t" + hashlib.sha256(
            b"Flying\nVigilance (Attacking doesn't cause this creature to tap.)").hexdigest() + "\t", joined)
        # MDFC and split keep both faces' cost/type/PT, but no Oracle text
        faces = read(os.path.join(self.clone(home), "faces", "ae.tsv"))
        self.assertIn("\ta\t{1}{R}\tInstant\t-\t-", faces)
        self.assertIn("\tb\t{1}{U}\tInstant\t-\t-", faces)
        emeria = read(os.path.join(self.clone(home), "cards", "6e.tsv"))
        self.assertIn("Sorcery // Land", emeria)
        fire = read(os.path.join(self.clone(home), "cards", "ae.tsv"))
        self.assertIn("Instant // Instant", fire)
        self.assertIn("{1}{R} // {1}{U}", fire)
        # a banned card keeps its banned legality, a not_legal one is absent
        lotus = read(os.path.join(self.clone(home), "legality", "50.tsv"))
        self.assertIn("commander\tbanned", lotus)
        self.assertNotIn("not_legal", lotus)
        # first printing facts
        bolt = read(os.path.join(self.clone(home), "cards", "44.tsv"))
        self.assertIn("\t1993-08-05\tlea\ttrue", bolt)
        # a child set's parent is stored in the same case as set_code, so the link joins
        sets = read(os.path.join(self.clone(home), "sets.tsv"))
        head = sets.splitlines()[1].split("\t")
        rows = [dict(zip(head, l.split("\t"))) for l in sets.splitlines()[2:] if l]
        codes = {r["set_code"] for r in rows}
        self.assertIn("p10e", codes)
        parent = next(r for r in rows if r["set_code"] == "p10e")["parent_code"]
        self.assertEqual(parent, "10e")
        for r in rows:
            if r["parent_code"] != "-":
                self.assertIn(r["parent_code"], codes, r)
        # a same-name DFC keeps both faces: the face key is the side, not the face name
        same = read(os.path.join(self.clone(home), "faces", "bb.tsv"))
        same_rows = [l for l in same.splitlines() if l.startswith("bb")]
        self.assertEqual(len(same_rows), 2, same)
        self.assertIn("\ta\t{3}{U}\tLegendary Creature — Human Wizard\t1\t3", same)
        self.assertIn("\tb\t-\tLegendary Creature — Human Wizard\t2\t4", same)
        # a meld set is three cards, not two two-faced ones: the result is its own card
        # row with its own oracle_sha, and the components carry no faces
        emn = [l.split("\t") for l in read(os.path.join(self.clone(home), "cards", "e0.tsv")).splitlines()[2:] if l]
        by_name = {r[1]: r for r in emn}
        self.assertEqual(sorted(by_name), ["Brisela, Voice of Nightmares", "Bruna, the Fading Light",
                                           "Gisela, the Broken Blade"])
        self.assertEqual(len({r[0] for r in emn}), 3)
        brisela = by_name["Brisela, Voice of Nightmares"]
        self.assertEqual(brisela[2], "meld")
        self.assertEqual(brisela[5], "Legendary Creature — Eldrazi Angel")
        self.assertEqual(by_name["Gisela, the Broken Blade"][5], "Legendary Creature — Angel Horror")
        self.assertEqual(brisela[9], hashlib.sha256(
            b"Flying, first strike, vigilance, lifelink\n"
            b"Your opponents can't cast spells with mana value 3 or less.").hexdigest())
        self.assertEqual(by_name["Gisela, the Broken Blade"][9], hashlib.sha256(
            b"Flying, first strike, lifelink\n(Melds with Bruna, the Fading Light.)").hexdigest())
        self.assertFalse(os.path.exists(os.path.join(self.clone(home), "faces", "e0.tsv")))
        emn_printings = read(os.path.join(self.clone(home), "printings", "emn.tsv"))
        self.assertIn("\t28b\t", emn_printings)

    def test_rerun_changes_no_byte(self):
        home = os.path.join(self.tmp, "m1")
        r = self.ingest(home)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        before = shards(self.clone(home))
        os.environ["CARDDB_NOW"] = "2026-11-20"  # a different ambient day must not change a byte
        r = self.cli(home, "ingest", "catalogue", "--mtgjson", FIXTURE, "--manifests", self.empty_manifests())
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("(0 rows changed)", r.stdout)
        self.assertEqual(shards(self.clone(home)), before)

    def test_a_changed_snapshot_stamps_the_snapshot_date_not_the_ambient_one(self):
        home = os.path.join(self.tmp, "m1")
        r = self.ingest(home)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        with open(FIXTURE, encoding="utf-8") as f:
            fixture = json.load(f)
        fixture["data"]["LEA"]["cards"][0]["rarity"] = "mythic"  # a real data change: printings is rewritten
        path = os.path.join(self.tmp, "changed.json")
        write(path, json.dumps(fixture) + "\n")
        os.environ["CARDDB_NOW"] = "2026-11-20"
        r = self.cli(home, "ingest", "catalogue", "--mtgjson", path, "--manifests", self.empty_manifests())
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertNotIn("(0 rows changed)", r.stdout)
        got = shards(self.clone(home))
        self.assertNotEqual(got["printings/lea.tsv"], read(os.path.join(GOLDEN_DIR, "printings", "lea.tsv")))
        self.assertIn("mtgjson\tprintings\t5.3.0+20261119\t2026-11-19", got["_meta/writers.tsv"])
        self.assertNotIn("2026-11-20", "\n".join(got.values()))

    def test_same_input_under_two_ambient_dates_is_byte_identical(self):
        trees = []
        for i, day in enumerate(("2026-11-19", "2026-11-20")):
            os.environ["CARDDB_NOW"] = day
            home = os.path.join(self.tmp, "d%d" % i)
            r = self.ingest(home)
            self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
            trees.append(shards(self.clone(home)))
        self.assertEqual(trees[0], trees[1])

    def test_cross_check_reports_and_does_not_overwrite(self):
        home = os.path.join(self.tmp, "m1")
        manifests = os.path.join(self.tmp, "manifests")
        os.makedirs(manifests)
        lea = {"code": "LEA", "name": "Limited Edition Alpha", "released": "1993-08-05", "set_type": "CORE",
               "xmage_ref": "x", "cards": [
                   {"name": "Black Lotus", "numbers": ["233"], "rarity": "RARE"},
                   {"name": "Lightning Bolt", "numbers": ["161"], "rarity": "COMMON"},
                   {"name": "Mox Emerald", "numbers": ["234"], "rarity": "RARE"},
               ]}
        zzz = {"code": "ZZZ", "name": "Nowhere", "released": "2001-01-01", "set_type": "CORE", "xmage_ref": "x",
               "cards": [{"name": "Nothing", "numbers": ["1"], "rarity": "COMMON"}]}
        write(os.path.join(manifests, "LEA.json"), json.dumps(lea) + "\n")
        write(os.path.join(manifests, "ZZZ.json"), json.dumps(zzz) + "\n")
        before = {fn: read(os.path.join(manifests, fn)) for fn in sorted(os.listdir(manifests))}
        r = self.ingest(home, manifests=manifests)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("cross-check 2 manifest set(s): 4 mismatch(es)", r.stdout)
        self.assertIn("manifest LEA: 1 card(s) in the manifest but not in MTGJSON: Mox Emerald", r.stdout)
        self.assertIn("manifest LEA: 1 card(s) in MTGJSON but not in the manifest: Serra Angel", r.stdout)
        self.assertIn("manifest LEA: 1 card(s) with different collector numbers: Black Lotus", r.stdout)
        self.assertIn("manifest ZZZ: set not in MTGJSON", r.stdout)
        after = {fn: read(os.path.join(manifests, fn)) for fn in sorted(os.listdir(manifests))}
        self.assertEqual(after, before)

    def test_cross_check_normalises_alternate_art_number(self):
        home = os.path.join(self.tmp, "m1")
        manifests = os.path.join(self.tmp, "manifests")
        os.makedirs(manifests)
        p10e = {"code": "P10E", "name": "Tenth Edition Promos", "released": "2007-07-13", "set_type": "PROMO",
                "xmage_ref": "x", "cards": [
                    {"name": "Lightning Bolt", "numbers": ["161*"], "rarity": "COMMON"},
                ]}
        write(os.path.join(manifests, "P10E.json"), json.dumps(p10e) + "\n")
        r = self.ingest(home, manifests=manifests)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        # MTGJSON writes the alternate-art marker as ★, the manifests write *: same printing
        self.assertIn("cross-check 1 manifest set(s): 0 mismatch(es)", r.stdout)

    def test_curated_lists_cite_wotc_per_row(self):
        for name in ("game-changers", "bracket-flags"):
            path = os.path.join(ROOT, "carddb", "curated", name + ".tsv")
            lines = read(path).splitlines()
            self.assertEqual(lines[0], "#carddb curated/%s v1" % name)
            names = lines[1].split("\t")
            rows = [l.split("\t") for l in lines[2:] if l]
            self.assertTrue(rows, name)
            for cells in rows:
                row = dict(zip(names, cells))
                self.assertRegex(row["source"], r"^https://magic\.wizards\.com/")
                self.assertRegex(row["cited"], r"^[0-9]{4}-[0-9]{2}-[0-9]{2}$")

    def test_gzipped_allprintings_input(self):
        home = os.path.join(self.tmp, "m1")
        path = os.path.join(self.tmp, "allprintings.json.gz")
        with gzip.open(path, "wt", encoding="utf-8") as f:
            f.write(read(FIXTURE))
        r = self.ingest(home, fixture=path)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("9 cards, 12 printings, 8 sets, 78 legality rows, 6 faces", r.stdout)

    def test_digital_only_card_has_no_paper_printing(self):
        home = os.path.join(self.tmp, "m1")
        oid = "aa" + "0" * 30
        fixture = {
            "meta": {"date": "2026-11-19", "version": "5.3.0+20261119"},
            "data": {"Y22": {"code": "Y22", "name": "Alchemy 2022", "releaseDate": "2022-07-07", "type": "alchemy",
                             "cards": [{
                                 "name": "A-Test Spell", "uuid": "11111111-2222-3333-4444-555555555555",
                                 "number": "1", "rarity": "rare", "layout": "normal", "manaCost": "{1}{U}",
                                 "manaValue": 2, "type": "Instant", "colors": ["U"], "colorIdentity": ["U"],
                                 "availability": ["arena"], "text": "Draw a card.",
                                 "legalities": {"commander": "Not Legal", "historic": "Legal"},
                                 "identifiers": {"scryfallOracleId": oid},
                             }]}},
        }
        path = os.path.join(self.tmp, "digital.json")
        write(path, json.dumps(fixture) + "\n")
        r = self.ingest(home, fixture=path)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        row = [l for l in read(os.path.join(self.clone(home), "cards", "aa.tsv")).split("\n") if l.startswith(oid)][0]
        self.assertTrue(row.endswith("\t-\t-\tfalse"), row)
        self.assertEqual(read(os.path.join(self.clone(home), "legality", "aa.tsv")).split("\n")[2:],
                         [oid + "\thistoric\tlegal", ""])
        self.assertFalse(os.path.exists(os.path.join(self.clone(home), "faces", "aa.tsv")))

    def test_a_full_ingest_removes_stale_rows(self):
        home = os.path.join(self.tmp, "m1")
        r = self.ingest(home)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        clone = self.clone(home)
        self.assertIn("\tmodern\tlegal", read(os.path.join(clone, "legality", "44.tsv")))
        with open(FIXTURE, encoding="utf-8") as f:
            fixture = json.load(f)
        # a format turns not_legal on the printing the card row is built from (M10, the newest)
        for c in fixture["data"]["M10"]["cards"]:
            c["legalities"]["modern"] = "Not Legal"
        path = os.path.join(self.tmp, "turned.json")
        write(path, json.dumps(fixture) + "\n")
        r = self.ingest(home, fixture=path)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        legality = read(os.path.join(clone, "legality", "44.tsv"))
        self.assertNotIn("\tmodern\t", legality)
        self.assertIn("\tcommander\tlegal", legality)
        self.assertTrue(os.path.exists(os.path.join(clone, "printings", "m10.tsv")))
        # and a printing disappears
        del fixture["data"]["M10"]  # no second Lightning Bolt printing
        path = os.path.join(self.tmp, "smaller.json")
        write(path, json.dumps(fixture) + "\n")
        r = self.ingest(home, fixture=path)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertFalse(os.path.exists(os.path.join(clone, "printings", "m10.tsv")))
        self.assertTrue(os.path.exists(os.path.join(clone, "cards", "44.tsv")))  # still printed in LEA
        self.assertNotIn("m10", read(os.path.join(clone, "printings", "lea.tsv")))

    def test_cross_check_finds_meld_cards_by_name_and_number(self):
        home = os.path.join(self.tmp, "m1")
        manifests = os.path.join(self.tmp, "manifests")
        os.makedirs(manifests)
        emn = {"code": "EMN", "name": "Eldritch Moon", "released": "2016-07-22", "set_type": "EXPANSION",
               "xmage_ref": "x", "cards": [
                   {"name": "Gisela, the Broken Blade", "numbers": ["28"], "rarity": "MYTHIC"},
                   {"name": "Bruna, the Fading Light", "numbers": ["15"], "rarity": "MYTHIC"},
                   {"name": "Brisela, Voice of Nightmares", "numbers": ["28b"], "rarity": "MYTHIC"},
               ]}
        write(os.path.join(manifests, "EMN.json"), json.dumps(emn) + "\n")
        r = self.ingest(home, manifests=manifests)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        # the meld result is a card of MTGJSON: neither absent nor an extra, numbers agree
        self.assertIn("cross-check 1 manifest set(s): 0 mismatch(es)", r.stdout)
        # a manifest without the meld result reports it as a MTGJSON extra
        emn["cards"].pop()
        write(os.path.join(manifests, "EMN.json"), json.dumps(emn) + "\n")
        r = self.cli(home, "ingest", "catalogue", "--mtgjson", FIXTURE, "--manifests", manifests)
        self.assertIn("manifest EMN: 1 card(s) in MTGJSON but not in the manifest: Brisela, Voice of Nightmares",
                      r.stdout)


if __name__ == "__main__":
    unittest.main()
