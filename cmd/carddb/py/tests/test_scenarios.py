import json
import os

from carddb import scenarios as scenarios_mod
from carddb.store import StoreError
from .helpers import Base, OID_A, OID_B, OID_C
from .test_compliance import card

OID_D = "de" + "3" * 30
OID_LAND = "ef" + "4" * 30


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def scenario(name, xmage, *cards):
    """A translated scenario: a Mountain only the setup places, one step per ref in cards."""
    return {"name": name, "xmage": xmage, "setup": {"p0": {"battlefield": ["Mountain"]}},
            "steps": [{"op": "cast", "card": c} for c in cards] + [{"op": "pass_to"}]}


def port_file(repo, family, cls, *scs):
    write(os.path.join(repo, "scenarios", "xmageport", family, cls + ".json"),
          json.dumps({"source": cls + ".java", "scenarios": list(scs)}))


def hand_file(repo, family, stem, name, *scenario_names):
    write(os.path.join(repo, "scenarios", "oracle", family, stem + ".json"),
          json.dumps({"card": name, "family": family, "scenarios": [{"name": n} for n in scenario_names]}))


def ratchet(repo, family, kind, stem, name, *scenario_names):
    write(os.path.join(repo, "scenarios", "oracle", family, kind, stem + ".json"),
          json.dumps({"card": name, "rows": {n: "why" for n in scenario_names}}))


def fixture(tmp):
    """Two translated files and one hand file: BoltTest#bug is listed in known_bugs.tsv."""
    repo = os.path.join(tmp, "repo")
    write(os.path.join(repo, "cmd", "xmageport", "known_bugs.tsv"),
          "# XMage Class#method <TAB> reason\nBoltTest#bug\ta gorge bug\n# BearTest#ok\tcommented out\n")
    port_file(repo, "damage", "BoltTest",
              scenario("bug", "BoltTest#bug", "p0:Bolt", "p1:Bear#2", "p0:Bolt#2", "p0:token:Whim"))
    port_file(repo, "copy", "BearTest",
              scenario("ok", "BearTest#ok", "p0:Bear", "p0:ability:Night#2", "p0:No Such Card"),
              scenario("quiet", "BearTest#quiet"))
    hand_file(repo, "etb-ltb", "whim-slug", "Whim", "enters", "leaves")
    return repo


class TestScenarioWriter(Base):
    def store(self):
        a = self.machine("a", init=True)
        a.put("cards", [card(OID_A, "Bolt"), card(OID_B, "Bear"), card(OID_C, "Night // Day", layout="split"),
                        card(OID_D, "Whim"), card(OID_LAND, "Mountain")], writer="mtgjson")
        return a

    @staticmethod
    def evidence(store):
        return sorted((r["oracle_id"], r["source"], r["ref"], r["outcome"]) for r in store.rows("evidence"))

    def test_outcomes_follow_the_known_bug_list(self):
        a = self.store()
        r = scenarios_mod.run(a, repo=fixture(self.tmp))
        self.assertEqual(self.evidence(a), sorted([
            (OID_A, "xmageport", "BoltTest#bug", "known_bug"),
            (OID_B, "xmageport", "BoltTest#bug", "known_bug"),
            (OID_B, "xmageport", "BearTest#ok", "pass"),
            (OID_C, "xmageport", "BearTest#ok", "pass"),  # an ability ref names its source's front face
            (OID_D, "hand", "etb-ltb/whim-slug/enters", "pass"),
            (OID_D, "hand", "etb-ltb/whim-slug/leaves", "pass"),
        ]))
        self.assertEqual(r["unresolved"], ["No Such Card"])
        self.assertEqual(r["changed"], 6)
        self.assertEqual(r["outcomes"], {("xmageport", "known_bug"): 2, ("xmageport", "pass"): 2,
                                         ("hand", "pass"): 2})
        self.assertEqual({r["writer"] for r in a.rows("evidence")}, {"scenario"})
        self.assertIn(("evidence", "scenario", "2026-11-19"),
                      {(r["table"], r["writer"], r["ran"]) for r in a.rows("_meta/writers")})

    def test_setup_only_cards_get_no_row(self):
        a = self.store()
        scenarios_mod.run(a, repo=fixture(self.tmp))
        got = {oid for oid, _s, _r, _o in self.evidence(a)}
        self.assertNotIn(OID_LAND, got)  # the Mountain is placed by setup and no step names it
        self.assertNotIn("BearTest#quiet", {ref for _o, _s, ref, _x in self.evidence(a)})

    def test_rerun_deletes_stale_rows_and_leaves_other_sources(self):
        a = self.store()
        a.put("evidence", [{"oracle_id": OID_A, "source": "dirac-lockstep", "ref": "blb", "outcome": "agree"}],
              writer="dirac")
        repo = fixture(self.tmp)
        scenarios_mod.run(a, repo=repo)
        other = [r for r in a.rows("evidence") if r["source"] == "dirac-lockstep"]
        before = self.snapshot(a.dir)
        self.assertEqual(scenarios_mod.run(a, repo=repo)["changed"], 0)
        self.assertEqual(self.snapshot(a.dir), before)
        port_file(repo, "damage", "BoltTest", scenario("other", "BoltTest#other", "p0:Bolt"))
        hand_file(repo, "etb-ltb", "whim-slug", "Whim", "enters")
        scenarios_mod.run(a, repo=repo)
        self.assertEqual(self.evidence(a), sorted([
            (OID_A, "dirac-lockstep", "blb", "agree"),
            (OID_A, "xmageport", "BoltTest#other", "pass"),
            (OID_B, "xmageport", "BearTest#ok", "pass"),
            (OID_C, "xmageport", "BearTest#ok", "pass"),
            (OID_D, "hand", "etb-ltb/whim-slug/enters", "pass"),
        ]))
        self.assertEqual([r for r in a.rows("evidence") if r["source"] == "dirac-lockstep"], other)

    def test_a_ratcheted_hand_scenario_is_not_a_pass(self):
        a = self.store()
        repo = fixture(self.tmp)
        hand_file(repo, "etb-ltb", "whim-slug", "Whim", "enters", "leaves", "both")
        ratchet(repo, "etb-ltb", "known-divergent", "whim-slug", "Whim", "enters", "both")
        ratchet(repo, "etb-ltb", "known-unconsumed", "whim-slug", "Whim", "leaves", "both")
        ratchet(repo, "evasion", "known-divergent", "whim-slug", "Whim", "leaves")  # another family's row
        scenarios_mod.run(a, repo=repo)
        got = {ref: o for _oid, source, ref, o in self.evidence(a) if source == "hand"}
        self.assertEqual(got, {"etb-ltb/whim-slug/enters": "diverge", "etb-ltb/whim-slug/leaves": "unconsumed",
                               "etb-ltb/whim-slug/both": "diverge"})

    def test_a_repo_without_a_corpus_deletes_nothing(self):
        a = self.store()
        repo = fixture(self.tmp)
        scenarios_mod.run(a, repo=repo)
        before = self.evidence(a)
        os.remove(os.path.join(repo, "scenarios", "oracle", "etb-ltb", "whim-slug.json"))
        with self.assertRaises(StoreError):
            scenarios_mod.run(a, repo=repo)
        self.assertEqual(self.evidence(a), before)

    def test_cli_verb(self):
        self.store()
        repo = fixture(self.tmp)
        home = os.path.join(self.tmp, "a")
        p = self.cli(home, "scenarios", "--repo", repo, "--measured", "2026-11-20")
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
        self.assertEqual(p.stdout, "carddb scenarios: 6 rows changed; hand:pass=2 xmageport:known_bug=2 "
                                   "xmageport:pass=2; 1 unresolved names skipped\n")
        self.assertIn(("scenario", "2026-11-20"),
                      {(r["writer"], r["ran"]) for r in self.machine("a").rows("_meta/writers")})
