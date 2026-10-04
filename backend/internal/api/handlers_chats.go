package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/chat"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// toChatDTO maps a chat row to the wire shape.
func toChatDTO(row repository.ChatRow) dto.Chat {
	return dto.Chat{ID: row.ID, Title: row.Title, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

// toChatMessageDTO maps a message row to the wire shape.
func toChatMessageDTO(row repository.MessageRow) dto.ChatMessage {
	return dto.ChatMessage{
		ID:               row.ID,
		Role:             row.Role,
		Content:          row.Content,
		ReasoningContent: row.ReasoningContent,
		CreatedAt:        row.CreatedAt,
	}
}

// ListChats implements GET /api/v1/chats (updated_at DESC).
func (s *Server) ListChats(w http.ResponseWriter, r *http.Request) {
	rows, err := s.chatSvc.ListChats(r.Context())
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	items := make([]dto.Chat, 0, len(rows))
	for _, row := range rows {
		items = append(items, toChatDTO(row))
	}
	writeJSON(w, http.StatusOK, items)
}

// CreateChat implements POST /api/v1/chats — title defaults to 新会话 (the
// request body is optional).
func (s *Server) CreateChat(w http.ResponseWriter, r *http.Request) {
	var input chat.CreateChatInput
	if r.ContentLength != 0 {
		var body struct {
			Title string `json:"title"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			apperr.WriteHTTP(w, err)
			return
		}
		input.Title = body.Title
	}
	row, err := s.chatSvc.CreateChat(r.Context(), input)
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toChatDTO(row))
}

// UpdateChat implements PATCH /api/v1/chats/{id} {title}.
func (s *Server) UpdateChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	row, err := s.chatSvc.RenameChat(r.Context(), id, body.Title)
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toChatDTO(row))
}

// DeleteChat implements DELETE /api/v1/chats/{id} (messages cascade; an
// active generation is cancelled first).
func (s *Server) DeleteChat(w http.ResponseWriter, r *http.Request) {
	if err := s.chatSvc.DeleteChat(r.Context(), chi.URLParam(r, "id")); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListChatMessages implements GET /api/v1/chats/{id}/messages (ascending).
func (s *Server) ListChatMessages(w http.ResponseWriter, r *http.Request) {
	rows, err := s.chatSvc.Messages(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	items := make([]dto.ChatMessage, 0, len(rows))
	for _, row := range rows {
		items = append(items, toChatMessageDTO(row))
	}
	writeJSON(w, http.StatusOK, items)
}

// ClearChatMessages implements DELETE /api/v1/chats/{id}/messages (/clear):
// messages and compact summary are dropped, conversation context is kept.
func (s *Server) ClearChatMessages(w http.ResponseWriter, r *http.Request) {
	if err := s.chatSvc.ClearMessages(r.Context(), chi.URLParam(r, "id")); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetConversationContext implements GET /api/v1/chats/{id}/context.
func (s *Server) GetConversationContext(w http.ResponseWriter, r *http.Request) {
	content, err := s.chatSvc.ConversationContext(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.ContextValue{Content: content})
}

// PutConversationContext implements PUT /api/v1/chats/{id}/context.
func (s *Server) PutConversationContext(w http.ResponseWriter, r *http.Request) {
	var body dto.ContextValue
	if err := decodeJSON(w, r, &body); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	if err := s.chatSvc.SetConversationContext(r.Context(), chi.URLParam(r, "id"), body.Content); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetGlobalContext implements GET /api/v1/context/global.
func (s *Server) GetGlobalContext(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, dto.ContextValue{Content: s.chatSvc.GlobalContext(r.Context())})
}

// PutGlobalContext implements PUT /api/v1/context/global.
func (s *Server) PutGlobalContext(w http.ResponseWriter, r *http.Request) {
	var body dto.ContextValue
	if err := decodeJSON(w, r, &body); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	if err := s.chatSvc.SetGlobalContext(r.Context(), body.Content); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CompactChat implements POST /api/v1/chats/{id}/compact (manual compact;
// 409 when a generation is active). The response carries the new summary.
func (s *Server) CompactChat(w http.ResponseWriter, r *http.Request) {
	summary, err := s.chatSvc.Compact(r.Context(), chi.URLParam(r, "id"), false)
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.CompactResponse{Summary: summary})
}
