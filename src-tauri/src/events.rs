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

/// Reserved for Phase 3 selection capture.
///
/// Payload will be `{ "text": string }` (docs/07 §5).
#[allow(dead_code)]
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
