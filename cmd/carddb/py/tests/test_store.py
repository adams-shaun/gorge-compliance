import os
import shutil
import subprocess
import sys
import threading
import unittest

from carddb import ValidationError, merge as merge_mod, shard, store as store_mod
from .helpers import Base, MAIN, OID_A, OID_B, OID_C, ROOT, git
from .test_query import keyproof_row


def read(p):
    with open(p, encoding="utf-8") as f:
        return f.read()


def legality(oid, fmt, status):
    return {"oracle_id": oid, "format": fmt, "status": status}


def status(oid, verdict, measured, writer=None):
    row = {"oracle_id": oid, "level": "A", "verdict": verdict, "gorge_pin": "8f7095353a64", "measured": measured}
    if writer:
        row["writer"] = writer
    return row


def printing(pid, oid, set_code):
    return {"printing_id": pid, "oracle_id": oid, "set_code": set_code, "collector_number": "1",
            "rarity": "common", "released": "1993-08-05"}


def run_go(verb, tree, *extra):
    go = shutil.which("go")
    return subprocess.run([go, "run", "./cmd/carddb", verb, "-schema", "carddb/schema", "-tree", tree] + list(extra),
                          cwd=ROOT, capture_output=True, text=True, env=dict(os.environ, GOWORK="off"))


class Put(Base):
    def test_rerun_is_zero_byte_change(self):
        a = self.machine("a", init=True)
        rows = [legality(OID_B, "modern", "legal"), legality(OID_A, "modern", "banned"),
                legality(OID_C, "legacy", "legal")]
        self.assertEqual(a.put("legality", rows, writer="loop"), 3)
        before = self.snapshot(a.dir)
        porcelain = git("status", "--porcelain", cwd=a.dir)
        self.assertEqual(a.put("legality", list(reversed(rows)), writer="loop"), 0)
        os.environ["CARDDB_NOW"] = "2030-01-01"  # a later day must not touch writers.tsv either
        self.assertEqual(a.put("legality", rows, writer="loop"), 0)
        self.assertEqual(self.snapshot(a.dir), before)
        self.assertEqual(git("status", "--porcelain", cwd=a.dir), porcelain)
        self.assertEqual(sorted(before), [".gitattributes", ".github/workflows/carddb.yml",
                                          "_meta/writers.tsv", "legality/ab.tsv", "legality/cd.tsv"])

    def test_rows_merge_by_key_sorted(self):
        a = self.machine("a", init=True)
        a.put("legality", [legality(OID_B, "modern", "legal"), legality(OID_A, "modern", "legal")], writer="loop")
        self.assertEqual(a.put("legality", [legality(OID_A, "modern", "banned"),
                                            legality(OID_A, "commander", "legal")], writer="loop"), 2)
        text = read(os.path.join(a.dir, "legality", "ab.tsv"))
        self.assertEqual(text.split("\n")[2:], [
            "%s\tcommander\tlegal" % OID_A, "%s\tmodern\tbanned" % OID_A, "%s\tmodern\tlegal" % OID_B, ""])
        self.assertEqual(text.split("\n")[0], "#carddb legality v1")

    def test_writers_row(self):
        a = self.machine("a", init=True)
        a.put("legality", [legality(OID_A, "modern", "legal")], writer="loop", version="v7")
        text = read(os.path.join(a.dir, "_meta", "writers.tsv"))
        self.assertEqual(text, "#carddb _meta/writers v1\nwriter\ttable\tversion\tran\nloop\tlegality\tv7\t2026-11-19\n")

    def test_invalid_rows_write_nothing(self):
        a = self.machine("a", init=True)
        before = self.snapshot(a.dir)
        for table, rows in [
            ("legality", [{"oracle_id": OID_A, "format": "modern", "status": "legal", "price": "1"}]),  # banned column
            ("cards", [{"oracle_id": OID_A, "name": "X", "layout": "normal", "mana_value": "two", "type_line": "T"}]),
            ("cards", [{"oracle_id": OID_A, "name": "X"}]),  # required columns missing
            ("evidence", [["a", "b"]]),  # wrong width
        ]:
            with self.assertRaises(ValidationError, msg=table):
                a.put(table, rows, writer="loop")
        self.assertEqual(self.snapshot(a.dir), before)

    def test_boundary_refuses_private_and_curated(self):
        a = self.machine("a", init=True)
        with self.assertRaises(store_mod.StoreError):
            a.put("text", [{"oracle_id": OID_A, "oracle_text": "x"}])  # a private table in the public store
        with self.assertRaises(store_mod.StoreError):
            a.put("curated/labels", [{"oracle_id": OID_A, "label": "x", "note": "-"}])
        with self.assertRaises(ValidationError):  # a Forge-script-shaped cell
            a.put("xledger", [{"oracle_id": OID_A, "tier": "T1", "state": "x", "note": "SVar:Foo$ Bar"}])

    def test_api_escapes_and_cli_passes_through(self):
        a = self.machine("a", init=True)
        a.put("xledger", [{"oracle_id": OID_A, "tier": "T1", "state": "ok", "note": "a\tb\nc\\d"}], writer="loop")
        self.assertIn("a\\tb\\nc\\\\d", read(os.path.join(a.dir, "xledger", "ab.tsv")))
        r = self.cli(os.path.join(self.tmp, "a"), "put", "xledger", "--source", "loop",
                     stdin="oracle_id\ttier\tstate\tnote\n%s\tT2\tok\tx\\ty\n" % OID_C)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("x\\ty", read(os.path.join(a.dir, "xledger", "cd.tsv")))

    def test_python_api(self):
        import carddb
        home = os.path.join(self.tmp, "api")
        store_mod.Store("public", home=home, schema=self.schema).init()
        n = carddb.put("legality", [legality(OID_A, "modern", "legal")], writer="loop", home=home)
        self.assertEqual(n, 1)

    def test_put_stamps_the_writer_and_a_lower_writer_does_not_flip_the_row(self):
        a = self.machine("a", init=True)
        a.put("status", [status(OID_A, "agree", "2026-11-19")], writer="ci")
        path = os.path.join(a.dir, "status", "ab.tsv")
        self.assertTrue(read(path).splitlines()[2].endswith("\t2026-11-19\tci"), read(path))
        before = self.snapshot(a.dir)
        self.assertEqual(a.put("status", [status(OID_A, "agree", "2026-11-19")], writer="loop"), 0)  # ci stays
        self.assertEqual(self.snapshot(a.dir), before)
        self.assertEqual(a.put("status", [status(OID_A, "agree", "2026-11-19", writer="ci")], writer="loop"), 0)
        b = self.machine("b", init=True)
        b.put("status", [status(OID_A, "agree", "2026-11-19")], writer="loop")
        self.assertEqual(b.put("status", [status(OID_A, "agree", "2026-11-19")], writer="ci"), 1)  # ci takes over
        self.assertTrue(read(os.path.join(b.dir, "status", "ab.tsv")).splitlines()[2].endswith("\tci"))

    def test_a_printing_that_changes_set_moves_and_the_go_checker_agrees(self):
        a = self.machine("a", init=True)
        a.put("printings", [printing("p1", OID_A, "lea"), printing("p2", OID_B, "lea")], writer="mtgjson")
        self.assertEqual(a.put("printings", [printing("p1", OID_A, "neo")], writer="mtgjson"), 1)
        self.assertNotIn("p1", read(os.path.join(a.dir, "printings", "lea.tsv")))
        self.assertIn("p2", read(os.path.join(a.dir, "printings", "lea.tsv")))
        self.assertIn("p1", read(os.path.join(a.dir, "printings", "neo.tsv")))
        # moving the last row out removes the emptied shard instead of leaving a header-only file
        a.put("printings", [printing("p2", OID_B, "neo")], writer="mtgjson")
        self.assertFalse(os.path.exists(os.path.join(a.dir, "printings", "lea.tsv")))
        self.assertEqual(a.put("printings", [printing("p1", OID_A, "neo"), printing("p2", OID_B, "neo")],
                               writer="mtgjson"), 0)  # rerun: no byte changes
        # the same key in one batch under two sets: the last row wins, one copy
        a.put("printings", [printing("p3", OID_C, "lea"), printing("p3", OID_C, "m21")], writer="mtgjson")
        self.assertFalse(os.path.exists(os.path.join(a.dir, "printings", "lea.tsv")))
        self.assertIn("p3", read(os.path.join(a.dir, "printings", "m21.tsv")))
        if shutil.which("go"):
            for verb in ("validate", "boundary"):
                p = run_go(verb, a.dir)
                self.assertEqual(p.returncode, 0, p.stdout + p.stderr)

    def test_a_printing_in_two_shards_is_refused_everywhere_the_go_checker_refuses_it(self):
        a = self.machine("a", init=True)
        a.put("printings", [printing("p1", OID_A, "lea")], writer="mtgjson")
        a.publish()
        # a hand-staged copy under another set (and one under an upper-cased set code)
        for stem in ("neo", "LEA"):
            text = read(os.path.join(a.dir, "printings", "lea.tsv")).replace("\tlea\t", "\t%s\t" % stem)
            with open(os.path.join(a.dir, "printings", stem + ".tsv"), "w", encoding="utf-8") as f:
                f.write(text)
        git("add", "-A", cwd=a.dir)
        with self.assertRaises(ValidationError) as cm:
            a.publish()
        self.assertIn("duplicate key (p1)", str(cm.exception))
        self.assertIn("first at printings/", str(cm.exception))
        self.assertEqual(git("ls-tree", "-r", "--name-only", "origin/carddb", cwd=a.dir).count("neo.tsv"), 0)
        if shutil.which("go"):
            p = run_go("validate", a.dir)
            self.assertEqual(p.returncode, 1)
            self.assertIn("duplicate key (p1)", p.stdout)

    def test_clones_that_home_one_printing_in_two_sets_cannot_publish_a_broken_tree(self):
        a = self.machine("a", init=True)
        b = self.machine("b")
        a.put("printings", [printing("p1", OID_A, "lea")], writer="mtgjson")
        b.put("printings", [printing("p1", OID_A, "neo")], writer="mtgjson")
        self.assertTrue(a.publish()["pushed"])
        with self.assertRaises(ValidationError):  # b rebases onto a's tree: two homes for p1
            b.publish()
        remote = git("ls-tree", "-r", "--name-only", "origin/carddb", cwd=a.dir)
        self.assertIn("printings/lea.tsv", remote)
        self.assertNotIn("printings/neo.tsv", remote)

    def test_go_validator_accepts_the_tool_output(self):
        go = shutil.which("go")
        if go is None:
            self.skipTest("no go")
        a = self.machine("a", init=True)
        a.put("legality", [legality(OID_A, "modern", "legal")], writer="loop")
        a.put("cards", [{"oracle_id": OID_A, "name": "Bolt", "layout": "normal", "mana_cost": "{R}", "mana_value": 1,
                         "type_line": "Instant", "colors": ["R"], "color_identity": ["R"]}], writer="loop")
        a.put("printings", [{"printing_id": "p1", "oracle_id": OID_A, "set_code": "lea", "collector_number": "161",
                             "rarity": "common", "released": "1993-08-05"}], writer="loop")
        a.put("_meta/unresolved", [{"kind": "card", "ref": "Zzz", "reason": "r"}], writer="loop")
        for verb in ("validate", "boundary"):
            p = subprocess.run([go, "run", "./cmd/carddb", verb, "-schema", "carddb/schema", "-tree", a.dir],
                               cwd=ROOT, capture_output=True, text=True, env=dict(os.environ, GOWORK="off"))
            self.assertEqual(p.returncode, 0, p.stdout + p.stderr)


class TestKeyproofRowRules(Base):
    """The keyproof table's closed vocabularies, worded like Go's keyproofRowRules."""

    REL = "keyproof/ab.tsv"

    def shard_text(self, rows):
        cols = self.schema.tables["keyproof"].scope_columns("public")
        names = [c.name for c in cols]
        cells = [[shard.escape_cell(r.get(n)) for n in names] for r in rows]
        return shard.render("keyproof", self.schema.version, names, cells)

    def test_bad_rows_yield_exactly_their_rule(self):
        bad = [
            ("leg", keyproof_row(OID_A, "bad-leg", "agreed", leg="bogus"),
             "leg 'bogus' is not xmage, forge, lockstep, scenario or cite"),
            ("state", keyproof_row(OID_A, "bad-state", "bogus"),
             "state 'bogus' is not agreed, diagnosed, diverged, owed, stale or excluded"),
            ("diagnosed without cite", keyproof_row(OID_A, "diag-no-cite", "diagnosed", leg="cite"),
             "diagnosed without a cite"),
            ("cite leg state", keyproof_row(OID_A, "cite-agreed", "agreed", leg="cite", cite="CR 702.4b"),
             "cite leg must be diagnosed, owed or excluded, not 'agreed'"),
            ("seats", keyproof_row(OID_A, "bad-seats", "agreed", seats=1),
             "seats '1' is not 0, 2, 3 or 4"),
            ("agreed zero seats", keyproof_row(OID_A, "zero-seats", "agreed", seats=0),
             "agreed with seats 0"),
        ]
        for name, row, rule in bad:
            with self.subTest(name):
                want = "%s: %s %s %s: %s" % (self.REL, row["oracle_id"], row["key"], row["leg"], rule)
                self.assertEqual(shard.validate_shard(self.schema, "public", self.REL,
                                                      self.shard_text([row])), [want])

    def test_clean_rows_per_leg_yield_nothing(self):
        clean = [
            keyproof_row(OID_A, "cast", "diagnosed", leg="cite", cite="CR 702.4b"),
            keyproof_row(OID_A, "cast", "agreed", leg="forge"),
            keyproof_row(OID_A, "cast", "agreed", leg="lockstep", seats=3),
            keyproof_row(OID_A, "cast", "agreed", leg="scenario"),
            keyproof_row(OID_A, "cast", "agreed", leg="xmage"),
        ]
        self.assertEqual(shard.validate_tree(self.schema, "public",
                                             [(self.REL, self.shard_text(clean))]), [])

    def test_reordered_header_has_the_same_verdict(self):
        head = ["leg", "state", "cite", "seats", "key", "oracle_id"]
        row = ["xmage", "bogus", "-", "2", "cast", OID_A]
        want = ("%s: %s cast xmage: state 'bogus' is not agreed, diagnosed, diverged, owed, stale or "
                "excluded" % (self.REL, OID_A))
        self.assertEqual(shard.keyproof_row_rules(self.REL, head, row), [want])

    def test_put_of_diagnosed_without_cite_writes_nothing(self):
        a = self.machine("a", init=True)
        before = self.snapshot(a.dir)
        with self.assertRaises(ValidationError) as cm:
            a.put("keyproof", [keyproof_row(OID_A, "cast", "diagnosed", leg="cite")], writer="xproof")
        self.assertIn("%s: %s cast cite: diagnosed without a cite" % (self.REL, OID_A), str(cm.exception))
        self.assertEqual(self.snapshot(a.dir), before)


class Publish(Base):
    def test_nothing_staged_is_a_noop(self):
        a = self.machine("a", init=True)
        self.assertIsNone(a.publish())
        a.put("legality", [legality(OID_A, "modern", "legal")], writer="loop")
        r = a.publish()
        self.assertTrue(r["committed"] and r["pushed"])
        self.assertIsNone(a.publish())
        self.assertIsNone(a.publish())

    def test_commit_message_and_single_commit(self):
        a = self.machine("a", init=True)
        a.put("legality", [legality(OID_A, "modern", "legal")], writer="loop")
        a.put("status", [status(OID_A, "pass", "2026-11-19")], writer="cert")
        a.put("legality", [legality(OID_B, "modern", "legal")], writer="loop")
        before = git("rev-list", "--count", "HEAD", cwd=a.dir)
        r = a.publish()
        self.assertEqual(r["message"], "carddb: cert,loop 3 rows @8f7095353a64")
        self.assertEqual(int(git("rev-list", "--count", "HEAD", cwd=a.dir)), int(before) + 1)
        self.assertEqual(git("log", "-1", "--format=%B", cwd=a.dir).strip(), r["message"])
        self.assertEqual(git("rev-parse", "HEAD", cwd=a.dir), git("rev-parse", "carddb", cwd=self.remote))

    def test_refuses_over_50k_lines_without_bulk(self):
        a = self.machine("a", init=True)
        old = store_mod.MAX_PUBLISH_LINES
        store_mod.MAX_PUBLISH_LINES = 5
        self.addCleanup(setattr, store_mod, "MAX_PUBLISH_LINES", old)
        a.put("legality", [legality(OID_A, "f%d" % i, "legal") for i in range(10)], writer="loop")
        with self.assertRaises(store_mod.StoreError):
            a.publish()
        self.assertEqual(git("rev-list", "--count", "HEAD", cwd=a.dir).strip(), "1")  # nothing committed
        self.assertTrue(a.publish(bulk=True)["pushed"])

    def test_boundary_checked_before_commit(self):
        a = self.machine("a", init=True)
        p = os.path.join(a.dir, "legality")
        os.makedirs(p)
        with open(os.path.join(p, "ab.tsv"), "w") as f:
            f.write("#carddb legality v1\noracle_id\tformat\tstatus\n%s\tmodern\tlegal\n%s\tmodern\tlegal\n" % (OID_A, OID_A))
        git("add", "-A", cwd=a.dir)
        with self.assertRaises(ValidationError):
            a.publish()

    def test_two_clones_racing_one_key_sequential(self):
        """Both machines put the same key; the newer measured date wins whatever the push order."""
        for first in ("a", "b"):
            with self.subTest(first=first):
                self._race_sequential(first)

    def _race_sequential(self, first):
        git("init", "--bare", "--quiet", self.remote + first)
        os.environ["CARDDB_PUBLIC_REMOTE"] = self.remote + first
        a = self.machine("a" + first, init=True)
        b = self.machine("b" + first)
        a.put("status", [status(OID_A, "agree", "2026-11-18")], writer="loop")
        b.put("status", [status(OID_A, "diverge", "2026-11-19")], writer="ci")
        b.put("legality", [legality(OID_B, "modern", "legal")], writer="ci")
        a.put("legality", [legality(OID_C, "modern", "legal")], writer="loop")
        for m in ((a, b) if first == "a" else (b, a)):
            self.assertTrue(m.publish()["pushed"])
        a.publish(), b.publish()
        for m in (a, b):
            m.git("pull", "--quiet", "--ff-only", "origin", "carddb")
        self.assertEqual(self.snapshot(a.dir), self.snapshot(b.dir))
        text = read(os.path.join(a.dir, "status", "ab.tsv"))
        self.assertIn("diverge", text)
        self.assertNotIn("agree", text)
        self.assertIn(OID_B, read(os.path.join(a.dir, "legality", "ab.tsv")))
        self.assertIn(OID_C, read(os.path.join(a.dir, "legality", "cd.tsv")))
        writers = read(os.path.join(a.dir, "_meta", "writers.tsv"))
        for w in ("ci\tlegality", "ci\tstatus", "loop\tlegality", "loop\tstatus"):
            self.assertIn(w, writers)
        self.assertEqual(git("log", "--format=%s", "--merges", cwd=a.dir), "")  # linear history

    def test_two_clones_racing_publish_concurrently(self):
        a = self.machine("a", init=True)
        b = self.machine("b")
        a.put("legality", [legality(OID_A, "modern", "legal")], writer="loop")
        b.put("legality", [legality(OID_A, "modern", "banned")], writer="ci")
        results, errs = {}, []
        gate = threading.Barrier(2)

        def run(name, home):
            gate.wait()
            results[name] = self.cli(home, "publish")
        ts = [threading.Thread(target=run, args=(n, os.path.join(self.tmp, n))) for n in ("a", "b")]
        [t.start() for t in ts]
        [t.join() for t in ts]
        for n, r in results.items():
            self.assertEqual(r.returncode, 0, n + ": " + r.stdout + r.stderr)
        self.cli(os.path.join(self.tmp, "a"), "publish")
        self.cli(os.path.join(self.tmp, "b"), "publish")
        for m in (a, b):
            m.git("pull", "--quiet", "--ff-only", "origin", "carddb")
        self.assertEqual(self.snapshot(a.dir), self.snapshot(b.dir))
        rows = read(os.path.join(a.dir, "legality", "ab.tsv")).split("\n")[2:-1]
        self.assertEqual(len(rows), 1)  # one row, no conflict markers
        self.assertNotIn("<<<<", "".join(rows))
        self.assertEqual(git("status", "--porcelain", cwd=a.dir), "")


class Merge(Base):
    def merge(self, base, ours, theirs, table="status"):
        cols = [c.name for c in self.schema.tables[table].columns]
        r = lambda rows: "#carddb %s v1\n%s\n" % (table, "\t".join(cols)) + "".join("\t".join(x) + "\n" for x in rows)
        return merge_mod.merge_texts(self.schema, table + "/ab.tsv", r(base), r(ours), r(theirs))

    def test_commutative_idempotent(self):
        x = [OID_A, "A", "agree", "pin", "2026-11-18", "loop"]
        y = [OID_A, "A", "diverge", "pin", "2026-11-19", "loop"]
        z = [OID_B, "A", "agree", "pin", "2026-11-17", "loop"]
        self.assertEqual(self.merge([], [x, z], [y]), self.merge([], [y], [x, z]))
        self.assertIn("diverge", self.merge([], [x], [y]))
        self.assertNotIn("agree", self.merge([], [x], [y]))
        merged = self.merge([x], [x, z], [x])
        self.assertEqual(merged, self.merge([x], [x], [x, z]))
        self.assertEqual(merged.count("\n"), 4)

    def test_tie_breaks_on_bytes_and_modify_beats_delete(self):
        x = [OID_A, "A", "agree", "pin", "2026-11-19", "ci"]
        y = [OID_A, "A", "zzz", "pin", "2026-11-19", "ci"]
        self.assertEqual(self.merge([], [x], [y]), self.merge([], [y], [x]))
        self.assertIn("zzz", self.merge([], [x], [y]))
        self.assertIn("zzz", self.merge([x], [], [y]))
        self.assertEqual(self.merge([x], [], [x]).count("\n"), 2)  # a clean delete stays deleted

    def test_ci_beats_cert_beats_loop_on_the_real_schema(self):
        """Same measured date, different verdicts: the writer column decides, whatever the verdict bytes say."""
        for table, row in (("status", lambda v, w: [OID_A, "A", v, "pin", "2026-11-19", w]),
                           ("evidence", lambda v, w: [OID_A, "oracle", "s1", v, w]),
                           ("cert", lambda v, w: [OID_A, "lightning bolt", "Lightning Bolt", "pin", "pin",
                                                  "-", "-", "lea", "1993-08-05", v, "-", "-",
                                                  "1", "1", "1", "1", "0", "0", "B", "ok",
                                                  "0", "0", "-", "-", "-", "2026-11-19", w])):
            hi, lo = ("agree", "diverge") if table != "cert" else ("C2", "C0")
            for winner, loser in (("ci", "loop"), ("ci", "cert"), ("cert", "loop")):
                with self.subTest(table=table, winner=winner, loser=loser):
                    w, l = row(lo, winner), row(hi, loser)  # the loser's value sorts later than the winner's
                    # for status "diverge" < "agree" is false: pick so the loser has the greater bytes
                    if "\t".join(l) < "\t".join(w):
                        w, l = row(hi, winner), row(lo, loser)
                    self.assertGreater("\t".join(l), "\t".join(w))  # bytes alone would pick the loser
                    for a, b in ((w, l), (l, w)):
                        merged = self.merge([], [a], [b], table=table)
                        self.assertIn("\t" + winner, merged.split("\n")[2], merged)
                        self.assertEqual(merged.count("\n"), 3)

    def test_newer_measured_still_beats_a_higher_writer(self):
        old_ci = [OID_A, "A", "agree", "pin", "2026-11-18", "ci"]
        new_loop = [OID_A, "A", "diverge", "pin", "2026-11-19", "loop"]
        self.assertIn("loop", self.merge([], [old_ci], [new_loop]).split("\n")[2])
        self.assertIn("loop", self.merge([], [new_loop], [old_ci]).split("\n")[2])

    def test_ci_wins_between_racing_clones_whatever_the_push_order(self):
        for first in ("a", "b"):
            with self.subTest(first=first):
                git("init", "--bare", "--quiet", self.remote + first)
                os.environ["CARDDB_PUBLIC_REMOTE"] = self.remote + first
                a = self.machine("a" + first, init=True)
                b = self.machine("b" + first)
                a.put("status", [status(OID_A, "diverge", "2026-11-19")], writer="loop")  # loop's bytes sort later
                b.put("status", [status(OID_A, "agree", "2026-11-19")], writer="ci")
                for m in ((a, b) if first == "a" else (b, a)):
                    self.assertTrue(m.publish()["pushed"])
                a.publish(), b.publish()
                for m in (a, b):
                    m.git("pull", "--quiet", "--ff-only", "origin", "carddb")
                self.assertEqual(self.snapshot(a.dir), self.snapshot(b.dir))
                row = read(os.path.join(a.dir, "status", "ab.tsv")).split("\n")[2]
                self.assertTrue(row.endswith("agree\t8f7095353a64\t2026-11-19\tci"), row)

    def test_writer_priority_column(self):
        cols = merge_mod.TIMESTAMP_COLUMNS
        self.assertEqual(merge_mod.WRITER_PRIORITY["ci"] > merge_mod.WRITER_PRIORITY["cert"] >
                         merge_mod.WRITER_PRIORITY["loop"], True)
        from carddb.schema import Column
        c = [Column("k", "string", True, True, "public", "d"), Column("writer", "string", True, False, "public", "d"),
             Column("v", "string", True, False, "public", "d")]
        ours, theirs = ["k1", "loop", "zzz"], ["k1", "ci", "aaa"]
        self.assertEqual(merge_mod.merge_rows(c, [], [ours], [theirs]), [theirs])
        self.assertEqual(merge_mod.merge_rows(c, [], [theirs], [ours]), [theirs])

    def test_unmergeable_sides(self):
        with self.assertRaises(merge_mod.MergeError):
            merge_mod.merge_texts(self.schema, "status/ab.tsv", "", "#carddb status v1\nx\n", "#carddb status v1\ny\n")


def kp(oid, key, leg, **kw):
    """One keyproof seed row (the xengine keyproof -seed shape: the v1 ledger
    columns, the seed's fixed cells); kw overrides a cell."""
    row = {"oracle_id": oid, "key": key, "leg": leg, "desc": "-", "state": "owed", "reason": "untried",
           "refs": "-", "source": "seed", "agree_items": "0", "diff_items": "0", "witness": "-",
           "sig": "-", "cite": "-", "ticket": "-", "seats": "0", "multiplayer": "false",
           "gorge_sha": "8f7095353a64", "corpus_pin": "-", "script_sha": "-", "xmage_ref": "-",
           "forge_ref": "-", "measured": "2026-11-19", "replayed": "-", "replayed_sha": "-", "writer": "seed"}
    row.update(kw)
    return row


class TestPutIfAbsent(Base):
    """The keyproof seed put: add the absent rows, change no measured one."""

    def _by_key(self, a):
        return {(r["oracle_id"], r["key"], r["leg"]): r for r in a.rows("keyproof")}

    def test_seed_adds_absent_rows_and_changes_no_measured_row(self):
        a = self.machine("a", init=True)
        a.put("keyproof", [kp(OID_A, "cast", "xmage", state="agreed", reason="-", refs="X", source="steps",
                              agree_items="3", seats="2", writer="xproof")], writer="xproof")
        batch = [kp(OID_A, "cast", "xmage"), kp(OID_A, "cast", "lockstep"),
                 kp(OID_B, "land", "xmage"), kp(OID_B, "land", "lockstep")]
        self.assertEqual(a.put("keyproof", batch, writer="seed", if_absent=True), 3)
        rows = self._by_key(a)
        self.assertEqual(len(rows), 4)  # the agreed row survived, the three absent keys were added
        self.assertEqual(rows[(OID_A, "cast", "xmage")]["state"], "agreed")
        self.assertEqual(rows[(OID_A, "cast", "xmage")]["agree_items"], "3")
        self.assertEqual(rows[(OID_A, "cast", "lockstep")]["state"], "owed")
        self.assertEqual(rows[(OID_A, "cast", "lockstep")]["reason"], "untried")
        before = self.snapshot(a.dir)
        porcelain = git("status", "--porcelain", cwd=a.dir)
        self.assertEqual(a.put("keyproof", list(reversed(batch)), writer="seed", if_absent=True), 0)
        self.assertEqual(self.snapshot(a.dir), before)  # a rerun changes no byte
        self.assertEqual(git("status", "--porcelain", cwd=a.dir), porcelain)

    def test_refresh_takes_only_the_named_columns(self):
        a = self.machine("a", init=True)
        a.put("keyproof", [kp(OID_A, "cast", "xmage", state="agreed", reason="-", refs="X", source="steps",
                              agree_items="3", seats="2", writer="xproof")], writer="xproof")
        # the seed may correct multiplayer (and desc/script_sha) on a stored row
        self.assertEqual(a.put("keyproof", [kp(OID_A, "cast", "xmage", multiplayer="true")],
                              writer="seed", if_absent=True, refresh=("multiplayer",)), 1)
        row = self._by_key(a)[(OID_A, "cast", "xmage")]
        self.assertEqual(row["multiplayer"], "true")
        self.assertEqual(row["state"], "agreed")  # nothing else moved
        self.assertEqual(row["reason"], "-")
        self.assertEqual(row["source"], "steps")
        self.assertEqual(row["agree_items"], "3")
        self.assertEqual(row["seats"], "2")
        self.assertEqual(row["writer"], "xproof")
        # without --refresh the same put changes nothing
        before = self.snapshot(a.dir)
        self.assertEqual(a.put("keyproof", [kp(OID_A, "cast", "xmage", multiplayer="false")],
                              writer="seed", if_absent=True), 0)
        self.assertEqual(self.snapshot(a.dir), before)
        with self.assertRaises(store_mod.StoreError):
            a.put("keyproof", [kp(OID_A, "cast", "xmage")], writer="seed", refresh=("multiplayer",))
        with self.assertRaises(store_mod.StoreError):
            a.put("keyproof", [kp(OID_A, "cast", "xmage")], writer="seed", if_absent=True,
                  refresh=("no_such_column",))

    def test_non_seed_writer_never_sets_multiplayer(self):
        a = self.machine("a", init=True)
        # the seed sets the key's flag
        a.put("keyproof", [kp(OID_A, "cast", "xmage", multiplayer="true"),
                           kp(OID_A, "cast", "lockstep", multiplayer="true")],
              writer="seed", if_absent=True, refresh=("multiplayer",))
        self.assertEqual(self._by_key(a)[(OID_A, "cast", "xmage")]["multiplayer"], "true")
        # a non-seed put of multiplayer=false over the stored true keeps true
        a.put("keyproof", [kp(OID_A, "cast", "xmage", state="agreed", reason="-", refs="X", source="steps",
                              seats="2", writer="xproof")], writer="xproof")
        row = self._by_key(a)[(OID_A, "cast", "xmage")]
        self.assertEqual((row["state"], row["multiplayer"]), ("agreed", "true"))
        # a new leg of the key inherits the stored sibling's value, not the input's
        a.put("keyproof", [kp(OID_A, "cast", "forge", state="agreed", reason="-", refs="F", source="steps",
                              seats="2", writer="xproof")], writer="xproof")
        self.assertEqual(self._by_key(a)[(OID_A, "cast", "forge")]["multiplayer"], "true")
        # a key with no stored sibling starts at false whatever the input says
        a.put("keyproof", [kp(OID_B, "cast", "xmage", state="owed", seats="2", writer="xproof",
                              multiplayer="true")], writer="xproof")
        self.assertEqual(self._by_key(a)[(OID_B, "cast", "xmage")]["multiplayer"], "false")


if __name__ == "__main__":
    unittest.main()
