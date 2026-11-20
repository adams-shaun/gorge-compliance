"""The compliance status writer: XMage manifests + FORGE_ORACLE_REF -> support rows.

Ported from four-yay tools/deckbuild/deckdb.py load_engines: a corpus card is
xmage-listed, xmage-unfinished or xmage-lacks_card from the manifests, and
forge-listed for every corpus card (Forge covers the whole corpus).

Matching is on the card's front face (norm_name, like deckdb's nn): listed
first, then unfinished. The farm's failing overrides (CDB-14, writer `farm`)
live in the same table at the same keys; a compliance run never overwrites a
failing row, it leaves it for the farm writer to refresh. For a reproducible
`measured` date, the caller passes `ran` (the CLI's --measured) or sets
CARDDB_NOW; without one the run is dated today (UTC) and a rerun on another
day restamps the rows.
"""
import glob
import json
import os
import subprocess

from .norm import norm_name
from .schema import repo_root
from .store import StoreError, today

UNFINISHED_WHY = "manifest lists it unfinished"
LACKS_CARD_WHY = "no XMage manifest lists it"


def forge_oracle_ref(makefile):
    """FORGE_ORACLE_REF from the Makefile (deckdb.py:434-437), first 10 hex."""
    try:
        with open(makefile, encoding="utf-8") as f:
            for line in f:
                if line.startswith("FORGE_ORACLE_REF"):
                    if "=" not in line:
                        raise StoreError("compliance: %s: FORGE_ORACLE_REF line has no '='" % makefile)
                    return line.split("=", 1)[1].strip()[:10]
    except OSError as e:
        raise StoreError("compliance: %s: %s" % (makefile, e))
    raise StoreError("compliance: %s has no FORGE_ORACLE_REF line" % makefile)


def compliance_rev(repo):
    """The compliance checkout's HEAD (10 hex): the pin the XMage manifests are at."""
    p = subprocess.run(["git", "-C", repo, "rev-parse", "--short=10", "HEAD"],
                       capture_output=True, text=True)
    if p.returncode != 0:
        raise StoreError("compliance: %s: %s" % (repo, p.stderr.strip()))
    return p.stdout.strip()


def load_manifests(mdir):
    """(listed {norm_name -> card name}, unfinished {norm_name}) over manifests/*.json,
    deckdb.py:438-448: every face of a manifest card counts as listed unless the card
    is listed unfinished."""
    listed, unfinished = {}, set()
    for fn in sorted(glob.glob(os.path.join(mdir, "*.json"))):
        try:
            with open(fn, encoding="utf-8") as f:
                m = json.load(f)
        except (OSError, ValueError) as e:
            raise StoreError("compliance: %s: %s" % (fn, e))
        unf = {norm_name(n) for n in m.get("unfinished") or []}
        unf.discard("")  # a name that normalises to nothing (e.g. `_____`) cannot be matched
        unfinished |= unf
        for c in m.get("cards") or []:
            n = c.get("name") or ""
            for face in [n] + [f.strip() for f in n.split("//")]:
                k = norm_name(face)
                if k and k not in unf:
                    listed.setdefault(k, n)
    return listed, unfinished


def support_rows(cards, listed, unfinished, xmage_pin, forge_pin, measured):
    """One xmage row and one forge row per corpus card (deckdb.py:486-491): matched
    on the card's front face (norm_name, deckdb's nn) — listed first, then
    unfinished, else lacks_card; forge is listed for every corpus card."""
    rows = []
    for oid, name in cards:
        nn = norm_name(name)
        if nn in listed:
            st, why = "listed", ""
        elif nn in unfinished:
            st, why = "unfinished", UNFINISHED_WHY
        else:
            st, why = "lacks_card", LACKS_CARD_WHY
        rows.append({"oracle_id": oid, "engine": "xmage", "status": st, "pin": xmage_pin,
                     "why": why, "measured": measured})
        rows.append({"oracle_id": oid, "engine": "forge", "status": "listed", "pin": forge_pin,
                     "why": "", "measured": measured})
    return rows


def run(store, repo=None, writer="ci", version=None, ran=None):
    """Compute the support statuses and put them. -> (rows changed, {(engine, status): n})."""
    repo = repo or repo_root()
    mdir = os.path.join(repo, "oracle", "compliance", "manifests")
    if not glob.glob(os.path.join(mdir, "*.json")):
        raise StoreError("compliance: %s holds no XMage manifests" % mdir)
    listed, unfinished = load_manifests(mdir)
    xmage_pin = compliance_rev(repo)
    forge_pin = forge_oracle_ref(os.path.join(repo, "Makefile"))
    rows = support_rows(store.cards(), listed, unfinished, xmage_pin, forge_pin, today(ran))
    failing = {(r["oracle_id"], r["engine"]) for r in store.rows("support")
               if r["status"] == "failing"}
    if failing:
        rows = [r for r in rows if (r["oracle_id"], r["engine"]) not in failing]
    counts = {}
    for r in rows:
        counts[(r["engine"], r["status"])] = counts.get((r["engine"], r["status"]), 0) + 1
    changed = store.put("support", rows, writer=writer, version=version, ran=ran)
    return changed, counts
