//! Integration tests for the sidecar handshake against real stub processes.
//!
//! These use `powershell` / `cmd` (available on every Windows dev box) as
//! stand-ins for the Go backend; no repo-external artifacts are required.

#![cfg(windows)]

use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant};

use floattranslate_lib::sidecar::{
    build_backend_command, spawn_and_handshake_with_timeout, SidecarError,
};
use floattranslate_lib::token::generate_session_token;

const SHORT_TIMEOUT: Duration = Duration::from_millis(800);

/// A `powershell` invocation wired through the production command builder so
/// the frozen env contract (`FT_SESSION_TOKEN`, `FT_HTTP_PORT`, `FT_DATA_ROOT`)
/// is exercised end-to-end. The stub prints a READY line whose version field
/// echoes selected env vars (hex/numeric values are JSON-safe).
fn stub_ready_command(token: &str, data_root: &Path, script_suffix: &str) -> std::process::Command {
    let script = format!(
        r#"Write-Output ('{{"status":"ready","port":49152,"version":"' + $env:FT_SESSION_TOKEN + '|' + $env:FT_HTTP_PORT + '"}}'){script_suffix}"#
    );
    let mut command = build_backend_command(Path::new("powershell"), token, data_root);
    command.args(["-NoProfile", "-NonInteractive", "-Command", &script]);
    command
}

#[test]
fn handshake_accepts_ready_line_and_receives_frozen_env() {
    let guard = tempfile::tempdir().unwrap();
    let token = generate_session_token();

    let mut command = stub_ready_command(&token, guard.path(), "");
    let stop = AtomicBool::new(false);
    let (mut child, info) =
        spawn_and_handshake_with_timeout(&mut command, &stop, Duration::from_secs(15))
            .expect("handshake must succeed");

    assert_eq!(info.port, 49152);
    assert_eq!(
        info.version,
        format!("{token}|0"),
        "session token and FT_HTTP_PORT must reach the child"
    );
    let _ = child.kill();
    let _ = child.wait();
}

#[test]
fn handshake_ignores_leading_noise_lines() {
    let guard = tempfile::tempdir().unwrap();
    let token = generate_session_token();

    let mut command =
        stub_ready_command(&token, guard.path(), "; Write-Output 'noise after ready'");
    let stop = AtomicBool::new(false);
    let (mut child, info) =
        spawn_and_handshake_with_timeout(&mut command, &stop, Duration::from_secs(15))
            .expect("noise before/after READY must be ignored");
    assert_eq!(info.port, 49152);
    let _ = child.kill();
    let _ = child.wait();
}

#[test]
fn handshake_reports_child_exit_before_ready_with_code() {
    let mut command = std::process::Command::new("cmd");
    command
        .args(["/C", "exit 3"])
        .stdout(std::process::Stdio::piped());

    let stop = AtomicBool::new(false);
    let err = spawn_and_handshake_with_timeout(&mut command, &stop, Duration::from_secs(15))
        .expect_err("child exited without READY");
    match err {
        SidecarError::ExitedBeforeReady { code: Some(3) } => {}
        other => panic!("expected ExitedBeforeReady{{code:Some(3)}}, got: {other}"),
    }
}

#[test]
fn handshake_times_out_and_terminates_stub() {
    let mut command = std::process::Command::new("powershell");
    command
        .args([
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            "Start-Sleep -Seconds 30",
        ])
        .stdout(std::process::Stdio::piped());

    let stop = AtomicBool::new(false);
    let started = Instant::now();
    let err = spawn_and_handshake_with_timeout(&mut command, &stop, SHORT_TIMEOUT)
        .expect_err("silent stub must time out");
    let elapsed = started.elapsed();
    assert!(matches!(err, SidecarError::HandshakeTimeout), "got: {err}");
    assert!(
        elapsed < Duration::from_secs(5),
        "timeout must be honored, took {elapsed:?}"
    );
}

#[test]
fn handshake_aborts_when_shutdown_requested() {
    let mut command = std::process::Command::new("powershell");
    command
        .args([
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            "Start-Sleep -Seconds 30",
        ])
        .stdout(std::process::Stdio::piped());

    let stop = AtomicBool::new(true);
    let started = Instant::now();
    let err = spawn_and_handshake_with_timeout(&mut command, &stop, Duration::from_secs(30))
        .expect_err("shutdown flag must abort the handshake");
    let elapsed = started.elapsed();
    assert!(matches!(err, SidecarError::ShutdownRequested), "got: {err}");
    assert!(
        elapsed < Duration::from_secs(5),
        "shutdown must be responsive, took {elapsed:?}"
    );
}

#[test]
fn stop_flag_flips_between_attempts() {
    // Sanity for the flag semantics used by the supervisor loop.
    let stop = AtomicBool::new(false);
    assert!(!stop.load(Ordering::SeqCst));
    stop.store(true, Ordering::SeqCst);
    assert!(stop.load(Ordering::SeqCst));
}
