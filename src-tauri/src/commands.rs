//! Tauri IPC commands exposed to the WebView (backend wiring + hotkeys).

use std::fmt;

use serde::{Deserialize, Serialize};
use tauri::State;

use crate::hotkey::ApplyHotkeysResult;
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

/// Request body of `apply_hotkeys` (frozen contract): both bindings in the
/// frozen `"Ctrl+Alt+Space"` string format.
#[derive(Debug, Clone, Deserialize)]
pub struct HotkeyInput {
    #[serde(alias = "showHide")]
    pub show_hide: String,
    #[serde(alias = "translateSelection")]
    pub translate_selection: String,
}

/// Applies a new pair of global hotkey bindings.
///
/// Frozen contract: the frontend invokes this at boot and after settings
/// edits; Rust is the registration authority and never reads Go settings.
/// Registration follows register-first/rollback-on-failure semantics, so the
/// previous bindings keep working whenever the new ones cannot be registered
/// (the offending string is returned as `conflict`). Until the first call no
/// hotkeys are registered at all.
#[tauri::command]
pub fn apply_hotkeys(
    hotkeys: HotkeyInput,
    state: State<'_, AppState>,
) -> Result<ApplyHotkeysResult, String> {
    Ok(state
        .hotkeys
        .apply(&hotkeys.show_hide, &hotkeys.translate_selection))
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

    #[test]
    fn hotkey_input_accepts_frozen_field_names() {
        let input: HotkeyInput = serde_json::from_str(
            r#"{"show_hide":"Ctrl+Alt+Space","translate_selection":"Ctrl+Alt+Q"}"#,
        )
        .unwrap();
        assert_eq!(input.show_hide, "Ctrl+Alt+Space");
        assert_eq!(input.translate_selection, "Ctrl+Alt+Q");
    }

    #[test]
    fn hotkey_input_tolerates_camel_case_aliases() {
        let input: HotkeyInput = serde_json::from_str(
            r#"{"showHide":"Ctrl+Alt+Space","translateSelection":"Ctrl+Alt+Q"}"#,
        )
        .unwrap();
        assert_eq!(input.show_hide, "Ctrl+Alt+Space");
        assert_eq!(input.translate_selection, "Ctrl+Alt+Q");
    }
}
