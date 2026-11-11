#!/usr/bin/env python3
"""Static support scan: Forge card scripts vs manabrew-engine Rust source.

usage: mb_scan.py <engine_src> <res_dir> <pool: fdn|commons|all> [out.json]
Heuristic flags per card:
  api_unknown     SP$/AB$/DB$ name not in Rust ALL_API_TYPES
  api_unimpl      known ApiType without effect_dispatch arm
  trig_unknown    T: Mode$ not a Rust TriggerType variant
  static_unknown  S: Mode$ not a Rust StaticMode variant (falls to Other)
  repl_unknown    R: Event$ not a Rust ReplacementType variant
  kw_unknown      K: head not a Rust Keyword display name and not a literal anywhere
  key_unref       a Key$ never appears as a string literal / keys.rs const in Rust
  multiface       AlternateMode present (DFC/split/adventure/...)
"""
import json, os, re, sys, collections

src, res, pool = sys.argv[1], sys.argv[2], sys.argv[3]
out = sys.argv[4] if len(sys.argv) > 4 else None

lits = collections.Counter()
files_with = collections.defaultdict(set)
rs_text = {}
extra = os.path.join(os.path.dirname(os.path.dirname(src.rstrip('/'))), 'forge-card-script', 'src')
for root, _, fs in list(os.walk(src)) + list(os.walk(extra)):
    for f in fs:
        if f.endswith('.rs'):
            p = os.path.join(root, f)
            t = open(p, encoding='utf-8', errors='replace').read()
            rs_text[p] = t
            for m in re.finditer(r'"((?:[^"\\]|\\.)*)"', t):
                lits[m.group(1)] += 1
                files_with[m.group(1)].add(os.path.relpath(p, src))

lit_lower = {k.lower() for k in lits}
# lits also include things like "ValidCard$" inside strings; split literal contents into tokens
tok_lower = set()
for k in lits:
    for t in re.split(r'[^A-Za-z0-9_+\-]+', k):
        if t:
            tok_lower.add(t.lower())

api = rs_text[os.path.join(src, 'ability/api_type.rs')]
api_names = {n.lower(): v for n, v in re.findall(r'\("(\w+)",\s*ApiType::(\w+)\)', api)}
er = rs_text[os.path.join(src, 'ability/effects/effect_resolver.rs')]
impl = set(re.findall(r'ApiType::(\w+)\s*=>', er[er.index('effect_dispatch! {'):]))

def enum_variants(path, name):
    t = rs_text[os.path.join(src, path)]
    i = t.index('pub enum ' + name)
    body = t[t.index('{', i) + 1:]
    depth, j = 1, 0
    while depth:
        c = body[j]
        depth += c == '{'
        depth -= c == '}'
        j += 1
    body = body[:j - 1]
    body = re.sub(r'//[^\n]*', '', body)
    body = re.sub(r'#\[[^\]]*\]', '', body)
    names = set()
    for m in re.finditer(r'^\s*(\w+)\s*(?:[,({]|$)', body, re.M):
        names.add(m.group(1).lower())
    # strum serialize aliases
    for m in re.finditer(r'serialize\s*=\s*"(\w+)"', t[i:i + len(body) + 4000]):
        names.add(m.group(1).lower())
    return names

trig = enum_variants('trigger/trigger_type.rs', 'TriggerType')
# trigger_type.rs also has explicit name mapping arms: TriggerType::X => "Name"
trig |= {s.lower() for s in re.findall(r'TriggerType::\w+\s*=>\s*"(\w+)"', rs_text[os.path.join(src, 'trigger/trigger_type.rs')])}
stat = enum_variants('staticability/static_ability.rs', 'StaticMode')
repl = enum_variants('replacement/replacement_type.rs', 'ReplacementType')
kwt = rs_text[os.path.join(src, 'keyword/keyword_instance.rs')]
kw_names = {s.lower() for s in re.findall(r'Keyword::\w+\s*=>\s*"([^"]+)"', kwt)}

keys_rs = rs_text[os.path.join(src, 'parsing/keys.rs')]
key_consts = {v.lower() for v in re.findall(r'pub const \w+: &str = "([^"]+)"', keys_rs)}

# ---- card pool ----
cards_dir = os.path.join(res, 'cardsfolder')
by_name = {}
for root, _, fs in os.walk(cards_dir):
    for f in fs:
        if f.endswith('.txt'):
            p = os.path.join(root, f)
            raw = open(p, encoding='utf-8', errors='replace').read()
            m = re.search(r'^Name:(.*)$', raw, re.M)
            if m:
                by_name.setdefault(m.group(1).strip().lower(), (p, raw))

def edition_cards(path, rarities=None, sections=('cards',)):
    names = []
    sec = None
    for line in open(path, encoding='utf-8', errors='replace'):
        line = line.rstrip('\n')
        if line.startswith('['):
            sec = line.strip('[]').lower()
            continue
        if sec not in sections:
            continue
        m = re.match(r'\s*\S+\s+([CURMSLT])\s+(.+?)(?:\s+@.*)?$', line)
        if not m:
            continue
        if rarities and m.group(1) not in rarities:
            continue
        names.append(m.group(2).strip())
    return names

def edition_meta(path):
    meta = {}
    for line in open(path, encoding='utf-8', errors='replace'):
        if line.startswith('[') and not line.startswith('[metadata'):
            break
        if '=' in line:
            k, v = line.split('=', 1)
            meta[k.strip()] = v.strip()
    return meta

ed_dir = os.path.join(res, 'editions')
if pool == 'fdn':
    names = edition_cards(os.path.join(ed_dir, 'Foundations.txt'), sections=('cards', 'precon product'))
elif pool == 'commons':
    names = []
    for f in os.listdir(ed_dir):
        meta = edition_meta(os.path.join(ed_dir, f))
        if meta.get('Type') in ('Core', 'Expansion', 'Draft', 'Starter'):
            names += edition_cards(os.path.join(ed_dir, f), {'C'})
else:
    names = list(by_name)
pool_names = []
seen = set()
for n in names:
    k = n.lower()
    if k in seen:
        continue
    seen.add(k)
    pool_names.append(n)

def params(line):
    out = []
    for part in line.split('|'):
        part = part.strip()
        m = re.match(r'([A-Za-z0-9]+)\$\s*(.*)', part)
        if m:
            out.append((m.group(1), m.group(2).strip()))
    return out

AI_ONLY = {k.lower() for k in ('AILogic','AITgts','AIPreference','AIPreferenceOverride','Ultimate','Image','Separator','ValidDescription','DiscardValidDesc','SpellDescription','StackDescription','PrecostDesc','CostDesc','Description','TgtPrompt','AILifeThreshold','AIManaPref','AIRepeatCostFill','ChoiceTitle','AnnounceTitle','SelectPrompt','PromptMessage','MayPlayText','AITgtsOnlyBetterThanSelf','IsCurse','AIPhyrexianPayment','AiAttackController','AiLogic')}
flags_total = collections.Counter()
key_unref_counter = collections.Counter()
kw_counter = collections.Counter()
trig_counter = collections.Counter()
static_counter = collections.Counter()
repl_counter = collections.Counter()
api_counter = collections.Counter()
per_card = {}
missing = []
basic = {'plains', 'island', 'swamp', 'mountain', 'forest', 'wastes'}
for n in pool_names:
    ent = by_name.get(n.lower())
    if not ent:
        # split / dfc names "A // B"
        ent = by_name.get(n.split(' // ')[0].lower())
    if not ent:
        missing.append(n)
        continue
    p, raw = ent
    fl = set()
    detail = []
    lines = raw.splitlines()
    svars = {}
    for l in lines:
        if l.startswith('SVar:'):
            k, _, v = l[5:].partition(':')
            svars[k] = v
    if re.search(r'^AlternateMode:', raw, re.M):
        fl.add('multiface')
    ability_lines = []
    for l in lines:
        key, _, val = l.partition(':')
        if key in ('A', 'T', 'S', 'R'):
            ability_lines.append((key, val))
        elif key == 'K':
            head = val.split(':')[0].strip()
            h = head.lower()
            if h not in kw_names and h not in lit_lower and h not in tok_lower:
                fl.add('kw_unknown'); kw_counter[head] += 1; detail.append('K:' + head)
    for k, v in svars.items():
        if re.search(r'(?:^|\|)\s*(?:DB|AB|SP)\$', v) or v.strip().startswith('Mode$') or 'Event$' in v:
            ability_lines.append(('SVar', v))
    for kind, val in ability_lines:
        ps = params(val)
        d = dict(ps)
        for apikey in ('SP', 'AB', 'DB'):
            if apikey in d:
                a = d[apikey].split()[0] if d[apikey] else ''
                if a.lower() not in api_names:
                    fl.add('api_unknown'); api_counter[a] += 1; detail.append('API:' + a)
                elif api_names[a.lower()] not in impl:
                    fl.add('api_unimpl'); api_counter[a] += 1; detail.append('API-unimpl:' + a)
        if kind == 'T' and 'Mode' in d:
            m = d['Mode'].strip().lower()
            if m not in trig:
                fl.add('trig_unknown'); trig_counter[d['Mode']] += 1; detail.append('T:' + d['Mode'])
        if kind == 'S' and 'Mode' in d:
            for m in re.split(r'[ ,]+', d['Mode']):
                if m and m.lower() not in stat:
                    fl.add('static_unknown'); static_counter[m] += 1; detail.append('S:' + m)
        if kind == 'R' and 'Event' in d:
            m = d['Event'].strip().lower()
            if m not in repl:
                fl.add('repl_unknown'); repl_counter[d['Event']] += 1; detail.append('R:' + d['Event'])
        for k, _ in ps:
            kl = k.lower()
            if kl in AI_ONLY:
                continue
            if kl not in key_consts and kl not in lit_lower and (kl + '$') not in lit_lower and kl not in tok_lower:
                fl.add('key_unref'); key_unref_counter[k] += 1; detail.append('key:' + k)
    for f in fl:
        flags_total[f] += 1
    per_card[n] = {'file': os.path.relpath(p, res), 'flags': sorted(fl), 'detail': sorted(set(detail))}

clean = [n for n, v in per_card.items() if not (set(v['flags']) - {'multiface'})]
clean_single = [n for n, v in per_card.items() if not v['flags']]
summary = {
    'pool': pool, 'pool_size': len(pool_names), 'scripts_found': len(per_card), 'missing': missing[:20], 'missing_n': len(missing),
    'flag_counts': dict(flags_total),
    'clean_incl_multiface': len(clean), 'clean_single_face': len(clean_single),
    'top_unref_keys': key_unref_counter.most_common(40),
    'top_unknown_kw': kw_counter.most_common(30),
    'top_unknown_trig': trig_counter.most_common(20),
    'top_unknown_static': static_counter.most_common(20),
    'top_unknown_repl': repl_counter.most_common(20),
    'top_unknown_api': api_counter.most_common(20),
}
print(json.dumps(summary, indent=1))
if out:
    json.dump({'summary': summary, 'cards': per_card}, open(out, 'w'), indent=1)
