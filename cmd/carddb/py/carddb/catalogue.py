"""catalogue: MTGJSON AllPrintings -> cards, printings, sets, legality and faces.

The published catalogue never carries Oracle text.  A card row keeps
`oracle_sha`, the full sha256 (lowercase hex) of the normalised Oracle text:
each face's text with CRLF/CR folded to LF and trailing newlines removed,
faces joined by "\\n//\\n" -- the same face-join shape gorge's
oracletext.Digest uses.  It is not the same value as gorge's digest: that
hashes Text(f) verbatim (only unescaping literal "\\n"), so MTGJSON text with a
trailing newline or CRLF produces a different digest here.
Faces carry only the printed cost, type line and P/T.

`ingest` writes a full snapshot: every catalogue table is replaced by the
rows of the file, so a ruling that makes a format `not_legal` or removes a
printing does not leave a stale row behind.  The cross-check against
oracle/compliance/manifests only reports; it never writes a manifest or uses
one to override MTGJSON.
"""
import datetime
import gzip
import hashlib
import json
import os

from .schema import PUBLIC
from .store import Store, StoreError

CATALOGUE_TABLES = ("cards", "printings", "sets", "legality", "faces")
MANIFEST_DIR = os.path.join("oracle", "compliance", "manifests")
# Manifests spell a split card with its combined name and a transform/MDFC card
# with its front-face name; accept both when matching a set card list.
FRONT_NAME_LAYOUTS = frozenset({"transform", "modal_dfc", "meld", "flip", "adventure"})


class Summary:
    def __init__(self, tables, changed, mismatches, checked, meta):
        self.tables = tables
        self.changed = changed
        self.mismatches = mismatches
        self.checked = checked
        self.meta = meta


def _open_json(path):
    """The file as text; gzip is detected by its magic bytes (AllPrintings.json.gz)."""
    try:
        with open(path, "rb") as f:
            magic = f.read(2)
    except OSError as e:
        raise StoreError("cannot read %s: %s" % (path, e))
    if magic == b"\x1f\x8b":
        return gzip.open(path, "rt", encoding="utf-8")
    return open(path, encoding="utf-8")


def load_allprintings(path):
    """(meta, data) of an MTGJSON AllPrintings file; raises StoreError when it is not one."""
    try:
        with _open_json(path) as f:
            doc = json.load(f)
    except (OSError, ValueError) as e:
        raise StoreError("%s: not an MTGJSON AllPrintings file: %s" % (path, e))
    if not isinstance(doc, dict) or not isinstance(doc.get("data"), dict) or not doc["data"]:
        raise StoreError("%s: not an MTGJSON AllPrintings file: no data object" % path)
    meta = doc.get("meta")
    if not isinstance(meta, dict) or not _date(meta.get("date")) or not meta.get("version"):
        raise StoreError("%s: not an MTGJSON AllPrintings file: meta.date/meta.version missing" % path)
    return meta, doc["data"]


def _date(value):
    """value as a YYYY-MM-DD string, or '' when it is not one."""
    s = str(value or "")
    if len(s) != 10 or s[4] != "-" or s[7] != "-":
        return ""
    try:
        datetime.date.fromisoformat(s)
    except ValueError:
        return ""
    return s


def _str(value):
    return "-" if value is None or value == "" else str(value)


def _set_code(value):
    """A set code in the published spelling: lower-case, '-' when absent."""
    code = _str(value)
    return "-" if code == "-" else code.lower()


def _list(value):
    if not value:
        return []
    return [str(x) for x in value if x is not None and str(x) != ""]


def _join(items):
    items = [str(x) for x in items if x is not None and str(x) != ""]
    return ",".join(items) if items else "-"


def _name_key(name):
    return " ".join(str(name or "").casefold().split())


def _number_key(number):
    """MTGJSON marks an alternate-art printing ★ where the manifests write *."""
    return str(number or "").strip().replace("★", "*")


def _front(card):
    """A card entry that starts a printing: no side, or side a."""
    return card.get("side") in (None, "", "a")


def _meld(card):
    """Meld components (side a) and the meld result (side b) are separate cards, not faces."""
    return card.get("layout") == "meld"


def _starts_card(card):
    """A card entry that is its own catalogue card: a front, or a meld result."""
    return _front(card) or (_meld(card) and card.get("side") == "b")


def _card_name(card):
    """The card's own name: a meld card is named alone, not with its meld partner."""
    if _meld(card):
        return _str(card.get("faceName") or card.get("name"))
    return _str(card.get("name") or card.get("faceName"))


def _group(card, by_uuid):
    """Every face of one printing, front first by side.

    A meld card is a face of nothing: Gisela and Bruna are each one face, and
    Brisela, the meld result, is a card of its own (one oracle_id, no faces)."""
    if _meld(card):
        return [card]
    ids = [card.get("uuid")] + [x for x in (card.get("otherFaceIds") or [])]
    faces, seen = [], set()
    for uuid in ids:
        face = by_uuid.get(uuid)
        if face is not None and uuid not in seen:
            seen.add(uuid)
            faces.append(face)
    if len(faces) < 2:
        return [card]
    faces.sort(key=lambda f: (str(f.get("side") or ""), str(f.get("uuid") or "")))
    return faces


def _face_rows(oid, faces):
    rows = []
    for i, face in enumerate(faces):
        side = str(face.get("side") or chr(ord("a") + i))
        rows.append({
            "oracle_id": oid,
            "face": side,
            "mana_cost": _str(face.get("manaCost")),
            "type_line": _str(face.get("type")),
            "power": _str(face.get("power")),
            "toughness": _str(face.get("toughness")),
        })
    return rows


def normalise_text(faces):
    """The text the digest covers: faces in order, LF line endings, one // join."""
    parts = []
    for face in faces:
        text = str(face.get("text") or "").replace("\r\n", "\n").replace("\r", "\n")
        parts.append(text.rstrip("\n"))
    return "\n//\n".join(parts)


def oracle_sha(faces):
    """Full sha256 (lowercase hex) of a card's normalised Oracle text."""
    return hashlib.sha256(normalise_text(faces).encode("utf-8")).hexdigest()


def _combined(faces, key, unique=True):
    """The values of key across faces, in face order, joined by //."""
    parts = []
    for face in faces:
        value = _str(face.get(key))
        if value != "-" and (not unique or value not in parts):
            parts.append(value)
    return " // ".join(parts) if parts else "-"


def _face_values(faces, key):
    """The union of a list field across faces, in face order."""
    out = []
    for face in faces:
        for value in _list(face.get(key)):
            if value not in out:
                out.append(value)
    return out


def _keywords(faces):
    out = set()
    for face in faces:
        for kw in _list(face.get("keywords")):
            out.add(kw)
    return sorted(out)


def _legalities(card):
    out = []
    legal = card.get("legalities") or {}
    if not isinstance(legal, dict):
        return out
    for fmt in sorted(legal):
        status = str(legal[fmt] or "").strip().lower().replace(" ", "_").replace("-", "_")
        if not status or status == "not_legal":
            continue
        out.append((fmt.lower(), status))
    return out


def build_catalogue(data):
    """{table: [row dicts]} for an AllPrintings `data` object, deterministically ordered."""
    sets, printings, best, first, paper = [], [], {}, {}, {}
    seen_sets = set()
    for key in sorted(data):
        s = data[key]
        if not isinstance(s, dict):
            raise StoreError("MTGJSON set %r is not an object" % key)
        code = _set_code(s.get("code") or key)
        if code in seen_sets:
            raise StoreError("MTGJSON set code %s appears twice" % code)
        seen_sets.add(code)
        release = _date(s.get("releaseDate"))
        if not release:
            raise StoreError("MTGJSON set %s has no valid releaseDate" % key)
        sets.append({
            "set_code": code,
            "name": _str(s.get("name") or code),
            "release_date": release,
            "set_type": _str(s.get("type")),
            "parent_code": _set_code(s.get("parentCode")),
        })
        cards = s.get("cards") or []
        if not isinstance(cards, list):
            raise StoreError("MTGJSON set %s: cards is not a list" % key)
        by_uuid = {c.get("uuid"): c for c in cards if c.get("uuid")}
        for c in cards:
            if not _starts_card(c):
                continue
            oid = (c.get("identifiers") or {}).get("scryfallOracleId")
            uuid = c.get("uuid")
            number = _str(c.get("number"))
            if not oid or not uuid or number == "-":
                raise StoreError("MTGJSON set %s: card %r lacks identifiers.scryfallOracleId, uuid or number" % (
                    key, c.get("name")))
            faces = _group(c, by_uuid)
            row_key = (release, code, number, str(uuid))
            paper_here = any("paper" in (f.get("availability") or []) for f in faces)
            if paper_here and (oid not in first or row_key < first[oid][0]):
                first[oid] = (row_key, release, code)
            paper[oid] = paper.get(oid, False) or paper_here
            rec = {
                "name": _card_name(c),
                "layout": _str(c.get("layout") or "normal"),
                "mana_cost": _combined(faces, "manaCost"),
                "mana_value": int(round(float(c.get("manaValue") or 0))),
                "type_line": _combined(faces, "type", unique=False),
                "colors": _join(_face_values(faces, "colors") if len(faces) > 1 else _list(c.get("colors"))),
                "color_identity": _join(_face_values(faces, "colorIdentity") if len(faces) > 1
                                        else _list(c.get("colorIdentity"))),
                "keywords": _join(_keywords(faces)),
                "oracle_sha": oracle_sha(faces),
                "legalities": _legalities(c),
                "faces": _face_rows(oid, faces) if len(faces) > 1 else [],
            }
            if oid not in best or row_key > best[oid][0]:
                best[oid] = (row_key, rec)
            printings.append({
                "printing_id": str(uuid),
                "oracle_id": oid,
                "set_code": code,
                "collector_number": number,
                "rarity": _str(c.get("rarity")).lower(),
                "released": release,
            })

    cards, faces = [], []
    for oid in sorted(best):
        rec = best[oid][1]
        f = first.get(oid)
        cards.append({
            "oracle_id": oid,
            "name": rec["name"],
            "layout": rec["layout"],
            "mana_cost": rec["mana_cost"],
            "mana_value": rec["mana_value"],
            "type_line": rec["type_line"],
            "colors": rec["colors"],
            "color_identity": rec["color_identity"],
            "keywords": rec["keywords"],
            "oracle_sha": rec["oracle_sha"],
            "first_released": f[1] if f else "-",
            "first_set": f[2] if f else "-",
            "paper": "true" if paper.get(oid) else "false",
        })
        faces.extend(rec["faces"])

    legality = []
    for oid in sorted(best):
        for fmt, status in best[oid][1]["legalities"]:
            legality.append({"oracle_id": oid, "format": fmt, "status": status})

    return {
        "cards": cards,
        "printings": printings,
        "sets": sets,
        "legality": legality,
        "faces": faces,
    }


def _aliases(card):
    """Every name a set manifest may use for one physical printing."""
    names = []
    for n in (card.get("name"), card.get("faceName")):
        if n and n not in names:
            names.append(n)
    return names


def _display(card):
    """The name a set list most likely uses: front face for a DFC, combined for a split."""
    if card.get("layout") in FRONT_NAME_LAYOUTS and card.get("faceName"):
        return card["faceName"]
    return card.get("name") or card.get("faceName")


def _sample(items, n=20):
    items = list(items)
    if len(items) <= n:
        return ", ".join(items)
    return ", ".join(items[:n]) + ", ... %d more" % (len(items) - n)


def cross_check(directory, data):
    """(report lines, manifest count) comparing every manifest's set card list with data.

    Only read: no manifest is written and no MTGJSON row is replaced from one.
    """
    if not directory:
        return [], 0
    if not os.path.isdir(directory):
        raise StoreError("%s: no such manifest directory" % directory)
    sets = {}
    for key in sorted(data):
        s = data[key]
        if isinstance(s, dict):
            sets[_set_code(s.get("code") or key)] = s
    manifests = []
    for fn in sorted(os.listdir(directory)):
        if not fn.endswith(".json"):
            continue
        path = os.path.join(directory, fn)
        try:
            with open(path, encoding="utf-8") as f:
                m = json.load(f)
        except (OSError, ValueError) as e:
            raise StoreError("%s: %s" % (path, e))
        if not isinstance(m, dict) or not m.get("code") or not isinstance(m.get("cards", []), list):
            raise StoreError("%s: not a set manifest (code/cards)" % path)
        manifests.append(m)
    lines = []
    for m in sorted(manifests, key=lambda x: _set_code(x["code"])):
        code = _set_code(m["code"])
        label = code.upper()
        s = sets.get(code)
        if s is None:
            lines.append("manifest %s: set not in MTGJSON" % label)
            continue
        aliases, manifest = {}, {}
        for c in s.get("cards") or []:
            names = _aliases(c)
            number = _number_key(_str(c.get("number")))
            for n in names:
                aliases.setdefault(_name_key(n), set()).add(number)
        for c in m.get("cards") or []:
            if not c.get("name"):
                continue
            manifest[_name_key(c["name"])] = c
        missing = sorted(manifest[k]["name"] for k in set(manifest) - set(aliases))
        # an extra is a physical printing whose card has no alias in the manifest
        extra = []
        for c in s.get("cards") or []:
            if not _starts_card(c):
                continue
            keys = {_name_key(n) for n in _aliases(c) if n}
            if keys and not (keys & set(manifest)):
                extra.append(_display(c) or c.get("name") or "")
        extra = sorted(set(x for x in extra if x))
        number_diff = []
        for k in sorted(set(manifest) & set(aliases)):
            want = set(_number_key(x) for x in (manifest[k].get("numbers") or []))
            have = aliases[k]
            if want != have:
                number_diff.append((manifest[k]["name"], sorted(want), sorted(have)))
        if missing:
            lines.append("manifest %s: %d card(s) in the manifest but not in MTGJSON: %s" % (
                label, len(missing), _sample(missing)))
        if extra:
            lines.append("manifest %s: %d card(s) in MTGJSON but not in the manifest: %s" % (
                label, len(extra), _sample(extra)))
        if number_diff:
            parts = ["%s (manifest %s; MTGJSON %s)" % (n, ",".join(w) or "-", ",".join(h) or "-")
                     for n, w, h in number_diff]
            lines.append("manifest %s: %d card(s) with different collector numbers: %s" % (
                label, len(number_diff), _sample(parts, 10)))
    return lines, len(manifests)


def ingest(path, store=None, manifests_dir=None, writer="mtgjson", version=None):
    """Read an AllPrintings file, replace the catalogue tables, report manifest mismatches."""
    meta, data = load_allprintings(path)
    if manifests_dir:
        mismatches, checked = cross_check(manifests_dir, data)
    else:
        mismatches, checked = [], 0
    tables = build_catalogue(data)
    del data  # the rows are the snapshot; release the 4+ GB AllPrintings document before writing
    store = store or Store(PUBLIC)
    ver = version or str(meta["version"])
    # The snapshot's own date, never the wall clock: the same AllPrintings file must
    # produce byte-identical shards, _meta/writers included, on any run date.
    ran = meta["date"]
    changed = 0
    for table in CATALOGUE_TABLES:
        changed += store.put(table, tables[table], writer=writer, version=ver, ran=ran, replace=True)
    changed += store.put("_meta/sources", [{
        "source": "mtgjson",
        "version": ver,
        "retrieved": meta["date"],
        "licence": "MIT",
    }], writer=writer, version=ver, ran=ran)
    return Summary(tables={k: len(v) for k, v in tables.items()}, changed=changed,
                   mismatches=mismatches, checked=checked, meta=meta)
