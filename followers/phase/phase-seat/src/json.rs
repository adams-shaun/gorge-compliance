//! Strict JSON (the SpellBench v2 engine-role line rules) and canonical JSON (RFC 8785 restricted to
//! integer-only data), as `docs/site/api/spellbench-v2.md` in gorge states them.
//!
//! Strict: valid UTF-8, one value and nothing after it, no duplicate keys at any depth, integers only
//! (no fraction, no exponent), absolute value at most 2^53-1, no leading zeros, nesting at most 64,
//! no raw control characters in strings, valid escapes, no unpaired surrogate escapes.

use serde_json::{Map, Number, Value};
use sha2::{Digest, Sha256};

/// The largest integer the wire takes (2^53 - 1).
pub const MAX_SAFE: i64 = 9_007_199_254_740_991;
const MAX_DEPTH: usize = 64;

/// Parse one strict-JSON line. The error is the `malformed_json` message.
pub fn parse_strict(line: &[u8]) -> Result<Value, String> {
    let text = std::str::from_utf8(line).map_err(|e| format!("invalid UTF-8 at {}", e.valid_up_to()))?;
    let mut p = Parser { b: text.as_bytes(), i: 0 };
    p.ws();
    let v = p.value(0)?;
    p.ws();
    if p.i != p.b.len() {
        return Err(format!("trailing data at {}", p.i));
    }
    Ok(v)
}

struct Parser<'a> {
    b: &'a [u8],
    i: usize,
}

impl Parser<'_> {
    fn ws(&mut self) {
        while matches!(self.b.get(self.i), Some(b' ' | b'\t' | b'\n' | b'\r')) {
            self.i += 1;
        }
    }

    fn value(&mut self, depth: usize) -> Result<Value, String> {
        match self.b.get(self.i) {
            None => Err("unexpected end".to_string()),
            Some(b'{') => self.object(depth),
            Some(b'[') => self.array(depth),
            Some(b'"') => Ok(Value::String(self.string()?)),
            Some(b't') => self.lit("true", Value::Bool(true)),
            Some(b'f') => self.lit("false", Value::Bool(false)),
            Some(b'n') => self.lit("null", Value::Null),
            Some(b'-' | b'0'..=b'9') => self.number(),
            Some(_) => Err(format!("unexpected character at {}", self.i)),
        }
    }

    fn deep(&self, depth: usize) -> Result<(), String> {
        if depth >= MAX_DEPTH {
            return Err(format!("nesting deeper than {MAX_DEPTH} at {}", self.i));
        }
        Ok(())
    }

    fn lit(&mut self, word: &str, v: Value) -> Result<Value, String> {
        if self.b[self.i..].starts_with(word.as_bytes()) {
            self.i += word.len();
            Ok(v)
        } else {
            Err(format!("bad literal at {}", self.i))
        }
    }

    fn number(&mut self) -> Result<Value, String> {
        let start = self.i;
        let neg = self.b[self.i] == b'-';
        if neg {
            self.i += 1;
        }
        let digits = self.i;
        while matches!(self.b.get(self.i), Some(b'0'..=b'9')) {
            self.i += 1;
        }
        if self.i == digits {
            return Err(format!("number without digits at {start}"));
        }
        if matches!(self.b.get(self.i), Some(b'.' | b'e' | b'E')) {
            return Err(format!("number with a fraction or an exponent at {start}"));
        }
        if self.i - digits > 1 && self.b[digits] == b'0' {
            return Err(format!("number with a leading zero at {start}"));
        }
        let text = std::str::from_utf8(&self.b[start..self.i]).expect("ascii digits");
        let n: i64 = text.parse().map_err(|_| format!("integer out of range at {start}"))?;
        if n.unsigned_abs() > MAX_SAFE as u64 {
            return Err(format!("integer beyond 2^53-1 at {start}"));
        }
        // `-0` reads as 0.
        Ok(Value::Number(Number::from(if n == 0 { 0 } else { n })))
    }

    fn hex4(&mut self) -> Result<u32, String> {
        let s = self.b.get(self.i..self.i + 4).ok_or_else(|| format!("short \\u escape at {}", self.i))?;
        let mut n = 0u32;
        for &c in s {
            n = n * 16 + (c as char).to_digit(16).ok_or_else(|| format!("bad \\u escape at {}", self.i))?;
        }
        self.i += 4;
        Ok(n)
    }

    fn string(&mut self) -> Result<String, String> {
        let at = self.i;
        self.i += 1;
        let mut out = String::new();
        loop {
            let c = *self.b.get(self.i).ok_or_else(|| format!("unterminated string at {at}"))?;
            match c {
                b'"' => {
                    self.i += 1;
                    return Ok(out);
                }
                b'\\' => {
                    self.i += 1;
                    let e = *self.b.get(self.i).ok_or_else(|| format!("unterminated escape at {at}"))?;
                    self.i += 1;
                    match e {
                        b'"' => out.push('"'),
                        b'\\' => out.push('\\'),
                        b'/' => out.push('/'),
                        b'b' => out.push('\u{8}'),
                        b'f' => out.push('\u{c}'),
                        b'n' => out.push('\n'),
                        b'r' => out.push('\r'),
                        b't' => out.push('\t'),
                        b'u' => {
                            let hi = self.hex4()?;
                            let cp = if (0xD800..0xDC00).contains(&hi) {
                                if self.b.get(self.i..self.i + 2) != Some(b"\\u") {
                                    return Err(format!("unpaired surrogate escape at {}", self.i));
                                }
                                self.i += 2;
                                let lo = self.hex4()?;
                                if !(0xDC00..0xE000).contains(&lo) {
                                    return Err(format!("unpaired surrogate escape at {}", self.i));
                                }
                                0x10000 + ((hi - 0xD800) << 10) + (lo - 0xDC00)
                            } else if (0xDC00..0xE000).contains(&hi) {
                                return Err(format!("unpaired surrogate escape at {}", self.i));
                            } else {
                                hi
                            };
                            out.push(char::from_u32(cp).ok_or_else(|| format!("bad code point at {}", self.i))?);
                        }
                        _ => return Err(format!("bad escape at {}", self.i - 1)),
                    }
                }
                0..=0x1f => return Err(format!("raw control character in string at {}", self.i)),
                _ => {
                    // The input is valid UTF-8: copy one whole character.
                    let rest = std::str::from_utf8(&self.b[self.i..]).expect("valid utf-8");
                    let ch = rest.chars().next().expect("non-empty");
                    out.push(ch);
                    self.i += ch.len_utf8();
                }
            }
        }
    }

    fn array(&mut self, depth: usize) -> Result<Value, String> {
        self.deep(depth)?;
        self.i += 1;
        let mut out = Vec::new();
        self.ws();
        if self.b.get(self.i) == Some(&b']') {
            self.i += 1;
            return Ok(Value::Array(out));
        }
        loop {
            self.ws();
            out.push(self.value(depth + 1)?);
            self.ws();
            match self.b.get(self.i) {
                Some(b',') => self.i += 1,
                Some(b']') => {
                    self.i += 1;
                    return Ok(Value::Array(out));
                }
                _ => return Err(format!("expected , or ] at {}", self.i)),
            }
        }
    }

    fn object(&mut self, depth: usize) -> Result<Value, String> {
        self.deep(depth)?;
        self.i += 1;
        let mut out = Map::new();
        self.ws();
        if self.b.get(self.i) == Some(&b'}') {
            self.i += 1;
            return Ok(Value::Object(out));
        }
        loop {
            self.ws();
            if self.b.get(self.i) != Some(&b'"') {
                return Err(format!("expected a key at {}", self.i));
            }
            let key = self.string()?;
            self.ws();
            if self.b.get(self.i) != Some(&b':') {
                return Err(format!("expected : at {}", self.i));
            }
            self.i += 1;
            self.ws();
            let v = self.value(depth + 1)?;
            if out.insert(key.clone(), v).is_some() {
                return Err(format!("duplicate key {key:?}"));
            }
            self.ws();
            match self.b.get(self.i) {
                Some(b',') => self.i += 1,
                Some(b'}') => {
                    self.i += 1;
                    return Ok(Value::Object(out));
                }
                _ => return Err(format!("expected , or }} at {}", self.i)),
            }
        }
    }
}

/// Canonical JSON: no whitespace, object keys by UTF-16 code units, integers in plain decimal, the
/// short escapes `\" \\ \b \f \n \r \t`, other controls below U+0020 as lowercase `\u00xx`, all else
/// raw UTF-8.
pub fn canonical(v: &Value) -> String {
    let mut s = String::new();
    write_canonical(v, &mut s);
    s
}

fn write_canonical(v: &Value, out: &mut String) {
    match v {
        Value::Null => out.push_str("null"),
        Value::Bool(b) => out.push_str(if *b { "true" } else { "false" }),
        Value::Number(n) => out.push_str(&n.to_string()),
        Value::String(s) => write_string(s, out),
        Value::Array(a) => {
            out.push('[');
            for (i, e) in a.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                write_canonical(e, out);
            }
            out.push(']');
        }
        Value::Object(m) => {
            let mut keys: Vec<&String> = m.keys().collect();
            keys.sort_by(|a, b| a.encode_utf16().cmp(b.encode_utf16()));
            out.push('{');
            for (i, k) in keys.into_iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                write_string(k, out);
                out.push(':');
                write_canonical(&m[k], out);
            }
            out.push('}');
        }
    }
}

fn write_string(s: &str, out: &mut String) {
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{8}' => out.push_str("\\b"),
            '\u{c}' => out.push_str("\\f"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
}

/// `"sha256:" + hex(SHA-256(bytes))`.
pub fn sha256_tag(bytes: &[u8]) -> String {
    format!("sha256:{}", hex(&Sha256::digest(bytes)))
}

pub fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write;
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        write!(s, "{b:02x}").expect("write to a String");
    }
    s
}

/// `deck_id`: rows sorted by name in code point order, canonical `[{"count":N,"name":".."},..]`.
pub fn deck_id(rows: &[(String, u32)]) -> String {
    let mut rows: Vec<&(String, u32)> = rows.iter().collect();
    rows.sort_by(|a, b| a.0.as_bytes().cmp(b.0.as_bytes()));
    let arr = rows
        .into_iter()
        .map(|(name, count)| {
            let mut m = Map::new();
            m.insert("count".into(), Value::from(*count));
            m.insert("name".into(), Value::String(name.clone()));
            Value::Object(m)
        })
        .collect();
    sha256_tag(canonical(&Value::Array(arr)).as_bytes())
}

/// `domain_id`: names sorted in code point order, the canonical array of strings.
pub fn domain_id(names: &[String]) -> String {
    let mut names: Vec<&String> = names.iter().collect();
    names.sort_by(|a, b| a.as_bytes().cmp(b.as_bytes()));
    let arr = names.into_iter().map(|n| Value::String(n.clone())).collect();
    sha256_tag(canonical(&Value::Array(arr)).as_bytes())
}

/// HMAC-SHA-256 (RFC 2104), written over `sha2` so no further crate is needed.
pub fn hmac_sha256(key: &[u8], msg: &[u8]) -> [u8; 32] {
    let mut k = [0u8; 64];
    if key.len() > 64 {
        k[..32].copy_from_slice(&Sha256::digest(key));
    } else {
        k[..key.len()].copy_from_slice(key);
    }
    let mut inner = Sha256::new();
    inner.update(k.map(|b| b ^ 0x36));
    inner.update(msg);
    let mut outer = Sha256::new();
    outer.update(k.map(|b| b ^ 0x5c));
    outer.update(inner.finalize());
    outer.finalize().into()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn canonical_test_vector() {
        let v = parse_strict(r#"{"b":"Chainer's Edict","a":"Lim-D\u00fbl's Vault","c":"tab\there"}"#.as_bytes()).unwrap();
        let c = canonical(&v);
        assert_eq!(c, "{\"a\":\"Lim-D\u{fb}l's Vault\",\"b\":\"Chainer's Edict\",\"c\":\"tab\\there\"}");
        assert_eq!(sha256_tag(c.as_bytes()), "sha256:041575311eb1deb02f63f70361e14159034faf0d2a31e57edf8b4cf037680377");
    }

    #[test]
    fn deck_and_domain_vectors() {
        let rows = vec![("Mountain".to_string(), 18), ("Lightning Bolt".to_string(), 4)];
        assert_eq!(deck_id(&rows), "sha256:0df0a001e3c4b74b1061b21e319a645f32fbe3173120e432864e14d6d6f2f5d2");
        let names = vec!["Mountain".to_string(), "Lightning Bolt".to_string()];
        assert_eq!(domain_id(&names), "sha256:74f7f4b39eecbed1c039cf4b229fa533069d2cdd8caf3bb6380b832eb40fb697");
    }

    #[test]
    fn utf16_key_order() {
        // U+1F600 (a surrogate pair, 0xD83D...) sorts before U+FFFD in UTF-16 code units.
        let v = parse_strict("{\"\u{fffd}\":1,\"\u{1f600}\":2}".as_bytes()).unwrap();
        assert_eq!(canonical(&v), "{\"\u{1f600}\":2,\"\u{fffd}\":1}");
    }

    #[test]
    fn strict_rules_refuse_what_the_spec_lists() {
        for bad in [
            &br#"{"a":1} x"#[..],
            br#"{"request_type":"hello","request_type":"hello"}"#,
            br#"{"protocol_minor":1.0}"#,
            br#"{"protocol_minor":1e3}"#,
            br#"9007199254740992"#,
            br#"007"#,
            br#""\ud800""#,
            b"\"a\x01b\"",
            br#"{"a":"\q"}"#,
            &[b'['; 65].iter().chain(&[b']'; 65]).copied().collect::<Vec<u8>>(),
            &[0xff, 0xfe],
        ] {
            assert!(parse_strict(bad).is_err(), "should refuse {:?}", String::from_utf8_lossy(bad));
        }
        // 64 levels pass, 65 do not.
        let ok: Vec<u8> = [b'['; 64].iter().chain(&[b']'; 64]).copied().collect();
        assert!(parse_strict(&ok).is_ok());
        assert_eq!(parse_strict(b"-0").unwrap(), Value::from(0));
        assert_eq!(parse_strict(b"9007199254740991").unwrap(), Value::from(9_007_199_254_740_991i64));
        assert_eq!(parse_strict(r#""\ud83d\ude00""#.as_bytes()).unwrap(), Value::String("\u{1f600}".into()));
    }

    #[test]
    fn hmac_rfc4231_case_2() {
        let mac = hmac_sha256(b"Jefe", b"what do ya want for nothing?");
        assert_eq!(hex(&mac), "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843");
    }
}
