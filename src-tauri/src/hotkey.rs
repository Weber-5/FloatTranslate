//! Global hotkeys (docs/00 §3, docs/07 §4).
//!
//! Frozen cross-agent contract (Phase 3):
//!
//! * Tauri command `apply_hotkeys(hotkeys: {show_hide, translate_selection})`
//!   → `Result<{registered, conflict}, String>` ([`ApplyHotkeysResult`]).
//!   The frontend invokes it at boot and after settings edits; Rust never
//!   reads Go settings, and until the first invocation NO hotkeys are
//!   registered (avoids conflicts from stale configuration).
//! * String format: `"Ctrl+Alt+Space"` style. Modifiers `Ctrl/Alt/Shift/Win`
//!   (case-insensitive); keys A-Z, 0-9, F1-F24, Space, Enter, Tab, Backspace,
//!   arrows (Up/Down/Left/Right), Home/End/PageUp/PageDown and common
//!   punctuation ``- = [ ] ; ' , . / \``. An invalid string yields
//!   `registered=false` with the offending string as `conflict`. At least one
//!   modifier is required (a bare key must never become a global hotkey — it
//!   would swallow that key system-wide).
//! * Semantics: parse + validate both bindings, register the new ones FIRST
//!   and only then free the replaced ones. If either registration fails,
//!   whatever was just registered is unregistered again and the previous
//!   registrations stay intact; the command returns `registered=false` plus
//!   the offending string. Identical bindings for both roles are rejected.
//! * Registration authority is Rust (`tauri-plugin-global-shortcut`); the
//!   plugin-level event handler dispatches via [`HotkeyManager::action_for`]:
//!   show/hide toggles the main window, translate-selection starts the
//!   selection capture ([`crate::selection::start_capture`]).
//!
//! Registrations are torn down on app exit through
//! [`HotkeyManager::unregister_all`] (the plugin would clean up anyway; we do
//! it explicitly to be safe).

use std::fmt;
use std::path::Path;
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager};
use tauri_plugin_global_shortcut::{
    Code, Modifiers as ShortcutModifiers, Shortcut, ShortcutEvent, ShortcutState,
};

use crate::log_line;

/// A parsed, validated hotkey binding.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Hotkey {
    pub ctrl: bool,
    pub alt: bool,
    pub shift: bool,
    pub win: bool,
    pub key: Key,
}

/// Non-modifier key of a hotkey binding.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Key {
    /// `A`..=`Z` (normalized to uppercase).
    Letter(char),
    /// `0`..=`9`.
    Digit(u8),
    /// `F1`..=`F24`.
    Function(u8),
    Space,
    Enter,
    Tab,
    Backspace,
    ArrowUp,
    ArrowDown,
    ArrowLeft,
    ArrowRight,
    Home,
    End,
    PageUp,
    PageDown,
    Minus,
    Equal,
    BracketLeft,
    BracketRight,
    Semicolon,
    Quote,
    Comma,
    Period,
    Slash,
    Backslash,
}

/// Parse failure. The message is for logs; the `apply_hotkeys` command
/// surfaces the original string as the conflict.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HotkeyParseError(String);

impl fmt::Display for HotkeyParseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for HotkeyParseError {}

/// Parses a `"Ctrl+Alt+Space"` style hotkey string (frozen format).
///
/// Tokens are separated by `+`; whitespace around tokens is tolerated.
/// Modifiers (case-insensitive): `Ctrl`/`Control`, `Alt`, `Shift`,
/// `Win`/`Windows`/`Super`. Exactly one non-modifier key is required and at
/// least one modifier.
pub fn parse_hotkey(input: &str) -> Result<Hotkey, HotkeyParseError> {
    let mut ctrl = false;
    let mut alt = false;
    let mut shift = false;
    let mut win = false;
    let mut key: Option<Key> = None;

    for token in input.split('+') {
        let token = token.trim();
        if token.is_empty() {
            return Err(HotkeyParseError(format!(
                "empty token in hotkey string {input:?}"
            )));
        }
        match token.to_ascii_lowercase().as_str() {
            "ctrl" | "control" => ctrl = true,
            "alt" => alt = true,
            "shift" => shift = true,
            "win" | "windows" | "super" => win = true,
            _ => {
                if key.is_some() {
                    return Err(HotkeyParseError(format!(
                        "multiple keys in hotkey string {input:?}"
                    )));
                }
                key = Some(parse_key_token(token).ok_or_else(|| {
                    HotkeyParseError(format!("unknown hotkey token {token:?} in {input:?}"))
                })?);
            }
        }
    }

    let Some(key) = key else {
        return Err(HotkeyParseError(format!(
            "missing key in hotkey string {input:?}"
        )));
    };
    if !(ctrl || alt || shift || win) {
        return Err(HotkeyParseError(format!(
            "missing modifier (need Ctrl/Alt/Shift/Win) in hotkey string {input:?}"
        )));
    }
    Ok(Hotkey {
        ctrl,
        alt,
        shift,
        win,
        key,
    })
}

/// Parses a single non-modifier key token (lowercase already applied by the
/// caller is not required; matching here is case-insensitive).
fn parse_key_token(token: &str) -> Option<Key> {
    let lower = token.to_ascii_lowercase();
    let mut chars = lower.chars();
    let (first, second) = (chars.next(), chars.next());
    if second.is_none() {
        let ch = first?;
        if let Some(punct) = punctuation_key(ch) {
            return Some(punct);
        }
        if ch.is_ascii_digit() {
            return Some(Key::Digit(ch as u8 - b'0'));
        }
        if ch.is_ascii_lowercase() {
            return Some(Key::Letter(ch.to_ascii_uppercase()));
        }
        return None;
    }
    // Multi-character tokens: F-keys and named keys.
    if let Some(digits) = lower.strip_prefix('f') {
        return match digits.parse::<u8>() {
            Ok(n) if (1..=24).contains(&n) => Some(Key::Function(n)),
            _ => None,
        };
    }
    match lower.as_str() {
        "space" => Some(Key::Space),
        "enter" | "return" => Some(Key::Enter),
        "tab" => Some(Key::Tab),
        "backspace" => Some(Key::Backspace),
        "up" | "arrowup" => Some(Key::ArrowUp),
        "down" | "arrowdown" => Some(Key::ArrowDown),
        "left" | "arrowleft" => Some(Key::ArrowLeft),
        "right" | "arrowright" => Some(Key::ArrowRight),
        "home" => Some(Key::Home),
        "end" => Some(Key::End),
        "pageup" => Some(Key::PageUp),
        "pagedown" => Some(Key::PageDown),
        _ => None,
    }
}

fn punctuation_key(ch: char) -> Option<Key> {
    Some(match ch {
        '-' => Key::Minus,
        '=' => Key::Equal,
        '[' => Key::BracketLeft,
        ']' => Key::BracketRight,
        ';' => Key::Semicolon,
        '\'' => Key::Quote,
        ',' => Key::Comma,
        '.' => Key::Period,
        '/' => Key::Slash,
        '\\' => Key::Backslash,
        _ => return None,
    })
}

impl Hotkey {
    /// Converts to the plugin's `Shortcut` (global-hotkey `HotKey`).
    pub fn to_shortcut(self) -> Shortcut {
        let mut mods = ShortcutModifiers::empty();
        if self.ctrl {
            mods |= ShortcutModifiers::CONTROL;
        }
        if self.alt {
            mods |= ShortcutModifiers::ALT;
        }
        if self.shift {
            mods |= ShortcutModifiers::SHIFT;
        }
        if self.win {
            // On Windows `SUPER` maps to MOD_WIN.
            mods |= ShortcutModifiers::SUPER;
        }
        Shortcut::new(
            if mods.is_empty() { None } else { Some(mods) },
            self.key.code(),
        )
    }
}

impl Key {
    /// Canonical display name (used by [`Hotkey`]'s `Display`).
    fn name(self) -> String {
        match self {
            Self::Letter(c) => c.to_string(),
            Self::Digit(d) => d.to_string(),
            Self::Function(n) => format!("F{n}"),
            Self::Space => "Space".to_string(),
            Self::Enter => "Enter".to_string(),
            Self::Tab => "Tab".to_string(),
            Self::Backspace => "Backspace".to_string(),
            Self::ArrowUp => "Up".to_string(),
            Self::ArrowDown => "Down".to_string(),
            Self::ArrowLeft => "Left".to_string(),
            Self::ArrowRight => "Right".to_string(),
            Self::Home => "Home".to_string(),
            Self::End => "End".to_string(),
            Self::PageUp => "PageUp".to_string(),
            Self::PageDown => "PageDown".to_string(),
            Self::Minus => "-".to_string(),
            Self::Equal => "=".to_string(),
            Self::BracketLeft => "[".to_string(),
            Self::BracketRight => "]".to_string(),
            Self::Semicolon => ";".to_string(),
            Self::Quote => "'".to_string(),
            Self::Comma => ",".to_string(),
            Self::Period => ".".to_string(),
            Self::Slash => "/".to_string(),
            Self::Backslash => "\\".to_string(),
        }
    }

    /// The matching `keyboard_types` key code (parser output is always in
    /// range; out-of-range arms are unreachable and map to `Code::NoEvent`).
    fn code(self) -> Code {
        match self {
            Self::Letter(c) => match c {
                'A' => Code::KeyA,
                'B' => Code::KeyB,
                'C' => Code::KeyC,
                'D' => Code::KeyD,
                'E' => Code::KeyE,
                'F' => Code::KeyF,
                'G' => Code::KeyG,
                'H' => Code::KeyH,
                'I' => Code::KeyI,
                'J' => Code::KeyJ,
                'K' => Code::KeyK,
                'L' => Code::KeyL,
                'M' => Code::KeyM,
                'N' => Code::KeyN,
                'O' => Code::KeyO,
                'P' => Code::KeyP,
                'Q' => Code::KeyQ,
                'R' => Code::KeyR,
                'S' => Code::KeyS,
                'T' => Code::KeyT,
                'U' => Code::KeyU,
                'V' => Code::KeyV,
                'W' => Code::KeyW,
                'X' => Code::KeyX,
                'Y' => Code::KeyY,
                'Z' => Code::KeyZ,
                // Parser only produces A-Z here.
                _ => Code::Unidentified,
            },
            Self::Digit(d) => match d {
                0 => Code::Digit0,
                1 => Code::Digit1,
                2 => Code::Digit2,
                3 => Code::Digit3,
                4 => Code::Digit4,
                5 => Code::Digit5,
                6 => Code::Digit6,
                7 => Code::Digit7,
                8 => Code::Digit8,
                9 => Code::Digit9,
                _ => Code::Unidentified,
            },
            Self::Function(n) => match n {
                1 => Code::F1,
                2 => Code::F2,
                3 => Code::F3,
                4 => Code::F4,
                5 => Code::F5,
                6 => Code::F6,
                7 => Code::F7,
                8 => Code::F8,
                9 => Code::F9,
                10 => Code::F10,
                11 => Code::F11,
                12 => Code::F12,
                13 => Code::F13,
                14 => Code::F14,
                15 => Code::F15,
                16 => Code::F16,
                17 => Code::F17,
                18 => Code::F18,
                19 => Code::F19,
                20 => Code::F20,
                21 => Code::F21,
                22 => Code::F22,
                23 => Code::F23,
                24 => Code::F24,
                _ => Code::Unidentified,
            },
            Self::Space => Code::Space,
            Self::Enter => Code::Enter,
            Self::Tab => Code::Tab,
            Self::Backspace => Code::Backspace,
            Self::ArrowUp => Code::ArrowUp,
            Self::ArrowDown => Code::ArrowDown,
            Self::ArrowLeft => Code::ArrowLeft,
            Self::ArrowRight => Code::ArrowRight,
            Self::Home => Code::Home,
            Self::End => Code::End,
            Self::PageUp => Code::PageUp,
            Self::PageDown => Code::PageDown,
            Self::Minus => Code::Minus,
            Self::Equal => Code::Equal,
            Self::BracketLeft => Code::BracketLeft,
            Self::BracketRight => Code::BracketRight,
            Self::Semicolon => Code::Semicolon,
            Self::Quote => Code::Quote,
            Self::Comma => Code::Comma,
            Self::Period => Code::Period,
            Self::Slash => Code::Slash,
            Self::Backslash => Code::Backslash,
        }
    }
}

impl fmt::Display for Hotkey {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let mut parts: Vec<String> = Vec::with_capacity(5);
        if self.ctrl {
            parts.push("Ctrl".to_string());
        }
        if self.alt {
            parts.push("Alt".to_string());
        }
        if self.shift {
            parts.push("Shift".to_string());
        }
        if self.win {
            parts.push("Win".to_string());
        }
        parts.push(self.key.name());
        f.write_str(&parts.join("+"))
    }
}

/// Action the plugin event handler should perform for a pressed hotkey.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum HotkeyAction {
    ShowHide,
    TranslateSelection,
}

/// Registration backend, abstracted so [`HotkeyManager`] logic can be tested
/// without a desktop session.
pub trait Registrar: Send + Sync + 'static {
    fn register(&self, hotkey: &Hotkey) -> Result<(), String>;
    fn unregister(&self, hotkey: &Hotkey) -> Result<(), String>;
}

/// Bookkeeping of the currently applied bindings.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
struct CurrentHotkeys {
    show_hide: Option<Hotkey>,
    translate_selection: Option<Hotkey>,
}

/// Owns the current global hotkey registrations and applies new bindings with
/// register-first / rollback-on-failure semantics. Held in [`crate::AppState`].
pub struct HotkeyManager {
    registrar: Box<dyn Registrar>,
    current: Mutex<CurrentHotkeys>,
}

/// Result payload of the `apply_hotkeys` command (frozen contract).
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct ApplyHotkeysResult {
    pub registered: bool,
    pub conflict: Option<String>,
}

impl HotkeyManager {
    pub fn new(registrar: Box<dyn Registrar>) -> Self {
        Self {
            registrar,
            current: Mutex::new(CurrentHotkeys::default()),
        }
    }

    /// Production registrar backed by the global-shortcut plugin.
    #[cfg(windows)]
    pub fn with_app_handle(app: AppHandle) -> Self {
        Self::new(Box::new(PluginRegistrar { app }))
    }

    /// Non-Windows placeholder: 1.0 is Windows-only.
    #[cfg(not(windows))]
    pub fn with_app_handle(_app: AppHandle) -> Self {
        Self::new(Box::new(UnsupportedRegistrar))
    }

    /// Applies a new pair of bindings (frozen `apply_hotkeys` semantics):
    ///
    /// 1. parse + validate both strings; any invalid string is returned as
    ///    the conflict without touching registrations,
    /// 2. reject identical bindings for both roles,
    /// 3. register the new bindings first (skipping ones already live),
    /// 4. only after both succeeded, free the replaced old bindings,
    /// 5. on any registration failure, unregister what was just registered —
    ///    the previous registrations were never touched, so the old hotkeys
    ///    keep working.
    ///
    /// Logs the applied bindings on success.
    pub fn apply(&self, show_hide: &str, translate_selection: &str) -> ApplyHotkeysResult {
        let parsed_show = match parse_hotkey(show_hide) {
            Ok(hotkey) => hotkey,
            Err(err) => {
                log_line(&format!("invalid show_hide hotkey {show_hide:?}: {err}"));
                return rejected(show_hide);
            }
        };
        let parsed_translate = match parse_hotkey(translate_selection) {
            Ok(hotkey) => hotkey,
            Err(err) => {
                log_line(&format!(
                    "invalid translate_selection hotkey {translate_selection:?}: {err}"
                ));
                return rejected(translate_selection);
            }
        };
        if parsed_show == parsed_translate {
            log_line(&format!(
                "show_hide and translate_selection must differ (both {translate_selection:?})"
            ));
            return rejected(translate_selection);
        }

        let mut current = self
            .current
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let old_show = current.show_hide;
        let old_translate = current.translate_selection;
        let olds: Vec<Hotkey> = [old_show, old_translate].into_iter().flatten().collect();

        // Step 1: register the new bindings first so the old ones stay intact
        // until the new pair is secured.
        let mut registered_now: Vec<Hotkey> = Vec::new();
        for (role, hotkey, raw) in [
            ("show_hide", parsed_show, show_hide),
            ("translate_selection", parsed_translate, translate_selection),
        ] {
            if olds.contains(&hotkey) {
                continue; // already live; its registration serves this role
            }
            match self.registrar.register(&hotkey) {
                Ok(()) => registered_now.push(hotkey),
                Err(err) => {
                    log_line(&format!("failed to register {role} hotkey {hotkey}: {err}"));
                    self.rollback(&registered_now);
                    return rejected(raw);
                }
            }
        }

        // Step 2: free replaced old bindings (best effort; a failed unregister
        // leaves the old hotkey live alongside the new one — logged, harmless).
        for old in &olds {
            if *old == parsed_show || *old == parsed_translate {
                continue;
            }
            if let Err(err) = self.registrar.unregister(old) {
                log_line(&format!(
                    "failed to unregister replaced hotkey {old}: {err}; it stays registered"
                ));
            }
        }

        // Step 3: commit.
        current.show_hide = Some(parsed_show);
        current.translate_selection = Some(parsed_translate);
        log_line(&format!(
            "hotkeys applied: show_hide={parsed_show}, translate_selection={parsed_translate}"
        ));
        ApplyHotkeysResult {
            registered: true,
            conflict: None,
        }
    }

    /// Unregisters hotkeys registered within a failed `apply` call. Previous
    /// registrations were never touched by the failed attempt.
    fn rollback(&self, registered_now: &[Hotkey]) {
        for hotkey in registered_now {
            if let Err(err) = self.registrar.unregister(hotkey) {
                log_line(&format!("rollback: failed to unregister {hotkey}: {err}"));
            }
        }
    }

    /// Maps a pressed plugin shortcut to its action, based on the currently
    /// applied bindings.
    pub(crate) fn action_for(&self, shortcut: &Shortcut) -> Option<HotkeyAction> {
        let current = self
            .current
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if current
            .show_hide
            .is_some_and(|hotkey| hotkey.to_shortcut().eq(shortcut))
        {
            return Some(HotkeyAction::ShowHide);
        }
        if current
            .translate_selection
            .is_some_and(|hotkey| hotkey.to_shortcut().eq(shortcut))
        {
            return Some(HotkeyAction::TranslateSelection);
        }
        None
    }

    /// Best-effort teardown on app exit (the plugin would clean up anyway).
    pub fn unregister_all(&self) {
        let mut current = self
            .current
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        for hotkey in [current.show_hide, current.translate_selection]
            .into_iter()
            .flatten()
        {
            if let Err(err) = self.registrar.unregister(&hotkey) {
                log_line(&format!("failed to unregister {hotkey} on exit: {err}"));
            }
        }
        *current = CurrentHotkeys::default();
    }
}

fn rejected(conflict: &str) -> ApplyHotkeysResult {
    ApplyHotkeysResult {
        registered: false,
        conflict: Some(conflict.to_string()),
    }
}

/// Plugin-backed registrar: registration authority is Rust.
#[cfg(windows)]
struct PluginRegistrar {
    app: AppHandle,
}

#[cfg(windows)]
impl Registrar for PluginRegistrar {
    fn register(&self, hotkey: &Hotkey) -> Result<(), String> {
        use tauri_plugin_global_shortcut::GlobalShortcutExt;
        self.app
            .global_shortcut()
            .register(hotkey.to_shortcut())
            .map_err(|err| err.to_string())
    }

    fn unregister(&self, hotkey: &Hotkey) -> Result<(), String> {
        use tauri_plugin_global_shortcut::GlobalShortcutExt;
        self.app
            .global_shortcut()
            .unregister(hotkey.to_shortcut())
            .map_err(|err| err.to_string())
    }
}

/// Non-Windows placeholder registrar (1.0 ships Windows only).
#[cfg(not(windows))]
struct UnsupportedRegistrar;

#[cfg(not(windows))]
impl Registrar for UnsupportedRegistrar {
    fn register(&self, _hotkey: &Hotkey) -> Result<(), String> {
        Err("global hotkeys are only supported on Windows in 1.0".to_string())
    }

    fn unregister(&self, _hotkey: &Hotkey) -> Result<(), String> {
        Err("global hotkeys are only supported on Windows in 1.0".to_string())
    }
}

/// Plugin-level event handler (installed once at builder setup). Fires for
/// every registered shortcut; dispatches by comparing against the currently
/// applied bindings. Runs on the plugin's event thread — both actions are
/// non-blocking.
pub(crate) fn handle_shortcut_event(app: &AppHandle, shortcut: &Shortcut, event: ShortcutEvent) {
    if event.state() != ShortcutState::Pressed {
        return;
    }
    let Some(state) = app.try_state::<crate::AppState>() else {
        return;
    };
    match state.hotkeys.action_for(shortcut) {
        Some(HotkeyAction::ShowHide) => crate::toggle_main_window_for_hotkey(app),
        Some(HotkeyAction::TranslateSelection) => crate::selection::start_capture(app.clone()),
        None => {}
    }
}

// ---------------------------------------------------------------------------
// Persistence (improvement bug #5)
// ---------------------------------------------------------------------------

/// Name of the last-applied-bindings file below the data root. The bindings
/// are re-applied at app startup BEFORE the webview boots, so the hotkeys
/// work from the first second instead of only after the frontend applied the
/// settings (the frontend remains the runtime authority).
pub const PERSISTENCE_FILENAME: &str = "hotkeys.json";

/// The persisted binding pair (same strings as the `apply_hotkeys` command).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PersistedHotkeys {
    pub show_hide: String,
    pub translate_selection: String,
}

/// Saves the applied bindings (best effort; failures are logged by callers).
pub fn save_persisted(path: &Path, saved: &PersistedHotkeys) -> Result<(), String> {
    let json = serde_json::to_string_pretty(saved)
        .map_err(|err| format!("encode {PERSISTENCE_FILENAME}: {err}"))?;
    std::fs::write(path, json).map_err(|err| format!("write {PERSISTENCE_FILENAME}: {err}"))
}

/// Loads the persisted bindings; `None` when missing or malformed (a corrupt
/// file is deleted so the next successful apply can rewrite it).
pub fn load_persisted(path: &Path) -> Option<PersistedHotkeys> {
    let raw = match std::fs::read_to_string(path) {
        Ok(raw) => raw,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return None,
        Err(err) => {
            log_line(&format!("failed to read {PERSISTENCE_FILENAME}: {err}"));
            return None;
        }
    };
    match serde_json::from_str::<PersistedHotkeys>(&raw) {
        Ok(parsed) => Some(parsed),
        Err(err) => {
            log_line(&format!(
                "malformed {PERSISTENCE_FILENAME} discarded: {err}"
            ));
            let _ = std::fs::remove_file(path);
            None
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::Arc;

    // ------------------------------------------------------------------
    // Parser
    // ------------------------------------------------------------------

    fn canon(input: &str) -> String {
        parse_hotkey(input).expect(input).to_string()
    }

    #[test]
    fn parses_frozen_defaults() {
        assert_eq!(canon("Ctrl+Alt+Space"), "Ctrl+Alt+Space");
        assert_eq!(canon("Ctrl+Alt+Q"), "Ctrl+Alt+Q");
    }

    #[test]
    fn parses_case_and_whitespace_insensitively() {
        assert_eq!(canon("ctrl+alt+space"), "Ctrl+Alt+Space");
        assert_eq!(canon("CTRL+ALT+SPACE"), "Ctrl+Alt+Space");
        assert_eq!(canon("Ctrl + Alt + Space"), "Ctrl+Alt+Space");
        assert_eq!(canon("  ctrl+alt+q  "), "Ctrl+Alt+Q");
    }

    #[test]
    fn parses_modifier_aliases() {
        assert_eq!(canon("Control+Alt+Space"), "Ctrl+Alt+Space");
        assert_eq!(canon("Win+N"), "Win+N");
        assert_eq!(canon("Windows+N"), "Win+N");
        assert_eq!(canon("Super+N"), "Win+N");
        assert_eq!(canon("Ctrl+Shift+Win+F12"), "Ctrl+Shift+Win+F12");
        assert_eq!(canon("Alt+Shift+B"), "Alt+Shift+B");
        // Modifier order does not matter; duplicate modifiers are idempotent.
        assert_eq!(canon("Shift+Ctrl+X"), "Ctrl+Shift+X");
        assert_eq!(canon("ctrl+ctrl+a"), "Ctrl+A");
    }

    #[test]
    fn parses_all_letters_digits_and_fkeys() {
        for c in b'A'..=b'Z' {
            let ch = c as char;
            assert_eq!(canon(&format!("Ctrl+{ch}")), format!("Ctrl+{ch}"));
            assert_eq!(
                canon(&format!("Ctrl+{}", ch.to_ascii_lowercase())),
                format!("Ctrl+{ch}")
            );
        }
        for d in 0..=9u8 {
            assert_eq!(canon(&format!("Ctrl+{d}")), format!("Ctrl+{d}"));
        }
        for n in 1..=24u8 {
            assert_eq!(canon(&format!("Alt+F{n}")), format!("Alt+F{n}"));
        }
    }

    #[test]
    fn parses_named_keys() {
        for (token, expected) in [
            ("Space", "Space"),
            ("Enter", "Enter"),
            ("Return", "Enter"),
            ("Tab", "Tab"),
            ("Backspace", "Backspace"),
            ("Up", "Up"),
            ("ArrowUp", "Up"),
            ("Down", "Down"),
            ("ArrowDown", "Down"),
            ("Left", "Left"),
            ("ArrowLeft", "Left"),
            ("Right", "Right"),
            ("ArrowRight", "Right"),
            ("Home", "Home"),
            ("End", "End"),
            ("PageUp", "PageUp"),
            ("PageDown", "PageDown"),
        ] {
            assert_eq!(canon(&format!("Ctrl+{token}")), format!("Ctrl+{expected}"));
        }
    }

    #[test]
    fn parses_punctuation_keys() {
        for (token, expected) in [
            ('-', "-"),
            ('=', "="),
            ('[', "["),
            (']', "]"),
            (';', ";"),
            ('\'', "'"),
            (',', ","),
            ('.', "."),
            ('/', "/"),
            ('\\', "\\"),
        ] {
            assert_eq!(canon(&format!("Ctrl+{token}")), format!("Ctrl+{expected}"));
        }
    }

    #[test]
    fn rejects_invalid_strings() {
        for input in [
            "",
            "   ",
            "+",
            "Ctrl+",
            "Ctrl",
            "Alt+Shift",
            "A",
            "space",
            "Ctrl+A+B",
            "Ctrl+Space+Enter",
            "Ctrl+F25",
            "Ctrl+F0",
            "Ctrl+Foo",
            "Ctrl+11",
            "Ctrl+ç",
            "Ctrl+Plus",
            "Ctrl+Alt+",
            "Ctrl++A",
        ] {
            let err = parse_hotkey(input).expect_err(input);
            assert!(err.to_string().contains(input), "{input:?} → {err}");
        }
    }

    #[test]
    fn single_letter_f_is_letter_not_function() {
        assert_eq!(canon("Ctrl+F"), "Ctrl+F");
    }

    #[test]
    fn to_shortcut_is_canonical_across_input_forms() {
        let a = parse_hotkey("Ctrl+Alt+Space").unwrap().to_shortcut();
        let b = parse_hotkey("ctrl + alt + SPACE").unwrap().to_shortcut();
        assert_eq!(a, b);
    }

    // ------------------------------------------------------------------
    // apply logic with a mocked registrar
    // ------------------------------------------------------------------

    struct MockRegistrar {
        registered: Mutex<Vec<String>>,
        register_calls: AtomicUsize,
        unregister_calls: AtomicUsize,
        fail_register: Mutex<Vec<String>>,
    }

    impl MockRegistrar {
        fn new() -> Self {
            Self {
                registered: Mutex::new(Vec::new()),
                register_calls: AtomicUsize::new(0),
                unregister_calls: AtomicUsize::new(0),
                fail_register: Mutex::new(Vec::new()),
            }
        }

        fn fail_on(&self, canonical: &str) {
            self.fail_register
                .lock()
                .unwrap()
                .push(canonical.to_string());
        }

        fn registered(&self) -> Vec<String> {
            self.registered.lock().unwrap().clone()
        }
    }

    impl Registrar for MockRegistrar {
        fn register(&self, hotkey: &Hotkey) -> Result<(), String> {
            self.register_calls.fetch_add(1, Ordering::SeqCst);
            let name = hotkey.to_string();
            if self.fail_register.lock().unwrap().contains(&name) {
                return Err(format!("mock: registration of {name} failed"));
            }
            let mut registered = self.registered.lock().unwrap();
            if registered.contains(&name) {
                return Err(format!("mock: {name} already registered"));
            }
            registered.push(name);
            Ok(())
        }

        fn unregister(&self, hotkey: &Hotkey) -> Result<(), String> {
            self.unregister_calls.fetch_add(1, Ordering::SeqCst);
            let name = hotkey.to_string();
            let mut registered = self.registered.lock().unwrap();
            match registered.iter().position(|n| *n == name) {
                Some(index) => {
                    registered.remove(index);
                    Ok(())
                }
                None => Err(format!("mock: {name} was not registered")),
            }
        }
    }

    /// Lets the manager own an `Arc` clone while the tests keep a handle for
    /// assertions.
    impl Registrar for Arc<MockRegistrar> {
        fn register(&self, hotkey: &Hotkey) -> Result<(), String> {
            (**self).register(hotkey)
        }

        fn unregister(&self, hotkey: &Hotkey) -> Result<(), String> {
            (**self).unregister(hotkey)
        }
    }

    fn setup(fail_register: &[&str]) -> (HotkeyManager, Arc<MockRegistrar>) {
        let mock = Arc::new(MockRegistrar::new());
        for canonical in fail_register {
            mock.fail_on(canonical);
        }
        let manager = HotkeyManager::new(Box::new(Arc::clone(&mock)));
        (manager, mock)
    }

    #[test]
    fn apply_registers_both_bindings_on_first_call() {
        let (manager, mock) = setup(&[]);
        let result = manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q");
        assert_eq!(
            result,
            ApplyHotkeysResult {
                registered: true,
                conflict: None
            }
        );
        assert_eq!(
            mock.registered(),
            vec!["Ctrl+Alt+Space".to_string(), "Ctrl+Alt+Q".to_string()]
        );
        assert_eq!(mock.register_calls.load(Ordering::SeqCst), 2);
    }

    #[test]
    fn apply_rejects_invalid_string_without_registering() {
        let (manager, mock) = setup(&[]);
        let result = manager.apply("banana", "Ctrl+Alt+Q");
        assert_eq!(
            result,
            ApplyHotkeysResult {
                registered: false,
                conflict: Some("banana".to_string())
            }
        );
        assert!(mock.registered().is_empty());

        let result = manager.apply("Ctrl+Alt+Space", "ctrl alt q");
        assert_eq!(result.conflict.as_deref(), Some("ctrl alt q"));
        assert!(mock.registered().is_empty());
        assert_eq!(mock.register_calls.load(Ordering::SeqCst), 0);
    }

    #[test]
    fn apply_rejects_identical_bindings() {
        let (manager, mock) = setup(&[]);
        let result = manager.apply("Ctrl+Alt+Q", "ctrl+alt+q");
        assert!(!result.registered);
        assert_eq!(result.conflict.as_deref(), Some("ctrl+alt+q"));
        assert!(mock.registered().is_empty());
    }

    #[test]
    fn apply_failure_rolls_back_and_keeps_previous_bindings() {
        let (manager, mock) = setup(&[]);
        assert!(manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q").registered);

        mock.fail_on("Ctrl+Alt+T");
        let result = manager.apply("Ctrl+Alt+S", "Ctrl+Alt+T");
        assert!(!result.registered);
        assert_eq!(result.conflict.as_deref(), Some("Ctrl+Alt+T"));
        // Previous bindings untouched, rolled-back binding gone.
        assert_eq!(
            mock.registered(),
            vec!["Ctrl+Alt+Space".to_string(), "Ctrl+Alt+Q".to_string()]
        );
        assert_eq!(mock.unregister_calls.load(Ordering::SeqCst), 1);

        // Old action mapping still works after the failed attempt.
        let old_show = parse_hotkey("Ctrl+Alt+Space").unwrap();
        assert_eq!(
            manager.action_for(&old_show.to_shortcut()),
            Some(HotkeyAction::ShowHide)
        );
    }

    #[test]
    fn apply_swap_succeeds_without_new_registrations() {
        let (manager, mock) = setup(&[]);
        assert!(manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q").registered);

        let result = manager.apply("Ctrl+Alt+Q", "Ctrl+Alt+Space");
        assert!(result.registered);
        assert_eq!(result.conflict, None);
        assert_eq!(mock.register_calls.load(Ordering::SeqCst), 2);
        assert_eq!(mock.unregister_calls.load(Ordering::SeqCst), 0);
        assert_eq!(
            mock.registered(),
            vec!["Ctrl+Alt+Space".to_string(), "Ctrl+Alt+Q".to_string()]
        );
    }

    #[test]
    fn apply_same_values_is_a_noop() {
        let (manager, mock) = setup(&[]);
        assert!(manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q").registered);
        assert!(manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q").registered);
        assert_eq!(mock.register_calls.load(Ordering::SeqCst), 2);
        assert_eq!(mock.unregister_calls.load(Ordering::SeqCst), 0);
    }

    #[test]
    fn apply_replaces_only_changed_binding() {
        let (manager, mock) = setup(&[]);
        assert!(manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q").registered);

        let result = manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+T");
        assert!(result.registered);
        assert_eq!(
            mock.registered(),
            vec!["Ctrl+Alt+Space".to_string(), "Ctrl+Alt+T".to_string()]
        );
        assert_eq!(mock.register_calls.load(Ordering::SeqCst), 3);
        assert_eq!(mock.unregister_calls.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn apply_moves_binding_between_roles() {
        let (manager, mock) = setup(&[]);
        assert!(manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q").registered);

        // Ctrl+Alt+Q becomes show/hide; the old show/hide is freed.
        let result = manager.apply("Ctrl+Alt+Q", "Ctrl+Alt+T");
        assert!(result.registered);
        assert_eq!(
            mock.registered(),
            vec!["Ctrl+Alt+Q".to_string(), "Ctrl+Alt+T".to_string()]
        );
        let show = parse_hotkey("Ctrl+Alt+Q").unwrap();
        assert_eq!(
            manager.action_for(&show.to_shortcut()),
            Some(HotkeyAction::ShowHide)
        );
    }

    #[test]
    fn action_for_maps_current_bindings_only() {
        let (manager, _mock) = setup(&[]);
        assert!(manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q").registered);

        let show = parse_hotkey("Ctrl+Alt+Space").unwrap().to_shortcut();
        let translate = parse_hotkey("Ctrl+Alt+Q").unwrap().to_shortcut();
        let unknown = parse_hotkey("Ctrl+Alt+Z").unwrap().to_shortcut();
        assert_eq!(manager.action_for(&show), Some(HotkeyAction::ShowHide));
        assert_eq!(
            manager.action_for(&translate),
            Some(HotkeyAction::TranslateSelection)
        );
        assert_eq!(manager.action_for(&unknown), None);
    }

    #[test]
    fn unregister_all_clears_everything() {
        let (manager, mock) = setup(&[]);
        assert!(manager.apply("Ctrl+Alt+Space", "Ctrl+Alt+Q").registered);
        manager.unregister_all();
        assert!(mock.registered().is_empty());
        // Idempotent.
        manager.unregister_all();
        assert!(mock.registered().is_empty());
    }

    #[test]
    fn result_serializes_to_frozen_shape() {
        let ok = ApplyHotkeysResult {
            registered: true,
            conflict: None,
        };
        assert_eq!(
            serde_json::to_string(&ok).unwrap(),
            r#"{"registered":true,"conflict":null}"#
        );
        let conflict = ApplyHotkeysResult {
            registered: false,
            conflict: Some("Ctrl+Alt+X".to_string()),
        };
        assert_eq!(
            serde_json::to_string(&conflict).unwrap(),
            r#"{"registered":false,"conflict":"Ctrl+Alt+X"}"#
        );
    }

    // --- persistence --------------------------------------------------------

    #[test]
    fn persisted_hotkeys_roundtrip() {
        let tmp = tempfile::tempdir().unwrap();
        let path = tmp.path().join(PERSISTENCE_FILENAME);
        let saved = PersistedHotkeys {
            show_hide: "Ctrl+Alt+Space".to_string(),
            translate_selection: "Ctrl+Alt+Q".to_string(),
        };
        save_persisted(&path, &saved).unwrap();
        assert_eq!(load_persisted(&path), Some(saved));
    }

    #[test]
    fn load_persisted_missing_file_is_none() {
        let tmp = tempfile::tempdir().unwrap();
        assert_eq!(load_persisted(&tmp.path().join(PERSISTENCE_FILENAME)), None);
    }

    #[test]
    fn load_persisted_malformed_is_none_and_removes_the_file() {
        let tmp = tempfile::tempdir().unwrap();
        let path = tmp.path().join(PERSISTENCE_FILENAME);
        std::fs::write(&path, "{not json").unwrap();
        assert_eq!(load_persisted(&path), None);
        assert!(!path.exists(), "malformed file must be discarded");
    }
}
