"""The scenario evidence writer: the committed scenario corpora -> evidence rows.

Nothing is run: CI runs both corpora (package audit) against ledgers that can only be
true -- a listed scenario that stops failing fails the build, like an unlisted one that
fails -- so each outcome is read off the committed files.

  scenarios/xmageport/<family>/<Class>.json   source `xmageport`, ref the scenario's
      `xmage` value (Class#method), one row per card a step of the scenario names in
      `card` (a card only the setup places gets none)
  scenarios/oracle/<family>/<slug>.json       source `hand`, ref
      <family>/<slug>/<scenario name>, one row for the file's `card`

Outcome, first match:
  known_bug    the scenario's `xmage` value is a row of cmd/xmageport/known_bugs.tsv
  diverge      a hand scenario listed in <family>/known-divergent/ (CI requires it to fail)
  unconsumed   a hand scenario listed in <family>/known-unconsumed/ (CI requires a step
               answer no decision matched)
  pass         every other scenario
Only known_bug blocks card_proven.proven.

Files are globbed exactly as the audit's loaders glob them (<family>/*.json), so a row
never stands for a file CI does not run. Names resolve with norm.Resolver (norm_name,
the front face); a name no cards row answers to is counted and skipped. Rows carry ids
and outcomes only: no Oracle text, no test source, no ratchet reason.

Evidence rows of these two sources that a rerun no longer derives are deleted; every
other source's rows are left alone. For a reproducible _meta/writers date the caller
passes `measured` or sets CARDDB_NOW (see compliance.py).
"""
import glob
import json
import os
import re

from .dirac import _replace_mine
from .norm import Resolver
from .schema import repo_root
from .store import StoreError

WRITER = "scenario"
PORT_SOURCE = "xmageport"
HAND_SOURCE = "hand"
SOURCES = (PORT_SOURCE, HAND_SOURCE)
PORT_REL = os.path.join("scenarios", "xmageport")
HAND_REL = os.path.join("scenarios", "oracle")
KNOWN_BUGS_REL = os.path.join("cmd", "xmageport", "known_bugs.tsv")
# the hand corpus's two ratchet directories and the outcome each one's rows carry, first match
HAND_RATCHETS = (("known-divergent", "diverge"), ("known-unconsumed", "unconsumed"))

_SEAT = re.compile(r"^p[0-9]+:")
_ORDINAL = re.compile(r"#[0-9]+$")


def _load_json(path):
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError) as e:
        raise StoreError("scenarios: %s: %s" % (path, e))


def _files(repo, rel):
    """The corpus's scenario files. None at all is an error: deriving nothing from a wrong
    --repo would delete every row of the source."""
    root = os.path.join(repo, rel)
    out = sorted(glob.glob(os.path.join(glob.escape(root), "*", "*.json")))
    if not out:
        raise StoreError("scenarios: %s holds no scenario files" % root)
    return out


def known_bugs(repo):
    """The Class#method ids of known_bugs.tsv (the first field of each non-comment line)."""
    path = os.path.join(repo, KNOWN_BUGS_REL)
    try:
        with open(path, encoding="utf-8") as f:
            lines = f.read().split("\n")
    except OSError as e:
        raise StoreError("scenarios: %s: %s" % (path, e))
    return {l.split("\t", 1)[0] for l in lines if l and not l.startswith("#")}


def card_of(ref):
    """The card a step's `card` ref names, else None. The spellings are gorge's runner's:
    pN:Name, pN:Name#k (the k-th object of the name), pN:ability:Name#k (a pending ability,
    named by its source card) and pN:token:Name#k (a token, which is no card)."""
    name = _SEAT.sub("", ref, count=1)
    if name.startswith("token:"):
        return None
    if name.startswith("ability:"):
        name = name[len("ability:"):]
    return _ORDINAL.sub("", name) or None


def port_rows(repo):
    """[{card, source, ref, outcome}] over scenarios/xmageport, `card` still a name."""
    bugs = known_bugs(repo)
    out = []
    for fn in _files(repo, PORT_REL):
        try:
            for s in _load_json(fn)["scenarios"]:
                ref = s["xmage"]
                outcome = "known_bug" if ref in bugs else "pass"
                cards = {card_of(st["card"]) for st in s.get("steps") or () if st.get("card")}
                out += [{"card": c, "source": PORT_SOURCE, "ref": ref, "outcome": outcome}
                        for c in sorted(cards - {None})]
        except (AttributeError, KeyError, TypeError) as e:
            raise StoreError("scenarios: %s: not a translated scenario file (%r)" % (fn, e))
    return out


def _ratcheted(repo, kind):
    """{(family, card, scenario name)} over scenarios/oracle/<family>/<kind>/*.json."""
    out = set()
    pattern = os.path.join(glob.escape(os.path.join(repo, HAND_REL)), "*", kind, "*.json")
    for fn in sorted(glob.glob(pattern)):
        family = os.path.basename(os.path.dirname(os.path.dirname(fn)))
        d = _load_json(fn)
        try:
            out.update((family, d["card"], name) for name in d["rows"])
        except (KeyError, TypeError) as e:
            raise StoreError("scenarios: %s: not a ratchet file (%r)" % (fn, e))
    return out


def hand_rows(repo):
    """[{card, source, ref, outcome}] over scenarios/oracle, `card` still a name."""
    bugs = known_bugs(repo)
    ratchets = [(_ratcheted(repo, kind), outcome) for kind, outcome in HAND_RATCHETS]
    out = []
    for fn in _files(repo, HAND_REL):
        d = _load_json(fn)
        stem = os.path.splitext(os.path.basename(fn))[0]
        try:
            card, family = d["card"], d["family"]
            for s in d["scenarios"]:
                if s.get("xmage") in bugs:
                    outcome = "known_bug"
                else:
                    key = (family, card, s["name"])
                    outcome = next((o for rows, o in ratchets if key in rows), "pass")
                out.append({"card": card, "source": HAND_SOURCE,
                            "ref": "%s/%s/%s" % (family, stem, s["name"]), "outcome": outcome})
        except (AttributeError, KeyError, TypeError) as e:
            raise StoreError("scenarios: %s: not a hand scenario file (%r)" % (fn, e))
    return out


def run(store, repo=None, measured=None, writer=WRITER, version=None):
    """Derive both corpora's evidence rows, put them and delete the stale ones.
    -> {"changed": rows changed, "outcomes": {(source, outcome): rows}, "unresolved": [names]}."""
    repo = repo or repo_root()
    named = port_rows(repo) + hand_rows(repo)
    cards = store.cards()
    if not cards:
        raise StoreError("scenarios: the clone holds no cards rows: run `carddb ingest catalogue` first")
    resolver = Resolver(cards)
    rows = {}
    for r in named:
        oid = resolver.resolve(r["card"])
        if r["card"] in resolver.misses:
            continue
        rows[(oid, r["source"], r["ref"])] = r["outcome"]
    ev = [{"oracle_id": oid, "source": source, "ref": ref, "outcome": outcome}
          for (oid, source, ref), outcome in sorted(rows.items())]
    counts = {}
    for r in ev:
        k = (r["source"], r["outcome"])
        counts[k] = counts.get(k, 0) + 1
    changed = _replace_mine(store, "evidence", ev, lambda r: r["source"] in SOURCES, writer, version, measured)
    return {"changed": changed, "outcomes": counts, "unresolved": sorted(resolver.misses)}
