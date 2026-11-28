"""carddb: the card data set's tool (python3 -I, stdlib only). See cmd/carddb/README.md.

API:  carddb.put(table, rows, writer=...)   stage rows in this machine's clone
      carddb.publish()                      commit, rebase, push what is staged
      carddb.norm_name(name), carddb.resolve(name), carddb.ruling_id(oracle_id, published, text)
"""
from .norm import Resolver, norm_name, pseudo_id, ruling_id
from .schema import PRIVATE, PUBLIC, load_schema
from .shard import ValidationError
from .store import Store, StoreError, carddb_home


def _store_for(table, home, schema):
    s = load_schema(schema) if isinstance(schema, str) else schema
    t = (s or load_schema()).tables.get(table)
    kind = PRIVATE if t is not None and t.is_private() and not table.startswith("_meta/") else PUBLIC
    return Store(kind, home=home, schema=s or None)


def put(table, rows, writer=None, home=None, schema=None, **kw):
    """Merge rows (dicts or column-ordered sequences) into the table's shards of the
    clone under CARDDB_HOME and stage them. Returns the number of rows changed."""
    return _store_for(table, home, schema).put(table, rows, writer=writer, **kw)


def publish(kind=PUBLIC, home=None, schema=None, **kw):
    return Store(kind, home=home, schema=schema).publish(**kw)


def resolve(name, home=None, schema=None, record=True, resolver=None):
    """name -> oracle_id from the clone's cards, else x:<sha1(norm_name)[:16]> (and the miss
    is recorded in _meta/unresolved unless record=False). Pass a Resolver for bulk use."""
    store = Store(PUBLIC, home=home, schema=schema)
    r = resolver or Resolver(store.cards())
    out = r.resolve(name)
    if record and resolver is None and r.misses:
        store.flush_unresolved(r)
    return out
