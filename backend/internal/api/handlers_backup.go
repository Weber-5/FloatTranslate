package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/backup"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/ulcid"
)

// decodeFilePath decodes and validates a {path} request body.
func decodeFilePath(w http.ResponseWriter, r *http.Request) (string, error) {
	var body dto.FilePathRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return "", err
	}
	if body.Path == "" {
		return "", apperr.New(apperr.CodeInvalidRequest, "path 不能为空", false)
	}
	return body.Path, nil
}

// ExportBackup implements POST /api/v1/backup/export: assembles the backup
// document exactly per schemas/backup.schema.json and writes UTF-8 JSON to
// the requested path (parent directories are created). The backup NEVER
// contains the API key (not part of settings), the session token, logs or
// translation_cache rows (docs/05 §5, docs/08 §7).
func (s *Server) ExportBackup(w http.ResponseWriter, r *http.Request) {
	path, err := decodeFilePath(w, r)
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	doc, err := s.assembleBackup(r)
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	if werr := backup.WriteFile(path, doc); werr != nil {
		apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeInvalidRequest, "备份文件写入失败", false, werr))
		return
	}
	writeJSON(w, http.StatusOK, dto.ExportResponse{Exported: true, Path: path})
}

// assembleBackup collects every exported table and builds the document.
func (s *Server) assembleBackup(r *http.Request) (*backup.File, error) {
	ctx := r.Context()

	settings, err := s.mergedSettings(ctx) // merged non-secret settings, provider row excluded
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "读取设置失败", true, err)
	}
	settingsRaw, merr := json.Marshal(settings)
	if merr != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "设置序列化失败", false, merr)
	}

	doc := backup.New(json.RawMessage(settingsRaw), func() string {
		return time.Now().UTC().Format(time.RFC3339)
	})

	historyRows, err := s.history.ListAll(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "读取历史失败", true, err)
	}
	for _, row := range historyRows {
		doc.TranslationHistory = append(doc.TranslationHistory, backup.HistoryEntry{
			ID:        row.ID,
			Kind:      row.Kind,
			InputText: row.InputText,
			Result:    json.RawMessage(row.ResultJSON),
			Source:    row.Source,
			Model:     row.Model,
			CreatedAt: row.CreatedAt,
		})
	}

	vocabRows, err := s.vocabulary.List(ctx, "")
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "读取单词本失败", true, err)
	}
	for _, row := range vocabRows {
		doc.Vocabulary = append(doc.Vocabulary, backup.VocabularyEntry{
			Lemma:        row.Lemma,
			Word:         json.RawMessage(row.WordJSON),
			SavedAt:      row.SavedAt,
			LastViewedAt: row.LastViewedAt,
		})
	}

	termRows := s.terms.Snapshot()
	for _, row := range termRows {
		doc.Terminology = append(doc.Terminology, backup.TerminologyEntry{
			ID:        row.ID,
			Source:    row.Source,
			Target:    row.Target,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}

	chatRows, err := s.chats.ListChats(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "读取会话失败", true, err)
	}
	for _, row := range chatRows {
		doc.Chats = append(doc.Chats, backup.ChatEntry{
			ID:                  row.ID,
			Title:               row.Title,
			ConversationContext: row.ConversationContext,
			CompactSummary:      row.CompactSummary,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		})
	}

	messageRows, err := s.chats.ListAllMessages(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "读取消息失败", true, err)
	}
	for _, row := range messageRows {
		var generationID *string
		if row.GenerationID.Valid {
			v := row.GenerationID.String
			generationID = &v
		}
		doc.Messages = append(doc.Messages, backup.MessageEntry{
			ID:               row.ID,
			ChatID:           row.ChatID,
			Role:             row.Role,
			Content:          row.Content,
			ReasoningContent: row.ReasoningContent,
			CreatedAt:        row.CreatedAt,
			GenerationID:     generationID,
		})
	}

	tabRows, err := s.tabs.List(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "读取标签页失败", true, err)
	}
	for _, row := range tabRows {
		var translationID *string
		if row.TranslationID.Valid {
			v := row.TranslationID.String
			translationID = &v
		}
		payload := json.RawMessage(row.PayloadJSON)
		if len(payload) == 0 {
			payload = json.RawMessage("{}")
		}
		doc.OpenTabs = append(doc.OpenTabs, backup.TabEntry{
			ID:            row.ID,
			Kind:          row.Kind,
			Title:         row.Title,
			TranslationID: translationID,
			Payload:       payload,
			Position:      row.Position,
			IsActive:      row.IsActive,
			UpdatedAt:     row.UpdatedAt,
		})
	}
	return doc, nil
}

// ImportBackup implements POST /api/v1/backup/import: reads the file,
// validates the minimal schema (required keys, integer backup_schema_version)
// and merges per docs/05 §6:
//   - settings: backup rows overwrite the same key (provider row skipped)
//   - vocabulary: lemma NOCASE, newer last_viewed_at wins (with its word_json)
//   - terminology: source NOCASE, backup value overwrites, existing id kept
//   - history: insert-by-id, existing ignored
//   - chats: insert-by-id, existing ignored
//   - messages: insert-by-id, existing ignored, orphans (unknown chat_id)
//     skipped and counted
//   - open_tabs: full replace, positions re-normalized 0..n-1, exactly one
//     is_active
func (s *Server) ImportBackup(w http.ResponseWriter, r *http.Request) {
	path, err := decodeFilePath(w, r)
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	doc, err := backup.ReadFile(path)
	if err != nil {
		switch {
		case errors.Is(err, backup.ErrUnsupportedVersion):
			apperr.WriteHTTP(w, apperr.New(apperr.CodeBackupVersionUnsupported,
				"备份版本过高，当前仅支持 backup_schema_version 1", false))
		case errors.Is(err, os.ErrNotExist):
			apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "备份文件不存在", false))
		default:
			apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeInvalidRequest, "备份文件无效", false, err))
		}
		return
	}

	counts, err := s.mergeBackup(r, doc)
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.ImportResponse{Imported: counts})
}

// mergeBackup applies the frozen merge rules and returns the applied counts.
func (s *Server) mergeBackup(r *http.Request, doc *backup.File) (dto.ImportCounts, error) {
	ctx := r.Context()
	var counts dto.ImportCounts

	// Chats first so message inserts always find their parent.
	for _, chat := range doc.Chats {
		exists, err := s.chats.ChatExists(ctx, chat.ID)
		if err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入会话失败", true, err)
		}
		if exists {
			continue
		}
		if err := s.chats.InsertChat(ctx, repository.ChatRow{
			ID:                  chat.ID,
			Title:               chat.Title,
			ConversationContext: chat.ConversationContext,
			CompactSummary:      chat.CompactSummary,
			CreatedAt:           chat.CreatedAt,
			UpdatedAt:           chat.UpdatedAt,
		}); err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入会话失败", true, err)
		}
		counts.Chats++
	}

	for _, msg := range doc.Messages {
		exists, err := s.chats.MessageExists(ctx, msg.ID)
		if err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入消息失败", true, err)
		}
		if exists {
			continue // insert-by-id: existing rows are ignored
		}
		chatExists, err := s.chats.ChatExists(ctx, msg.ChatID)
		if err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入消息失败", true, err)
		}
		if !chatExists {
			counts.MessagesSkipped++ // orphan message: chat_id unknown after merge
			continue
		}
		row := repository.MessageRow{
			ID:               msg.ID,
			ChatID:           msg.ChatID,
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			CreatedAt:        msg.CreatedAt,
		}
		if msg.GenerationID != nil {
			row.GenerationID = toNullString(*msg.GenerationID)
		}
		if err := s.chats.InsertMessage(ctx, row); err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入消息失败", true, err)
		}
		counts.Messages++
	}

	// Vocabulary: lemma NOCASE; the newer last_viewed_at wins and its
	// word_json replaces the stored one (saved_at keeps the first-save time).
	existingVocab, err := s.vocabulary.List(ctx, "")
	if err != nil {
		return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入单词本失败", true, err)
	}
	byLemma := make(map[string]repository.VocabularyRow, len(existingVocab))
	for _, row := range existingVocab {
		byLemma[lowerASCII(row.Lemma)] = row
	}
	for _, entry := range doc.Vocabulary {
		existing, found := byLemma[lowerASCII(entry.Lemma)]
		if found && entry.LastViewedAt <= existing.LastViewedAt {
			continue // stored entry is not older: nothing to do
		}
		savedAt := entry.SavedAt
		if found {
			savedAt = existing.SavedAt
		}
		if err := s.vocabulary.Upsert(ctx, repository.VocabularyRow{
			Lemma:        entry.Lemma,
			WordJSON:     string(entry.Word),
			SavedAt:      savedAt,
			LastViewedAt: entry.LastViewedAt,
		}); err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入单词本失败", true, err)
		}
		counts.Vocabulary++
	}

	// Terminology: source NOCASE; the backup value overwrites and the
	// existing row's id is kept.
	existingTerms := s.terms.Snapshot()
	bySource := make(map[string]repository.TerminologyRow, len(existingTerms))
	for _, row := range existingTerms {
		bySource[lowerASCII(row.Source)] = row
	}
	for _, entry := range doc.Terminology {
		if existing, found := bySource[lowerASCII(entry.Source)]; found {
			if err := s.terms.ImportUpdate(ctx, existing.ID, entry.Source, entry.Target, entry.UpdatedAt); err != nil {
				return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入术语表失败", true, err)
			}
			counts.Terminology++
			continue
		}
		id := entry.ID
		if id == "" {
			id = ulcid.New()
		}
		if _, err := s.terms.CreateWithID(ctx, id, entry.Source, entry.Target, entry.CreatedAt, entry.UpdatedAt); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				// The backup id belongs to a different source; remap.
				if _, err := s.terms.Create(ctx, entry.Source, entry.Target); err != nil {
					return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入术语表失败", true, err)
				}
			} else {
				return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入术语表失败", true, err)
			}
		}
		counts.Terminology++
	}

	// History: insert-by-id, existing ignored. normalized_input is not part
	// of the backup format and stays empty on imported rows.
	for _, entry := range doc.TranslationHistory {
		exists, err := s.history.HistoryExists(ctx, entry.ID)
		if err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入历史失败", true, err)
		}
		if exists {
			continue
		}
		if !json.Valid(entry.Result) {
			return counts, apperr.New(apperr.CodeInvalidRequest, "备份中的翻译结果不是合法 JSON", false)
		}
		if err := s.history.Insert(ctx, repository.HistoryRow{
			ID:         entry.ID,
			Kind:       entry.Kind,
			InputText:  entry.InputText,
			ResultJSON: string(entry.Result),
			Source:     entry.Source,
			Model:      entry.Model,
			CreatedAt:  entry.CreatedAt,
		}); err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入历史失败", true, err)
		}
		counts.TranslationHistory++
	}

	// Settings: backup rows overwrite the same key; the provider row is
	// skipped (the provider configuration is never part of a backup merge —
	// the API key lives only in the Credential Manager).
	if len(doc.Settings) > 0 {
		var rows map[string]json.RawMessage
		if err := json.Unmarshal(doc.Settings, &rows); err != nil {
			return counts, apperr.New(apperr.CodeInvalidRequest, "备份中的 settings 不是 JSON 对象", false)
		}
		for key, value := range rows {
			if key == settingsKeyProvider {
				continue
			}
			if err := s.settings.Put(ctx, key, string(value)); err != nil {
				return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入设置失败", true, err)
			}
			counts.Settings++
		}
	}

	// Tabs: replace everything with the imported set, positions
	// re-normalized 0..n-1 and exactly one is_active tab.
	normalized := backup.NormalizeTabs(doc.OpenTabs)
	rows := make([]repository.TabRow, 0, len(normalized))
	for _, tab := range normalized {
		rows = append(rows, repository.TabRow{
			ID:            tab.ID,
			Kind:          tab.Kind,
			Title:         tab.Title,
			TranslationID: toNullStringFromPtr(tab.TranslationID),
			PayloadJSON:   string(tab.Payload),
			Position:      tab.Position,
			IsActive:      tab.IsActive,
			UpdatedAt:     tab.UpdatedAt,
		})
	}
	if len(rows) == 0 {
		if err := s.tabs.DeleteAll(ctx); err != nil {
			return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入标签页失败", true, err)
		}
	} else if err := s.tabs.ReplaceAll(ctx, rows); err != nil {
		return counts, apperr.Wrap(apperr.CodeDatabaseError, "导入标签页失败", true, err)
	}
	counts.OpenTabs = len(rows)
	return counts, nil
}

func lowerASCII(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

func toNullString(v string) sql.NullString { return sql.NullString{String: v, Valid: true} }

func toNullStringFromPtr(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}
