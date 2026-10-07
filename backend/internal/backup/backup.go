// Package backup implements the JSON backup format (docs/05 §5-6,
// schemas/backup.schema.json):
//
//	{backup_schema_version, exported_at, app_version, settings,
//	 translation_history, vocabulary, terminology, chats, messages, open_tabs}
//
// A backup NEVER contains the API key, the local session token, credential
// metadata or translation_cache rows (docs/05 §5, docs/08 §7). Assembly and
// validation live here as pure functions; SQLite access stays with the
// repositories and the API layer, which converts repository rows into the
// entry types below.
package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SchemaVersion is the backup format version this build exports. Import
// rejects files with a HIGHER version (BACKUP_VERSION_UNSUPPORTED).
const SchemaVersion = 1

// AppVersion is stamped into every export. Keep it in sync with the released
// app version (config.DefaultVersion); the API export test compares against
// this constant so the two can never drift apart silently.
const AppVersion = "1.1.0"

// Errors surfaced by Validate/ReadFile; the API layer maps them to
// INVALID_REQUEST / BACKUP_VERSION_UNSUPPORTED envelopes.
var (
	// ErrInvalidFormat marks a structurally invalid backup file.
	ErrInvalidFormat = errors.New("backup: invalid format")
	// ErrUnsupportedVersion marks a backup with a newer backup_schema_version.
	ErrUnsupportedVersion = errors.New("backup: unsupported version")
)

// HistoryEntry is one translation_history item.
type HistoryEntry struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	InputText string          `json:"input_text"`
	Result    json.RawMessage `json:"result"`
	Source    string          `json:"source"`
	Model     string          `json:"model"`
	CreatedAt string          `json:"created_at"`
}

// VocabularyEntry is one saved word (word holds the decoded WordTranslation).
type VocabularyEntry struct {
	Lemma        string          `json:"lemma"`
	Word         json.RawMessage `json:"word"`
	SavedAt      string          `json:"saved_at"`
	LastViewedAt string          `json:"last_viewed_at"`
}

// TerminologyEntry is one terminology mapping.
type TerminologyEntry struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ChatEntry is one chat session.
type ChatEntry struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	ConversationContext string `json:"conversation_context"`
	CompactSummary      string `json:"compact_summary"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

// MessageEntry is one chat message; GenerationID is null when absent.
type MessageEntry struct {
	ID               string  `json:"id"`
	ChatID           string  `json:"chat_id"`
	Role             string  `json:"role"`
	Content          string  `json:"content"`
	ReasoningContent string  `json:"reasoning_content"`
	CreatedAt        string  `json:"created_at"`
	GenerationID     *string `json:"generation_id"`
}

// TabEntry is one persisted tab; TranslationID is null when absent.
type TabEntry struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Title         string          `json:"title"`
	TranslationID *string         `json:"translation_id"`
	Payload       json.RawMessage `json:"payload"`
	Position      int             `json:"position"`
	IsActive      bool            `json:"is_active"`
	UpdatedAt     string          `json:"updated_at"`
}

// File is the complete backup document. Settings is the merged non-secret
// settings object (the provider row excluded) as raw JSON.
type File struct {
	BackupSchemaVersion int                `json:"backup_schema_version"`
	ExportedAt          string             `json:"exported_at"`
	AppVersion          string             `json:"app_version"`
	Settings            json.RawMessage    `json:"settings"`
	TranslationHistory  []HistoryEntry     `json:"translation_history"`
	Vocabulary          []VocabularyEntry  `json:"vocabulary"`
	Terminology         []TerminologyEntry `json:"terminology"`
	Chats               []ChatEntry        `json:"chats"`
	Messages            []MessageEntry     `json:"messages"`
	OpenTabs            []TabEntry         `json:"open_tabs"`
}

// requiredKeys is the schemas/backup.schema.json required list.
var requiredKeys = []string{
	"backup_schema_version", "exported_at", "app_version", "settings",
	"translation_history", "vocabulary", "terminology", "chats", "messages",
	"open_tabs",
}

// New builds a backup document with the frozen envelope fields.
func New(settings json.RawMessage, now func() string) *File {
	return &File{
		BackupSchemaVersion: SchemaVersion,
		ExportedAt:          now(),
		AppVersion:          AppVersion,
		Settings:            settings,
		TranslationHistory:  []HistoryEntry{},
		Vocabulary:          []VocabularyEntry{},
		Terminology:         []TerminologyEntry{},
		Chats:               []ChatEntry{},
		Messages:            []MessageEntry{},
		OpenTabs:            []TabEntry{},
	}
}

// Validate checks the minimal contract: all required keys present and
// backup_schema_version an integer the importer understands. version > 1 →
// ErrUnsupportedVersion; anything else structurally wrong → ErrInvalidFormat.
func Validate(doc *File) error {
	if doc == nil {
		return fmt.Errorf("%w: document is null", ErrInvalidFormat)
	}
	if doc.BackupSchemaVersion < 1 {
		return fmt.Errorf("%w: backup_schema_version must be a positive integer", ErrInvalidFormat)
	}
	if doc.BackupSchemaVersion > SchemaVersion {
		return fmt.Errorf("%w: backup_schema_version %d > supported %d",
			ErrUnsupportedVersion, doc.BackupSchemaVersion, SchemaVersion)
	}
	if len(doc.Settings) == 0 {
		return fmt.Errorf("%w: settings missing", ErrInvalidFormat)
	}
	return nil
}

// validateTopLevel enforces the required-keys rule on the raw document
// (before typed decoding) so missing arrays/objects are rejected explicitly.
func validateTopLevel(raw []byte) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("%w: not a JSON object: %v", ErrInvalidFormat, err)
	}
	for _, key := range requiredKeys {
		if _, ok := obj[key]; ok {
			continue
		}
		return fmt.Errorf("%w: missing required key %q", ErrInvalidFormat, key)
	}
	var version int
	if err := json.Unmarshal(obj["backup_schema_version"], &version); err != nil {
		return fmt.Errorf("%w: backup_schema_version must be an integer", ErrInvalidFormat)
	}
	return nil
}

// NormalizeTabs re-normalizes an imported tab set: positions 0..n-1 in the
// order given and exactly one is_active tab (the first one; when none was
// active the first tab becomes active).
func NormalizeTabs(tabs []TabEntry) []TabEntry {
	out := make([]TabEntry, len(tabs))
	copy(out, tabs)
	activeSeen := false
	for i := range out {
		out[i].Position = i
	}
	for i := range out {
		if out[i].IsActive {
			if activeSeen {
				out[i].IsActive = false // exactly one active: keep the first
			}
			activeSeen = true
		}
	}
	if !activeSeen && len(out) > 0 {
		out[0].IsActive = true
	}
	return out
}

// WriteFile writes the backup as UTF-8 JSON (indented for readability) to
// path, creating parent directories as needed.
func WriteFile(path string, doc *File) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("backup: create directories: %w", err)
		}
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: encode: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("backup: write %s: %w", path, err)
	}
	return nil
}

// ReadFile reads and decodes a backup file, enforcing the minimal schema
// check (required keys, integer version) and the version ceiling.
func ReadFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("backup: read %s: %w", path, err)
	}
	if err := validateTopLevel(raw); err != nil {
		return nil, err
	}
	var doc File
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFormat, err)
	}
	if err := Validate(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}
