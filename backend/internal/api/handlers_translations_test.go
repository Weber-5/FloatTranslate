package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
)

// UX review 2026-10-07 (P0-1): one 47-minute translation request was observed
// in a user log. A request that overruns its budget must fail fast with a
// clear, retryable error instead of a generic provider failure.
func TestMapTranslationDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	<-ctx.Done() // let the budget expire

	mapped := mapTranslationDeadline(ctx, errors.New("provider stalled"))
	if mapped == nil {
		t.Fatal("expired budget must produce an error")
	}

	rec := httptest.NewRecorder()
	apperr.WriteHTTP(rec, mapped)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, rec.Body.String())
	}
	if body.Error.Code != string(apperr.CodeTranslationFailed) {
		t.Errorf("code = %q, want %q", body.Error.Code, apperr.CodeTranslationFailed)
	}
	if !body.Error.Retryable {
		t.Error("a timeout must be retryable")
	}
	if !strings.Contains(body.Error.Message, "超时") {
		t.Errorf("message should say it timed out, got %q", body.Error.Message)
	}
}

// The deadline rewrite must never touch errors from a live request.
func TestMapTranslationDeadlineKeepsOtherErrors(t *testing.T) {
	original := errors.New("boom")
	if got := mapTranslationDeadline(context.Background(), original); got != original {
		t.Errorf("live request error was rewritten: %v", got)
	}
	if got := mapTranslationDeadline(context.Background(), nil); got != nil {
		t.Errorf("nil error must stay nil, got %v", got)
	}
}
