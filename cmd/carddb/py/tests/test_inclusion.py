import os
import shutil
import subprocess
import sys
import tempfile
import unittest

from .helpers import MAIN, OID_A, OID_B, OID_C
from carddb import inclusion, shard
from carddb.norm import Resolver
from carddb.schema import PUBLIC, load_schema
from carddb.store import StoreError

OID_FI = "ef" + "3" * 30

CARDS = [
    (OID_A, "Sol Ring"),
    (OID_B, "Arcane Signet"),
    (OID_C, "Command Tower"),
    (OID_FI, "Fire // Ice"),
]

HEADER = "card\tdecks\trate\trate_precon\trate_casual\trate_cedh\trate_ranking\n"
INPUT = HEADER + "".join("\t".join(r) + "\n" for r in [
    ("Sol Ring", "900", "0.900000", "0.990001", "0.880000", "0.970000", "0.910007"),
    ("Arcane Signet", "700", "0.700000", "0.950002", "0.690000", "0.400000", "0.700003"),
    ("Not A Real Card", "50", "0.050000", "0.000004", "0.050000", "0.010000", "0.050005"),
    ("Fire", "30", "0.030000", "0.000006", "0.030000", "0.020000", "0.030006"),
    # Folds onto Command Tower: fewer decks, so the canonical name keeps its row.
    ("COMMAND TOWER", "400", "0.400000", "0.900007", "0.390000", "0.300000", "0.400008"),
    ("Command Tower", "800", "0.800000", "0.990009", "0.790000", "0.600000", "0.800009"),
])


class TestInclusion(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="carddb-inclusion-")
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.schema = load_schema()
        self.src = os.path.join(self.tmp, "inclusion.tsv")
        with open(self.src, "w", encoding="utf-8") as f:
            f.write(INPUT)

    def result(self, text=None):
        if text is not None:
            with open(self.src, "w", encoding="utf-8") as f:
                f.write(text)
        self.resolver = Resolver(CARDS)
        return inclusion.rows(inclusion.read_input(self.src), self.resolver, "decksample-v1")

    def by_oid(self, res):
        return {r["oracle_id"]: r for r in res.rows}

    def test_resolved_row_copies_shares_and_drops_precon_and_ranking(self):
        res = self.result()
        self.assertEqual(self.by_oid(res)[OID_A], {
            "oracle_id": OID_A, "decks": "900", "rate": "0.900000", "rate_casual": "0.880000",
            "rate_cedh": "0.970000", "sample": "decksample-v1"})
        out = os.path.join(self.tmp, "out.tsv")
        text = inclusion.write(out, res.rows, self.schema)
        self.assertEqual(text.split("\n")[:2], ["#carddb curated/commander-inclusion v1",
                                                "oracle_id\tdecks\trate\trate_casual\trate_cedh\tsample"])
        for dropped in ("rate_precon", "rate_ranking", "0.990001", "0.910007", "0.950002", "0.700003",
                        "0.990009", "0.800009"):
            self.assertNotIn(dropped, text)

    def test_unresolved_name_left_out_and_counted(self):
        res = self.result()
        self.assertEqual(res.unresolved, 1)
        self.assertEqual(self.resolver.misses, {})
        self.assertNotIn("0.050000", "".join(r["rate"] for r in res.rows))
        self.assertTrue(all(not r["oracle_id"].startswith("x:") for r in res.rows))

    def test_split_card_resolves_by_front_face(self):
        res = self.result(HEADER + "Fire // Ice\t30\t0.030000\t0\t0.030000\t0.020000\t0\n")
        self.assertEqual([r["oracle_id"] for r in res.rows], [OID_FI])
        self.assertEqual(res.unresolved, 0)

    def test_fold_keeps_more_decks(self):
        res = self.result()
        self.assertEqual(res.folded, 1)
        self.assertEqual(self.by_oid(res)[OID_C]["decks"], "800")
        self.assertEqual(self.by_oid(res)[OID_C]["rate_cedh"], "0.600000")

    def test_fold_tie_keeps_bytewise_smaller_name(self):
        res = self.result(HEADER +
                          "command tower\t5\t0.000500\t0\t0.000500\t0.000500\t0\n"
                          "Command Tower\t5\t0.000400\t0\t0.000400\t0.000400\t0\n")
        self.assertEqual(res.folded, 1)
        self.assertEqual(self.by_oid(res)[OID_C]["rate"], "0.000400")  # "C" < "c"

    def test_sorted_and_rerun_byte_identical(self):
        res = self.result()
        oids = [r["oracle_id"] for r in res.rows]
        self.assertEqual(oids, sorted(oids))
        self.assertEqual(oids, [OID_A, OID_B, OID_C, OID_FI])
        a, b = os.path.join(self.tmp, "a.tsv"), os.path.join(self.tmp, "b.tsv")
        inclusion.write(a, res.rows, self.schema)
        inclusion.write(b, self.result().rows, self.schema)
        with open(a, "rb") as fa, open(b, "rb") as fb:
            self.assertEqual(fa.read(), fb.read())

    def test_output_validates_public(self):
        out = os.path.join(self.tmp, "out.tsv")
        text = inclusion.write(out, self.result().rows, self.schema)
        self.assertEqual(shard.validate_file(self.schema, PUBLIC, "curated/commander-inclusion.tsv", text), [])

    def test_bad_header_rejected(self):
        with self.assertRaises(StoreError):
            self.result("card\tdecks\trate\n")

    def test_cli(self):
        cards = os.path.join(self.tmp, "cards.tsv")
        with open(cards, "w", encoding="utf-8") as f:
            f.write("".join("%s\t%s\n" % c for c in CARDS))
        out = os.path.join(self.tmp, "cli.tsv")
        home = os.path.join(self.tmp, "home")
        env = dict(os.environ, CARDDB_HOME=home)
        args = [sys.executable, "-I", MAIN, "inclusion", "--from", self.src, "--sample", "decksample-v1",
                "--cards", cards, "--out", out]
        p = subprocess.run(args, capture_output=True, text=True, env=env)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(p.stdout, "inclusion: 4 rows from 6 names, 1 unresolved, 1 folded, sample decksample-v1\n")
        with open(out, "rb") as f:
            first = f.read()
        p = subprocess.run(args, capture_output=True, text=True, env=env)
        self.assertEqual(p.returncode, 0, p.stderr)
        with open(out, "rb") as f:
            self.assertEqual(f.read(), first)
        # No clone was touched, so nothing could reach _meta/unresolved.
        self.assertFalse(os.path.exists(os.path.join(home, "public", "_meta", "unresolved.tsv")))

    def test_committed_file_is_verb_output(self):
        # The committed file must be what the verb writes; with no sample rows it is the header-only shard.
        path = os.path.join(os.path.dirname(MAIN), "..", "..", "carddb", "curated", "commander-inclusion.tsv")
        with open(path, encoding="utf-8") as f:
            text = f.read()
        self.assertEqual(shard.validate_file(self.schema, PUBLIC, "curated/commander-inclusion.tsv", text), [])
        h, cols, _rows = shard.parse_text(text)
        self.assertEqual((h, cols), ("#carddb curated/commander-inclusion v1",
                                     ["oracle_id", "decks", "rate", "rate_casual", "rate_cedh", "sample"]))


if __name__ == "__main__":
    unittest.main()
