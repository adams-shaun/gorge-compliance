//! Corpus spelling -> phase faces.
//!
//! phase's database is keyed by lower-case *face* name: `fire` and `ice` are keys, `fire // ice`
//! is not. The database's own lookup (`get_face_by_name`) already splits composite names, falls
//! back to the front face and folds the common accents; this module adds an NFKD retry for the
//! spellings that fold misses, finds the other face of a two-faced card, and reports an unknown
//! name as an error instead of skipping it (`resolve_deck_list` silently skips unknown names, so
//! it is never used here).

use engine::database::card_db::CardDatabase;
use engine::types::card::{CardFace, PrintedCardRef};
use unicode_normalization::char::is_combining_mark;
use unicode_normalization::UnicodeNormalization;

/// One face of a card as phase knows it.
pub type FaceRef<'a> = &'a CardFace;

/// NFKD-folded, accent-stripped form of `name` (curly apostrophes straightened).
pub fn fold(name: &str) -> String {
    let mut out = String::with_capacity(name.len());
    for c in name.nfkd().filter(|c| !is_combining_mark(*c)) {
        match c {
            '\u{2019}' | '\u{2018}' => out.push('\''),
            // Ligatures have no NFKD decomposition; phase spells them out ("Aether Vial").
            '\u{c6}' => out.push_str("Ae"),
            '\u{e6}' => out.push_str("ae"),
            '\u{152}' => out.push_str("Oe"),
            '\u{153}' => out.push_str("oe"),
            c => out.push(c),
        }
    }
    out
}

fn lookup<'a>(db: &'a CardDatabase, name: &str) -> Option<&'a CardFace> {
    db.get_face_by_name(name).or_else(|| {
        let folded = fold(name);
        (folded != name).then(|| db.get_face_by_name(&folded)).flatten()
    })
}

/// Every face of the card `corpus_name` spells, front face first. A name phase does not know is an
/// `Err("not in card-data")`.
pub fn phase_faces<'a>(db: &'a CardDatabase, corpus_name: &str) -> Result<Vec<FaceRef<'a>>, String> {
    let found = lookup(db, corpus_name).ok_or_else(|| "not in card-data".to_string())?;
    let mut faces = vec![found];
    if let Some(oracle_id) = found.scryfall_oracle_id.clone() {
        let printed = PrintedCardRef { oracle_id, face_name: found.name.clone() };
        if let Some(other) = db.get_other_face_by_printed_ref(&printed) {
            // `is_front_face_key` is true for single-faced cards and for face 0 of a multi-faced one.
            if db.is_front_face_key(&found.name.to_lowercase()) {
                faces.push(other);
            } else {
                faces.insert(0, other);
            }
        }
    }
    Ok(faces)
}
