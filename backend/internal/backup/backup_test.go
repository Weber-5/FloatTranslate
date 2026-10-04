package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleDoc(t *testing.T) *File {
	t.Helper()
	doc := New(json.RawMessage(`{"theme":"dark","proxy_mode":"none"}`), func() string {
		return "2026-10-04T00:00:00Z"
	})
	doc.TranslationHistory = append(doc.TranslationHistory, HistoryEntry{
		ID: "h1", Kind: "word", InputText: "run",
		Result: json.RawMessage(`{"word":"run"}`), Source: "model", Model: "deepseek-flash",
		CreatedAt: "2026-10-04T00:00:00Z",
	})
	doc.Vocabulary = append(doc.Vocabulary, VocabularyEntry{
		Lemma: "run", Word: json.RawMessage(`{"word":"run"}`),
		SavedAt: "2026-10-04T00:00:00Z", LastViewedAt: "2026-10-04T00:00:00Z",
	})
	doc.Terminology = append(doc.Terminology, TerminologyEntry{
		ID: "t1", Source: "API", Target: "接口",
		CreatedAt: "2026-10-04T00:00:00Z", UpdatedAt: "2026-10-04T00:00:00Z",
	})
	doc.Chats = append(doc.Chats, ChatEntry{ID: "c1", Title: "chat"})
	gid := "g1"
	doc.Messages = append(doc.Messages, MessageEntry{
		ID: "m1", ChatID: "c1", Role: "user", Content: "hi", CreatedAt: "2026-10-04T00:00:00Z", GenerationID: &gid,
	})
	doc.OpenTabs = append(doc.OpenTabs, TabEntry{ID: "tab1", Kind: "word", Title: "run", Payload: json.RawMessage(`{}`)})
	return doc
}

// jsonEqual compares two raw JSON values semantically.
func jsonEqual(a, b []byte) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	ea, _ := json.Marshal(va)
	eb, _ := json.Marshal(vb)
	return string(ea) == string(eb)
}

func TestWriteReadRoundTrip(t *testing.T) {
	doc := sampleDoc(t)
	path := filepath.Join(t.TempDir(), "nested", "dir", "backup.json")
	if err := WriteFile(path, doc); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.BackupSchemaVersion != 1 || got.ExportedAt != doc.ExportedAt || got.AppVersion != AppVersion {
		t.Errorf("envelope wrong: %+v", got)
	}
	if !jsonEqual(got.Settings, doc.Settings) {
		t.Errorf("settings = %s, want %s", got.Settings, doc.Settings)
	}
	if len(got.TranslationHistory) != 1 || got.TranslationHistory[0].ID != "h1" {
		t.Errorf("history wrong: %+v", got.TranslationHistory)
	}
	if len(got.Messages) != 1 || got.Messages[0].GenerationID == nil || *got.Messages[0].GenerationID != "g1" {
		t.Errorf("messages wrong: %+v", got.Messages)
	}

	// The file is UTF-8 JSON whose top-level key set matches the schema
	// exactly (additionalProperties=false).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("file is not JSON: %v", err)
	}
	wantKeys := map[string]bool{
		"backup_schema_version": true, "exported_at": true, "app_version": true,
		"settings": true, "translation_history": true, "vocabulary": true,
		"terminology": true, "chats": true, "messages": true, "open_tabs": true,
	}
	for key := range obj {
		if !wantKeys[key] {
			t.Errorf("unexpected top-level key %q", key)
		}
		delete(wantKeys, key)
	}
	for key := range wantKeys {
		t.Errorf("missing required key %q", key)
	}
}

func TestReadFileRejects(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return path
	}

	// Newer schema version.
	unsupported := write("v9.json", `{"backup_schema_version":9,"exported_at":"x","app_version":"x","settings":{},"translation_history":[],"vocabulary":[],"terminology":[],"chats":[],"messages":[],"open_tabs":[]}`)
	if _, err := ReadFile(unsupported); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("v9 error = %v, want ErrUnsupportedVersion", err)
	}

	// Missing required key.
	missing := write("missing.json", `{"backup_schema_version":1,"settings":{}}`)
	if _, err := ReadFile(missing); !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("missing-key error = %v, want ErrInvalidFormat", err)
	}

	// Non-integer version.
	badVersion := write("badver.json", `{"backup_schema_version":"1","settings":{}}`)
	if _, err := ReadFile(badVersion); !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("string version error = %v, want ErrInvalidFormat", err)
	}

	// Not a JSON object.
	notObj := write("notobj.json", `[1,2,3]`)
	if _, err := ReadFile(notObj); !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("array error = %v, want ErrInvalidFormat", err)
	}

	// Non-existent file.
	if _, err := ReadFile(filepath.Join(dir, "nope.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file error = %v, want os.ErrNotExist", err)
	}
}

func TestNormalizeTabs(t *testing.T) {
	t.Run("positions renormalized, none active → first active", func(t *testing.T) {
		tabs := []TabEntry{
			{ID: "a", Position: 7},
			{ID: "b", Position: 3},
			{ID: "c", Position: 5},
		}
		got := NormalizeTabs(tabs)
		for i, tab := range got {
			if tab.Position != i {
				t.Errorf("tab %d position = %d, want %d", i, tab.Position, i)
			}
		}
		if !got[0].IsActive || got[1].IsActive || got[2].IsActive {
			t.Errorf("exactly the first tab must be active: %+v", got)
		}
	})

	t.Run("multiple active → keep the first only", func(t *testing.T) {
		tabs := []TabEntry{
			{ID: "a", IsActive: false},
			{ID: "b", IsActive: true},
			{ID: "c", IsActive: true},
		}
		got := NormalizeTabs(tabs)
		if !got[1].IsActive || got[0].IsActive || got[2].IsActive {
			t.Errorf("first active must win: %+v", got)
		}
		if got[0].Position != 0 || got[1].Position != 1 || got[2].Position != 2 {
			t.Errorf("positions = %d %d %d", got[0].Position, got[1].Position, got[2].Position)
		}
	})

	t.Run("input not mutated", func(t *testing.T) {
		tabs := []TabEntry{{ID: "a", Position: 9}, {ID: "b", IsActive: true, Position: 4}}
		_ = NormalizeTabs(tabs)
		if tabs[0].Position != 9 || tabs[1].Position != 4 {
			t.Error("NormalizeTabs must not mutate its input")
		}
	})

	t.Run("empty stays empty", func(t *testing.T) {
		if got := NormalizeTabs(nil); len(got) != 0 {
			t.Errorf("NormalizeTabs(nil) = %+v", got)
		}
	})
}

func TestValidate(t *testing.T) {
	doc := sampleDoc(t)
	if err := Validate(doc); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
	doc.BackupSchemaVersion = 2
	if err := Validate(doc); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("version 2 error = %v, want ErrUnsupportedVersion", err)
	}
	doc.BackupSchemaVersion = 0
	if err := Validate(doc); !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("version 0 error = %v, want ErrInvalidFormat", err)
	}
	if err := Validate(nil); !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("nil error = %v, want ErrInvalidFormat", err)
	}
}

func TestFileContainsNoSecretFields(t *testing.T) {
	// Static guarantee: the marshaled shape of a fully populated document
	// carries no credential-ish keys anywhere.
	doc := sampleDoc(t)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{"api_key", "session_token", "authorization", "credential", "api_key_configured", "api_key_hint"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Errorf("backup JSON contains banned key fragment %q:\n%s", banned, raw)
		}
	}
}
