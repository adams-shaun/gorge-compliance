import json
import os
import unittest
import unittest.mock

from carddb import dirac as dirac_mod
from carddb import store as store_mod
from .helpers import Base, OID_A, OID_B, OID_C, ROOT
from .test_compliance import card, corpus, fixture_repo, OID_D

PIN = "32bfe4888a634a0f8b31b7e82cafe400e85e6853"
SNAP_PIN = "8f7095353a64deadbeef8f7095353a64deadbeef"
MEASURED = "2026-11-19"


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def fixture(tmp, gaps=None, pin=PIN):
    """(checkout, gorge dir): Bolt supported (two lists), Slop refused, Whim unverified,
    Night // Day evidenced under its first face, a below-credit pod-only row ignored."""
    repo = os.path.join(tmp, "repo")
    write(os.path.join(repo, "GORGE_PIN"), pin + "\n")
    write(os.path.join(repo, dirac_mod.GAPS_REL),
          gaps if gaps is not None else "#dirac-gaps gorge-pin %s\nWhim\tunverified\tin no evidence list\n" % pin)
    g = os.path.join(tmp, "gorge")
    sup = os.path.join(g, "dirac", "support")
    write(os.path.join(sup, "evidence", "blb.txt"), "Bolt\nNight\n")
    write(os.path.join(sup, "evidence", "fdn.txt"), "Bolt\nSlop\nPodOnly\t3\n")
    write(os.path.join(sup, "refused", "slop.json"), json.dumps(
        {"card": "Slop", "class": "lockstep", "why": "an  offer gate\nprices it wrong", "repro": {}}))
    return repo, g


class Dirac(Base):
    def rows(self, store, table="support"):
        return list(store.rows(table))

    def mapped(self, store):
        return {(r["oracle_id"], r["engine"]): r for r in store.rows("support")}

    def test_three_cards_and_evidence(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt"), card(OID_B, "Slop"), card(OID_D, "Whim"),
                        card(OID_C, "Elsewhere")], writer="mtgjson")
        repo, g = fixture(self.tmp)
        changed, counts, nev = dirac_mod.run(a, repo=repo, gorge=g)
        self.assertEqual(counts, {"supported": 1, "refused": 1, "unverified": 1})
        self.assertEqual(nev, 2)
        rows = self.mapped(a)
        self.assertEqual(sorted(k for k in rows if k[1] == "dirac"),
                         sorted([(OID_A, "dirac"), (OID_B, "dirac"), (OID_D, "dirac")]))  # not Elsewhere
        pin = PIN[:12]
        self.assertEqual(rows[(OID_A, "dirac")],
                         {"oracle_id": OID_A, "engine": "dirac", "status": "supported", "pin": pin,
                          "why": "-", "measured": MEASURED, "writer": "ci"})
        self.assertEqual(rows[(OID_B, "dirac")]["status"], "refused")
        self.assertEqual(rows[(OID_B, "dirac")]["why"], "lockstep: an offer gate prices it wrong")
        self.assertEqual(rows[(OID_D, "dirac")]["status"], "unverified")
        self.assertEqual(rows[(OID_D, "dirac")]["why"], "in no evidence list")
        for r in rows.values():
            self.assertEqual(r["pin"], pin)
        ev = sorted((r["oracle_id"], r["source"], r["ref"], r["outcome"]) for r in a.rows("evidence"))
        self.assertEqual(ev, [(OID_A, "dirac-lockstep", "blb", "agree"),
                              (OID_A, "dirac-lockstep", "fdn", "agree")])
        writers = {(r["table"], r["writer"]) for r in a.rows("_meta/writers")}
        self.assertLessEqual({("support", "ci"), ("evidence", "ci")}, writers)

    def test_first_face_evidence_and_gap_override(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_C, "Night // Day", layout="split"), card(OID_A, "Bolt")], writer="mtgjson")
        gaps = ("#dirac-gaps gorge-pin %s\nBolt\tnot_lowered\tkgen does not lower it\n"
                "Night // Day\tsupported\ttwo-seat only\n" % PIN)
        repo, g = fixture(self.tmp, gaps=gaps)
        dirac_mod.run(a, repo=repo, gorge=g)
        rows = self.mapped(a)
        # evidenced but no longer lowered: the snapshot wins over the evidence list
        self.assertEqual(rows[(OID_A, "dirac")]["status"], "not_lowered")
        self.assertEqual(rows[(OID_C, "dirac")]["status"], "supported")
        self.assertEqual(rows[(OID_C, "dirac")]["why"], "two-seat only")
        refs = [(r["oracle_id"], r["ref"]) for r in a.rows("evidence")]
        self.assertEqual(refs, [(OID_C, "blb")])  # a not-lowered card has no evidence rows

    def test_rerun_changes_nothing_and_other_engines_survive(self):
        from .test_compliance import fixture_repo as compliance_repo
        from carddb import compliance as compliance_mod
        a = self.machine("a", init=True)
        corpus(a)
        a.put("cards", [card(OID_A, "Bolt")], writer="mtgjson")
        compliance_mod.run(a, repo=compliance_repo(self.tmp))
        before_other = [r for r in a.rows("support") if r["engine"] != "dirac"]
        repo, g = fixture(self.tmp)
        first, _, _ = dirac_mod.run(a, repo=repo, gorge=g)
        self.assertGreater(first, 0)
        snap = self.snapshot(a.dir)
        again, _, _ = dirac_mod.run(a, repo=repo, gorge=g)
        self.assertEqual(again, 0)
        self.assertEqual(self.snapshot(a.dir), snap)
        self.assertEqual([r for r in a.rows("support") if r["engine"] != "dirac"], before_other)

    def test_stale_dirac_rows_are_dropped(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt")], writer="mtgjson")
        repo, g = fixture(self.tmp)
        dirac_mod.run(a, repo=repo, gorge=g)
        os.remove(os.path.join(g, "dirac", "support", "evidence", "fdn.txt"))
        os.remove(os.path.join(g, "dirac", "support", "evidence", "blb.txt"))
        write(os.path.join(g, "dirac", "support", "evidence", "blb.txt"), "Other\n")
        dirac_mod.run(a, repo=repo, gorge=g)
        self.assertEqual(self.rows(a), [])
        self.assertEqual(self.rows(a, "evidence"), [])

    def test_stale_snapshot_still_writes(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt"), card(OID_D, "Whim"),
                        card(OID_C, "Night // Day", layout="split")], writer="mtgjson")
        gaps = ("#dirac-gaps gorge-pin %s\n"
                "Bolt\tnot_lowered\tkgen does not lower it\n"
                "Whim\tnot_lowered\tkgen does not lower it\n"
                "Night // Day\tsupported\ttwo-seat only\n" % SNAP_PIN)
        repo, g = fixture(self.tmp, gaps=gaps, pin=PIN)
        r = dirac_mod.run_report(a, repo=repo, gorge=g)
        self.assertTrue(r.stale)
        self.assertEqual((r.pin, r.snapshot_pin), (PIN, SNAP_PIN))
        self.assertEqual(r.stale_rows, 1)
        self.assertEqual(r.counts, {"supported": 2, "not_lowered": 1})
        rows = self.mapped(a)
        # evidenced at GORGE_PIN: lowered regardless of what the stale snapshot says
        self.assertEqual(rows[(OID_A, "dirac")]["status"], "supported")
        self.assertEqual(rows[(OID_A, "dirac")]["why"], "-")
        # the snapshot's two-seat why survives an evidenced card
        self.assertEqual(rows[(OID_C, "dirac")]["status"], "supported")
        self.assertEqual(rows[(OID_C, "dirac")]["why"], "two-seat only")
        # unevidenced: the snapshot's not_lowered, marked with the pin it came from
        self.assertEqual(rows[(OID_D, "dirac")]["status"], "not_lowered")
        self.assertEqual(rows[(OID_D, "dirac")]["why"],
                         "kgen does not lower it (lowering snapshot at gorge %s)" % SNAP_PIN[:10])
        refs = sorted((r["oracle_id"], r["ref"]) for r in a.rows("evidence"))
        self.assertEqual(refs, [(OID_A, "blb"), (OID_A, "fdn"), (OID_C, "blb")])

    def test_current_snapshot_rows_unchanged(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt"), card(OID_B, "Slop"), card(OID_D, "Whim"),
                        card(OID_C, "Night // Day", layout="split")], writer="mtgjson")
        gaps = ("#dirac-gaps gorge-pin %s\n"
                "Bolt\tnot_lowered\tkgen does not lower it\n"
                "Whim\tunverified\tin no evidence list\n"
                "Night // Day\tsupported\ttwo-seat only\n" % PIN)
        repo, g = fixture(self.tmp, gaps=gaps)
        r = dirac_mod.run_report(a, repo=repo, gorge=g)
        self.assertFalse(r.stale)
        self.assertEqual((r.snapshot_pin, r.stale_rows), ("", 0))
        rows = self.mapped(a)
        # today's precedence: the snapshot's status beats the evidence list
        self.assertEqual((rows[(OID_A, "dirac")]["status"], rows[(OID_A, "dirac")]["why"]),
                         ("not_lowered", "kgen does not lower it"))
        self.assertEqual((rows[(OID_C, "dirac")]["status"], rows[(OID_C, "dirac")]["why"]),
                         ("supported", "two-seat only"))
        self.assertEqual((rows[(OID_D, "dirac")]["status"], rows[(OID_D, "dirac")]["why"]),
                         ("unverified", "in no evidence list"))
        self.assertEqual(rows[(OID_B, "dirac")]["status"], "refused")
        for r in rows.values():
            self.assertNotIn("lowering snapshot at gorge", r["why"])
        self.assertEqual([(r["oracle_id"], r["ref"]) for r in a.rows("evidence")], [(OID_C, "blb")])

    def test_evidence_seat_rules(self):
        """kgen's readSupport: a plain row evidences; seats >= 4; xproof with seats >= 2."""
        oids = {"Plain": "%032x" % 1, "PodFour": "%032x" % 2, "PodThree": "%032x" % 3,
                "Xproof": "%032x" % 4, "XproofOne": "%032x" % 5}
        a = self.machine("a", init=True)
        a.put("cards", [card(oid, name) for name, oid in oids.items()], writer="mtgjson")
        repo, g = fixture(self.tmp)
        write(os.path.join(g, "dirac", "support", "evidence", "seats.txt"),
              "Plain\nPodFour\t4\nPodThree\t3\nXproof\t2\txproof\nXproofOne\t1\txproof\n")
        dirac_mod.run(a, repo=repo, gorge=g)
        rows = self.mapped(a)
        self.assertEqual(sorted(oid for oid, engine in rows if engine == "dirac"),
                         sorted([oids["Plain"], oids["PodFour"], oids["Xproof"]]))
        self.assertEqual(sorted((r["oracle_id"], r["ref"]) for r in a.rows("evidence")),
                         sorted([(oids["Plain"], "seats"), (oids["PodFour"], "seats"),
                                 (oids["Xproof"], "seats")]))

    def test_other_writers_rows_with_escapes_survive_reruns(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt"), card(OID_B, "Other")], writer="mtgjson")
        a.put("support", [{"oracle_id": OID_B, "engine": "forge", "status": "failing", "pin": "abc",
                           "why": "a\\b\tc", "measured": MEASURED}], writer="ci")
        a.put("evidence", [{"oracle_id": OID_B, "source": "oracle", "ref": "r\\1", "outcome": "agree"}],
              writer="ci")
        before = ([r for r in a.rows("support") if r["engine"] == "forge"],
                  [r for r in a.rows("evidence") if r["source"] == "oracle"])
        self.assertEqual(before[0][0]["why"], "a\\\\b\\tc")  # stored escaped
        repo, g = fixture(self.tmp)
        for _ in range(3):
            dirac_mod.run(a, repo=repo, gorge=g)
        self.assertEqual(([r for r in a.rows("support") if r["engine"] == "forge"],
                          [r for r in a.rows("evidence") if r["source"] == "oracle"]), before)

    def test_cli_stale_snapshot_writes_rows_and_reports(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt"), card(OID_D, "Whim")], writer="mtgjson")
        repo, g = fixture(self.tmp)
        self.assertEqual(self.cli(os.path.join(self.tmp, "a"), "dirac", "--repo", repo, "--gorge-dir", g,
                                  "--measured", MEASURED).returncode, 0)
        snap = self.snapshot(a.dir)
        write(os.path.join(repo, dirac_mod.GAPS_REL),
              "#dirac-gaps gorge-pin %s\nWhim\tnot_lowered\tkgen does not lower it\n" % SNAP_PIN)
        p = self.cli(os.path.join(self.tmp, "a"), "dirac", "--repo", repo, "--gorge-dir", g,
                     "--measured", MEASURED)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertNotIn("::warning::", p.stderr)
        self.assertIn("dirac: snapshot at %s, GORGE_PIN %s: 1 rows from the snapshot may be stale"
                      % (SNAP_PIN, PIN), p.stdout)
        self.assertNotEqual(self.snapshot(a.dir), snap)
        rows = self.mapped(a)
        self.assertEqual(rows[(OID_D, "dirac")]["status"], "not_lowered")
        self.assertEqual(rows[(OID_D, "dirac")]["why"],
                         "kgen does not lower it (lowering snapshot at gorge %s)" % SNAP_PIN[:10])

    def test_cli_gorge_unavailable_keeps_rows_and_exits_zero(self):
        """A cold runner (gorge not in the module cache -> empty dir) must not fail the publish."""
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt")], writer="mtgjson")
        repo, g = fixture(self.tmp)
        self.assertEqual(self.cli(os.path.join(self.tmp, "a"), "dirac", "--repo", repo, "--gorge-dir", g,
                                  "--measured", MEASURED).returncode, 0)
        snap = self.snapshot(a.dir)
        empty = os.path.join(self.tmp, "no-gorge")
        os.makedirs(empty)
        p = self.cli(os.path.join(self.tmp, "a"), "dirac", "--repo", repo, "--gorge-dir", empty,
                     "--measured", MEASURED)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn("::warning::", p.stderr)
        self.assertEqual(self.snapshot(a.dir), snap)
        with unittest.mock.patch.dict(os.environ, {"CARDDB_GORGE_DIR": ""}), \
                unittest.mock.patch.object(dirac_mod.subprocess, "run",
                                           return_value=unittest.mock.Mock(returncode=0, stdout="\n", stderr="")):
            with self.assertRaises(dirac_mod.Unavailable):
                dirac_mod.gorge_dir(repo)

    def test_cli_verb(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt")], writer="mtgjson")
        repo, g = fixture(self.tmp)
        p = self.cli(os.path.join(self.tmp, "a"), "dirac", "--repo", repo, "--gorge-dir", g,
                     "--measured", MEASURED)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn("supported=1", p.stdout)
        self.assertIn("2 evidence rows", p.stdout)

    def test_pinned(self):
        """The writer against the pinned gorge: dirac's supported count is support.md's ALL row."""
        try:
            g = dirac_mod.gorge_dir(ROOT)
        except store_mod.StoreError as e:
            self.skipTest("the pinned gorge module is not available: %s" % e)
        with open(os.path.join(ROOT, dirac_mod.GAPS_REL), encoding="utf-8") as f:
            head = f.readline().rstrip("\n")
        if head != "#dirac-gaps gorge-pin " + dirac_mod.full_pin(ROOT):
            self.skipTest("dirac-gaps.tsv is at another gorge pin than GORGE_PIN (%s): regenerate it with"
                          " `go run ./cmd/diracsupport`" % head)
        sup = os.path.join(g, "dirac", "support")
        # one card per gorge name: the corpus the lists and the snapshot name
        with open(os.path.join(ROOT, dirac_mod.GAPS_REL), encoding="utf-8") as f:
            gap_names = [l.split("\t")[0] for l in f.read().split("\n")[1:] if l]
        refused = {}
        import glob
        for fn in sorted(glob.glob(os.path.join(sup, "refused", "*.json"))):
            with open(fn, encoding="utf-8") as f:
                refused[json.load(f)["card"]] = 1
        evidence_names = []
        for fn in sorted(glob.glob(os.path.join(sup, "evidence", "*.txt"))):
            with open(fn, encoding="utf-8") as f:
                evidence_names += [l for l in f.read().split("\n") if l and "\t" not in l]
        allnames = sorted(set(evidence_names) | set(gap_names) | set(refused))
        cards = [("%032x" % i, n) for i, n in enumerate(allnames)]
        a = self.machine("a", init=True)
        a.put("cards", [card(oid, n) for oid, n in cards], writer="mtgjson")
        dirac_mod.run(a, repo=ROOT, gorge=g)
        by = {}
        for r in a.rows("support"):
            by[r["status"]] = by.get(r["status"], 0) + 1
        with open(os.path.join(g, "dirac", "docs", "support.md"), encoding="utf-8") as f:
            allrow = [l for l in f.read().split("\n") if l.startswith("| ALL |")][0]
        cells = [c.strip() for c in allrow.strip("|").split("|")]
        total, lowered, refused_n, unverified, supported = (int(c) for c in cells[1:6])
        self.assertEqual(by.get("supported"), supported)
        self.assertEqual(by.get("refused"), refused_n)
        self.assertEqual(by.get("unverified"), unverified)
        self.assertEqual(sum(by.values()), total)
        self.assertEqual(total - by.get("not_lowered", 0), lowered)
        self.assertEqual({r["pin"] for r in a.rows("support")}, {dirac_mod.full_pin(ROOT)[:12]})


if __name__ == "__main__":
    unittest.main()
