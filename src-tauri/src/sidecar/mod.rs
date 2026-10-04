//! Go sidecar lifecycle (docs/02 §5–6, docs/07 §7).
//!
//! * Spawn the backend exe with `FT_SESSION_TOKEN`, `FT_HTTP_PORT=0`,
//!   `FT_DATA_ROOT`.
//! * Wait (≤15s) for the one-line READY JSON on stdout (see [`ready`]).
//! * Supervise: on unexpected child exit or failed start, restart with the
//!   frozen backoff schedule 1s → 3s → 9s (max 3 restarts), then emit
//!   `backend-failed` and stop.
//! * On app exit: terminate the child (kill, then reap).
//!
//! Everything runs on a plain std thread; the Tauri runtime's async reactor
//! is not involved.

pub mod ready;

use std::fmt;
use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{self, RecvTimeoutError};
use std::sync::{Arc, Mutex, MutexGuard};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use tauri::{AppHandle, Emitter};

use crate::events::{BackendFailedPayload, BackendStatusPayload};
use crate::{events, log_line};
use ready::ReadyInfo;

/// Name of the backend executable (frozen contract).
pub const BACKEND_EXE_NAME: &str = "floattranslate-backend.exe";
/// Environment variable overriding backend discovery (frozen contract).
pub const ENV_BACKEND_EXE: &str = "FT_BACKEND_EXE";
/// Environment variable carrying the per-run session token (frozen contract).
pub const ENV_SESSION_TOKEN: &str = "FT_SESSION_TOKEN";
/// Environment variable asking the backend for a dynamic port (frozen contract).
pub const ENV_HTTP_PORT: &str = "FT_HTTP_PORT";
/// Environment variable carrying the resolved data root (frozen contract).
pub const ENV_DATA_ROOT: &str = "FT_DATA_ROOT";

/// Maximum time to wait for the READY line (frozen contract: ~15s).
pub const READY_TIMEOUT: Duration = Duration::from_secs(15);
/// Frozen restart backoff schedule in seconds (1s, 3s, 9s; max 3 restarts).
const RESTART_BACKOFF_SECS: [u64; 3] = [1, 3, 9];
/// How often the supervisor polls child exit / shutdown flags.
const SUPERVISE_POLL: Duration = Duration::from_millis(150);
/// How often the handshake wait re-checks timeout / shutdown.
const HANDSHAKE_POLL: Duration = Duration::from_millis(100);
/// Grace period when reaping the exit code of a child that died before READY.
const REAP_GRACE: Duration = Duration::from_secs(1);

/// Sidecar lifecycle status, mirrored to the frontend via `backend-status`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BackendStatus {
    Starting,
    Ready,
    Restarting,
    Failed,
}

impl BackendStatus {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Starting => "starting",
            Self::Ready => "ready",
            Self::Restarting => "restarting",
            Self::Failed => "failed",
        }
    }
}

impl fmt::Display for BackendStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Point-in-time view of the sidecar state; read by `get_backend_config`.
#[derive(Debug, Clone)]
pub struct BackendSnapshot {
    pub status: BackendStatus,
    pub message: Option<String>,
    pub port: Option<u16>,
    pub version: Option<String>,
}

#[derive(Debug, thiserror::Error)]
pub enum SidecarError {
    #[error("backend executable not found; tried: {candidates}")]
    BackendNotFound { candidates: String },
    #[error("failed to spawn backend: {0}")]
    Spawn(String),
    #[error("backend READY handshake timed out after {}s", READY_TIMEOUT.as_secs())]
    HandshakeTimeout,
    #[error("backend exited before signaling READY (exit code {code:?})")]
    ExitedBeforeReady { code: Option<i32> },
    #[error("invalid READY handshake: {0}")]
    InvalidHandshake(String),
    #[error("supervisor shutdown requested")]
    ShutdownRequested,
}

/// Resolves the backend executable (frozen contract, in order):
///
/// 1. `FT_BACKEND_EXE` environment variable (if it points at an existing file),
/// 2. `<repo>/backend/bin/floattranslate-backend.exe` (repo root is derived
///    from the standard dev layout `<repo>/src-tauri/target/<profile>/`),
/// 3. `<exe_dir>/resources/floattranslate-backend.exe` (NSIS install layout:
///    the release bundle config `scripts/tauri-release.config.json` ships the
///    backend under `<install>/resources`),
/// 4. `<exe_dir>/floattranslate-backend.exe` (portable/custom layout).
///
/// Every candidate that was tried is listed in the error for diagnostics.
pub fn resolve_backend_exe(
    exe_dir: &Path,
    ft_backend_exe: Option<&str>,
) -> Result<PathBuf, SidecarError> {
    let mut candidates: Vec<PathBuf> = Vec::new();

    if let Some(value) = ft_backend_exe {
        let value = value.trim();
        if !value.is_empty() {
            candidates.push(PathBuf::from(value));
        }
    }
    for repo_root in repo_roots_from_exe_dir(exe_dir) {
        candidates.push(repo_root.join("backend").join("bin").join(BACKEND_EXE_NAME));
    }
    // NSIS install layout: the release bundle config copies the backend into
    // `<install>/resources/` (docs/10 packaging).
    candidates.push(exe_dir.join("resources").join(BACKEND_EXE_NAME));
    candidates.push(exe_dir.join(BACKEND_EXE_NAME));

    if let Some(found) = candidates.iter().find(|p| p.is_file()) {
        return Ok(found.clone());
    }
    Err(SidecarError::BackendNotFound {
        candidates: candidates
            .iter()
            .map(|p| p.display().to_string())
            .collect::<Vec<_>>()
            .join("; "),
    })
}

/// Dev-layout helper: `<repo>/src-tauri/target/<profile>/floattranslate.exe`
/// → `<repo>`. Returns nothing when the exe is not inside a `target` dir
/// (installed or portable layout).
fn repo_roots_from_exe_dir(exe_dir: &Path) -> Vec<PathBuf> {
    let Some(target_dir) = exe_dir.parent() else {
        return Vec::new();
    };
    if target_dir.file_name() != Some(std::ffi::OsStr::new("target")) {
        return Vec::new();
    }
    let Some(src_tauri_dir) = target_dir.parent() else {
        return Vec::new();
    };
    if src_tauri_dir.file_name() != Some(std::ffi::OsStr::new("src-tauri")) {
        return Vec::new();
    }
    match src_tauri_dir.parent() {
        Some(repo) => vec![repo.to_path_buf()],
        None => Vec::new(),
    }
}

/// Builds the spawn command for the backend with the frozen environment
/// contract. Public so integration tests can reuse it with arbitrary stub
/// executables.
pub fn build_backend_command(exe: &Path, token: &str, data_root: &Path) -> Command {
    let mut command = Command::new(exe);
    command
        .env(ENV_SESSION_TOKEN, token)
        .env(ENV_HTTP_PORT, "0")
        .env(ENV_DATA_ROOT, data_root)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    // The Go sidecar is a console-subsystem binary; without CREATE_NO_WINDOW
    // a release build (GUI subsystem, no parent console) makes Windows
    // allocate a VISIBLE console for it next to the app window.
    #[cfg(windows)]
    {
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        use std::os::windows::process::CommandExt as _;
        command.creation_flags(CREATE_NO_WINDOW);
    }
    command
}

/// Spawns `command` and performs the READY handshake.
///
/// Public (with an explicit timeout) so integration tests can drive it with
/// stub processes and short timeouts. On any error the child is terminated
/// before returning.
pub fn spawn_and_handshake_with_timeout(
    command: &mut Command,
    stop: &AtomicBool,
    timeout: Duration,
) -> Result<(Child, ReadyInfo), SidecarError> {
    let mut child = command
        .spawn()
        .map_err(|err| SidecarError::Spawn(err.to_string()))?;

    let stdout = match child.stdout.take() {
        Some(pipe) => pipe,
        None => {
            terminate(&mut child);
            return Err(SidecarError::Spawn(
                "backend stdout pipe unavailable".to_string(),
            ));
        }
    };

    let (tx, rx) = mpsc::channel::<Result<ReadyInfo, SidecarError>>();
    let reader_thread = thread::Builder::new()
        .name("floattranslate-backend-stdout".to_string())
        .spawn(move || {
            // Reads backend stdout until EOF. Sends exactly one result: the
            // first valid READY line, the first fatal READY payload, or an
            // "exited before READY" error at EOF. Lines after READY are
            // drained silently so the backend can never block on a full pipe.
            let mut reader = BufReader::new(stdout);
            let mut sent = false;
            loop {
                let mut line = String::new();
                match reader.read_line(&mut line) {
                    Ok(0) | Err(_) => break,
                    Ok(_) => {
                        if sent {
                            continue;
                        }
                        let trimmed = line.trim();
                        if trimmed.is_empty() {
                            continue;
                        }
                        match ready::parse_ready_line(trimmed) {
                            Ok(info) => {
                                sent = tx.send(Ok(info)).is_ok();
                            }
                            Err(ready::ReadyLineError::NotReady) => {}
                            Err(ready::ReadyLineError::Invalid(reason)) => {
                                let _ = tx.send(Err(SidecarError::InvalidHandshake(reason)));
                                break;
                            }
                        }
                    }
                }
            }
            if !sent {
                let _ = tx.send(Err(SidecarError::ExitedBeforeReady { code: None }));
            }
        });
    let reader_thread = match reader_thread {
        Ok(handle) => handle,
        Err(_) => {
            terminate(&mut child);
            return Err(SidecarError::Spawn(
                "failed to spawn backend stdout reader thread".to_string(),
            ));
        }
    };
    // The reader thread is detached; it terminates at EOF (i.e. when the
    // backend exits or is killed by us).
    drop(reader_thread);

    let deadline = Instant::now() + timeout;
    let outcome = loop {
        match rx.recv_timeout(HANDSHAKE_POLL) {
            Ok(result) => break result,
            Err(RecvTimeoutError::Timeout) => {
                if stop.load(Ordering::SeqCst) {
                    break Err(SidecarError::ShutdownRequested);
                }
                if Instant::now() >= deadline {
                    break Err(SidecarError::HandshakeTimeout);
                }
            }
            Err(RecvTimeoutError::Disconnected) => {
                break Err(SidecarError::ExitedBeforeReady { code: None });
            }
        }
    };

    match outcome {
        Ok(info) => Ok((child, info)),
        Err(mut err) => {
            if let SidecarError::ExitedBeforeReady { code: None } = err {
                err = SidecarError::ExitedBeforeReady {
                    code: reap_exit_code(&mut child),
                };
            }
            terminate(&mut child);
            Err(err)
        }
    }
}

/// Production wrapper around [`spawn_and_handshake_with_timeout`] using the
/// frozen 15s READY timeout.
pub fn spawn_and_handshake(
    command: &mut Command,
    stop: &AtomicBool,
) -> Result<(Child, ReadyInfo), SidecarError> {
    spawn_and_handshake_with_timeout(command, stop, READY_TIMEOUT)
}

/// Polls for the child's exit status for a short grace period. Used to enrich
/// "exited before READY" errors with the real exit code.
fn reap_exit_code(child: &mut Child) -> Option<i32> {
    let deadline = Instant::now() + REAP_GRACE;
    loop {
        match child.try_wait() {
            Ok(Some(status)) => return status.code(),
            Ok(None) if Instant::now() >= deadline => return None,
            Ok(None) => thread::sleep(Duration::from_millis(20)),
            Err(_) => return None,
        }
    }
}

/// Terminates the child: `Child::kill` on Windows is TerminateProcess, then
/// the process is reaped. A graceful cooperative shutdown path (job objects /
/// shutdown pipe) is a later-phase refinement; this implements the contract's
/// "kill (then wait)" step.
fn terminate(child: &mut Child) {
    let _ = child.kill();
    let _ = child.wait();
}

/// Shared snapshot cell (poison-tolerant by design: a snapshot is diagnostic
/// state, never worth panicking over).
struct Shared {
    snapshot: Mutex<BackendSnapshot>,
}

impl Shared {
    fn lock(&self) -> MutexGuard<'_, BackendSnapshot> {
        self.snapshot
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
    }
}

/// Handle to the sidecar supervisor thread. Cloned into Tauri state.
pub struct Supervisor {
    shared: Arc<Shared>,
    stop: Arc<AtomicBool>,
    worker: Mutex<Option<JoinHandle<()>>>,
}

impl Supervisor {
    /// Starts the supervision thread. The thread owns every spawn/restart
    /// decision; this constructor never blocks on the backend.
    pub fn start(
        app: AppHandle,
        backend_exe: Result<PathBuf, SidecarError>,
        token: String,
        data_root: PathBuf,
    ) -> Arc<Self> {
        let supervisor = Arc::new(Self {
            shared: Arc::new(Shared {
                snapshot: Mutex::new(BackendSnapshot {
                    status: BackendStatus::Starting,
                    message: None,
                    port: None,
                    version: None,
                }),
            }),
            stop: Arc::new(AtomicBool::new(false)),
            worker: Mutex::new(None),
        });

        let worker = thread::Builder::new()
            .name("floattranslate-sidecar-supervisor".to_string())
            .spawn({
                let supervisor = Arc::clone(&supervisor);
                let app_for_thread = app.clone();
                move || supervise_loop(supervisor, app_for_thread, backend_exe, token, data_root)
            });

        match worker {
            Ok(handle) => {
                *supervisor
                    .worker
                    .lock()
                    .unwrap_or_else(|poisoned| poisoned.into_inner()) = Some(handle);
            }
            Err(err) => {
                // Extremely unlikely (thread table exhaustion); report as a
                // terminal failure so the UI is not left in "starting" forever.
                fail_and_stop(
                    &supervisor,
                    &app,
                    format!("failed to start sidecar supervisor thread: {err}"),
                );
            }
        }
        supervisor
    }

    /// Current sidecar snapshot.
    pub fn snapshot(&self) -> BackendSnapshot {
        self.shared.lock().clone()
    }

    /// Signals the supervision thread to stop (which terminates any running
    /// backend child) and joins it. Idempotent; called on app exit.
    pub fn shutdown(&self) {
        self.stop.store(true, Ordering::SeqCst);
        let worker = self
            .worker
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .take();
        if let Some(worker) = worker {
            let _ = worker.join();
        }
    }
}

enum ExitPhase {
    /// Stop flag observed: everything already cleaned up.
    Shutdown,
    /// Child died while the app was running (needs a restart attempt).
    UnexpectedExit(String),
}

fn supervise_loop(
    supervisor: Arc<Supervisor>,
    app: AppHandle,
    backend_exe: Result<PathBuf, SidecarError>,
    token: String,
    data_root: PathBuf,
) {
    let mut attempts: usize = 0;
    let mut consecutive_failures: usize = 0;

    loop {
        if supervisor.stop.load(Ordering::SeqCst) {
            return;
        }

        // Resolve the exe on every attempt so a later-installed backend is
        // picked up; resolution errors are terminal (retrying cannot help).
        let exe_path = match &backend_exe {
            Ok(path) => path.clone(),
            Err(err) => {
                fail_and_stop(&supervisor, &app, missing_backend_message(err));
                return;
            }
        };
        if !exe_path.is_file() {
            let not_found = SidecarError::BackendNotFound {
                candidates: exe_path.display().to_string(),
            };
            fail_and_stop(&supervisor, &app, missing_backend_message(&not_found));
            return;
        }

        let phase_status = if attempts == 0 {
            BackendStatus::Starting
        } else {
            BackendStatus::Restarting
        };
        attempts += 1;
        set_snapshot_and_emit(
            &supervisor,
            &app,
            BackendSnapshot {
                status: phase_status,
                message: None,
                port: None,
                version: None,
            },
        );

        let mut command = build_backend_command(&exe_path, &token, &data_root);
        match spawn_and_handshake(&mut command, &supervisor.stop) {
            Ok((mut child, info)) => {
                log_line(&format!(
                    "backend READY: port={} version={}",
                    info.port, info.version
                ));
                set_snapshot_and_emit(
                    &supervisor,
                    &app,
                    BackendSnapshot {
                        status: BackendStatus::Ready,
                        message: None,
                        port: Some(info.port),
                        version: Some(info.version),
                    },
                );

                match supervise_until_exit(&mut child, &supervisor.stop) {
                    ExitPhase::Shutdown => {
                        terminate(&mut child);
                        log_line("backend terminated for app exit");
                        return;
                    }
                    ExitPhase::UnexpectedExit(reason) => {
                        log_line(&format!("backend exited unexpectedly: {reason}"));
                    }
                }
            }
            Err(SidecarError::ShutdownRequested) => return,
            Err(err) => {
                log_line(&format!("backend start attempt failed: {err}"));
            }
        }

        consecutive_failures += 1;
        if consecutive_failures > RESTART_BACKOFF_SECS.len() {
            fail_and_stop(
                &supervisor,
                &app,
                format!(
                    "backend keeps failing after {} restart attempts; giving up",
                    RESTART_BACKOFF_SECS.len()
                ),
            );
            return;
        }

        let delay = Duration::from_secs(RESTART_BACKOFF_SECS[consecutive_failures - 1]);
        let message = format!("restarting in {delay:?}");
        log_line(&format!("backend {message}"));
        set_snapshot_and_emit(
            &supervisor,
            &app,
            BackendSnapshot {
                status: BackendStatus::Restarting,
                message: Some(message),
                port: None,
                version: None,
            },
        );
        if !wait_respecting_shutdown(&supervisor.stop, delay) {
            return;
        }
    }
}

/// Waits for the child to exit or the stop flag to be set (polling keeps this
/// deadlock-free without an extra reaper thread).
fn supervise_until_exit(child: &mut Child, stop: &AtomicBool) -> ExitPhase {
    loop {
        if stop.load(Ordering::SeqCst) {
            return ExitPhase::Shutdown;
        }
        match child.try_wait() {
            Ok(Some(status)) => {
                return ExitPhase::UnexpectedExit(format!("exit code {status:?}"));
            }
            Ok(None) => thread::sleep(SUPERVISE_POLL),
            Err(err) => {
                return ExitPhase::UnexpectedExit(format!("try_wait failed: {err}"));
            }
        }
    }
}

/// Sleeps in small slices so shutdown stays responsive. Returns `false` when
/// shutdown was requested.
fn wait_respecting_shutdown(stop: &AtomicBool, total: Duration) -> bool {
    let deadline = Instant::now() + total;
    loop {
        if stop.load(Ordering::SeqCst) {
            return false;
        }
        let now = Instant::now();
        if now >= deadline {
            return true;
        }
        thread::sleep(SUPERVISE_POLL.min(deadline.saturating_duration_since(now)));
    }
}

fn set_snapshot(supervisor: &Supervisor, snapshot: BackendSnapshot) {
    *supervisor.shared.lock() = snapshot;
}

fn set_snapshot_and_emit(supervisor: &Supervisor, app: &AppHandle, snapshot: BackendSnapshot) {
    let status = snapshot.status;
    let message = snapshot.message.clone();
    set_snapshot(supervisor, snapshot);
    emit_status(app, status, message.as_deref());
}

fn emit_status(app: &AppHandle, status: BackendStatus, message: Option<&str>) {
    let payload = BackendStatusPayload {
        status: status.as_str(),
        message: message.map(str::to_string),
    };
    if let Err(err) = app.emit(events::BACKEND_STATUS, payload) {
        log_line(&format!("failed to emit {}: {err}", events::BACKEND_STATUS));
    }
}

/// Terminal failure: update snapshot, emit `backend-status: failed` **and**
/// `backend-failed` with the message (frozen contract).
fn fail_and_stop(supervisor: &Supervisor, app: &AppHandle, message: String) {
    log_line(&format!("backend failed: {message}"));
    set_snapshot(
        supervisor,
        BackendSnapshot {
            status: BackendStatus::Failed,
            message: Some(message.clone()),
            port: None,
            version: None,
        },
    );
    emit_status(app, BackendStatus::Failed, Some(&message));
    if let Err(err) = app.emit(events::BACKEND_FAILED, BackendFailedPayload { message }) {
        log_line(&format!("failed to emit {}: {err}", events::BACKEND_FAILED));
    }
}

/// Frozen dev-mode message (docs/00 contract) for a missing backend exe.
fn missing_backend_message(err: &SidecarError) -> String {
    if cfg!(debug_assertions) {
        "后端未找到（开发模式请先构建 backend）".to_string()
    } else {
        format!("后端可执行文件未找到：{err}")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn repo_root_detected_from_target_layout() {
        let guard = tempfile::tempdir().unwrap();
        let base = guard.path().to_path_buf();
        let exe_dir = base
            .join("repo")
            .join("src-tauri")
            .join("target")
            .join("debug");
        let roots = repo_roots_from_exe_dir(&exe_dir);
        assert_eq!(roots, vec![base.join("repo")]);
    }

    #[test]
    fn no_repo_root_for_installed_layout() {
        let guard = tempfile::tempdir().unwrap();
        let exe_dir = guard.path().join("install-root");
        assert!(repo_roots_from_exe_dir(&exe_dir).is_empty());
    }

    #[test]
    fn resolve_prefers_existing_env_candidate() {
        let guard = tempfile::tempdir().unwrap();
        let base = guard.path().to_path_buf();
        let exe_dir = base.join("bin");
        std::fs::create_dir_all(&exe_dir).unwrap();
        let env_exe = base.join("custom").join("my-backend.exe");
        std::fs::create_dir_all(env_exe.parent().unwrap()).unwrap();
        std::fs::write(&env_exe, b"stub").unwrap();
        std::fs::write(exe_dir.join(BACKEND_EXE_NAME), b"stub").unwrap();

        let resolved = resolve_backend_exe(&exe_dir, Some(env_exe.to_str().unwrap())).unwrap();
        assert_eq!(resolved, env_exe);
    }

    #[test]
    fn resolve_falls_back_to_repo_candidate_before_exe_dir() {
        let guard = tempfile::tempdir().unwrap();
        let base = guard.path().to_path_buf();
        let exe_dir = base
            .join("repo")
            .join("src-tauri")
            .join("target")
            .join("debug");
        std::fs::create_dir_all(&exe_dir).unwrap();
        let repo_backend = base.join("repo").join("backend").join("bin");
        std::fs::create_dir_all(&repo_backend).unwrap();
        std::fs::write(repo_backend.join(BACKEND_EXE_NAME), b"stub").unwrap();
        // exe_dir candidate also exists but must lose to the repo candidate.
        std::fs::write(exe_dir.join(BACKEND_EXE_NAME), b"stub").unwrap();

        let resolved = resolve_backend_exe(&exe_dir, None).unwrap();
        assert_eq!(resolved, repo_backend.join(BACKEND_EXE_NAME));
    }

    #[test]
    fn resolve_prefers_resources_candidate_before_exe_dir() {
        let guard = tempfile::tempdir().unwrap();
        let exe_dir = guard.path().join("install");
        std::fs::create_dir_all(exe_dir.join("resources")).unwrap();
        std::fs::write(exe_dir.join("resources").join(BACKEND_EXE_NAME), b"stub").unwrap();
        // exe_dir candidate also exists but must lose to the resources
        // candidate (bundled layout).
        std::fs::write(exe_dir.join(BACKEND_EXE_NAME), b"stub").unwrap();

        let resolved = resolve_backend_exe(&exe_dir, None).unwrap();
        assert_eq!(resolved, exe_dir.join("resources").join(BACKEND_EXE_NAME));
    }

    #[test]
    fn resolve_uses_exe_dir_candidate_last() {
        let guard = tempfile::tempdir().unwrap();
        let exe_dir = guard.path().join("install");
        std::fs::create_dir_all(&exe_dir).unwrap();
        std::fs::write(exe_dir.join(BACKEND_EXE_NAME), b"stub").unwrap();

        let resolved = resolve_backend_exe(&exe_dir, None).unwrap();
        assert_eq!(resolved, exe_dir.join(BACKEND_EXE_NAME));
    }

    #[test]
    fn resolve_lists_all_candidates_when_none_exist() {
        let guard = tempfile::tempdir().unwrap();
        let base = guard.path().to_path_buf();
        let exe_dir = base.join("empty-bin");
        std::fs::create_dir_all(&exe_dir).unwrap();
        let env_path = base.join("missing-dir").join("ghost.exe");

        let err = resolve_backend_exe(&exe_dir, Some(env_path.to_str().unwrap())).unwrap_err();
        match err {
            SidecarError::BackendNotFound { candidates } => {
                assert!(candidates.contains(&env_path.display().to_string()));
                assert!(candidates.contains(&exe_dir.join(BACKEND_EXE_NAME).display().to_string()));
            }
            other => panic!("expected BackendNotFound, got: {other}"),
        }
    }

    #[test]
    fn backend_command_carries_frozen_env_and_stdio() {
        let guard = tempfile::tempdir().unwrap();
        let base = guard.path().to_path_buf();
        let token = "abc123";
        let command = build_backend_command(&base.join("backend.exe"), token, &base);
        // We cannot inspect Command env directly; spawn a probe instead.
        let mut probe = build_backend_command(Path::new("cmd"), token, &base);
        probe
            .args(["/C", "echo %FT_SESSION_TOKEN% %FT_HTTP_PORT%"])
            .stdout(Stdio::piped());
        let _ = command; // constructed successfully
        let output = probe.output().expect("run probe");
        let stdout = String::from_utf8_lossy(&output.stdout);
        assert!(
            stdout.contains(token) && stdout.contains("0"),
            "env not propagated: {stdout}"
        );
    }

    #[test]
    fn status_strings_match_frozen_event_values() {
        assert_eq!(BackendStatus::Starting.as_str(), "starting");
        assert_eq!(BackendStatus::Ready.as_str(), "ready");
        assert_eq!(BackendStatus::Restarting.as_str(), "restarting");
        assert_eq!(BackendStatus::Failed.as_str(), "failed");
    }

    #[test]
    fn backoff_wait_returns_false_on_shutdown() {
        let stop = AtomicBool::new(true);
        assert!(!wait_respecting_shutdown(&stop, Duration::from_secs(5)));
        let running = AtomicBool::new(false);
        assert!(wait_respecting_shutdown(
            &running,
            Duration::from_millis(10)
        ));
    }
}
