package api

import (
	"net/http"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
)

// ClearBusiness implements POST /api/v1/data/clear-business (docs/05 §7):
// deletes translation_history, translation_cache, vocabulary, chats (messages
// disappear via ON DELETE CASCADE, which also clears compact/conversation
// state) and open_tabs. Settings (including proxy and context preferences)
// and the Credential Manager API key are preserved. Terminology is preserved
// (it is a user preference, not transient business data). Responds 204.
func (s *Server) ClearBusiness(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"translation_history", func() error { return s.history.DeleteAll(ctx) }},
		{"translation_cache", func() error { return s.cache.DeleteAll(ctx) }},
		{"vocabulary", func() error { return s.vocabulary.DeleteAll(ctx) }},
		{"chats", func() error { return s.chats.DeleteAllChats(ctx) }},
		{"open_tabs", func() error { return s.tabs.DeleteAll(ctx) }},
	} {
		if err := op.run(); err != nil {
			apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "清除业务数据失败: "+op.name, true, err))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResetApp implements POST /api/v1/data/reset-app (docs/05 §7): everything
// ClearBusiness deletes, plus the whole settings table, the open_tabs wipe
// and the Credential Manager API key deletion. The Tauri host separately
// clears the native window state. Responds 204.
func (s *Server) ResetApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"translation_history", func() error { return s.history.DeleteAll(ctx) }},
		{"translation_cache", func() error { return s.cache.DeleteAll(ctx) }},
		{"vocabulary", func() error { return s.vocabulary.DeleteAll(ctx) }},
		{"chats", func() error { return s.chats.DeleteAllChats(ctx) }},
		{"open_tabs", func() error { return s.tabs.DeleteAll(ctx) }},
		{"settings", func() error { return s.settings.DeleteAll(ctx) }},
		{"credential", func() error { return s.providerSettings.DeleteCredential() }},
	} {
		if err := op.run(); err != nil {
			apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "重置应用失败: "+op.name, true, err))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
