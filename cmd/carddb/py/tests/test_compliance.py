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
    "bbb": '{"code": "bbb", "cards": [{"name": "Day"}], "unfinished": []}',
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
        self.assertEqual(rows[(OID_C, "xmage")]["status"], "listed")  # listed by its back face
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
        with open(os.path.join(repo, "oracle", "compliance", "manifests", "aaa.json"), "w") as f:
            f.write('{"code": "aaa", "cards": [{"name": "Bolt"}, {"name": "Slop"}], "unfinished": []}')
        changed, _ = compliance_mod.run(a, repo=repo)
        self.assertEqual(changed, 1)
        after = read(shard).split("\n")
        self.assertEqual(before[0], after[0])  # the shard header is untouched
        self.assertEqual(len(before), len(after))
        self.assertEqual(sum(1 for x, y in zip(before, after) if x != y), 1)

    def test_unfinished_wins_over_a_listed_face(self):
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


class ComplianceCli(Base):
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
