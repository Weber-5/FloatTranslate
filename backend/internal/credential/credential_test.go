package credential

import "testing"

func TestMemoryRoundTrip(t *testing.T) {
	s := NewMemory()

	value, found, err := s.Load()
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if found || value != "" {
		t.Fatalf("empty store reported found=%v value=%q", found, value)
	}

	if err := s.Save("sk-test-value-42"); err != nil {
		t.Fatalf("save: %v", err)
	}
	value, found, err = s.Load()
	if err != nil || !found || value != "sk-test-value-42" {
		t.Fatalf("after save: found=%v value=%q err=%v", found, value, err)
	}

	// Save replaces.
	if err := s.Save("sk-second-value"); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	value, _, _ = s.Load()
	if value != "sk-second-value" {
		t.Fatalf("after replace: %q", value)
	}

	// Delete removes and is idempotent.
	if err := s.Delete(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Delete(); err != nil {
		t.Fatalf("delete twice: %v", err)
	}
	if _, found, _ := s.Load(); found {
		t.Fatal("credential still present after delete")
	}
}

// TestWindowsCredentialStoreE2E exercises the real Windows Credential
// Manager. It is gated behind FT_CRED_E2E=1 so ordinary runs never touch the
// user's credential vault; the smoke pipeline sets it.
func TestWindowsCredentialStoreE2E(t *testing.T) {
	if !windowsE2EEnabled {
		t.Skip("set FT_CRED_E2E=1 to run against the real Credential Manager")
	}
	s := NewWindows()

	// Start from a clean slate (idempotent delete).
	if err := s.Delete(); err != nil {
		t.Fatalf("initial delete: %v", err)
	}

	if _, found, err := s.Load(); err != nil || found {
		t.Fatalf("expected missing credential, found=%v err=%v", found, err)
	}

	if err := s.Save("sk-e2e-smoke-key-9183"); err != nil {
		t.Fatalf("save: %v", err)
	}
	value, found, err := s.Load()
	if err != nil || !found {
		t.Fatalf("load after save: found=%v err=%v", found, err)
	}
	if value != "sk-e2e-smoke-key-9183" {
		t.Fatalf("loaded value = %q", value)
	}

	if err := s.Save("sk-e2e-replaced"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if value, _, _ := s.Load(); value != "sk-e2e-replaced" {
		t.Fatalf("after replace = %q", value)
	}

	if err := s.Delete(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Delete(); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
	if _, found, _ := s.Load(); found {
		t.Fatal("credential still present after delete")
	}
}
