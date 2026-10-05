//! Selection capture (docs/00 §3, docs/07 §5).
//!
//! Frozen flow: snapshot the clipboard → simulate `Ctrl+C` on the foreground
//! application → wait for the clipboard to update (short timeout + polling) →
//! read the text → restore the original clipboard → wake the main window →
//! emit `selection-captured` `{"text": ...}`. On any failure the original
//! clipboard is restored (best effort), NO event is emitted and the failure is
//! logged — the user's clipboard is never knowingly destroyed (1.0 guarantees
//! plain text: other formats degrade to CF_UNICODETEXT snapshot/restore).
//!
//! The capture runs on a dedicated worker thread spawned per request (never
//! the main/event thread); overlapping requests are dropped. The core
//! snapshot→wait→restore state machine ([`CaptureEngine`]) is generic over an
//! injectable clipboard/trigger/clock and is unit tested without a desktop;
//! the real Windows paths are exercised by the `FT_SELECTION_E2E=1`-gated
//! integration test at the bottom of this file.
//!
//! Detection of the update: the clipboard sequence number changing proves a
//! new copy happened (this also covers the "selection equals the previous
//! clipboard content" case); when no sequence number is available the state
//! machine falls back to a content diff against the snapshot.

use std::fmt;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;

use tauri::{AppHandle, Emitter};

use crate::events::{SelectionCapturedPayload, SELECTION_CAPTURED};
use crate::log_line;

/// Total time to wait for the clipboard to change after simulating Ctrl+C.
pub const CAPTURE_TIMEOUT: Duration = Duration::from_millis(1200);
/// Poll interval while waiting for the clipboard to change.
pub const POLL_INTERVAL: Duration = Duration::from_millis(50);
/// How long to keep retrying the initial clipboard snapshot before giving up
/// on restoring (a busy clipboard must not abort the capture; docs/07 §5).
pub const SNAPSHOT_RETRY_WINDOW: Duration = Duration::from_millis(300);
/// Settle time before retrying the copy (improvement bug #5: some apps
/// swallow the first simulated Ctrl+C — busy focus, IME, first-keystroke
/// handling — so the copy is sent once more before giving up).
pub const COPY_RETRY_DELAY: Duration = Duration::from_millis(150);

/// Guards against overlapping captures (hotkey pressed twice).
static CAPTURE_IN_PROGRESS: AtomicBool = AtomicBool::new(false);

// ---------------------------------------------------------------------------
// Injectable environment
// ---------------------------------------------------------------------------

/// Clipboard text surface used by the capture state machine.
pub trait ClipboardText: Send + Sync {
    /// Readable text, or `None` when the clipboard holds no readable text
    /// (empty, non-text-only format, or temporarily busy).
    fn read(&self) -> Option<String>;
    /// Replace the clipboard content with `text` (best effort).
    fn write(&self, text: &str) -> Result<(), String>;
    /// Monotonic clipboard mutation counter, when the platform exposes one.
    fn sequence(&self) -> Option<u32>;
}

/// Copies the current selection of the foreground application to the
/// clipboard (simulated Ctrl+C).
pub trait CopyTrigger: Send + Sync {
    fn send_copy(&self) -> Result<(), String>;
}

/// Injectable clock so tests run deterministically.
pub trait CaptureClock: Send + Sync {
    fn now_ms(&self) -> u64;
    fn sleep(&self, duration: Duration);
}

// ---------------------------------------------------------------------------
// Capture state machine (snapshot → wait → restore)
// ---------------------------------------------------------------------------

/// Successful capture result.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CaptureOutcome {
    /// Captured selection text.
    pub text: String,
    /// Whether the original clipboard text was restored. `false` means the
    /// original was unreadable (nothing to restore) or the restore write
    /// failed; both are logged by the caller.
    pub restored: bool,
}

/// Capture failure.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CaptureError {
    /// Simulating Ctrl+C failed. The clipboard was not modified by the
    /// capture itself (a restore is still attempted as a safety net).
    CopyFailed(String),
    /// The clipboard did not update within [`CAPTURE_TIMEOUT`].
    NoUpdate,
}

impl fmt::Display for CaptureError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::CopyFailed(reason) => write!(f, "failed to simulate Ctrl+C: {reason}"),
            Self::NoUpdate => {
                write!(f, "clipboard did not update within {:?}", CAPTURE_TIMEOUT)
            }
        }
    }
}

/// The capture state machine. Generic over clipboard, copy trigger and clock
/// so the snapshot→wait→restore logic is testable without a desktop session.
pub struct CaptureEngine<C: ClipboardText, T: CopyTrigger, K: CaptureClock> {
    clipboard: C,
    trigger: T,
    clock: K,
    timeout: Duration,
    poll: Duration,
    snapshot_retry: Duration,
    /// How many times the simulated Ctrl+C is sent before giving up (bug #5).
    copy_attempts: u32,
    /// Settle delay between copy attempts.
    retry_delay: Duration,
}

impl<C: ClipboardText, T: CopyTrigger, K: CaptureClock> CaptureEngine<C, T, K> {
    /// Engine with the frozen 1.0 timings (copy attempted twice).
    pub fn new(clipboard: C, trigger: T, clock: K) -> Self {
        Self {
            clipboard,
            trigger,
            clock,
            timeout: CAPTURE_TIMEOUT,
            poll: POLL_INTERVAL,
            snapshot_retry: SNAPSHOT_RETRY_WINDOW,
            copy_attempts: 2,
            retry_delay: COPY_RETRY_DELAY,
        }
    }

    /// Runs one capture attempt (docs/07 §5 steps 2–6). Restore is attempted
    /// on every path; the caller decides whether to wake/emit.
    pub fn capture(&self) -> Result<CaptureOutcome, CaptureError> {
        // Step 2: snapshot the clipboard (best effort, with a short retry
        // window; a busy clipboard must not abort the capture).
        let snapshot = self.snapshot_text();
        let initial_sequence = self.clipboard.sequence();

        // Steps 3+4+5: simulate Ctrl+C (retried when the first keystroke is
        // swallowed) and wait for the clipboard to update, then read the text.
        let mut copy_result = self.trigger.send_copy();
        let mut outcome = match copy_result {
            Ok(()) => self.wait_for_update(initial_sequence, snapshot.as_deref()),
            Err(reason) => Err(CaptureError::CopyFailed(reason)),
        };
        if outcome.is_err() {
            for _ in 1..self.copy_attempts {
                self.clock.sleep(self.retry_delay);
                copy_result = self.trigger.send_copy();
                outcome = match copy_result {
                    Ok(()) => self.wait_for_update(initial_sequence, snapshot.as_deref()),
                    Err(reason) => Err(CaptureError::CopyFailed(reason)),
                };
                if outcome.is_ok() {
                    break;
                }
            }
        }

        // Step 6: restore the original clipboard (best effort) on EVERY path.
        let mut restored = false;
        if let Some(original) = snapshot.as_deref() {
            restored = self.clipboard.write(original).is_ok();
        }

        match outcome {
            Ok(text) => Ok(CaptureOutcome { text, restored }),
            Err(err) => Err(err),
        }
    }

    /// Repeatedly tries to read clipboard text for [`SNAPSHOT_RETRY_WINDOW`].
    /// `None` means "no readable snapshot" — restore is then skipped (we must
    /// not guess; emptying could destroy a non-text format).
    fn snapshot_text(&self) -> Option<String> {
        let deadline = self.clock.now_ms() + ms(self.snapshot_retry);
        loop {
            if let Some(text) = self.clipboard.read() {
                return Some(text);
            }
            if self.clock.now_ms() >= deadline {
                return None;
            }
            self.clock.sleep(self.poll);
        }
    }

    /// Polls for the clipboard update: the sequence number changing proves a
    /// fresh copy (even with identical content); without a sequence number a
    /// content diff against the snapshot is used. Text must be readable for
    /// the capture to succeed.
    fn wait_for_update(
        &self,
        initial_sequence: Option<u32>,
        snapshot: Option<&str>,
    ) -> Result<String, CaptureError> {
        let deadline = self.clock.now_ms() + ms(self.timeout);
        loop {
            let sequence = self.clipboard.sequence();
            let text = self.clipboard.read();
            let updated = match (initial_sequence, sequence) {
                (Some(before), Some(after)) => after != before,
                _ => text.as_deref() != snapshot,
            };
            if updated {
                if let Some(text) = text {
                    return Ok(text);
                }
            }
            if self.clock.now_ms() >= deadline {
                return Err(CaptureError::NoUpdate);
            }
            self.clock.sleep(self.poll);
        }
    }
}

fn ms(duration: Duration) -> u64 {
    duration.as_millis() as u64
}

// ---------------------------------------------------------------------------
// Real Windows environment
// ---------------------------------------------------------------------------

/// Real clipboard through `clipboard-win` (CF_UNICODETEXT, opens with internal
/// retries; busy → `None`, which the snapshot retry loop handles).
#[cfg(windows)]
#[derive(Clone, Copy)]
struct WindowsClipboard;

#[cfg(windows)]
impl ClipboardText for WindowsClipboard {
    fn read(&self) -> Option<String> {
        clipboard_win::get_clipboard_string().ok()
    }

    fn write(&self, text: &str) -> Result<(), String> {
        clipboard_win::set_clipboard_string(text).map_err(|err| err.to_string())
    }

    fn sequence(&self) -> Option<u32> {
        clipboard_win::raw::seq_num().map(|num| num.get())
    }
}

/// Simulates Ctrl+C on the foreground application via SendInput. The
/// foreground window is recorded first (docs/07 §5 step 1) for diagnostics;
/// focus is never switched away from the user's app.
#[cfg(windows)]
#[derive(Clone, Copy)]
struct CtrlCTrigger;

#[cfg(windows)]
impl CopyTrigger for CtrlCTrigger {
    fn send_copy(&self) -> Result<(), String> {
        use windows::Win32::UI::Input::KeyboardAndMouse::{
            SendInput, INPUT, VIRTUAL_KEY, VK_CONTROL,
        };
        use windows::Win32::UI::WindowsAndMessaging::GetForegroundWindow;

        // Virtual-Key code for 'C' (0x43); the windows crate does not export
        // the letter VK_ constants.
        const VK_C: VIRTUAL_KEY = VIRTUAL_KEY(0x43);

        unsafe {
            let foreground = GetForegroundWindow();
            let target = if foreground.0.is_null() {
                "none".to_string()
            } else {
                format!("{:p}", foreground.0)
            };
            log_line(&format!(
                "selection capture: simulating Ctrl+C (foreground window {target})"
            ));

            let inputs = [
                keyboard_input(VK_CONTROL, false),
                keyboard_input(VK_C, false),
                keyboard_input(VK_C, true),
                keyboard_input(VK_CONTROL, true),
            ];
            let sent = SendInput(&inputs, std::mem::size_of::<INPUT>() as i32);
            if sent as usize == inputs.len() {
                return Ok(());
            }
            // Partial delivery: release both keys so no modifier stays stuck.
            let releases = [keyboard_input(VK_C, true), keyboard_input(VK_CONTROL, true)];
            SendInput(&releases, std::mem::size_of::<INPUT>() as i32);
            Err(format!(
                "SendInput delivered {sent} of {} events",
                inputs.len()
            ))
        }
    }
}

#[cfg(windows)]
fn keyboard_input(
    vk: windows::Win32::UI::Input::KeyboardAndMouse::VIRTUAL_KEY,
    up: bool,
) -> windows::Win32::UI::Input::KeyboardAndMouse::INPUT {
    use windows::Win32::UI::Input::KeyboardAndMouse::{
        INPUT, INPUT_0, INPUT_KEYBOARD, KEYBDINPUT, KEYBD_EVENT_FLAGS, KEYEVENTF_KEYUP,
    };
    INPUT {
        r#type: INPUT_KEYBOARD,
        Anonymous: INPUT_0 {
            ki: KEYBDINPUT {
                wVk: vk,
                wScan: 0,
                dwFlags: if up {
                    KEYEVENTF_KEYUP
                } else {
                    KEYBD_EVENT_FLAGS(0)
                },
                time: 0,
                dwExtraInfo: 0,
            },
        },
    }
}

/// Wall-clock clock for the production engine.
#[cfg(windows)]
struct StdClock {
    base: std::time::Instant,
}

#[cfg(windows)]
impl StdClock {
    fn new() -> Self {
        Self {
            base: std::time::Instant::now(),
        }
    }
}

#[cfg(windows)]
impl CaptureClock for StdClock {
    fn now_ms(&self) -> u64 {
        self.base.elapsed().as_millis() as u64
    }

    fn sleep(&self, duration: Duration) {
        std::thread::sleep(duration);
    }
}

// ---------------------------------------------------------------------------
// Orchestration
// ---------------------------------------------------------------------------

/// Entry point from the global-hotkey handler: spawns the capture worker and
/// returns immediately (never blocks the caller). On success the main window
/// is woken (shown + focused) BEFORE `selection-captured` reaches the
/// frontend (frozen contract).
pub(crate) fn start_capture(app: AppHandle) {
    if CAPTURE_IN_PROGRESS.swap(true, Ordering::SeqCst) {
        log_line("selection capture already in progress; ignoring hotkey");
        return;
    }
    let spawned = std::thread::Builder::new()
        .name("floattranslate-selection-capture".to_string())
        .spawn(move || {
            let result = run_capture();
            CAPTURE_IN_PROGRESS.store(false, Ordering::SeqCst);
            match result {
                Ok(outcome) => {
                    if !outcome.restored {
                        log_line(
                            "selection capture: previous clipboard could not be restored \
                             (unreadable snapshot or restore write failed)",
                        );
                    }
                    // Wake first, then emit — the frontend must see the window
                    // before the event arrives.
                    crate::show_main_window(&app);
                    if let Err(err) = app.emit(
                        SELECTION_CAPTURED,
                        SelectionCapturedPayload { text: outcome.text },
                    ) {
                        log_line(&format!("failed to emit {SELECTION_CAPTURED}: {err}"));
                    }
                }
                Err(err) => log_line(&format!("selection capture failed: {err}")),
            }
        });
    if let Err(err) = spawned {
        CAPTURE_IN_PROGRESS.store(false, Ordering::SeqCst);
        log_line(&format!("failed to spawn selection capture thread: {err}"));
    }
}

#[cfg(windows)]
fn run_capture() -> Result<CaptureOutcome, CaptureError> {
    CaptureEngine::new(WindowsClipboard, CtrlCTrigger, StdClock::new()).capture()
}

#[cfg(not(windows))]
fn run_capture() -> Result<CaptureOutcome, CaptureError> {
    Err(CaptureError::CopyFailed(
        "selection capture is only implemented on Windows in 1.0".to_string(),
    ))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::AtomicUsize;
    use std::sync::{Arc, Mutex};

    // ------------------------------------------------------------------
    // Mocks
    // ------------------------------------------------------------------

    #[derive(Default)]
    struct MockClipboard {
        text: Mutex<Option<String>>,
        sequence: Mutex<u32>,
        /// Number of initial reads to fail (simulates a busy clipboard).
        read_failures: Mutex<usize>,
        write_calls: Mutex<Vec<String>>,
        /// Restore writes of these texts fail.
        fail_write_of: Mutex<Vec<String>>,
        /// When true, `sequence()` reports no sequence number.
        no_sequence: Mutex<bool>,
    }

    impl MockClipboard {
        fn with_text(text: &str) -> Self {
            let clipboard = Self::default();
            *clipboard.text.lock().unwrap() = Some(text.to_string());
            *clipboard.sequence.lock().unwrap() = 1;
            clipboard
        }

        fn write_calls(&self) -> Vec<String> {
            self.write_calls.lock().unwrap().clone()
        }

        fn text(&self) -> Option<String> {
            self.text.lock().unwrap().clone()
        }
    }

    impl ClipboardText for MockClipboard {
        fn read(&self) -> Option<String> {
            let mut failures = self.read_failures.lock().unwrap();
            if *failures > 0 {
                *failures -= 1;
                return None;
            }
            drop(failures);
            self.text.lock().unwrap().clone()
        }

        fn write(&self, text: &str) -> Result<(), String> {
            self.write_calls.lock().unwrap().push(text.to_string());
            if self.fail_write_of.lock().unwrap().iter().any(|t| t == text) {
                return Err("mock write failure".to_string());
            }
            *self.text.lock().unwrap() = Some(text.to_string());
            *self.sequence.lock().unwrap() += 1;
            Ok(())
        }

        fn sequence(&self) -> Option<u32> {
            if *self.no_sequence.lock().unwrap() {
                None
            } else {
                Some(*self.sequence.lock().unwrap())
            }
        }
    }

    /// Lets the trigger and the assertions share one clipboard instance.
    impl ClipboardText for Arc<MockClipboard> {
        fn read(&self) -> Option<String> {
            (**self).read()
        }

        fn write(&self, text: &str) -> Result<(), String> {
            (**self).write(text)
        }

        fn sequence(&self) -> Option<u32> {
            (**self).sequence()
        }
    }

    /// What the mocked trigger does per `send_copy` call.
    enum MockAction {
        /// Simulates the target app copying: writes through the clipboard.
        Copy(&'static str),
        /// send_copy itself fails.
        Fail,
        /// send_copy succeeds but nothing is copied (no selection).
        Nothing,
    }

    struct MockTrigger {
        clipboard: Arc<MockClipboard>,
        actions: Mutex<Vec<MockAction>>,
        calls: AtomicUsize,
    }

    impl MockTrigger {
        fn scripted(clipboard: Arc<MockClipboard>, actions: Vec<MockAction>) -> Self {
            Self {
                clipboard,
                actions: Mutex::new(actions),
                calls: AtomicUsize::new(0),
            }
        }
    }

    impl CopyTrigger for MockTrigger {
        fn send_copy(&self) -> Result<(), String> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            let mut actions = self.actions.lock().unwrap();
            let action = if actions.is_empty() {
                MockAction::Nothing
            } else {
                actions.remove(0)
            };
            match action {
                MockAction::Copy(text) => self.clipboard.write(text),
                MockAction::Fail => Err("mock SendInput failure".to_string()),
                MockAction::Nothing => Ok(()),
            }
        }
    }

    /// Deterministic clock: sleeps advance virtual time.
    struct MockClock {
        now: Mutex<u64>,
        sleeps: AtomicUsize,
    }

    impl MockClock {
        fn new() -> Self {
            Self {
                now: Mutex::new(0),
                sleeps: AtomicUsize::new(0),
            }
        }

        fn sleeps(&self) -> usize {
            self.sleeps.load(Ordering::SeqCst)
        }
    }

    impl CaptureClock for MockClock {
        fn now_ms(&self) -> u64 {
            *self.now.lock().unwrap()
        }

        fn sleep(&self, duration: Duration) {
            *self.now.lock().unwrap() += duration.as_millis() as u64;
            self.sleeps.fetch_add(1, Ordering::SeqCst);
        }
    }

    fn engine(
        clipboard: Arc<MockClipboard>,
        actions: Vec<MockAction>,
    ) -> CaptureEngine<Arc<MockClipboard>, MockTrigger, MockClock> {
        let trigger = MockTrigger::scripted(Arc::clone(&clipboard), actions);
        CaptureEngine::new(clipboard, trigger, MockClock::new())
    }

    // ------------------------------------------------------------------
    // Tests
    // ------------------------------------------------------------------

    #[test]
    fn captures_updated_text_and_restores_original() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        let result = engine(clipboard.clone(), vec![MockAction::Copy("new")]).capture();
        assert_eq!(
            result,
            Ok(CaptureOutcome {
                text: "new".to_string(),
                restored: true,
            })
        );
        assert_eq!(clipboard.text(), Some("old".to_string()));
    }

    #[test]
    fn captures_identical_text_via_sequence_number() {
        // The selection equals the previous clipboard content; only the
        // sequence number proves the fresh copy.
        let clipboard = Arc::new(MockClipboard::with_text("same"));
        let result = engine(clipboard.clone(), vec![MockAction::Copy("same")]).capture();
        assert_eq!(
            result,
            Ok(CaptureOutcome {
                text: "same".to_string(),
                restored: true,
            })
        );
        assert_eq!(clipboard.text(), Some("same".to_string()));
    }

    #[test]
    fn times_out_when_nothing_is_copied_and_restores() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        let state = engine(clipboard.clone(), vec![]);
        let result = state.capture();
        assert_eq!(result, Err(CaptureError::NoUpdate));
        assert_eq!(clipboard.text(), Some("old".to_string()));
        assert_eq!(clipboard.write_calls(), vec!["old".to_string()]);
        // Two copy attempts (bug #5 retry): 1200ms timeout / 50ms poll = 24
        // sleeps each; the MockClock retry settle is ONE sleep call (150ms).
        assert_eq!(state.clock.sleeps(), 49);
    }

    #[test]
    fn retries_the_copy_when_the_first_keystroke_is_swallowed() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        // First send_copy produces nothing; the second one copies.
        let result = engine(
            clipboard.clone(),
            vec![MockAction::Nothing, MockAction::Copy("late")],
        )
        .capture();
        assert_eq!(
            result,
            Ok(CaptureOutcome {
                text: "late".to_string(),
                restored: true,
            })
        );
        assert_eq!(clipboard.text(), Some("old".to_string()));
    }

    #[test]
    fn no_update_after_retry_reports_timeout() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        let result = engine(clipboard.clone(), vec![MockAction::Nothing]).capture();
        assert_eq!(result, Err(CaptureError::NoUpdate));
        assert_eq!(clipboard.text(), Some("old".to_string()));
    }

    #[test]
    fn skips_restore_when_snapshot_unavailable() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        // The snapshot window allows 7 reads (0..=300ms at 50ms steps); all
        // fail, so the capture proceeds without a restorable snapshot while
        // later poll reads succeed.
        *clipboard.read_failures.lock().unwrap() = 7;
        let result = engine(clipboard.clone(), vec![MockAction::Copy("new")]).capture();
        assert_eq!(
            result,
            Ok(CaptureOutcome {
                text: "new".to_string(),
                restored: false,
            })
        );
        // The only write is the simulated copy itself; no restore write of
        // "old" happened (we must not guess the original content).
        assert_eq!(clipboard.write_calls(), vec!["new".to_string()]);
        assert_eq!(clipboard.text(), Some("new".to_string()));
    }

    #[test]
    fn restore_failure_does_not_fail_the_capture() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        clipboard
            .fail_write_of
            .lock()
            .unwrap()
            .push("old".to_string());
        let result = engine(clipboard.clone(), vec![MockAction::Copy("new")]).capture();
        assert_eq!(
            result,
            Ok(CaptureOutcome {
                text: "new".to_string(),
                restored: false,
            })
        );
        assert_eq!(clipboard.text(), Some("new".to_string()));
    }

    #[test]
    fn copy_failure_is_retried_then_reported_with_restore_attempted() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        // Bug #5: even a SendInput failure gets one retry (transient OS-level
        // copy failures happen); only when BOTH attempts fail does the error
        // surface — and the restore is still attempted.
        let result = engine(clipboard.clone(), vec![MockAction::Fail, MockAction::Fail]).capture();
        assert_eq!(
            result,
            Err(CaptureError::CopyFailed(
                "mock SendInput failure".to_string()
            ))
        );
        assert_eq!(clipboard.text(), Some("old".to_string()));
        assert_eq!(clipboard.write_calls(), vec!["old".to_string()]);
    }

    #[test]
    fn copy_failure_recovers_on_the_retry() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        // First send fails (transient), the retry copies successfully.
        let result = engine(
            clipboard.clone(),
            vec![MockAction::Fail, MockAction::Copy("recovered")],
        )
        .capture();
        assert_eq!(
            result,
            Ok(CaptureOutcome {
                text: "recovered".to_string(),
                restored: true,
            })
        );
    }

    #[test]
    fn snapshot_retries_busy_clipboard() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        // Three busy reads, then the snapshot succeeds.
        *clipboard.read_failures.lock().unwrap() = 3;
        let state = engine(clipboard.clone(), vec![MockAction::Copy("new")]);
        let result = state.capture();
        assert_eq!(
            result,
            Ok(CaptureOutcome {
                text: "new".to_string(),
                restored: true,
            })
        );
        // Three failed snapshot reads, each retried after one poll sleep.
        assert_eq!(state.clock.sleeps(), 3);
    }

    #[test]
    fn falls_back_to_content_diff_without_sequence_number() {
        let clipboard = Arc::new(MockClipboard::with_text("old"));
        *clipboard.no_sequence.lock().unwrap() = true;
        let result = engine(clipboard.clone(), vec![MockAction::Copy("new")]).capture();
        assert_eq!(
            result,
            Ok(CaptureOutcome {
                text: "new".to_string(),
                restored: true,
            })
        );
    }

    #[test]
    fn content_diff_misses_identical_text_without_sequence_number() {
        // Documented limitation: without a sequence number, a copy that
        // produces exactly the snapshot content cannot be distinguished from
        // "nothing happened" and times out.
        let clipboard = Arc::new(MockClipboard::with_text("same"));
        *clipboard.no_sequence.lock().unwrap() = true;
        let result = engine(clipboard, vec![MockAction::Copy("same")]).capture();
        assert_eq!(result, Err(CaptureError::NoUpdate));
    }

    // ------------------------------------------------------------------
    // Real-desktop integration (gated)
    // ------------------------------------------------------------------

    /// Simulates the target app's copy by writing directly — exercises the
    /// REAL clipboard save/restore through the state machine without needing
    /// a focused text selection.
    #[cfg(windows)]
    struct FakeCopyTrigger(&'static str);

    #[cfg(windows)]
    impl CopyTrigger for FakeCopyTrigger {
        fn send_copy(&self) -> Result<(), String> {
            WindowsClipboard.write(self.0)
        }
    }

    /// Read-only probe of the real clipboard paths (read + sequence number).
    /// Gated like the full E2E but safe to run in any session: it never
    /// writes the clipboard and never sends keystrokes.
    #[test]
    #[cfg(windows)]
    #[ignore = "requires a real desktop session; opt in with FT_SELECTION_E2E=1"]
    fn real_clipboard_read_only_probe() {
        if std::env::var("FT_SELECTION_E2E").ok().as_deref() != Some("1") {
            return; // gate: run only when explicitly requested
        }
        let clipboard = WindowsClipboard;
        let text = clipboard.read();
        log_line(&format!(
            "E2E read-only probe: clipboard text readable={} chars={}",
            text.is_some(),
            text.as_ref().map(|t| t.chars().count()).unwrap_or(0)
        ));
        let sequence = clipboard.sequence();
        assert!(
            sequence.is_some(),
            "GetClipboardSequenceNumber must be available in a desktop session"
        );
        // Reading twice must not change the sequence number.
        assert_eq!(clipboard.sequence(), sequence);
    }

    /// Real SendInput/clipboard round trip. Gated: run with
    /// `FT_SELECTION_E2E=1 cargo test -- --ignored` on a real desktop
    /// session. The Ctrl+C phase sends real keystrokes to whatever window has
    /// focus; without a text selection it simply times out, which is an
    /// accepted outcome here.
    #[test]
    #[cfg(windows)]
    #[ignore = "requires a real desktop session; opt in with FT_SELECTION_E2E=1"]
    fn real_clipboard_save_capture_restore_roundtrip() {
        if std::env::var("FT_SELECTION_E2E").ok().as_deref() != Some("1") {
            return; // gate: run only when explicitly requested
        }
        let clipboard = WindowsClipboard;
        let original = clipboard.read();

        clipboard
            .write("FloatTranslate E2E guard")
            .expect("set guard clipboard text");

        // Phase 1: real clipboard, simulated copy (no SendInput).
        let engine = CaptureEngine::new(
            clipboard,
            FakeCopyTrigger("FloatTranslate E2E captured"),
            StdClock::new(),
        );
        let outcome = engine.capture().expect("fake-copy capture must succeed");
        assert_eq!(outcome.text, "FloatTranslate E2E captured");
        assert!(outcome.restored, "guard text must be restored");
        assert_eq!(
            clipboard.read().as_deref(),
            Some("FloatTranslate E2E guard")
        );

        // Phase 2: real SendInput Ctrl+C. Without a selection target this
        // times out; the guard text must survive either way.
        let engine = CaptureEngine::new(clipboard, CtrlCTrigger, StdClock::new());
        match engine.capture() {
            Ok(outcome) => log_line(&format!(
                "E2E: real Ctrl+C captured {} chars",
                outcome.text.chars().count()
            )),
            Err(CaptureError::NoUpdate) => {}
            Err(err) => panic!("unexpected E2E error: {err}"),
        }
        assert_eq!(
            clipboard.read().as_deref(),
            Some("FloatTranslate E2E guard")
        );

        // Give the user their clipboard back (best effort).
        match original {
            Some(text) => {
                clipboard.write(&text).expect("restore original clipboard");
            }
            None => {
                let _ = clipboard.write("");
            }
        }
    }
}
