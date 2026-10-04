package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// GetTabs implements GET /api/v1/tabs.
func (s *Server) GetTabs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.tabs.List(r.Context())
	if err != nil {
		apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "读取标签页失败", true, err))
		return
	}
	items := make([]dto.TabState, 0, len(rows))
	for _, row := range rows {
		var payload map[string]any
		if row.PayloadJSON != "" {
			if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
				apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "标签页数据损坏", false, err))
				return
			}
		}
		items = append(items, dto.TabState{
			ID:       row.ID,
			Kind:     row.Kind,
			Title:    row.Title,
			Position: row.Position,
			IsActive: row.IsActive,
			Payload:  payload,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

// PutTabs implements PUT /api/v1/tabs as a Phase 1 full replace.
func (s *Server) PutTabs(w http.ResponseWriter, r *http.Request) {
	var body []dto.TabState
	if err := decodeJSON(w, r, &body); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	rows := make([]repository.TabRow, 0, len(body))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, tab := range body {
		if tab.ID == "" {
			apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "标签页 id 不能为空", false))
			return
		}
		if tab.Kind != "word" && tab.Kind != "text" {
			apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "标签页 kind 仅支持 word 或 text", false))
			return
		}
		payloadJSON := "{}"
		if tab.Payload != nil {
			raw, err := json.Marshal(tab.Payload)
			if err != nil {
				apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "标签页 payload 必须是 JSON 对象", false))
				return
			}
			payloadJSON = string(raw)
		}
		rows = append(rows, repository.TabRow{
			ID:          tab.ID,
			Kind:        tab.Kind,
			Title:       tab.Title,
			PayloadJSON: payloadJSON,
			Position:    tab.Position,
			IsActive:    tab.IsActive,
			UpdatedAt:   now,
		})
	}
	if err := s.tabs.ReplaceAll(r.Context(), rows); err != nil {
		apperr.WriteHTTP(w, apperr.Wrap(apperr.CodeDatabaseError, "保存标签页失败", true, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
