//! Window geometry persistence (frozen contract).
//!
//! Our own JSON file inside the data root — deliberately **not**
//! `tauri-plugin-window-state`:
//! `<data_root>/window-state.json`
//! `{ "width": f64, "height": f64, "x": f64?, "y": f64?, "maximized": bool,
//!   "always_on_top": bool }`
//!
//! This module is pure (no Tauri types) so serde and clamping logic are unit
//! testable. Tauri glue lives in `lib.rs`.

use std::path::{Path, PathBuf};
use std::{fmt, fs, io};

use serde::{Deserialize, Serialize};

pub const WINDOW_STATE_FILENAME: &str = "window-state.json";
/// Default window size (docs/00 §2: 420×720).
pub const DEFAULT_WIDTH: f64 = 420.0;
pub const DEFAULT_HEIGHT: f64 = 720.0;
/// Minimum window size (docs/00 §2: 360×520).
pub const MIN_WIDTH: f64 = 360.0;
pub const MIN_HEIGHT: f64 = 520.0;
/// Minimum visible strip (physical px) required for a restored position.
const MIN_VISIBLE: f64 = 100.0;

/// Persisted main-window geometry. Physical pixels.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct WindowState {
    pub width: f64,
    pub height: f64,
    #[serde(default)]
    pub x: Option<f64>,
    #[serde(default)]
    pub y: Option<f64>,
    #[serde(default)]
    pub maximized: bool,
    #[serde(default = "default_always_on_top")]
    pub always_on_top: bool,
}

fn default_always_on_top() -> bool {
    true
}

impl WindowState {
    /// First-run geometry: default size, OS-chosen position, always on top
    /// (docs/00 §2).
    pub fn default_geometry() -> Self {
        Self {
            width: DEFAULT_WIDTH,
            height: DEFAULT_HEIGHT,
            x: None,
            y: None,
            maximized: false,
            always_on_top: true,
        }
    }

    fn is_sane(&self) -> bool {
        self.width.is_finite()
            && self.height.is_finite()
            && self.width >= MIN_WIDTH
            && self.height >= MIN_HEIGHT
            && self.x.is_none_or(f64::is_finite)
            && self.y.is_none_or(f64::is_finite)
    }
}

/// Monitor working bounds in physical pixels (pure data for clamping).
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct MonitorBounds {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

#[derive(Debug, thiserror::Error)]
pub enum WindowStateError {
    #[error("failed to write window state to {path}: {source}")]
    Write {
        path: PathBuf,
        #[source]
        source: io::Error,
    },
    #[error("failed to serialize window state: {0}")]
    Serialize(#[from] serde_json::Error),
}

impl fmt::Display for WindowState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}x{}", self.width, self.height)?;
        match (self.x, self.y) {
            (Some(x), Some(y)) => write!(f, "@({x},{y})"),
            _ => write!(f, "@(default)"),
        }
    }
}

/// Loads and validates persisted geometry.
///
/// Returns `None` when the file is missing, corrupt or insane; callers fall
/// back to defaults in that case (frozen contract: "if restore fails,
/// defaults").
pub fn load(path: &Path) -> Option<WindowState> {
    let raw = fs::read_to_string(path).ok()?;
    let state: WindowState = serde_json::from_str(&raw).ok()?;
    if !state.is_sane() {
        return None;
    }
    Some(state)
}

/// Saves geometry atomically enough for Phase 1: write a temp file, then
/// rename over the target.
pub fn save(path: &Path, state: &WindowState) -> Result<(), WindowStateError> {
    let json = serde_json::to_string_pretty(state)?;
    let tmp = path.with_extension("json.tmp");
    fs::write(&tmp, json).map_err(|source| WindowStateError::Write {
        path: path.to_path_buf(),
        source,
    })?;
    fs::rename(&tmp, path).map_err(|source| WindowStateError::Write {
        path: path.to_path_buf(),
        source,
    })?;
    Ok(())
}

/// `value.clamp(lo, hi)` without the panic when `lo > hi` (degenerate range,
/// e.g. a tiny monitor): degenerates to `hi`.
fn clamp_in_range(value: f64, lo: f64, hi: f64) -> f64 {
    if lo > hi {
        return hi;
    }
    value.clamp(lo, hi)
}

/// Pure sanity clamp of a stored geometry against monitor bounds.
///
/// * sizes are pushed into `[min, monitor]` (monitor unknown → only min);
/// * positions are clamped so at least [`MIN_VISIBLE`] px stay on screen;
/// * unknown/NaN positions drop back to `None` (OS default);
/// * the restored `maximized` / `always_on_top` flags are kept as-is.
pub fn clamp_to_monitor(
    state: &WindowState,
    monitor: Option<MonitorBounds>,
    min_w: f64,
    min_h: f64,
) -> WindowState {
    let mut out = state.clone();

    if !out.width.is_finite() || !out.height.is_finite() {
        out.width = DEFAULT_WIDTH;
        out.height = DEFAULT_HEIGHT;
    }
    out.width = out.width.max(min_w);
    out.height = out.height.max(min_h);

    let Some(bounds) = monitor.filter(|b| b.width > 0.0 && b.height > 0.0) else {
        out.x = None;
        out.y = None;
        return out;
    };

    out.width = out.width.min(bounds.width);
    out.height = out.height.min(bounds.height);

    match (out.x, out.y) {
        (Some(x), Some(y)) if x.is_finite() && y.is_finite() => {
            // Keep at least MIN_VISIBLE px of the window on the monitor.
            let min_x = bounds.x + MIN_VISIBLE - out.width;
            let max_x = bounds.x + bounds.width - MIN_VISIBLE;
            let min_y = bounds.y + MIN_VISIBLE - out.height;
            let max_y = bounds.y + bounds.height - MIN_VISIBLE;
            out.x = Some(clamp_in_range(x, min_x, max_x));
            out.y = Some(clamp_in_range(y, min_y, max_y));
        }
        _ => {
            out.x = None;
            out.y = None;
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    const MONITOR: Option<MonitorBounds> = Some(MonitorBounds {
        x: 0.0,
        y: 0.0,
        width: 1920.0,
        height: 1080.0,
    });

    fn sample() -> WindowState {
        WindowState {
            width: 420.0,
            height: 720.0,
            x: Some(100.0),
            y: Some(80.0),
            maximized: false,
            always_on_top: true,
        }
    }

    #[test]
    fn serde_roundtrip_keeps_all_fields() {
        let original = sample();
        let json = serde_json::to_string(&original).unwrap();
        let parsed: WindowState = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed, original);
    }

    #[test]
    fn serde_defaults_optional_fields() {
        let parsed: WindowState =
            serde_json::from_str(r#"{"width":420.0,"height":720.0}"#).unwrap();
        assert_eq!(parsed.x, None);
        assert_eq!(parsed.y, None);
        assert!(!parsed.maximized);
        assert!(parsed.always_on_top, "always_on_top defaults to true");
    }

    #[test]
    fn load_missing_file_is_none() {
        let guard = tempfile::tempdir().unwrap();
        assert_eq!(load(&guard.path().join(WINDOW_STATE_FILENAME)), None);
    }

    #[test]
    fn load_corrupt_file_is_none() {
        let guard = tempfile::tempdir().unwrap();
        let path = guard.path().join(WINDOW_STATE_FILENAME);
        std::fs::write(&path, "not json at all").unwrap();
        assert_eq!(load(&path), None);
    }

    #[test]
    fn load_undersized_geometry_is_rejected() {
        let guard = tempfile::tempdir().unwrap();
        let path = guard.path().join(WINDOW_STATE_FILENAME);
        std::fs::write(&path, r#"{"width":100.0,"height":100.0}"#).unwrap();
        assert_eq!(load(&path), None);
    }

    #[test]
    fn save_then_load_roundtrips() {
        let guard = tempfile::tempdir().unwrap();
        let path = guard.path().join(WINDOW_STATE_FILENAME);
        save(&path, &sample()).expect("save");
        assert_eq!(load(&path), Some(sample()));
        // Renaming over an existing file must succeed.
        let mut second = sample();
        second.x = Some(300.0);
        save(&path, &second).expect("overwrite save");
        assert_eq!(load(&path), Some(second));
    }

    #[test]
    fn clamp_keeps_sane_geometry_untouched() {
        assert_eq!(
            clamp_to_monitor(&sample(), MONITOR, MIN_WIDTH, MIN_HEIGHT),
            sample()
        );
    }

    #[test]
    fn clamp_pulls_window_back_from_right_edge() {
        let mut state = sample();
        state.x = Some(5000.0);
        let clamped = clamp_to_monitor(&state, MONITOR, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.x, Some(1920.0 - MIN_VISIBLE));
        assert_eq!(clamped.width, 420.0);
    }

    #[test]
    fn clamp_pulls_window_back_from_left_edge() {
        let mut state = sample();
        state.x = Some(-1000.0);
        let clamped = clamp_to_monitor(&state, MONITOR, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.x, Some(MIN_VISIBLE - 420.0));
    }

    #[test]
    fn clamp_pulls_window_back_from_bottom_and_top() {
        let mut state = sample();
        state.y = Some(5000.0);
        let clamped = clamp_to_monitor(&state, MONITOR, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.y, Some(1080.0 - MIN_VISIBLE));

        state.y = Some(-900.0);
        let clamped = clamp_to_monitor(&state, MONITOR, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.y, Some(MIN_VISIBLE - 720.0));
    }

    #[test]
    fn clamp_respects_monitor_offset() {
        let monitor = Some(MonitorBounds {
            x: -1920.0,
            y: -200.0,
            width: 1920.0,
            height: 1080.0,
        });
        let mut state = sample();
        state.x = Some(9000.0);
        state.y = Some(9000.0);
        let clamped = clamp_to_monitor(&state, monitor, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.x, Some(-1920.0 + 1920.0 - MIN_VISIBLE));
        assert_eq!(clamped.y, Some(-200.0 + 1080.0 - MIN_VISIBLE));
    }

    #[test]
    fn clamp_caps_size_to_monitor_and_floor_to_min() {
        let mut state = sample();
        state.width = 99999.0;
        state.height = 100.0;
        let clamped = clamp_to_monitor(&state, MONITOR, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.width, 1920.0);
        assert_eq!(clamped.height, MIN_HEIGHT);
    }

    #[test]
    fn clamp_without_monitor_drops_position() {
        let clamped = clamp_to_monitor(&sample(), None, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.x, None);
        assert_eq!(clamped.y, None);
        assert_eq!(clamped.width, 420.0);
    }

    #[test]
    fn clamp_half_position_drops_both() {
        let mut state = sample();
        state.y = None;
        let clamped = clamp_to_monitor(&state, MONITOR, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.x, None);
        assert_eq!(clamped.y, None);
    }

    #[test]
    fn clamp_nan_size_falls_back_to_defaults() {
        let mut state = sample();
        state.width = f64::NAN;
        let clamped = clamp_to_monitor(&state, MONITOR, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.width, DEFAULT_WIDTH);
        assert_eq!(clamped.height, DEFAULT_HEIGHT);
    }

    #[test]
    fn clamp_keeps_maximized_and_always_on_top_flags() {
        let mut state = sample();
        state.maximized = true;
        state.always_on_top = false;
        let clamped = clamp_to_monitor(&state, MONITOR, MIN_WIDTH, MIN_HEIGHT);
        assert!(clamped.maximized);
        assert!(!clamped.always_on_top);
    }

    #[test]
    fn degenerate_monitor_bounds_do_not_panic() {
        let tiny = Some(MonitorBounds {
            x: 0.0,
            y: 0.0,
            width: 10.0,
            height: 10.0,
        });
        let clamped = clamp_to_monitor(&sample(), tiny, MIN_WIDTH, MIN_HEIGHT);
        assert_eq!(clamped.width, 10.0);
        assert!(clamped.x.is_some());
    }
}
