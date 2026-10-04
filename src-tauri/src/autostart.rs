//! OS login autostart (docs/07 §9) via `tauri-plugin-autostart`.
//!
//! Settings switch, default OFF: nothing is registered until the frontend
//! invokes the `set_autostart` command with an explicit user opt-in. The
//! plugin targets the CURRENT executable path (on Windows: the
//! `HKCU\...\CurrentVersion\Run` value named after the app); un-registering
//! is idempotent (a missing entry is treated as success).
//!
//! Generic over [`tauri::Runtime`] and public so the opt-in E2E integration
//! test (`tests/autostart_e2e.rs`) can drive the same code paths against a
//! mock runtime instead of opening a real window. Kept out of the unit-test
//! binary deliberately: instantiating Tauri's app machinery drags the native
//! GUI stack into the test executable, whose comctl32 v6 imports need the
//! shipped app manifest and make the plain test binary fail to load.

use tauri::{AppHandle, Runtime};
use tauri_plugin_autostart::ManagerExt;

use crate::log_line;

/// Registers or unregisters login autostart for the current executable and
/// returns the resulting registration state.
///
/// The returned bool is read back through the plugin manager
/// (`is_enabled`), so it reflects the OS truth rather than the request.
/// Both the write and the read errors are surfaced as `Err(message)`.
pub fn set_enabled<R: Runtime>(app: &AppHandle<R>, enabled: bool) -> Result<bool, String> {
    let manager = app.autolaunch();
    if enabled {
        manager
            .enable()
            .map_err(|err| format!("failed to enable autostart: {err}"))?;
    } else {
        manager
            .disable()
            .map_err(|err| format!("failed to disable autostart: {err}"))?;
    }
    let state = manager
        .is_enabled()
        .map_err(|err| format!("failed to read autostart state: {err}"))?;
    log_line(&format!(
        "autostart {}: {state}",
        if enabled { "enable" } else { "disable" }
    ));
    Ok(state)
}

/// Returns the current login-autostart registration state.
pub fn is_enabled<R: Runtime>(app: &AppHandle<R>) -> Result<bool, String> {
    app.autolaunch()
        .is_enabled()
        .map_err(|err| format!("failed to read autostart state: {err}"))
}
