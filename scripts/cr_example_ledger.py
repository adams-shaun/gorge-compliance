#!/usr/bin/env python3
"""Diff LEDGER.tsv against the worked examples of the pinned rules text.

Usage: cr_example_ledger.py <rules-text> <ledger>

Reads the rules text the way gorge's rules-index script does: "\\r" and form
feeds are dropped, the body lies between the second "1. Game Concepts" line
and the second "Glossary" line, and a line starting "Example:" belongs to the
last rule or subrule heading before it. A wrapped line that merely begins
with a rule id (a cross-reference such as "601.2b and 601.2f-h.") is not a
heading: the text moves forward in rule order and a heading starts a
paragraph. Prints the (rule, n) pairs the ledger
lacks or has in excess; writes nothing. The rules text is not in this
repository and is never committed; the script is run by hand when the pinned
text changes. Expected output for the 2026-08-07 text, against an empty
ledger: 277 pairs on 215 rules.

A row the ledger lacks prints as

    +\\t<rule>\\t<n>\\t<suggested-status>

where the suggested status is "out-of-scope" when the example's rule is
outside four-player Commander (the scope list below; it repeats gorge's
rules-index scope list, so when that list changes there, change it here), and
"open" otherwise. A ledger row the text no longer has prints as

    -\\t<rule>\\t<n>\\t<ledger-status>

Exit status is 0 when the ledger and the text agree, 1 when they do not.
"""

import re
import sys

# Examples under these top-level sections are outside four-player Commander:
# draft, two-headed giant, Vanguard, Planechase, Conspiracy, Commander variants
# and the like. Repeats gorge's rules-index scope list; change both together.
OUT_OF_SCOPE_SECTIONS = frozenset(
    {123, 311, 312, 313, 314, 315, 407, 717, 729, 801, 803, 804, 805, 807,
     808, 809, 810, 811, 900, 901, 902, 904, 905}
)

# Individual rules outside four-player Commander (draft and vote mana, the
# "vote" keyword and "helm of the host"-style keyword variants). Repeats
# gorge's rules-index scope list; change both together.
OUT_OF_SCOPE_RULES = frozenset(
    {"701.31", "701.32", "701.33", "701.51", "701.52", "702.106", "702.159"}
)

GAME_CONCEPTS = "1. Game Concepts"
GLOSSARY = "Glossary"

# A rule or subrule heading: "106.10.", "106.12a ", and the text's one
# unpunctuated rule heading, "606.5 ". Section headings ("106. Mana")
# deliberately do not match; an example belongs to the last rule or subrule
# heading before it.
HEADING_RE = re.compile(r"^\s*(\d{3})\.(\d+)([a-z]*)(?:\.\s+|\s+)\S")

# A section heading ("106. Mana"). It anchors no example, but it does start
# a paragraph, which the wrapped-line guard below needs to know.
SECTION_RE = re.compile(r"^\d{3}\. \S")

# The end of a paragraph. A real heading starts one; a line wrapped in the
# middle of a sentence does not.
PARAGRAPH_END_RE = re.compile(r'[.?!:”’"’)\]}]$')

EXAMPLE_PREFIX = "Example:"


def is_out_of_scope(rule):
    """True when the rule or subrule id is outside four-player Commander."""
    major = int(rule.split(".", 1)[0])
    if major in OUT_OF_SCOPE_SECTIONS:
        return True
    base = re.match(r"\d+\.\d+", rule)
    return base is not None and base.group(0) in OUT_OF_SCOPE_RULES


def rule_sort_key(rule):
    """Rule order: major, minor, then subrule letters shortest first (z < aa)."""
    m = re.match(r"(\d+)\.(\d+)([a-z]*)$", rule)
    if m is None:
        raise ValueError("not a rule id: %r" % (rule,))
    letters = m.group(3)
    return (int(m.group(1)), int(m.group(2)), len(letters), letters)


def body_lines(text):
    """The rules body: between the second GAME_CONCEPTS and GLOSSARY lines."""
    lines = text.replace("\r", "").replace("\f", "").split("\n")
    headings = [i for i, ln in enumerate(lines) if ln.strip() == GAME_CONCEPTS]
    glossary = [i for i, ln in enumerate(lines) if ln.strip() == GLOSSARY]
    if len(headings) < 2 or len(glossary) < 2:
        raise ValueError("expected %r and %r twice each" % (GAME_CONCEPTS, GLOSSARY))
    return lines[headings[1]:glossary[1]]


def _headingish(line):
    """True when the line is itself a section or rule heading."""
    return bool(SECTION_RE.match(line) or HEADING_RE.match(line))


def example_pairs(text):
    """[(rule, n)] for every worked example, in rule order then ordinal."""
    counts = {}
    last_rule = None
    last_key = None
    lines = body_lines(text)
    for i, raw in enumerate(lines):
        stripped = raw.strip()
        if stripped.startswith(EXAMPLE_PREFIX):
            if last_rule is None:
                raise ValueError("example before any rule heading")
            counts[last_rule] = counts.get(last_rule, 0) + 1
            continue
        m = HEADING_RE.match(raw)
        if m is None:
            continue
        rule = "%s.%s%s" % (m.group(1), m.group(2), m.group(3))
        key = rule_sort_key(rule)
        if last_key is not None and key <= last_key:
            # A wrapped cross-reference ("601.2b and 601.2f-h.") begins with
            # a rule id, but the rules text only ever moves forward.
            continue
        if i > 0:
            prev = lines[i - 1].rstrip()
            if (prev and prev.strip() != GAME_CONCEPTS
                    and not PARAGRAPH_END_RE.search(prev) and not _headingish(prev)):
                # A line wrapped out of the preceding sentence, not a heading.
                continue
        last_rule, last_key = rule, key
    pairs = []
    for rule in sorted(counts, key=rule_sort_key):
        for n in range(1, counts[rule] + 1):
            pairs.append((rule, n))
    return pairs


def read_ledger_rows(path):
    """[(rule, n, status)] for the ledger's data rows (header line validated)."""
    with open(path, encoding="utf-8") as fh:
        lines = fh.read().split("\n")
    if not lines or lines[0].split("\t") != ["rule", "n", "status", "scenario", "note"]:
        raise ValueError("%s: header must be rule\\tn\\tstatus\\tscenario\\tnote" % (path,))
    rows = []
    for ln in lines[1:]:
        if not ln:
            continue
        cols = ln.split("\t")
        if len(cols) != 5:
            raise ValueError("%s: expected 5 tab-separated columns: %s" % (path, ln))
        rows.append((cols[0], int(cols[1]), cols[2]))
    return rows


def read_ledger_pairs(path):
    """[(rule, n)] for the ledger's data rows (header line skipped)."""
    return [(r, n) for r, n, _ in read_ledger_rows(path)]


def diff(pairs, ledger_pairs):
    """(missing, excess): text pairs the ledger lacks, ledger pairs the text lacks."""
    want, have = set(pairs), set(ledger_pairs)
    missing = [p for p in pairs if p in want - have]
    excess = [p for p in ledger_pairs if p in have - want]
    return missing, excess


def main(argv):
    if len(argv) != 3:
        sys.stderr.write("usage: cr_example_ledger.py <rules-text> <ledger>\n")
        return 2
    with open(argv[1], encoding="utf-8") as fh:
        pairs = example_pairs(fh.read())
    ledger_rows = read_ledger_rows(argv[2])
    ledger_pairs = [(r, n) for r, n, _ in ledger_rows]
    ledger_statuses = {(r, n): st for r, n, st in ledger_rows}
    missing, excess = diff(pairs, ledger_pairs)
    for rule, n in missing:
        status = "out-of-scope" if is_out_of_scope(rule) else "open"
        print("+\t%s\t%d\t%s" % (rule, n, status))
    for rule, n in excess:
        print("-\t%s\t%d\t%s" % (rule, n, ledger_statuses[(rule, n)]))
    return 1 if (missing or excess) else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
