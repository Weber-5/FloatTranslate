//! READY handshake line parsing (frozen contract).
//!
//! The Go backend prints exactly one line of JSON to stdout:
//! `{"status":"ready","port":<int>,"version":"1.0.0"}`
//!
//! This module is a pure function so it can be unit tested in isolation.

use serde_json::Value;

/// Successfully parsed READY announcement.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReadyInfo {
    /// Loopback port the backend bound (must be non-zero).
    pub port: u16,
    /// Backend version string.
    pub version: String,
}

/// Parse failure classification.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReadyLineError {
    /// The line is not a READY announcement (log output, other JSON, ...).
    /// The handshake should keep waiting for later lines.
    NotReady,
    /// The line claims `status == "ready"` but the payload is invalid.
    /// The handshake should fail fast with this reason.
    Invalid(String),
}

/// Parses one stdout line of the backend process.
pub fn parse_ready_line(line: &str) -> Result<ReadyInfo, ReadyLineError> {
    let value: Value = serde_json::from_str(line).map_err(|_| ReadyLineError::NotReady)?;
    let Value::Object(fields) = &value else {
        return Err(ReadyLineError::NotReady);
    };

    match fields.get("status").and_then(Value::as_str) {
        Some("ready") => {}
        _ => return Err(ReadyLineError::NotReady),
    }

    let port_raw = fields
        .get("port")
        .and_then(Value::as_u64)
        .ok_or_else(|| ReadyLineError::Invalid("missing integer port".to_string()))?;
    let port = u16::try_from(port_raw)
        .map_err(|_| ReadyLineError::Invalid(format!("port {port_raw} out of u16 range")))?;
    if port == 0 {
        return Err(ReadyLineError::Invalid(
            "port 0 is not a valid listener port".to_string(),
        ));
    }

    let version = fields
        .get("version")
        .and_then(Value::as_str)
        .filter(|v| !v.is_empty())
        .ok_or_else(|| ReadyLineError::Invalid("missing version string".to_string()))?;

    Ok(ReadyInfo {
        port,
        version: version.to_string(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_frozen_contract_line() {
        let info =
            parse_ready_line(r#"{"status":"ready","port":48123,"version":"1.0.0"}"#).unwrap();
        assert_eq!(
            info,
            ReadyInfo {
                port: 48123,
                version: "1.0.0".to_string()
            }
        );
    }

    #[test]
    fn tolerates_whitespace_and_extra_fields() {
        let info = parse_ready_line(
            r#"  { "status" : "ready" , "port" : 80 , "version" : "1.0.0", "extra": 1 }  "#,
        )
        .unwrap();
        assert_eq!(info.port, 80);
        assert_eq!(info.version, "1.0.0");
    }

    #[test]
    fn non_json_line_is_not_ready() {
        assert_eq!(
            parse_ready_line("hello world"),
            Err(ReadyLineError::NotReady)
        );
    }

    #[test]
    fn json_without_ready_status_is_not_ready() {
        assert_eq!(
            parse_ready_line(r#"{"status":"stopping"}"#),
            Err(ReadyLineError::NotReady)
        );
        assert_eq!(
            parse_ready_line(r#"{"level":"info","msg":"listening"}"#),
            Err(ReadyLineError::NotReady)
        );
    }

    #[test]
    fn non_object_json_is_not_ready() {
        assert_eq!(parse_ready_line("[1,2,3]"), Err(ReadyLineError::NotReady));
        assert_eq!(
            parse_ready_line(r#""ready""#),
            Err(ReadyLineError::NotReady)
        );
    }

    #[test]
    fn ready_line_without_port_is_invalid() {
        assert_eq!(
            parse_ready_line(r#"{"status":"ready","version":"1.0.0"}"#),
            Err(ReadyLineError::Invalid("missing integer port".to_string()))
        );
    }

    #[test]
    fn ready_line_with_zero_port_is_invalid() {
        assert_eq!(
            parse_ready_line(r#"{"status":"ready","port":0,"version":"1.0.0"}"#),
            Err(ReadyLineError::Invalid(
                "port 0 is not a valid listener port".to_string()
            ))
        );
    }

    #[test]
    fn ready_line_with_out_of_range_port_is_invalid() {
        assert_eq!(
            parse_ready_line(r#"{"status":"ready","port":70000,"version":"1.0.0"}"#),
            Err(ReadyLineError::Invalid(
                "port 70000 out of u16 range".to_string()
            ))
        );
        assert_eq!(
            parse_ready_line(r#"{"status":"ready","port":-1,"version":"1.0.0"}"#),
            Err(ReadyLineError::Invalid("missing integer port".to_string()))
        );
    }

    #[test]
    fn ready_line_with_string_port_is_invalid() {
        assert_eq!(
            parse_ready_line(r#"{"status":"ready","port":"48123","version":"1.0.0"}"#),
            Err(ReadyLineError::Invalid("missing integer port".to_string()))
        );
    }

    #[test]
    fn ready_line_without_version_is_invalid() {
        assert_eq!(
            parse_ready_line(r#"{"status":"ready","port":80}"#),
            Err(ReadyLineError::Invalid(
                "missing version string".to_string()
            ))
        );
        assert_eq!(
            parse_ready_line(r#"{"status":"ready","port":80,"version":""}"#),
            Err(ReadyLineError::Invalid(
                "missing version string".to_string()
            ))
        );
    }
}
