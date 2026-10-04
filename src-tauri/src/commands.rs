//! Tauri IPC commands exposed to the WebView (Phase 1: backend wiring only).

use std::fmt;

use serde::Serialize;
use tauri::State;

use crate::sidecar::BackendStatus;
use crate::AppState;

/// Payload of `get_backend_config` (frozen contract):
/// `{ "base_url": "http://127.0.0.1:<port>/api/v1", "token": ..., "data_root": ... }`
#[derive(Debug, Clone, Serialize)]
pub struct BackendConfig {
    pub base_url: String,
    pub token: String,
    pub data_root: String,
}

/// Serializable command error (Tauri requires `Serialize` on the `Err` type).
#[derive(Debug)]
pub struct CommandError(String);

impl CommandError {
    fn new(message: impl Into<String>) -> Self {
        Self(message.into())
    }
}

impl fmt::Display for CommandError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for CommandError {}

impl Serialize for CommandError {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_str(&self.0)
    }
}

/// Returns the loopback base URL plus the session token for the Go sidecar.
///
/// Errors while the sidecar is not ready; the frontend should react to the
/// `backend-status` event (see [`crate::events`]) instead of polling this
/// command. The token is per-run and must never be logged (docs/08 §4).
#[tauri::command]
pub fn get_backend_config(state: State<'_, AppState>) -> Result<BackendConfig, CommandError> {
    let snapshot = state.backend.snapshot();
    if snapshot.status != BackendStatus::Ready {
        return Err(CommandError::new(match snapshot.message {
            Some(message) => {
                format!(
                    "backend not ready: {} ({message})",
                    snapshot.status.as_str()
                )
            }
            None => format!("backend not ready: {}", snapshot.status.as_str()),
        }));
    }
    let Some(port) = snapshot.port else {
        return Err(CommandError::new(
            "backend is ready but the port is unknown",
        ));
    };
    Ok(BackendConfig {
        base_url: format!("http://127.0.0.1:{port}/api/v1"),
        token: state.token.clone(),
        data_root: state.data_root.to_string_lossy().into_owned(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn command_error_serializes_as_string() {
        let err = CommandError::new("boom");
        let json = serde_json::to_string(&err).unwrap();
        assert_eq!(json, r#""boom""#);
    }

    #[test]
    fn command_error_display_matches_inner() {
        let err = CommandError::new("backend not ready: starting");
        assert_eq!(err.to_string(), "backend not ready: starting");
    }
}
