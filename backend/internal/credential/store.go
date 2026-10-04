// Package credential owns the provider API key secret (ADR-006, docs/05 §4).
// The API key NEVER enters SQLite, logs or backups: it lives in the Windows
// Credential Manager under the stable target name
// "FloatTranslate/ProviderApiKey" with user name "FloatTranslate". Tests and
// non-Windows builds use MemoryCredentialStore.
package credential

// TargetName is the frozen Credential Manager target for the provider API
// key (ADR-006). Never rename it without a migration.
const TargetName = "FloatTranslate/ProviderApiKey"

// UserName is the user name stored alongside the credential.
const UserName = "FloatTranslate"

// Store abstracts secret persistence for the provider API key. Implementations
// must be safe for concurrent use.
//
// Semantics:
//   - Load returns (value, true, nil) when a credential exists,
//     ("", false, nil) when none exists, and a non-nil error on failure.
//   - Save creates or replaces the credential.
//   - Delete removes it; deleting a non-existent credential is a no-op (nil).
type Store interface {
	Load() (string, bool, error)
	Save(value string) error
	Delete() error
}
