//! Tauri IPC commands exposed to the WebView (backend wiring + hotkeys + export).

use std::fmt;
use std::fs;
use std::path::Path;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager, State};
use tauri_plugin_dialog::DialogExt;

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

/// Maximum characters kept from `default_file_name` (frozen contract).
const MAX_EXPORT_FILE_NAME_LEN: usize = 120;
/// Fallback when sanitization leaves no usable file name (frozen contract).
const FALLBACK_EXPORT_FILE_NAME: &str = "chat-export.md";
/// Markdown extension of the save filter, also appended when missing.
const MARKDOWN_EXTENSION: &str = "md";

/// Request body of `export_markdown` (frozen contract):
/// `{ "default_file_name": ..., "content": ... }`.
#[derive(Debug, Clone, Deserialize)]
pub struct ExportMarkdownOptions {
    #[serde(alias = "defaultFileName")]
    pub default_file_name: String,
    pub content: String,
}

/// Response of `export_markdown` (frozen contract):
/// `{ "ok": bool, "path": string|null, "cancelled": bool|null }`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct ExportMarkdownResult {
    pub ok: bool,
    pub path: Option<String>,
    pub cancelled: Option<bool>,
}

/// Sanitizes `default_file_name` into a safe single-component file name.
///
/// Path separators and Windows-reserved characters (`< > : " / \ | ? *`) are
/// stripped, surrounding whitespace is trimmed and the name is capped at 120
/// characters. An empty result falls back to `chat-export.md`, and `.md` is
/// appended whenever the name carries no extension.
fn sanitize_default_file_name(raw: &str) -> String {
    let stripped: String = raw
        .chars()
        .filter(|c| !matches!(c, '<' | '>' | ':' | '"' | '/' | '\\' | '|' | '?' | '*'))
        .collect();
    let mut name: String = stripped
        .trim()
        .chars()
        .take(MAX_EXPORT_FILE_NAME_LEN)
        .collect();
    if name.is_empty() {
        return FALLBACK_EXPORT_FILE_NAME.to_string();
    }
    if Path::new(&name).extension().is_none() {
        name.push('.');
        name.push_str(MARKDOWN_EXTENSION);
    }
    name
}

/// Writes `content` (UTF-8) to `path`, creating missing parent directories.
fn write_export_file(path: &Path, content: &str) -> Result<(), String> {
    if let Some(parent) = path.parent() {
        if !parent.as_os_str().is_empty() {
            fs::create_dir_all(parent).map_err(|err| {
                format!(
                    "failed to create export directory {}: {err}",
                    parent.display()
                )
            })?;
        }
    }
    fs::write(path, content)
        .map_err(|err| format!("failed to write export file {}: {err}", path.display()))
}

/// Opens a native SAVE dialog and writes the session markdown export.
///
/// Frozen contract: the dialog is titled `导出会话`, filtered to Markdown
/// (`.md`), modal to the main window and pre-filled with the sanitized
/// `default_file_name`. User cancel yields `{ok:false, cancelled:true,
/// path:null}`; success writes UTF-8 content and yields
/// `{ok:true, path:Some(..), cancelled:null}`; IO failures carry the message
/// in `Err`. Async so the plugin's blocking dialog never runs on the main
/// thread (the plugin dispatches to the main thread itself).
#[tauri::command]
pub async fn export_markdown(
    app: AppHandle,
    options: ExportMarkdownOptions,
) -> Result<ExportMarkdownResult, String> {
    let file_name = sanitize_default_file_name(&options.default_file_name);

    let mut dialog = app
        .dialog()
        .file()
        .set_title("导出会话")
        .add_filter("Markdown", &[MARKDOWN_EXTENSION])
        .set_file_name(&file_name);
    if let Some(window) = app.get_webview_window("main") {
        dialog = dialog.set_parent(&window);
    }
    let Some(chosen) = dialog.blocking_save_file() else {
        return Ok(ExportMarkdownResult {
            ok: false,
            path: None,
            cancelled: Some(true),
        });
    };

    // Windows save dialogs may return `\\?\`-prefixed UNC paths; simplify for
    // the user-facing string while keeping a plain `PathBuf` for IO.
    let path = chosen
        .simplified()
        .into_path()
        .map_err(|err| format!("invalid export path: {err}"))?;
    write_export_file(&path, &options.content)?;
    Ok(ExportMarkdownResult {
        ok: true,
        path: Some(path.to_string_lossy().into_owned()),
        cancelled: None,
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

    // --- export_markdown ---------------------------------------------------

    #[test]
    fn sanitize_keeps_name_with_extension() {
        assert_eq!(sanitize_default_file_name("report.md"), "report.md");
        assert_eq!(
            sanitize_default_file_name("会话 2026-10-04"),
            "会话 2026-10-04.md"
        );
        assert_eq!(sanitize_default_file_name("archive.v2"), "archive.v2");
    }

    #[test]
    fn sanitize_strips_reserved_and_separator_chars() {
        assert_eq!(
            sanitize_default_file_name("a<b>:c\"d/e\\f|g?h*i"),
            "abcdefghi.md"
        );
        // Windows-only drive syntax collapses to a plain name.
        assert_eq!(
            sanitize_default_file_name("C:\\evil\\path.md"),
            "Cevilpath.md"
        );
    }

    #[test]
    fn sanitize_falls_back_when_empty() {
        assert_eq!(sanitize_default_file_name(""), "chat-export.md");
        assert_eq!(sanitize_default_file_name("   "), "chat-export.md");
        assert_eq!(sanitize_default_file_name("<>:\"/\\|?*"), "chat-export.md");
    }

    #[test]
    fn sanitize_caps_length_at_120_chars() {
        let long = "n".repeat(200);
        // Cap applies to the base name; the extension is appended afterwards.
        assert_eq!(
            sanitize_default_file_name(&long),
            format!("{}.md", "n".repeat(MAX_EXPORT_FILE_NAME_LEN))
        );

        let long_with_ext = format!("{long}.md");
        // The original ".md" sits beyond the cap, so it is cut and re-appended.
        assert_eq!(
            sanitize_default_file_name(&long_with_ext),
            format!("{}.md", "n".repeat(MAX_EXPORT_FILE_NAME_LEN))
        );
    }

    #[test]
    fn export_options_accept_frozen_field_names() {
        let options: ExportMarkdownOptions =
            serde_json::from_str(r##"{"default_file_name":"chat.md","content":"# 头部\n\n正文"}"##)
                .unwrap();
        assert_eq!(options.default_file_name, "chat.md");
        assert_eq!(options.content, "# 头部\n\n正文");
    }

    #[test]
    fn export_options_tolerate_camel_case_alias() {
        let options: ExportMarkdownOptions =
            serde_json::from_str(r#"{"defaultFileName":"chat.md","content":"body"}"#).unwrap();
        assert_eq!(options.default_file_name, "chat.md");
        assert_eq!(options.content, "body");
    }

    #[test]
    fn export_result_serializes_frozen_shape() {
        let success = ExportMarkdownResult {
            ok: true,
            path: Some("C:\\out\\chat.md".into()),
            cancelled: None,
        };
        assert_eq!(
            serde_json::to_string(&success).unwrap(),
            r#"{"ok":true,"path":"C:\\out\\chat.md","cancelled":null}"#
        );

        let cancelled = ExportMarkdownResult {
            ok: false,
            path: None,
            cancelled: Some(true),
        };
        assert_eq!(
            serde_json::to_string(&cancelled).unwrap(),
            r#"{"ok":false,"path":null,"cancelled":true}"#
        );
    }

    #[test]
    fn write_export_file_creates_missing_parents() {
        let tmp = tempfile::tempdir().unwrap();
        let path = tmp.path().join("nested").join("dir").join("chat.md");
        write_export_file(&path, "# 正文").unwrap();
        assert_eq!(fs::read_to_string(&path).unwrap(), "# 正文");
    }

    #[test]
    fn write_export_file_reports_io_errors() {
        // A path under a *file* can never become a directory.
        let tmp = tempfile::tempdir().unwrap();
        let blocker = tmp.path().join("blocker");
        fs::write(&blocker, b"x").unwrap();
        let err = write_export_file(&blocker.join("chat.md"), "body").unwrap_err();
        assert!(err.contains("failed to create export directory"), "{err}");
    }
}
