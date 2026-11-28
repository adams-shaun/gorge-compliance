"""Tests for scripts/cr_example_ledger.py: python3 -m unittest scripts/cr_example_ledger_test.py"""

import io
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import cr_example_ledger as C

# A ten-line synthetic rules text: the body lies between the second
# "1. Game Concepts" line and the second "Glossary" line, the decoy example
# before the body must not count, and a form feed must be dropped.
SYNTHETIC = (
    "1. Game Concepts\n"
    "Glossary\n"
    "1. Game Concepts\n"
    "   100.1. Some rule text.\n"
    "      100.1a Some subrule.\n"
    "         Example: First worked example.\n"
    "         Example: Second worked example.\n"
    "700. Some later rule.\n"
    "\fGlossary\n"
    "Glossary\n"
)


class PairsTest(unittest.TestCase):
    def test_two_examples_under_one_subrule(self):
        self.assertEqual(C.example_pairs(SYNTHETIC), [("100.1a", 1), ("100.1a", 2)])

    def test_example_before_body_not_counted(self):
        text = "1. Game Concepts\n   Example: decoy.\n" + SYNTHETIC
        self.assertEqual(C.example_pairs(text), [("100.1a", 1), ("100.1a", 2)])

    def test_carriage_returns_dropped(self):
        self.assertEqual(C.example_pairs(SYNTHETIC.replace("\n", "\r\n")),
                         [("100.1a", 1), ("100.1a", 2)])

    def test_out_of_scope(self):
        self.assertTrue(C.is_out_of_scope("900.4"))
        self.assertTrue(C.is_out_of_scope("701.31a"))
        self.assertFalse(C.is_out_of_scope("701.30"))
        self.assertFalse(C.is_out_of_scope("603.7a"))

    def test_diff_and_main(self):
        pairs = [("100.1a", 1), ("100.1a", 2)]
        missing, excess = C.diff(pairs, [("100.1a", 1), ("900.4", 1)])
        self.assertEqual(missing, [("100.1a", 2)])
        self.assertEqual(excess, [("900.4", 1)])
        with tempfile.TemporaryDirectory() as d:
            text_path = os.path.join(d, "rules.txt")
            ledger_path = os.path.join(d, "LEDGER.tsv")
            with open(text_path, "w") as fh:
                fh.write(SYNTHETIC)
            with open(ledger_path, "w") as fh:
                fh.write("rule\tn\tstatus\tscenario\tnote\n"
                         "100.1a\t1\topen\t\t\n"
                         "900.4\t1\tout-of-scope\t\t\n")
            stdout = io.StringIO()
            saved = sys.stdout
            sys.stdout = stdout
            try:
                rc = C.main(["cr_example_ledger.py", text_path, ledger_path])
            finally:
                sys.stdout = saved
            self.assertEqual(rc, 1)
            self.assertEqual(stdout.getvalue().splitlines(),
                             ["+\t100.1a\t2\topen", "-\t900.4\t1\tout-of-scope"])


if __name__ == "__main__":
    unittest.main()
