//go:build windows

package credential

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/danieljoos/wincred"
)

// errCredNotFound is windows.ERROR_NOT_FOUND, the OS error the Credential
// Manager API returns when the target credential does not exist.
const errCredNotFound = syscall.Errno(1168)

// WindowsCredentialStore persists the provider API key in the Windows
// Credential Manager (generic credential, ADR-006).
type WindowsCredentialStore struct {
	target string
}

// NewWindows returns a store bound to the frozen target name.
func NewWindows() *WindowsCredentialStore {
	return &WindowsCredentialStore{target: TargetName}
}

// Load returns the stored secret. found=false when no credential exists.
func (s *WindowsCredentialStore) Load() (string, bool, error) {
	cred, err := wincred.GetGenericCredential(s.target)
	if err != nil {
		if errors.Is(err, errCredNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("credential load %q: %w", s.target, err)
	}
	return string(cred.CredentialBlob), true, nil
}

// Save creates or replaces the credential.
func (s *WindowsCredentialStore) Save(value string) error {
	cred := wincred.NewGenericCredential(s.target)
	cred.UserName = UserName
	cred.CredentialBlob = []byte(value)
	if err := cred.Write(); err != nil {
		return fmt.Errorf("credential save %q: %w", s.target, err)
	}
	return nil
}

// Delete removes the credential; deleting a missing one is a no-op.
func (s *WindowsCredentialStore) Delete() error {
	cred := wincred.NewGenericCredential(s.target)
	if err := cred.Delete(); err != nil {
		if errors.Is(err, errCredNotFound) {
			return nil
		}
		return fmt.Errorf("credential delete %q: %w", s.target, err)
	}
	return nil
}
