//! Per-run local session token (docs/08 §2): 32 random bytes, hex encoded.
//!
//! The token is generated once per app run, handed to the Go sidecar via the
//! `FT_SESSION_TOKEN` environment variable and exposed to the WebView through
//! the `get_backend_config` command. It must never be logged (docs/08 §4).

const TOKEN_BYTES: usize = 32;
const HEX_DIGITS: &[u8; 16] = b"0123456789abcdef";

/// Generates a 64-character lowercase hex session token.
///
/// Panics only if the OS entropy source is unavailable, which is an
/// unrecoverable environment failure; it never happens on the hot path.
pub fn generate_session_token() -> String {
    use rand::RngCore;

    let mut bytes = [0u8; TOKEN_BYTES];
    rand::rng().fill_bytes(&mut bytes);

    let mut out = String::with_capacity(TOKEN_BYTES * 2);
    for byte in bytes {
        out.push(HEX_DIGITS[(byte >> 4) as usize] as char);
        out.push(HEX_DIGITS[(byte & 0x0f) as usize] as char);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn token_is_64_lowercase_hex_chars() {
        let token = generate_session_token();
        assert_eq!(token.len(), TOKEN_BYTES * 2);
        assert!(
            token
                .chars()
                .all(|c| c.is_ascii_hexdigit() && !c.is_ascii_uppercase()),
            "token must be lowercase hex, got: {token}"
        );
    }

    #[test]
    fn tokens_differ_between_calls() {
        let a = generate_session_token();
        let b = generate_session_token();
        assert_ne!(a, b);
    }
}
