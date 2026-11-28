"""The docs site's per-set status, carried as cert_sets columns instead of the docs-data branch.

`oraclediff status -all -json -level A|B` (one adopt.SetStatus JSON object per line) used to be
force-pushed with a meta.json to an orphan docs-data branch that gorge's docs build fetched.
`ingest status` folds the same measurement into cert_sets (the `a_*`, `b_*`, `printed` and
`status_*` columns), and `export` turns a built carddb.sqlite back into the status-A.jsonl,
status-B.jsonl and meta.json shapes scripts/docs_sets.py reads, so the data rides the data branch and
the `carddb-latest` release asset.

A set the certification job has not written gets a minimal `queued` row (the required columns the
status can fill); a set it has written keeps every certification column and gains only the status ones.
Both directions are deterministic: rows in set-code order, buckets in key order, no wall clock.
"""
import json
import os
import re
import sqlite3

from .query import STATUS_AGREE, _status_value_column
from .store import StoreError, today

NO_PRINTED = "no printed list"
NOT_IN_CORPUS = "not in corpus"
UNSUPPORTED = "unsupported"

# SetStatus JSON field -> cert_sets column suffix, per level.
_A_INTS = (("cards", "a_cards"), ("outstanding", "a_outstanding"), ("problem_cards", "a_problem_cards"),
           ("xmage_verified", "a_xmage_verified"), ("forge_only", "a_forge_only"), ("hand", "a_hand"),
           ("non_tournament", "a_non_tournament"))
_B_INTS = (("cards", "b_cards"), ("outstanding", "b_outstanding"), ("problem_cards", "b_problem_cards"),
           ("non_tournament", "b_non_tournament"))

_BUCKET_KEY = re.compile(r"^[^;=\t\n]+$")
_COMMIT = re.compile(r"^[0-9a-f]{4,40}$")
_REF = re.compile(r"^[0-9a-fA-F]{7,40}$")


def load_status(path):
    """{set code: SetStatus dict} of an `oraclediff status -json` file; a line that is not a JSON
    object with a set and an integer card count is skipped (the tool prints progress beside them)."""
    out = {}
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line.startswith("{"):
                    continue
                try:
                    r = json.loads(line)
                except ValueError:
                    continue
                if isinstance(r, dict) and r.get("set") and isinstance(r.get("cards"), int):
                    out[str(r["set"]).lower()] = r
    except OSError as e:
        raise StoreError("docs-status: %s" % e)
    return out


def load_meta(path):
    try:
        with open(path, encoding="utf-8") as f:
            m = json.load(f)
    except (OSError, ValueError) as e:
        raise StoreError("docs-status: %s: %s" % (path, e))
    if not isinstance(m, dict):
        raise StoreError("docs-status: %s: not a JSON object" % path)
    for k in ("commit", "gorge_pin", "forge_ref", "date"):
        if not isinstance(m.get(k), str) or not m[k]:
            raise StoreError("docs-status: %s: need a %r string" % (path, k))
    if not _COMMIT.match(m["commit"]) or not _COMMIT.match(m["gorge_pin"]) or not _REF.match(m["forge_ref"]):
        raise StoreError("docs-status: %s: commit, gorge_pin and forge_ref must be hex" % path)
    if not isinstance(m.get("ratchet_failures", 0), int) or m.get("ratchet_failures", 0) < 0:
        raise StoreError("docs-status: %s: ratchet_failures must be a non-negative integer" % path)
    return m


def encode_buckets(b):
    """`name=n;` entries in name order; None for no buckets."""
    if not b:
        return None
    for k in b:
        if not _BUCKET_KEY.match(k):
            raise StoreError("docs-status: bucket name %r has a reserved character" % k)
    return ";".join("%s=%d" % (k, b[k]) for k in sorted(b)) + ";"


def decode_buckets(text):
    out = {}
    for ent in (text or "").split(";"):
        if ent and ent != "-":
            k, _, v = ent.rpartition("=")
            out[k] = int(v)
    return out


def status_rows(a, b, meta, existing=None):
    """The cert_sets rows (dicts) folding Level A (and B, for the sets it covers) into the table.

    existing: {set code: row dict as Store.rows gives it ('-' for empty)}; a set already there keeps its
    certification columns."""
    existing = existing or {}
    rows = []
    for code in sorted(a):
        ra, rb = a[code], b.get(code)
        base = existing.get(code)
        row = {k: (None if v == "-" else v) for k, v in base.items()} if base else {}
        row["set_code"] = code
        row["printed"] = bool(ra.get("printed"))
        row["a_declared"] = ra.get("declared") or None
        for src, col in _A_INTS:
            row[col] = ra.get(src, 0)
        row["a_buckets"] = encode_buckets(ra.get("buckets"))
        row["b_declared"] = (rb or {}).get("declared") or None
        for src, col in _B_INTS:
            row[col] = rb.get(src, 0) if rb else None
        row["b_buckets"] = encode_buckets((rb or {}).get("buckets"))
        row["status_date"] = meta["date"]
        row["status_commit"] = meta["commit"]
        row["status_gorge"] = meta["gorge_pin"]
        row["status_forge_ref"] = meta["forge_ref"]
        row["ratchet_failures"] = int(meta.get("ratchet_failures", 0))
        if ra.get("released"):
            row["released"] = row.get("released") or ra["released"]
        if ra.get("set_type"):
            row["set_type"] = row.get("set_type") or ra["set_type"]
        if not base:
            bk = ra.get("buckets") or {}
            row["gorge_sha"] = meta["gorge_pin"][:12]
            row["corpus_pin"] = meta["forge_ref"][:10]
            row["paper"] = ra["cards"]
            row["playable"] = max(0, ra["cards"] - int(bk.get(UNSUPPORTED, 0)) - int(bk.get(NOT_IN_CORPUS, 0)))
            row["state"] = "queued"
            row["measured"] = meta["date"]
        row["writer"] = None  # put stamps the run's --source
        rows.append(row)
    return rows


# Every status column the fold writes: cleared on a set that has dropped out of the measurement.
STATUS_COLUMNS = (("printed", "a_declared", "a_buckets", "b_declared", "b_buckets", "status_date", "status_commit",
                   "status_gorge", "status_forge_ref", "ratchet_failures")
                  + tuple(c for _, c in _A_INTS) + tuple(c for _, c in _B_INTS))
# Certification columns a status-only `queued` row leaves empty; a row with any of them is the cert job's.
_CERT_ONLY = ("new_paper", "reprints", "excluded", "c0", "c1", "c2", "c3", "open", "level", "cursor", "entered",
              "certified")


def uncovered_rows(existing, folded):
    """The existing rows of sets outside the measurement, as dicts, to pass back with replace: a row with
    status columns has them cleared (so the export is a snapshot of the newest measurement, as docs-data
    was), and a `queued` row only the status writer made is left out, which deletes it."""
    keep = []
    for code in sorted(set(existing) - set(folded)):
        row = {k: (None if v == "-" else v) for k, v in existing[code].items()}
        if row.get("status_date") is not None:
            if row.get("state") == "queued" and all(row.get(c) is None for c in _CERT_ONLY):
                continue
            for c in STATUS_COLUMNS:
                row[c] = None
        keep.append(row)
    return keep


def run(store, status_a, status_b, meta_path, writer="ci", version=None, ran=None):
    """Fold the status files into cert_sets: a set the measurement covers gains the status columns, a set it
    no longer covers loses them. -> (rows changed, sets folded)."""
    a = load_status(status_a)
    if not a:
        raise StoreError("docs-status: %s holds no Level A rows" % status_a)
    b = load_status(status_b) if status_b else {}
    meta = load_meta(meta_path)
    existing = {r["set_code"]: r for r in store.rows("cert_sets")}
    rows = status_rows(a, b, meta, existing)
    others = uncovered_rows(existing, set(a))
    # replace: a status-only row of a dropped set goes; every other row is passed back as it was read
    changed = store.put("cert_sets", rows + others, writer=writer, version=version, ran=ran or today(), replace=True)
    return changed, len(rows)


def _int(v):
    return 0 if v is None else int(v)


def export(db_path, out_dir):
    """Write status-A.jsonl, status-B.jsonl and meta.json (the docs-data shapes) from a built SQLite.
    -> (Level A sets, Level B sets). Only sets with status columns are exported."""
    con = sqlite3.connect("file:%s?mode=ro" % db_path, uri=True)
    con.row_factory = sqlite3.Row
    try:
        sets = {r["set_code"]: r["name"] for r in con.execute("SELECT set_code, name FROM sets")}
        rows = con.execute("SELECT * FROM cert_sets WHERE status_date IS NOT NULL ORDER BY set_code").fetchall()
    finally:
        con.close()
    if not rows:
        raise StoreError("docs-status: %s has no cert_sets rows with status columns" % db_path)
    os.makedirs(out_dir, exist_ok=True)
    la, lb = [], []
    for r in rows:
        code = r["set_code"]
        base = {"set": code, "name": sets.get(code, code), "set_type": r["set_type"] or "",
                "released": r["released"] or ""}
        a = dict(base, level="A", printed=bool(r["printed"]), cards=_int(r["a_cards"]),
                 outstanding=_int(r["a_outstanding"]))
        if r["a_declared"]:
            a["declared"] = r["a_declared"]
        for src, col in _A_INTS[3:]:
            if r[col]:
                a[src] = r[col]
        if r["a_problem_cards"]:
            a["problem_cards"] = r["a_problem_cards"]
        if r["a_buckets"]:
            a["buckets"] = decode_buckets(r["a_buckets"])
        la.append(a)
        if r["b_cards"] is not None:
            bb = dict(base, level="B", printed=bool(r["printed"]), cards=_int(r["b_cards"]),
                      outstanding=_int(r["b_outstanding"]))
            if r["b_declared"]:
                bb["declared"] = r["b_declared"]
            if r["b_problem_cards"]:
                bb["problem_cards"] = r["b_problem_cards"]
            if r["b_non_tournament"]:
                bb["non_tournament"] = r["b_non_tournament"]
            if r["b_buckets"]:
                bb["buckets"] = decode_buckets(r["b_buckets"])
            lb.append(bb)
    first = max(reversed(rows), key=lambda r: r["status_date"])  # the newest measurement; the first set on a tie
    meta = {"commit": first["status_commit"], "gorge_pin": first["status_gorge"],
            "forge_ref": first["status_forge_ref"], "date": first["status_date"],
            "ratchet_failures": _int(first["ratchet_failures"])}
    for name, items in (("status-A.jsonl", la), ("status-B.jsonl", lb)):
        with open(os.path.join(out_dir, name), "w", encoding="utf-8", newline="\n") as f:
            for it in items:
                f.write(json.dumps(it, sort_keys=True, separators=(",", ":")) + "\n")
    with open(os.path.join(out_dir, "meta.json"), "w", encoding="utf-8", newline="\n") as f:
        json.dump(meta, f, sort_keys=True)
        f.write("\n")
    return len(la), len(lb)


# ------------------------------------------------------------------ per-set xproof snippet

XPROOF_STATES = ("agreed", "owed", "diagnosed", "diverged", "excluded")


def _has_table(con, name):
    return con.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (name,)).fetchone() is not None


def sets_data(db_path):
    """-> ({set_code: {"xproof": {state: abilities}, ["proven_cards": n]}}, names, writers).

    xproof counts keyproof rows (abilities) of the cards printed in the set, per state; a card printed in
    several sets counts in each. A set with no keyproof row is ABSENT (never an all-zero entry); a state
    that is not one of XPROOF_STATES is not counted. `proven_cards` (distinct cards of the set with a status row whose
    verdict is in query.STATUS_AGREE, any level; NOT query's proven_keys sense) is present only when the status table has rows, else the key is absent.
    `dirac_supported` is absent until a dirac per-set writer exists. writers: [{writer, table, ran}]."""
    con = sqlite3.connect("file:%s?mode=ro" % db_path, uri=True)
    con.row_factory = sqlite3.Row
    try:
        names = {r["set_code"]: r["name"] for r in con.execute("SELECT set_code, name FROM sets")}
        out = {}
        if _has_table(con, "keyproof") and _has_table(con, "printings"):
            if not con.execute("SELECT 1 FROM sqlite_master WHERE type='view' AND name='keyproof_keys'").fetchone():
                raise StoreError("docs-sets: %s predates the keyproof leg rows (no keyproof_keys view); "
                                 "rebuild it with carddb build" % db_path)
            # keyproof_keys: one row per key, not per leg, so the counts are abilities as before.
            # one row per (set, card, key): a card printed twice in a set must not count its keys twice
            for r in con.execute(
                    "SELECT s.set_code AS set_code, k.state AS state, COUNT(*) AS n FROM keyproof_keys k "
                    "JOIN (SELECT DISTINCT set_code, oracle_id FROM printings) s ON s.oracle_id=k.oracle_id "
                    "GROUP BY s.set_code, k.state ORDER BY s.set_code, k.state"):
                if r["state"] not in XPROOF_STATES:
                    continue
                ent = out.setdefault(r["set_code"], {"xproof": {s: 0 for s in XPROOF_STATES}})
                ent["xproof"][r["state"]] = r["n"]
            if _has_table(con, "status") and con.execute("SELECT 1 FROM status LIMIT 1").fetchone():
                col = _status_value_column(con)
                proven = {}
                if col:
                    for r in con.execute(
                            "SELECT p.set_code AS set_code, COUNT(DISTINCT p.oracle_id) AS n FROM printings p "
                            "JOIN status t ON t.oracle_id=p.oracle_id WHERE lower(t.%s) IN (%s) "
                            "GROUP BY p.set_code" % (col, ",".join("?" * len(STATUS_AGREE))), STATUS_AGREE):
                        proven[r["set_code"]] = r["n"]
                for code, ent in out.items():
                    ent["proven_cards"] = proven.get(code, 0)
        writers = []
        if _has_table(con, "_meta_writers"):
            writers = [{"writer": r["writer"], "table": r["table"], "ran": r["ran"]} for r in con.execute(
                'SELECT writer, "table", ran FROM _meta_writers ORDER BY writer, "table"')]
    finally:
        con.close()
    return out, names, writers


def _cell(text):
    return str(text).replace("|", "\\|").replace("\n", " ")


def _section(name, lines):
    return ["<!-- --8<-- [start:%s] -->" % name] + lines + ["<!-- --8<-- [end:%s] -->" % name]


def render_sets_md(data, names, writers):
    """The finished markdown gorge includes unchanged: three snippet sections, headline, sets, freshness."""
    codes = sorted(data)
    totals = {s: sum(data[c]["xproof"][s] for c in codes) for s in XPROOF_STATES}
    have_proven = any("proven_cards" in data[c] for c in codes)
    head = ["| xproof abilities, summed over sets | Count |", "| --- | ---: |"]
    head.append("| Sets with xproof rows | %d |" % len(codes))
    for s in XPROOF_STATES:
        head.append("| %s | %d |" % (s.capitalize(), totals[s]))
    if have_proven:
        head.append("| Proven cards | %d |" % sum(data[c].get("proven_cards", 0) for c in codes))
    cols = ["Set", "Name", "Agreed", "Owed", "Diverged"] + (["Proven"] if have_proven else [])
    body = ["| " + " | ".join(cols) + " |", "| --- | --- | ---: | ---: | ---: |" + (" ---: |" if have_proven else "")]
    for c in codes:
        x = data[c]["xproof"]
        row = ["`%s`" % c, _cell(names.get(c, c)), str(x["agreed"]), str(x["owed"]), str(x["diverged"])]
        if have_proven:
            row.append(str(data[c].get("proven_cards", 0)))
        body.append("| " + " | ".join(row) + " |")
    if not codes:
        body = ["No set has xproof rows yet."]
    elif have_proven:
        body += ["", "*Proven: cards of the set with an agreeing status verdict (not every key proven).*"]
    if writers:
        fresh = ["*Data written by: " + "; ".join("%s (%s) %s" % (_cell(w["writer"]), _cell(w["table"]), w["ran"])
                                                  for w in writers) + ".*"]
    else:
        fresh = ["*No writer freshness recorded.*"]
    lines = (["<!-- carddb docs-sets: generated, do not edit -->", ""] + _section("headline", head) + [""] +
             _section("sets", body) + [""] + _section("freshness", fresh))
    return "\n".join(lines) + "\n"


def export_sets(db_path, out_dir):
    """Write sets.json, writers.json and sets-carddb.md from a built SQLite (docs-data shapes, all
    deterministic: sorted keys, no wall clock). -> number of sets written."""
    data, names, writers = sets_data(db_path)
    os.makedirs(out_dir, exist_ok=True)
    texts = {"sets.json": json.dumps(data, sort_keys=True, indent=1) + "\n",
             "writers.json": json.dumps(writers, sort_keys=True, indent=1) + "\n",
             "sets-carddb.md": render_sets_md(data, names, writers)}
    for name, text in texts.items():
        with open(os.path.join(out_dir, name), "w", encoding="utf-8", newline="\n") as f:
            f.write(text)
    return len(data)
