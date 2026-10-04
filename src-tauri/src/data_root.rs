//! Data root resolution (docs/07 §8): installed vs portable.
//!
//! * Portable: a `portable.flag` file next to the running executable selects
//!   `<exe_dir>/FloatTranslateData`. In debug builds the environment variable
//!   `FT_PORTABLE=1` selects the same layout (development convenience).
//! * Installed: `%APPDATA%\FloatTranslate` (FOLDERID_RoamingAppData via the
//!   `dirs` crate, frozen decision).
//!
//! The directory is created if missing.

use std::io;
use std::path::{Path, PathBuf};

/// Marker file enabling portable mode next to the executable.
pub const PORTABLE_FLAG_NAME: &str = "portable.flag";
/// Data directory name for portable mode (and as fallback, see below).
pub const PORTABLE_DATA_DIR_NAME: &str = "FloatTranslateData";
/// Subdirectory of `%APPDATA%` (FOLDERID_RoamingAppData) in installed mode.
pub const INSTALLED_DIR_NAME: &str = "FloatTranslate";
/// Development-only override honored in debug builds (`FT_PORTABLE=1`).
pub const ENV_PORTABLE: &str = "FT_PORTABLE";

/// Pure resolution logic (unit tested).
///
/// Precedence:
/// 1. `portable_flag_present` → `<exe_dir>/FloatTranslateData`
/// 2. `dev_portable_env` (debug builds only, caller decides) → same as portable
/// 3. installed → `<roaming_appdata>/FloatTranslate`
/// 4. roaming dir unavailable → portable fallback next to the exe
pub fn resolve_data_root(
    exe_dir: &Path,
    portable_flag_present: bool,
    dev_portable_env: bool,
    roaming_appdata: Option<&Path>,
) -> PathBuf {
    if portable_flag_present || dev_portable_env {
        return exe_dir.join(PORTABLE_DATA_DIR_NAME);
    }
    match roaming_appdata {
        Some(base) => base.join(INSTALLED_DIR_NAME),
        None => exe_dir.join(PORTABLE_DATA_DIR_NAME),
    }
}

#[derive(Debug, thiserror::Error)]
pub enum DataRootError {
    #[error("failed to create data root {path}: {source}")]
    Create {
        path: PathBuf,
        #[source]
        source: io::Error,
    },
}

/// Creates the data root (and parents) if missing.
pub fn ensure_dir(path: &Path) -> Result<PathBuf, DataRootError> {
    std::fs::create_dir_all(path).map_err(|source| DataRootError::Create {
        path: path.to_path_buf(),
        source,
    })?;
    Ok(path.to_path_buf())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    fn temp_exe_dir() -> (tempfile::TempDir, PathBuf) {
        let dir = tempfile::tempdir().expect("tempdir");
        let exe_dir = dir.path().join("app").join("bin");
        fs::create_dir_all(&exe_dir).expect("create exe dir");
        (dir, exe_dir)
    }

    #[test]
    fn portable_flag_selects_exe_relative_dir() {
        let (_guard, exe_dir) = temp_exe_dir();
        let roaming = Path::new("C:\\Users\\x\\AppData\\Roaming");
        let root = resolve_data_root(&exe_dir, true, false, Some(roaming));
        assert_eq!(root, exe_dir.join(PORTABLE_DATA_DIR_NAME));
    }

    #[test]
    fn flag_file_detection_feeds_portable_resolution() {
        let guard = tempfile::tempdir().expect("tempdir");
        let flag = guard.path().join(PORTABLE_FLAG_NAME);
        fs::write(&flag, b"").expect("write flag");
        let detected = flag.is_file();
        assert!(detected);
        let root = resolve_data_root(guard.path(), detected, false, None);
        assert_eq!(root, guard.path().join(PORTABLE_DATA_DIR_NAME));
    }

    #[test]
    fn dev_portable_env_selects_exe_relative_dir() {
        let (_guard, exe_dir) = temp_exe_dir();
        let roaming = Path::new("C:\\Users\\x\\AppData\\Roaming");
        let root = resolve_data_root(&exe_dir, false, true, Some(roaming));
        assert_eq!(root, exe_dir.join(PORTABLE_DATA_DIR_NAME));
    }

    #[test]
    fn installed_mode_uses_roaming_appdata() {
        let (_guard, exe_dir) = temp_exe_dir();
        let roaming = Path::new("C:\\Users\\x\\AppData\\Roaming");
        let root = resolve_data_root(&exe_dir, false, false, Some(roaming));
        assert_eq!(root, roaming.join(INSTALLED_DIR_NAME));
    }

    #[test]
    fn falls_back_next_to_exe_without_roaming_dir() {
        let (_guard, exe_dir) = temp_exe_dir();
        let root = resolve_data_root(&exe_dir, false, false, None);
        assert_eq!(root, exe_dir.join(PORTABLE_DATA_DIR_NAME));
    }

    #[test]
    fn ensure_dir_creates_nested_missing_dirs() {
        let guard = tempfile::tempdir().expect("tempdir");
        let target = guard.path().join("a").join("b").join("FloatTranslateData");
        let created = ensure_dir(&target).expect("ensure_dir");
        assert_eq!(created, target);
        assert!(target.is_dir());
    }
}
