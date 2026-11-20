"""Launcher: python3 -I cmd/carddb/main.py <verb> ... (-I drops the script directory from
sys.path, so the package is put on it here)."""
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "py"))

from carddb.cli import main  # noqa: E402

if __name__ == "__main__":
    sys.exit(main())
