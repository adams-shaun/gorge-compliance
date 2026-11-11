#!/usr/bin/env python3
"""Audit gorge Commander deck JSONs against MTGJSON (run with python3 -I).

usage: python3 -I audit.py <outdir> <AtomicCards.json.gz> <DeckList.json> <official-decks-dir> <gorgecheck.json|-> <deck.json>...

Checks per deck: 100 cards incl. commander(s); singleton (basics / "any number" / "up to N" excepted);
commander eligibility (leadershipSkills.commander, legal partner/background/doctor pair); colour identity;
legalities.commander == Legal; exact name resolution (Forge spelling vs unknown); product fidelity against
the MTGJSON official list when the deck's name or notes claim a product; cards not printed in the
product's sets; gorge registry/ValidateCommander/Unsupported findings (from gorgecheck.json, produced by
cmd/deckcheck: go run ./cmd/deckcheck <corpus dir> <deck.json>... > gorgecheck.json). Writes <outdir>/audit.json and <outdir>/REPORT-tables.md.
"""
import collections
import difflib
import gzip
import json
import os
import re
import sys
import unicodedata

BASICS = {"Plains", "Island", "Swamp", "Mountain", "Forest", "Wastes",
          "Snow-Covered Plains", "Snow-Covered Island", "Snow-Covered Swamp",
          "Snow-Covered Mountain", "Snow-Covered Forest", "Snow-Covered Wastes"}
WORDNUM = {"two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10}

# severity tiers: 1 = deck is not a legal Commander deck / wrong card; 2 = product or source fidelity;
# 3 = gorge engine support; 4 = spelling only; 0 = informational
TIER = {
    "not_100": 1, "not_singleton": 1, "commander_not_eligible": 1, "illegal_pair": 1,
    "off_identity": 1, "not_legal": 1, "unknown_card": 1, "commander_not_in_list": 1,
    "gorge_validate": 1, "gorge_not_in_registry": 1,
    "product_added": 2, "product_removed": 2, "product_count": 2, "not_in_product_sets": 2,
    "commander_designation": 2, "refused_replaced_by_basics": 2, "product_list_now_published": 2,
    "gorge_unsupported": 3,
    "spelling": 4,
    "info": 0,
}


def fold(s):
    s = s.replace("’", "'").replace("‘", "'").replace("“", '"').replace("”", '"')
    s = s.replace("—", "-").replace("–", "-")
    s = unicodedata.normalize("NFKD", s)  # also maps an ellipsis character to "..."
    s = "".join(ch for ch in s if not unicodedata.combining(ch))
    s = re.sub(r"\s*\.\s*\.\s*\.", "...", s)  # MTGJSON "With Great Power . . ." vs Forge "With Great Power..."
    return re.sub(r"\s+", " ", s).strip().casefold()


def norm_deckname(s):
    return re.sub(r"[^a-z0-9]", "", fold(s))


class Cards:
    def __init__(self, path):
        self.data = json.load(gzip.open(path))["data"]
        self.face = {}
        self.folded = {}
        for key, faces in self.data.items():
            for f in faces:
                if f.get("faceName"):
                    self.face.setdefault(f["faceName"], key)
            for part in key.split(" // "):
                self.face.setdefault(part, key)
            self.folded.setdefault(fold(key), key)
        for n, key in list(self.face.items()):
            self.folded.setdefault(fold(n), key)
        self.names = list(self.data)

    def resolve(self, name):
        """-> (mtgjson key or None, how) how in exact|face|spelling|unknown."""
        if name in self.data:
            return name, "exact"
        if name in self.face:
            return self.face[name], "face"
        k = self.folded.get(fold(name))
        if k:
            return k, "spelling"
        # "A // B" written with one slash or no spaces
        alt = re.sub(r"\s*/+\s*", " // ", name)
        if alt in self.data:
            return alt, "spelling"
        return None, "unknown"

    def first(self, key):
        return self.data[key][0]

    def ident(self, key):
        return set(self.first(key).get("colorIdentity") or [])

    def text(self, key):
        return "\n".join(f.get("text") or "" for f in self.data[key])

    def keywords(self, key):
        out = set()
        for f in self.data[key]:
            out |= set(f.get("keywords") or [])
        return out

    def max_copies(self, key):
        if key in BASICS:
            return None
        t = self.text(key)
        if re.search(r"A deck can have any number of cards named", t):
            return None
        m = re.search(r"A deck can have up to (\w+) cards named", t)
        if m:
            return WORDNUM.get(m.group(1).lower(), 1)
        return 1

    def is_commander(self, key):
        f = self.first(key)
        return bool((f.get("leadershipSkills") or {}).get("commander"))

    def pair_ok(self, a, b):
        ka, kb = self.keywords(a), self.keywords(b)
        ta, tb = self.text(a), self.text(b)
        fa, fb = self.first(a), self.first(b)
        if "Partner" in ka and "Partner" in kb and "Partner with" not in ka and "Partner with" not in kb:
            return True
        if "Partner with" in ka and "Partner with" in kb and b.split(" // ")[0] in ta and a.split(" // ")[0] in tb:
            return True
        if "Friends forever" in ka and "Friends forever" in kb:
            return True
        for x, y, fx, fy in ((a, b, fa, fb), (b, a, fb, fa)):
            if "Choose a Background" in self.keywords(x) and "Background" in (fy.get("subtypes") or []):
                return True
            if "Doctor's companion" in self.keywords(x) and "Doctor" in (fy.get("subtypes") or []):
                return True
        return False


def load_official(dirpath):
    out = {}
    for fn in sorted(os.listdir(dirpath)):
        if fn.endswith(".json"):
            d = json.load(open(os.path.join(dirpath, fn)))["data"]
            out[fn[:-5]] = d
    return out


def official_counts(d):
    c = collections.Counter()
    for board in ("commander", "mainBoard"):
        for e in d.get(board) or []:
            c[e["name"]] += e.get("count", 1)
    return c


def product_claim(j, official, decklist):
    """the MTGJSON official deck this deck claims to be, or None. Matches the deck name, or a product
    name quoted in the notes ("WotC stock ... precon 'X'", "... precon \"X\"")."""
    byname = {}
    for fn, d in official.items():
        byname.setdefault(norm_deckname(d["name"]), fn)
    n = norm_deckname(j.get("name", ""))
    if n in byname:
        return byname[n], "name"
    notes = j.get("notes") or ""
    for m in re.finditer(r"[\"'“]([^\"'”]{3,60})[\"'”]", notes):
        k = norm_deckname(m.group(1))
        if k in byname and re.search(r"(?i)precon|product|stock", notes):
            return byname[k], "notes"
    return None, None


def audit_deck(path, C, official, gorge):
    j = json.load(open(path))
    probs = []

    def P(kind, card, detail):
        probs.append({"kind": kind, "card": card, "detail": detail})

    cmds = j.get("commanders") or ([j["commander"]] if j.get("commander") else [])
    counts = collections.Counter()
    for e in j["cards"]:
        counts[e["name"]] += e["count"]
    total = sum(counts.values())
    if total != 100:
        P("not_100", "", f"{total} cards including the commander(s)")

    resolved = {}
    for name in counts:
        key, how = C.resolve(name)
        resolved[name] = key
        if how == "unknown":
            close = difflib.get_close_matches(name, C.names, n=2, cutoff=0.85)
            P("unknown_card", name, "no MTGJSON card or face of this name" + (f"; closest: {close}" if close else ""))
        elif how == "spelling":
            P("spelling", name, f"MTGJSON spells it {key!r}")
    for n in cmds:
        if n not in counts:
            P("commander_not_in_list", n, "commander is not one of the deck's card entries")
            key, how = C.resolve(n)
            resolved.setdefault(n, key)

    # commander checks
    ckeys = [resolved.get(n) for n in cmds]
    for n, k in zip(cmds, ckeys):
        if k and not C.is_commander(k):
            P("commander_not_eligible", n, "MTGJSON leadershipSkills.commander is false")
    if len(ckeys) == 2 and all(ckeys) and not C.pair_ok(*ckeys):
        P("illegal_pair", " & ".join(cmds), "not a legal Partner / Partner with / Background / Doctor pair")
    if len(ckeys) > 2:
        P("illegal_pair", " & ".join(cmds), f"{len(ckeys)} commanders")
    ident = set()
    for k in ckeys:
        if k:
            ident |= C.ident(k)

    # a lone commander that wants a partner/background while its natural partner sits in the 99
    if len(ckeys) == 1 and ckeys[0]:
        k0 = ckeys[0]
        for name, k in resolved.items():
            if not k or name in cmds or not C.is_commander(k):
                continue
            if C.pair_ok(k0, k):
                stem = os.path.basename(path)
                slugged = re.sub(r"[^a-z0-9]+", "-", fold(k.split(" // ")[0])).strip("-")
                in_slug = slugged in stem
                P("commander_designation", name,
                  f"{name!r} can pair with commander {cmds[0]!r} and is in the 99" +
                  ("; the deck's source names it as a commander, so the deck file should list both in \"commanders\"" if in_slug else
                   " (may be intended as a 99 card; check the source)"))

    for name, n in counts.items():
        k = resolved[name]
        if not k:
            continue
        mx = C.max_copies(k)
        if mx is not None and n > mx:
            P("not_singleton", name, f"{n} copies (max {mx})")
        ci = C.ident(k)
        if ckeys and all(ckeys) and not ci <= ident:
            P("off_identity", name, f"identity {''.join(sorted(ci))} outside commander's {''.join(sorted(ident)) or 'C'}")
        leg = (C.first(k).get("legalities") or {}).get("commander")
        if leg != "Legal":
            P("not_legal", name, f"MTGJSON legalities.commander = {leg or 'absent (not legal)'}")

    # product fidelity
    notes = j.get("notes") or ""
    fn, how = product_claim(j, official, None)
    if fn:
        od = official[fn]
        oc = official_counts(od)
        mine = collections.Counter()
        for name, n in counts.items():
            mine[resolved[name] or name] += n
        sets = set(od.get("sourceSetCodes") or []) | {od["code"]}
        if re.search(r"(?i)interim|official list unreleased", notes):
            P("product_list_now_published", "", f"notes call the list interim/unreleased, but MTGJSON has the official "
              f"{od['name']} ({od['code']}, released {od['releaseDate']}; {od.get('source', '')})")
        for name in sorted(set(mine) | set(oc)):
            a, b = mine.get(name, 0), oc.get(name, 0)
            if a and not b:
                printed = set(C.first(name).get("printings") or []) if name in C.data else set()
                extra = "" if printed & sets else f"; never printed in {'/'.join(sorted(sets))}"
                P("product_added", name, f"x{a} not in the official {od['name']} list{extra}")
                if not printed & sets:
                    P("not_in_product_sets", name, f"not printed in {'/'.join(sorted(sets))}")
            elif b and not a:
                P("product_removed", name, f"x{b} in the official {od['name']} list, missing here")
            elif a != b:
                P("product_count", name, f"x{a} here, x{b} in the official list")
        ocmd = [e["name"] for e in od.get("commander") or []]
        if ocmd and [resolved.get(n) or n for n in cmds] != ocmd:
            P("product_count", " & ".join(cmds), f"commander differs from the official {ocmd}")
        claim = f"{od['name']} ({od['code']}) by {how}"
    else:
        claim = ""
        m = re.search(r"(?i)foundations", notes + j.get("name", ""))
        if m:
            claim = "Foundations (no MTGJSON list matched)"

    m = re.search(r"kernel-refused copies replaced by basics: ([^;]*)", notes)
    if m:
        P("refused_replaced_by_basics", "", m.group(1).strip())

    g = gorge.get(os.path.abspath(path))
    if g:
        if g.get("parse_err"):
            P("gorge_validate", "", "gorge deck.Parse: " + g["parse_err"])
        for n in g.get("not_in_registry") or []:
            P("gorge_not_in_registry", n, "gorge registry Lookup fails")
        for v in g.get("validate") or []:
            P("gorge_validate", "", v)
        for n, miss in sorted((g.get("unsupported") or {}).items()):
            P("gorge_unsupported", n, "gorge lacks " + ", ".join(miss))
    elif gorge:
        P("info", "", "not run through the gorge checker")

    ok = not any(TIER[p["kind"]] in (1, 2, 3, 4) for p in probs)
    return {"ok": ok, "name": j.get("name"), "commander": " & ".join(cmds), "cards": total,
            "distinct": len(counts), "claim": claim, "problems": probs}


def summarize(r):
    by = collections.OrderedDict()
    for p in sorted(r["problems"], key=lambda p: (TIER[p["kind"]] or 9, p["kind"])):
        by.setdefault(p["kind"], []).append(p)
    parts = []
    for kind, ps in by.items():
        if kind == "info":
            continue
        names = [p["card"] or p["detail"] for p in ps]
        if kind in ("product_list_now_published", "refused_replaced_by_basics", "not_100", "gorge_validate"):
            names = [p["detail"] for p in ps]
        shown = "; ".join(names[:12]) + (f"; +{len(names) - 12} more" if len(names) > 12 else "")
        parts.append(f"**{kind}** ({len(ps)}): {shown}")
    return "<br>".join(parts) if parts else "clean"


def severity(r):
    tiers = [TIER[p["kind"]] for p in r["problems"] if TIER[p["kind"]]]
    return min(tiers) if tiers else 9


def main():
    outdir, atomic, decklist, offdir, gorgef = sys.argv[1:6]
    paths = sys.argv[6:]
    C = Cards(atomic)
    official = load_official(offdir)
    gorge = {}
    if gorgef != "-":
        for g in json.load(open(gorgef)):
            gorge[os.path.abspath(g["path"])] = g
    res = {}
    for arg in paths:
        # "<label>=<file>" reports the deck under <label> (e.g. land:testkit/testutil/decks/x.json for a
        # copy extracted with git show); a bare path is reported as itself.
        label, _, p = arg.rpartition("=") if "=" in arg and not os.path.exists(arg) else ("", "", arg)
        j = json.load(open(p))
        if not (j.get("commander") or j.get("commanders")):
            continue
        res[label or os.path.abspath(p)] = audit_deck(p, C, official, gorge)
    with open(os.path.join(outdir, "audit.json"), "w") as f:
        json.dump(res, f, indent=1, ensure_ascii=False)
    groups = collections.OrderedDict()
    for p, r in res.items():
        groups.setdefault((p.rsplit("/", 1)[0]), []).append((p, r))
    lines = []
    for d, rows in groups.items():
        lines += [f"### `{d}`", "", "| deck | commander | cards (distinct) | product claim | problems |", "|---|---|---|---|---|"]
        for p, r in rows:
            lines.append(f"| {os.path.basename(p)} | {r['commander']} | {r['cards']} ({r['distinct']}) | {r['claim']} | {summarize(r)} |")
        lines.append("")
    lines += ["### Clean decks", ""]
    lines += [f"- `{p}`" for p, r in res.items() if r["ok"]]
    lines += ["", "### Decks with problems, by severity (1 = illegal/off-identity/not-100/unknown; 2 = product/source fidelity; 3 = gorge-unsupported cards; 4 = spelling only)", ""]
    bad = sorted(((severity(r), p, r) for p, r in res.items() if not r["ok"]), key=lambda t: (t[0], -len(t[2]["problems"]), t[1]))
    for s, p, r in bad:
        kinds = collections.Counter(q["kind"] for q in r["problems"] if TIER[q["kind"]])
        lines.append(f"- tier {s}: `{p}` — " + ", ".join(f"{k} {n}" for k, n in sorted(kinds.items(), key=lambda kv: (TIER[kv[0]], kv[0]))))
    with open(os.path.join(outdir, "REPORT-tables.md"), "w") as f:
        f.write("\n".join(lines) + "\n")
    print(f"{len(res)} decks, {sum(r['ok'] for r in res.values())} clean")


if __name__ == "__main__":
    main()
