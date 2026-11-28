import unittest

from .helpers import Base, OID_A, OID_B  # noqa: F401  (sets up sys.path-independent imports)
from carddb import Resolver, norm_name, pseudo_id, ruling_id


class NormName(unittest.TestCase):
    def test_marked_equals_plain(self):
        # gorge engine/cards normalize_name_test.go TestNormalizeNameFoldsDiacritics
        for marked, plain in [
            ("Dáin Ironfoot", "Dain Ironfoot"),
            ("Lim-Dûl's Vault", "Lim-Dul's Vault"),
            ("Mjölnir, Hammer of Thor", "Mjolnir, Hammer of Thor"),
            ("Araña, Heart of the Spider", "Arana, Heart of the Spider"),
            ("Bespoke Bō", "Bespoke Bo"),
            ("Bartolomé del Presidio", "Bartolome del Presidio"),
            ("Éowyn, Lady of Rohan", "eowyn lady of rohan"),
            ("Robo-Piñata", "Robo-Pinata"),
        ]:
            self.assertEqual(norm_name(marked), norm_name(plain), marked)

    def test_golden_values(self):
        # values of gorge NormalizeName at the pin (go run, see the ticket report)
        for name, want in GOLDEN:
            self.assertEqual(norm_name(name), want, name)

    def test_known_divergence(self):
        for name, ours, gorge in KNOWN_DIVERGENT:
            self.assertEqual(norm_name(name), ours)
            self.assertNotEqual(ours, gorge)

    def test_pseudo_id(self):
        self.assertEqual(pseudo_id("Forge Only"), pseudo_id("forge  only!"))
        self.assertRegex(pseudo_id("Forge Only"), r"^x:[0-9a-f]{16}$")

    def test_resolver(self):
        r = Resolver([(OID_B, "Fire // Ice"), (OID_A, "Lim-Dûl's Vault")])
        self.assertEqual(r.resolve("fire"), OID_B)
        self.assertEqual(r.resolve("Ice"), OID_B)  # back face
        self.assertEqual(r.resolve("Lim-Dul's Vault"), OID_A)
        self.assertTrue(r.resolve("No Such Card").startswith("x:"))
        self.assertEqual(list(r.misses), ["No Such Card"])


class TestRulingId(unittest.TestCase):
    OID = OID_A
    PUBLISHED = "2026-01-02"
    TEXT = 'The "named" permanent stays tapped.'

    def test_fixed_id(self):
        # sha256("ab00...00\n2026-01-02\nThe \"named\" permanent stays tapped.")[:12]
        self.assertEqual(ruling_id(self.OID, self.PUBLISHED, self.TEXT), "cd162149f976")

    def test_formatting_variants_agree(self):
        want = ruling_id(self.OID, self.PUBLISHED, self.TEXT)
        for text in (
            '  The   "named"\tpermanent stays tapped.  ',
            "The \u201cnamed\u201d permanent stays tapped.\r\n",
            'The\n"named" permanent\nstays tapped.',
        ):
            self.assertEqual(ruling_id(self.OID, self.PUBLISHED, text), want, repr(text))

    def test_date_changes_id(self):
        self.assertNotEqual(ruling_id(self.OID, self.PUBLISHED, self.TEXT),
                            ruling_id(self.OID, "2026-01-03", self.TEXT))


# (input, gorge cards.NormalizeName output at the pinned commit), measured with go run
GOLDEN = [
    ("Lightning Bolt", "lightning bolt"),
    ("Fire // Ice", "fire"),
    ("Dáin Ironfoot", "dain ironfoot"),
    ("Lim-Dûl's Vault", "limduls vault"),
    ("Jötun Grunt", "jotun grunt"),
    ("Urza's Tower", "urzas tower"),
    ("  Wear  //  Tear ", "wear"),
    ("Kongming, \u201cSleeping Dragon\u201d", "kongming sleeping dragon"),
    ("Séance", "seance"),
    ("B.F.M. (Big Furry Monster)", "bfm big furry monster"),
    ("Who // What // When // Where // Why", "who"),
    ("Bartolomé del Presidio", "bartolome del presidio"),
    ("Lúthien's Gift", "luthiens gift"),
    ("Ōkina, Temple to the Grandfathers", "okina temple to the grandfathers"),
    ("Ifh-Bíff Efreet", "ifhbiff efreet"),
]

# Known divergence, kept because norm_name is moved verbatim from four-yay: its _MARKS table
# folds the ligature (Æ -> ae) where gorge leaves it ("æther vial"). Both sides stay
# self-consistent; a carddb key never crosses into gorge's byName index.
KNOWN_DIVERGENT = [("Æther Vial", "aether vial", "æther vial")]
