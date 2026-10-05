//! FloatTranslate — Windows host (Rust + Tauri 2), Phase 1 skeleton.
//!
//! Responsibilities (docs/02 §2): frameless window, tray, single instance and
//! Go sidecar supervision. Business logic (translation/chat/SQLite/LLM) lives
//! in the Go backend and must never be implemented here.
//!
//! Startup sequence (docs/02 §5):
//! 1. single-instance plugin acquires the lock (2nd launch just refocuses),
//! 2. data root is resolved (installed vs portable, docs/07 §8),
//! 3. a per-run session token is generated,
//! 4. the sidecar supervisor thread spawns the Go backend and waits for READY,
//! 5. the main window geometry is restored from `<data_root>/window-state.json`.

// These modules are public so integration tests in `tests/` can exercise the
// sidecar handshake, data-root resolution, geometry persistence, hotkey
// parsing and the capture state machine directly.
// These modules are public so integration tests in `tests/` can exercise the
// sidecar handshake, data-root resolution, geometry persistence, hotkey
// parsing, the capture state machine and the autostart toggle directly.
pub mod autostart;
pub mod data_root;
pub mod hotkey;
pub mod selection;
pub mod sidecar;
pub mod token;
pub mod window_state;

mod commands;
mod events;
mod tray;

use std::io::Write;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use tauri::{AppHandle, Manager, RunEvent, WebviewWindow, WindowEvent};

use crate::sidecar::Supervisor;
use crate::window_state::WindowState;

/// Shared application state managed by Tauri.
pub struct AppState {
    /// Resolved data root (docs/07 §8).
    pub data_root: PathBuf,
    /// Per-run session token shared with the sidecar. Never log it.
    pub token: String,
    /// Sidecar supervisor.
    pub backend: Arc<Supervisor>,
    /// Current always-on-top preference (default true). Toggled at runtime by
    /// the `set_always_on_top` command and persisted with the window state.
    pub always_on_top: AtomicBool,
    /// Global hotkey registrations (Phase 3). Rust is the registration
    /// authority; nothing is registered until the frontend invokes the
    /// `apply_hotkeys` command.
    pub hotkeys: hotkey::HotkeyManager,
}

/// Runs the Tauri application. Only returns on fatal startup errors or after
/// a clean exit (tray 退出 / OS shutdown).
pub fn run() {
    let builder = tauri::Builder::default()
        // Must be the first plugin: second launches forward here and exit.
        .plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            log_line("second instance detected; focusing existing window");
            show_main_window(app);
        }))
        // Global hotkeys (docs/07 §4). One plugin-level handler dispatches by
        // comparing against the currently applied bindings; nothing is
        // registered until the frontend invokes `apply_hotkeys`.
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(|app, shortcut, event| {
                    hotkey::handle_shortcut_event(app, shortcut, event);
                })
                .build(),
        )
        // Native message/file dialogs (Phase 4 markdown export). The plugin
        // dispatches to the main thread itself, so commands may call its
        // blocking APIs from the async runtime.
        .plugin(tauri_plugin_dialog::init())
        // OS login autostart (docs/07 §9, Phase 5). Registered with no extra
        // launch args; the plugin targets the CURRENT executable path.
        // Nothing is written to the OS until the frontend invokes
        // `set_autostart` (settings switch, default OFF).
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            None,
        ))
        .setup(setup_app)
        .on_window_event(|window, event| {
            if window.label() != "main" {
                return;
            }
            if let WindowEvent::CloseRequested { api, .. } = event {
                // docs/07 §1: close → hide to tray, never exit the app.
                api.prevent_close();
                let handle = window.app_handle();
                if let Some(state) = handle.try_state::<AppState>() {
                    if let Some(webview_window) = handle.get_webview_window(window.label()) {
                        persist_window_state(&webview_window, &state);
                    }
                }
                let _ = window.hide();
            }
        })
        .invoke_handler(tauri::generate_handler![
            commands::get_backend_config,
            commands::get_backend_status,
            commands::apply_hotkeys,
            commands::export_markdown,
            commands::pick_save_path,
            commands::pick_open_path,
            commands::set_autostart,
            commands::get_autostart,
            commands::set_always_on_top,
            commands::reset_window_state,
            commands::open_logs_dir,
            commands::minimize_window,
            commands::hide_to_tray
        ]);

    let app = match builder.build(tauri::generate_context!()) {
        Ok(app) => app,
        Err(err) => {
            log_line(&format!(
                "failed to build FloatTranslate application: {err}"
            ));
            std::process::exit(1);
        }
    };

    app.run(|app_handle, event| {
        if let RunEvent::Exit = event {
            on_app_exit(app_handle);
        }
    });
}

fn setup_app(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let handle = app.handle().clone();

    // --- data root (docs/07 §8) -----------------------------------------
    let exe = std::env::current_exe()?;
    let exe_dir = exe
        .parent()
        .ok_or("failed to locate the executable directory")?
        .to_path_buf();

    let portable_flag = exe_dir.join(data_root::PORTABLE_FLAG_NAME).is_file();
    let dev_portable =
        cfg!(debug_assertions) && std::env::var(data_root::ENV_PORTABLE).is_ok_and(|v| v == "1");
    let resolved = data_root::resolve_data_root(
        &exe_dir,
        portable_flag,
        dev_portable,
        dirs::data_dir().as_deref(),
    );
    let data_root = data_root::ensure_dir(&resolved)?;
    let mode = if portable_flag || dev_portable {
        "portable"
    } else {
        "installed"
    };
    log_line(&format!("mode: {mode}; data root: {}", data_root.display()));

    // --- session token ----------------------------------------------------
    let token = token::generate_session_token();

    // --- window geometry (frozen contract: our own file) -------------------
    let window = handle
        .get_webview_window("main")
        .ok_or("main window is missing from the runtime")?;
    let state_path = data_root.join(window_state::WINDOW_STATE_FILENAME);
    let restored = window_state::load(&state_path);
    match &restored {
        Some(state) => log_line(&format!("restored window state: {state}")),
        None => log_line("window state: using defaults"),
    }
    apply_window_state(&window, restored.as_ref());
    let always_on_top = restored.as_ref().map(|s| s.always_on_top).unwrap_or(true);

    // --- sidecar (docs/07 §7) ---------------------------------------------
    let backend_exe = sidecar::resolve_backend_exe(
        &exe_dir,
        std::env::var(sidecar::ENV_BACKEND_EXE).ok().as_deref(),
    );
    match &backend_exe {
        Ok(path) => log_line(&format!("backend exe: {}", path.display())),
        Err(err) => log_line(&format!("backend exe resolution: {err}")),
    }
    let supervisor = Supervisor::start(
        handle.clone(),
        backend_exe,
        token.clone(),
        data_root.clone(),
    );

    app.manage(AppState {
        data_root,
        token,
        backend: supervisor,
        always_on_top: AtomicBool::new(always_on_top),
        hotkeys: hotkey::HotkeyManager::with_app_handle(handle.clone()),
    });

    // --- runtime window icon (improvement bug #3) --------------------------
    // The exe resource usually provides the window/taskbar icon, but a stale
    // shell icon cache or a failed resource lookup leaves a blank tile in the
    // taskbar. Setting the icon explicitly on the window removes that class
    // of failure.
    match tauri::image::Image::from_bytes(include_bytes!("../icons/icon.ico")) {
        Ok(icon) => {
            if let Err(err) = window.set_icon(icon) {
                log_line(&format!("failed to set window icon: {err}"));
            }
        }
        Err(err) => log_line(&format!("failed to decode window icon: {err}")),
    }

    // --- hotkey persistence (improvement bug #5) ----------------------------
    // Hotkeys used to exist only after the frontend booted and applied the
    // settings, so presses in the first seconds did nothing. The last applied
    // bindings are persisted in the data root and re-applied here, before the
    // webview loads; the frontend remains the runtime authority.
    {
        let state = handle.state::<AppState>();
        let hotkeys_path = state.data_root.join(hotkey::PERSISTENCE_FILENAME);
        if let Some(saved) = hotkey::load_persisted(&hotkeys_path) {
            let result = state
                .hotkeys
                .apply(&saved.show_hide, &saved.translate_selection);
            if result.registered {
                log_line(&format!(
                    "hotkeys restored from disk: show_hide={}, translate_selection={}",
                    saved.show_hide, saved.translate_selection
                ));
            } else {
                log_line(&format!(
                    "persisted hotkeys failed to apply (conflict: {:?}); \
                     waiting for the frontend to re-apply",
                    result.conflict
                ));
            }
        }
    }

    tray::create_tray(&handle)?;

    Ok(())
}

/// Restores geometry from a persisted state (with monitor clamping), or the
/// frozen defaults. Best-effort: every setter failure is tolerated.
fn apply_window_state(window: &WebviewWindow, stored: Option<&WindowState>) {
    let monitor = window_monitor_bounds(window);
    let target = match stored {
        Some(state) => window_state::clamp_to_monitor(
            state,
            monitor,
            window_state::MIN_WIDTH,
            window_state::MIN_HEIGHT,
        ),
        None => WindowState::default_geometry(),
    };

    let _ = window.set_size(tauri::PhysicalSize::new(
        target.width.round() as u32,
        target.height.round() as u32,
    ));
    if let (Some(x), Some(y)) = (target.x, target.y) {
        let _ = window.set_position(tauri::PhysicalPosition::new(
            x.round() as i32,
            y.round() as i32,
        ));
    }
    let _ = window.set_always_on_top(target.always_on_top);
    if target.maximized {
        let _ = window.maximize();
    }
}

/// Physical-pixel bounds of the window's current monitor, falling back to the
/// primary monitor. `None` when the platform cannot answer (position restore
/// then degrades to the OS default).
fn window_monitor_bounds(window: &WebviewWindow) -> Option<window_state::MonitorBounds> {
    let monitor = window
        .current_monitor()
        .ok()
        .flatten()
        .or_else(|| window.primary_monitor().ok().flatten())?;
    let position = monitor.position();
    let size = monitor.size();
    Some(window_state::MonitorBounds {
        x: position.x as f64,
        y: position.y as f64,
        width: size.width as f64,
        height: size.height as f64,
    })
}

/// Captures the live geometry of the main window.
fn capture_window_state(window: &WebviewWindow, always_on_top: bool) -> WindowState {
    let size = window
        .inner_size()
        .map(|s| (s.width as f64, s.height as f64))
        .unwrap_or((window_state::DEFAULT_WIDTH, window_state::DEFAULT_HEIGHT));
    let position = window.outer_position().ok();
    let maximized = window.is_maximized().unwrap_or(false);
    WindowState {
        width: size.0,
        height: size.1,
        x: position.as_ref().map(|p| p.x as f64),
        y: position.as_ref().map(|p| p.y as f64),
        maximized,
        always_on_top,
    }
}

/// Writes `<data_root>/window-state.json`; failures are logged, never fatal
/// (a lost geometry file must not take the app down).
fn persist_window_state(window: &WebviewWindow, state: &AppState) {
    let snapshot = capture_window_state(window, state.always_on_top.load(Ordering::SeqCst));
    let path = state.data_root.join(window_state::WINDOW_STATE_FILENAME);
    if let Err(err) = window_state::save(&path, &snapshot) {
        log_line(&format!("failed to save window state: {err}"));
    }
}

/// Final cleanup on `RunEvent::Exit`: unregister hotkeys, persist geometry,
/// terminate the sidecar child, then let the process exit (code 0 for
/// tray-initiated quits).
fn on_app_exit(app_handle: &AppHandle) {
    let Some(state) = app_handle.try_state::<AppState>() else {
        return;
    };
    state.hotkeys.unregister_all();
    log_line("global hotkeys unregistered");
    if let Some(window) = app_handle.get_webview_window("main") {
        persist_window_state(&window, &state);
    }
    state.backend.shutdown();
    log_line("sidecar stopped; FloatTranslate exit complete");
}

/// Shows, un-minimizes and focuses the main window (tray left click /
/// 显示, single-instance wakeup).
pub(crate) fn show_main_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

/// Tray 显示/隐藏: hide when visible, otherwise show + focus.
pub(crate) fn toggle_main_window(app: &AppHandle) {
    let Some(window) = app.get_webview_window("main") else {
        return;
    };
    if window.is_visible().unwrap_or(false) {
        let _ = window.hide();
    } else {
        show_main_window(app);
    }
}

/// Show/hide hotkey toggle (frozen contract): hide only when the window is
/// BOTH visible and focused; otherwise show + focus (un-minimize if needed).
/// A visible but unfocused window is brought to the front instead of hidden.
pub(crate) fn toggle_main_window_for_hotkey(app: &AppHandle) {
    let Some(window) = app.get_webview_window("main") else {
        return;
    };
    let visible = window.is_visible().unwrap_or(false);
    let focused = window.is_focused().unwrap_or(false);
    if visible && focused {
        let _ = window.hide();
    } else {
        show_main_window(app);
    }
}

/// Lifecycle/diagnostic logging to stdout.
///
/// Hot-path safe: write errors (e.g. no console in release GUI builds) are
/// ignored. Callers must never pass secrets (docs/08 §4).
pub(crate) fn log_line(message: &str) {
    let _ = writeln!(std::io::stdout(), "[floattranslate] {message}");
}
