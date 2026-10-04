package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/chat"
	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
)

// heartbeatInterval is the frozen SSE idle heartbeat (~15s, comment line
// only — it consumes no seq numbers).
const heartbeatInterval = 15 * time.Second

// generationEnvelope is the frozen per-event wrapper:
// {"generation_id":"<ULID>","seq":<int>,"data":{...}}.
type generationEnvelope struct {
	GenerationID string `json:"generation_id"`
	Seq          int    `json:"seq"`
	Data         any    `json:"data"`
}

// CreateGeneration implements POST /api/v1/chats/{id}/generations: validate,
// start the generation and stream SSE. Validation/404/409 failures answer
// with the standard JSON error envelope; once streaming starts the status is
// always 200 and failures travel as generation.error events.
func (s *Server) CreateGeneration(w http.ResponseWriter, r *http.Request) {
	chatID := chi.URLParam(r, "id")
	var body dto.ChatGenerationRequest
	if err := decodeJSON(w, r, &body); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	if body.Content == "" {
		apperr.WriteHTTP(w, apperr.New(apperr.CodeInvalidRequest, "content 不能为空", false))
		return
	}
	generationID, events, err := s.chatSvc.Start(r.Context(), chatID, chat.StartInput{
		Content:       body.Content,
		Thinking:      body.Thinking,
		ReferenceText: body.ReferenceText,
	})
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	s.streamGeneration(w, r, chatID, generationID, events)
}

// RegenerateChat implements POST /api/v1/chats/{id}/regenerate: delete the
// last assistant row and re-stream the SAME pipeline (no new user message,
// thinking off) as SSE.
func (s *Server) RegenerateChat(w http.ResponseWriter, r *http.Request) {
	chatID := chi.URLParam(r, "id")
	generationID, events, err := s.chatSvc.Regenerate(r.Context(), chatID)
	if err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	s.streamGeneration(w, r, chatID, generationID, events)
}

// CancelGeneration implements POST
// /api/v1/chats/{id}/generations/{generationId}/cancel — 202 when a
// generation with that id is active for the chat, 404 otherwise.
func (s *Server) CancelGeneration(w http.ResponseWriter, r *http.Request) {
	chatID := chi.URLParam(r, "id")
	generationID := chi.URLParam(r, "generationId")
	if err := s.chatSvc.Cancel(chatID, generationID); err != nil {
		apperr.WriteHTTP(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// streamGeneration pumps service events to the client as framed SSE events
// with a ~15s idle heartbeat. On client disconnect the generation is
// cancelled (partial content is persisted by the service) and the event
// channel is drained to completion so the producer can never block.
func (s *Server) streamGeneration(w http.ResponseWriter, r *http.Request, chatID, generationID string, events <-chan chat.Event) {
	sse, ok := newSSEWriter(w)
	if !ok {
		apperr.WriteHTTP(w, apperr.New(apperr.CodeInternal, "流式响应不受支持", false))
		return
	}
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case ev, open := <-events:
			if !open {
				return // generation settled and unregistered
			}
			sse.event(string(ev.Name), generationEnvelope{
				GenerationID: generationID,
				Seq:          ev.Seq,
				Data:         ev.Data,
			})
			if sse.failed {
				// Client write failed: treat like a disconnect.
				_ = s.chatSvc.Cancel(chatID, generationID)
				s.drainGeneration(events)
				return
			}
		case <-r.Context().Done():
			// Client disconnected: cancel, persist partial (service), drain.
			_ = s.chatSvc.Cancel(chatID, generationID)
			s.drainGeneration(events)
			return
		case <-heartbeat.C:
			sse.ping()
		}
	}
}

// drainGeneration consumes the remaining events without writing so the run
// goroutine always finishes (persist + registry cleanup) and closes it.
func (s *Server) drainGeneration(events <-chan chat.Event) {
	for range events {
	}
}
