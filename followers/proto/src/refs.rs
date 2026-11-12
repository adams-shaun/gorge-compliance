//! gorge's object-reference grammar and a ref-to-engine-id table.
//!
//! * `pN` is seat N (a player).
//! * `pN:Name` / `pN:Name#k`: a card OWNED by seat N; no suffix is the first, `#2` the second.
//! * `pN:token:Sub#k`: the k-th battlefield token CONTROLLED by seat N whose name contains `Sub`
//!   (case-insensitive).
//! * `pN:ability:Name#k`: an ability on the stack (parsed, never resolved by a follower).
//!
//! A card name may contain `:` (only after the first `pN:`); `#` only appears as the trailing
//! ordinal, which is always digits.

use std::collections::BTreeMap;
use std::fmt;

/// One parsed object reference. `ord` is 1-based.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Ref {
    Player(u8),
    Card { seat: u8, name: String, ord: u32 },
    Token { seat: u8, sub: String, ord: u32 },
    Ability { seat: u8, name: String, ord: u32 },
}

/// Split `pN` / `pN:rest` into the seat and the (possibly absent) rest.
fn split_seat(s: &str) -> Result<(u8, Option<&str>), String> {
    let body = s
        .strip_prefix('p')
        .ok_or_else(|| format!("ref {s:?}: must start with 'p'"))?;
    let (digits, rest) = match body.find(':') {
        Some(i) => (&body[..i], Some(&body[i + 1..])),
        None => (body, None),
    };
    if digits.is_empty() || !digits.bytes().all(|b| b.is_ascii_digit()) {
        return Err(format!("ref {s:?}: bad seat {digits:?}"));
    }
    let seat = digits
        .parse::<u8>()
        .map_err(|_| format!("ref {s:?}: seat {digits:?} out of range"))?;
    Ok((seat, rest))
}

/// Split a trailing `#digits` ordinal off `s`; no suffix means ordinal 1. `None` if a `#` is
/// present but is not followed by a positive integer.
fn split_ord(s: &str) -> Option<(&str, u32)> {
    match s.rfind('#') {
        Some(i) => {
            let d = &s[i + 1..];
            if d.is_empty() || !d.bytes().all(|b| b.is_ascii_digit()) {
                return None;
            }
            let k: u32 = d.parse().ok()?;
            if k == 0 {
                return None;
            }
            Some((&s[..i], k))
        }
        None => Some((s, 1)),
    }
}

impl Ref {
    pub fn parse(s: &str) -> Result<Ref, String> {
        let (seat, rest) = split_seat(s)?;
        let Some(rest) = rest else {
            return Ok(Ref::Player(seat));
        };
        let (name, ord) =
            split_ord(rest).ok_or_else(|| format!("ref {s:?}: bad '#' ordinal suffix"))?;
        if name.is_empty() {
            return Err(format!("ref {s:?}: empty name"));
        }
        if let Some(sub) = name.strip_prefix("token:") {
            if sub.is_empty() {
                return Err(format!("ref {s:?}: empty token name"));
            }
            return Ok(Ref::Token { seat, sub: sub.to_string(), ord });
        }
        if let Some(n) = name.strip_prefix("ability:") {
            if n.is_empty() {
                return Err(format!("ref {s:?}: empty ability name"));
            }
            return Ok(Ref::Ability { seat, name: n.to_string(), ord });
        }
        Ok(Ref::Card { seat, name: name.to_string(), ord })
    }
}

impl fmt::Display for Ref {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        fn ord(f: &mut fmt::Formatter<'_>, k: u32) -> fmt::Result {
            if k > 1 {
                write!(f, "#{k}")
            } else {
                Ok(())
            }
        }
        match self {
            Ref::Player(n) => write!(f, "p{n}"),
            Ref::Card { seat, name, ord: k } => {
                write!(f, "p{seat}:{name}")?;
                ord(f, *k)
            }
            Ref::Token { seat, sub, ord: k } => {
                write!(f, "p{seat}:token:{sub}")?;
                ord(f, *k)
            }
            Ref::Ability { seat, name, ord: k } => {
                write!(f, "p{seat}:ability:{name}")?;
                ord(f, *k)
            }
        }
    }
}

/// The bare name of a ref string: `pN:`, `token:`/`ability:` and `#k` stripped. A bare player
/// ref (`pN`) has no name and yields "".
pub fn ref_name(r: &str) -> &str {
    let mut s = r;
    if let Some(body) = s.strip_prefix('p') {
        match body.find(':') {
            Some(i) if i > 0 && body[..i].bytes().all(|b| b.is_ascii_digit()) => {
                s = &body[i + 1..];
            }
            None if !body.is_empty() && body.bytes().all(|b| b.is_ascii_digit()) => return "",
            _ => {}
        }
    }
    for p in ["token:", "ability:"] {
        if let Some(rest) = s.strip_prefix(p) {
            s = rest;
            break;
        }
    }
    match split_ord(s) {
        Some((n, _)) => n,
        None => s,
    }
}

/// Ref-to-engine-id table. Ids are whatever the engine uses; a ref stays bound to its object
/// through zone changes (use [`RefTable::rebind`] when the engine re-ids on a zone change).
/// Backed by a `BTreeMap` so no iteration order is ever nondeterministic.
#[derive(Debug, Clone)]
pub struct RefTable<Id: Copy + Eq> {
    bound: BTreeMap<Ref, Id>,
}

impl<Id: Copy + Eq> Default for RefTable<Id> {
    fn default() -> Self {
        RefTable { bound: BTreeMap::new() }
    }
}

impl<Id: Copy + Eq> RefTable<Id> {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn bind(&mut self, r: &Ref, id: Id) {
        self.bound.insert(r.clone(), id);
    }

    pub fn bound(&self, r: &Ref) -> Option<Id> {
        self.bound.get(r).copied()
    }

    /// Resolve `r`: a bound ref yields its id; an unbound card ref is the k-th live object (in
    /// creation order) owned by its seat with a current or face name equal to the ref's name.
    /// Token, ability and player refs are never resolved from `live`.
    pub fn resolve(&self, r: &Ref, live: &[(Id, u8, Vec<String>)]) -> Option<Id> {
        if let Some(id) = self.bound(r) {
            return Some(id);
        }
        let Ref::Card { seat, name, ord } = r else {
            return None;
        };
        live.iter()
            .filter(|(_, owner, names)| owner == seat && names.iter().any(|n| n == name))
            .nth((*ord as usize).checked_sub(1)?)
            .map(|(id, _, _)| *id)
    }

    /// Re-point every ref bound to `old` at `new`.
    pub fn rebind(&mut self, old: Id, new: Id) {
        for id in self.bound.values_mut() {
            if *id == old {
                *id = new;
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn refs_parse_round_trip() {
        for s in [
            "p0",
            "p1",
            "p0:Lightning Bolt",
            "p1:Mountain#2",
            "p0:token:Goblin#3",
            "p1:token:Treasure",
            "p0:ability:Llanowar Elves#2",
        ] {
            let r = Ref::parse(s).unwrap();
            assert_eq!(r.to_string(), s);
            assert_eq!(Ref::parse(&r.to_string()).unwrap(), r);
        }
        assert_eq!(
            Ref::parse("p1:Mountain#2").unwrap(),
            Ref::Card { seat: 1, name: "Mountain".into(), ord: 2 }
        );
        assert_eq!(
            Ref::parse("p0:Mountain").unwrap(),
            Ref::Card { seat: 0, name: "Mountain".into(), ord: 1 }
        );
        assert_eq!(
            Ref::parse("p0:ability:Foo#2").unwrap(),
            Ref::Ability { seat: 0, name: "Foo".into(), ord: 2 }
        );
        for bad in ["", "x", "p", "pX", "p0:", "p0:Foo#", "p0:Foo#0", "p0:Foo#a", "p999", "p0:token:"] {
            assert!(Ref::parse(bad).is_err(), "{bad:?} should not parse");
        }
        assert_eq!(ref_name("p0:token:Goblin#2"), "Goblin");
        assert_eq!(ref_name("p1:Mountain#2"), "Mountain");
        assert_eq!(ref_name("p1:ability:Bolt"), "Bolt");
        assert_eq!(ref_name("p1"), "");
    }

    #[test]
    fn refs_name_with_colon() {
        let r = Ref::parse("p0:Circle of Protection: Red#2").unwrap();
        assert_eq!(
            r,
            Ref::Card { seat: 0, name: "Circle of Protection: Red".into(), ord: 2 }
        );
        assert_eq!(r.to_string(), "p0:Circle of Protection: Red#2");
        assert_eq!(ref_name("p0:Circle of Protection: Red"), "Circle of Protection: Red");
        assert_eq!(ref_name("p0:Circle of Protection: Red#2"), "Circle of Protection: Red");
    }

    #[test]
    fn refs_token_case_insensitive() {
        // The grammar matches the substring case-insensitively; the ref keeps it as written and
        // the driver matches it against token names.
        let a = Ref::parse("p0:token:Goblin#2").unwrap();
        let Ref::Token { seat, sub, ord } = &a else { panic!("not a token: {a:?}") };
        assert_eq!((*seat, *ord), (0, 2));
        assert!("Goblin Token".to_lowercase().contains(&sub.to_lowercase()));
        assert!("goblin token".to_lowercase().contains(&sub.to_lowercase()));
        assert_eq!(ref_name("p0:token:GOBLIN"), "GOBLIN");
        // Token refs never resolve from the live card list.
        let t: RefTable<u32> = RefTable::new();
        assert_eq!(t.resolve(&a, &[(1, 0, vec!["Goblin Token".into()])]), None);
    }

    #[test]
    fn refs_unbound_kth_live_object() {
        let t: RefTable<u32> = RefTable::new();
        let live = vec![
            (10, 0, vec!["Mountain".to_string()]),
            (11, 1, vec!["Mountain".to_string()]),
            (12, 0, vec!["Mountain".to_string()]),
            (13, 0, vec!["Fire // Ice".to_string(), "Fire".to_string(), "Ice".to_string()]),
        ];
        let c = |s: &str| Ref::parse(s).unwrap();
        assert_eq!(t.resolve(&c("p0:Mountain"), &live), Some(10));
        assert_eq!(t.resolve(&c("p0:Mountain#2"), &live), Some(12));
        assert_eq!(t.resolve(&c("p0:Mountain#3"), &live), None);
        assert_eq!(t.resolve(&c("p1:Mountain"), &live), Some(11));
        assert_eq!(t.resolve(&c("p0:Ice"), &live), Some(13));
        assert_eq!(t.resolve(&c("p0:Fire"), &live), Some(13));
        assert_eq!(t.resolve(&c("p0"), &live), None);
    }

    #[test]
    fn refs_bound_follows_rebind() {
        let mut t: RefTable<u32> = RefTable::new();
        let r = Ref::parse("p0:Mountain#2").unwrap();
        let live = vec![(1, 0, vec!["Mountain".to_string()]), (2, 0, vec!["Mountain".to_string()])];
        t.bind(&r, 1);
        // Bound wins over the unbound rule.
        assert_eq!(t.resolve(&r, &live), Some(1));
        assert_eq!(t.bound(&r), Some(1));
        t.rebind(1, 77);
        assert_eq!(t.bound(&r), Some(77));
        assert_eq!(t.resolve(&r, &live), Some(77));
        t.rebind(5, 6);
        assert_eq!(t.bound(&r), Some(77));
    }
}
