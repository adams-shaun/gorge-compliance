import os
import unittest

from carddb import compliance as compliance_mod
from carddb import store as store_mod
from .helpers import Base, OID_A, OID_B, OID_C, git

OID_D = "ef" + "3" * 30
FORGE_REF = "b33362f6004458d1509a7f44a87dcbe279f56040"
MEASURED = "2026-11-19"

MANIFESTS = {
    "aaa": '{"code": "aaa", "cards": [{"name": "Bolt"}], "unfinished": ["Slop"]}',
    "bbb": '{"code": "bbb", "cards": [{"name": "Night // Day"}], "unfinished": []}',
}


def read(p):
    with open(p, encoding="utf-8") as f:
        return f.read()


def card(oid, name, layout="normal", mana_value=1, type_line="Instant"):
    return {"oracle_id": oid, "name": name, "layout": layout, "mana_value": mana_value,
            "type_line": type_line}


def fixture_repo(tmp, manifests=MANIFESTS):
    """A checkout-sized git repo: Makefile with FORGE_ORACLE_REF and XMage manifests."""
    d = os.path.join(tmp, "repo")
    os.makedirs(os.path.join(d, "oracle", "compliance", "manifests"))
    with open(os.path.join(d, "Makefile"), "w") as f:
        f.write("FORGE_ORACLE_REF ?= %s\n" % FORGE_REF)
    for code, m in manifests.items():
        with open(os.path.join(d, "oracle", "compliance", "manifests", code + ".json"), "w") as f:
            f.write(m)
    git("init", "--quiet", cwd=d)
    git("-c", "user.email=t@t", "-c", "user.name=t", "add", "-A", cwd=d)
    git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "--quiet", "-m", "m", cwd=d)
    return d


def corpus(store):
    store.put("cards", [card(OID_A, "Bolt"), card(OID_B, "Slop"),
                        card(OID_C, "Night // Day"), card(OID_D, "Whim")], writer="mtgjson")


class Compliance(Base):
    def rows(self, store):
        return {(r["oracle_id"], r["engine"]): r for r in store.rows("support")}

    def test_statuses_pins_and_writers(self):
        a = self.machine("a", init=True)
        corpus(a)
        repo = fixture_repo(self.tmp)
        rev = git("rev-parse", "--short=10", "HEAD", cwd=repo).strip()
        changed, counts = compliance_mod.run(a, repo=repo)
        self.assertEqual(changed, 8)
        self.assertEqual(counts, {("xmage", "listed"): 2, ("xmage", "unfinished"): 1,
                                  ("xmage", "lacks_card"): 1, ("forge", "listed"): 4})
        rows = self.rows(a)
        self.assertEqual(rows[(OID_A, "xmage")],
                         {"oracle_id": OID_A, "engine": "xmage", "status": "listed", "pin": rev,
                          "why": "-", "measured": MEASURED, "writer": "ci"})
        self.assertEqual(rows[(OID_B, "xmage")]["status"], "unfinished")
        self.assertEqual(rows[(OID_B, "xmage")]["why"], "manifest lists it unfinished")
        self.assertEqual(rows[(OID_C, "xmage")]["status"], "listed")  # listed by its full name
        self.assertEqual(rows[(OID_D, "xmage")]["status"], "lacks_card")
        self.assertEqual(rows[(OID_D, "xmage")]["why"], "no XMage manifest lists it")
        for (oid, eng), r in rows.items():
            self.assertEqual(r["pin"], rev if eng == "xmage" else FORGE_REF[:10])
            self.assertEqual(r["measured"], MEASURED)
            self.assertEqual(r["writer"], "ci")
            if eng == "forge":
                self.assertEqual(r["status"], "listed")
                self.assertEqual(r["why"], "-")
        writers = [r for r in a.rows("_meta/writers") if r["table"] == "support"]
        self.assertEqual(writers, [{"writer": "ci", "table": "support", "version": "unversioned",
                                    "ran": MEASURED}])

    def test_rerun_changes_nothing(self):
        a = self.machine("a", init=True)
        corpus(a)
        repo = fixture_repo(self.tmp)
        compliance_mod.run(a, repo=repo)
        before = self.snapshot(a.dir)
        porcelain = git("status", "--porcelain", cwd=a.dir)
        changed, _ = compliance_mod.run(a, repo=repo)
        self.assertEqual(changed, 0)
        self.assertEqual(self.snapshot(a.dir), before)
        self.assertEqual(git("status", "--porcelain", cwd=a.dir), porcelain)

    def test_unfinished_to_listed_is_one_line_diff(self):
        a = self.machine("a", init=True)
        corpus(a)
        repo = fixture_repo(self.tmp)
        compliance_mod.run(a, repo=repo)
        shard = os.path.join(a.dir, "support", "ab.tsv")
        before = read(shard).split("\n")
        rev = git("rev-parse", "--short=10", "HEAD", cwd=repo).strip()
        with open(os.path.join(repo, "oracle", "compliance", "manifests", "aaa.json"), "w") as f:
            f.write('{"code": "aaa", "cards": [{"name": "Bolt"}, {"name": "Slop"}], "unfinished": []}')
        changed, _ = compliance_mod.run(a, repo=repo)
        self.assertEqual(changed, 1)
        after = read(shard).split("\n")
        self.assertEqual(before[0], after[0])  # the shard header is untouched
        self.assertEqual(len(before), len(after))
        diffs = [i for i, (x, y) in enumerate(zip(before, after)) if x != y]
        self.assertEqual(diffs, [before.index("\t".join(
            [OID_B, "xmage", "unfinished", rev, "manifest lists it unfinished", MEASURED, "ci"]))])
        self.assertEqual(after[diffs[0]], "\t".join(
            [OID_B, "xmage", "listed", rev, "-", MEASURED, "ci"]))

    def test_unfinished_wins_within_its_manifest(self):
        a = self.machine("a", init=True)
        repo = fixture_repo(self.tmp, manifests={
            "aaa": '{"code": "aaa", "cards": [{"name": "Fire // Ice"}], "unfinished": ["Fire // Ice"]}',
            "bbb": '{"code": "bbb", "cards": [{"name": "Bolt"}], "unfinished": []}'})
        a.put("cards", [card(OID_A, "Fire // Ice", layout="split", mana_value=2,
                             type_line="Instant // Instant")], writer="mtgjson")
        compliance_mod.run(a, repo=repo)
        rows = self.rows(a)
        self.assertEqual(rows[(OID_A, "xmage")]["status"], "unfinished")
        self.assertEqual(rows[(OID_A, "xmage")]["why"], "manifest lists it unfinished")
        self.assertEqual(rows[(OID_A, "forge")]["status"], "listed")

    def test_listed_beats_unfinished_across_manifests(self):
        """deckdb checks listed before unfinished (deckdb.py:487-489): a card listed by
        one manifest is listed even when another manifest's unfinished list names it."""
        a = self.machine("a", init=True)
        repo = fixture_repo(self.tmp, manifests={
            "aaa": '{"code": "aaa", "cards": [{"name": "Slop"}], "unfinished": []}',
            "bbb": '{"code": "bbb", "cards": [], "unfinished": ["Slop"]}'})
        a.put("cards", [card(OID_A, "Slop")], writer="mtgjson")
        compliance_mod.run(a, repo=repo)
        self.assertEqual(self.rows(a)[(OID_A, "xmage")]["status"], "listed")

    def test_back_face_only_listing_is_not_a_match(self):
        """deckdb matches the front face only (nn = norm_name): a manifest listing the
        standalone back face `Day` does not list the DFC Night // Day."""
        a = self.machine("a", init=True)
        repo = fixture_repo(self.tmp, manifests={
            "aaa": '{"code": "aaa", "cards": [{"name": "Day"}], "unfinished": []}'})
        a.put("cards", [card(OID_A, "Night // Day")], writer="mtgjson")
        compliance_mod.run(a, repo=repo)
        self.assertEqual(self.rows(a)[(OID_A, "xmage")]["status"], "lacks_card")

    def test_failing_rows_are_preserved(self):
        """The farm's failing override (CDB-14) is never clobbered by a compliance run."""
        a = self.machine("a", init=True)
        corpus(a)
        repo = fixture_repo(self.tmp)
        a.put("support", [{"oracle_id": OID_B, "engine": "xmage", "status": "failing",
                           "pin": "farm0pin12", "why": "step", "measured": MEASURED,
                           "writer": "farm"}], writer="farm")
        compliance_mod.run(a, repo=repo)
        rows = self.rows(a)
        self.assertEqual(rows[(OID_B, "xmage")],
                         {"oracle_id": OID_B, "engine": "xmage", "status": "failing",
                          "pin": "farm0pin12", "why": "step", "measured": MEASURED,
                          "writer": "farm"})

    def test_norm_name_matches_across_diacritics(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Juzam Djinn", type_line="Summon Djinn")], writer="mtgjson")
        repo = fixture_repo(self.tmp, manifests={
            "aaa": '{"code": "aaa", "cards": [{"name": "Juz\\u00e1m Djinn"}], "unfinished": []}'})
        compliance_mod.run(a, repo=repo)
        self.assertEqual(self.rows(a)[(OID_A, "xmage")]["status"], "listed")

    def test_refuses_a_repo_without_manifests_or_ref(self):
        a = self.machine("a", init=True)
        corpus(a)
        repo = fixture_repo(self.tmp)
        os.remove(os.path.join(repo, "oracle", "compliance", "manifests", "aaa.json"))
        os.remove(os.path.join(repo, "oracle", "compliance", "manifests", "bbb.json"))
        with self.assertRaises(store_mod.StoreError):
            compliance_mod.run(a, repo=repo)
        mf = os.path.join(repo, "Makefile")
        os.rename(mf, mf + "~")
        with open(mf, "w") as f:
            f.write("FORGE_REPO ?= x\n")
        with open(os.path.join(repo, "oracle", "compliance", "manifests", "aaa.json"), "w") as f:
            f.write('{"code": "aaa", "cards": [{"name": "Bolt"}], "unfinished": []}')
        with self.assertRaises(store_mod.StoreError):
            compliance_mod.run(a, repo=repo)
        os.remove(mf)  # no Makefile at all: StoreError, not FileNotFoundError
        with self.assertRaises(store_mod.StoreError):
            compliance_mod.run(a, repo=repo)
        os.rename(mf + "~", mf)
        with open(mf, "w") as f:
            f.write("FORGE_ORACLE_REF x\n")  # a FORGE_ORACLE_REF line without '='
        with self.assertRaises(store_mod.StoreError):
            compliance_mod.run(a, repo=repo)


class ComplianceCli(Base):
    def rows(self, store):
        return {(r["oracle_id"], r["engine"]): r for r in store.rows("support")}

    def test_cli_punctuation_only_card_exits_0(self):
        """A card whose name normalises to nothing (`_____`) is lacks_card, not a crash."""
        a = self.machine("a", init=True)
        home = os.path.join(self.tmp, "a")
        a.put("cards", [card(OID_C, "_____")], writer="mtgjson")
        repo = fixture_repo(self.tmp, manifests={
            "aaa": '{"code": "aaa", "cards": [{"name": "_____"}], "unfinished": []}'})
        p = self.cli(home, "compliance", "--repo", repo)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn("carddb compliance: 2 rows changed; forge:listed=1 xmage:lacks_card=1", p.stdout)
        self.assertEqual(self.rows(a)[(OID_C, "xmage")]["status"], "lacks_card")
        self.assertEqual(self.rows(a)[(OID_C, "xmage")]["why"], "no XMage manifest lists it")

    def test_cli_measured_flag(self):
        a = self.machine("a", init=True)
        home = os.path.join(self.tmp, "a")
        corpus(a)
        repo = fixture_repo(self.tmp)
        p = self.cli(home, "compliance", "--repo", repo, "--measured", "2026-01-02")
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn("carddb compliance: 8 rows changed", p.stdout)
        for r in self.rows(a).values():
            self.assertEqual(r["measured"], "2026-01-02")

    def test_cli_verb(self):
        a = self.machine("a", init=True)
        home = os.path.join(self.tmp, "a")
        corpus(a)
        repo = fixture_repo(self.tmp)
        p = self.cli(home, "compliance", "--repo", repo)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn("carddb compliance: 8 rows changed; forge:listed=4", p.stdout)
        self.assertIn("xmage:lacks_card=1 xmage:listed=2 xmage:unfinished=1", p.stdout)
        shard = os.path.join(a.dir, "support", "ab.tsv")
        before = read(shard)
        p = self.cli(home, "compliance", "--repo", repo)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn("carddb compliance: 0 rows changed", p.stdout)
        self.assertEqual(read(shard), before)
        p = self.cli(home, "compliance", "--repo", os.path.join(self.tmp, "nope"))
        self.assertEqual(p.returncode, 2)
        self.assertIn("no XMage manifests", p.stderr)


if __name__ == "__main__":
    unittest.main()
