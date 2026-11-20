"""The compliance status writer: XMage manifests + FORGE_ORACLE_REF -> support rows.

Ported from four-yay tools/deckbuild/deckdb.py load_engines: a corpus card is
xmage-listed, xmage-unfinished or xmage-lacks_card from the manifests, and
forge-listed for every corpus card (Forge covers the whole corpus; the farm's
failing overrides are a separate writer and merge on top by the newer
`measured` date, then the writer priority).
"""
import glob
import json
import os
import subprocess

from .norm import name_keys, norm_name
from .schema import repo_root
from .store import StoreError, today

UNFINISHED_WHY = "manifest lists it unfinished"
LACKS_CARD_WHY = "no XMage manifest lists it"


def forge_oracle_ref(makefile):
    """FORGE_ORACLE_REF from the Makefile (deckdb.py:434-437), first 10 hex."""
    with open(makefile, encoding="utf-8") as f:
        for line in f:
            if line.startswith("FORGE_ORACLE_REF"):
                return line.split("=", 1)[1].strip()[:10]
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
        unfinished |= unf
        for c in m.get("cards") or []:
            n = c.get("name") or ""
            for face in [n] + [f.strip() for f in n.split("//")]:
                k = norm_name(face)
                if k and k not in unf:
                    listed.setdefault(k, n)
    return listed, unfinished


def support_rows(cards, listed, unfinished, xmage_pin, forge_pin, measured):
    """One xmage row and one forge row per corpus card (deckdb.py:460-482): a card
    is unfinished when its front face is in a manifest's unfinished list, listed
    when any of its faces is listed, else lacks_card; forge is listed for every
    corpus card."""
    rows = []
    for oid, name in cards:
        keys = name_keys(name)
        if keys[0] in unfinished:
            st, why = "unfinished", UNFINISHED_WHY
        elif any(k in listed for k in keys):
            st, why = "listed", ""
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
    counts = {}
    for r in rows:
        counts[(r["engine"], r["status"])] = counts.get((r["engine"], r["status"]), 0) + 1
    changed = store.put("support", rows, writer=writer, version=version, ran=ran)
    return changed, counts
