//! Opt-in E2E tests for the OS autostart toggle (`FT_AUTOSTART_E2E=1`).
//!
//! Lives in `tests/` (not in the unit-test binary) on purpose: the mock
//! runtime pulls enough of Tauri's app machinery into the executable that the
//! native GUI stack gets linked, whose comctl32 v6 imports require the
//! embedded application manifest only the shipped binary carries. A lean
//! integration test binary links only the referenced chain and loads fine.
//!
//! These tests perform a REAL registration for the *test binary* under a
//! dedicated name and clean up after themselves, so a plain `cargo test`
//! never touches the user's autostart choice.

#![cfg(windows)]

use floattranslate_lib::autostart::{is_enabled, set_enabled};

/// Builds a mock-runtime app hosting the plugin under a DEDICATED
/// registration name, so the test can never clobber an unrelated login
/// entry (e.g. a registry `Run` value named after the real app).
fn mock_app_with_autostart() -> tauri::App<tauri::test::MockRuntime> {
    tauri::test::mock_builder()
        .plugin(
            tauri_plugin_autostart::Builder::new()
                .app_name("FloatTranslate-E2E-Autostart")
                .build(),
        )
        .build(tauri::test::mock_context(tauri::test::noop_assets()))
        .expect("mock app with autostart plugin")
}

fn gate_open() -> bool {
    std::env::var("FT_AUTOSTART_E2E").ok().as_deref() == Some("1")
}

#[test]
fn autostart_enable_disable_roundtrip_e2e() {
    if !gate_open() {
        eprintln!("skipping: set FT_AUTOSTART_E2E=1 to run the real autostart registration test");
        return;
    }

    let app = mock_app_with_autostart();
    let handle = app.handle().clone();

    // Register, observe the enabled state, then unregister again.
    assert!(
        set_enabled(&handle, true).expect("enable autostart"),
        "set_enabled(true) must read back as enabled"
    );
    assert!(
        is_enabled(&handle).expect("read state"),
        "get_autostart must report the registration"
    );

    assert!(
        !set_enabled(&handle, false).expect("disable autostart"),
        "set_enabled(false) must read back as disabled"
    );
    assert!(
        !is_enabled(&handle).expect("read state"),
        "get_autostart must report the removal"
    );
}

/// Un-registering when nothing is registered must stay a harmless no-op
/// (frozen plugin behavior `set_autostart(false)` relies on).
#[test]
fn autostart_disable_is_idempotent_e2e() {
    if !gate_open() {
        eprintln!("skipping: set FT_AUTOSTART_E2E=1 to run the real autostart registration test");
        return;
    }

    let app = mock_app_with_autostart();
    let handle = app.handle().clone();

    // Disable twice: the second call must also be Ok (missing entry).
    set_enabled(&handle, false).expect("first disable");
    set_enabled(&handle, false).expect("second disable");
    assert!(!is_enabled(&handle).expect("read state"));
}
