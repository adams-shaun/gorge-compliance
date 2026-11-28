"""release and size: the release planner, the dry-run path, and the carddb workflow.

The workflow test is the repo's lint fallback: actionlint when the CI image has
it, otherwise a YAML parse plus the assertions the workflow must keep. The
release tests use a fake `gh`, so they never touch the network."""
import datetime
import hashlib
import json
import os
import shutil
import stat
import subprocess
import unittest

from carddb import release as release_mod

from .helpers import ROOT, Base
from .test_build import write


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def payload(chars=4 << 20):
    """Deterministic, hard-to-compress text: the fixture's D7 measurement must
    be a non-trivial number, not a rounding of a few tiny rows."""
    parts, size, i = [], 0, 0
    while size < chars:
        part = hashlib.sha256(("carddb-test-%d" % i).encode()).hexdigest()
        parts.append(part)
        size += len(part)
        i += 1
    return "".join(parts)[:chars]


def object_kib(directory):
    """Total object-store KiB the way packed_mib measures it: loose + packed."""
    p = subprocess.run(["git", "-C", directory, "count-objects", "-v"],
                       capture_output=True, text=True, check=True)
    total = 0
    for line in p.stdout.splitlines():
        key, _, value = line.partition(":")
        if key in ("size", "size-pack"):
            total += int(value.strip())
    return total


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


def fake_gh(tmp, tags, log, list_fails=False):
    """A gh that logs every call: list/view answer from tags, mutations succeed.

    list_fails makes only `release list` fail, the mid-run case where a real
    run must not mistake "could not list" for "nothing to prune"."""
    d = os.path.join(tmp, "bin")
    os.makedirs(d, exist_ok=True)
    gh = os.path.join(d, "gh")
    listing = "exit 1" if list_fails else "echo '%s'" % json.dumps(
        [{"tagName": t} for t in tags])
    write(gh, "#!/bin/sh\n"
              "echo \"$@\" >> '%s'\n"
              "case \"$1 $2\" in\n"
              "  'release list') %s ;;\n"
              "  'release view') for t in %s; do [ \"$3\" = \"$t\" ] && exit 0; done; exit 1 ;;\n"
              "esac\n"
              "exit 0\n" % (log, listing, " ".join(tags)))
    os.chmod(gh, os.stat(gh).st_mode | stat.S_IEXEC)
    return gh


class ReleasePlan(Base):
    def fixture(self):
        pub = self.machine("pub", init=True)
        pub.put("cards", [{"oracle_id": "ab" + "0" * 30, "name": payload(),
                           "layout": "normal", "mana_value": 1, "type_line": "Instant"}],
                writer="mtgjson")
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
        # The D7 number is the branch's real object store, not a 0 placeholder.
        self.assertGreater(r["packed_mib"], 0.0)
        self.assertGreater(meta["packed_mib"], 0.0)
        self.assertAlmostEqual(meta["packed_mib"], object_kib(pub.dir) / 1024.0, places=1)

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
        latest_upload = [c for c in calls if c.startswith("release upload carddb-latest ")]
        weekly_upload = [c for c in calls if c.startswith("release upload carddb-2026-11-16 ")]
        self.assertEqual(len(latest_upload), 1)
        self.assertEqual(len(weekly_upload), 1)
        self.assertIn("carddb-latest.json", latest_upload[0])
        self.assertNotIn("carddb-latest.json", weekly_upload[0])
        self.assertTrue(any(c.startswith("release delete carddb-2026-06-01 ") and "--cleanup-tag" in c
                            for c in calls))

    def test_missing_gh_fails_a_real_run_loudly(self):
        pub, db = self.fixture()
        out = os.path.join(self.tmp, "out.txt")
        with open(out, "w", encoding="utf-8") as f, self.assertRaises(release_mod.StoreError):
            release_mod.run(pub, db, repo="x/y", gh=os.path.join(self.tmp, "no-such-gh"),
                            now="2026-11-19", dry_run=False, out=f)

    def test_a_failed_release_list_is_loud_in_a_real_run(self):
        pub, db = self.fixture()
        log = os.path.join(self.tmp, "gh.log")
        gh = fake_gh(self.tmp, ["carddb-latest"], log, list_fails=True)
        out = os.path.join(self.tmp, "out.txt")
        with open(out, "w", encoding="utf-8") as f, self.assertRaises(release_mod.StoreError) as e:
            release_mod.run(pub, db, repo="x/y", gh=gh, now="2026-11-19", dry_run=False, out=f)
        self.assertIn("release list", str(e.exception))
        calls = read(log).splitlines()
        self.assertFalse(any(c.startswith("release delete ") for c in calls))

    def test_over_budget_fails_before_any_release_mutation(self):
        pub, db = self.fixture()
        log = os.path.join(self.tmp, "gh.log")
        gh = fake_gh(self.tmp, ["carddb-latest"], log)
        out = os.path.join(self.tmp, "out.txt")
        with open(out, "w", encoding="utf-8") as f, self.assertRaises(release_mod.StoreError) as e:
            release_mod.run(pub, db, repo="x/y", gh=gh, now="2026-11-19", dry_run=False,
                            budget_mib=0.001, out=f)
        self.assertIn("D7 budget", str(e.exception))
        self.assertFalse(os.path.exists(log))  # no gh command ran


class SizeVerb(Base):
    def test_size_writes_the_budget_and_validates(self):
        pub = self.machine("pub", init=True)
        pub.put("cards", [{"oracle_id": "ab" + "0" * 30, "name": payload(),
                           "layout": "normal", "mana_value": 1, "type_line": "Instant"}],
                writer="mtgjson")
        r = self.cli(pub.home, "size", "--measured", "2026-11-19")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("data branch packed", r.stdout)
        rows = sorted(pub.rows("_meta/size"), key=lambda x: x["metric"])
        self.assertEqual([(x["metric"], x["unit"], x["measured"]) for x in rows],
                         [("branch_packed_mib", "MiB", "2026-11-19"),
                          ("budget_mib", "MiB", "2026-11-19")])
        self.assertEqual(rows[1]["amount"], "300")
        amount = int(rows[0]["amount"])
        self.assertGreater(amount, 0)
        # The D7 number is this clone's object store, not a packfile-only 0.
        self.assertEqual(amount, int(object_kib(pub.dir) / 1024.0 + 0.5))
        self.assertEqual(self.cli(pub.home, "validate").returncode, 0)
        self.assertEqual(self.cli(pub.home, "boundary").returncode, 0)

    def test_size_dates_meta_and_the_writers_row_from_the_same_committed_date(self):
        # No --measured and no CARDDB_NOW: size_rows must use the data tip's
        # date for BOTH `_meta/size.measured` and `_meta/writers.ran`, not the
        # tip for one and the wall clock for the other.
        old = os.environ.pop("CARDDB_NOW", None)

        def restore():
            if old is not None:
                os.environ["CARDDB_NOW"] = old
        self.addCleanup(restore)
        for k, v in (("GIT_AUTHOR_DATE", "2020-01-02T00:00:00+00:00"),
                     ("GIT_COMMITTER_DATE", "2020-01-02T00:00:00+00:00")):
            old_v = os.environ.get(k)
            os.environ[k] = v
            self.addCleanup(lambda k=k, old_v=old_v: os.environ.pop(k, None) if old_v is None
                            else os.environ.__setitem__(k, old_v))
        pub = self.machine("pub", init=True)
        r = self.cli(pub.home, "size")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual({x["measured"] for x in pub.rows("_meta/size")}, {"2020-01-02"})
        writer = [x for x in pub.rows("_meta/writers") if x["table"] == "_meta/size"]
        self.assertEqual(len(writer), 1)
        self.assertEqual(writer[0]["ran"], "2020-01-02")

    def test_over_budget_fails_before_publishing(self):
        pub = self.machine("pub", init=True)
        pub.put("cards", [{"oracle_id": "ab" + "0" * 30, "name": payload(),
                           "layout": "normal", "mana_value": 1, "type_line": "Instant"}],
                writer="mtgjson")
        r = self.cli(pub.home, "size", "--measured", "2026-11-19", "--max-pack-mb", "0.001")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("refusing to publish", r.stderr)

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
    def test_workflow_copy_leaves_the_data_branch_in_the_publish(self):
        try:
            import yaml
        except ImportError:
            self.skipTest("PyYAML is not installed")
        doc = yaml.safe_load(read(os.path.join(ROOT, ".github/workflows/carddb.yml")))
        steps = doc["jobs"]["publish"]["steps"]
        names = [s.get("name", "") for s in steps]
        at = names.index("Drop the data branch's workflow copy")
        self.assertLess(at, names.index("Publish the data branch"))

        pub = self.machine("pub", init=True)
        copy = ".github/workflows/carddb.yml"
        write(os.path.join(pub.dir, copy), "name: carddb\n")  # what an older publish left there
        pub.git("add", "--", copy)
        pub.git("commit", "--quiet", "-m", "legacy workflow copy")
        pub.git("push", "--quiet", "origin", "HEAD:carddb")

        def tracked():
            return pub.git("ls-tree", "-r", "--name-only", "origin/carddb").stdout.split()

        def drop():
            p = subprocess.run(["bash", "-c", steps[at]["run"]], capture_output=True, text=True,
                               env=dict(os.environ, CARDDB_HOME=pub.home))
            self.assertEqual(p.returncode, 0, p.stdout + p.stderr)

        self.assertIn(copy, tracked())
        drop()
        self.assertTrue(pub.publish(bulk=True)["pushed"])
        self.assertEqual(tracked(), [".gitattributes"])
        drop()  # already gone: nothing staged, nothing to publish
        self.assertIsNone(pub.publish(bulk=True))

    def test_carddb_workflow_yaml_and_lint(self):
        try:
            import yaml
        except ImportError:
            self.skipTest("PyYAML is not installed; actionlint is the CI-side lint")
        path = os.path.join(ROOT, ".github/workflows/carddb.yml")
        text = read(path)
        doc = yaml.safe_load(text)
        trig = doc.get(True) or doc.get("on")
        self.assertNotIn("push", trig)  # writers push every few minutes: only the schedule publishes
        self.assertEqual(trig["schedule"][0]["cron"], "23 */3 * * *")
        self.assertEqual(trig["workflow_dispatch"]["inputs"]["dry_run"]["type"], "boolean")
        self.assertIn("carddb", doc["concurrency"]["group"])
        self.assertIn("run_id", doc["concurrency"]["group"])
        # Only a PR's runs cancel each other: a publishing run is never cut off by the next one.
        self.assertEqual(doc["concurrency"]["cancel-in-progress"], "${{ github.event_name == 'pull_request' }}")
        self.assertEqual(set(doc["jobs"]), {"lint", "check", "publish"})
        publish = doc["jobs"]["publish"]
        self.assertEqual(publish["concurrency"], {"group": "carddb-publish", "cancel-in-progress": False})
        self.assertEqual(publish["permissions"]["contents"], "write")
        # A red check or lint must not let the data branch/release move: an `if`
        # without a status function keeps the implied success() of every need.
        self.assertEqual(publish["needs"], ["check", "lint"])
        self.assertEqual(publish["if"], "github.event_name != 'pull_request'")

        steps = publish["steps"]
        by_name = {s.get("name"): s for s in steps}
        names = [s.get("name", "") for s in steps]
        joined = "\n".join(names + [s.get("run", "") for s in steps]).lower()
        for want in ("catalogue", "compliance", "build", "compress", "parquet", "release"):
            self.assertIn(want, joined)
        self.assertIn("dry_run", joined)

        # The per-table Parquet archive is a release asset: the export step
        # pins its CI-only pyarrow, and the release step ships the archive.
        export_step = by_name["Export the per-table Parquet (pyarrow, CI-only)"]
        self.assertIn("pyarrow==", export_step["run"])
        self.assertIn("carddb-parquet.tar.gz", export_step["run"])

        # The dry-run guarantee is structural: the publish step is skipped
        # exactly on dry_run, and the release step has a real dry-run branch.
        self.assertEqual(by_name["Publish the data branch"].get("if"), "env.DRY_RUN != 'true'")
        release_step = by_name["Publish the release"]["run"]
        self.assertIn('if [ "$DRY_RUN" = "true" ]', release_step)
        self.assertIn("--dry-run", release_step)
        self.assertIn("else", release_step)
        self.assertIn("carddb-parquet.tar.gz", release_step)

        # No wall clock reaches a generated report: schedule/dispatch date from
        # the data tip.
        runs = "\n".join(s.get("run", "") for s in steps)
        self.assertNotIn("date -u +%F", runs)
        self.assertIn("--format=%cs", runs)
        self.assertIn("CARDDB_NOW", runs)
        self.assertNotIn("HEAD_TS", text)
        self.assertIn("MemAvailable", runs)
        self.assertIn("actionlint", text)
        self.assertIn("carddb-latest", text)
        self.assertIn("keep-weeks", text)

        actionlint = shutil.which("actionlint")
        if actionlint:
            p = subprocess.run([actionlint, path], capture_output=True, text=True)
            self.assertEqual(p.returncode, 0, p.stdout + p.stderr)


if __name__ == "__main__":
    unittest.main()
