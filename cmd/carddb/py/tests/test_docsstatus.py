"""CDB-37: the docs site's per-set status as cert_sets columns, and the export back to the docs-data shapes."""
import json
import os

from carddb import build as build_mod
from carddb import docsstatus
from carddb.store import StoreError
from .helpers import Base
from .test_cert import GORGE, PIN, cert_set_row, read

FORGE = "418ba5215fd4b17f6e0f3cb2ae9d93e4a1c5b7f0"
PINNED = "608a3e2c25fcdb472a8a2815fa22436491d2f416"

A_LINES = [
    {"set": "lea", "name": "Limited Edition Alpha", "set_type": "core", "released": "1993-08-05", "level": "A",
     "printed": True, "declared": "A", "cards": 10, "outstanding": 3, "xmage_verified": 4, "forge_only": 2, "hand": 1,
     "problem_cards": 2, "buckets": {"unsupported": 1, "not in corpus": 1}, "non_tournament": 1},
    {"set": "2xm", "name": "Double Masters", "set_type": "masters", "released": "2020-08-07", "level": "A",
     "printed": False, "cards": 5, "outstanding": 1, "problem_cards": 1, "buckets": {"no printed list": 1}},
]
B_LINES = [{"set": "lea", "name": "Limited Edition Alpha", "level": "B", "printed": True, "cards": 9,
            "outstanding": 2, "problem_cards": 2, "buckets": {"mismatch": 2}}]
META = {"commit": "abcdef123", "gorge_pin": PINNED, "forge_ref": FORGE, "date": "2026-11-20", "ratchet_failures": 1}


def write(tmp, name, text):
    p = os.path.join(tmp, name)
    with open(p, "w", encoding="utf-8") as f:
        f.write(text)
    return p


def jsonl(rows):
    return "progress line\n" + "".join(json.dumps(r) + "\n" for r in rows)


class DocsStatus(Base):
    def files(self, a=A_LINES, b=B_LINES, meta=META):
        return (write(self.tmp, "A.jsonl", jsonl(a)), write(self.tmp, "B.jsonl", jsonl(b)),
                write(self.tmp, "meta.json", json.dumps(meta)))

    def test_new_sets_get_a_queued_row_with_the_status_columns(self):
        a = self.machine("a", init=True)
        fa, fb, fm = self.files()
        changed, n = docsstatus.run(a, fa, fb, fm)
        self.assertEqual((changed, n), (2, 2))
        rows = {r["set_code"]: r for r in a.rows("cert_sets")}
        lea = rows["lea"]
        self.assertEqual((lea["state"], lea["writer"], lea["paper"], lea["playable"]), ("queued", "ci", "10", "8"))
        self.assertEqual((lea["gorge_sha"], lea["corpus_pin"], lea["measured"]), (PINNED[:12], FORGE[:10], "2026-11-20"))
        self.assertEqual((lea["printed"], lea["a_cards"], lea["b_cards"], lea["b_buckets"]), ("true", "10", "9", "mismatch=2;"))
        self.assertEqual(lea["a_buckets"], "not in corpus=1;unsupported=1;")
        self.assertEqual(lea["c0"], "-")
        self.assertEqual((rows["2xm"]["b_cards"], rows["2xm"]["printed"]), ("-", "false"))
        self.assertEqual((lea["status_commit"], lea["status_gorge"], lea["ratchet_failures"]), ("abcdef123", PINNED, "1"))

    def test_a_certified_set_keeps_its_certification_columns(self):
        a = self.machine("a", init=True)
        a.put("cert_sets", [cert_set_row("lea")], writer="cert")
        fa, fb, fm = self.files()
        docsstatus.run(a, fa, fb, fm)
        lea = {r["set_code"]: r for r in a.rows("cert_sets")}["lea"]
        self.assertEqual((lea["state"], lea["level"], lea["gorge_sha"], lea["corpus_pin"], lea["c3"], lea["paper"]),
                         ("certified", "C3", GORGE, PIN, "3", "10"))
        self.assertEqual((lea["a_cards"], lea["status_date"]), ("10", "2026-11-20"))

    def test_rerun_changes_nothing_and_a_set_without_b_drops_it(self):
        a = self.machine("a", init=True)
        fa, fb, fm = self.files()
        docsstatus.run(a, fa, fb, fm)
        before = self.snapshot(a.dir)
        self.assertEqual(docsstatus.run(a, fa, fb, fm)[0], 0)
        self.assertEqual(self.snapshot(a.dir), before)
        docsstatus.run(a, fa, None, fm)
        self.assertEqual({r["set_code"]: r for r in a.rows("cert_sets")}["lea"]["b_cards"], "-")

    def test_a_set_dropped_from_the_measurement_loses_its_status(self):
        a = self.machine("a", init=True)
        a.put("cert_sets", [cert_set_row("2xm")], writer="cert")
        fa, fb, fm = self.files()
        docsstatus.run(a, fa, fb, fm)
        rows = {r["set_code"]: r for r in a.rows("cert_sets")}
        self.assertEqual(set(rows), {"lea", "2xm"})
        self.assertEqual(rows["2xm"]["a_cards"], "5")
        a2 = write(self.tmp, "A2.jsonl", jsonl(A_LINES[1:]))  # lea dropped (status-only row), 2xm stays
        fm2 = write(self.tmp, "m2.json", json.dumps(dict(META, date="2026-11-21")))
        docsstatus.run(a, a2, None, fm2)
        rows = {r["set_code"]: r for r in a.rows("cert_sets")}
        self.assertEqual(set(rows), {"2xm"})  # the queued status-only lea row is gone
        a3 = write(self.tmp, "A3.jsonl", jsonl(A_LINES[:1]))  # 2xm dropped: the certified row stays, status cleared
        fm3 = write(self.tmp, "m3.json", json.dumps(dict(META, date="2026-11-22")))
        docsstatus.run(a, a3, None, fm3)
        rows = {r["set_code"]: r for r in a.rows("cert_sets")}
        self.assertEqual(set(rows), {"lea", "2xm"})
        x = rows["2xm"]
        self.assertEqual((x["state"], x["c3"]), ("certified", "3"))
        for c in docsstatus.STATUS_COLUMNS:
            self.assertEqual(x[c], "-", c)
        db = os.path.join(self.tmp, "stale.sqlite")
        build_mod.build(db, self.schema, a.dir, None, {})
        out = os.path.join(self.tmp, "stale")
        self.assertEqual(docsstatus.export(db, out), (1, 0))
        self.assertEqual([json.loads(l)["set"] for l in read(os.path.join(out, "status-A.jsonl")).splitlines()], ["lea"])

    def test_bad_inputs(self):
        a = self.machine("a", init=True)
        fa, fb, fm = self.files(meta=dict(META, commit="not hex"))
        with self.assertRaises(StoreError):
            docsstatus.run(a, fa, fb, fm)
        empty = write(self.tmp, "empty.jsonl", "nothing\n")
        with self.assertRaises(StoreError):
            docsstatus.run(a, empty, None, write(self.tmp, "m.json", json.dumps(META)))
        with self.assertRaises(StoreError):
            docsstatus.encode_buckets({"a=b": 1})

    def test_export_round_trips_through_the_built_sqlite(self):
        a = self.machine("a", init=True)
        a.put("sets", [{"set_code": "lea", "name": "Limited Edition Alpha", "release_date": "1993-08-05", "set_type": "core"},
                       {"set_code": "2xm", "name": "Double Masters", "release_date": "2020-08-07", "set_type": "masters"}],
              writer="mtgjson")
        fa, fb, fm = self.files()
        docsstatus.run(a, fa, fb, fm)
        db = os.path.join(self.tmp, "out.sqlite")
        build_mod.build(db, self.schema, a.dir, None, {})
        out = os.path.join(self.tmp, "export")
        self.assertEqual(docsstatus.export(db, out), (2, 1))
        got_a = [json.loads(l) for l in read(os.path.join(out, "status-A.jsonl")).splitlines()]
        want = {r["set"]: r for r in A_LINES}
        self.assertEqual([r["set"] for r in got_a], ["2xm", "lea"])
        for r in got_a:
            w = want[r["set"]]
            for k in ("name", "set_type", "released", "level", "printed", "cards", "outstanding", "problem_cards",
                      "buckets", "declared", "xmage_verified", "forge_only", "hand", "non_tournament"):
                self.assertEqual(r.get(k), w.get(k), (r["set"], k))
        got_b = [json.loads(l) for l in read(os.path.join(out, "status-B.jsonl")).splitlines()]
        self.assertEqual(got_b[0]["buckets"], {"mismatch": 2})
        self.assertEqual(json.loads(read(os.path.join(out, "meta.json"))), META)
        docsstatus.export(db, out + "2")  # deterministic: a second export is byte-identical
        for n in ("status-A.jsonl", "status-B.jsonl", "meta.json"):
            self.assertEqual(read(os.path.join(out, n)), read(os.path.join(out + "2", n)))

    def test_the_cli_verbs(self):
        home = os.path.join(self.tmp, "cli")
        self.assertEqual(self.cli(home, "init").returncode, 0)
        fa, fb, fm = self.files()
        r = self.cli(home, "ingest", "status", "--status-a", fa, "--status-b", fb, "--meta", fm, "--source", "ci")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("2 set(s) folded into cert_sets (2 rows changed)", r.stdout)
        r = self.cli(home, "ingest", "status", "--meta", fm)
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(self.cli(home, "validate").returncode, 0)


class DocsSets(Base):
    """export_sets: sets.json, writers.json and the finished sets-carddb.md snippet from keyproof."""

    def fixture(self, with_status=True):
        from .test_query import flat, keyproof_rows, OID_A, OID_B, OID_C
        a = self.machine("a", init=True)
        a.put("sets", [{"set_code": "lea", "name": "Limited Edition Alpha", "release_date": "1993-08-05", "set_type": "core"},
                       {"set_code": "2ed", "name": "Unlimited | Edition", "release_date": "1993-12-01", "set_type": "core"},
                       {"set_code": "3ed", "name": "Revised", "release_date": "1994-04-01", "set_type": "core"}],
              writer="mtgjson")
        a.put("printings", [
            {"printing_id": "p1", "oracle_id": OID_A, "set_code": "lea", "collector_number": "1", "rarity": "common"},
            {"printing_id": "p1b", "oracle_id": OID_A, "set_code": "lea", "collector_number": "1b", "rarity": "common"},
            {"printing_id": "p2", "oracle_id": OID_A, "set_code": "2ed", "collector_number": "1", "rarity": "common"},
            {"printing_id": "p3", "oracle_id": OID_B, "set_code": "lea", "collector_number": "2", "rarity": "common"},
            {"printing_id": "p4", "oracle_id": OID_C, "set_code": "2ed", "collector_number": "3", "rarity": "common"},
            {"printing_id": "p5", "oracle_id": OID_C, "set_code": "3ed", "collector_number": "3", "rarity": "common"},
        ], writer="catalogue")
        a.put("keyproof", flat(
            keyproof_rows(OID_A, "cast", "agreed"), keyproof_rows(OID_A, "f0/a0", "agreed"),
            keyproof_rows(OID_B, "cast", "owed", reason="no-generator"),
            keyproof_rows(OID_B, "f0/a0", "diagnosed", sig="a1b2c3d4e5f6", cite="CR 702.4b"),
            keyproof_rows(OID_C, "cast", "diverged", diff_items=1),
        ), writer="xproof")
        if with_status:
            a.put("status", [{"oracle_id": OID_A, "level": "A", "verdict": "agree", "gorge_pin": "8f7095353a64",
                              "measured": "2026-11-19", "writer": "loop"},
                             {"oracle_id": OID_C, "level": "B", "verdict": "diverge", "gorge_pin": "8f7095353a64",
                              "measured": "2026-11-19", "writer": "loop"}], writer="loop")
        db = os.path.join(self.tmp, "sets.sqlite")
        build_mod.build(db, self.schema, a.dir, None, {})
        return db

    def test_sets_json_counts_abilities_per_set_and_a_set_without_rows_is_absent(self):
        db = self.fixture()
        out = os.path.join(self.tmp, "o")
        self.assertEqual(docsstatus.export_sets(db, out), 3)
        got = json.loads(read(os.path.join(out, "sets.json")))
        self.assertEqual(list(got), ["2ed", "3ed", "lea"])  # sorted
        z = {"agreed": 0, "owed": 0, "diagnosed": 0, "diverged": 0, "excluded": 0}
        # lea: card A twice printed there counts once; 2ed: A's two keys plus C's diverged one
        self.assertEqual(got["lea"], {"xproof": dict(z, agreed=2, owed=1, diagnosed=1), "proven_cards": 1})
        self.assertEqual(got["2ed"], {"xproof": dict(z, agreed=2, diverged=1), "proven_cards": 1})
        self.assertEqual(got["3ed"], {"xproof": dict(z, diverged=1), "proven_cards": 0})
        self.assertNotIn("dirac_supported", got["lea"])
        # a set with no keyproof row at all is absent, never all zero
        a = self.machine("a")
        a.put("sets", [{"set_code": "alch", "name": "Alchemy", "release_date": "2020-01-01", "set_type": "alchemy"}],
              writer="mtgjson")
        db2 = os.path.join(self.tmp, "more.sqlite")
        build_mod.build(db2, self.schema, a.dir, None, {})
        docsstatus.export_sets(db2, out + "2")
        self.assertNotIn("alch", json.loads(read(os.path.join(out + "2", "sets.json"))))

    def test_proven_cards_is_omitted_when_the_status_table_has_no_rows(self):
        out = os.path.join(self.tmp, "o")
        docsstatus.export_sets(self.fixture(with_status=False), out)
        got = json.loads(read(os.path.join(out, "sets.json")))
        for code, ent in got.items():
            self.assertNotIn("proven_cards", ent, code)
        md = read(os.path.join(out, "sets-carddb.md"))
        self.assertNotIn("Proven", md)

    def test_writers_json_and_the_markdown_snippet_sections(self):
        db = self.fixture()
        out = os.path.join(self.tmp, "o")
        docsstatus.export_sets(db, out)
        writers = json.loads(read(os.path.join(out, "writers.json")))
        self.assertTrue(writers)
        self.assertEqual(writers, sorted(writers, key=lambda w: (w["writer"], w["table"])))
        self.assertIn({"writer": "xproof", "table": "keyproof", "ran": "2026-11-19"}, writers)
        for w in writers:
            self.assertEqual(set(w), {"writer", "table", "ran"})
        md = read(os.path.join(out, "sets-carddb.md"))
        for name in ("headline", "sets", "freshness"):
            self.assertEqual(md.count("<!-- --8<-- [start:%s] -->\n" % name), 1, name)
            self.assertLess(md.index("[start:%s]" % name), md.index("[end:%s]" % name))
        self.assertIn("| `lea` | Limited Edition Alpha | 2 | 1 | 0 | 1 |", md)
        self.assertIn("| `2ed` | Unlimited \\| Edition | 2 | 0 | 1 | 1 |", md)
        self.assertIn("| Agreed | 4 |", md)
        self.assertIn("xproof (keyproof) 2026-11-19", md)

    def test_rerun_over_unchanged_inputs_changes_no_byte(self):
        db = self.fixture()
        a, b = os.path.join(self.tmp, "r1"), os.path.join(self.tmp, "r2")
        docsstatus.export_sets(db, a)
        docsstatus.export_sets(db, b)
        docsstatus.export_sets(db, a)
        for n in ("sets.json", "writers.json", "sets-carddb.md"):
            self.assertEqual(read(os.path.join(a, n)), read(os.path.join(b, n)), n)

    def test_cli_verb(self):
        db = self.fixture()
        out = os.path.join(self.tmp, "cli")
        r = self.cli(os.path.join(self.tmp, "h"), "docs-sets", "--db", db, "--out", out)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(os.path.exists(os.path.join(out, "sets-carddb.md")))
