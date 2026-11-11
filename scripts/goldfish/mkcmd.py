"""mkcmd.py OUT: put each Commander archetype's commander(s) under a Commander header.

Rewrites every OUT/txt/commander-<slug>.txt that fetch.sh wrote. MTGGoldfish's Commander lists carry
the commander inside the main list, so the commander is taken from the archetype slug: a card whose
slugged name (front face of a "//" card) is the slug, starts it or ends it, at most two (a partner
pair). Prints each archetype and how many commanders it found; 0 means the slug matched no card and
the list needs a hand edit before cmd/deckimport accepts it as Commander.
"""
import re, sys, os


def slug(s):
    return re.sub(r'[^a-z0-9]+', '-', s.lower()).strip('-')


d = sys.argv[1]
for f in sorted(os.listdir(d + '/txt')):
    if not (f.startswith('commander-') and f.endswith('.txt')):
        continue
    a = f[:-4]
    rest = a[len('commander-'):]
    lines = [l for l in open(d + '/txt/' + f).read().splitlines() if l.strip() and l.strip() != 'Commander']
    cmd = []
    main = []
    for l in lines:
        m = re.match(r'(\d+)\s+(.*)', l)
        n = m.group(2) if m else l
        s = slug(n.split(' // ')[0])
        if s and (rest == s or rest.startswith(s + '-') or rest.endswith('-' + s)) and len(cmd) < 2:
            cmd.append(l)
        else:
            main.append(l)
    open(d + '/txt/' + f, 'w').write('\n'.join(main) + '\n\nCommander\n' + '\n'.join(cmd) + '\n')
    print(a, len(cmd))
