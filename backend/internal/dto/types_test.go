package dto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWordTranslationFieldNames(t *testing.T) {
	wt := WordTranslation{
		Word:       "run",
		Lemma:      "run",
		PhoneticUK: "/rʌn/",
		PhoneticUS: "/rʌn/",
		PartsOfSpeech: []PartOfSpeech{
			{Part: "verb", Meanings: []string{"跑"}},
		},
		Synonyms:    []string{"jog"},
		Inflections: []string{"runs"},
	}
	raw, err := json.Marshal(wt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, key := range []string{
		`"word"`, `"lemma"`, `"phonetic_uk"`, `"phonetic_us"`,
		`"parts_of_speech"`, `"synonyms"`, `"inflections"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("WordTranslation JSON missing %s: %s", key, s)
		}
	}
	for _, banned := range []string{"PhoneticUK", "phoneticUk", "partsOfSpeech"} {
		if strings.Contains(s, banned) {
			t.Errorf("WordTranslation JSON must not contain %s", banned)
		}
	}
}

func TestTextTranslationFieldNames(t *testing.T) {
	tt := TextTranslation{
		SourceMarkdown:     "hello",
		TranslatedMarkdown: "你好",
		Segments:           []Segment{{Source: "hello", Translation: "你好"}},
	}
	raw, _ := json.Marshal(tt)
	s := string(raw)
	for _, key := range []string{`"source_markdown"`, `"translated_markdown"`, `"segments"`, `"source"`, `"translation"`} {
		if !strings.Contains(s, key) {
			t.Errorf("TextTranslation JSON missing %s: %s", key, s)
		}
	}
}

func TestTranslationResponseFieldNames(t *testing.T) {
	resp := TranslationResponse{
		TranslationID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Kind:          "word",
		Source:        "model",
		Result:        map[string]any{"word": "run"},
		Model:         "deepseek-flash",
		CreatedAt:     "2026-01-01T00:00:00Z",
	}
	raw, _ := json.Marshal(resp)
	s := string(raw)
	for _, key := range []string{`"translation_id"`, `"kind"`, `"source"`, `"result"`, `"model"`, `"created_at"`} {
		if !strings.Contains(s, key) {
			t.Errorf("TranslationResponse JSON missing %s: %s", key, s)
		}
	}
}

func TestTranslationRequestUnmarshal(t *testing.T) {
	var req TranslationRequest
	body := `{"text":"hello","force_kind":"word","bypass_cache":true}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Text != "hello" || req.ForceKind != "word" || !req.BypassCache {
		t.Errorf("unexpected request: %+v", req)
	}
}

func TestProviderSettingsFieldNames(t *testing.T) {
	view := ProviderSettingsView{
		Mode:             "deepseek",
		BaseURL:          "https://api.deepseek.com",
		TranslationModel: "deepseek-flash",
		ChatModel:        "deepseek-flash",
		APIKeyConfigured: true,
		APIKeyHint:       "sk-...",
	}
	raw, _ := json.Marshal(view)
	s := string(raw)
	for _, key := range []string{`"mode"`, `"base_url"`, `"translation_model"`, `"chat_model"`, `"api_key_configured"`, `"api_key_hint"`} {
		if !strings.Contains(s, key) {
			t.Errorf("ProviderSettingsView JSON missing %s: %s", key, s)
		}
	}
	if strings.Contains(s, `"api_key":`) {
		t.Errorf("ProviderSettingsView must never expose api_key")
	}

	upd := ProviderSettingsUpdate{Mode: "deepseek", APIKey: "sk-secret"}
	raw, _ = json.Marshal(upd)
	if !strings.Contains(string(raw), `"api_key"`) {
		t.Errorf("ProviderSettingsUpdate must accept api_key write-only field")
	}
}

func TestHistoryItemFieldNames(t *testing.T) {
	item := HistoryItem{ID: "id1", Kind: "word", InputText: "run", Result: nil, CreatedAt: "2026-01-01T00:00:00Z"}
	raw, _ := json.Marshal(item)
	s := string(raw)
	for _, key := range []string{`"id"`, `"kind"`, `"input_text"`, `"result"`, `"created_at"`} {
		if !strings.Contains(s, key) {
			t.Errorf("HistoryItem JSON missing %s: %s", key, s)
		}
	}
}
