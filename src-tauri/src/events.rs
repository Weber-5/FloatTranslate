//! Central registry of Tauri event names emitted by the host.
//!
//! The frontend consumes these; names are part of the frozen contract and
//! must not be renamed casually.

/// Sidecar lifecycle changed.
///
/// Payload: `{ "status": "starting" | "ready" | "restarting" | "failed",
/// "message": string | null }`
pub const BACKEND_STATUS: &str = "backend-status";

/// Sidecar gave up after exhausting its restart budget.
///
/// Payload: `{ "message": string }`
pub const BACKEND_FAILED: &str = "backend-failed";

/// Tray "设置" menu item was activated: show the main window and navigate to
/// settings. No payload.
pub const OPEN_SETTINGS: &str = "open-settings";

/// Selection capture completed (Phase 3, docs/07 §5).
///
/// Emitted to the main window only after a successful capture, and only after
/// the window has been woken (shown + focused).
///
/// Payload: `{ "text": string }`
pub const SELECTION_CAPTURED: &str = "selection-captured";

/// Payload for [`BACKEND_STATUS`].
#[derive(Debug, Clone, serde::Serialize)]
pub struct BackendStatusPayload {
    pub status: &'static str,
    pub message: Option<String>,
}

/// Payload for [`BACKEND_FAILED`].
#[derive(Debug, Clone, serde::Serialize)]
pub struct BackendFailedPayload {
    pub message: String,
}

/// Payload for [`SELECTION_CAPTURED`] (frozen contract: `{ "text": string }`).
#[derive(Debug, Clone, serde::Serialize)]
pub struct SelectionCapturedPayload {
    pub text: String,
}
