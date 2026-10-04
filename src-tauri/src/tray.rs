//! System tray (docs/07 §2): 显示/隐藏, 设置, 退出.
//!
//! * Left click → show/focus the main window (no menu on left click).
//! * 显示/隐藏 → toggle main window visibility + focus.
//! * 设置 → show window and emit `open-settings` for the frontend.
//! * 退出 → real quit: `RunEvent::Exit` terminates the sidecar, then the
//!   process exits with code 0.

use tauri::menu::{MenuBuilder, MenuItemBuilder};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter};

use crate::events;
use crate::{log_line, show_main_window, toggle_main_window};

pub const TRAY_ID: &str = "floattranslate-main-tray";
const TRAY_TOOLTIP: &str = "FloatTranslate";
const MENU_SHOW_HIDE: &str = "show_hide";
const MENU_SETTINGS: &str = "settings";
const MENU_QUIT: &str = "quit";

pub fn create_tray(app: &AppHandle) -> tauri::Result<()> {
    let show_hide = MenuItemBuilder::with_id(MENU_SHOW_HIDE, "显示/隐藏").build(app)?;
    let settings = MenuItemBuilder::with_id(MENU_SETTINGS, "设置").build(app)?;
    let quit = MenuItemBuilder::with_id(MENU_QUIT, "退出").build(app)?;

    let menu = MenuBuilder::new(app)
        .item(&show_hide)
        .item(&settings)
        .separator()
        .item(&quit)
        .build()?;

    // Phase 1 placeholder icon (src-tauri/icons/icon.ico); the real brand
    // icon arrives with packaging in Phase 6.
    let icon = tauri::image::Image::from_bytes(include_bytes!("../icons/icon.ico"))?;

    TrayIconBuilder::with_id(TRAY_ID)
        .icon(icon)
        .tooltip(TRAY_TOOLTIP)
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| handle_menu_event(app, event.id().as_ref()))
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main_window(tray.app_handle());
            }
        })
        .build(app)?;
    Ok(())
}

fn handle_menu_event(app: &AppHandle, id: &str) {
    match id {
        MENU_SHOW_HIDE => toggle_main_window(app),
        MENU_SETTINGS => {
            show_main_window(app);
            if let Err(err) = app.emit(events::OPEN_SETTINGS, ()) {
                log_line(&format!("failed to emit {}: {err}", events::OPEN_SETTINGS));
            }
        }
        MENU_QUIT => {
            // Real quit. The RunEvent::Exit handler in lib.rs terminates the
            // sidecar and persists window state; then the process exits 0.
            log_line("quit requested from tray");
            app.exit(0);
        }
        _ => {}
    }
}
