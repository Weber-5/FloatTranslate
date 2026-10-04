package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// sseWriter frames Server-Sent Events exactly as frozen (docs/04 §11):
//
//	event: <name>\n
//	data: <one-line JSON>\n
//	\n
//
// plus comment heartbeats (": ping\n\n") written by the generation handlers
// while the stream is idle.
type sseWriter struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	failed   bool
	failedAt string
}

// newSSEWriter sets the streaming response headers and writes the 200 status
// (an SSE stream always answers 200; errors travel as generation.error
// events). ok is false when the ResponseWriter does not support flushing.
func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseWriter{w: w, flusher: flusher}, true
}

// event writes one named event with the payload marshaled to a single JSON
// line (HTML escaping off so Chinese text and markdown stay readable).
func (s *sseWriter) event(name string, payload any) {
	raw, err := marshalSSELine(payload)
	if err != nil {
		s.failed = true
		s.failedAt = name
		return
	}
	var b bytes.Buffer
	b.WriteString("event: ")
	b.WriteString(name)
	b.WriteString("\ndata: ")
	b.Write(raw)
	b.WriteString("\n\n")
	if _, err := io.WriteString(s.w, b.String()); err != nil {
		s.failed = true
		s.failedAt = name
		return
	}
	s.flusher.Flush()
}

// ping writes the frozen heartbeat comment line (no event, no seq).
func (s *sseWriter) ping() {
	_, _ = io.WriteString(s.w, ": ping\n\n")
	s.flusher.Flush()
}

// marshalSSELine marshals v as one JSON line without a trailing newline.
func marshalSSELine(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
