"""Name identity: norm_name and resolve().

norm_name is moved verbatim from four-yay tools/deckbuild/deckdb.py (147-164 at
origin/main 3adaa57b7a); it mirrors gorge engine/cards.NormalizeName. Keep it
byte-for-byte; the goldens in tests/test_norm.py come from gorge's
normalize_name_test.go.
"""
import hashlib
import unicodedata

_MARKS = {"æ": "ae", "œ": "oe", "ø": "o", "ß": "ss", "ð": "d", "þ": "th", "ł": "l"}


def norm_name(s):
    """gorge engine/cards.NormalizeName: front face, lowercase, diacritics off,
    punctuation dropped, whitespace collapsed."""
    i = s.find("//")
    if i > 0:
        s = s[:i]
    out, prev_space = [], True
    for ch in s.lower():
        if ch.isalpha() or ch.isdigit():
            ch = _MARKS.get(ch, ch)
            d = unicodedata.normalize("NFKD", ch)
            out.append("".join(c for c in d if not unicodedata.combining(c)))
            prev_space = False
        elif ch.isspace():
            if not prev_space:
                out.append(" ")
                prev_space = True
    return "".join(out).strip()


def pseudo_id(name):
    """x:<sha1(norm_name)[:16]>: the id of a name no MTGJSON card answers to."""
    return "x:" + hashlib.sha1(norm_name(name).encode("utf-8")).hexdigest()[:16]


# A ruling's id has to survive the source's formatting: a typographic quote and
# its ASCII spelling are the same ruling, and so is the same sentence wrapped
# differently. Both are folded before hashing; the line-ending canonicalisation
# is the catalogue's oracle_sha normalisation (catalogue.normalise_text).
_QUOTES = str.maketrans({
    "\u201c": '"', "\u201d": '"', "\u201e": '"', "\u201f": '"',
    "\u2018": "'", "\u2019": "'", "\u201a": "'", "\u201b": "'",
})


def normalised_ruling_text(text):
    """The text a ruling id covers: catalogue-normalised, quotes folded, whitespace collapsed."""
    from .catalogue import normalise_text  # lazy: catalogue imports store, which imports norm's siblings
    t = normalise_text([{"text": str(text)}])
    return " ".join(t.translate(_QUOTES).split())


def ruling_id(oracle_id, published, text):
    """First 12 hex of sha256("<oracle_id>\\n<published>\\n<normalised text>")."""
    key = "\n".join((str(oracle_id), str(published), normalised_ruling_text(text)))
    return hashlib.sha256(key.encode("utf-8")).hexdigest()[:12]


def name_keys(name):
    """Every norm_name a card name answers to: the front face (what gorge keys
    on) and, for `Front // Back`, the back face."""
    keys = [norm_name(name)]
    i = name.find("//")
    if i > 0:
        back = norm_name(name[i + 2:])
        if back and back not in keys:
            keys.append(back)
    return [k for k in keys if k]


class Resolver:
    """name -> oracle_id from a card list, else the x: pseudo id.

    Misses are remembered (name -> pseudo id) so a caller can flush them to
    `_meta/unresolved` in one put (store.Store.flush_unresolved)."""

    def __init__(self, cards):
        """cards: iterable of (oracle_id, name)."""
        self._by_norm = {}
        for oid, name in sorted(cards):
            for k in name_keys(name):
                self._by_norm.setdefault(k, oid)  # smallest oracle_id wins a fold
        self.misses = {}

    def resolve(self, name):
        oid = self._by_norm.get(norm_name(name))
        if oid is not None:
            return oid
        pid = pseudo_id(name)
        self.misses[name] = pid
        return pid
