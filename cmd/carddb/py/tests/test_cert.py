"""The certification writer (cert/cert_sets), the private cert partition and the proven view/verb."""
import os
import sqlite3
import unittest

from carddb import build as build_mod
from carddb import query
from carddb.norm import norm_name
from carddb.store import Store, StoreError
from .helpers import Base, OID_A, OID_B, OID_C, git

OID_D = "ef" + "3" * 30

GORGE = "8f7095353a64"
PIN = "418ba5215f"


def cert_row(oid, name, level, **kw):
    row = {
        "oracle_id": oid, "norm_name": norm_name(name), "name": name,
        "gorge_sha": GORGE, "corpus_pin": PIN,
        "script_sha": "a" * 64, "oracle_sha": "b" * 64,
        "first_set": "lea", "released": "1993-08-05",
        "level": level, "exclusion": "-", "refs": "FX",
        "keys_total": 3, "keys_used": 3, "keys_compared": 3, "keys_agreed": 3,
        "keys_unreachable": 0, "triggers_unmeasured": 0,
        "scen_level": "B", "oracle_audit": "ok",
        "open_deltas": 0, "diagnosed_deltas": 0,
        "tickets": "-", "why": "-", "witness": "f0/a0=item=1;seed=2",
        "measured": "2026-11-20",
    }
    row.update(kw)
    return row


def cert_set_row(code, **kw):
    row = {
        "set_code": code, "gorge_sha": GORGE, "corpus_pin": PIN,
        "released": "1993-08-05", "set_type": "core",
        "paper": 10, "new_paper": 9, "reprints": 1, "playable": 9, "excluded": 0,
        "c0": 1, "c1": 2, "c2": 3, "c3": 3, "open": 0,
        "level": "C3", "state": "certified", "cursor": "-",
        "entered": "2026-11-20", "certified": "2026-11-20", "measured": "2026-11-20",
    }
    row.update(kw)
    return row


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def tsv(columns, row):
    return "\t".join(str(row.get(c, "-")) for c in columns) + "\n"


class CertWriter(Base):
    def test_schema_keeps_the_cert_card_and_cert_set_columns(self):
        cert = [c.name for c in self.schema.tables["cert"].columns]
        self.assertEqual(cert, [
            "oracle_id", "norm_name", "name", "gorge_sha", "corpus_pin", "script_sha", "oracle_sha",
            "first_set", "released", "level", "exclusion", "refs", "keys_total", "keys_used",
            "keys_compared", "keys_agreed", "keys_unreachable", "triggers_unmeasured", "scen_level",
            "oracle_audit", "open_deltas", "diagnosed_deltas", "tickets", "why", "witness", "measured",
            "writer"])
        sets = [c.name for c in self.schema.tables["cert_sets"].columns]
        self.assertEqual(sets, [
            "set_code", "gorge_sha", "corpus_pin", "released", "set_type", "paper", "new_paper",
            "reprints", "playable", "excluded", "c0", "c1", "c2", "c3", "open", "level", "state",
            "cursor", "entered", "certified", "measured",
            "printed", "a_declared", "a_cards", "a_outstanding", "a_problem_cards", "a_xmage_verified",
            "a_forge_only", "a_hand", "a_non_tournament", "a_buckets", "b_declared", "b_cards", "b_outstanding",
            "b_problem_cards", "b_non_tournament", "b_buckets", "status_date", "status_commit", "status_gorge",
            "status_forge_ref", "ratchet_failures", "writer"])
        for private in ("cert_events", "cert_runs"):
            self.assertTrue(self.schema.tables[private].is_private(), private)

    def test_put_cert_is_the_current_row_and_reruns_cleanly(self):
        a = self.machine("a", init=True)
        rows = [cert_row(OID_A, "Lightning Bolt", "C1"), cert_row(OID_B, "Fire // Ice", "C2")]
        self.assertEqual(a.put("cert", rows, writer="cert"), 2)
        text = read(os.path.join(a.dir, "cert", "ab.tsv"))
        self.assertEqual(len([l for l in text.splitlines() if l.startswith(OID_A + "\t")]), 1)
        self.assertIn("\tC1\t", text)
        # the key is oracle_id: a newer pin/level replaces the old row, never appends a history row
        self.assertEqual(a.put("cert", [cert_row(OID_A, "Lightning Bolt", "C3",
                                                 measured="2026-11-21")], writer="cert"), 1)
        text = read(os.path.join(a.dir, "cert", "ab.tsv"))
        self.assertEqual(text.count(OID_A), 1)
        self.assertIn("\tC3\t", text)
        self.assertNotIn("\tC1\t", text)
        self.assertEqual(a.put("cert", [cert_row(OID_A, "Lightning Bolt", "C3",
                                                 measured="2026-11-21")], writer="cert"), 0)

    def test_put_cert_sets_and_the_cli_accept_full_rows(self):
        home = os.path.join(self.tmp, "a")
        self.assertEqual(self.cli(home, "init").returncode, 0)
        cols = [c.name for c in self.schema.tables["cert"].columns]
        row = cert_row(OID_A, "Lightning Bolt", "C3")
        r = self.cli(home, "put", "cert", "--source", "cert",
                     stdin="\t".join(cols) + "\n" + tsv(cols, row))
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("1 rows changed", r.stdout)
        scols = [c.name for c in self.schema.tables["cert_sets"].columns]
        srow = cert_set_row("lea")
        r = self.cli(home, "put", "cert_sets", "--source", "cert",
                     stdin="\t".join(scols) + "\n" + tsv(scols, srow))
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("1 rows changed", r.stdout)
        tree = os.path.join(home, "public")
        self.assertIn("1\t2\t3\t3", read(os.path.join(tree, "cert_sets.tsv")))

    def test_private_partition_needs_the_private_store(self):
        a = self.machine("a", init=True)
        event = {"event_id": "e1", "t": "2026-11-20T00:00:00Z", "event": "certified",
                 "gorge_sha": GORGE, "corpus_pin": PIN, "norm_name": norm_name("Lightning Bolt"),
                 "set_code": "lea", "from_level": "C1", "to_level": "C2", "detail": "-"}
        with self.assertRaises(StoreError):
            a.put("cert_events", [event], writer="cert")
        os.environ["CARDDB_PRIVATE_REMOTE"] = os.path.join(self.tmp, "private.git")
        self.addCleanup(os.environ.pop, "CARDDB_PRIVATE_REMOTE", None)
        git("init", "--bare", "--quiet", os.environ["CARDDB_PRIVATE_REMOTE"])
        p = Store("private", home=os.path.join(self.tmp, "a"), schema=self.schema).init()
        self.assertEqual(p.put("cert_events", [event], writer="cert"), 1)
        self.assertEqual(p.put("cert_runs", [{
            "run_id": "r1", "t": "2026-11-20T00:00:00Z", "gorge_sha": GORGE, "corpus_pin": PIN,
            "deal_set": "lea", "cert_set": "lea", "sets_certified": 0, "sets_total": 1, "cards": 1,
            "c0": 0, "c1": 0, "c2": 1, "c3": 0, "excluded": 0, "open": 0, "diagnosed": 0,
            "farm_games_6h": 10, "events": 1, "decision": "ok", "detail": "-"}], writer="cert"), 1)


class Proven(Base):
    def fixture(self):
        a = self.machine("a", init=True)
        a.put("cert", [
            cert_row(OID_A, "Lightning Bolt", "C0"),
            cert_row(OID_B, "Fire // Ice", "C1"),
            cert_row(OID_C, "Dáin Ironfoot", "C2"),
            cert_row(OID_D, "Excluded Card", "C3", exclusion="ante"),
        ], writer="cert")
        return a

    def build(self, a):
        out = os.path.join(self.tmp, "carddb.sqlite")
        build_mod.build(out, self.schema, a.dir, None, {})
        return out

    def test_view_and_verb_are_the_same_proven_set_as_cert(self):
        a = self.fixture()
        out = self.build(a)
        conn = sqlite3.connect(out)
        self.addCleanup(conn.close)
        direct = [r[0] for r in conn.execute(
            "SELECT oracle_id FROM cert WHERE level IN ('C2','C3') AND exclusion IS NULL ORDER BY oracle_id")]
        view = [r[0] for r in conn.execute("SELECT oracle_id FROM proven ORDER BY oracle_id")]
        self.assertEqual(view, direct)
        self.assertEqual(view, [OID_C])  # C2; the C3 row is excluded
        self.assertEqual(len(query.proven_from_db(out)), 1)
        # the verb over the clone and over the built file agree, header and all
        over_clone = self.cli(os.path.join(self.tmp, "a"), "proven")
        over_db = self.cli(os.path.join(self.tmp, "a"), "proven", "--db", out)
        self.assertEqual(over_clone.returncode, 0, over_clone.stderr)
        self.assertEqual(over_db.returncode, 0, over_db.stderr)
        self.assertEqual(over_clone.stdout, over_db.stdout)
        lines = over_clone.stdout.strip().split("\n")
        self.assertEqual(lines[0].split("\t")[:4], ["oracle_id", "norm_name", "name", "level"])
        self.assertEqual([l.split("\t")[0] for l in lines[1:]], [OID_C])
        count = self.cli(os.path.join(self.tmp, "a"), "proven", "--format", "count")
        self.assertEqual(count.stdout, "1\n")

    def test_proven_filters_exclusions_and_below_c2(self):
        rows = [cert_row(OID_A, "Lightning Bolt", "C0"),
                cert_row(OID_B, "Fire // Ice", "C2"),
                cert_row(OID_C, "Dáin Ironfoot", "C3", exclusion="ante"),
                cert_row(OID_D, "Excluded Card", "C3")]
        self.assertEqual([r["oracle_id"] for r in query.proven_rows(rows)], [OID_B, OID_D])
        self.assertEqual(query.format_proven([], "count"), "0\n")


if __name__ == "__main__":
    unittest.main()
