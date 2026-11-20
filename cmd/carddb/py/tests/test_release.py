"""release and size: the release planner, the dry-run path, and the carddb workflow.

The workflow test is the repo's lint fallback: actionlint when the CI image has
it, otherwise a YAML parse plus the assertions the workflow must keep. The
release tests use a fake `gh`, so they never touch the network."""
import datetime
import json
import os
import shutil
import stat
import unittest

from carddb import release as release_mod

from .helpers import ROOT, Base
from .test_build import write


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


class PrunePlan(unittest.TestCase):
    def test_keeps_twelve_weeks_and_the_newest_of_each_older_quarter(self):
        tags = [
            "carddb-latest",
            "carddb-2026-11-16",   # this week
            "carddb-2026-09-07",   # 10 weeks
            "carddb-2026-08-24",   # 12.4 weeks, only release of Q3
            "carddb-2026-06-01",   # Q2: newer one in the same quarter is kept
            "carddb-2026-06-08",
            "carddb-2026-03-02",   # Q1: same
            "carddb-2026-03-09",
            "carddb-2025-12-01",   # the only Q4 2025 release: kept
            "carddb-not-a-date",
            "other-release",
        ]
        self.assertEqual(release_mod.plan_prune(tags, "2026-11-19"),
                         ["carddb-2026-03-02", "carddb-2026-06-01"])

    def test_prune_is_independent_of_input_order(self):
        tags = ["carddb-2026-03-02", "carddb-2026-03-09", "carddb-2026-06-01",
                "carddb-2026-06-08", "carddb-latest"]
        want = release_mod.plan_prune(tags, "2026-11-19")
        self.assertEqual(want, release_mod.plan_prune(list(reversed(tags)), "2026-11-19"))
        self.assertEqual(want, sorted(want))

    def test_nothing_to_prune_when_everything_is_recent(self):
        self.assertEqual(release_mod.plan_prune(["carddb-2026-11-16"], "2026-11-19"), [])


class WeekTag(unittest.TestCase):
    def test_monday_of_the_iso_week(self):
        self.assertEqual(release_mod.week_tag("2026-11-19"), "carddb-2026-11-16")
        self.assertEqual(release_mod.week_tag("2026-11-16"), "carddb-2026-11-16")
        self.assertEqual(release_mod.week_tag(datetime.date(2026, 11, 22)), "carddb-2026-11-16")


def fake_gh(tmp, tags, log):
    """A gh that logs every call: list/view answer from tags, mutations succeed."""
    d = os.path.join(tmp, "bin")
    os.makedirs(d, exist_ok=True)
    gh = os.path.join(d, "gh")
    write(gh, "#!/bin/sh\n"
              "echo \"$@\" >> '%s'\n"
              "case \"$1 $2\" in\n"
              "  'release list') echo '%s' ;;\n"
              "  'release view') for t in %s; do [ \"$3\" = \"$t\" ] && exit 0; done; exit 1 ;;\n"
              "esac\n"
              "exit 0\n" % (log, json.dumps([{"tagName": t} for t in tags]), " ".join(tags)))
    os.chmod(gh, os.stat(gh).st_mode | stat.S_IEXEC)
    return gh


class ReleasePlan(Base):
    def fixture(self):
        pub = self.machine("pub", init=True)
        pub.put("cards", [{"oracle_id": "ab" + "0" * 30, "name": "Bolt", "layout": "normal",
                           "mana_value": 1, "type_line": "Instant"}], writer="mtgjson")
        db = os.path.join(self.tmp, "carddb.sqlite")
        write(db, "SQLite format 3\x00 not a real database\n")
        write(db + ".zst", "zstd frame\n")
        return pub, db

    def test_dry_run_plans_the_latest_weekly_and_prune_without_mutating(self):
        pub, db = self.fixture()
        log = os.path.join(self.tmp, "gh.log")
        gh = fake_gh(self.tmp, ["carddb-latest", "carddb-2026-11-16",
                                "carddb-2026-06-01", "carddb-2026-06-08"], log)
        out = os.path.join(self.tmp, "out.txt")
        with open(out, "w", encoding="utf-8") as f:
            r = release_mod.run(pub, db, repo="x/y", gh=gh, now="2026-11-19", dry_run=True,
                                keep_weeks=12, budget_mib=300, out=f)
        text = read(out)
        self.assertEqual(r["weekly"], "carddb-2026-11-16")
        self.assertEqual(r["pruned"], ["carddb-2026-06-01"])
        self.assertIn("[dry-run]", text)
        self.assertIn("release upload carddb-latest", text)
        self.assertIn("release upload carddb-2026-11-16", text)
        self.assertIn("release delete carddb-2026-06-01", text)
        self.assertIn("packed", text)
        calls = read(log).splitlines()
        self.assertTrue(calls)
        for line in calls:
            self.assertTrue(line.startswith("release view ") or line.startswith("release list "), line)
        meta = json.loads(read(os.path.join(self.tmp, "carddb-latest.json")))
        self.assertEqual(meta["budget_mib"], 300)
        self.assertEqual(meta["tag"], "carddb-latest")
        self.assertEqual(meta["asset"], "carddb.sqlite.zst")
        self.assertEqual(len(meta["sha256"]), 64)

    def test_a_real_run_creates_the_week_uploads_and_deletes(self):
        pub, db = self.fixture()
        log = os.path.join(self.tmp, "gh.log")
        gh = fake_gh(self.tmp, ["carddb-latest", "carddb-2026-06-01", "carddb-2026-06-08"], log)
        out = os.path.join(self.tmp, "out.txt")
        with open(out, "w", encoding="utf-8") as f:
            r = release_mod.run(pub, db, repo="x/y", gh=gh, now="2026-11-19", dry_run=False,
                                keep_weeks=12, budget_mib=300, out=f)
        self.assertTrue(r["created_weekly"])
        self.assertEqual(r["pruned"], ["carddb-2026-06-01"])
        calls = read(log).splitlines()
        self.assertTrue(any(c.startswith("release create carddb-2026-11-16 ") for c in calls))
        self.assertTrue(any(c.startswith("release upload carddb-latest ") and "--clobber" in c
                            for c in calls))
        self.assertTrue(any(c.startswith("release delete carddb-2026-06-01 ") and "--cleanup-tag" in c
                            for c in calls))

    def test_missing_gh_fails_a_real_run_loudly(self):
        pub, db = self.fixture()
        out = os.path.join(self.tmp, "out.txt")
        with open(out, "w", encoding="utf-8") as f, self.assertRaises(release_mod.StoreError):
            release_mod.run(pub, db, repo="x/y", gh=os.path.join(self.tmp, "no-such-gh"),
                            now="2026-11-19", dry_run=False, out=f)


class SizeVerb(Base):
    def test_size_writes_the_budget_and_validates(self):
        pub = self.machine("pub", init=True)
        r = self.cli(pub.home, "size", "--measured", "2026-11-19")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("data branch packed", r.stdout)
        rows = sorted(pub.rows("_meta/size"), key=lambda x: x["metric"])
        self.assertEqual([(x["metric"], x["unit"], x["measured"]) for x in rows],
                         [("branch_packed_mib", "MiB", "2026-11-19"),
                          ("budget_mib", "MiB", "2026-11-19")])
        self.assertEqual(rows[1]["amount"], "300")
        self.assertEqual(self.cli(pub.home, "validate").returncode, 0)
        self.assertEqual(self.cli(pub.home, "boundary").returncode, 0)

    def test_release_dry_run_builds_without_uploading(self):
        pub = self.machine("pub", init=True)
        pub.put("cards", [{"oracle_id": "ab" + "0" * 30, "name": "Bolt", "layout": "normal",
                           "mana_value": 1, "type_line": "Instant"}], writer="mtgjson")
        src = os.path.join(self.tmp, "src")
        write(os.path.join(src, "carddb/curated/labels.tsv"),
              "#carddb curated/labels v1\noracle_id\tlabel\tnote\n")
        db = os.path.join(self.tmp, "carddb.sqlite")
        r = self.cli(pub.home, "build", "--src-dir", src, "--out", db)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(os.path.isfile(db))
        log = os.path.join(self.tmp, "gh.log")
        gh = fake_gh(self.tmp, ["carddb-latest"], log)
        r = self.cli(pub.home, "release", "--db", db, "--repo", "x/y",
                     "--gh", gh, "--now", "2026-11-19", "--dry-run")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("[dry-run]", r.stdout)
        self.assertIn("release upload", r.stdout)
        self.assertFalse(any(c.startswith(("release upload", "release create", "release delete"))
                             for c in read(log).splitlines()))


class Workflow(Base):
    def test_carddb_workflow_yaml_and_lint(self):
        try:
            import yaml
        except ImportError:
            self.skipTest("PyYAML is not installed; actionlint is the CI-side lint")
        path = os.path.join(ROOT, ".github/workflows/carddb.yml")
        doc = yaml.safe_load(read(path))
        trig = doc.get(True) or doc.get("on")
        self.assertIn("carddb", trig["push"]["branches"])
        self.assertEqual(trig["schedule"][0]["cron"], "23 4 * * *")
        self.assertEqual(trig["workflow_dispatch"]["inputs"]["dry_run"]["type"], "boolean")
        self.assertIn("carddb", doc["concurrency"]["group"])
        self.assertTrue(doc["concurrency"]["cancel-in-progress"])
        self.assertTrue({"lint", "check", "ready", "publish"} <= set(doc["jobs"]))
        self.assertEqual(doc["jobs"]["publish"]["permissions"]["contents"], "write")
        steps = doc["jobs"]["publish"]["steps"]
        names = [s.get("name", "") for s in steps]
        joined = "\n".join(names + [s.get("run", "") for s in steps]).lower()
        for want in ("catalogue", "compliance", "build", "compress", "release"):
            self.assertIn(want, joined)
        self.assertIn("dry_run", joined)
        self.assertIn("actionlint", read(path))
        self.assertIn("carddb-latest", read(path))
        self.assertIn("keep-weeks", read(path))

        actionlint = shutil.which("actionlint")
        if actionlint:
            import subprocess
            p = subprocess.run([actionlint, path], capture_output=True, text=True)
            self.assertEqual(p.returncode, 0, p.stdout + p.stderr)


if __name__ == "__main__":
    unittest.main()
