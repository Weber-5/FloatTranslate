//! TODO(Phase 3): clipboard + selection capture (docs/07 §5).
//!
//! Algorithm: stash clipboard → simulate `Ctrl+C` → poll for clipboard update
//! → restore original clipboard → wake the window → emit
//! `selection-captured` ([`crate::events::SELECTION_CAPTURED`]). Must never
//! destroy the user's clipboard on failure. Phase 1 placeholder.
