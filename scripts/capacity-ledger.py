#!/usr/bin/env python3
"""capacity-ledger.py MEASURE_DIR SHA POLICY [LEDGER]: append kfuzz's capacity stalls per table to a TSV.

MEASURE_DIR holds one kfuzz lockstep output per pool, surface and mulligan setting, named
<pool>.<surface>.m<mulligans>.out (std-fra.autopay.m1.out). From each file the last summary line,
  std-fra: 1500 games (...), ... 4 capacity [dynmana=4], 0 hang (...)
gives one row per table named in the brackets. LEDGER is the TSV to append to (default
$CAPACITY_LEDGER); it is created with a header row. docs/capacity.md has the columns.
"""
import glob, os, re, sys

if len(sys.argv) < 4:
    sys.exit(__doc__)
d, sha, pol = sys.argv[1:4]
out = sys.argv[4] if len(sys.argv) > 4 else os.environ.get('CAPACITY_LEDGER', '')
if not out:
    sys.exit('capacity-ledger.py: no ledger path (4th argument or CAPACITY_LEDGER)')
new = not os.path.exists(out)
with open(out, 'a') as w:
    if new:
        w.write('sha\tpolicy\tpool\tsurface\tmull\tgames\ttable\tstalls\n')
    for f in sorted(glob.glob(d + '/*.out')):
        m = re.match(r'(.+)\.(\w+)\.m?(\d)\.out', os.path.basename(f))
        lines = [l for l in open(f, errors='replace') if ' capacity [' in l and 'games (' in l]
        if not m or not lines:
            continue
        l = lines[-1]
        g = int(re.search(r'(\d+) games', l).group(1))
        for k, v in re.findall(r'([\w ]+)=(\d+)', re.search(r'capacity \[([^\]]*)\]', l).group(1)):
            w.write(f'{sha}\t{pol}\t{m[1]}\t{m[2]}\t{m[3]}\t{g}\t{k.strip()}\t{v}\n')
