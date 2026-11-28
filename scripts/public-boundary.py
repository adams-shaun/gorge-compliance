#!/usr/bin/env python3
"""public-boundary.py [--fix]: no tracked file names a private host path or box address.

This repo is public. 11-28: committed evidence and diagnosis text carried a home directory, the private
training disk's mount point and a farm box's IP (inside a `fleet/remote/pulled/<ip>/` archive path). The
CI lint job runs this check; --fix rewrites the matches to neutral placeholders in place (the forward fix,
no history rewrite), so a seat that copies a local path into a note sees its PR go red with the fix named.

Exit 0 clean, 1 with one `path:line: <match>` per hit.
"""
import re
import subprocess
import sys

# Each pattern is written so this file's own text does not match it. /home/ubuntu and /home/runner are the generic
# cloud and CI accounts that scripts address on remote boxes, not anyone's home.
HOME = r"/home/(?!ubuntu\b|runner\b)[a-z_][a-z0-9_-]*"
RULES = [
    (re.compile(HOME + r"/projects/"), "~/projects/"),
    (re.compile(HOME + r"(?=[/\s`'\")]|$)"), "~"),
    (re.compile(r"(fleet/remote/pulled/)\d{1,3}(?:\.\d{1,3}){3}/"), r"\1<box>/"),
    (re.compile(r"/mn[t]/sata/gorge-training/"), "<training-root>/"),
    (re.compile(r"/mn[t]/sata\b"), "<training-root>"),
]
SELF = "scripts/public-boundary.py"


def tracked():
    out = subprocess.run(["git", "ls-files", "-z"], capture_output=True, check=True).stdout
    return [p for p in out.decode().split("\0") if p and p != SELF]


def main(argv):
    fix = "--fix" in argv
    hits = 0
    for path in tracked():
        try:
            with open(path, encoding="utf-8") as fh:
                text = fh.read()
        except (UnicodeDecodeError, IsADirectoryError, FileNotFoundError):
            continue
        new = text
        for rx, rep in RULES:
            new = rx.sub(rep, new)
        if new == text:
            continue
        if fix:
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(new)
            print(f"public-boundary: rewrote {path}")
            continue
        for n, line in enumerate(text.splitlines(), 1):
            for rx, _ in RULES:
                m = rx.search(line)
                if m:
                    print(f"{path}:{n}: {m.group(0)}")
                    hits += 1
                    break
    if hits:
        print(f"public-boundary: {hits} private path(s) in tracked files; run python3 scripts/public-boundary.py --fix")
        return 1
    if not fix:
        print("public-boundary: clean")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
