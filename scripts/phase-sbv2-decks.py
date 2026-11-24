#!/usr/bin/env python3
"""phase-sbv2-decks.py POOL.txt CARD-DATA.json OUT_DIR [N]

Writes N (default 10) gorge deck JSON files, one per colour pair, built from the card names of POOL
(oracle/xengine/pools/fdn-commons.txt): 23 spells drawn from the pair's colours (or colourless) and 17
basic lands, at most 2 copies of a spell. Only card names, colours and types from phase's card data are
read; no Oracle text is copied. Deterministic: the draw order is a sha256 of the card name and the deck
index, no clock or ambient randomness. The decks are the catalog `phase-sbv2.sh -decks OUT_DIR` serves.
"""
import hashlib, itertools, json, os, sys

pool, data, out = sys.argv[1:4]
n = int(sys.argv[4]) if len(sys.argv) > 4 else 10
names = []
for line in open(pool, encoding="utf-8"):
    line = line.split("#", 1)[0].strip()
    if line:
        names.append(line)
cards = json.load(open(data, encoding="utf-8"))
colour = {"White": "W", "Blue": "U", "Black": "B", "Red": "R", "Green": "G"}
land = {"W": "Plains", "U": "Island", "B": "Swamp", "R": "Mountain", "G": "Forest"}
spells = {}  # name -> set of colours
for nm in names:
    c = cards.get(nm.lower())
    if c is None or "Land" in c["card_type"]["core_types"]:
        continue
    shards = c["mana_cost"].get("shards", []) if isinstance(c["mana_cost"], dict) else []
    cols = set()
    for s in shards:
        for part in str(s).replace("/", " ").replace("{", " ").replace("}", " ").split():
            if part in colour:
                cols.add(colour[part])
            elif part in "WUBRG" and len(part) == 1:
                cols.add(part)
    spells[nm] = cols
pairs = list(itertools.combinations("WUBRG", 2))
os.makedirs(out, exist_ok=True)
for i in range(n):
    pair = pairs[i % len(pairs)]
    ok = sorted((nm for nm, cols in spells.items() if cols <= set(pair)),
                key=lambda nm: hashlib.sha256(f"{i}:{nm}".encode()).hexdigest())
    chosen, count = [], 0
    for nm in itertools.chain(ok, ok):  # a second pass takes the second copy
        if count == 23:
            break
        have = sum(1 for x in chosen if x == nm)
        if have < 2:
            chosen.append(nm)
            count += 1
    assert count == 23, (pair, count)
    deck = {}
    for nm in chosen:
        deck[nm] = deck.get(nm, 0) + 1
    colours_used = [c for c in pair if any(c in spells[nm] for nm in deck)] or list(pair)
    for j in range(17):
        b = land[colours_used[j % len(colours_used)]]
        deck[b] = deck.get(b, 0) + 1
    name = "FDNC%02d-%s" % (i + 1, "".join(pair))
    doc = {"name": name, "format": "limited", "archetype": "".join(pair),
           "notes": "built by scripts/phase-sbv2-decks.py from oracle/xengine/pools/fdn-commons.txt",
           "cards": [{"name": k, "count": v} for k, v in sorted(deck.items())]}
    with open(os.path.join(out, name.lower() + ".json"), "w", encoding="utf-8") as f:
        json.dump(doc, f, indent=1, sort_keys=True)
        f.write("\n")
print("wrote %d decks to %s" % (n, out))
